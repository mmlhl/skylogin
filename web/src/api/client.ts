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

  /** 启动游戏：Go 会关掉发烧平台、取票、冒充平台窗口 */
  launch: () => request("/api/launch", { method: "POST" }),

	/** 多开：每个账号起一个客户端；带 cols/rows 时窗口就绪后渐进平铺；w/h 自定义窗口尺寸；
	 *  display "cascade" = 窗口堆叠（文件窗式错开），"fullscreen" = 全屏（不排列，切换靠监控墙） */
	launchMulti: (
		ids: string[],
		layout?: { cols: number; rows: number },
		size?: { w: number; h: number },
		display?: "tile" | "cascade" | `cascade:${string}` | "fullscreen"
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
			}),
		}),

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

  /** 华为扫码：开始（Go 去华为申请二维码） */
  startHuaweiLogin: () => request("/api/login", { method: "POST" }),

  /** 华为扫码：取消 */
  cancelHuaweiLogin: () => request("/api/login/cancel", { method: "POST" }),

  /** 网易扫码：开始（Go 去 MPay 申请二维码） */
  startScan: () => request("/api/scan/start", { method: "POST" }),

  /** 网易扫码：停止 */
  stopScan: () => request("/api/scan/stop", { method: "POST" }),

  /**
   * 渠道网页登录（B站 / 4399 / vivo 共用一套流程，key 区分渠道）。
   * 打开官方登录页子进程，捕获凭证后在线验证并存账号。
   */
  startChannelLogin: (key: "bili" | "m4399" | "vivo", targetUserID = "") =>
    request(`/api/${key}/login/start`, { method: "POST", body: JSON.stringify({ targetUserId: targetUserID }) }),

  /** 渠道网页登录进度（waiting / done / failed） */
  channelLoginStatus: (key: "bili" | "m4399" | "vivo") => request(`/api/${key}/login/status`),

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

  /** 账号密码登录进度 */
  mpayLoginStatus: () => request("/api/mpay-login/status"),

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







