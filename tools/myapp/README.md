# 应用宝（myapp）渠道探测工具

验证「应用宝 QQ 扫码」整条链路：申请二维码 → 扫码 → 换 atk → 校验。
不开窗、不需要浏览器、不需要代理，纯 HTTP。

## 用法

```bash
cd tools/myapp
go run .            # 默认监听 127.0.0.1:8899
go run . -addr 127.0.0.1:9000   # 换端口
```

然后浏览器打开提示的地址，用手机 QQ 扫码并确认。页面上会实时打印每一步：

```
① 提取到 xlogin 地址（含 h5sig）
② xlogin 完成（cookie 5 个：pt_login_sig,pt_clientip,pt_serverip,pt_local_token,uikey）
③ 二维码已就绪
   轮询参数含 pt_openlogin_data（326 字节）
扫码确认成功，原始响应: ptuiCB('0','0','http://auth://tauth.qq.com/?code=…')
✓ 回调里直接带 code: 21A7FEBDBF1D……
✓ code 换 token 成功！atk=0C9CC3A8851C… openid=… 60.0 天
✓✓ qq_verify_login 通过 —— 全链路纯 HTTP 成立
```

看到最后一行就说明链路通了。

## 它在验证什么

| 步骤 | 端点 | 关键点 |
|---|---|---|
| ① 拿登录页 | `openmobile.qq.com/oauth2.0/m_authorize` | 不回 302，返回 JS 里的 `var src`（xlogin 地址） |
| ② 建会话 | `xui.ptlogin2.qq.com/cgi-bin/xlogin` | 种下 `pt_login_sig` 等 cookie |
| ③ 要二维码 | `xui.ptlogin2.qq.com/ssl/ptqrshow` | 下发 `qrsig` |
| ④ 轮询 | `xui.ptlogin2.qq.com/ssl/ptqrlogin` | **必须带 `pt_openlogin_data`**，否则回调降级成 check_sig（死域名） |
| ⑤ 换 token | `ysdk.qq.com/cmd/QQCodeLogin` | HMAC-SHA256 签名，`expiresIn` 是字符串 |
| ⑥ 校验 | `ysdk.qq.com/auth/qq_verify_login` | `ptk` 不能漏 |

## 常见现象

| 现象 | 原因 |
|---|---|
| 回调地址是 `ptlogin4.openmobile.qq.com/check_sig…` | 轮询少了 `pt_openlogin_data` |
| 轮询一直没反应 | 手动设了 `Cookie` 头，把 `pt_login_sig` 覆盖掉了 |
| `code=-5 subcode=-501` | 把 `ptsigx` 当成 code 喂了 `QQCodeLogin`（它要的是 32 位 code） |
| `11000 missing essential parameters` | `qq_verify_login` 少了 `ptk` |

协议细节见 [`docs/channels/应用宝.md`](../../docs/channels/应用宝.md)。

## 注意

- 工具只在本机跑，凭证只打印在页面上，不落盘、不上传。
- 扫码用的是你自己的 QQ 号，`openid` 是账号标识，别往外贴。
- `code` 一次性、几分钟内有效；工具拿到就立刻换 token。
