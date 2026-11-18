/**
 * 功能契约层：界面只看这一页。
 *
 * 目标是「功能与 UI 解耦」：四种界面壳（shells/）不直接碰 HTTP、不直接碰
 * 后端状态，只消费这个接口。两套实现：
 *   - demo/store.ts       内存 mock（设计演示页 demo.html 用）
 *   - realStore.ts        真实后端绑定（正式界面用）
 * 壳本身对用哪套实现毫无感知。
 */

export type Channel =
  | "huawei"
  | "netease"
  | "bilibili"
  | "m4399"
  | "vivo"
  | "uc_platform"
  | "myapp";
export type CredState = "ok" | "expired";

export interface Account {
  id: string;
  name: string;
  /** 打码后的显示名（maskAll 时用它替代 name） */
  maskedName: string;
  channel: Channel;
  /** 完整 ID（仅「显示」打开时可见） */
  fullId: string;
  /** 打码后的 ID */
  maskedId: string;
  level: string;
  cred: CredState;
  running: boolean;
  /** 本次运行秒数（demo 用计时器驱动；正式版由 store 适配层推算） */
  runSeconds: number;
  current: boolean;
  /** 副本标注的区（android / ios）；主记录为空 */
  region?: string;
  /** 区的中文名（安卓区 / iOS 区），界面直接显示；空 = 未选 */
  regionLabel?: string;
  /** 非空 = 选了渠道但当前凭证用不了，内容是原因（界面直接提示） */
  regionBlocked?: string;
  /** 住在某个平台账号的寄宿槽里：渠道由扫码设备固定，不再显示选区 */
  hosted?: boolean;
  /** 提供槽位的宿主账号 id（UserID == playerId）；空 = 没被寄宿 */
  hostedBy?: string;
  /** 存过账号密码：凭证过期能静默自动重登（「刷新凭证」不再弹输入框） */
  hasPassword?: boolean;
  /** 上次登录用的账号名（邮箱 / 手机号），刷新时预填 */
  loginUser?: string;
  /** 远程身份 id（同一账号的原始行与副本相同） */
  playerId: string;
  /** 这行账号自己的游戏画质档位（空 = 跟随游戏） */
  qualityMode?: string;
  /** 这行账号自己的帧率（0 = 跟随游戏） */
  qualityFps?: number;
  /** 单开时的显示方式："" 跟随游戏 / "windowed" 窗口 / "fullscreen" 全屏 */
  displayMode?: string;
  /** 这一行的自动进入开关：true/false 明确设置，null/undefined = 跟随全局 */
  autoEnter?: boolean | null;
  /** 副本键（"PlayerID-2" 样式）；空 = 原始行 */
  copyKey?: string;
  /** 用户自己打的标签（多个，自由输入）。副本与原件各存各的。 */
  tags: string[];
  /** 添加到本机的时间（unix 秒）——按时间分段/排序用 */
  createdAt?: number;
  /** 最后一次成功启动的时间（unix 秒）——"最近登录"排序用 */
  lastLoginTs?: number;

  // ── 账号详情（登录环境与时间，见 Go 侧 accountView）──
  /** 登录窗加载的 mpay.dll 路径（空 = 这个渠道不走 DLL） */
  mpayPath?: string;
  /** mpay.dll 版本号 */
  mpayVersion?: string;
  /** 登录用的平台身份：ma75 / a50 … */
  gameId?: string;
  /** 登录方式原文：qrcode / password / sms / channelscan */
  loginMethod?: string;
  /** 登录来源原文：qrcode / weblogin / mpaydll / channelscan … */
  source?: string;
  /** 添加到本机的时间（展示串） */
  created?: string;
  /** 最后一次成功启动的时间（展示串） */
  lastLogin?: string;
  /** 凭证等级的人话描述（账号级 / 会话级 / 大神会话） */
  tokenSaLabel?: string;
  /** 凭证签发时间（展示串） */
  tokenAt?: string;
  /** 上次验证凭证的时间（unix 秒） */
  lastCheck?: number;
  /** 上次失败原因 */
  lastError?: string;
}

/** 一条待处理的账号提示（一个账号一条） */
export interface AuthHintItem {
  kind: string;
  /** 短原因，清单里用它（message 是整句） */
  title?: string;
  message: string;
  account?: string;
  /** 相关账号的 ID：界面按它精确回跳到该账号（重名也不会串） */
  accountId?: string;
}

/** 后端推给界面的「需要用户处理」提示（目前是网易凭证过期） */
export interface AuthHint {
  kind?: string;
  /** 短标题（后端给了就用，界面上一般显示 message） */
  title?: string;
  message?: string;
  /** 相关账号（用户名 / 手机号），仅展示用 */
  account?: string;
  /** 相关账号的 ID：一键刷新按它精确回跳到该账号 */
  accountId?: string;
  /** 一共几条待处理；>1 时界面给清单 */
  count?: number;
  /** 全部待处理提示，按发生时间排序 */
  items?: AuthHintItem[];
}

export interface NewAccountDraft {
  channel: Channel;
  name?: string;
}

