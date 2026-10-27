/**
 * 真实后端的 LauncherStore 实现：contract → /api/*。
 *
 * 数据来源：useAppState 每秒轮询的 state（Go 是唯一真相来源），
 * 动作都是异步发请求：失败立刻 toast，成功后等下一次轮询刷出新状态。
 * 「运行时长」后端没有，这里按 running 翻转的时间点本地推算。
 */
import { reactive, watch } from "vue";
import { api } from "./api/client";
import { state } from "./composables/useAppState";
import { maskId, maskName } from "./composables/useMask";
import {
  recommendLayout,
  type Account,
  type Channel,
  LAYOUT_NONE,
  type GridLayout,
  type LauncherStore,
} from "./shellkit/contract";

export interface RealStoreHooks {
  /** 添加账号：打开渠道/登录方式选择（真实登录流程从那里走） */
  onAdd?: () => void;
  /** 刷新凭证：按账号打开对应的登录方式列表 */
  onRefresh?: (acc: { accountId: string; name: string; channel: Channel }) => void;
  /** 打开设置面板 */
  onOpenSettings?: () => void;
}

export function createRealStore(hooks: RealStoreHooks): LauncherStore {
  const st = reactive({
    accounts: [] as Account[],
    maskAll: true,
    selectedIds: [] as string[],
    toasts: [] as { id: number; text: string }[],
  });

  /** 运行起点：账号翻到 running 时记一个时间戳 */
  const startedAt = new Map<string, number>();

  let toastSeq = 0;
  function toast(text: string) {
    const id = ++toastSeq;
    st.toasts.push({ id, text });
    setTimeout(() => {
      const i = st.toasts.findIndex((t) => t.id === id);
      if (i >= 0) st.toasts.splice(i, 1);
    }, 2600);
  }

  function sync() {
    const now = Date.now();
    const list: Account[] = state.accounts.map((a) => {
      if (a.running && !startedAt.has(a.id)) startedAt.set(a.id, now);
      if (!a.running) startedAt.delete(a.id);
      const begin = startedAt.get(a.id);
      return {
        id: a.id,
        name: a.name,
        maskedName: maskName(a.name),
        channel: a.channel,
        fullId: a.accountId,
        maskedId: a.masked || maskId(a.accountId),
        level: a.level,
        cred: a.credState === "expired" ? "expired" : "ok",
        running: !!a.running,
        runSeconds: a.running && begin ? Math.floor((now - begin) / 1000) : 0,
        current: a.current,
      };
    });
    for (const k of [...startedAt.keys()]) {
      if (!list.some((a) => a.id === k)) startedAt.delete(k);
    }
    // 只在变化时替换，避免每秒无谓地触发全界面重渲
    if (JSON.stringify(list) !== JSON.stringify(st.accounts)) {
      st.accounts.splice(0, st.accounts.length, ...list);
    }
  }
  watch(() => state.accounts, sync, { immediate: true });

  const find = (id: string) => st.accounts.find((a) => a.id === id);
  const currentId = () => st.accounts.find((a) => a.current)?.id;

  const store: LauncherStore = {
    get accounts() {
      return st.accounts;
    },
    get current() {
      return st.accounts.find((a) => a.current);
    },
    get runningAccounts() {
      return st.accounts.filter((a) => a.running);
    },
    get maskAll() {
      return st.maskAll;
    },
    get selectedIds() {
      return st.selectedIds;
    },
    get authHint() {
      const h = state.authHint;
      return h && h.kind ? { kind: h.kind, message: h.message, account: h.account } : {};
    },
    get toasts() {
      return st.toasts;
    },

    select(id) {
      void api.selectAccount(id).then((r) => {
        if (r.ok === false) toast(r.message || "切换失败");
      });
    },

    launch(id) {
      const target = id ?? currentId();
      if (!target) return toast("还没有账号，先添加一个");
      void (async () => {
        if (target !== currentId()) {
          const s = await api.selectAccount(target);
          if (s.ok === false) return toast(s.message || "切换失败");
        }
        const r = await api.launch();
        if (r.ok === false) toast(r.message || "启动失败");
      })();
    },

    stop(id) {
      const name = find(id)?.name ?? "账号";
      void api.stopAccount(id).then((r) => {
        if (r.ok === false) toast(r.message || `停止 ${name} 失败`);
      });
    },

    refreshCred(id) {
      const a = find(id);
      if (a) hooks.onRefresh?.({ accountId: a.id, name: a.name, channel: a.channel });
    },

    rename(id, name) {
      if (!name.trim()) return;
      void api.renameAccount(id, name).then((r) => {
        if (r.ok === false) toast(r.message || "改名失败");
      });
    },

    remove(id) {
      void api.deleteAccount(id).then((r) => {
        if (r.ok === false) toast(r.message || "删除失败");
      });
    },

    toggleMask() {
      st.maskAll = !st.maskAll;
    },

    addAccount() {
      hooks.onAdd?.();
    },

    toggleSelect(id) {
      const i = st.selectedIds.indexOf(id);
      if (i >= 0) st.selectedIds.splice(i, 1);
      else st.selectedIds.push(id);
    },

    selectAll() {
      st.selectedIds = st.accounts.map((a) => a.id);
    },

    invertSelect() {
      st.selectedIds = st.accounts.filter((a) => !st.selectedIds.includes(a.id)).map((a) => a.id);
    },

    selectRunning() {
      st.selectedIds = st.accounts.filter((a) => a.running).map((a) => a.id);
      if (!st.selectedIds.length) toast("没有正在运行的账号");
    },

    clearSelect() {
      st.selectedIds = [];
    },

    bulkLaunch() {
      const ids = st.accounts.filter((a) => st.selectedIds.includes(a.id) && !a.running).map((a) => a.id);
      if (!ids.length) return toast("选中的账号都在运行中");
      store.multiLaunch(ids);
    },

    bulkStop() {
      const ids = st.accounts.filter((a) => st.selectedIds.includes(a.id) && a.running).map((a) => a.id);
      if (!ids.length) return toast("选中的账号没有在运行的");
      void Promise.all(ids.map((id) => api.stopAccount(id))).then((rs) => {
        const fail = rs.filter((r) => r.ok === false).length;
        toast(fail ? `已停止 ${rs.length - fail} 个，${fail} 个失败` : `已停止 ${rs.length} 个客户端`);
      });
    },

    bulkRemove() {
      const picked = st.accounts.filter((a) => st.selectedIds.includes(a.id));
      if (!picked.length) return;
      const names = picked.map((a) => a.name);
      void Promise.all(picked.map((a) => api.deleteAccount(a.id))).then((rs) => {
        const fail = rs.filter((r) => r.ok === false).length;
        if (!fail) st.selectedIds = [];
        toast(fail ? `删除了 ${rs.length - fail} 个，${fail} 个失败` : `已删除 ${names.length} 个账号`);
      });
    },

    multiLaunch(ids, layout, size, display) {
      const ready = ids.filter((id) => !find(id)?.running);
      if (!ready.length) return toast("所选账号都在运行中");
      if (ready.length === 1) return store.launch(ready[0]);
      // 全屏不带排列方案；LAYOUT_NONE（cols=0）让后面的拼接逻辑统一走"无排列"分支
      const fullscreen = display === "fullscreen";
      const lay = fullscreen ? LAYOUT_NONE : (layout ?? recommendLayout(ready.length));
      void (async () => {
        const r = await api.launchMulti(ready, lay.cols > 0 ? lay : undefined, size, display);
        if (r.ok === false) return toast(r.message || "多开失败");
        const modeText =
          display === "fullscreen"
            ? "，全屏（用监控墙或 Alt+Tab 切换）"
            : display === "cascade"
              ? "，窗口堆叠（每个都露出标题条）"
              : lay.cols > 0
                ? `，将按${lay.label}平铺`
                : "";
        const sizeText = !fullscreen && size ? `，窗口 ${size.w}×${size.h}` : "";
        // 游戏窗口最小 1024×720，3 个以上屏幕必然互相遮挡——提示用监控墙
        const wallHint = ready.length >= 3 ? "；有遮挡可用「监控墙」查看全部" : "";
        toast(`多开已启动 ${ready.length} 个客户端${modeText}${sizeText}${wallHint}`);
      })();
    },

    multiDefaults() {
      return {
        qualityMode: state.settings.multiQualityMode,
        fps: state.settings.multiQualityFps,
        arbiter: state.settings.mouseArbiter,
        arbiterMin: state.settings.mouseArbiterMin,
      };
    },

    saveMultiDefaults(qualityMode, fps, arbiter, arbiterMin) {
      void api.saveSettings({
        multiQualityMode: qualityMode,
        multiQualityFps: fps,
        mouseArbiter: arbiter,
        mouseArbiterMin: arbiterMin,
      }).then((r) => {
        if (r.ok === false) toast(r.message || "保存失败");
      });
    },

    openSettings() {
      hooks.onOpenSettings?.();
    },

    toggleWall() {
      void api.wallToggle().then((r) => toast(r.message || (r.ok === false ? "监控墙开关失败" : "监控墙已更新")));
    },

    openRunning() {
      toast("「运行」聚合页规划中，账号状态见详情面板");
    },

    toast,
  };
  return store;
}
