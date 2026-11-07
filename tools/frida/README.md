# frida 观测脚本

给九游（UC SDK）渠道做协议分析时用的 hook 脚本。**只观测，不修改行为**——
不改变任何返回值，不注入业务逻辑，卸载后设备恢复原样。

## 两个脚本的分工

| 脚本 | 输出 | 什么时候用 |
|---|---|---|
| `uc-token-dump.js` | `sid` / `refreshToken` / `serviceTicket` / `accountList` | 常驻。只需要看登录令牌时用这个，日志量很小 |
| `uc-crypt-dump.js` | 每次加解密的算法、明文、密文、key/iv | 对齐加密格式时用。输出量大，分析完就关掉 |

## 用法

1. 在设备端的 hook 工具里导入 JS，绑定到目标进程（光遇九游包是 `com.netease.sky.aligames`）
2. **完全重启目标进程**——hook 只在进程启动时注入
3. 触发一次登录，然后读工具里的 hook 日志

脚本里的 `setTimeout` 延迟是为「等 SDK 加载完」准备的；UC SDK 的类是运行时
从 `app_ntp0/<版本>/.unzip/classes*.dex` 解压加载的，启动早期还不存在。

## 两个坑

**单条日志有长度上限**（约 520 字符）。所以 `uc-crypt-dump` 把长内容按 240 字符
分片输出，格式是 `<方向><序号>|<偏移>|<内容>`，重组时按偏移排序拼接即可。
不分片的话，响应尾部（往往正是 `refreshToken` 所在的位置）会被截掉。

**先清空日志再触发**。这两个脚本都会在启动阶段捕获大量流量，不清日志的话
翻页翻不到最新记录。

## 已经用它确认过的事实

- UC 的加密在 Java 层（`javax.crypto.Cipher`），不涉及 native
- 对称算法是 AES/CBC/PKCS7，每次随机 16 字节 key/iv
- 非对称用的是 `getSecurityKey` 下发的 RSA 公钥（512 位，格式 `5|` + base64 DER）
- 免密登录依赖设备上九游 App 的登录态（`connectCode`），PC 端无法复刻
- 短信登录与续期走 UC 自有协议，与 mtop 无关

协议细节见 `internal/ucsdk` 各文件的注释（尤其是 `crypto.go` 与 `api.go`）。
