# 光遇账号登录器 (skylogin)

> 光·遇 PC（网易 ma75）× 华为渠道 / 网易官方账号 的轻量免扫码登录器。
> 单文件 exe，**不装证书、不改 hosts、不开代理、不注入游戏进程**。

## 它做什么

光·遇 PC 是由「发烧游戏平台」启动的。启动后，游戏会向平台要一张**一次性票据**，拿到才继续登录流程。

这个工具就是**扮演那个平台**：

```
游戏 --(op13 要票)--> 本工具 --(op14 发票)--> 游戏进入大厅
                         │
                         ├─ 用华为账号换 access_token
                         ├─ uni_sauth 换登录包
                         └─ MPay 二维码 scan+confirm 换一次性票据
```

因为扮演的是"启动器"，所以**完全不需要中间人劫持**——没有证书、没有 hosts、没有代理、没有 443 端口占用。

## 怎么用

1. **双击 `skylogin.exe`**（弹 UAC 点「是」；光遇本体要求管理员权限）
2. 左边是账号列表，第一次用点 **＋ 添加华为账号**（华为手机扫码）
   或 **＋ 添加网易账号**（光遇手游 / 网易大神扫码）
3. 选中账号，点右边的大按钮 **启动光遇**
4. 等游戏加载完就进大厅了

之后每次只要：双击 → 点启动。

- 多个账号可以随时在左边切换；双击账号也能直接启动
- 右键账号可改别名、复制玩家 ID、删除
- 关闭窗口后托盘图标还在，右键可「显示主界面 / 启动光遇 / 退出」
- 游戏路径、设备标识、是否自动关闭发烧平台，都在右侧「设置」里。游戏路径默认自动检测（读发烧平台的装机信息），换盘或自定义目录可以在设置里手动指定
- 出问题时点「打开日志」，把 `log.txt` 发出来

## 凭证自动刷新

华为 ST 是长期凭证，工具会在**每次点「启动光遇」时**用它重新签发 access_token 与
gameAuthSign。这样即使隔了很久没登录，也不用重新扫码。

如果 ST 本身失效（华为侧回收），界面会提示"请重新扫码"，点一次二维码即可。

## 日志

出错时把 `%APPDATA%\SkyLogin\log.txt` 发出来即可，里面记录了每一步（账号、刷新、
取票、IPC 请求、发票据）。

## 数据存放

全部在本机，不上传：

```
%APPDATA%\SkyLogin\
  accounts.json        账号（含华为 ST / access_token / gameAuthSign）
  sdk_device.json      设备指纹（SAUTH 用）
  huawei_device.json   华为渠道设备指纹
```

删掉这个目录 = 彻底清除。

## 需要的前置条件

- Windows 10/11 x64（自带 WebView2 运行时；缺失时会自动退回系统浏览器）
- 已安装光·遇 PC（工具会从发烧平台的注册表信息里自动找到 `sky.exe`，不需要填路径；找不到时可在设置里手动选择）
- 一个能扫码登录的华为账号，且该华为账号已绑定光·遇

## 常见问题

**Q：提示「发烧平台正在运行，不能同时启用平台模拟」？**
A：本工具会自动结束发烧平台进程来让出 IPC 窗口；若失败，请手动退出发烧平台再试。

**Q：扫码后提示"换取登录凭证失败"？**
A：多为华为账号未绑定光·遇，或华为侧风控要求二次验证。可先在手机上正常登录一次光·遇手游。

**Q：游戏起来了但卡在登录界面？**
A：说明票据没被接受。请把 `%APPDATA%\SkyLogin\log.txt` 发出来，里面记了每一步。

**Q：会不会封号？**
A：本工具只代替"平台发票据"这一步，不改游戏文件、不读写游戏内存。风险与第三方登录工具同级，请自行判断；建议先用小号验证。

## 技术说明

| 模块 | 作用 |
|---|---|
| `internal/huawei` | 华为 HMS 扫码登录：getqrInfo → 轮询 → loginByQrCode(ST) → silent_token → getGameAuthSign |
| `internal/netsec` | SAUTH 构造 + HMAC-SHA256 签名 + `uni_sauth` |
| `internal/mpay` | MPay 二维码：create_login → scan → confirm → query 取 `login_info.code` |
| `internal/fever` | 冒充发烧平台：`LHMW_FG_Main` 窗口 + XOR + op13/op14 |
| `internal/web` | 本地控制面板（随机端口，仅 127.0.0.1） |


