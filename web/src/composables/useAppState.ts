/**
 * 全局状态：每秒拉一次 `/api/state`，界面完全由它驱动。
 *
 * 设计取舍：不做复杂的本地状态同步（不需要 Vuex/Pinia），
 * 因为数据量小、来源单一——Go 是唯一的真相来源，前端只负责展示。
 * 这样别的开发者接手时，只要看懂 `state` 一个对象就够了。
 */
import { reactive, computed } from "vue";
import { api } from "../api/client";
import type { AccountView, AppState } from "../api/types";

/** 默认状态：第一次请求回来前先占位，避免界面闪烁 */
function emptyState(): AppState {
  return {
    accounts: [],
    game: "未启动",
    login: { status: "idle", message: "", qr: "", nick: "", player: "", expireAt: 0, attempt: 0, steps: [] },
    scan: { status: "idle", message: "", qr: "", channel: "", uuid: "", expireAt: 0 },
    settings: {
      skyExe: "",
      skyFound: false,
      skyAuto: false,
      deviceId: "",
      deviceIdSet: false,
      deviceIdSource: "",
      deviceInfo: { fingerprint: "", uniqueId: "", hasKey: false, mode: "" },
      closeLauncher: true,
      railWidth: 0,
      mirrorDir: "",
      mirrorDefault: "",
      mirrorSameVolume: true,
      mirrorSizeText: "",
      dataDir: "",
      version: "",
      buildTime: "",
      repo: "",
      goVersion: "",
      protocol: "",
    },
    authHint: {},
    version: "",
  };
}

/** 整个应用共享的状态对象 */
export const state = reactive<AppState>(emptyState());

/** 打码开关：按账号 id 记录，或全局打开 */
export const reveal = reactive({
  all: false,
  ids: {} as Record<string, boolean>,
});

/** 判断某个账号当前是否显示明文 */
export function isRevealed(id: string): boolean {
  return reveal.all || !!reveal.ids[id];
}

/** 切换单个账号的显示状态 */
export function toggleReveal(id: string) {
  reveal.ids[id] = !reveal.ids[id];
}

/** 顶部总开关 */
export function toggleRevealAll() {
  reveal.all = !reveal.all;
  reveal.ids = {};
}

/** 当前选中的账号 */
export const currentAccount = computed<AccountView | undefined>(() =>
  state.accounts.find((a) => a.current)
);

/** 最近一次轮询的错误（网络断了之类），给界面提示用 */
export const pollError = reactive({ message: "" });

/** 拉一次状态 */
export async function refresh() {
  try {
    const data = await api.state();
    Object.assign(state, data);
    pollError.message = "";
  } catch (e) {
    pollError.message = e instanceof Error ? e.message : String(e);
  }
}

/** 启动轮询；返回停止函数 */
export function startPolling(intervalMs = 1000): () => void {
  void refresh();
  const timer = window.setInterval(refresh, intervalMs);
  return () => window.clearInterval(timer);
}
