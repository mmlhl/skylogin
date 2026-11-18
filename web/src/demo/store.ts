/**
 * 演示用功能实现：内存假数据 + 模拟行为。
 *
 * 和 api/mock.ts 的区别：这里不拦截 fetch、不依赖后端接口形状，
 * 是 contract.ts 的直接实现，专门用来验证「同一套功能，四套 UI」。
 */
import { reactive } from "vue";
import { mockLayoutPlan } from "./layoutPlan";
import { demoUi } from "../shellkit/ui";
import {
  layoutLabel,
  type Account,
  type LaunchGroup,
  type LauncherStore,
  type NewAccountDraft,
} from "../shellkit/contract";

let seq = 100;
const nextId = () => `demo${(seq++).toString().padStart(8, "0")}`;

function account(
  p: Partial<Account> & {
    name: string;
    channel: Account["channel"];
    fullId: string;
  },
): Account {
  return {
    id: nextId(),
    playerId: p.playerId || p.fullId,
    maskedId: p.fullId.slice(0, 4) + "********" + p.fullId.slice(-4),
    maskedName: p.name.length > 1 ? p.name.slice(0, 1) + "***" : p.name,
    level: "",
    cred: "ok",
    tags: p.tags ?? [],
    createdAt: p.createdAt ?? Date.now() / 1000,
    lastLoginTs: p.lastLoginTs ?? 0,
    running: false,
    runSeconds: 0,
    current: false,
    ...p,
  };
}

const state = reactive({
  accounts: [
    account({
      name: "示例玩家",
      channel: "huawei",
      fullId: "1100000000000001",
      level: "7",
      current: true,
    }),
    account({
      name: "13800138000",
      channel: "netease",
      fullId: "aedemo0000000000",
      running: true,
      runSeconds: 42 * 60,
    }),
    account({
      name: "归墟行者",
      channel: "huawei",
      fullId: "253f0000000007a",
      cred: "expired",
    }),
    account({
      name: "云野常客",
      channel: "netease",
      fullId: "8c11000000000033",
    }),
  ] as Account[],
  tagOrder: [] as string[],
  maskAll: true,
  multiQualityMode: "",
  multiQualityFps: 0,
  groups: [] as LaunchGroup[],
  selectedIds: [] as string[],
  toasts: [] as { id: number; text: string }[],
});