## 目录结构

```
skylogin/
├── cmd/skylogin/          程序入口：只做装配（提权 → 打开界面）
│   └── rsrc.syso          图标与 DPI 清单（必须放在 main 包目录才会被内嵌）
├── internal/app/          业务流程：账号存储、扫码登录、取票、启动游戏
├── internal/win/          Windows 提权
├── internal/huawei/       华为 HMS 扫码登录（二维码 → ST → access_token → gameAuthSign）
├── internal/netsec/       SAUTH 构造 + HMAC-SHA256 签名 + uni_sauth
├── internal/mpay/         MPay 建码 / 扫码 / 确认 / 轮询取票据
├── internal/fever/        冒充发烧平台：LHMW_FG_Main 窗口 + XOR + op13/op14
├── internal/device/       设备指纹
├── internal/profile/      光遇与华为的固定参数
├── internal/web/          本地控制面板（HTML 内嵌进二进制）
└── assets/                图标与清单的源文件
```

## 从源码构建

```powershell
go build -trimpath -ldflags "-s -w -H=windowsgui" -o skylogin.exe ./cmd/skylogin
```

`-H=windowsgui` 让 exe 不弹出黑色控制台窗口，双击就是纯 GUI。

图标与 DPI 清单已经由 `rsrc` 生成好放在 `cmd/skylogin/rsrc.syso`，
无需额外工具即可构建出带图标的 exe。如需重新生成：

```powershell
go install github.com/akavel/rsrc@latest
rsrc -ico assets/app.ico -manifest assets/app.manifest -o cmd/skylogin/rsrc.syso -arch amd64
```

## 登录链路（已实测跑通）

```
华为扫码 ─► ST ─► silent_token ─► gameAuthSign ─► SAUTH + HMAC-SHA256 ─► uni_sauth
                                                                         │
                                                                  登录包（含 SAUTH_JSON）
                                                                         │
   ┌─────────────────────────────────────────────────────────────────────┘
   ▼
create_login（游戏自身参数）─► scan ─► confirm_login ─► query 轮询到 status=2
   ▼
login_info.code = 10 位一次性票据
   ▼
冒充发烧平台（LHMW_FG_Main，op13/op14）─► 游戏换取会话 ─► 进入大厅
```

全程**不装证书、不改 hosts、不开代理、不加载 mpay.dll**。

### 关键参数（实测确认，勿随意改动）

| 参数 | 值 | 说明 |
|---|---|---|
| `game_id` | `aecfs5n2y4aaaakd-g-ma75` | 必须用**目标游戏自己的长 id** |
| `jf_game_id` | `ma75` | |
| `app_channel` | `a50_sdk_cn` | 网易 PC SDK 通道 |
| `cv` | `c4.20.0` | 客户端版本 |
| `gv` / `sv` | `0.0.0.0` / `10.0.19045` | |
| `qrcode_channel_type` | `2` | |
| `is_remember` | `2` | |
| `device_id` | 客户端设备标识 | 由 MPay 分配，无法自行算出；工具内置一份，可在设置里替换 |

### 几个容易踩的坑

- `scan` 第一次可能返回 **`1424`**，响应里带正确的长 id（`game.id`），换掉重试即可
- `confirm_login` 用 `app_channel=netease.wyzymnqsd_cps_dev` 建码会成功但票据不可用；
  必须用 `a50_sdk_cn` + 游戏自身长 id
- `uni_sauth` 的 `timestamp` 必须在服务器时间 ±5 分钟内，所以**取票前要重新签名**
- 游戏收到 op14 后不再重复索票 = 票据被接受；若每秒重复索票，说明票据无效

## 授权

本项目以 **MIT** 许可证发布，详见 `LICENSE`。

协议实现来自对官方客户端（发烧平台与游戏）的流量观察与接口行为分析，
代码独立编写。













## 发布构建

仓库带了 GitHub Actions（`.github/workflows/build.yml`），但**不会因为 push 就构建**。
只有两种情况会跑：

| 方式 | 操作 | 结果 |
|---|---|---|
| 手动 | Actions → 构建 → **Run workflow** | 出一个构建产物（artifacts 里下载） |
| 发版本 | 推一个 `v1.2.0` 这样的标签 | 构建 + 自动创建 Release |

手动触发时可以填版本号（留空则用 `build-<构建序号>`），也可以勾「创建 Release」。
构建出来的 exe 版本号会写进日志第一行，方便确认用户用的是哪个版本。
