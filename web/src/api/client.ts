/**
 * 前端与 Go 交互的唯一入口。
 *
 * 工作方式：
 *   1. Go 用 net/http 在 127.0.0.1 的随机端口起一个本地服务（internal/web/server.go）
 *   2. 界面（WebView2 里的这个页面）跟它是同源，所以直接 fetch 相对路径即可
 *   3. 所有接口都返回 JSON，失败时带 { ok: false, message }
 *
 * 想加新功能时，改这三处：
 *   - Go：internal/web/server.go 注册路由 + internal/app 里实现
 *   - 这里：加一个请求函数
 *   - types.ts：补上返回结构
 */
import type { ApiResult, AppState } from "./types";

/** 通用请求：自动带 JSON 头、自动解析、失败抛错 */
async function request<T = ApiResult>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    cache: "no-store",
    ...init,
    headers: { "Content-Type": "application/json", ...(init?.headers || {}) },
  });
  const text = await res.text();
  let data: unknown = {};
  try {
    data = text ? JSON.parse(text) : {};
  } catch {
    throw new Error(`接口返回不是 JSON：${path}`);
  }
  return data as T;
}

/** 把参数拼成 query string（只保留非 undefined） */
function qs(params: Record<string, string | number | boolean | undefined>): string {
  const u = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) {
    if (v !== undefined && v !== "") u.set(k, String(v));
  }
  const s = u.toString();
  return s ? `?${s}` : "";
}

