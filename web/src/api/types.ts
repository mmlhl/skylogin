/**
 * 后端数据结构定义。
 *
 * 对应 Go 侧 `internal/web/server.go` 里 `/api/state` 的返回。
 * 改后端字段时，这里必须同步改——这是前后端唯一的"契约"。
 */

/** 渠道：华为渠道 / 网易官方 / B站渠道 */
export type Channel = "huawei" | "netease" | "bilibili" | "m4399" | "vivo";

/** 左栏里的一个账号（由 Go 侧的 accountView 组装） */
export interface AccountView {
  /** 账号唯一标识（华为是玩家 id，网易是 MPay 账号 id） */
  id: string;
  /** 显示名：优先别名，其次昵称/手机号 */
  name: string;
  /** 昵称（去别名后用来兜底） */
  nick: string;
  channel: Channel;
  /** 渠道的中文名，直接显示 */
  channelLabel: string;
  /** 账号 ID 的标题，目前统一是「账号 ID」 */
  idLabel: string;
  /** 账号 ID 原文（前端负责打码） */
  accountId: string;
  playerId: string;
  masked: string;
  level: string;
  updated: string;
  /** 这个账号的游戏进程是否在运行 */
  running?: boolean;
  /** 凭证状态：ok / stale / expired */
  credState?: string;
  /** 最近一次失败原因 */
  lastError?: string;
  /** 登录来源：scan / sms / mpaydll / channel */
  source?: string;

  /** 是否是当前选中的账号 */
  current: boolean;
}

/** 华为扫码登录的进度 */
export interface LoginState {
  status: "idle" | "waiting" | "scanned" | "success" | "error";
  message: string;
  /** 二维码图片（data URL） */
  qr: string;
  nick: string;
  player: string;
  /** 二维码过期时间戳（毫秒），用于倒计时 */
  expireAt: number;
  attempt: number;
  /** 底部步骤条：1 等待扫码 / 2 确认登录 / 3 完成（Go 侧是 []int） */
  steps: number[];
}

/** 手机扫码添加网易账号的进度 */
export interface ScanState {
  status: "idle" | "waiting" | "scanned" | "ready" | "done" | "error";
  message: string;
  qr: string;
  channel: string;
  uuid: string;
  expireAt: number;
}

/** 设置面板需要的全部信息 */
export interface Settings {
  skyExe: string;
  skyFound: boolean;
  skyAuto: boolean;
  deviceId: string;
  deviceIdSet: boolean;
  deviceIdSource: string;
  deviceInfo: {
    fingerprint: string;
    uniqueId: string;
    hasKey: boolean;
    mode: string;
  };
  closeLauncher: boolean;
  /** 多开默认画质（游戏内部档位名：LowPower / Presentation；空 = 跟随游戏） */
  multiQualityMode: string;
  /** 多开默认帧率（0 = 跟随游戏） */
  multiQualityFps: number;
  /** 鼠标仲裁开关（多开后防后台客户端抢鼠标） */
  mouseArbiter: boolean;
  /** 仲裁持续时长（分钟） */
  mouseArbiterMin: number;
  /** 左栏宽度（像素，0 表示用默认值） */
  railWidth: number;
  /** 多开镜像目录（第 2 个客户端从这里启动） */
  mirrorDir: string;
  /** 默认镜像目录（游戏所在盘） */
  mirrorDefault: string;
  /** 镜像目录和游戏是否同一磁盘（同盘才是硬链接、不占空间） */
  mirrorSameVolume: boolean;
  /** 游戏目录的逻辑大小，用于提示 */
  mirrorSizeText: string;
  dataDir: string;
  version: string;
  /** exe 构建时间（文件修改时间） */
  buildTime: string;
  repo: string;
  goVersion: string;
  protocol: string;
}

/** 需要用户处理的问题（比如网易凭证过期），空对象表示没有 */
export interface AuthHint {
  kind?: string;
  title?: string;
  message?: string;
  account?: string;
}

/** `/api/state` 的完整返回 */
export interface AppState {
  accounts: AccountView[];
  game: string;
  login: LoginState;
  scan: ScanState;
  settings: Settings;
  authHint: AuthHint;
  version: string;
}
/** 接口统一的返回包装 */
export interface ApiResult {
  ok?: boolean;
  message?: string;
  [key: string]: unknown;
}
