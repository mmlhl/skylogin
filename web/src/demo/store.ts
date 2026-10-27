/**
 * 演示用功能实现：内存假数据 + 模拟行为。
 *
 * 和 api/mock.ts 的区别：这里不拦截 fetch、不依赖后端接口形状，
 * 是 contract.ts 的直接实现，专门用来验证「同一套功能，四套 UI」。
 */
import { reactive } from "vue";
import { demoUi } from "../shellkit/ui";
import { LAYOUT_NONE, recommendLayout, type Account, type GridLayout, type LauncherStore, type NewAccountDraft } from "../shellkit/contract";

let seq = 100;
const nextId = () => `demo${(seq++).toString().padStart(8, "0")}`;

function account(p: Partial<Account> & { name: string; channel: Account["channel"]; fullId: string }): Account {
  return {
    id: nextId(),
    maskedId: p.fullId.slice(0, 4) + "********" + p.fullId.slice(-4),
    maskedName: p.name.length > 1 ? p.name.slice(0, 1) + "***" : p.name,
    level: "",
    cred: "ok",
    running: false,
    runSeconds: 0,
    current: false,
    ...p,
  };
}

const state = reactive({
  accounts: [
    account({ name: "示例玩家", channel: "huawei", fullId: "1100000000000001", level: "7", current: true }),
    account({ name: "13800138000", channel: "netease", fullId: "aedemo0000000000", running: true, runSeconds: 42 * 60 }),
    account({ name: "归墟行者", channel: "huawei", fullId: "253f0000000007a", cred: "expired" }),
    account({ name: "云野常客", channel: "netease", fullId: "8c11000000000033" }),
  ] as Account[],
  maskAll: true,
  multiQualityMode: "",
  multiQualityFps: 0,
  selectedIds: [] as string[],
  toasts: [] as { id: number; text: string }[],
});

let toastSeq = 0;
function toast(text: string) {
  const id = ++toastSeq;
  state.toasts.push({ id, text });
  setTimeout(() => {
    const i = state.toasts.findIndex((t) => t.id === id);
    if (i >= 0) state.toasts.splice(i, 1);
  }, 2600);
}

/** 演示计时器：运行中的账号每秒 +1。模块级单例，避免 HMR 重复挂。 */
const g = globalThis as unknown as { __demoTicker?: number };
if (!g.__demoTicker) {
  g.__demoTicker = window.setInterval(() => {
    for (const a of state.accounts) if (a.running) a.runSeconds += 1;
  }, 1000);
}

