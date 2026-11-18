<script lang="ts">
// 切回账号页时保留编辑副本；只缓存草稿，已保存数据仍以后端为准。
const draftSessions = new WeakMap<
  object,
  { selected: string; drafts: Map<string, { value: string; baseline: string }> }
>();
</script>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import {
  Play,
  Plus,
  Save,
  Trash2,
  Copy,
  X,
  Search,
  Crosshair,
  Grid2x2,
  ChevronUp,
  ChevronDown,
  Settings2,
  Monitor,
} from "lucide-vue-next";
import type {
  Account,
  LaunchGroup,
  LaunchSlot,
  LauncherStore,
} from "../shellkit/contract";
import {
  channelLabel,
  canonicalLayout,
  layoutLabel,
} from "../shellkit/contract";
import { useLayoutPlan } from "../composables/useLayoutPlan";
import { maskName } from "../composables/useMask";

const props = defineProps<{ store: LauncherStore; embedded?: boolean }>();
const emit = defineEmits<{ close: [] }>();
const store = props.store;
const session = draftSessions.get(store) ?? { selected: "", drafts: new Map() };
draftSessions.set(store, session);
const clone = <T,>(v: T): T => JSON.parse(JSON.stringify(v));
const q = ref("");
const gid = ref(session.selected);
const draft = ref<LaunchGroup | null>(null);
const baseline = ref("");
const slotKey = ref("");
const showSettings = ref(false);
const showAdd = ref(false);
const addQuery = ref("");
const addTag = ref("");
const note = ref("");
const error = ref("");
const layoutNotice = ref("");
const busy = ref<"" | "save" | "launch" | "capture" | "create" | "delete">("");
const pending = ref<
  | null
  | { kind: "switch"; id: string }
  | { kind: "new" }
  | { kind: "close" }
  | { kind: "delete" }
>(null);
const physical = ref<{
  w: number;
  h: number;
  workArea?: { x: number; y: number; w: number; h: number };
}>({ w: 0, h: 0 });
const dirty = computed(
  () => !!draft.value && JSON.stringify(draft.value) !== baseline.value,
);
const list = computed(() =>
  store.groups.filter(
    (g) => !q.value || g.name.toLowerCase().includes(q.value.toLowerCase()),
  ),
);
const slots = computed(() => draft.value?.slots ?? []);
const accOf = (key: string) => store.accounts.find((a) => a.id === key);
const accountName = (a: Account) => (store.maskAll ? a.maskedName : a.name);
const slotName = (s: LaunchSlot) => {
  const a = accOf(s.accountKey);
  return a ? accountName(a) : store.maskAll ? "已删除的账号" : s.name;
};
// 后端错误也可能包含账号名；只转换展示文本，不修改草稿或真实账号。
const privateText = (text: string) => {
  if (!store.maskAll) return text;
  for (const a of [...store.accounts].sort(
    (a, b) => b.name.length - a.name.length,
  ))
    if (a.name) text = text.split(a.name).join(a.maskedName);
  for (const s of slots.value)
    if (!accOf(s.accountKey) && s.name)
      text = text.split(s.name).join("已删除的账号");
  return maskName(text);
};
const isMissing = (s: LaunchSlot) => !accOf(s.accountKey);
const enabled = computed(() => slots.value.filter((s) => !s.disabled));
const validEnabled = computed(() => enabled.value.filter((s) => !isMissing(s)));
const issues = computed(() =>
  enabled.value.filter(
    (s) =>
      isMissing(s) ||
      accOf(s.accountKey)?.cred === "expired" ||
      accOf(s.accountKey)?.regionBlocked,
  ),
);
const finalDisplay = (s: LaunchSlot) =>
  s.display ||
  draft.value?.defDisplay ||
  (draft.value?.layout === "fullscreen"
    ? "fullscreen"
    : accOf(s.accountKey)?.displayMode || "windowed");
const windows = computed(() =>
  validEnabled.value.filter((s) => finalDisplay(s) !== "fullscreen"),
);
const showWindowPositions = ref(false);
const showPositionControls = computed(
  () => draft.value?.layout !== "fullscreen" || (windows.value.length > 0 && showWindowPositions.value),
);
const layoutRequest = computed(() => ({
  count: windows.value.length,
  mode:
    draft.value?.layout === "fullscreen" && windows.value.length
      ? ""
      : draft.value?.layout || "",
}));
const planner = useLayoutPlan(store, layoutRequest);
const layoutPlan = planner.result;
const planLoading = planner.loading;
const planError = planner.error;
// 全屏组仍需按全部成员评估可切换的窗口方案，不能拿空窗口子集禁用菜单。
const optionPlanner = useLayoutPlan(
  store,
  computed(() => ({
    count:
      draft.value?.layout === "fullscreen"
        ? validEnabled.value.length
        : windows.value.length,
    mode: draft.value?.layout || "",
  })),
);
const modeOptions = computed(
  () => optionPlanner.result.value?.options ?? layoutPlan.value?.options ?? [],
);
// 全屏是整组的显示选择；显式窗口成员的子集规划不能覆盖它的选中态。
const effectiveMode = computed(() =>
  draft.value?.layout === "fullscreen"
    ? "fullscreen"
    : layoutPlan.value?.mode ?? canonicalLayout(draft.value?.layout || "tile"),
);
watch(layoutPlan, (p) => {
  if (p) physical.value = { w: p.screenW, h: p.screenH, workArea: p.workArea };
});
function chooseLayout(mode: string) {
  if (!draft.value || busy.value) return;
  draft.value.layout = canonicalLayout(mode);
  showWindowPositions.value = false;
  draft.value.defDisplay = mode === "fullscreen" ? "fullscreen" : "windowed";
  layoutNotice.value = "排列方案已更改，保存后生效；已记住的手动位置保持不变。";
}
const curSlot = computed(
  () =>
    slots.value.find((s) => s.accountKey === slotKey.value) ??
    slots.value[0] ??
    null,
);
const manual = computed(() => windows.value.some((s) => s.hasRect));
const savedRectsUsable = computed(
  () =>
    manual.value &&
    windows.value.every((s) => s.hasRect && s.w > 0 && s.h > 0) &&
    !!draft.value?.screenW &&
    (!physical.value.w ||
      (draft.value.screenW === physical.value.w &&
        draft.value.screenH === physical.value.h)),
);
const W = computed(() =>
  savedRectsUsable.value
    ? draft.value!.screenW
    : layoutPlan.value?.screenW || physical.value.w || 1920,
);
const H = computed(() =>
  savedRectsUsable.value
    ? draft.value!.screenH
    : layoutPlan.value?.screenH || physical.value.h || 1080,
);
const launchCapacityBad = computed(() => false);
const incomplete = computed(
  () =>
    manual.value &&
    windows.value.some((s) => !s.hasRect || s.w <= 0 || s.h <= 0),
);
const screenChanged = computed(
  () =>
    manual.value &&
    !!physical.value.w &&
    (draft.value?.screenW !== physical.value.w ||
      draft.value?.screenH !== physical.value.h),
);
const posWarning = computed(() =>
  screenChanged.value
    ? `屏幕尺寸与记录时不同，启动时将按“${layoutLabel(effectiveMode.value)}”重新排列。`
    : incomplete.value
      ? `部分启用窗口未记录位置，启动时将按“${layoutLabel(effectiveMode.value)}”重新排列。`
      : "",
);
const available = computed(() =>
  store.accounts.filter((a) => !slots.value.some((s) => s.accountKey === a.id)),
);
const tagPicks = computed(() => [
  ...new Set(available.value.flatMap((a) => a.tags)),
]);
const filteredAvailable = computed(() =>
  available.value.filter(
    (a) =>
      (!addQuery.value ||
        `${accountName(a)} ${channelLabel(a.channel)} ${a.tags.join(" ")}`
          .toLowerCase()
          .includes(addQuery.value.toLowerCase())) &&
      (!addTag.value || a.tags.includes(addTag.value)),
  ),
);
const QUALITIES = [
  { v: "", label: "跟随账号" },
  { v: "LowPower", label: "低功耗" },
  { v: "Default", label: "默认" },
  { v: "HighQuality", label: "高画质" },
  { v: "Presentation", label: "极致" },
];
const DISPLAYS = [
  { v: "", label: "跟随账号" },
  { v: "windowed", label: "窗口" },
  { v: "fullscreen", label: "全屏" },
];
const qualityLabel = (v: string) =>
  QUALITIES.find((x) => x.v === v)?.label || "跟随账号";