// 两套演示启动组：一套两开（有位置），一套单开全屏（没记位置）
state.groups.push({
  id: "g-demo-1",
  name: "日常双开",
  layout: "tile",
  cols: 2,
  rows: 2,
  defMode: "HighQuality",
  defFps: 60,
  defDisplay: "windowed",
  stopFirst: true,
  autoWall: true,
  gapSec: 1,
  screenW: 1920,
  screenH: 1080,
  updated: "今天 21:04",
  slots: [
    {
      accountKey: state.accounts[0].id,
      name: "示例玩家",
      mode: "",
      fps: 0,
      display: "",
      disabled: false,
      x: 0,
      y: 0,
      w: 960,
      h: 540,
      hasRect: true,
    },
    {
      accountKey: state.accounts[1].id,
      name: "13800138000",
      mode: "LowPower",
      fps: 30,
      display: "",
      disabled: false,
      x: 960,
      y: 540,
      w: 960,
      h: 540,
      hasRect: true,
    },
  ],
});
state.groups.push({
  id: "g-demo-2",
  name: "全屏挂机",
  layout: "fullscreen",
  cols: 1,
  rows: 1,
  defMode: "Presentation",
  defFps: 120,
  defDisplay: "fullscreen",
  stopFirst: true,
  autoWall: false,
  gapSec: 0,
  screenW: 0,
  screenH: 0,
  updated: "11-08",
  slots: [
    {
      accountKey: state.accounts[2].id,
      name: "归墟行者",
      mode: "",
      fps: 0,
      display: "",
      disabled: false,
      x: 0,
      y: 0,
      w: 0,
      h: 0,
      hasRect: false,
    },
  ],
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

export const accountDemo = { delay: 0, tagsError: "", reorderError: "", tagOrderError: "", calls: [] as { action: string; value: unknown }[] };
(window as unknown as { __accountDemo: typeof accountDemo }).__accountDemo = accountDemo;
function normalizeDemoTags(tags: string[], limit = 8): string[] {
  const result: string[] = [], seen = new Set<string>();
  for (const raw of tags) {
    const tag = [...raw.trim()].slice(0, 16).join("");
    const key = tag.toLowerCase();
    if (!tag || seen.has(key)) continue;
    seen.add(key); result.push(tag);
    if (result.length >= limit) break;
  }
  return result;
}
let accountSaveQueue: Promise<unknown> = Promise.resolve();
function saveDemoAccount(action: "tags" | "reorder" | "tagOrder", value: unknown, apply: () => void): Promise<boolean> {
  const run = async () => {
    accountDemo.calls.push({ action, value });
    const error = accountDemo[`${action}Error`];
    if (accountDemo.delay) await new Promise((resolve) => setTimeout(resolve, accountDemo.delay));
    if (error) { toast(error); return false; }
    try { apply(); return true; }
    catch (e) { toast(e instanceof Error ? e.message : "保存失败"); return false; }
  };
  const result = accountSaveQueue.then(run, run);
  accountSaveQueue = result;
  return result;
}

export const store: LauncherStore = {
  get accounts() {
    return state.accounts;
  },
  get groups() {
    return state.groups;
  },
  async planLayout(body) {
    return mockLayoutPlan(body);
  },
  async saveGroup(g) {
    const saved = JSON.parse(
      JSON.stringify({ ...g, id: g.id || `g${Date.now()}` }),
    ) as LaunchGroup;
    const at = state.groups.findIndex((x) => x.id === saved.id);
    if (at >= 0) state.groups[at] = saved;
    else state.groups.push(saved);
    return saved;
  },
  async deleteGroup(id) {
    const at = state.groups.findIndex((x) => x.id === id);
    if (at >= 0) state.groups.splice(at, 1);
  },
  async launchGroup() {
    // 演示模式没有真实进程：只把"会发生什么"说清楚，不假装启动成功
    toast("演示模式：真实版会先停掉正在跑的客户端，再按这套的位置窗口");
    return { ok: true, message: "演示模式不启动" };
  },
  async captureRects() {
    return {
      rects: [],
      screenW: 1920,
      screenH: 1080,
      workArea: { x: 0, y: 0, w: 1920, h: 1040 },
    };
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
    const acc = state.accounts.find(
      (a) => a.id === (id ?? state.accounts.find((x) => x.current)?.id),
    );
    if (!acc) return void toast("还没有账号，先添加一个");
    if (acc.running) return;
    const other = state.accounts.find((a) => a.running && a.id !== acc.id);
    if (other)
      return void toast(`游戏全局单实例：${other.name} 正在运行，请用「多开」`);
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

  setTags(id, tags) {
    const draft = [...tags];
    return saveDemoAccount("tags", { id, tags: draft }, () => {
      const a = state.accounts.find((x) => x.id === id);
      if (!a) throw new Error("找不到这个账号");
      a.tags = normalizeDemoTags(draft);
    });
  },
  get tagOrder() { return state.tagOrder; },
  setTagOrder(tags) {
    const draft = [...tags];
    return saveDemoAccount("tagOrder", draft, () => { state.tagOrder = normalizeDemoTags(draft, Infinity); });
  },

  reorder(ids) {
    const draft = [...ids];
    return saveDemoAccount("reorder", draft, () => {
      if (!draft.length) throw new Error("缺少顺序");
      const byId = new Map(state.accounts.map((a) => [a.id, a]));
      const next = [...new Set(draft)].map((id) => byId.get(id)).filter((a): a is Account => !!a);
      for (const a of state.accounts) if (!next.includes(a)) next.push(a);
      state.accounts.splice(0, state.accounts.length, ...next);
    });
  },

  setAccountQuality(id, mode, fps, _display, autoEnter) {
    const acc = state.accounts.find((a) => a.id === id);
    if (acc) acc.qualityMode = mode;
    if (acc) acc.qualityFps = fps;
    if (acc) acc.autoEnter = autoEnter ?? null;
  },

  setRailWidth() {},

  duplicate(id, name, region = "") {
    const acc = state.accounts.find((a) => a.id === id);
    if (!acc) return;
    const n =
      state.accounts.filter((a) => a.playerId === acc.playerId && a.copyKey)
        .length + 1;
    const copy: Account = {
      ...acc,
      id: acc.playerId + "-" + (n + 1),
      copyKey: acc.playerId + "-" + (n + 1),
      name: name || acc.name + " · 副本" + n,
      region: region,
      regionLabel:
        region === "ios"
          ? "iOS 区"
          : region === "android"
            ? "安卓区"
            : region === "harmony"
              ? "鸿蒙区"
              : "",
      current: false,
    };
    state.accounts.push(copy);
    toast("已创建副本");
  },

  setAccountRegion(id, region) {
    const acc = state.accounts.find((a) => a.id === id);
    if (!acc) return;
    acc.region = region;
    acc.regionLabel =
      region === "ios"
        ? "iOS 区"
        : region === "android"
          ? "安卓区"
          : region === "harmony"
            ? "鸿蒙区"
            : "";
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
    state.selectedIds = state.accounts
      .filter((a) => !state.selectedIds.includes(a.id))
      .map((a) => a.id);
  },

  selectRunning() {
    state.selectedIds = state.accounts
      .filter((a) => a.running)
      .map((a) => a.id);
    if (!state.selectedIds.length) toast("没有正在运行的账号");
  },

  clearSelect() {
    state.selectedIds = [];
  },

  bulkLaunch() {
    const ids = state.accounts
      .filter((a) => state.selectedIds.includes(a.id) && !a.running)
      .map((a) => a.id);
    if (!ids.length) return void toast("选中的账号都在运行中");
    this.multiLaunch(ids);
  },

  bulkStop() {
    const running = state.accounts.filter(
      (a) => state.selectedIds.includes(a.id) && a.running,
    );
    if (!running.length) return void toast("选中的账号没有在运行的");
    for (const a of running) {
      a.running = false;
      a.runSeconds = 0;
    }
    toast(`已停止 ${running.length} 个客户端`);
  },

  bulkRemove() {
    const picked = state.accounts.filter((a) =>
      state.selectedIds.includes(a.id),
    );
    if (!picked.length) return;
    const names = picked.map((a) => a.name);
    state.accounts = state.accounts.filter(
      (a) => !state.selectedIds.includes(a.id),
    );
    state.selectedIds = [];
    if (!state.accounts.some((a) => a.current) && state.accounts[0])
      state.accounts[0].current = true;
    toast(`已删除 ${names.length} 个账号：${names.join("、")}`);
  },

  addAccount(draft?: NewAccountDraft) {
    // 不带参数 = 打开共享的「添加账号」弹窗（选渠道 / 登录方式）
    if (!draft) {
      demoUi.addOpen = true;
      return;
    }
    const pool = ["暮土新丁", "禁阁巡游", "圣岛农夫", "遇境过客", "霞谷滑手"];
    const name =
      draft.name?.trim() || pool[state.accounts.length % pool.length];
    const fullId = nextId() + "fe";
    const acc = account({ name, channel: draft.channel, fullId });
    state.accounts.push(acc);
    this.select(acc.id);
    toast(`扫码成功，已添加 ${name}`);
  },

  multiLaunch(ids, layout, size, display) {
    const picked = state.accounts.filter(
      (a) => ids.includes(a.id) && !a.running,
    );
    if (!picked.length) return void toast("所选账号都在运行中");
    for (const a of picked) {
      a.cred = "ok";
      a.running = true;
      a.runSeconds = 0;
    }
    const plan = mockLayoutPlan({ count: picked.length, mode: display || "" });
    toast(`演示模式：${picked.length} 个客户端，${layoutLabel(plan.mode)}`);
  },

  multiDefaults() {
    return {
      qualityMode: state.multiQualityMode,
      fps: state.multiQualityFps,
      arbiter: true,
      arbiterMin: 3,
      autoEnter: true,
      autoEnterSlotMs: 10000,
    };
  },

  saveMultiDefaults(qualityMode, fps, arbiter, arbiterMin, autoEnter, autoEnterSlotMs) {
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
