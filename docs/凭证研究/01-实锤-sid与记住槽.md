# 实锤一：平台"记住账号"是怎么回事

## 用大白话说

网易发烧平台登录时，会让你"记住这个账号"，下次直接点一下就登录，不用扫码。
这个"记住"存在服务端，不是存在你电脑上。

我们以前扫码是**匿名**的，所以平台从来不记住我们扫的号。
平台自己扫码时带着它的"登录会话"，所以能记住。

我们现在也带上会话，平台就记住了。

**记住之后的好处**：这个号哪怕临时凭证过期了，也能凭"记住"直接出票，不用再扫。

**限制**：一个平台账号只能记住**一个**号。扫了新的，旧的就被顶掉。

## 几个卡了很久的问题，现在有答案了

### 问："记住"是按电脑分的，还是按账号分的？

按**平台账号**分。

同一台电脑上，用两个不同的平台账号去问"你记住谁了"：

```
平台账号 1  →  记住的是 199****3859
平台账号 2  →  记住的是 js2fv5
```

两个答案互不影响。而同一个平台账号，反复登录问多少次，答案都是同一个。

### 问：那多登录几次能不能多几个"记住位置"？

不能。反复登录只是换了一张新会话，位置还是那一个。只有**换一个平台账号**才多一个位置。

### 问：换个设备登录能不能多几个位置？

不能。而且就算想试也过不去——凭证是绑设备的，换个设备号去用会被直接拒绝
（`sauth error`）。

## 怎么写入、怎么读取（技术附录）

**平台账号 = 加载湾的 `id`（如 `user_A`）。"记住位置"挂在它身上。**

换会话（sid）：

```
POST https://loadingbaycn.webapp.163.com/app/v1/login
Content-Type: application/json
channel: platform
cv: w1.18.45.8
deviceid: <平台设备的 MAC 形式>
uni-mac: <平台设备指纹>

{"sauth_json":"{\"gameid\":\"a50\",\"login_channel\":\"netease\",
  \"app_channel\":\"netease.allysdk3rd\",\"platform\":\"pc\",
  \"sdkuid\":\"<网易账号 id>\",\"sessionid\":\"<账号级 token>\",
  \"deviceid\":\"<签发该 token 的设备>\",\"get_access_token\":\"1\", ...}",
 "state":""}
→ {"data":{"sid":"<64 位>","id":"user_A",...},"code":200}
```

前提：`sessionid` 必须是**账号级 token**，且 `deviceid` 要和签发它的设备一致。
扫码号的会话级 token 这里是过不去的（`1000 sauth error`）。

写"记住"——换票时带上 sid：

```
GET /app/v1/third_party_account/ticket?code=<扫码拿到的 code>&app_id=63&device_id=<设备>
请求头：sid: <上面换到的会话>
```

读"记住"：

```
GET /app/v1/third_party_account?app_id=63&device_id=<设备>
请求头：sid: <会话>
→ {"data":{"account_id":"acct_A","account_name":"...","login_channel":"netease"}}
```

免扫出票：

```
GET /app/v1/third_party_account/ticket?code=&app_id=63&account_id=<账号>
请求头：sid: <会话>
→ {"data":{"ticket":"AAAA…"}}
```

## 实测记录

| 时间 | 做了什么 | 我们设备的"记住位置" |
|---|---|---|
| 19:31 | 匿名换票若干次 | 空 |
| 19:41 | 带 sid 换票（扫 acct4） | `acct4` |
| 19:42 | 带 sid 换票（扫 acct3） | 变成 `acct3`，acct4 被顶掉 |

免扫出票实测：

```
account_id=acct_C（在位置里） → 拿到票 ✅
account_id=acct_D（不在）     → 6008 登录已失效，重新扫码授权
account_id=acct_E（不在）     → 6008
```

两个会话各记各的：

```
acct1 的会话 → 记住 acct_A
acct2 的会话 → 记住 acct_B
```

## 代码

- `internal/launcher/session.go`：`Login()` / `SavedAccount()` / `TicketForAccount()`
- `internal/launcher/launcher.go`：`ExchangeTicketWithSid()`
- `internal/app/platformsid.go`：扫码时挑一个空位置，续期时挨个位置试
- `internal/app/scan.go`：扫码换票带 sid
- `internal/app/start.go`：凭证失效时先试"记住的位置"

提交：`b32bcc6`
## 重登不会清掉"记住"（已验证）

和临时会话不一样：**"记住"跟登录会话是两回事**，重登不会把它清掉。

实测两步：

```
密码重登了"位置的主人"那个号
  → 位置里还是 acct_A，照样能免扫出票 ✅

又给"被记住的号"在别处造了一条新会话（模拟它被人重新登录）
  → 位置里还是 acct_A，照样能免扫出票 ✅
```

所以：

| 动作 | 临时会话 | 记住的位置 |
|---|---|---|
| 同一个号在别处重登 | 被顶掉 | **不受影响** |
| 重新扫码（带会话） | — | 换成新扫的那个号 |

**"记住"只在"用同一个平台账号再扫一次"时才会变。**

## 2026-11-06 凌晨：记住槽能不能突破（结论：不能，但搞清了它的键是什么）

### 三个实验

**1. 同一个账号换设备号 → 拿不到 sid**

```
用账号原设备 → sid=8awd2VmO8vhY… ✅
用新登记的 a50 设备 → sid="" err=换会话失败: code=1000 sauth error
```

token 是绑设备的（token 里的 `odi` 必须和 `deviceid` 一致），所以"一账号多设备 = 多槽"这条路走不通。

**2. 同一个账号换平台 udid → sid 变了，但槽是同一个**

```
默认 udid → sid=qE_qYHiJ3_7p…   该槽记住的是 aebgex4u4ruo…
换一个 udid → sid=r0bEtlQlqOGg…  该槽记住的**还是** aebgex4u4ruo…
```

也就是说：`udid` 只影响发出来的会话串，**记住槽并不按 udid 分**。

**3. 接口本身就是"一槽一号"**

`GET /app/v1/third_party_account?app_id=63` 返回的是单个 `data.account_id`，
不是列表——服务端的数据模型就是每个平台会话记一个账号。

### 结论

**槽位数 = 你能产生 sid 的网易账号数，一账号一槽，没有免费扩展的办法。**

但这并不致命：

- `sa=-9`（账号级）token 是长期有效的，根本不需要槽
- `sa=3`（会话级）靠 `create_ticket + login/ticket` 滚动续期，也不需要槽
- 槽只是"token 真的丢了"时的免扫兜底

所以真正的持久化抓手是 **token 等级 + 滚动续期**，不是扩槽。
（顺带：`launcher.LoginExt` 可以指定 udid，留作诊断用。）