function remember() {
  session.selected = gid.value;
  if (draft.value && dirty.value)
    session.drafts.set(gid.value, {
      value: JSON.stringify(draft.value),
      baseline: baseline.value,
    });
  else if (draft.value) session.drafts.delete(gid.value);
}
function loadGroup(id: string) {
  showWindowPositions.value = false;
  remember();
  gid.value = id;
  const saved = store.groups.find((g) => g.id === id);
  const cached = session.drafts.get(id);
  layoutNotice.value = "";
  draft.value = cached ? JSON.parse(cached.value) : saved ? clone(saved) : null;
  baseline.value = cached?.baseline ?? JSON.stringify(saved ?? null);
  slotKey.value = draft.value?.slots[0]?.accountKey ?? "";
  note.value = cached ? "已恢复未保存的修改" : "";
  error.value = "";
  showAdd.value = false;
  session.selected = id;
}
function requestAction(action: NonNullable<typeof pending.value>) {
  if (busy.value) return;
  if (action.kind === "delete" || dirty.value) pending.value = action;
  else void perform(action);
}
async function perform(action: NonNullable<typeof pending.value>) {
  pending.value = null;
  if (action.kind === "switch") loadGroup(action.id);
  else if (action.kind === "new") await createGroup();
  else if (action.kind === "close") emit("close");
  else await removeGroup();
}
async function resolvePending(save: boolean) {
  const action = pending.value;
  if (!action || busy.value) return;
  if (save && !(await commit())) return;
  if (!save && draft.value) {
    session.drafts.delete(gid.value);
    const saved = store.groups.find((g) => g.id === gid.value);
    draft.value = saved ? clone(saved) : null;
    baseline.value = JSON.stringify(draft.value);
  }
  await perform(action);
}
watch(
  () => store.groups,
  () => {
    if (busy.value || dirty.value) return;
    const id = store.groups.some((g) => g.id === gid.value)
      ? gid.value
      : (store.groups[0]?.id ?? "");
    // 轮询拿到相同配置时保留成员选择、添加面板和操作反馈。
    const saved = store.groups.find((g) => g.id === id);
    if (
      draft.value &&
      id === gid.value &&
      JSON.stringify(saved) === baseline.value
    )
      return;
    loadGroup(id);
  },
  { immediate: true, deep: true },
);
watch([draft, baseline], remember, { deep: true });
function beforeUnload(e: BeforeUnloadEvent) {
  if (dirty.value || session.drafts.size) {
    e.preventDefault();
    e.returnValue = "";
  }
}
onMounted(() => {
  window.addEventListener("beforeunload", beforeUnload);
  void store
    .captureRects()
    .then((r) => {
      physical.value = { w: r.screenW, h: r.screenH, workArea: r.workArea };
    })
    .catch(() => {});
});
onBeforeUnmount(() => {
  remember();
  window.removeEventListener("beforeunload", beforeUnload);
  onBlockUp();
});
function failure(e: unknown) {
  error.value = e instanceof Error ? e.message : "操作未完成，请重试";
}
function validate() {
  if (!draft.value?.name.trim()) throw new Error("请先给这套启动组起个名字");
  for (const s of slots.value) {
    if (
      s.hasRect &&
      (![s.x, s.y, s.w, s.h].every(Number.isFinite) || s.w <= 0 || s.h <= 0)
    )
      throw new Error(`「${slotName(s)}」的窗口宽高必须大于 0`);
  }
}
async function saveDraft() {
  validate();
  const plan = await planner.ensure();
  if (draft.value && draft.value.layout !== "fullscreen")
    draft.value.layout = plan.mode;
  const value = clone(draft.value!);
  value.name = value.name.trim();
  value.gapSec = 0;
  const saved = await store.saveGroup(value);
  session.drafts.delete(gid.value);
  baseline.value = JSON.stringify(saved);
  gid.value = saved.id;
  draft.value = clone(saved);
  session.selected = saved.id;
  return saved;
}
async function commit() {
  if (!draft.value || busy.value) return false;
  busy.value = "save";
  error.value = "";
  note.value = "";
  try {
    await saveDraft();
    note.value = "已保存";
    return true;
  } catch (e) {
    failure(e);
    return false;
  } finally {
    busy.value = "";
  }
}
async function createGroup() {
  busy.value = "create";
  error.value = "";
  try {
    const saved = await store.saveGroup({
      id: "",
      name: `新启动组 ${store.groups.length + 1}`,
      layout: "tile",
      cols: 2,
      rows: 2,
      defMode: "",
      defFps: 0,
      defDisplay: "",
      stopFirst: true,
      autoWall: true,
      gapSec: 0,
      screenW: 0,
      screenH: 0,
      slots: [],
    });
    q.value = "";
    loadGroup(saved.id);
    showSettings.value = true;
    showAdd.value = true;
  } catch (e) {
    failure(e);
  } finally {
    busy.value = "";
  }
}
async function copyGroup() {
  if (!draft.value || busy.value) return;
  busy.value = "create";
  error.value = "";
  try {
    validate();
    const saved = await store.saveGroup({
      ...clone(draft.value),
      id: "",
      name: `${draft.value.name} 副本`,
      gapSec: 0,
    });
    q.value = "";
    loadGroup(saved.id);
    note.value = "已复制当前配置，原组草稿已保留";
  } catch (e) {
    failure(e);
  } finally {
    busy.value = "";
  }
}
async function removeGroup() {
  if (!draft.value) return;
  busy.value = "delete";
  error.value = "";
  try {
    await store.deleteGroup(draft.value.id);
    session.drafts.delete(gid.value);
    draft.value = null;
    baseline.value = "null";
    loadGroup(store.groups[0]?.id ?? "");
  } catch (e) {
    failure(e);
  } finally {
    busy.value = "";
  }
}
async function launch() {
  if (!draft.value || busy.value || !validEnabled.value.length) return;
  busy.value = "launch";
  error.value = "";
  note.value = "正在保存并检查账号…";
  try {
    const saved = await saveDraft();
    note.value = "正在检查账号并启动，请稍候…";
    const result = await store.launchGroup(saved.id);
    if (!result.ok) throw new Error(result.message || "启动失败");
    note.value = result.message || "启动请求已完成";
  } catch (e) {
    note.value = "";
    failure(e);
  } finally {
    busy.value = "";
  }
}
function addSlot(a: Account) {
  if (!draft.value || slots.value.some((s) => s.accountKey === a.id)) return;
  draft.value.slots.push({
    accountKey: a.id,
    name: a.name,
    mode: "",
    fps: 0,
    display: "",
    disabled: false,
    x: 0,
    y: 0,
    w: 0,
    h: 0,
    hasRect: false,
    zOrder: slots.value.length + 1,
  });
  slotKey.value = a.id;
}
function addFiltered() {
  for (const a of [...filteredAvailable.value]) addSlot(a);
}
function removeSlot(i: number) {
  draft.value?.slots.splice(i, 1);
}
function move(i: number, dir: number) {
  const target = i + dir;
  if (!draft.value || target < 0 || target >= slots.value.length) return;
  draft.value.slots.splice(target, 0, draft.value.slots.splice(i, 1)[0]);
}
function setAuto() {
  for (const s of slots.value) s.hasRect = false;
  note.value = "已改为自动排列，保存后生效";
}
function autoRect(s: LaunchSlot) {
  const index = windows.value.indexOf(s);
  return (
    layoutPlan.value?.rects[index] ?? {
      x: 0,
      y: 0,
      w: 0,
      h: 0,
      zOrder: index + 1,
    }
  );
}
function rect(s: LaunchSlot) {
  return savedRectsUsable.value && s.hasRect
    ? { x: s.x, y: s.y, w: s.w, h: s.h }
    : autoRect(s);
}
const orderedLayers = computed(() =>
  slots.value
    .map((s, i) => ({ s, i, z: s.zOrder && s.zOrder > 0 ? s.zOrder : i + 1 }))
    .sort((a, b) => a.z - b.z || a.i - b.i)
    .map((x) => x.s),
);
const layerRank = (s: LaunchSlot) => orderedLayers.value.indexOf(s) + 1;
const layerText = (s: LaunchSlot) =>
  `层级 ${layerRank(s)}${layerRank(s) === slots.value.length ? " · 最前" : layerRank(s) === 1 ? " · 最后" : ""}`;