export const store: LauncherStore = {
  get accounts() {
    return state.accounts;
  },
  get current() {
    return state.accounts.find((a) => a.current);
  },
  get runningAccounts() {
    return state.accounts.filter((a) => a.running);
  },
  get maskAll() {
    return state.maskAll;
  },
  get selectedIds() {
    return state.selectedIds;
  },
  get authHint() {
    return {} as import("../shellkit/contract").AuthHint;
  },
  get toasts() {
    return state.toasts;
  },

  select(id) {
    for (const a of state.accounts) a.current = a.id === id;
  },

  launch(id) {
    const acc = state.accounts.find((a) => a.id === (id ?? state.accounts.find((x) => x.current)?.id));
    if (!acc) return void toast("还没有账号，先添加一个");
    if (acc.running) return;
    const other = state.accounts.find((a) => a.running && a.id !== acc.id);
    if (other) return void toast(`游戏全局单实例：${other.name} 正在运行，请用「多开」`);
    acc.cred = "ok"; // 启动时自动刷新凭证
    acc.running = true;
    acc.runSeconds = 0;
    if (!acc.current) this.select(acc.id);
    toast(`已启动 ${acc.name}`);
  },

  stop(id) {
    const acc = state.accounts.find((a) => a.id === id);
    if (!acc?.running) return;
    acc.running = false;
    acc.runSeconds = 0;
    toast(`已停止 ${acc.name}`);
  },

  refreshCred(id) {
    const acc = state.accounts.find((a) => a.id === id);
    if (!acc) return;
    acc.cred = "ok";
    toast(`${acc.name} 的凭证已刷新`);
  },

  rename(id, name) {
    const acc = state.accounts.find((a) => a.id === id);
    if (acc && name.trim()) {
      acc.name = name.trim();
      toast("别名已修改");
    }
  },

  remove(id) {
    const i = state.accounts.findIndex((a) => a.id === id);
    if (i < 0) return;
    const [gone] = state.accounts.splice(i, 1);
    const sel = state.selectedIds.indexOf(id);
    if (sel >= 0) state.selectedIds.splice(sel, 1);
    if (gone.current && state.accounts[0]) state.accounts[0].current = true;
    toast(`已删除 ${gone.name}`);
  },

  toggleMask() {
    state.maskAll = !state.maskAll;
  },

  toggleSelect(id) {
    const i = state.selectedIds.indexOf(id);
    if (i >= 0) state.selectedIds.splice(i, 1);
    else state.selectedIds.push(id);
  },

  selectAll() {
    state.selectedIds = state.accounts.map((a) => a.id);
  },

  invertSelect() {
    state.selectedIds = state.accounts.filter((a) => !state.selectedIds.includes(a.id)).map((a) => a.id);
  },

  selectRunning() {
    state.selectedIds = state.accounts.filter((a) => a.running).map((a) => a.id);
    if (!state.selectedIds.length) toast("没有正在运行的账号");
  },

  clearSelect() {
    state.selectedIds = [];
  },

  bulkLaunch() {
    const ids = state.accounts.filter((a) => state.selectedIds.includes(a.id) && !a.running).map((a) => a.id);
    if (!ids.length) return void toast("选中的账号都在运行中");
    this.multiLaunch(ids);
  },

  bulkStop() {
    const running = state.accounts.filter((a) => state.selectedIds.includes(a.id) && a.running);
    if (!running.length) return void toast("选中的账号没有在运行的");
    for (const a of running) {
      a.running = false;
      a.runSeconds = 0;
    }
    toast(`已停止 ${running.length} 个客户端`);
  },

  bulkRemove() {
    const picked = state.accounts.filter((a) => state.selectedIds.includes(a.id));
    if (!picked.length) return;
    const names = picked.map((a) => a.name);
    state.accounts = state.accounts.filter((a) => !state.selectedIds.includes(a.id));
    state.selectedIds = [];
    if (!state.accounts.some((a) => a.current) && state.accounts[0]) state.accounts[0].current = true;
    toast(`已删除 ${names.length} 个账号：${names.join("、")}`);
  },

  addAccount(draft?: NewAccountDraft) {
    // 不带参数 = 打开共享的「添加账号」弹窗（选渠道 / 登录方式）
    if (!draft) {
      demoUi.addOpen = true;
      return;
    }
    const pool = ["暮土新丁", "禁阁巡游", "圣岛农夫", "遇境过客", "霞谷滑手"];
    const name = draft.name?.trim() || pool[state.accounts.length % pool.length];
    const fullId = nextId() + "fe";
    const acc = account({ name, channel: draft.channel, fullId });
    state.accounts.push(acc);
    this.select(acc.id);
    toast(`扫码成功，已添加 ${name}`);
  },

  multiLaunch(ids, layout, size, display) {
    const picked = state.accounts.filter((a) => ids.includes(a.id) && !a.running);
    if (!picked.length) return void toast("所选账号都在运行中");
    for (const a of picked) {
      a.cred = "ok";
      a.running = true;
      a.runSeconds = 0;
    }
    if (display === "fullscreen") {
      toast(`多开已启动 ${picked.length} 个客户端，全屏（用监控墙切换）`);
      return;
    }
    if (display === "cascade") {
      toast(`多开已启动 ${picked.length} 个客户端，窗口堆叠（每个都露出标题条）`);
      return;
    }
    const lay = layout ?? (picked.length > 1 ? recommendLayout(picked.length) : LAYOUT_NONE);
    const layText = lay.cols > 0 ? `，按${lay.label}平铺` : "";
    const sizeText = size ? `，窗口 ${size.w}×${size.h}` : "";
    toast(`多开已启动 ${picked.length} 个客户端${layText}${sizeText}`);
  },

  multiDefaults() {
    return { qualityMode: state.multiQualityMode, fps: state.multiQualityFps, arbiter: true, arbiterMin: 3 };
  },

  saveMultiDefaults(qualityMode, fps, arbiter, arbiterMin) {
    state.multiQualityMode = qualityMode;
    state.multiQualityFps = fps;
    toast("多开默认已保存");
  },

  openSettings() {
    toast("设置面板沿用正式版实现，演示里不弹");
  },

  toggleWall() {
    toast("监控墙为原生窗口，演示里不弹");
  },

  openRunning() {
    toast("「运行」聚合页为扩展示意");
  },

  toast,
};
