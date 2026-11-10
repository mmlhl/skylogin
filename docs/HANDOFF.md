# 交接文档（HANDOFF）

给下一位维护者的实操手册：架构怎么咬合、哪些行为是实测验证过的、
哪里有坑、出问题怎么查。用法层面的说明在 `README.md`，这里不重复。

## 构建与部署

```
cd web && npm run build        # 前端产物进 internal/web/dist（go:embed，必须先构建）
cd .. && go build -trimpath -ldflags "-s -w -H=windowsgui" -o skylogin.exe ./cmd/skylogin
```

- `-H=windowsgui` 必带，否则会弹控制台窗口
- 登录器是单实例：**换 exe 前先结束旧进程**（`taskkill /IM skylogin.exe /F`，
  需管理员），否则新进程会被互斥挡下静默退出——你会以为部署了其实没有
- 运行期数据在 `%APPDATA%\SkyLogin\`：账号、config.json、log.txt

## 架构一览

```
web UI (Vue, dist 内嵌) ──/api/*──> 登录器 (Go, 管理员运行)
                                     ├─ fever.Host   冒充发烧平台，接 op13 发票(op14)
                                     ├─ clients.go   pid→账号路由表 + 镜像生命周期
                                     ├─ fever.Mirror 硬链接镜像（多开隔离）
                                     ├─ webviewshim  浏览器壳：改 CEF channel，多开各用各的 profile
                                     ├─ win/wall.go  监控墙（DWM 缩略图）
                                     └─ mousearbiter 鼠标仲裁（多开焦点治理）
镜像目录 = 硬链接游戏本体 + 独立 preferences.sav + 浏览器壳；退出后自动删
```

## 实测行为矩阵（2026-10-28/29，多轮截图验证）

给游戏发合成输入能不能生效——**这是自动化方案的边界，别再重走验证路**：

| 界面 | 合成键盘（VK/扫描码/WM_KEYDOWN/WM_CHAR） | 合成鼠标（SendInput/窗口消息） |
|---|---|---|
| mpay 弹窗（MPAY_LOGIN，CEF 网页） | ✅ 空格触发默认按钮（登录），**免焦点** | ✅ |
| 引擎标题画面（D3D 画布） | ❌ 全部被过滤（真实激活后依然无效） | ✅ 有效（坐标正确时） |
| 引擎大厅/游戏内 | 未系统测试，按标题画面对待 | 同上 |

其他实测事实：
- mpay 弹窗里 Tab / 方向键**不能**移动渠道选中项，空格永远确认默认的
  iOS——想选 Android 只能鼠标点击
- 引擎画面无任何 Win32 子窗口、UIA 树 0 后代（CEF 未开无障碍）——
  "获取控件自动点击"不可行，只能坐标点击
- mpay 弹窗内容由游戏自带的 `webview_support_browser.exe`（CEF）渲染；
  该浏览器全局单实例（同 channel 共享 profile），多开必须靠浏览器壳改
  channel（见 webviewshim.go）
- 曾探索并**放弃**的方向：Interception 驱动级注入（内核驱动+反作弊风险）、
  硬件 HID（需外接设备）、屏幕取色定位按钮（放弃视觉方案）
- 曾实现后**整体删除**的自动登录/自动选渠道功能及其监控工具：
  见提交历史（automation.go / mousemon / screen.go），机制结论保留在上表

## 已知坑（都有过血的教训）

1. **DPI 虚拟化**：进程整体不是 DPI 感知的，普通 goroutine 里
   `GetWindowRect`/`SetCursorPos` 拿到和设置的都是虚拟坐标（125% 缩放下
   差 25%）。两个例外都靠"锁 OS 线程 + 临时切 PMv2"：游戏窗口排列
   （arrange.go `enterPhysicalPixels`）和主界面窗口（webview.go，建窗前
   切换并 `LockOSThread` 钉死，窗口因此恒为 PMv2、坐标全是物理像素）。
   感知切换只对本线程生效，goroutine 迁移会把切换留在旧线程上——所以
   这两处必须锁线程。排查输入"不生效"或几何对不上时先查这个。
   主窗位置尺寸的记忆在 win/wingeo.go：物理像素存 config（win_x/...），
   恢复前先对主屏分辨率，对不上就走默认算法（设计尺寸 × DPI 夹进工作区）。
2. **单实例互斥 + 提权**：skylogin 和 Sky.exe 都是管理员；用普通权限
   进程测试它们的 IPC/窗口行为会拿到误导性结果。
3. **镜像清理竞态**（已修，9618b26）：镜像清理观察者按 `sky-*` 前缀
   整目录删除，上一批退出 3 秒后触发；新一批在这窗口里启动会被删掉
   运行中的文件 → `UniCrashReporter.exe is missing` → 批量闪退。
   修复 = 删除前 `ProcessesFrom(dir)` 检查在用。若再出现"换批闪退"，
   先查这里。
4. **单实例检测竞态**：游戏的单实例检查是"启动时查一次"，几乎同时
   启动都能过；但如果上一批的子进程（UniCrashReporter / CEF 浏览器）
   还活着，新客户端可能被拦。强杀旧批后等 2 秒、顺手清一下这两个
   子进程再开。
5. **换 exe 要先杀旧进程**：单实例互斥会让新 exe 启动失败且无提示
   （见"构建与部署"）。
6. **launcher 日志有 GBK/UTF-8 混排**：Git Bash 里 `tail` 出来乱码时用
   `iconv -f GBK -t UTF-8` 或 PowerShell 读。

## 多开批量闪退调试指南

1. 看登录器 log.txt：`很快退出` = 启动后 8 秒看门狗发现进程没了
   （注意有误报：游戏自重启时也会触发，以复现脚本的进程计数为准）
2. 查 Windows 事件日志 `Application` Id=1000（真崩溃才有，游戏自退出没有）
3. 弹的 Warning 对话框文字用枚举 `#32770` 子窗口的方式读（CTyped 工具脚本
   思路：EnumWindows → ClassName=="#32770" → EnumChildWindows 取 Static 文字）
4. 镜像目录里有 `Sky.log`（常为空）和 `webviewsupport.cef904430\`；
   崩溃收集器是 `CrashHunter_PC3.dll`

## 监控墙设计要点（win/wall.go + app/wall.go）

- **缩略图每秒热替换**：`relayoutWall(true)` 每秒"注册新图→设属性→注销
  旧图"。DWM 缩略图在源表面挂起/恢复、全屏切换后会失去活链接，事件
  扳机（窗口数变化等）覆盖不全，热替换让任何失效 1 秒内自愈。
  墙窗口缩放走 `relayoutWall(false)`（只更新属性，避免 WM_SIZE 风暴重注册）
- **墙是 WS_EX_NOACTIVATE**：点击墙不抢游戏前台；切客户端由 wallClick
  主动完整激活（AttachThreadInput + SetForeground + SetFocus）
- **每格 = 顶部标题带 + 整块画面**：红 × 在标题带右端（缩略图区域外，
  永不被画面盖住）；点击区域 = 标题带 ∪ 画面
- **app→win 交互走 WallHooks**（SavedRect/RectSink/Actions/StopClient），
  win 层不含业务
- **客户端全退出时必须推空列表**（app/wall.go），否则墙上残留白格

## 鼠标仲裁（app/mousearbiter.go）

多开时后台客户端引擎的焦点标志可能停在"我有焦点"，持续把光标拉回
窗口中心。仲裁只做一件事：确认某客户端"确定不在前台"后给它补发
`WM_ACTIVATE(WA_INACTIVE)` 组消息，让引擎自己解锁。三条铁律：
1. 绝不 `SetForegroundWindow`（多开窗口全重叠，抢焦点=画面跳变）
2. 绝不给前台窗口发通知（误发=正在玩的游戏松鼠标）
3. 前台读不清（切换瞬态）就整轮跳过

`ForegroundWindow()` 必须用 `GetForegroundWindow`——曾经写成
`SetForegroundWindow`（读前台变成写前台，引发大面积误判），有回归
测试 `TestForegroundWindowMatchesSystem` 看守。

## 登录/取票链路速查

- 网易官方：token → `create_ticket`（a50 上下文优先，失败退回 token
  自带上下文）→ op13 应答。票据预签 + 按 pid 路由（clients.go）
- 渠道（B站/4399/vivo）：凭证保活（ensureChannelCredential）→ 预签；
  vivo 的 openToken 短命，取票失败时走 Cookie 静默续期重签
- 九游（ensureUCCredential）：用 refreshToken 滚动续期，sid 只有 24 小时，
  续一次换一张新票、旧票当场作废，所以续完必须立刻落盘
- 取票请求带 ProcessID，路由按 pid 精确匹配；已回收路由的请求直接忽略
- 票据一次性，`slot.ticket` 用掉即清；应答有 10 分钟窗口 + 次数上限


## v1.2.0 变更速览（2026-10-30 发版）

- **凭证不再频繁过期**：短信登录改走 `mobile/finish`（账号级 token，
  s=yd_… 无 lt/od，与发烧平台同款）；mpay.dll 窗口登录改回调 token 优先
  （旧代码优先用票换会话级 token，等于主动降级）。三条链路详见 `.ai/08`
- **多开画质/帧率**：四档（LowPower/Default/HighQuality/Presentation）+ 120 帧
- **窗口堆叠方案**：双摞（默认）/两行横向/两列竖向/满扇形，
  多开弹窗可选、监控墙顶栏一键重排（`cascade:<scheme>` 透传）
- **排列时机两重防闪退**：等全部客户端发票成功（凭证链路通的业务信号）
  + 3 秒宽限；窗口矩形稳定 2.5 秒才摆。启动期闪退弹窗提示（launchAlert）
- **弹窗跟随**：摆主窗时同 pid 的 MPAY 登录/选渠道/投影窗一起平移
- **重启登录器不丢状态**：按窗口标题 "N · 账号名" 恢复 pid→账号路由，
  监控墙自动重开；墙加 WS_EX_APPWINDOW（任务栏/Alt+Tab 可见）
- **凭证文件全部 DPAPI 加密**（internal/secretfile）：accounts/config/
  三份设备指纹/last_scan，明文文件读时自动升级
- **新用户默认控制台界面**：壳偏好存储改 1-based，0=未设置回落默认

监控墙按钮：圆角胶囊 + 主题配色（排列淡金、停止红调）。

## v1.6.2 凭证判定修正（2026-11-10）

起因：用户日志里 9 个网易账号全被判"不可用"，实际是本机解析不出
`service.mkey.163.com`，凭证本身正常。

**约定（以后改动必须遵守）**

1. **网络失败不等于凭证失效。** 只有服务端明确回 `1306` 才能标 `expired`。
   DNS 失败、超时、5xx、非 JSON 一律归为"暂时无法确认"：不写状态、不弹横幅、
   不触发重登。判定入口是 `internal/mpay.Classify` + `internal/app.credErrFrom`。
2. **6008 不是鉴权错误码。** 它是加载湾"记住槽未命中"的正常回退信号，
   每个没走过扫码写槽的账号都会拿到。
3. **自动重登有 10 分钟冷却**（`reloginthrottle.go`）。用户手动刷新凭证不受限。
4. **DNS 兜底只在系统解析失败后生效**，进程内实现，不改系统配置；
   兜底生效会写一行日志。
5. 启动日志带 `vcs.revision`。**CI 构建一定带 `+dirty`**，这不是源码不干净：
   workflow 里有 `npm run build` 会重新生成 `internal/web/dist`（该目录入库、
   被 go:embed 使用），git 因此认为工作区被修改。判断"跑的是哪份代码"看
   commit 哈希，别看 dirty 标记。

**改动文件**

- 新增：`internal/netdns/`、`internal/mpay/errkind.go`、
  `internal/app/credstate.go`、`internal/app/reloginthrottle.go`、
  `internal/app/buildinfo.go`
- 修改：`internal/mpay/{mpay,ticket}.go`（DNS 兜底、NotJSONError、
  `GetUserInfo` 检查业务码）、`internal/app/{start,credcheck,store,hint,
  channelplatform,app,relogin}.go`、`internal/launcher/{launcher,session}.go`
- 测试：`internal/mpay/errkind_test.go`、`internal/app/credstate_test.go`

**未完成 / 待确认**

- `status=1003`（记住账号写库失败，13 次全失败）：只出现在 1.6.0
  （二进制里有 `游戏账号库` / `wasm invoke`）；1.6.1 已移除该功能
  （提交 d5fe993「移除 mpay.db 直写功能」），日志里这些失败全部发生在
  1.6.0 阶段，**不需要再修**。
- 渠道服（B站/4399/九游/vivo）的过期判定仍按原逻辑，未纳入本轮分类。

## 渠道凭证的网络误判（2026-11-10）

用户反馈：机器上连 baidu / qq / github 全部 10061（连接被拒），B站登录
因此报"B站凭证验证失败"。凭证是好的，问题在网络。

**约定（渠道链路同样适用）**

1. **网络失败不等于凭证失效。** 判据统一走 `internal/neterr.IsNetwork`
   （DNS / 拨号 / TLS / 超时），mpay 与 channel 共用同一套。
2. **`finalize*` 在验证请求网络失败时保留登录结果**，只记一条日志，
   交给启动时的 `uni_sauth` 再验。让用户白登录一遍是最伤的。
3. **`channelSAUTH` 包装错误必须用 `%w`。** 用 `%s` 会断开错误链，
   上层根本分不清网络与凭证——这个坑很隐蔽。
4. **渠道的 HTTP 客户端必须带超时。** `bili.Verify` 以前用
   `http.PostForm`（等于 `DefaultClient`，没有超时），网络卡住时请求永久挂起。

**已知未处理**

vivo 的静默续期（`refreshVivoCredential`）失败仍会标过期。那条路是重开
登录窗，失败原因混着"用户取消"和"网络问题"，暂时无法分类。

**排错口诀**

拿到 `actively refused`（10061）先让用户跑
`Test-NetConnection www.baidu.com -Port 443`：
通了是代理问题，不通是本地拦截（hosts / 安全软件 / TUN 残留）。
注意 10061 是"对方回 RST"，10060 才是超时，两者含义完全不同。

## 应用宝渠道（2026-11-10 接入）

**链路**：微信 / QQ 两条扫码，都是纯 HTTP，不开窗、不需要浏览器。

- 微信：`wx_scan_code_login` → 微信二维码 → 长轮询 → `wx_verify_code` → atk/rtk
- QQ：`m_authorize` → `xlogin` → `ptqrshow` → `ptqrlogin`（**必须带 `pt_openlogin_data`**）→ `QQCodeLogin`

**代码位置**

| 想改什么 | 去哪个文件 |
|---|---|
| QQ 扫码协议 | `internal/myapp/qq.go` |
| 微信续期 | `internal/myapp/refresh.go` |
| 渠道注册与两个扫码方式 | `internal/channel/myapp.go` |
| 落库、extra_data 拼装、续期 | `internal/app/myappaccount.go` |
| 取票时的身份参数 | `internal/app/app.go` 的 `buildSauth` |

**寿命**

| 渠道 | 凭证 | 续期 |
|---|---|---|
| 微信 | atk 2h / rtk 30 天可滚动 | 有，能永久续 |
| QQ | atk 60 天 | **无**（YSDK 没实现，反编译确认） |

**二维码怎么到界面**：后端生成后存进 `loginProgress.QR`，经
`/api/myapp/login/status` 的 `qr` 字段（PNG base64）下发，
前端 `ChannelAskDialog` 显示。`loginProgress.set()` 会保留 QR，否则文案更新会冲掉。

**关对话框必须调 `/api/channel/login/cancel`**：扫码最长阻塞 5 分钟，
不取消的话 `tracker.begin` 一直拦着，用户再点就是"没反应"。
轮询用 `PollCtx` / `PollQQCtx`，取消能立刻断开长轮询。