function blockStyle(s: LaunchSlot) {
  const r = rect(s);
  return {
    left: `${(r.x / W.value) * 100}%`,
    top: `${(r.y / H.value) * 100}%`,
    width: `${(r.w / W.value) * 100}%`,
    height: `${(r.h / H.value) * 100}%`,
    zIndex: layerRank(s),
    visibility:
      r.w > 0 && (!planLoading.value || s.hasRect)
        ? ("visible" as const)
        : ("hidden" as const),
  };
}
function materializePlan(
  plan: NonNullable<typeof layoutPlan.value>,
  replace = false,
) {
  if (!draft.value) return;
  const changed =
    draft.value.screenW !== plan.screenW ||
    draft.value.screenH !== plan.screenH;
  if (changed) for (const s of slots.value) s.hasRect = false;
  draft.value.screenW = plan.screenW;
  draft.value.screenH = plan.screenH;
  for (const [i, member] of windows.value.entries()) {
    const target = plan.rects[i];
    if (!target || target.w <= 0 || target.h <= 0)
      throw new Error("规划没有返回完整窗口位置，请重试");
    if (replace || !member.hasRect)
      Object.assign(member, target, {
        zOrder: member.zOrder || target.zOrder || i + 1,
        hasRect: true,
      });
  }
}
async function setManual() {
  if (busy.value || !windows.value.length) return;
  busy.value = "capture";
  error.value = "";
  try {
    const plan = await planner.ensure();
    materializePlan(plan);
    note.value =
      "已采用当前方案的实际尺寸和位置，可拖动微调；显示层级会一并保存。";
  } catch (e) {
    failure(e);
  } finally {
    busy.value = "";
  }
}
async function fillGrid() {
  if (!draft.value || busy.value || !windows.value.length) return;
  busy.value = "capture";
  error.value = "";
  try {
    const plan = await planner.ensure();
    materializePlan(plan, true);
    if (draft.value.layout !== "fullscreen") draft.value.layout = plan.mode;
    note.value = `已按“${layoutLabel(plan.mode)}”重新生成位置，记得保存。`;
  } catch (e) {
    failure(e);
  } finally {
    busy.value = "";
  }
}
async function setLayer(front: boolean) {
  if (!curSlot.value || busy.value) return;
  const selected = curSlot.value;
  if (windows.value.length && !manual.value) await setManual();
  if (error.value) return;
  const ordered = orderedLayers.value.filter((s) => s !== selected);
  front ? ordered.push(selected) : ordered.unshift(selected);
  ordered.forEach((s, i) => (s.zOrder = i + 1));
  note.value = "显示层级已调整，启动顺序不变；保存后还原。";
}
async function capture() {
  if (!draft.value || busy.value) return;
  busy.value = "capture";
  error.value = "";
  try {
    const r = await store.captureRects();
    const matched = r.rects.filter((x) =>
      windows.value.some((s) => s.accountKey === x.id),
    );
    if (!matched.length)
      throw new Error(
        "没有可记录的启用窗口。请先运行组内账号，摆好窗口后再试。",
      );
    if (!r.screenW || !r.screenH)
      throw new Error("未读到屏幕尺寸，本次没有修改位置");
    if (draft.value.screenW !== r.screenW || draft.value.screenH !== r.screenH)
      for (const s of slots.value) s.hasRect = false;
    draft.value.screenW = r.screenW;
    draft.value.screenH = r.screenH;
    physical.value = { w: r.screenW, h: r.screenH, workArea: r.workArea };
    for (const x of matched)
      Object.assign(slots.value.find((s) => s.accountKey === x.id)!, {
        x: x.x,
        y: x.y,
        w: x.w,
        h: x.h,
        hasRect: !x.fullscreen,
        zOrder: x.zOrder,
      });
    note.value = `已记录 ${matched.length} 个窗口，记得保存${matched.some((x) => x.fullscreen) ? "；全屏窗口记录为整屏大小" : ""}`;
  } catch (e) {
    failure(e);
  } finally {
    busy.value = "";
  }
}
function updateCoord(s: LaunchSlot, key: "x" | "y" | "w" | "h", e: Event) {
  const value = Number((e.target as HTMLInputElement).value);
  if (!Number.isFinite(value)) return;
  s[key] =
    key === "w" || key === "h"
      ? Math.max(
          key === "w"
            ? layoutPlan.value?.minW || 1024
            : layoutPlan.value?.minH || 720,
          Math.round(value),
        )
      : Math.round(value);
  s.hasRect = true;
}
const dragInfo = ref<{
  key: string;
  startX: number;
  startY: number;
  x: number;
  y: number;
  width: number;
  height: number;
} | null>(null);
async function onBlockDown(e: PointerEvent, s: LaunchSlot) {
  if (busy.value || e.button !== 0) return;
  slotKey.value = s.accountKey;
  if (!draft.value) return;
  if (planLoading.value || !layoutPlan.value) {
    await setManual();
    return;
  }
  const box = (
    e.currentTarget as HTMLElement
  ).parentElement!.getBoundingClientRect();
  materializePlan(layoutPlan.value);
  dragInfo.value = {
    key: s.accountKey,
    startX: e.clientX,
    startY: e.clientY,
    x: s.x,
    y: s.y,
    width: box.width,
    height: box.height,
  };
  window.addEventListener("pointermove", onBlockMove);
  window.addEventListener("pointerup", onBlockUp, { once: true });
  window.addEventListener("pointercancel", onBlockUp, { once: true });
}
function onBlockMove(e: PointerEvent) {
  const d = dragInfo.value;
  const s = slots.value.find((s) => s.accountKey === d?.key);
  if (!d || !s) return;
  s.x = Math.max(
    0,
    Math.min(
      Math.max(0, W.value - s.w),
      Math.round(d.x + ((e.clientX - d.startX) / d.width) * W.value),
    ),
  );
  s.y = Math.max(
    0,
    Math.min(
      Math.max(0, H.value - s.h),
      Math.round(d.y + ((e.clientY - d.startY) / d.height) * H.value),
    ),
  );
}
function onBlockUp() {
  window.removeEventListener("pointermove", onBlockMove);
  window.removeEventListener("pointerup", onBlockUp);
  window.removeEventListener("pointercancel", onBlockUp);
  dragInfo.value = null;
}
</script>