export const api = {
  /** 拉取整个界面状态（左栏账号、扫码进度、设置、提示） */
  state: () => request<AppState>("/api/state"),

  /** 主窗口按钮：min / max（最大化与还原切换）/ close。
   *  窗口是无边框的，系统不再画标题栏，这三个动作由前端标题条触发。 */
  windowControl: (action: "min" | "max" | "close") =>
    request("/api/window", { method: "POST", body: JSON.stringify({ action }) }),

  /** 让窗口进入系统拖动循环（标题条 mousedown 时调）。
   *  WebView2 的客户区被 Chromium 吃掉鼠标事件，页面里的拖拽不会变成窗口拖动。 */
  windowDrag: () =>
    request("/api/window", { method: "POST", body: JSON.stringify({ action: "drag" }) }),

  /** 挂起/恢复 WebView 渲染进程（窗口不可见时省内存）。
   *  挂起后页面状态保留，恢复不用重新加载。 */
  windowSuspend: (on: boolean) =>
    request("/api/window", { method: "POST", body: JSON.stringify({ action: on ? "suspend" : "resume" }) }),

  /** 启动游戏：Go 会关掉发烧平台、取票、冒充平台窗口 */
  launch: () => request("/api/launch", { method: "POST" }),

	/** 多开：每个账号起一个客户端；带 cols/rows 时窗口就绪后渐进平铺；w/h 自定义窗口尺寸；
	 *  display "cascade" = 窗口堆叠（文件窗式错开），"fullscreen" = 全屏（不排列，切换靠监控墙） */
	launchMulti: (
		ids: string[],
		layout?: { cols: number; rows: number },
		size?: { w: number; h: number },
		display?: "tile" | "cascade" | `cascade:${string}` | "fullscreen",
		qualityMode?: string,
		qualityFps?: number
	) =>
		request("/api/launch-multi", {
			method: "POST",
			body: JSON.stringify({
				ids,
				cols: layout?.cols,
				rows: layout?.rows,
				w: size?.w,
				h: size?.h,
				display: display && display !== "tile" ? display : undefined,
				qualityMode,
				qualityFps,
			}),
		}),

	/** 检查更新（构建号由后端注入，本地开发版为 0） */
	releaseCheck: () => request("/api/release/check", { method: "POST" }),

	/** 增量拉取公告（since = 已见过的最大公告序号） */
	releaseAnnouncements: (since: number) =>
		request(`/api/release/announcements${qs({ since })}`, { method: "POST" }),

	/** 记录公告已读序号 / 被忽略的更新构建号（存后端配置） */
	releaseState: (annSeen: number, dismissedBuild: number, pendingBuild: number) =>
		request("/api/release/state", { method: "POST", body: JSON.stringify({ annSeen, dismissedBuild, pendingBuild }) }),

	/** 用系统浏览器打开下载渠道（白名单在 release 包里） */
	openExternal: (url: string) => request("/api/openurl", { method: "POST", body: JSON.stringify({ url }) }),

	/** 切换隐私打码（窗口标题 / 监控墙标签 / 列表显示同一开关） */
	setMaskTitles: (on: boolean) =>
		request("/api/ui/mask", { method: "POST", body: JSON.stringify({ on }) }),

	/** 把正在运行的客户端窗口按 cols×rows 网格铺到主屏；w/h 自定义窗口尺寸 */
	arrange: (cols: number, rows: number, size?: { w: number; h: number }) =>
		request("/api/arrange", {
			method: "POST",
			body: JSON.stringify({ cols, rows, w: size?.w, h: size?.h }),
		}),

	/** 开/关监控墙（正在运行客户端的实时缩略图，点格子切到前台） */
	wallToggle: () => request<ApiResult & { message?: string }>("/api/wall", { method: "POST" }),

	/** 记住界面壳偏好（WebView2 端口随机，localStorage 记不住，存后端配置） */
	saveShell: (shell: number) =>
		request("/api/ui/shell", { method: "POST", body: JSON.stringify({ shell }) }),

	/** 读取界面壳偏好（没有记录时返回 -1） */
	loadShell: () => request<ApiResult & { shell?: number }>("/api/ui/shell"),

  /** 切换当前账号 */
  selectAccount: (id: string) => request(`/api/accounts/select${qs({ id })}`, { method: "POST" }),

  /** 改账号别名 */
  renameAccount: (id: string, name: string) =>
    request(`/api/accounts/rename${qs({ id, name })}`, { method: "POST" }),

  /** 停止这个账号正在运行的光遇 */
  stopAccount: (id: string) => request(`/api/accounts/stop${qs({ id })}`, { method: "POST" }),

  /** 删除账号 */
  deleteAccount: (id: string) => request(`/api/accounts/delete${qs({ id })}`, { method: "POST" }),

  /** 保存某行账号的画质/帧率（账号级 = 具体档位） */
  setAccountQuality: (id: string, mode: string, fps: number) =>
    request("/api/accounts/quality", { method: "POST", body: JSON.stringify({ id, mode, fps }) }),

  /** 创建一条账号的副本（name 为空时后端自动起名，region 为空表示副本先不选区） */
  duplicateAccount: (id: string, name: string, region = "") =>
    request("/api/accounts/duplicate", { method: "POST", body: JSON.stringify({ id, name, region }) }),

  /** 设置账号的所选渠道（仅网易官方账号；空值 = 先不选） */
  setAccountRegion: (id: string, region: string) =>
    request(`/api/accounts/region${qs({ id, region })}`, { method: "POST" }),

  /**
   * 无头账号密码登录：不弹网易登录窗，直接在这里收账号密码，
   * 由 mpay.dll 的无界面接口完成登录。
   *
   * 好处：① 凭证天然是游戏身份，选渠道能立刻生效；
   *       ② 密码能用 Windows DPAPI 加密存下来，凭证过期后自动重登。
   */
  headlessLogin: (user: string, password: string, region = "", name = "") =>
    request("/api/accounts/headless-login", {
      method: "POST",
      body: JSON.stringify({ user, password, region, name }),
    }),

  /**
   * 刷新凭证：用存下来的账号密码静默重登。
   * 没存密码（扫码 / 官方登录窗进来的）会返回 ok:false，调用方改成弹输入框。
   */
  reloginAccount: (id: string) =>
    request<{ ok?: boolean; message?: string; result?: { name?: string; canHost?: boolean } }>(
      "/api/accounts/relogin",
      { method: "POST", body: JSON.stringify({ id }) }
    ),

  /**
   * 寄宿池状态：哪些账号能当宿主、槽里现在记着谁、还有没有空槽。
   *
   * 一个平台账号只能带一个扫码号（服务端一对一），槽满了要用新号就会顶掉旧号。
   */
  hostPool: () => request("/api/hosts"),

  /** 华为扫码：开始（Go 去华为申请二维码） */
  startHuaweiLogin: () => request("/api/login", { method: "POST" }),

  /** 华为扫码：取消 */
  cancelHuaweiLogin: () => request("/api/login/cancel", { method: "POST" }),

  /** 网易扫码：开始（Go 去 MPay 申请二维码） */
  /** 网易扫码：开始（Go 去 MPay 申请二维码）。host 非空表示指定寄宿到哪个平台账号的槽 */
  startScan: (host = "") =>
    request(`/api/scan/start${host ? `?host=${encodeURIComponent(host)}` : ""}`, { method: "POST" }),

  /** 网易扫码：停止 */
  stopScan: () => request("/api/scan/stop", { method: "POST" }),

  /**
   * 渠道网页登录（B站 / 4399 / vivo 共用一套流程，key 区分渠道）。
   * 打开官方登录页子进程，捕获凭证后在线验证并存账号。
   *
   * key 不再由前端枚举：它是后端登录入口清单里的 action 字段
   * （后端少一个渠道，前端就不会传这个 key）。
   */
  startChannelLogin: (key: string, targetUserID = "") =>
    request(`/api/${key}/login/start`, { method: "POST", body: JSON.stringify({ targetUserId: targetUserID }) }),

  /** 渠道网页登录进度（waiting / done / failed） */
  channelLoginStatus: (key: string) => request(`/api/${key}/login/status`),

  /**
   * 渠道「协议登录」（九游密码 / 验证码）：不开窗，Go 侧直接算凭证。
   * method 传 password 或 sms（空即账号密码）。
   * 立即返回，进度走 channelLoginStatus；需要用户输入时 status 里带 ask。
   */
  startProtocolLogin: (key: string, method = "password") =>
    request("/api/channel/login/start", { method: "POST", body: JSON.stringify({ channel: key, method }) }),

  /** 提交上一步 ask 的答案（表单各字段或选项的 value） */
  answerChannelAsk: (key: string, values: Record<string, string>) =>
    request("/api/channel/login/answer", { method: "POST", body: JSON.stringify({ channel: key, values }) }),

  /**
   * 取消正在进行的协议登录。
   *
   * 扫码类流程（应用宝）会一直阻塞等用户扫，关掉对话框必须立刻取消，
   * 否则后端 tracker 会占着 5 分钟，用户再点就是"没反应"。
   */
  cancelChannelLogin: (key: string) =>
    request("/api/channel/login/cancel", { method: "POST", body: JSON.stringify({ channel: key }) }),

  /** 保存设置（只传要改的字段） */
  saveSettings: (payload: {
    skyExe?: string;
    deviceId?: string;
    mirrorDir?: string;
    closeLauncher?: boolean;
    multiQualityMode?: string;
    multiQualityFps?: number;
    mouseArbiter?: boolean;
    mouseArbiterMin?: number;
  }) => request("/api/settings", { method: "POST", body: JSON.stringify(payload) }),

  /** 选择游戏 exe（会弹系统文件框） */
  pickSkyPath: () => request<ApiResult & { path?: string }>(`/api/settings/pick-path${qs({})}`, { method: "POST" }),

  /** 选择多开镜像目录（会弹系统文件夹框） */
  pickFolder: () => request<ApiResult & { path?: string }>("/api/settings/pick-folder", { method: "POST" }),

  /**
   * 重新注册设备号。
   * mode：fingerprint 固定算法 / random 随机（不传则沿用当前模式）
   */
  registerDevice: (mode?: "fingerprint" | "random") =>
    request("/api/device/register", {
      method: "POST",
      body: JSON.stringify(mode ? { mode } : {}),
    }),

  /** 手动指定设备号 */
  setCustomDevice: (id: string) =>
    request(`/api/device/custom${qs({ id })}`, { method: "POST" }),

  /** 清理工具产生的数据 */
  cleanup: () => request("/api/cleanup", { method: "POST" }),

  /** 记住左栏宽度（拖完调一次） */
  setRailWidth: (width: number) =>
    request("/api/ui/rail-width", { method: "POST", body: JSON.stringify({ width }) }),

  /** 打开日志文件夹 */
  openLogs: () => request("/api/open-logs", { method: "POST" }),

  /**
   * 网页登录：打开网易官方登录页，用户在页面里登录，后端存下 token。
   * targetUserId 非空表示给已有账号刷新凭证（后端会校验账号 id 是否一致）。
   */
  webLoginStart: (targetUserId = "") =>
    request("/api/web-login/start", { method: "POST", body: JSON.stringify({ targetUserId }) }),

  /** 网页登录进度（等待/成功/失败） */
  webLoginStatus: () => request("/api/web-login/status"),

  /**
   * 账号密码登录：起子进程加载官方 mpay.dll，弹网易自己的登录窗。
   * targetUserId 非空表示给已有账号刷新凭证（后端会校验账号 id 是否一致）。
   */
  mpayLoginStart: (targetUserId = "") =>
    request("/api/mpay-login/start", { method: "POST", body: JSON.stringify({ targetUserId }) }),

  /** 保存官方登录窗的调试参数（设置页「开发者调试」） */
  saveMpayDebug: (dll: string, game: string, skipLogout: boolean, dataPath: string) =>
    request("/api/mpay/debug", {
      method: "POST",
      body: JSON.stringify({ dll, game, skipLogout, dataPath }),
    }),

  /** 渠道服游戏内扫码：强制用游戏身份开窗，渠道服 App 也能扫 */
  mpayChannelStart: () => request("/api/mpay-login/channel-start", { method: "POST", body: "{}" }),

  /** 账号密码登录进度 */
  mpayLoginStatus: () =>
    request<{ phase?: string; message?: string; account?: string; userId?: string }>("/api/mpay-login/status"),

  /** 取消账号密码登录（结束子进程） */
  mpayLoginCancel: () => request("/api/mpay-login/cancel", { method: "POST" }),

  /** 手机号登录：发送验证码 */
  smsSend: (mobile: string) =>
    request("/api/sms/send", { method: "POST", body: JSON.stringify({ mobile }) }),

  /**
   * 手机号登录：校验验证码并保存账号。
   * targetUserId 非空表示刷新已有账号，后端会校验登录的是不是同一个账号。
   */
  smsLogin: (mobile: string, code: string, targetUserId = "") =>
    request("/api/sms/login", { method: "POST", body: JSON.stringify({ mobile, code, targetUserId }) }),
};










