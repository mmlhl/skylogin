package ucsdk

// 业务接口封装。
//
// 所有请求共享同一个流程：
//
//	1. 首次调用前先 getSecurityKey 拿 RSA 公钥（一次性，缓存起来）
//	2. 把业务参数包成 {id, service, data, game, client}
//	3. seal 成 {"k","i","d"} 发出去
//	4. 拆 {"c","d"}，用请求时的 key/iv 解 d
//	5. 把 state.code 翻成 Go 错误

import (
	"bytes"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// GameInfo 是请求里必须带的游戏身份。
//
// 这些值按「游戏 × 渠道」而定：光遇九游包的 gameId 是 918836、
// channelId 是 KM_75033，换成别的游戏就完全不同。
// 所以由调用方从游戏档案里取，不在本包里写死。
type GameInfo struct {
	CPID           int    `json:"cpId"`
	GameID         int    `json:"gameId"`
	ChannelID      string `json:"channelId"`
	ServerID       int    `json:"serverId"`
	ServerName     string `json:"serverName"`
	RoleID         string `json:"roleId"`
	RoleName       string `json:"roleName"`
	RoleLevel      string `json:"roleLevel"`
	ZoneID         string `json:"zoneId"`
	ZoneName       string `json:"zoneName"`
	APKChannelID   string `json:"apkChannelId"`
	VersionCode    int    `json:"versionCode"`
	VersionName    string `json:"versionName"`
	BrandID        string `json:"brandId"`
	FirstInstallAt int64  `json:"firstInstallTime"`
	PackType       string `json:"packType"`
	Sign           string `json:"sign"`
	RuntimeType    string `json:"runtimeType"`
}

// Client 是协议客户端。可并发使用。
type Client struct {
	HTTP    *http.Client
	Game    GameInfo
	Verbose bool

	fetchMu        sync.Mutex // 串行化「取会话公钥」，避免并发重复打服务端
	mu             sync.Mutex
	publicKey      string      // getSecurityKey 下发的原文，空表示还没取到
	lastCode       int         // 上一次 call 的业务码，供需要分支的接口判断
	clientOverride *ClientInfo // 非空时覆盖内置的 client 段
}

// New 创建客户端。game 由调用方按当前游戏与渠道填。
func New(game GameInfo) *Client {
	return &Client{
		HTTP: &http.Client{Timeout: 20 * time.Second},
		Game: game,
	}
}

// state 是服务端统一的状态块。
type state struct {
	Code    int    `json:"code"`
	Msg     string `json:"msg"`
	SubCode int    `json:"subCode"`
}

// apiError 带业务码的错误，调用方可以按 Code 分支处理。
type apiError struct {
	Service string
	Code    int
	Msg     string
}

func (e *apiError) Error() string {
	if e.Msg == "" {
		return fmt.Sprintf("%s 失败（code=%d）", e.Service, e.Code)
	}
	return fmt.Sprintf("%s 失败（code=%d）：%s", e.Service, e.Code, e.Msg)
}

// Code 取出错误里的业务码；不是业务错误时返回 0。
func Code(err error) int {
	if e, ok := err.(*apiError); ok {
		return e.Code
	}
	return 0
}

// envelopeJSON 是信封的线上形态。
//
// 用结构体而不是 map，是为了固定字段顺序：服务端对请求体做过哈希校验，
// 顺序变了会直接判「非法请求」。字段顺序与实测抓包一致（i, k, d, v）。
type envelopeJSON struct {
	I string `json:"i"`
	K string `json:"k"`
	D string `json:"d"`
	// v 是信封版本号，不是常量：取公钥那条是 1，业务请求是 5。
	// 发错时服务端仍回外层 c=1，但响应会用另一套密钥加密，怎么都解不开。
	V int `json:"v"`
}

// request 是一次业务请求的完整形态。
//
// 用结构体而不是 map：字段顺序必须与 SDK 一致（id, service, data, game, client），
// 服务端对解密后的明文做哈希校验，顺序变了会被判成非法请求。
type request struct {
	ID      int64  `json:"id"`
	Service string `json:"service"`
	// Data 用 any：不同接口的字段顺序在抓包里是固定的，传 struct 才能保证
	Data any      `json:"data"`
	Game GameInfo `json:"game"`
	// Client 按阶段不同（字段集合与顺序都不一样），所以用 any，
	// 由调用方传 ClientInfo 或 BizClientInfo。
	Client any `json:"client"`
}

// response 是解密后的响应。
type response struct {
	State state           `json:"state"`
	ID    int64           `json:"id"`
	Data  json.RawMessage `json:"data"`
}

// call 用当前会话公钥发一次业务请求，返回解密后的 data 段。
//
// host 必须按服务选对（见 const.go），选错会得到「非法请求」。
func (c *Client) call(host, service string, data any) (json.RawMessage, error) {
	pub, err := c.ensureKey()
	if err != nil {
		return nil, err
	}
	// 业务请求：信封版本 5、brandId "JY"、client 业务版
	return c.callWith(pub, host, service, data, stageBiz)
}

// stage 表示请求所处的阶段。
//
// 三处一起变：信封版本号、game.brandId、client 段的结构。
// 混用任何一处都不会提示「格式错」，只会得到外层 c=1 但响应解不开，
// 或者能解开但业务回 code=42「系统繁忙」。
type stage int

const (
	stageBootstrap stage = iota // 取公钥
	stageBiz                    // 业务
)

// callWith 用指定公钥加密后发出请求。
//
// 单独拆出来是必要的：换取会话公钥那一步必须用引导公钥，
// 信封版本 1、game.brandId 空串；业务请求则是版本 5、brandId "JY"。
func (c *Client) callWith(pub *rsa.PublicKey, host, service string, data any, st stage) (json.RawMessage, error) {
	envVersion, brandID := 5, "JY"
	var client any = SkyBizClient()
	if st == stageBootstrap {
		envVersion, brandID, client = 1, "", SkyClient()
	}
	game := c.Game
	game.BrandID = brandID
	body, err := json.Marshal(request{
		ID:      time.Now().UnixMilli(),
		Service: service,
		Data:    data,
		Game:    game,
		Client:  client,
	})
	if err != nil {
		return nil, err
	}

	if c.Verbose {
		fmt.Printf("[ucsdk] %s 明文: %s\n", service, string(body))
	}

	env, err := seal(body, pub)
	if err != nil {
		return nil, err
	}
	if c.Verbose {
		fmt.Printf("[ucsdk] %s 本次 key=%s iv=%s\n", service,
			base64.StdEncoding.EncodeToString(env.key),
			base64.StdEncoding.EncodeToString(env.iv))
	}
	payload, err := json.Marshal(envelopeJSON{
		I: env.I,
		K: env.K,
		D: env.D,
		V: envVersion,
	})
	if err != nil {
		return nil, err
	}
	// base64 里的 = 要转义成 \u003d。
	//
	// SDK 用的是 Gson 的 HTML 安全转义，它会把 = < > & ' " 都写成 \uXXXX。
	// 按理说 JSON 解析后两者等价，但服务端对【原始 body】做了哈希校验，
	// 转义前后字节不同就会判成非法请求。
	payload = bytes.ReplaceAll(payload, []byte("="), []byte(`\u003d`))

	url := "http://" + host + pathPrefix + service + querySuffix
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	// 服务端要求声明成表单，但实际发的是 raw JSON —— 这是实测值，别"修正"
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "okhttp/${project.version}")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s 请求失败: %w", service, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s 返回 HTTP %d", service, resp.StatusCode)
	}

	var outer struct {
		C json.Number `json:"c"`
		D string      `json:"d"`
	}
	if err := json.Unmarshal(raw, &outer); err != nil {
		return nil, fmt.Errorf("%s 响应不是预期结构: %s", service, trim(string(raw), 200))
	}
	if c.Verbose {
		fmt.Printf("[ucsdk] %s 原始响应: %s\n", service, trim(string(raw), 200))
	}
	if outer.C != "" && outer.C.String() != "1" {
		// 外层 c 不是 1：信封层面就被拒了，通常意味着公钥过期或参数不合规
		return nil, &apiError{Service: service, Code: atoiSafe(outer.C.String()), Msg: "外层校验未通过"}
	}
	plain, err := env.open(outer.D)
	if err != nil {
		return nil, fmt.Errorf("%s 响应解密失败: %w", service, err)
	}
	if c.Verbose {
		fmt.Printf("[ucsdk] %s 外层 c=%s d长度=%d 解密得 %d 字节\n",
			service, outer.C.String(), len(outer.D), len(plain))
		fmt.Printf("[ucsdk] %s 响应明文: %s\n", service, trim(string(plain), 1500))
	}

	var inner response
	if err := json.Unmarshal(plain, &inner); err != nil {
		return nil, fmt.Errorf("%s 响应内容不是 JSON: %s", service, trim(string(plain), 200))
	}
	if inner.State.Code != 0 && inner.State.Code != CodeOK && inner.State.Code != CodeNeedAccount {
		return nil, &apiError{Service: service, Code: inner.State.Code, Msg: inner.State.Msg}
	}
	if inner.State.Code == CodeNeedAccount {
		// 214 不是失败，是「需要选账号」——把数据一起带出去，由调用方处理
		c.mu.Lock()
		c.lastCode = CodeNeedAccount
		c.mu.Unlock()
		return inner.Data, &apiError{Service: service, Code: CodeNeedAccount, Msg: inner.State.Msg}
	}
	c.mu.Lock()
	c.lastCode = 0
	c.mu.Unlock()
	return inner.Data, nil
}