<template>
  <div class="sheet" :class="{ embedded }" :aria-busy="!!busy">
    <header class="top">
      <Monitor />
      <h2>启动组</h2>
      <span>保存常用组合，下次一键启动</span
      ><button
        v-if="!embedded"
        class="icon close"
        aria-label="关闭"
        @click="requestAction({ kind: 'close' })"
      >
        <X />
      </button>
    </header>
    <div class="body">
      <aside class="nav">
        <div class="nsearch">
          <Search /><input
            v-model="q"
            aria-label="搜索启动组"
            placeholder="搜索启动组"
          />
        </div>
        <div class="nlist">
          <button
            v-for="g in list"
            :key="g.id"
            class="nitem"
            :class="{ on: gid === g.id }"
            :disabled="!!busy"
            @click="g.id !== gid && requestAction({ kind: 'switch', id: g.id })"
          >
            <b>{{ g.name }}</b
            ><span
              >{{ g.slots.filter((s) => !s.disabled).length }} /
              {{ g.slots.length }} 个启用 · {{ layoutLabel(g.layout)
              }}<em v-if="session.drafts.has(g.id) || (g.id === gid && dirty)">
                · 未保存</em
              ></span
            >
          </button>
          <p v-if="!list.length" class="muted navempty">
            {{ q ? "没有匹配的组" : "还没有启动组" }}
          </p>
        </div>
        <button
          class="newgroup"
          :disabled="!!busy"
          @click="requestAction({ kind: 'new' })"
        >
          <Plus />新建启动组
        </button>
        <p class="navhint">切回账号页时，未保存的修改会暂时保留。</p>
      </aside>
      <section v-if="draft" class="detail">
        <div class="dhead">
          <input
            v-model="draft.name"
            class="namein"
            aria-label="启动组名称"
            :disabled="!!busy"
          /><span class="save-state">{{ dirty ? "未保存" : "已保存" }}</span>
          <div class="dacts">
            <button
              class="icon"
              title="复制当前配置"
              aria-label="复制当前配置"
              :disabled="!!busy"
              @click="copyGroup"
            >
              <Copy /></button
            ><button
              class="icon"
              title="删除启动组"
              aria-label="删除启动组"
              :disabled="!!busy"
              @click="requestAction({ kind: 'delete' })"
            >
              <Trash2 /></button
            ><button class="btn" :disabled="!!busy || !dirty" @click="commit">
              <Save />{{ busy === "save" ? "保存中…" : "保存" }}</button
            ><button
              class="btn primary"
              :disabled="!!busy || !validEnabled.length || launchCapacityBad"
              @click="launch"
            >
              <Play />{{
                busy === "launch"
                  ? "启动中…"
                  : `启动 ${validEnabled.length} 个账号`
              }}
            </button>
          </div>
        </div>
        <div class="overview">
          <strong
            >{{ enabled.length }} 个启用<span>
              / {{ slots.length }} 个成员</span
            ></strong
          ><span v-if="issues.length" class="warning"
            >{{ issues.length }} 个账号需检查</span
          ><button class="textbtn" @click="showSettings = !showSettings">
            <Settings2 />组设置<ChevronDown
              :class="{ open: showSettings }"
            /></button
          ><span class="summary"
            >{{ layoutLabel(draft.layout) }} · {{ qualityLabel(draft.defMode)
            }}{{ draft.defFps ? ` · ${draft.defFps} 帧` : "" }}</span
          >
        </div>
        <p class="launchhint">
          {{
            draft.stopFirst
              ? "启动前会停掉所有现有游戏实例（包括组外账号），再启动这组。"
              : "未开启先停后启：有其他游戏实例运行时，需要先停止它们。"
          }}
        </p>
        <div
          v-if="error || note"
          class="feedback"
          :class="{ warning: error }"
          role="status"
        >
          {{ privateText(error || note) }}
        </div>
        <p v-if="layoutNotice" class="warning layout-notice" role="status">
          {{ layoutNotice }}
        </p>
        <div class="detail-scroll">
          <fieldset :disabled="!!busy" class="editor">
            <div v-if="showSettings" class="settings">
              <label
                >排列方案<select
                  :value="canonicalLayout(draft.layout)"
                  @change="
                    chooseLayout(($event.target as HTMLSelectElement).value)
                  "
                >
                  <option
                    v-for="option in modeOptions"
                    :key="option.id"
                    :value="option.id"
                    :disabled="!option.enabled"
                  >
                    {{ option.label
                    }}{{ !option.enabled ? "（当前放不下）" : "" }}
                  </option>
                </select></label
              >
              <label
                >默认画质<select v-model="draft.defMode">
                  <option v-for="x in QUALITIES" :key="x.v" :value="x.v">
                    {{ x.label }}
                  </option>
                </select></label
              >
              <label
                >默认帧率<select v-model.number="draft.defFps">
                  <option v-for="n in [0, 30, 60, 120]" :key="n" :value="n">
                    {{ n ? `${n} 帧` : "跟随账号" }}
                  </option>
                </select></label
              >
              <label
                >默认显示<select v-model="draft.defDisplay">
                  <option v-for="x in DISPLAYS" :key="x.v" :value="x.v">
                    {{ x.label }}
                  </option>
                </select></label
              >
              <div class="checks">
                <label
                  ><input
                    v-model="draft.stopFirst"
                    type="checkbox"
                  />启动前停止全部实例</label
                ><label
                  ><input
                    v-model="draft.autoWall"
                    type="checkbox"
                  />启动后打开监控墙</label
                >
              </div>
              <p class="settinghint">
                成员单独设置优先；选择“跟随组”时使用组默认，再跟随账号设置。多开账号会同时启动。
              </p>
            </div>
            <section class="position">
              <div class="layout-options" aria-label="窗口排列方案">
                <button
                  v-for="option in modeOptions"
                  :key="option.id"
                  type="button"
                  :class="{ active: effectiveMode === option.id }"
                  :aria-pressed="effectiveMode === option.id"
                  :disabled="!option.enabled || !!busy"
                  :title="option.reason || option.description"
                  @click="chooseLayout(option.id)"
                >
                  <b>{{ option.label }}</b
                  ><span>{{ option.description }}</span
                  ><small v-if="!option.enabled">{{ option.reason }}</small
                  ><small v-else-if="layoutPlan?.recommendedMode === option.id"
                    >按当前数量推荐</small
                  >
                </button>
              </div>
              <template v-if="showPositionControls">
              <p v-if="layoutPlan" class="screenhint plan-environment">
                可用桌面 {{ layoutPlan.workArea.w }}×{{
                  layoutPlan.workArea.h
                }}
                · 最小窗口 {{ layoutPlan.minW }}×{{ layoutPlan.minH }} ·
                {{
                  windows.length === 1
                    ? "单窗口居中"
                    : "按实际窗口尺寸安排，不缩成假平铺"
                }}
              </p>
              <p v-if="planLoading" class="screenhint" role="status">
                正在更新窗口规划…
              </p>
              <p v-else-if="planError" class="warning" role="alert">
                {{ planError }}
                <button class="textbtn" @click="planner.retry">重试</button>
              </p>
              <p v-else-if="windows.length && layoutPlan?.reason" class="warning plan-reason">
                {{ layoutPlan.reason }}
              </p>
              <p v-if="validEnabled.length >= 5" class="screenhint">
                账号较多时配合监控墙切换；当前模式优先露标题或画面。
              </p>
              <div class="sectionhead">
                <h3>{{ draft.layout === "fullscreen" ? "窗口成员的位置" : "窗口位置" }}</h3>
                <button v-if="draft.layout === 'fullscreen'" class="textbtn" @click="showWindowPositions = false">收起位置设置</button>
                <div class="position-mode">
                  <button
                    type="button"
                    :class="{ active: !manual }"
                    @click="setAuto"
                  >
                    自动排列</button
                  ><button
                    type="button"
                    :class="{ active: manual }"
                    :disabled="!windows.length || !!planError"
                    @click="setManual"
                  >
                    手动位置
                  </button>
                </div>
                <span class="muted"
                  >{{ windows.length }} 个窗口<span
                    v-if="validEnabled.length - windows.length"
                  >
                    ·
                    {{ validEnabled.length - windows.length }}
                    个全屏不参与排列</span
                  ></span
                >
              </div>
              <div class="layoutbar">
                <div class="canvas" :style="{ aspectRatio: `${W}/${H}` }">
                  <button
                    v-for="s in windows"
                    :key="s.accountKey"
                    class="wblock"
                    :class="{ on: curSlot?.accountKey === s.accountKey }"
                    :style="blockStyle(s)"
                    :title="`${slotName(s)} · ${layerText(s)}`"
                    :aria-label="`调整${slotName(s)}的位置`"
                    @pointerdown="onBlockDown($event, s)"
                    @click="slotKey = s.accountKey"
                  >
                    <b
                      >{{ slots.indexOf(s) + 1 }}
                      <small>{{
                        layerRank(s) === slots.length ? "前" : ""
                      }}</small></b
                    >
                  </button>
                  <p v-if="!windows.length" class="cempty">
                    {{
                      draft.layout === "fullscreen"
                        ? "专注操作一个账号，其他账号在后台；用监控墙切换。"
                        : "暂无启用的窗口成员"
                    }}
                  </p>
                </div>
                <div class="position-info">
                  <p class="muted">
                    {{
                      manual
                        ? "拖动画布或输入坐标微调；禁用成员不占位置。"
                        : "按启用窗口排列；切到手动位置可拖动微调。"
                    }}
                  </p>
                  <div
                    v-if="curSlot && windows.includes(curSlot)"
                    class="coords"
                  >
                    <span class="coordname">{{ slotName(curSlot) }}</span
                    ><label
                      v-for="key in ['x', 'y', 'w', 'h'] as const"
                      :key="key"
                      >{{ { x: "X", y: "Y", w: "宽", h: "高" }[key]
                      }}<input
                        type="number"
                        :value="rect(curSlot)[key]"
                        :min="
                          key === 'w'
                            ? layoutPlan?.minW
                            : key === 'h'
                              ? layoutPlan?.minH
                              : undefined
                        "
                        :disabled="!savedRectsUsable || !curSlot.hasRect"
                        @input="updateCoord(curSlot, key, $event)"
                    /></label>
                  </div>
                  <div v-if="curSlot" class="layer-actions">
                    <span
                      >{{ slotName(curSlot) }} · {{ layerText(curSlot) }}</span
                    ><button class="btn small" @click="setLayer(true)">
                      移到最前</button
                    ><button class="btn small" @click="setLayer(false)">
                      移到最后
                    </button>
                  </div>
                  <div class="position-actions">
                    <button
                      class="btn small"
                      :disabled="!windows.length || !!planError"
                      @click="fillGrid"
                    >
                      <Grid2x2 />按当前方案重排</button
                    ><button class="btn small" @click="capture">
                      <Crosshair />记住当前位置
                    </button>
                  </div>
                  <span class="screenhint">{{
                    draft.screenW
                      ? `记录屏幕 ${W}×${H}`
                      : physical.w
                        ? `当前屏幕 ${W}×${H} · 尚未记录位置`
                        : "屏幕尺寸尚未读取，画布仅作预览"
                  }}</span>
                </div>
              </div>
              <p v-if="posWarning" class="warning position-warning">
                {{ posWarning }}
              </p>
              </template>
              <div v-else class="fullscreen-summary">
                <p class="screenhint fullscreenhint">全屏成员无需设置窗口位置，通过监控墙或 Alt+Tab 切换。</p>
                <p v-if="windows.length" class="screenhint window-overrides">{{ windows.length }} 个成员单独设为窗口模式。<button class="textbtn" @click="showWindowPositions = true">设置这些窗口的位置</button></p>
                <div v-if="curSlot" class="layer-actions">
                  <span>{{ slotName(curSlot) }} · {{ layerText(curSlot) }}</span>
                  <button class="btn small" @click="setLayer(true)">移到最前</button>
                  <button class="btn small" @click="setLayer(false)">移到最后</button>
                </div>
              </div>
            </section>
            <section class="members">
              <div class="sectionhead">
                <h3>组内成员</h3>
                <span class="muted">按顺序记录成员，单独设置只对这组生效</span
                ><button class="btn small" @click="showAdd = !showAdd">
                  <Plus />添加成员
                </button>
              </div>
              <div v-if="showAdd" class="addpanel">
                <div class="addtools">
                  <input
                    v-model="addQuery"
                    aria-label="搜索可加入的账号"
                    placeholder="搜索账号、渠道或标签"
                  /><select v-model="addTag" aria-label="按标签筛选">
                    <option value="">全部标签</option>
                    <option v-for="t in tagPicks" :key="t" :value="t">
                      {{ t }}
                    </option></select
                  ><button
                    class="btn small"
                    :disabled="!filteredAvailable.length"
                    @click="addFiltered"
                  >
                    加入筛选结果（{{ filteredAvailable.length }}）</button
                  ><button
                    class="icon"
                    aria-label="收起添加成员"
                    @click="showAdd = false"
                  >
                    <X />
                  </button>
                </div>
                <div class="addlist">
                  <button
                    v-for="a in filteredAvailable"
                    :key="a.id"
                    class="addaccount"
                    @click="addSlot(a)"
                  >
                    <Plus /><span>{{ accountName(a) }}</span
                    ><small>{{ channelLabel(a.channel) }}</small>
                  </button>
                  <p v-if="!filteredAvailable.length" class="muted">
                    {{
                      available.length ? "没有匹配的账号" : "全部账号都已加入"
                    }}
                  </p>
                </div>
              </div>
              <div v-if="slots.length" class="member-table">
                <div class="thead">
                  <span>#</span><span>账号 / 渠道</span><span>{{ windows.length ? '位置' : '层级' }}</span
                  ><span>画质</span><span>帧率</span><span>显示</span
                  ><span>启用</span><span>顺序 / 移出</span>
                </div>
                <div
                  v-for="(s, i) in slots"
                  :key="s.accountKey"
                  class="trow"
                  :class="{
                    on: curSlot?.accountKey === s.accountKey,
                    off: s.disabled,
                  }"
                  @click="slotKey = s.accountKey"
                >
                  <span class="idx">{{ i + 1 }}</span>
                  <div class="account">
                    <b :title="slotName(s)">{{ slotName(s) }}</b
                    ><span
                      >{{
                        accOf(s.accountKey)
                          ? channelLabel(accOf(s.accountKey)!.channel)
                          : "账号已删除"
                      }}<em v-if="accOf(s.accountKey)?.cred === 'expired'">
                        · 凭证过期</em
                      ><em v-else-if="accOf(s.accountKey)?.regionBlocked">
                        · 需检查渠道</em
                      ></span
                    >
                  </div>
                  <span
                    class="pos"
                    :title="
                      finalDisplay(s) === 'fullscreen'
                        ? '全屏显示，不使用窗口坐标'
                        : s.hasRect
                          ? `${s.x}, ${s.y} · ${s.w}×${s.h}`
                          : '随排列方式计算'
                    "
                    >{{
                      finalDisplay(s) === "fullscreen"
                        ? "全屏"
                        : s.disabled
                          ? "本次跳过"
                          : s.hasRect
                            ? `${s.x},${s.y} · ${s.w}×${s.h}`
                            : "自动"
                    }}<small class="layer-hint">{{ layerText(s) }}</small></span
                  ><select
                    v-model="s.mode"
                    :aria-label="`${slotName(s)}画质`"
                    @click.stop
                  >
                    <option v-for="x in QUALITIES" :key="x.v" :value="x.v">
                      {{ x.v ? x.label : "跟随组" }}
                    </option></select
                  ><select
                    v-model.number="s.fps"
                    :aria-label="`${slotName(s)}帧率`"
                    @click.stop
                  >
                    <option v-for="n in [0, 30, 60, 120]" :key="n" :value="n">
                      {{ n || "跟随组" }}
                    </option></select
                  ><select
                    v-model="s.display"
                    :aria-label="`${slotName(s)}显示方式`"
                    @click.stop
                  >
                    <option v-for="x in DISPLAYS" :key="x.v" :value="x.v">
                      {{ x.v ? x.label : "跟随组" }}
                    </option></select
                  ><label
                    class="enabled"
                    :title="`${slotName(s)}自动进入：勾上就点`"
                    ><input
                      type="checkbox"
                      :checked="s.autoEnter !== false"
                      :aria-label="`${slotName(s)}自动进入`"
                      @click.stop
                      @change="
                        s.autoEnter = ($event.target as HTMLInputElement)
                          .checked
                      " /></label
                  ><label class="enabled"
                    ><input
                      :checked="!s.disabled"
                      type="checkbox"
                      :aria-label="`启用${slotName(s)}`"
                      @click.stop
                      @change="
                        s.disabled = !($event.target as HTMLInputElement)
                          .checked
                      "
                  /></label>
                  <div class="ops">
                    <button
                      class="icon"
                      title="上移"
                      aria-label="上移"
                      :disabled="i === 0"
                      @click.stop="move(i, -1)"
                    >
                      <ChevronUp /></button
                    ><button
                      class="icon"
                      title="下移"
                      aria-label="下移"
                      :disabled="i === slots.length - 1"
                      @click.stop="move(i, 1)"
                    >
                      <ChevronDown /></button
                    ><button
                      class="icon"
                      title="移出组"
                      aria-label="移出组"
                      @click.stop="removeSlot(i)"
                    >
                      <X />
                    </button>
                  </div>
                </div>
              </div>
              <div v-else class="member-empty">
                <p>还没有成员</p>
                <span>加入经常一起启动的账号，保存后即可反复使用。</span
                ><button class="btn" @click="showAdd = true">
                  <Plus />选择账号
                </button>
              </div>
            </section>
          </fieldset>
        </div>
        <footer class="footerhint">
          {{
            dirty
              ? "修改尚未保存。点击启动时会先保存，保存失败不会继续启动。"
              : "这套配置已保存，可直接启动；账号副本可作为独立成员加入。"
          }}
        </footer>
      </section>
      <section v-else class="empty">
        <Monitor />
        <h3>把常用账号存成启动组</h3>
        <p>选好成员、画质和窗口位置，下次一键启动。</p>
        <button
          class="btn primary"
          :disabled="!!busy"
          @click="requestAction({ kind: 'new' })"
        >
          <Plus />{{ busy === "create" ? "创建中…" : "新建启动组" }}
        </button>
        <p v-if="error" class="warning" role="alert">
          {{ privateText(error) }}
        </p>
      </section>
    </div>
    <div
      v-if="pending"
      class="dialog-backdrop"
      @click.self="!busy && (pending = null)"
    >
      <section
        class="dialog"
        role="dialog"
        aria-modal="true"
        :aria-label="pending.kind === 'delete' ? '删除启动组' : '未保存的修改'"
      >
        <h3>
          {{
            pending.kind === "delete"
              ? "删除这套启动组？"
              : "要保存当前修改吗？"
          }}
        </h3>
        <p>
          {{
            privateText(
              pending.kind === "delete"
                ? `将删除「${draft?.name}」及组内配置，账号本身会保留。此操作无法撤销。`
                : `「${draft?.name}」有未保存的修改。保存后再继续，或放弃这次修改。`,
            )
          }}
        </p>
        <p v-if="error" class="warning" role="alert">
          {{ privateText(error) }}
        </p>
        <div class="dialog-actions">
          <button class="btn" :disabled="!!busy" @click="pending = null">
            取消</button
          ><template v-if="pending.kind !== 'delete'"
            ><button
              class="btn"
              :disabled="!!busy"
              @click="resolvePending(false)"
            >
              放弃修改</button
            ><button
              class="btn primary"
              :disabled="!!busy"
              @click="resolvePending(true)"
            >
              {{ busy === "save" ? "保存中…" : "保存并继续" }}
            </button></template
          ><button
            v-else
            class="btn danger"
            :disabled="!!busy"
            @click="perform(pending)"
          >
            确认删除
          </button>
        </div>
      </section>
    </div>
  </div>