/** 兼容旧调用方的网格参数；新界面不再由网格推算窗口尺寸。 */
export interface GridLayout {
  key: string;
  label: string;
  cols: number;
  rows: number;
  max: number;
}

/** 统一窗口规划接口；正式界面只消费后端矩形，不另算尺寸。 */
export type LayoutMode =
  | "tile"
  | "cascade:cols"
  | "cascade:rows"
  | "fullscreen";
export interface LayoutRect {
  x: number;
  y: number;
  w: number;
  h: number;
  zOrder?: number;
}
export interface LayoutRequest {
  count: number;
  mode: string;
  cols?: number;
  rows?: number;
  w?: number;
  h?: number;
}
export interface LayoutPlan {
  ok: boolean;
  mode: LayoutMode;
  requestedMode: string;
  recommendedMode: LayoutMode;
  feasible: boolean;
  reason: string;
  message: string;
  screenW: number;
  screenH: number;
  workArea: LayoutRect;
  minW: number;
  minH: number;
  minSource: string;
  rects: LayoutRect[];
  options: {
    id: LayoutMode;
    label: string;
    description: string;
    enabled: boolean;
    reason?: string;
  }[];
}
export const canonicalLayout = (mode: string): LayoutMode =>
  mode === "fullscreen"
    ? "fullscreen"
    : mode === "cascade:rows" || mode === "cascade:fan"
      ? "cascade:rows"
      : mode.startsWith("cascade")
        ? "cascade:cols"
        : "tile";
export const layoutLabel = (mode: string): string =>
  ({
    tile: "窗口并排",
    "cascade:cols": "标题易切换",
    "cascade:rows": "画面多露出",
    fullscreen: "专注全屏",
  })[canonicalLayout(mode)];

/** 启动组里的一个成员：账号 + 它在这套里的启动参数 */
export interface LaunchSlot {
  accountKey: string;
  /** 账号显示名；账号被删掉时后端给"已删除的账号" */
  name: string;
  missing?: boolean;
  /** 覆盖值，空/0 = 跟随组 */
  mode: string;
  fps: number;
  display: string;
  /** 这次启动要不要自动点进游戏；null/缺省 = 跟随账号自己的设置 */
  autoEnter?: boolean | null;
  /** 这次不启动它，但保留位置与参数 */
  disabled: boolean;
  x: number;
  y: number;
  w: number;
  h: number;
  /** false = 这套没记过位置，启动时按排列方式算 */
  hasRect: boolean;
  /** 正数越大越靠前；旧数据缺省由后端按成员顺序补齐。 */
  zOrder?: number;
}

/**
 * 一套启动组：可重复执行的启动配方。
 * 参数分三层：组（排列/默认值/启动行为）→ 成员（顺序/位置/覆盖）→ 账号自己。
 */
export interface LaunchGroup {
  id: string;
  name: string;
  layout: LayoutMode | "cascade" | `cascade:${string}`;
  cols: number;
  rows: number;
  defMode: string;
  defFps: number;
  defDisplay: string;
  stopFirst: boolean;
  autoWall: boolean;
  gapSec: number;
  screenW: number;
  screenH: number;
  slots: LaunchSlot[];
  updated?: string;
}

/** 「记住当前各窗口位置」读到的一个窗口 */
export interface CapturedRect {
  /** 账号键（accountKey），用来对回槽位 */
  id: string;
  name: string;
  x: number;
  y: number;
  w: number;
  h: number;
  /** 全屏窗口记下来就是整屏，界面要提醒用户先切成窗口 */
  fullscreen: boolean;
  zOrder?: number;
}

