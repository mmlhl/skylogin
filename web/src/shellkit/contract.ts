/**
 * 功能契约层：界面只看这一页。
 *
 * 目标是「功能与 UI 解耦」：四种界面壳（shells/）不直接碰 HTTP、不直接碰
 * 后端状态，只消费这个接口。两套实现：
 *   - demo/store.ts       内存 mock（设计演示页 demo.html 用）
 *   - realStore.ts        真实后端绑定（正式界面用）
 * 壳本身对用哪套实现毫无感知。
 */

export type Channel = "huawei" | "netease" | "bilibili" | "m4399" | "vivo";
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
}

/** 后端推给界面的「需要用户处理」提示（目前是网易凭证过期） */
export interface AuthHint {
  kind?: string;
  message?: string;
  /** 相关账号（用户名 / 手机号），仅展示用 */
  account?: string;
  /** 相关账号的 ID：一键刷新按它精确回跳到该账号 */
  accountId?: string;
}

export interface NewAccountDraft {
  channel: Channel;
  name?: string;
}

/**
 * 多开时的窗口排列方案。
 *
 * 只用正方形网格：16:9 的屏幕被均分成 n×n 时，每一格恰好也是 16:9，
 * 游戏窗口铺进去不会变形。两个窗口 = 四分屏占两格（960×540），
 * 五到九个 = 九宫格占 N 格（640×360），以此类推。
 */
export interface GridLayout {
  key: string;
  label: string;
  cols: number;
  rows: number;
  /** 该方案最多容纳的窗口数 */
  max: number;
}

export const LAYOUTS: GridLayout[] = [
  { key: "fullscreen", label: "全屏", cols: 1, rows: 1, max: 1 },
  { key: "quad-2", label: "四分屏 · 占两格", cols: 2, rows: 2, max: 2 },
  { key: "quad-3", label: "四分屏 · 占三格", cols: 2, rows: 2, max: 3 },
  { key: "quad", label: "四分屏", cols: 2, rows: 2, max: 4 },
  { key: "grid-3", label: "九宫格", cols: 3, rows: 3, max: 9 },
  { key: "grid-4", label: "十六宫格", cols: 4, rows: 4, max: 16 },
];

export const LAYOUT_NONE: GridLayout = { key: "none", label: "不排列", cols: 0, rows: 0, max: Number.MAX_SAFE_INTEGER };

/** 按窗口数推荐最小能装下的网格。 */
export function recommendLayout(count: number): GridLayout {
  return LAYOUTS.find((l) => l.max >= count) ?? LAYOUT_NONE;
}

/** 界面壳允许调用的全部功能。 */
export interface LauncherStore {
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
   * display："tile" 窗口平铺（默认）/ "cascade" 窗口堆叠 / "fullscreen" 全屏。
   */
  multiLaunch(ids: string[], layout?: GridLayout, size?: { w: number; h: number }, display?: "tile" | "cascade" | "fullscreen"): void;

  /** 多开默认（画质 / 帧率 / 鼠标仲裁）：多开弹窗和设置面板读写的是同一份 */
  multiDefaults(): { qualityMode: string; fps: number; arbiter: boolean; arbiterMin: number };
  saveMultiDefaults(qualityMode: string, fps: number, arbiter?: boolean, arbiterMin?: number): void;

  /** 打开设置面板（由实现方决定弹什么） */
  openSettings(): void;
  /** 开/关监控墙（运行中客户端的实时缩略图，点格子切到前台） */
  toggleWall(): void;
  /** 打开运行聚合页（规划中，demo 里只 toast） */
  openRunning(): void;

  toast(text: string): void;
  readonly toasts: { id: number; text: string }[];
}

export const channelLabel = (c: Channel): string =>
  c === "huawei" ? "华为渠道" : c === "netease" ? "网易官方" : c === "m4399" ? "4399渠道" : c === "vivo" ? "vivo渠道" : "B站渠道";

/** 秒数 → 「42 分钟 / 1 小时 03 分」。 */
export function fmtRun(seconds: number): string {
  const m = Math.floor(seconds / 60);
  if (m < 60) return `${m} 分钟`;
  return `${Math.floor(m / 60)} 小时 ${String(m % 60).padStart(2, "0")} 分`;
}
