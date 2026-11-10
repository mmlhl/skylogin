/**
 * 后端数据结构定义。
 *
 * 对应 Go 侧 `internal/web/server.go` 里 `/api/state` 的返回。
 * 改后端字段时，这里必须同步改——这是前后端唯一的"契约"。
 */

/** 渠道：华为渠道 / 网易官方 / B站渠道 */
export type Channel = "huawei" | "netease" | "bilibili" | "m4399" | "vivo" | "uc_platform" | "myapp";

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
  /** 这行账号自己的画质档位（空 = 跟随游戏） */
  qualityMode?: string;
  /** 这行账号自己的帧率（0 = 跟随游戏） */
  qualityFps?: number;
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
  /** 登录方式原文：qrcode / password / sms / channelscan */
  loginMethod?: string;
  /** 上次验证凭证的时间（unix 秒） */
  lastCheck?: number;

  // ── 详情页用的登录环境 / 时间信息（对应 Go 侧 Account 的同名字段）──
  /** 登录窗实际加载的 mpay.dll 完整路径（只有官方登录窗的账号才有） */
  mpayPath?: string;
  /** 那份 mpay.dll 的版本号 */
  mpayVersion?: string;
  /** 登录用的平台身份（ma75 / a50 …） */
  gameId?: string;
  /** 第一次添加到本机的时间（unix 秒） */
  createdAt?: number;
  /** 上面时间的展示串（"2026-11-05 21:30"） */
  created?: string;
  /** 最后一次成功启动的时间 */
  lastLogin?: number;
  lastLoginText?: string;
  /** 当前凭证等级：-9 账号级 / 3 会话级 / -4 大神 */
  tokenSa?: number;
  tokenSaLabel?: string;
  /** 当前凭证的签发时间 */
  tokenAt?: number;
  tokenAtText?: string;

  /** 本机行标识：只有「同账号的第二个区」这类副本才有，主记录为空 */
  /** 副本键（"PlayerID-2" 样式）；空 = 原始行 */
  copyKey?: string;
  /** 这一行走的区：android / ios / 空 = 还没标注 */
  region?: string;
  /** 区的中文名，直接显示（安卓区 / iOS 区 / 空） */
  regionLabel?: string;
  /** 非空 = 选了渠道但当前凭证用不了（原因是给用户看的） */
  regionBlocked?: string;
  /** 这个号住在某个平台账号的寄宿槽里：渠道由扫码设备固定，不再显示选区 */
  hosted?: boolean;
  /** 提供槽位的宿主账号 id（UserID == PlayerID）；空 = 没被寄宿 */
  hostedBy?: string;
  /** 存过账号密码：凭证过期能静默自动重登 */
  hasPassword?: boolean;
  /** 上次登录用的账号名（邮箱 / 手机号），刷新时预填 */
  loginUser?: string;

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
  /** 这次登录的是列表里已有的账号（只是更新了凭证，没有新增） */
  existing?: boolean;
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
  /** 窗口标题 / 监控墙标签 / 列表的隐私打码开关（同一状态） */
  maskTitles: boolean;
  /** CI 注入的构建号（本地开发版 0）与构建时间 */
  appBuild: string;
  appBuildTime: string;
  /** 下载渠道（独立模块 release 注入） */
  release: { github: string; pan: string };
  /** 官方登录窗的调试参数（设置页「开发者调试」） */
  mpayDebug: {
    /** game = 游戏目录的 4.20；platform = 平台目录的 4.21 */
    dll: string;
    /** a50 = 平台上下文（二维码只认网易大神）；ma75 = 游戏上下文（光遇手游可扫） */
    game: string;
    /** 打开窗口前不清旧登录态 */
    skipLogout: boolean;
    /** 自定义数据目录（空 = 用默认） */
    dataPath: string;
  };
  /** 公告已读序号 / 被忽略的更新构建号（存后端，localStorage 端口随机记不住） */
  releaseAnnSeen: number;
  updateDismissed: number;
  updatePending: number;
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

/** 一种登录方式，由后端声明，前端只负责渲染 */
export interface LoginMethodView {
  /** web | qr | sms | password | channelscan | package */
  kind: string;
  label: string;
  hint: string;
  /** 方式的唯一标识（回传给后端）；同一 kind 有多个方式时靠它区分 */
  method?: string;
}

/**
 * 「添加账号」列表里的一项。
 *
 * 前端不认识任何具体渠道：后端返回几项就渲染几行，
 * group 为 true 的点了进二级菜单（目前只有「网易账号」）。
 */
export interface LoginEntryView {
  id: string;
  /** 调用接口时用的标识：bilibili / m4399 / vivo / huawei / netease */
  action: string;
  label: string;
  hint: string;
  initial: string;
  group: boolean;
  /** 只在刷新凭证时出现，不进「添加账号」列表 */
  onlyRefresh?: boolean;
  methods: LoginMethodView[];
}

/** `/api/state` 的完整返回 */
export interface AppState {
  accounts: AccountView[];
  game: string;
  login: LoginState;
  scan: ScanState;
  settings: Settings;
  authHint: AuthHint;
  /** 渠道服扫码的说明（渠道清单在 Go 侧维护） */
  channelScan?: { help?: string; channels?: unknown[] };
  /** 添加账号的入口清单（Go 是唯一真相来源） */
  loginChannels?: { entries: LoginEntryView[] };
  /** 启动事件弹窗（如客户端闪退）；id 递增，前端按 id 去重展示 */
  launchAlert: { id: number; title: string; message: string } | null;
  version: string;
}
/** 接口统一的返回包装 */
export interface ApiResult {
  ok?: boolean;
  message?: string;
  [key: string]: unknown;
}