/** 界面壳允许调用的全部功能。 */ export interface LauncherStore {
  readonly accounts: Account[];
  readonly current: Account | undefined;
  readonly runningAccounts: Account[];
  readonly maskAll: boolean;
  /** 多选：勾中的账号 id（与当前账号 current 相互独立） */
  readonly selectedIds: readonly string[];
  /** 后端提示（凭证过期等）；空对象 = 没有 */
  readonly authHint: AuthHint;

  select(id: string): void;
  /** 启动一个账号（缺省 = 当前账号）。游戏单实例时会被拒绝并 toast。 */
  launch(id?: string): void;
  stop(id: string): void;
  refreshCred(id: string): void;
  rename(id: string, name: string): void;
  remove(id: string): void;
  /** 创建一条账号的副本（name 为空时后端自动起名，region 为空表示副本先不选区）。 */
  duplicate(id: string, name: string, region?: string): void;
  /** 设置账号的所选渠道（仅网易官方账号；空值 = 先不选）。 */
  setAccountRegion(id: string, region: string): void;
  toggleMask(): void;
  /**
   * 添加账号。不带参数 = 打开添加入口（demo 弹「模拟扫码」，正式版弹真实登录流程）；
   * 带 draft = 直接确认创建（共享添加弹窗点「模拟扫码成功」时用）。
   */
  addAccount(draft?: NewAccountDraft): void;

  toggleSelect(id: string): void;
  selectAll(): void;
  invertSelect(): void;
  /** 只选中正在运行的账号 */
  selectRunning(): void;
  clearSelect(): void;
  /** 批量启动选中账号（多开语义，自动套推荐排列） */
  bulkLaunch(): void;
  /** 批量停止选中账号里正在运行的 */
  bulkStop(): void;
  /** 批量删除选中账号 */
  bulkRemove(): void;

  /**
   * 多开：绕过单实例限制，一次启动多个。
   * layout 为排列方案（缺省自动推荐）；size 为自定义窗口尺寸（缺省铺满格子）；
   * display："tile" 窗口平铺（默认）/ "cascade[:方案]" 堆叠 / "fullscreen" 全屏；
   * ovMode/ovFps 为本次多开的画质/帧率覆盖（空/0 = 按各账号自己的配置）。
   */
  multiLaunch(
    ids: string[],
    layout?: GridLayout,
    size?: { w: number; h: number },
    display?: "tile" | "cascade" | `cascade:${string}` | "fullscreen",
    ovMode?: string,
    ovFps?: number,
  ): void;
  /** 保存某行账号自己的画质/帧率（账号级就是具体档位，无"跟随"项）。 */
  /** 保存账号级游戏设置：画质 / 帧率 / 显示方式（空/0 = 跟随游戏） */
  setAccountQuality(
    id: string,
    mode: string,
    fps: number,
    display?: string,
    /** 自动进入：true/false 明确设置，null = 跟随全局 */
    autoEnter?: boolean | null,
  ): void;
  /**
   * 保存某个账号的标签（整组替换）。
   * 副本与原件各存各的——传的是哪一行，就只改那一行。
   */
  setTags(id: string, tags: string[]): Promise<boolean>;
  /** 标签分组顺序独立保存，不改变账号本身的标签。 */
  readonly tagOrder: string[];
  setTagOrder(tags: string[]): Promise<boolean>;
  /**
   * 按给定顺序重排账号（整表顺序）。
   * 没提到的行由后端按原相对顺序补在末尾，所以筛选后只传可见的也不会丢号。
   */
  reorder(ids: string[]): Promise<boolean>;
  /** 保存账号列表宽度（分界线拖拽结束）。 */
  setRailWidth(width: number): void;

  /** 获取可执行布局、推荐及不可用原因。 */
  planLayout(request: LayoutRequest): Promise<LayoutPlan>;

  /** 全部启动组（后端是唯一真相来源） */
  readonly groups: LaunchGroup[];
  /** 新建/更新一套启动组（带 id = 更新） */
  saveGroup(g: LaunchGroup): Promise<LaunchGroup>;
  /** 删除一套启动组 */
  deleteGroup(id: string): Promise<void>;
  /**
   * 按一套启动组拉起客户端（后端先停旧的再拉新的，然后按记录的位置摆）。
   * 返回的 message 可以直接 toast。
   */
  launchGroup(id: string): Promise<{ ok: boolean; message?: string }>;
  /**
   * 读当前运行中窗口的坐标（按启动顺序）。
   * 给「记住当前各窗口位置」用；没有客户端在跑时返回空数组。
   */
  captureRects(): Promise<{
    rects: CapturedRect[];
    screenW: number;
    screenH: number;
    workArea?: { x: number; y: number; w: number; h: number };
  }>;

  /** 多开默认（画质 / 帧率 / 鼠标仲裁）：多开弹窗和设置面板读写的是同一份 */
  multiDefaults(): {
    qualityMode: string;
    fps: number;
    arbiter: boolean;
    arbiterMin: number;
    /** 多开时是否自动点播放按钮进入游戏 */
    autoEnter: boolean;
    /** 每个窗口的点击计时（毫秒） */
    autoEnterSlotMs: number;
  };
  saveMultiDefaults(
    qualityMode: string,
    fps: number,
    arbiter?: boolean,
    arbiterMin?: number,
    autoEnter?: boolean,
    autoEnterSlotMs?: number,
  ): void;

  /** 打开设置面板（由实现方决定弹什么） */
  openSettings(): void;
  /** 开/关监控墙（运行中客户端的实时缩略图，点格子切到前台） */
  toggleWall(): void;
  /** 打开运行聚合页（规划中，demo 里只 toast） */
  openRunning(): void;

  toast(text: string): void;
  readonly toasts: { id: number; text: string }[];
}

export const channelLabel = (c: Channel | string): string => {
  switch (c) {
    case "huawei":
      return "华为渠道";
    case "netease":
      return "网易官方";
    case "m4399":
      return "4399渠道";
    case "vivo":
    case "nearme_vivo":
      return "vivo渠道";
    case "bilibili":
      return "B站渠道";
    case "myapp":
      return "应用宝渠道";
  }
  // 渠道服扫码拿到的其它渠道（小米/OPPO…）：渠道名以网易服务端返回的为准，
  // 认不出来就原样显示，别硬塞成 B站。
  return c ? `${c}渠道` : "渠道服";
};

/** 秒数 → 「42 分钟 / 1 小时 03 分」。 */
export function fmtRun(seconds: number): string {
  const m = Math.floor(seconds / 60);
  if (m < 60) return `${m} 分钟`;
  return `${Math.floor(m / 60)} 小时 ${String(m % 60).padStart(2, "0")} 分`;
}