// ensureKey 确保手上有可用的会话公钥。
//
// 会话公钥由服务端下发，而「下发」那一步本身要加密，用的是内置的引导公钥。
// 所以这里第一次调用时会先跑一遍 FetchSecurityKey，拿到之后缓存复用。
// 之后只有服务端主动轮换、或缓存被清掉时才会再跑一次。
func (c *Client) ensureKey() (*rsa.PublicKey, error) {
	// 只在缺 key 时才上锁，避免热路径上的空等
	c.mu.Lock()
	raw := c.publicKey
	c.mu.Unlock()

	if raw == "" {
		c.fetchMu.Lock()
		// 双检：等锁期间可能已经有别的 goroutine 取回来了
		c.mu.Lock()
		raw = c.publicKey
		c.mu.Unlock()
		if raw == "" {
			_, err := c.FetchSecurityKey()
			c.mu.Lock()
			raw = c.publicKey
			c.mu.Unlock()
			if err != nil && raw == "" {
				c.fetchMu.Unlock()
				return nil, fmt.Errorf("获取会话公钥失败: %w", err)
			}
		}
		c.fetchMu.Unlock()
	}

	if raw == "" {
		// 取回来了但服务端没给 securityKey，退回上次实测值兜底
		raw = builtinPublicKey
	}
	pub, err := parsePublicKey(raw)
	if err != nil {
		return nil, fmt.Errorf("公钥不可用: %w", err)
	}
	return pub, nil
}

