/**
 * 真实后端的 LauncherStore 实现：contract → /api/*。
 *
 * 数据来源：useAppState 每秒轮询的 state（Go 是唯一真相来源），
 * 标签与排序保存会等待确认并立即采用规范化响应，其它动作仍由轮询对齐。
 * 「运行时长」后端没有，这里按 running 翻转的时间点本地推算。
 */
import { reactive, watch } from "vue";
import { api, latestStateReadSequence } from "./api/client";
import { state } from "./composables/useAppState";
import { maskId, maskName } from "./composables/useMask";
import {
  canonicalLayout,
  layoutLabel,
  type Account,
  type Channel,
  type LaunchGroup,
  type LauncherStore,
} from "./shellkit/contract";
import { demoUi } from "./shellkit/ui";

export interface RealStoreHooks {
  /** 添加账号：打开渠道/登录方式选择（真实登录流程从那里走） */
  onAdd?: () => void;
  /** 刷新凭证：存过密码的号直接静默重登，没存密码的才按渠道打开登录方式 */
  onRefresh?: (acc: {
    accountId: string;
    name: string;
    channel: Channel;
    loginUser?: string;
  }) => void;
  /** 打开设置面板 */
  onOpenSettings?: () => void;
}

export function createRealStore(hooks: RealStoreHooks): LauncherStore {
  const st = reactive({
    accounts: [] as Account[],
    groups: [] as LaunchGroup[],
    tagOrder: [] as string[],
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

  type SavedOverlay<T> = { value: T; pending: boolean; fence: number };
  const tagOverlays = new Map<string, SavedOverlay<string[]>>();
  let orderOverlay: SavedOverlay<string[]> | undefined;
  let tagOrderOverlay: SavedOverlay<string[]> | undefined;
  let selectionOverlay: SavedOverlay<string | undefined> | undefined;
  let confirmedSelection: string | undefined;
  let selectionIntent = 0;
  let selectionQueue: Promise<unknown> = Promise.resolve();
  let lastAccountRead = 0;
  let saveQueue: Promise<unknown> = Promise.resolve();
  const same = (a: unknown, b: unknown) => JSON.stringify(a) === JSON.stringify(b);
  const sortByIds = <T extends { id: string }>(list: T[], ids: string[]) => {
    const ranks = new Map([...new Set(ids)].map((id, i) => [id, i]));
    return [...list].sort((a, b) => (ranks.get(a.id) ?? ids.length) - (ranks.get(b.id) ?? ids.length));
  };
  // 保存串行执行，失败只提示一次；后续操作不能被较早的响应或回滚覆盖。
  function enqueueSave(operation: () => Promise<boolean>): Promise<boolean> {
    const result = saveQueue.then(operation, operation);
    saveQueue = result.catch(() => undefined);
    return result;
  }

  function sync() {
    const sequence = state.clientReadSequence ?? 0;
    const stale = sequence < lastAccountRead;
    lastAccountRead = Math.max(lastAccountRead, sequence);
    // 保存结束后发起的新观测已越过屏障，应接纳服务端后续修改，而非永久等待值相等。
    const canRelease = <T>(overlay: SavedOverlay<T> | undefined) => !!overlay && !overlay.pending && !stale && sequence > overlay.fence;
    if (canRelease(selectionOverlay)) selectionOverlay = undefined;
    if (canRelease(orderOverlay)) orderOverlay = undefined;
    if (canRelease(tagOrderOverlay)) tagOrderOverlay = undefined;
    for (const [id, overlay] of tagOverlays) if (canRelease(overlay)) tagOverlays.delete(id);
    const previousCurrent = st.accounts.find((a) => a.current)?.id;
    if (!selectionOverlay && !stale) confirmedSelection = state.accounts.find((a) => a.current)?.id;
    const previousTags = new Map(st.accounts.map((a) => [a.id, a.tags]));
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
        current: selectionOverlay ? a.id === selectionOverlay.value : stale ? a.id === previousCurrent : a.current,
        region: a.region,
        // 被寄宿的号：渠道由扫码设备固定，详情页不显示选区按钮。
        // 这两个字段以前漏了透传，界面上就一直是"可选区"的样子。
        hosted: a.hosted,
        hostedBy: a.hostedBy,
        // 存过密码 → 「刷新凭证」可以静默重登；loginUser 用来预填输入框
        hasPassword: a.hasPassword,
        loginUser: a.loginUser,
        regionBlocked: a.regionBlocked,
        playerId: a.playerId,
        copyKey: a.copyKey,
        // 标签：后端给的是数组（nil 已经转成 []），这里再兜一次底
        tags: tagOverlays.get(a.id)?.value ?? (stale ? previousTags.get(a.id) : undefined) ?? a.tags ?? [],
        createdAt: a.createdAt,
        lastLoginTs: a.lastLogin,
        qualityMode: a.qualityMode,
        qualityFps: a.qualityFps,
        displayMode: a.displayMode,
        // 详情页：登录环境与时间（Go 侧已经在 accountView 里格式化成串了）
        mpayPath: a.mpayPath,
        mpayVersion: a.mpayVersion,
        gameId: a.gameId,
        loginMethod: a.loginMethod,
        source: a.source,
        created: a.created,
        lastLogin: a.lastLoginText,
        tokenSaLabel: a.tokenSaLabel,
        tokenAt: a.tokenAtText,
        lastCheck: a.lastCheck,
        lastError: a.lastError,
      };
    });
    const desiredOrder = orderOverlay?.value ?? (stale ? st.accounts.map((a) => a.id) : undefined);
    if (desiredOrder) list.splice(0, list.length, ...sortByIds(list, desiredOrder));
    const incomingTags = state.settings.accountTagOrder ?? [];
    const nextTagOrder = tagOrderOverlay?.value ?? (stale ? st.tagOrder : incomingTags);
    if (!same(st.tagOrder, nextTagOrder)) st.tagOrder = [...nextTagOrder];
    // 墙按钮 / API 改了打码状态时，列表跟随（每秒轮询对齐一次）
    st.maskAll = state.settings.maskTitles;
    for (const k of [...startedAt.keys()]) {
      if (!list.some((a) => a.id === k)) startedAt.delete(k);
    }
    // 只在变化时替换，避免每秒无谓地触发全界面重渲
    if (JSON.stringify(list) !== JSON.stringify(st.accounts)) {
      st.accounts.splice(0, st.accounts.length, ...list);
    }
  }
  watch(() => [state.accounts, state.settings.accountTagOrder], sync, { immediate: true });

  /**
   * 启动组：把后端视图映射成契约结构。
   *
   * 为什么在这里补一堆默认值：后端会省略空字段（omitempty），
   * 而界面（尤其是输入框）拿到 undefined 会显示成空字符串、
   * v-model.number 还会把空串变成 NaN。统一在入口补成 0/""/false 最省事。
   */
  function mapGroups(): LaunchGroup[] {
    const list = state.groups ?? [];
    return list.map((g) => ({
      id: g.id,
      name: g.name,
      layout: canonicalLayout(g.layout),
      cols: g.cols || 2,
      rows: g.rows || 2,
      defMode: g.defMode ?? "",
      defFps: g.defFps ?? 0,
      defDisplay: g.defDisplay ?? "",
      stopFirst: g.stopFirst !== false,
      autoWall: g.autoWall !== false,
      gapSec: g.gapSec ?? 0,
      screenW: g.screenW ?? 0,
      screenH: g.screenH ?? 0,
      updated: g.updated,
      slots: (g.slots ?? []).map((s) => ({
        accountKey: s.accountKey,
        name: s.name,
        missing: !!s.missing,
        mode: s.mode ?? "",
        fps: s.fps ?? 0,
        display: s.display ?? "",
        disabled: !!s.disabled,
        x: s.x ?? 0,
        y: s.y ?? 0,
        w: s.w ?? 0,
        h: s.h ?? 0,
        hasRect: !!s.hasRect,
        zOrder: s.zOrder,
      })),
    }));
  }
  watch(
    () => state.groups,
    () => {
      st.groups.splice(0, st.groups.length, ...mapGroups());
    },
    { immediate: true, deep: true },
  );

  const find = (id: string) => st.accounts.find((a) => a.id === id);
  const currentId = () => st.accounts.find((a) => a.current)?.id;

  const store: LauncherStore = {
    get accounts() {
      return st.accounts;
    },
    get groups() {
      return st.groups;
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
      // 整条原样透传：items 里每条都带 accountId，横幅按它精确定位账号。
      // 早先只传了 account 名字，于是点刷新会落到"当前选中"的号上——
      // 刷错了账号还看不出错（2026-11-11 反馈）。
      return h && h.kind ? h : {};
    },
    get toasts() {
      return st.toasts;
    },

    select(id) {
      const intent = ++selectionIntent;
      selectionOverlay = { value: id, pending: true, fence: latestStateReadSequence() };
      sync();
      const run = async () => {
        if (intent !== selectionIntent) return;
        try {
          const r = await api.selectAccount(id);
          if (r.ok === false) throw new Error(r.message || "切换失败");
          confirmedSelection = id;
          if (intent === selectionIntent) selectionOverlay = { value: id, pending: false, fence: latestStateReadSequence() };
        } catch (e) {
          if (intent === selectionIntent) {
            selectionOverlay = { value: confirmedSelection, pending: false, fence: latestStateReadSequence() };
            toast(e instanceof Error ? e.message : "切换失败");
          }
        }
        sync();
      };
      selectionQueue = selectionQueue.then(run, run);
    },

    launch(id) {
      const target = id ?? currentId();
      if (!target) return toast("还没有账号，先添加一个");
      void (async () => {
        await selectionQueue;
        if (target !== confirmedSelection) {
          const s = await api.selectAccount(target);
          if (s.ok === false) return toast(s.message || "切换失败");
          confirmedSelection = target;
          selectionOverlay = { value: target, pending: false, fence: latestStateReadSequence() };
          sync();
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
      if (!a) return;
      const openPicker = () =>
        hooks.onRefresh?.({
          accountId: a.id,
          name: a.name,
          channel: a.channel,
          loginUser: a.loginUser,
        });

      // 存过密码的号：直接静默重登，不用再让用户输一遍账号密码。
      // 被寄宿的号连密码都不用：它的票来自宿主的平台槽，槽位票能换回一条新凭证
      // （后端 /api/accounts/relogin 会走寄宿续期）。两样都没有才让用户动手。
      // 密码已经不对了（在别处改过）时后端会顺手清掉它，这里退回手动登录。
      if (!a.hasPassword && !a.hosted) {
        openPicker();
        return;
      }
      toast(`${a.name} 正在刷新凭证…`);
      void api.reloginAccount(a.id).then((r) => {
        if (r.ok === false) {
          toast(r.message || "刷新失败，请手动登录一次");
          openPicker();
          return;
        }
        toast(`${a.name} 凭证已刷新`);
      });
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

    setTags(id, tags) {
      const draft = [...tags];
      return enqueueSave(async () => {
        const before = [...(find(id)?.tags ?? [])];
        const overlay = { value: draft, pending: true, fence: latestStateReadSequence() };
        tagOverlays.set(id, overlay);
        sync();
        try {
          const r = await api.setAccountTags(id, draft);
          if (r.ok === false || !r.account || r.account.id !== id || !Array.isArray(r.account.tags)) throw new Error(r.message || "保存标签失败：未收到确认结果");
          overlay.value = [...r.account.tags];
          overlay.pending = false;
          overlay.fence = latestStateReadSequence();
          sync();
          return true;
        } catch (e) {
          overlay.value = before;
          overlay.pending = false;
          overlay.fence = latestStateReadSequence();
          sync();
          toast(e instanceof Error ? e.message : "保存标签失败");
          return false;
        }
      });
    },

    get tagOrder() { return st.tagOrder; },
    setTagOrder(tags) {
      const draft = [...tags];
      return enqueueSave(async () => {
        const before = [...st.tagOrder];
        tagOrderOverlay = { value: draft, pending: true, fence: latestStateReadSequence() };
        sync();
        try {
          const r = await api.setTagOrder(draft);
          if (r.ok === false || !Array.isArray(r.tagOrder)) throw new Error(r.message || "保存标签顺序失败：未收到确认结果");
          tagOrderOverlay = { value: [...r.tagOrder], pending: false, fence: latestStateReadSequence() };
          sync();
          return true;
        } catch (e) {
          tagOrderOverlay = { value: before, pending: false, fence: latestStateReadSequence() };
          sync();
          toast(e instanceof Error ? e.message : "保存标签顺序失败");
          return false;
        }
      });
    },

    reorder(ids) {
      const draft = [...ids];
      return enqueueSave(async () => {
        const before = st.accounts.map((a) => a.id);
        orderOverlay = { value: draft, pending: true, fence: latestStateReadSequence() };
        sync();
        try {
          const r = await api.reorderAccounts(draft);
          if (r.ok === false || !Array.isArray(r.ids)) throw new Error(r.message || "保存顺序失败：未收到确认结果");
          orderOverlay = { value: [...r.ids], pending: false, fence: latestStateReadSequence() };
          sync();
          return true;
        } catch (e) {
          orderOverlay = { value: before, pending: false, fence: latestStateReadSequence() };
          sync();
          toast(e instanceof Error ? e.message : "保存顺序失败");
          return false;
        }
      });
    },

    setAccountQuality(id, mode, fps, display = "", autoEnter) {
      // 画质 / 帧率 / 显示方式 / 自动进入一起存：都是"这行账号的游戏设置"，
      // 分几次请求只会多出几次半写状态。
      void api.setGameSettings(id, mode, fps, display, autoEnter).then((r) => {
        if (r.ok === false) toast(r.message || "保存失败");
      });
    },

    async planLayout(body) {
      const r = await api.planLayout(body);
      if (r.ok === false || !Array.isArray(r.rects))
        throw new Error(r.message || "无法规划窗口布局，请重试");
      return r;
    },

    async saveGroup(g) {
      const r = await api.saveGroup(g);
      if (r.ok === false) throw new Error(r.message || "保存启动组失败");
      const id = r.id || g.id;
      if (!id) throw new Error("保存未完成，请重试");
      // 优先采用服务端规范化结果，避免把未被采用的值显示为已保存。
      const saved = JSON.parse(
        JSON.stringify(r.groups?.find((x) => x.id === id) ?? { ...g, id }),
      ) as LaunchGroup;
      if (r.groups)
        st.groups.splice(0, st.groups.length, ...(r.groups as LaunchGroup[]));
      else {
        const at = st.groups.findIndex((x) => x.id === id);
        if (at >= 0) st.groups[at] = saved;
        else st.groups.push(saved);
      }
      return saved;
    },

    async deleteGroup(id) {
      const r = await api.deleteGroup(id);
      if (r.ok === false) throw new Error(r.message || "删除启动组失败");
      if (r.groups)
        st.groups.splice(0, st.groups.length, ...(r.groups as LaunchGroup[]));
      else {
        const at = st.groups.findIndex((x) => x.id === id);
        if (at >= 0) st.groups.splice(at, 1);
      }
    },

    async launchGroup(id) {
      const r = await api.launchGroup(id);
      // 后端的 message 是给用户看的整句（含"按位置摆放"/"位置已失效"这类关键信息），
      // 别在这里改写成通用文案，否则用户不知道窗口为什么会乱。
      return { ok: r.ok !== false, message: r.message };
    },

    async captureRects() {
      const r = await api.clientRects();
      if (r.ok === false) throw new Error(r.message || "读取窗口位置失败");
      return {
        screenW: r.screenW ?? 0,
        screenH: r.screenH ?? 0,
        workArea: r.workArea,
        rects: (r.rects ?? []).map((x) => ({
          ...x,
          fullscreen: !!x.fullscreen,
        })),
      };
    },

    setRailWidth(width: number) {
      void api.setRailWidth(width);
    },

    duplicate(id, name, region = "") {
      void api.duplicateAccount(id, name, region).then((r) => {
        if (r.ok === false) {
          toast(r.message || "创建副本失败");
          return;
        }
        toast("已创建副本");
      });
    },

    setAccountRegion(id: string, region: string) {
      void api.setAccountRegion(id, region).then((r) => {
        if (r.ok === false) toast(r.message || "切换渠道失败");
      });
    },

    toggleMask() {
      const next = !st.maskAll;
      st.maskAll = next;
      void api.setMaskTitles(next);
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
      st.selectedIds = st.accounts
        .filter((a) => !st.selectedIds.includes(a.id))
        .map((a) => a.id);
    },

    selectRunning() {
      st.selectedIds = st.accounts.filter((a) => a.running).map((a) => a.id);
      if (!st.selectedIds.length) toast("没有正在运行的账号");
    },

    clearSelect() {
      st.selectedIds = [];
    },

    bulkLaunch() {
      const ids = st.accounts
        .filter((a) => st.selectedIds.includes(a.id) && !a.running)
        .map((a) => a.id);
      if (!ids.length) return toast("选中的账号都在运行中");
      store.multiLaunch(ids);
    },

    bulkStop() {
      const ids = st.accounts
        .filter((a) => st.selectedIds.includes(a.id) && a.running)
        .map((a) => a.id);
      if (!ids.length) return toast("选中的账号没有在运行的");
      void Promise.all(ids.map((id) => api.stopAccount(id))).then((rs) => {
        const fail = rs.filter((r) => r.ok === false).length;
        toast(
          fail
            ? `已停止 ${rs.length - fail} 个，${fail} 个失败`
            : `已停止 ${rs.length} 个客户端`,
        );
      });
    },

    bulkRemove() {
      const picked = st.accounts.filter((a) => st.selectedIds.includes(a.id));
      if (!picked.length) return;
      const names = picked.map((a) => a.name);
      void Promise.all(picked.map((a) => api.deleteAccount(a.id))).then(
        (rs) => {
          const fail = rs.filter((r) => r.ok === false).length;
          if (!fail) st.selectedIds = [];
          toast(
            fail
              ? `删除了 ${rs.length - fail} 个，${fail} 个失败`
              : `已删除 ${names.length} 个账号`,
          );
        },
      );
    },

    multiLaunch(ids, layout, size, display, ovMode, ovFps) {
      const ready = ids.filter((id) => !find(id)?.running);
      if (!ready.length) return toast("所选账号都在运行中");

      void (async () => {
        try {
          const plan = await store.planLayout({
            count: ready.length,
            mode: display || "",
          });
          const r = await api.launchMulti(
            ready,
            undefined,
            undefined,
            plan.mode,
            ovMode,
            ovFps,
          );
          if (r.ok === false) return toast(r.message || "多开失败");
          toast(
            r.message ||
              `已启动 ${ready.length} 个客户端，${layoutLabel(plan.mode)}${ready.length >= 3 ? "；可用监控墙切换" : ""}`,
          );
        } catch (e) {
          toast(e instanceof Error ? e.message : "窗口规划失败，请重试");
        }
      })();
    },

    multiDefaults() {
      return {
        qualityMode: state.settings.multiQualityMode,
        fps: state.settings.multiQualityFps,
        arbiter: state.settings.mouseArbiter,
        arbiterMin: state.settings.mouseArbiterMin,
        autoEnter: state.settings.multiAutoEnter,
      };
    },

    saveMultiDefaults(qualityMode, fps, arbiter, arbiterMin, autoEnter) {
      void api
        .saveSettings({
          multiQualityMode: qualityMode,
          multiQualityFps: fps,
          mouseArbiter: arbiter,
          mouseArbiterMin: arbiterMin,
          multiAutoEnter: autoEnter,
        })
        .then((r) => {
          if (r.ok === false) toast(r.message || "保存失败");
        });
    },

    openSettings() {
      hooks.onOpenSettings?.();
    },

    toggleWall() {
      void api
        .wallToggle()
        .then((r) =>
          toast(
            r.message || (r.ok === false ? "监控墙开关失败" : "监控墙已更新"),
          ),
        );
    },

    openRunning() {
      // 「运行」= 启动组：一套可重复执行的启动配方（账号组合 + 位置 + 画质 + 启动行为）
      demoUi.openGroups();
    },

    toast,
  };
  return store;
}