</template>

<style scoped>
.layout-options {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
  margin: 0 0 12px;
}
.layout-options button {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 12px;
  text-align: left;
  border: 1px solid var(--line2);
  border-radius: 6px;
  color: var(--dim);
  background: transparent;
}
.layout-options button.active {
  border-color: var(--acc);
  background: rgba(216, 180, 124, 0.06);
}
.layout-options b {
  color: var(--tx);
  font-size: 13px;
  font-weight: 600;
}
.layout-options span,
.layout-options small {
  font-size: 12px;
  line-height: 1.5;
}
.layout-options small {
  color: var(--faint);
}
.plan-environment,
.plan-reason {
  margin: 8px 0;
}
.fullscreen-summary { margin-top: 12px; }
.fullscreenhint { margin: 0 0 10px; }
.layer-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
  font-size: 12px;
  color: var(--dim);
}
.layer-hint {
  display: block;
  font-size: 11px;
  color: var(--faint);
}

.sheet {
  --s0: #09090a;
  --s1: #101012;
  --s2: #161619;
  --line: rgba(255, 255, 255, 0.08);
  --line2: rgba(255, 255, 255, 0.14);
  --tx: #f4f2ef;
  --dim: #a5a29c;
  --faint: #8b8781;
  --acc: #d8b47c;
  --bad: #d49788;
  position: fixed;
  inset: 0;
  z-index: 60;
  display: flex;
  flex-direction: column;
  background: var(--s0);
  color: var(--tx);
  font-size: 14px;
  line-height: 1.5;
  min-width: 0;
  min-height: 0;
}
.sheet.embedded {
  position: relative;
  inset: auto;
  flex: 1;
  height: 100%;
  background: transparent;
  z-index: auto;
}
.sheet * {
  box-sizing: border-box;
}
.sheet button,
.sheet input,
.sheet select {
  font: inherit;
}
.sheet button {
  cursor: pointer;
}
.sheet button:disabled {
  opacity: 0.42;
  cursor: default;
}
.sheet button:focus-visible,
.sheet input:focus-visible,
.sheet select:focus-visible {
  outline: 2px solid var(--acc);
  outline-offset: 2px;
}
.sheet svg {
  width: 16px;
  height: 16px;
  flex: none;
  stroke-width: 1.5;
}
.top {
  height: 52px;
  flex: none;
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 0 20px;
  border-bottom: 1px solid var(--line);
}
.top h2 {
  font-size: 16px;
  margin: 0;
  font-weight: 600;
}
.top > span {
  font-size: 13px;
  color: var(--dim);
}
.close {
  margin-left: auto;
}
.body {
  display: flex;
  min-height: 0;
  flex: 1;
}
.nav {
  width: 220px;
  flex: none;
  background: var(--s1);
  border-right: 1px solid var(--line);
  display: flex;
  flex-direction: column;
  min-height: 0;
}
.nsearch {
  padding: 16px 12px;
  display: flex;
  align-items: center;
  gap: 8px;
}
.nsearch svg {
  color: var(--faint);
}
input,
select {
  min-width: 0;
  background: var(--s1);
  color: var(--tx);
  border: 1px solid var(--line2);
  border-radius: 5px;
  height: 36px;
  padding: 0 8px;
  color-scheme: dark;
}
.nsearch input {
  width: 100%;
  height: 36px;
  font-size: 13px;
}
.nlist {
  flex: 1;
  overflow: auto;
}
.nitem {
  width: 100%;
  padding: 12px 16px;
  display: flex;
  flex-direction: column;
  gap: 4px;
  border: 0;
  border-left: 2px solid transparent;
  background: transparent;
  color: var(--tx);
  text-align: left;
}
.nitem:hover {
  background: rgba(255, 255, 255, 0.03);
}
.nitem.on {
  border-left-color: var(--acc);
  background: var(--s2);
}
.nitem b {
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 14px;
  font-weight: 600;
}
.nitem > span {
  color: var(--dim);
  font-size: 12px;
}
.nitem em {
  font-style: normal;
}
.newgroup {
  margin: 12px 12px 0;
  padding: 0 12px;
  height: 36px;
  display: flex;
  align-items: center;
  gap: 8px;
  border: 1px dashed var(--line2);
  border-radius: 5px;
  background: transparent;
  color: var(--dim);
}
.navhint {
  font-size: 12px;
  color: var(--faint);
  margin: 12px 16px 16px;
}
.navempty {
  padding: 8px 16px;
}
.detail {
  flex: 1;
  min-width: 0;
  min-height: 0;
  display: flex;
  flex-direction: column;
  padding: 20px 24px 0;
}
.dhead {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.namein {
  flex: 1;
  min-width: 120px;
  width: 160px;
  height: 40px;
  padding: 0 8px;
  font-size: 24px !important;
  line-height: 1.3;
  font-weight: 600 !important;
  letter-spacing: -0.02em;
  background: transparent;
  border-color: transparent;
}
.namein:hover,
.namein:focus {
  border-color: var(--line2);
}
.save-state {
  color: var(--dim);
  font-size: 12px;
  white-space: nowrap;
}
.dacts {
  display: flex;
  gap: 8px;
  align-items: center;
}
.icon {
  height: 28px;
  width: 28px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 0;
  border: 0;
  background: transparent;
  border-radius: 4px;
  color: var(--dim);
  flex: none;
}
.icon:hover {
  background: rgba(255, 255, 255, 0.06);
  color: var(--tx);
}
.btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  height: 36px;
  padding: 0 12px;
  border: 1px solid var(--line2);
  border-radius: 5px;
  background: transparent;
  color: var(--tx);
  white-space: nowrap;
  font-size: 13px !important;
}
.btn:hover {
  background: rgba(255, 255, 255, 0.05);
}
.btn.primary {
  background: var(--acc);
  border-color: transparent;
  color: #14120c;
  font-weight: 600;
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.18);
}
.btn.small {
  height: 32px;
  font-size: 12px !important;
}
.overview {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
  margin-top: 12px;
  font-size: 13px;
}
.overview strong {
  font-weight: 600;
}
.overview strong span {
  font-weight: 400;
  color: var(--dim);
}
.textbtn {
  border: 0;
  background: transparent;
  display: inline-flex;
  align-items: center;
  gap: 4px;
  color: var(--dim);
  padding: 0;
  height: 28px;
  font-size: 13px !important;
}
.textbtn .open {
  transform: rotate(180deg);
}
.summary {
  color: var(--dim);
}
.launchhint {
  color: var(--dim);
  margin: 4px 0 12px;
  font-size: 13px;
}
.warning {
  color: var(--bad) !important;
}
.feedback {
  border-left: 2px solid var(--line2);
  padding: 8px 12px;
  background: rgba(255, 255, 255, 0.025);
  font-size: 13px;
  color: var(--dim);
  margin-bottom: 12px;
  max-height: 96px;
  overflow: auto;
  flex: none;
}
.feedback.warning {
  border-left-color: var(--bad);
}
.detail-scroll {
  flex: 1;
  min-height: 0;
  overflow: auto;
  scrollbar-gutter: stable;
  /* 滚动容器会裁切外置焦点环，两侧留出完整的 4px 绘制空间。 */
  padding: 4px;
}
.layout-notice {
  margin: 0 0 8px;
  font-size: 13px;
}
.editor {
  border: 0;
  margin: 0;
  padding: 0;
  min-width: 0;
}
.settings {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px 16px;
  padding: 16px 0;
  border-top: 1px solid var(--line);
}
.settings > label {
  display: flex;
  flex-direction: column;
  gap: 4px;
  font-size: 12px;
  color: var(--dim);
}
.settings select {
  width: 100%;
  font-size: 13px;
}
.checks {
  display: flex;
  gap: 8px;
  flex-direction: column;
  justify-content: center;
  font-size: 13px;
  color: var(--dim);
}
.checks label {
  display: flex;
  align-items: center;
  gap: 8px;
}
.checks input,
.enabled input {
  width: 16px;
  height: 16px;
  margin: 0;
  padding: 0;
  accent-color: var(--acc);
}
.settinghint {
  grid-column: 1/-1;
  font-size: 12px;
  color: var(--dim);
  margin: 0;
}
.sectionhead {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
  min-height: 40px;
}
.sectionhead h3 {
  font-size: 16px;
  line-height: 1.3;
  letter-spacing: -0.01em;
  margin: 0;
  font-weight: 600;
}
.sectionhead > .btn {
  margin-left: auto;
}
.muted {
  color: var(--dim);
  font-size: 13px;
  margin: 0;
}
.sectionhead > .muted {
  font-size: 12px;
}
.position {
  border-top: 1px solid var(--line);
  padding: 12px 0 16px;
}
.position-mode {
  display: flex;
  padding: 2px;
  border: 1px solid var(--line);
  border-radius: 5px;
}
.position-mode button {
  border: 0;
  background: transparent;
  color: var(--dim);
  height: 28px;
  padding: 0 8px;
  font-size: 12px;
}
.position-mode .active {
  background: rgba(255, 255, 255, 0.08);
  color: var(--tx);
  border-radius: 3px;
}
.layoutbar {
  display: flex;
  align-items: flex-start;
  gap: 16px;
  margin-top: 8px;
}
.canvas {
  position: relative;
  flex: none;
  width: 240px;
  max-height: 180px;
  border: 1px solid var(--line2);
  background: #101012;
  overflow: hidden;
  border-radius: 4px;
}
.wblock {
  position: absolute;
  padding: 0;
  min-width: 0;
  min-height: 0;
  border: 1px solid rgba(165, 162, 156, 0.5);
  background: rgba(165, 162, 156, 0.09);
  color: var(--dim);
  border-radius: 2px;
  text-align: left;
  cursor: grab !important;
  touch-action: none;
}
.wblock.on {
  border-color: var(--acc);
  background: rgba(216, 180, 124, 0.16);
  color: var(--acc);
  z-index: 1;
}
.wblock b {
  position: absolute;
  left: 4px;
  top: 0;
  font-size: 11px;
  font-weight: 600;
}
.cempty {
  position: absolute;
  inset: 0;
  display: grid;
  place-content: center;
  font-size: 12px;
  margin: 0;
  color: var(--dim);
  padding: 8px;
}
.position-info {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.position-info > .muted {
  font-size: 12px;
}
.coords {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.coordname {
  font-size: 12px;
  color: var(--dim);
  max-width: 100%;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
  width: 100%;
}
.coords label {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 12px;
  color: var(--dim);
}
.coords input {
  width: 64px;
  height: 28px;
  font-family: Consolas, monospace;
  font-size: 12px;
  padding: 0 4px;
}
.coords input:disabled {
  opacity: 0.5;
}
.position-actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}
.screenhint {
  font-size: 12px;
  color: var(--faint);
}
.position-warning {
  font-size: 12px;
  margin: 8px 0 0;
}
.members {
  border-top: 1px solid var(--line);
  padding-top: 12px;
  padding-bottom: 16px;
}
.member-table {
  width: 100%;
  margin-top: 8px;
}
.thead,
.trow {
  display: grid;
  grid-template-columns:
    24px minmax(110px, 1fr) minmax(72px, 120px)
    100px 80px 92px 36px 84px;
  gap: 8px;
  align-items: center;
}
.thead {
  height: 36px;
  color: var(--dim);
  font-size: 12px;
  border-bottom: 1px solid var(--line);
}
.trow {
  min-height: 64px;
  border-bottom: 1px solid var(--line);
  padding: 8px 0;
}
.trow.on {
  background: rgba(255, 255, 255, 0.035);
}
.trow.off .account,
.trow.off .pos,
.trow.off .idx {
  opacity: 0.5;
}
.idx {
  font-size: 12px;
  color: var(--dim);
  padding-left: 4px;
}
.account {
  display: flex;
  flex-direction: column;
  min-width: 0;
  gap: 4px;
}
.account b {
  font-size: 14px;
  font-weight: 500;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.account > span {
  font-size: 12px;
  color: var(--dim);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.account em {
  font-style: normal;
  color: var(--bad);
}
.pos {
  color: var(--dim);
  font-size: 11px;
  white-space: nowrap;
  text-overflow: ellipsis;
  overflow: hidden;
  font-variant-numeric: tabular-nums;
}
.trow select {
  width: 100%;
  height: 32px;
  font-size: 12px;
  padding: 0 4px;
}
.enabled {
  display: flex;
  justify-content: center;
}
.ops {
  display: flex;
  gap: 0;
}
.ops .icon {
  width: 28px;
  height: 28px;
}
.addpanel {
  margin: 8px 0 16px;
  border: 1px solid var(--line2);
  border-radius: 5px;
  padding: 12px;
  background: var(--s1);
}
.addtools {
  display: flex;
  gap: 8px;
  align-items: center;
  flex-wrap: wrap;
}
.addtools > input {
  flex: 1;
  min-width: 160px;
  font-size: 13px;
  height: 32px;
}
.addtools > select {
  height: 32px;
  max-width: 160px;
  font-size: 12px;
}
.addlist {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0 16px;
  max-height: 196px;
  overflow: auto;
  margin-top: 8px;
}
.addaccount {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  height: 40px;
  background: transparent;
  color: var(--tx);
  border: 0;
  border-bottom: 1px solid var(--line);
  text-align: left;
}
.addaccount > span {
  font-size: 13px;
  min-width: 0;
  white-space: nowrap;
  text-overflow: ellipsis;
  overflow: hidden;
}
.addaccount small {
  margin-left: auto;
  white-space: nowrap;
  font-size: 12px;
  color: var(--dim);
}
.addaccount svg {
  width: 14px;
  height: 14px;
}
.addaccount:hover {
  background: rgba(255, 255, 255, 0.04);
}
.member-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  padding: 32px 16px;
  color: var(--dim);
  font-size: 13px;
}
.member-empty p {
  margin: 0;
  color: var(--tx);
  font-size: 16px;
}
.member-empty .btn {
  margin-top: 8px;
}
.footerhint {
  flex: none;
  padding: 12px 0;
  border-top: 1px solid var(--line);
  color: var(--dim);
  font-size: 12px;
}
.empty {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 24px;
  text-align: center;
  gap: 12px;
}
.empty > svg {
  width: 32px;
  height: 32px;
  color: var(--dim);
}
.empty h3 {
  margin: 0;
  font-size: 20px;
  letter-spacing: -0.02em;
  font-weight: 600;
}
.empty p {
  margin: 0 0 8px;
  color: var(--dim);
  font-size: 13px;
}
.dialog-backdrop {
  position: fixed;
  inset: 0;
  z-index: 100;
  display: grid;
  place-items: center;
  background: rgba(0, 0, 0, 0.55);
  padding: 24px;
}
.dialog {
  width: 420px;
  max-width: 100%;
  padding: 24px;
  background: var(--s2);
  border: 1px solid var(--line2);
  border-radius: 8px;
  box-shadow: 0 24px 64px rgba(0, 0, 0, 0.55);
}
.dialog h3 {
  font-size: 16px;
  margin: 0 0 12px;
  font-weight: 600;
}
.dialog p {
  font-size: 13px;
  color: var(--dim);
  margin: 0 0 24px;
}
.dialog-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
.danger {
  color: var(--bad);
  border-color: var(--bad);
}
@media (max-width: 1200px) {
  .nav {
    width: 180px;
  }
  .detail {
    padding: 16px 16px 0;
  }
  .dhead {
    gap: 4px;
  }
  .dacts {
    gap: 4px;
  }
  .dacts .btn {
    padding: 0 8px;
  }
  .namein {
    font-size: 20px !important;
    min-width: 120px;
  }
  .canvas {
    width: 200px;
  }
  .thead,
  .trow {
    grid-template-columns: 20px minmax(100px, 1fr) 68px 88px 72px 80px 32px 72px;
    gap: 4px;
  }
  .ops .icon {
    width: 24px;
  }
  .summary {
    display: none;
  }
  .coords input {
    width: 56px;
  }
  .position-actions {
    gap: 4px;
  }
  .position-actions .btn {
    padding: 0 8px;
  }
  .top {
    padding: 0 16px;
  }
  .trow {
    min-height: 64px;
  }
  .sectionhead {
    gap: 8px;
  }
}
@media (max-width: 980px) {
  .nav {
    width: 156px;
  }
  .top > span {
    display: none;
  }
  .dacts {
    margin-left: auto;
  }
  .save-state {
    margin-left: auto;
  }
  .dhead .namein {
    flex-basis: 65%;
  }
  .dhead .dacts {
    width: 100%;
    justify-content: flex-end;
    margin-top: 4px;
  }
  .layoutbar {
    gap: 12px;
  }
  .canvas {
    width: 180px;
  }
  .settings {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .thead,
  .trow {
    grid-template-columns: 20px minmax(100px, 1fr) 84px 72px 80px 32px 72px;
    gap: 4px;
  }
  .thead > span:nth-child(3),
  .trow > .pos {
    display: none;
  }
  .sectionhead > .muted {
    display: none;
  }
  .detail {
    padding-left: 12px;
    padding-right: 12px;
  }
  .overview {
    gap: 8px;
  }
  .addlist {
    grid-template-columns: 1fr;
  }
}
</style>