// SetPublicKey 注入一份公钥（例如从抓包或配置里拿到的）。
// 传空值表示回到内置的实测值。
func (c *Client) SetPublicKey(raw string) {
	c.mu.Lock()
	c.publicKey = raw
	c.mu.Unlock()
}

// Call 发一次任意业务请求，返回解密后的 data 段。
//
// 给「还没封装成专用方法」的接口用，也是排错时最快的探针：
// 能拿到明文就说明信封、公钥、host 全部正确，问题只可能在业务参数上。
func (c *Client) Call(host, service string, data any) (json.RawMessage, error) {
	return c.call(host, service, data)
}

// SetClient 改写请求里的 client 段。
// 传 nil 表示回到内置的设备环境（抓包时的 OnePlus PJZ110）。
func (c *Client) SetClient(v *ClientInfo) {
	c.mu.Lock()
	c.clientOverride = v
	c.mu.Unlock()
}

// client 返回本次请求使用的 client 段。
func (c *Client) client() ClientInfo {
	c.mu.Lock()
	v := c.clientOverride
	c.mu.Unlock()
	if v != nil {
		return *v
	}
	return SkyClient()
}

// atoiSafe 把字符串转成整数，转不了就返回 0。
func atoiSafe(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// FetchSecurityKey 用引导公钥请求一次会话公钥，并立刻切换过去。
//
// 必须用 bootstrapPublicKey 加密：服务端要用与它配对的私钥才能解开信封，
// 换成别的公钥一律回 c=-1。拿到 securityKey 后缓存进 Client，
// 后续业务请求（call）都用这一把。
func (c *Client) FetchSecurityKey() (string, error) {
	pub, err := parsePublicKey(bootstrapPublicKey)
	if err != nil {
		return "", fmt.Errorf("引导公钥不可用: %w", err)
	}
	raw, err := c.callWith(pub, HostSDK, ServiceSecurityKey, struct{}{}, stageBootstrap)
	if err != nil {
		return "", err
	}
	var d struct {
		SecurityKey string `json:"securityKey"`
		Device      string `json:"device"`
	}
	if c.Verbose {
		fmt.Printf("[ucsdk] securityKey 响应明文: %s\n", string(raw))
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return "", fmt.Errorf("公钥响应解析失败: %w", err)
	}
	if d.SecurityKey != "" {
		c.SetPublicKey(d.SecurityKey)
	}
	return d.SecurityKey, nil
}

func trim(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
