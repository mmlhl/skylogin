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

1. **DPI 虚拟化**：登录器进程不是 DPI 感知的。普通 goroutine 里
   `GetWindowRect`/`SetCursorPos` 拿到和设置的都是虚拟坐标（125% 缩放下
   差 25%），点击全部落空。需要物理坐标时先
   `SetProcessDpiAwarenessContext(-4)` 或用 `win.enterPhysicalPixels`
   （arrange.go）。排查输入"不生效"时先查这个。
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
- 取票请求带 ProcessID，路由按 pid 精确匹配；已回收路由的请求直接忽略
- 票据一次性，`slot.ticket` 用掉即清；应答有 10 分钟窗口 + 次数上限
