<script setup lang="ts">
/**
 * 壳 03 · 控制台：图标栏 + 账号列表 + 详情面板，类 Linear。
 * 信息密度最高，运行中的客户端在详情页统一管理。
 */
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from "vue";
import {
  Flame, LayoutGrid, Monitor, Settings, Eye, EyeOff, Plus, Pencil, Play, Square,
  Layers, RefreshCw, CopyPlus, SlidersHorizontal, ChevronDown,
  // 视图切换是文字分段（.vseg），不用图标——图标表意实测失败
  ArrowUpDown, ArrowUp, ArrowDown, GripVertical, Search, X, Check, LayoutDashboard,
} from "lucide-vue-next";
import type { Account, LauncherStore } from "../contract";
import { orderedAccountTags, tagIdentity, replaceAccountSubsequence, matchesAccount, accountTimeBucket, type AccountListView, type AccountSection, type AccountSort } from "../accountList";
import LaunchGroups from "../../components/LaunchGroups.vue";

const props = defineProps<{ store: LauncherStore }>();
const store = props.store;
import { demoUi } from "../ui";
import { state as appState } from "../../composables/useAppState";
import { channelLabel, fmtRun } from "../contract";

const cur = computed(() => store.current);

// 账号列表宽度：可拖拽调节，存后端（设置里的 railWidth）
/**
 * 内容区视图：账号 / 启动组。
 *
 * 用"窗口内切换"而不是弹层：启动组是左侧图标栏的一级入口，
 * 一级入口应该换内容，不该盖一层——盖一层会让人以为点错了、想找关闭按钮。
 */
const view = ref<"accounts" | "groups">("accounts");

const railWidth = ref(appState.settings.railWidth >= 220 ? appState.settings.railWidth : 264);
const dragging = ref(false);
let cleanupDivider: (() => void) | undefined;
function onDividerDown(e: MouseEvent) {
  cleanupDivider?.();
  e.preventDefault();
  dragging.value = true;
  const startX = e.clientX;
  const startW = railWidth.value;
  const move = (ev: MouseEvent) => {
    railWidth.value = Math.min(520, Math.max(220, startW + (ev.clientX - startX)));
  };
  const up = () => {
    dragging.value = false;
    window.removeEventListener("mousemove", move);
    window.removeEventListener("mouseup", up);
    window.removeEventListener("blur", up);
    cleanupDivider = undefined;
    store.setRailWidth(railWidth.value);
  };
  window.addEventListener("mousemove", move);
  window.addEventListener("mouseup", up);
  window.addEventListener("blur", up);
  cleanupDivider = () => {
    window.removeEventListener("mousemove", move);
    window.removeEventListener("mouseup", up);
    window.removeEventListener("blur", up);
    dragging.value = false;
  };
}
function resizeRail(delta: number) {
  railWidth.value = Math.min(520, Math.max(220, railWidth.value + delta));
  store.setRailWidth(railWidth.value);
}
function resetRail() { railWidth.value = 288; store.setRailWidth(288); }
onUnmounted(() => cleanupDivider?.());
const expired = computed(() => cur.value?.cred === "expired");
const runningSelf = computed(() => !!cur.value?.running);

/* 列表的分组身份、文件顺序和标签组顺序各自独立，避免同名标签被误认为系统分组。 */
type ListView = AccountListView;
const VIEW_TABS: { k: ListView; label: string; tip: string }[] = [
  { k: "flat", label: "平铺", tip: "每个账号只显示一次" },
  { k: "tag", label: "标签", tip: "多标签账号会在各标签组中显示" },
  { k: "channel", label: "渠道", tip: "按渠道查看账号" },
  { k: "time", label: "时间", tip: "按添加时间查看账号" },
];
const SORTS: { k: AccountSort; label: string; tip: string }[] = [
  { k: "custom", label: "手动顺序", tip: "拖动左侧手柄调整，自动保存" },
  { k: "name", label: "名称", tip: "按账号名称查看，不改变手动顺序" },
  { k: "recent", label: "最近登录", tip: "最近登录在前，未登录过的放最后" },
  { k: "running", label: "运行优先", tip: "运行中的账号在前，其余保持手动顺序" },
];
const listView = ref<ListView>("flat");
const flatSort = ref<AccountSort>("custom");
const sortOpen = ref(false);
const search = ref("");
const collapsed = ref<string[]>([]);
const orderSaving = ref(false);
const tagOrderSaving = ref(false);
const listMessage = ref("");
const listError = ref(false);
const listBusy = computed(() => orderSaving.value || tagOrderSaving.value);
const sortLabel = computed(() => listView.value === "flat" ? SORTS.find((s) => s.k === flatSort.value)?.label : ({ tag: "标签顺序", channel: "按渠道", time: "按时间" })[listView.value]);
const tagOrderDraft = ref<string[] | null>(null);
const groupOrder = computed(() => orderedAccountTags(store.accounts, tagOrderDraft.value ?? store.tagOrder ?? []));
const visibleAccounts = computed(() => store.accounts.filter((a) => matchesAccount(a, search.value, channelLabel)));
const selectedCount = computed(() => store.accounts.filter((a) => store.selectedIds.includes(a.id)).length);
const CHANNEL_ORDER = ["华为渠道", "网易官方", "B站渠道", "4399渠道", "vivo渠道"];
const TIME_ORDER = ["最近 7 天", "8–30 天", "30 天以上"];
const displayName = (a: Account) => store.maskAll ? a.maskedName : a.name;
const sections = computed<AccountSection[]>(() => {
  const accs = visibleAccounts.value;
  if (listView.value === "flat") {
    const rows = [...accs];
    if (flatSort.value === "name") rows.sort((a, b) => a.name.localeCompare(b.name, "zh"));
    if (flatSort.value === "recent") rows.sort((a, b) => (b.lastLoginTs ?? 0) - (a.lastLoginTs ?? 0));
    if (flatSort.value === "running") rows.sort((a, b) => Number(b.running) - Number(a.running));
    return [{ key: "flat", label: "", rows, draggable: flatSort.value === "custom" }];
  }
  if (listView.value === "tag") {
    const groups: AccountSection[] = groupOrder.value.map((tag) => ({
      key: "tag:" + tagIdentity(tag), label: tag, tag,
      rows: accs.filter((a) => a.tags.some((t) => tagIdentity(t) === tagIdentity(tag))), draggable: true,
    }));
    groups.push({ key: "system:untagged", label: "未分组", untagged: true, rows: accs.filter((a) => !a.tags.length), draggable: true });
    return groups.filter((s) => s.rows.length);
  }
  if (listView.value === "channel") {
    const names = [...new Set(accs.map((a) => channelLabel(a.channel)))];
    const order = [...CHANNEL_ORDER.filter((n) => names.includes(n)), ...names.filter((n) => !CHANNEL_ORDER.includes(n))];
    return order.map((name) => ({ key: "channel:" + name, label: name, rows: accs.filter((a) => channelLabel(a.channel) === name), draggable: false }));
  }
  return TIME_ORDER.map((label) => ({ key: "time:" + label, label, rows: accs.filter((a) => accountTimeBucket(a) === label), draggable: false })).filter((s) => s.rows.length);
});
const listHint = computed(() => {
  if (listBusy.value) return "正在保存顺序…";
  if (listMessage.value) return listMessage.value;
  if (search.value.trim()) return `找到 ${visibleAccounts.value.length} / ${store.accounts.length} 个账号`;
  if (listView.value === "tag") return "多标签账号会出现在各组";
  if (listView.value !== "flat") return "分组查看 · 切换视图保留勾选";
  return flatSort.value === "custom" ? "拖动手柄排序，松手自动保存" : "仅改变查看方式，手动顺序保留";
});

function tagsOf(a: Account, group: AccountSection) {
  return group.tag ? a.tags.filter((t) => tagIdentity(t) !== tagIdentity(group.tag!)) : a.tags;
}
function rowTitle(a: Account, sec: AccountSection) {
  const tags = a.tags.length ? `标签：${a.tags.join("、")}` : "无标签";
  return `${displayName(a)}\n${a.running ? "运行中" : a.cred === "expired" ? "凭证过期" : "就绪"} · ${channelLabel(a.channel)}\n${tags}${sec.tag && a.tags.length > 1 ? "\n这是同一账号在不同标签组中的显示" : ""}`;
}
function closeSort() { sortOpen.value = false; }
async function openSort() {
  if (listView.value !== "flat" || listBusy.value) return;
  sortOpen.value = true;
  await nextTick();
  document.querySelector<HTMLButtonElement>(".sortmenu button.on")?.focus();
}
function onSortKeys(e: KeyboardEvent) {
  const items = Array.from(document.querySelectorAll<HTMLButtonElement>(".sortmenu button"));
  if (e.key === "ArrowDown" || e.key === "ArrowUp") {
    e.preventDefault();
    const at = items.indexOf(document.activeElement as HTMLButtonElement);
    items[(at + (e.key === "ArrowDown" ? 1 : -1) + items.length) % items.length]?.focus();
  }
}
function toggleSection(key: string) {
  if (collapsed.value.includes(key)) collapsed.value = collapsed.value.filter((k) => k !== key);
  else collapsed.value.push(key);
}
function sectionSelected(sec: AccountSection) { return sec.rows.filter((a) => store.selectedIds.includes(a.id)).length; }
function secSelectedAll(sec: AccountSection) { return sec.rows.length > 0 && sectionSelected(sec) === sec.rows.length; }
function toggleSelectSec(sec: AccountSection) {
  const target = !secSelectedAll(sec);
  for (const a of sec.rows) if (store.selectedIds.includes(a.id) !== target) store.toggleSelect(a.id);
}
function clearSelection() { for (const id of [...store.selectedIds]) store.toggleSelect(id); }
async function moveTagGroup(sec: AccountSection, delta: number) {
  if (!sec.tag || listBusy.value || search.value.trim()) return;
  const next = [...groupOrder.value];
  const from = next.findIndex((t) => tagIdentity(t) === tagIdentity(sec.tag!));
  const to = from + delta;
  if (to < 0 || to >= next.length) return;
  [next[from], next[to]] = [next[to]!, next[from]!];
  tagOrderSaving.value = true;
  tagOrderDraft.value = next;
  listMessage.value = "";
  try {
    const ok = await store.setTagOrder(next);
    listMessage.value = ok ? "标签组顺序已保存" : "标签组顺序未保存，请重试";
    listError.value = !ok;
  } catch {
    listMessage.value = "标签组顺序未保存，请重试"; listError.value = true;
  } finally { tagOrderDraft.value = null; tagOrderSaving.value = false; }
}
function tagGroupIndex(sec: AccountSection) { return groupOrder.value.findIndex((t) => tagIdentity(t) === tagIdentity(sec.tag ?? "")); }

/* 拖动仅从手柄开始：勾选、选号、触屏滚动都不再意外改变账号顺序。 */
const listScroll = ref<HTMLElement | null>(null);
const draggingId = ref("");
const dragSec = ref("");
const dragIds = ref<string[]>([]);
let dragSession: { id: string; key: string; pointer: number; startY: number; y: number; x: number; ids: string[]; active: boolean } | null = null;
let scrollFrame = 0;
let savedCursor = "";
let savedSelect = "";
let suppressClick = false;
let suppressTimer: ReturnType<typeof setTimeout> | undefined;
const orderPreview = ref<string[] | null>(null);
function canDragSec(sec: AccountSection) { return sec.draggable && sec.rows.length > 1 && !listBusy.value; }
function sectionElement(key: string) {
  return Array.from(listScroll.value?.querySelectorAll<HTMLElement>(".sect") ?? []).find((s) => s.dataset.sec === key);
}
function onRowPointerDown(e: PointerEvent, sec: AccountSection, a: Account) {
  if (e.button !== 0 || e.ctrlKey || e.metaKey || e.shiftKey || !canDragSec(sec)) return;
  cancelDrag();
  e.preventDefault(); e.stopPropagation();
  savedCursor = document.body.style.cursor;
  savedSelect = document.body.style.userSelect;
  dragSession = { id: a.id, key: sec.key, pointer: e.pointerId, startY: e.clientY, y: e.clientY, x: e.clientX, ids: sec.rows.map((x) => x.id), active: false };
  dragIds.value = [...dragSession.ids];
  window.addEventListener("pointermove", onRowPointerMove, { passive: false });
  window.addEventListener("pointerup", onRowPointerUp);
  window.addEventListener("pointercancel", cancelDrag);
  window.addEventListener("blur", cancelDrag);
}
function updateDragPosition() {
  const d = dragSession;
  if (!d?.active) return;
  const rows = Array.from(sectionElement(d.key)?.querySelectorAll<HTMLElement>(".lrow") ?? []);
  let at = rows.length;
  for (let i = 0; i < rows.length; i++) {
    const r = rows[i]!.getBoundingClientRect();
    if (d.y < r.top + r.height / 2) { at = i; break; }
  }
  const from = dragIds.value.indexOf(d.id);
  const next = dragIds.value.filter((id) => id !== d.id);
  if (from < at) at--;
  next.splice(Math.max(0, Math.min(next.length, at)), 0, d.id);
  if (JSON.stringify(next) !== JSON.stringify(dragIds.value)) dragIds.value = next;
}
function scrollDrag() {
  const d = dragSession;
  const box = listScroll.value;
  if (!d?.active || !box) return;
  const r = box.getBoundingClientRect();
  if (d.x >= r.left && d.x <= r.right) {
    if (d.y < r.top + 30) box.scrollTop -= 8;
    else if (d.y > r.bottom - 30) box.scrollTop += 8;
  }
  updateDragPosition();
  scrollFrame = requestAnimationFrame(scrollDrag);
}
function onRowPointerMove(e: PointerEvent) {
  const d = dragSession;
  if (!d || e.pointerId !== d.pointer) return;
  d.y = e.clientY; d.x = e.clientX;
  if (!d.active && Math.abs(e.clientY - d.startY) < 5) return;
  e.preventDefault();
  if (!d.active) {
    d.active = true; draggingId.value = d.id; dragSec.value = d.key;
    document.body.style.cursor = "grabbing"; document.body.style.userSelect = "none";
    scrollFrame = requestAnimationFrame(scrollDrag);
  }
  updateDragPosition();
}
function cleanDrag() {
  cancelAnimationFrame(scrollFrame);
  window.removeEventListener("pointermove", onRowPointerMove);
  window.removeEventListener("pointerup", onRowPointerUp);
  window.removeEventListener("pointercancel", cancelDrag);
  window.removeEventListener("blur", cancelDrag);
  if (dragSession) { document.body.style.cursor = savedCursor; document.body.style.userSelect = savedSelect; }
  dragSession = null; draggingId.value = ""; dragSec.value = ""; dragIds.value = [];
}
function blockDragClick() {
  suppressClick = true;
  clearTimeout(suppressTimer);
  suppressTimer = setTimeout(() => { suppressClick = false; }, 180);
}
function cancelDrag() {
  if (dragSession?.active) blockDragClick();
  cleanDrag();
}
async function onRowPointerUp(e: PointerEvent) {
  const d = dragSession;
  if (!d || e.pointerId !== d.pointer) return;
  const box = listScroll.value?.getBoundingClientRect();
  const secBox = sectionElement(d.key)?.getBoundingClientRect();
  const valid = d.active && box && secBox && e.clientX >= box.left && e.clientX <= box.right && e.clientY >= Math.max(box.top, secBox.top) && e.clientY <= Math.min(box.bottom, secBox.bottom);
  const tmp = [...dragIds.value];
  if (d.active) blockDragClick();
  cleanDrag();
  if (!valid || JSON.stringify(tmp) === JSON.stringify(d.ids)) return;
  const liveIds = store.accounts.map((a) => a.id);
  if (d.ids.some((id) => !liveIds.includes(id))) { listMessage.value = "账号列表已变化，请重新排序"; listError.value = true; return; }
  const next = replaceAccountSubsequence(liveIds, tmp);
  orderPreview.value = next;
  orderSaving.value = true; listMessage.value = "";
  try {
    const ok = await store.reorder(next);
    listMessage.value = ok ? "账号顺序已保存" : "顺序未保存，已恢复原顺序";
    listError.value = !ok;
  } catch { listMessage.value = "顺序未保存，已恢复原顺序"; listError.value = true; }
  finally { orderPreview.value = null; orderSaving.value = false; }
}
function selectRow(a: Account) { if (!suppressClick && !dragSession?.active) store.select(a.id); }
function rowsOf(sec: AccountSection) {
  const ids = dragSec.value === sec.key && dragSession?.active ? dragIds.value : orderPreview.value;
  if (!ids || (listView.value === "flat" && flatSort.value !== "custom")) return sec.rows;
  const map = new Map(sec.rows.map((a) => [a.id, a]));
  return ids.map((id) => map.get(id)).filter((a): a is Account => !!a);
}
async function moveAccount(sec: AccountSection, a: Account, delta: number) {
  if (!canDragSec(sec)) return;
  const ids = sec.rows.map((x) => x.id); const at = ids.indexOf(a.id); const to = at + delta;
  if (to < 0 || to >= ids.length) return;
  [ids[at], ids[to]] = [ids[to]!, ids[at]!];
  orderSaving.value = true; listMessage.value = "";
  orderPreview.value = replaceAccountSubsequence(store.accounts.map((x) => x.id), ids);
  try { const ok = await store.reorder(orderPreview.value); listError.value = !ok; listMessage.value = ok ? "账号顺序已保存" : "顺序未保存，已恢复原顺序"; }
  catch { listError.value = true; listMessage.value = "顺序未保存，已恢复原顺序"; }
  finally { orderPreview.value = null; orderSaving.value = false; }
}
function onGlobalKey(e: KeyboardEvent) { if (e.key === "Escape") { cancelDrag(); closeSort(); } }
onMounted(() => { document.addEventListener("click", closeSort); window.addEventListener("keydown", onGlobalKey); });
onUnmounted(() => { cancelDrag(); clearTimeout(suppressTimer); document.removeEventListener("click", closeSort); window.removeEventListener("keydown", onGlobalKey); });
watch([listView, flatSort, search, view], () => { cancelDrag(); closeSort(); listMessage.value = ""; });
watch(() => JSON.stringify(store.accounts.map((a) => [a.id, ...a.tags])), () => { if (dragSession) cancelDrag(); });

/* 标签草稿按账号隔离；保存成功才清空，输入法回车不触发提交。 */
const tagComposing = ref(false);
const tagDrafts = ref<Record<string, string>>({});
const tagSaving = ref<Record<string, boolean>>({});
const tagMessages = ref<Record<string, string>>({});
const tagErrors = ref<Record<string, boolean>>({});
watch(() => cur.value?.id, () => { tagComposing.value = false; });
watch(() => [cur.value?.id, cur.value ? tagDrafts.value[cur.value.id] : ""], () => {
  const id = cur.value?.id;
  if (id && !tagSaving.value[id]) { tagMessages.value[id] = ""; tagErrors.value[id] = false; }
}, { flush: "sync" });
const tagSuggestions = computed(() => {
  const a = cur.value; if (!a || a.tags.length >= 8) return [];
  const q = tagIdentity(tagDrafts.value[a.id] ?? "");
  return groupOrder.value.filter((t) => !a.tags.some((x) => tagIdentity(x) === tagIdentity(t)) && (!q || tagIdentity(t).includes(q))).slice(0, 5);
});
async function addTag(a: Account, suggestion?: string) {
  if (tagSaving.value[a.id]) return;
  const draft = tagDrafts.value[a.id] ?? "";
  const value = (suggestion ?? draft).trim();
  const fail = (message: string) => { tagMessages.value[a.id] = message; tagErrors.value[a.id] = true; };
  if (!value) return;
  if ([...value].length > 16) return fail("每个标签最多 16 个字");
  if (a.tags.some((t) => tagIdentity(t) === tagIdentity(value))) return fail("这个账号已有该标签");
  if (a.tags.length >= 8) return fail("每个账号最多 8 个标签，请先移除一个");
  tagSaving.value[a.id] = true; tagMessages.value[a.id] = "正在保存…"; tagErrors.value[a.id] = false;
  try {
    const ok = await store.setTags(a.id, [...a.tags, value]);
    tagMessages.value[a.id] = ok ? "标签已保存" : "未保存，请重试"; tagErrors.value[a.id] = !ok;
    if (ok && tagDrafts.value[a.id] === draft) tagDrafts.value[a.id] = "";
  } catch { fail("未保存，请重试"); }
  finally { tagSaving.value[a.id] = false; }
}
function onTagEnter(e: KeyboardEvent, a: Account) { if (e.isComposing || e.keyCode === 229) return; e.preventDefault(); void addTag(a); }
async function removeTag(a: Account, tag: string) {
  if (tagSaving.value[a.id]) return;
  tagSaving.value[a.id] = true; tagMessages.value[a.id] = "正在保存…"; tagErrors.value[a.id] = false;
  try { const ok = await store.setTags(a.id, a.tags.filter((t) => t !== tag)); tagMessages.value[a.id] = ok ? "标签已移除" : "未保存，请重试"; tagErrors.value[a.id] = !ok; }
  catch { tagMessages.value[a.id] = "未保存，请重试"; tagErrors.value[a.id] = true; }
  finally { tagSaving.value[a.id] = false; }
}

// 被寄宿的号：找出提供槽位的那个宿主账号，给用户看名字而不是一串 ID。
// 宿主可能已经被删掉了，那种情况下标出来——用户需要知道这条关系已经悬空。
const hostName = computed(() => {
  const id = cur.value?.hostedBy;
  if (!id) return "";
  const hit = store.accounts.find((x) => x.playerId === id || x.fullId === id);
  return hit ? displayName(hit) : `已删除的账号 ${store.maskAll ? '***' : id.slice(0, 6) + '…'}`;
});

// 详情默认只露最常用的四项，其余收在「更多信息」里（换账号时保持展开状态，
// 正在排查过期问题的用户不用每次重新点开）
const detailOpen = ref(false);

// 详情面板里的标签（都是"人话"，原文值只在 Go 日志和 accounts.json 里）。
// 登录方式按来源细分：同样是扫码，手机扫和游戏内让渠道客户端扫不是一回事。
function loginLabel(a?: { loginMethod?: string; source?: string }): string {
  switch (a?.loginMethod) {
    case "qrcode":
      return "扫码（手机）";
    case "password":
      // 网易账号密码现在走本工具自己的输入框（无头登录），不再弹官方窗。
      // 但历史上用官方窗登录的号还在（source=mpaydll），那种号的密码没存下来、
      // 以后不能自动重登，所以详情里仍然分开标，免得用户以为它也能自动续期。
      return a?.source === "mpaydll" ? "账号密码（官方窗）" : "账号密码";
    case "sms":
      return "手机验证码";
    case "channelscan":
      return "渠道服游戏内扫码";
  }
  switch (a?.source) {
    case "weblogin":
      return "渠道网页登录";
    case "channel":
      return "渠道登录";
    case "mpaydll":
      return "账号密码（官方窗）";
  }
  return "—";
}

// mpay 一栏只显示版本，路径放在 title 里（太长，而且用户基本不看）
const mpayLabel = computed(() => {
  const v = cur.value?.mpayVersion;
  if (!v) return "—";
  return "mpay " + v;
});

// 上次验证凭证的时间：只有验证过才显示
const lastCheckText = computed(() => {
  const t = cur.value?.lastCheck;
  if (!t) return "";
  const d = new Date(t * 1000);
  const p = (n: number) => String(n).padStart(2, "0");
  return `${d.getMonth() + 1}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
});

</script>

<template>
  <div class="win">
    <div class="rail">
      <span class="rlogo"><Flame /></span>
      <button class="rico" :class="{ on: view === 'accounts' }" title="账号" @click="view = 'accounts'"><LayoutGrid /></button>
      <button class="rico" :class="{ on: view === 'groups' }" title="启动组" @click="view = 'groups'"><Monitor /></button>
      <button class="rico" title="监控墙（实时小窗）" @click="store.toggleWall()"><LayoutDashboard /></button>
      <button class="rico bottom" title="设置" @click="store.openSettings()"><Settings /></button>
    </div>

    <!-- 账号视图：列表 + 详情（原来的那一套） -->
    <template v-if="view === 'accounts'">
    <div class="list" :style="{ width: railWidth + 'px' }">
      <!-- 头部两行，控件位置在任何视图下都不动：
           第一行：标题 + 排序（非平铺时禁用占位，不消失——消失会让打码键跳位）
           第二行：文字分段切换视图。图标版实测看不出功能，文字不用猜。 -->
      <div class="lhead">
        <span class="lname">账号 <em>{{ store.accounts.length }}</em></span>
        <div class="sortwrap">
          <button
            class="sortbtn"
            :class="{ on: sortOpen }"
            :disabled="listView !== 'flat' || listBusy"
            :title="listView === 'flat' ? '选择查看顺序，不改动已保存的手动顺序' : '分组查看时，账号仍沿用手动顺序'"
            aria-haspopup="menu" :aria-expanded="sortOpen"
            @keydown.down.prevent="openSort"
            @click.stop="sortOpen = !sortOpen"
          >
            <ArrowUpDown /><span>{{ sortLabel }}</span>
          </button>
          <div v-if="sortOpen && listView === 'flat'" class="sortmenu" role="menu" aria-label="账号查看顺序" @keydown="onSortKeys">
            <button
              v-for="o in SORTS"
              :key="o.k"
              :class="{ on: flatSort === o.k }"
              :title="o.tip" role="menuitemradio" :aria-checked="flatSort === o.k"
              @click.stop="flatSort = o.k; sortOpen = false"
            >
              <span>{{ o.label }}</span><Check v-if="flatSort === o.k" />
            </button>
          </div>
        </div>
        <button class="ico sm" :title="store.maskAll ? '显示完整信息' : '打码'" @click="store.toggleMask()">
          <EyeOff v-if="store.maskAll" :style="{ width: '14px', height: '14px' }" /><Eye v-else :style="{ width: '14px', height: '14px' }" />
        </button>
      </div>
      <div class="vseg" role="group" aria-label="账号分组方式">
        <button
          v-for="v in VIEW_TABS"
          :key="v.k"
          class="vtab"
          :class="{ on: listView === v.k }"
          :title="v.tip" :aria-pressed="listView === v.k" :disabled="listBusy"
          @click="listView = v.k"
        >
          {{ v.label }}
        </button>
      </div>

      <div class="listsearch">
        <Search aria-hidden="true" /><input v-model="search" aria-label="搜索账号或标签" placeholder="搜索账号、标签或渠道" :disabled="listBusy" />
        <button v-if="search" class="searchclear" aria-label="清空搜索" @click="search = ''"><X /></button>
      </div>
      <div class="listcontext" :class="{ error: listError && !!listMessage }" role="status" :title="listHint"><span>{{ listHint }}</span></div>
      <div v-if="selectedCount" class="listselection"><span>已勾选 {{ selectedCount }} 个账号</span><button @click="clearSelection">清空勾选</button></div>
      <p v-if="!store.accounts.length" class="lempty">还没有账号，添加后即可开始</p>
      <div v-else-if="!visibleAccounts.length" class="lempty search-empty"><span>没有匹配的账号</span><button @click="search = ''">清空搜索</button></div>
      <div v-else ref="listScroll" class="rows" :class="{ busy: listBusy }" :aria-busy="listBusy">
        <section v-for="sec in sections" :key="listView + sec.key" class="sect" :data-sec="sec.key">
          <div
            v-if="sec.label"
            class="shead"
            :class="{ allon: secSelectedAll(sec), partial: sectionSelected(sec) > 0 && !secSelectedAll(sec) }"
            :title="secSelectedAll(sec) ? '取消勾选这一组' : '勾选这一组；多标签账号只计一次'"
            @click="toggleSelectSec(sec)"
          >
            <button class="chevbtn" :title="collapsed.includes(sec.key) ? '展开这一组' : '折叠这一组'" :aria-expanded="!collapsed.includes(sec.key)" @click.stop="toggleSection(sec.key)"><ChevronDown class="chev" :class="{ closed: collapsed.includes(sec.key) }" /></button>
            <input class="section-check" type="checkbox" :checked="secSelectedAll(sec)" :indeterminate="sectionSelected(sec) > 0 && !secSelectedAll(sec)" :aria-label="'勾选' + sec.label + '组'" @click.stop @change="toggleSelectSec(sec)" />
            <button class="sname" :title="sec.untagged ? '未添加标签的账号' : sec.label" @click.stop="toggleSelectSec(sec)">{{ sec.label }}<small v-if="sec.untagged">无标签</small></button>
            <em>{{ sec.rows.length }}</em>
            <span v-if="listView === 'tag' && sec.tag" class="groupmoves">
              <button :disabled="listBusy || !!search.trim() || tagGroupIndex(sec) === 0" :aria-label="'上移标签组：' + sec.label" :title="search.trim() ? '清空搜索后调整标签组顺序' : '上移标签组'" @click.stop="moveTagGroup(sec, -1)"><ArrowUp /></button>
              <button :disabled="listBusy || !!search.trim() || tagGroupIndex(sec) === groupOrder.length - 1" :aria-label="'下移标签组：' + sec.label" :title="search.trim() ? '清空搜索后调整标签组顺序' : '下移标签组'" @click.stop="moveTagGroup(sec, 1)"><ArrowDown /></button>
            </span>
          </div>
          <!-- TransitionGroup 提供位移过渡：拖动时其余行是"让位"而不是"跳位" -->
          <TransitionGroup
            v-if="!collapsed.includes(sec.key)"
            name="rl"
            tag="div"
            class="rlist"
          >
            <div
              v-for="a in rowsOf(sec)"
              :key="a.id"
              class="lrow"
              :class="{ on: a.current, picked: store.selectedIds.includes(a.id), dragging: draggingId === a.id && dragSec === sec.key }"
              :data-account-id="a.id" :title="rowTitle(a, sec)"
              tabindex="0" role="button" :aria-label="'查看账号：' + displayName(a)" :aria-current="a.current ? 'true' : undefined"
              @click="selectRow(a)" @keydown.enter.self.prevent="selectRow(a)" @keydown.space.self.prevent="store.toggleSelect(a.id)"
            >
              <input
                class="sel"
                type="checkbox"
                :checked="store.selectedIds.includes(a.id)"
                title="选中该账号"
                @click.stop
                @change="store.toggleSelect(a.id)"
              />
              <button v-if="sec.draggable && sec.rows.length > 1" class="rowgrip" :disabled="listBusy" :aria-label="'调整账号顺序：' + displayName(a)" title="拖动排序；Alt + 上下键微调" @pointerdown="onRowPointerDown($event, sec, a)" @click.stop @keydown.alt.up.prevent.stop="moveAccount(sec, a, -1)" @keydown.alt.down.prevent.stop="moveAccount(sec, a, 1)"><GripVertical /></button>
              <span class="ltxt">
                <!-- 第一行：身份。名字是唯一主角，标签贴右缘——
                     身份和分类读成一条水平线，第二行只留状态 -->
                <span class="l1">
                  <b>{{ store.maskAll ? a.maskedName : a.name }}</b>
                  <span v-if="a.copyKey" class="copybadge" title="副本：独立记录，凭证与原件共享">副本</span>
                  <span v-if="tagsOf(a, sec).length" class="chips" :title="tagsOf(a, sec).join('、')">
                    <span v-for="t in tagsOf(a, sec).slice(0, railWidth < 330 ? 1 : 2)" :key="t" class="tagchip" :title="t">{{ t }}</span>
                    <span v-if="tagsOf(a, sec).length > (railWidth < 330 ? 1 : 2)" class="tagchip more">+{{ tagsOf(a, sec).length - (railWidth < 330 ? 1 : 2) }}</span>
                  </span>
                </span>
                <!-- 第二行：能不能用。状态词着色（运行=绿 / 过期=红），
                     时长和渠道退成中性，一眼扫出哪些在跑、哪些要处理 -->
                <span class="l2">
                  <span class="st">
                    <em v-if="a.running" class="dot ok live" />
                    <em v-else-if="a.cred === 'expired'" class="dot bad" />
                    <em v-else class="dot ok" />
                    <span v-if="a.running" class="sv run">运行中</span>
                    <span v-else-if="a.cred === 'expired'" class="sv exp">凭证过期</span>
                    <span v-else class="sv">就绪</span><template v-if="a.running"> · {{ fmtRun(a.runSeconds) }}</template><template v-if="listView !== 'channel'"> · {{ channelLabel(a.channel) }}</template>
                  </span>
                </span>
              </span>
            </div>
          </TransitionGroup>
        </section>
      </div>
      <button class="ladd" @click="store.addAccount()"><Plus />添加账号</button>
    </div>

    <!-- 拖动调节账号列表宽度 -->
    <div class="splitter" :class="{ dragging }" tabindex="0" role="separator" aria-label="账号列表宽度" aria-orientation="vertical" :aria-valuenow="railWidth" :aria-valuemin="220" :aria-valuemax="520" title="拖动调整列表宽度，双击还原" @mousedown="onDividerDown" @dblclick="resetRail" @keydown.left.prevent="resizeRail(-12)" @keydown.right.prevent="resizeRail(12)"></div>

    <div v-if="cur" class="det">
      <div class="dhead">
        <div>
          <p class="deyebrow">{{ channelLabel(cur.channel) }}</p>
          <div class="dtitle">
            <h3>{{ store.maskAll ? cur.maskedName : cur.name }}</h3>
            <button class="ico" title="游戏设置（画质 / 帧率）" @click="demoUi.openQuality(cur.id)"><SlidersHorizontal /></button>
            <button class="ico" title="修改别名" @click="demoUi.openRename(cur.id)"><Pencil /></button>
            <span v-if="runningSelf" class="pill live"><em class="dot ok live" />运行中</span>
            <span v-else-if="expired" class="pill exp"><em class="dot bad" />凭证过期</span>
            <span v-else class="pill ok"><em class="dot ok" />就绪</span>
          </div>
        </div>
      </div>

      <div class="dinfo">
        <div><span>账号 ID</span><b class="mono">{{ store.maskAll ? cur.maskedId : cur.fullId }}</b></div>
        <div><span>渠道</span><b>{{ channelLabel(cur.channel) }}</b></div>
        <div><span>添加时间</span><b class="mono">{{ cur.created || "—" }}</b></div>
        <div><span>最后登录</span><b class="mono">{{ cur.lastLogin || "—" }}</b></div>
      </div>

      <!-- 标签：在这里改。列表只负责显示和筛选，编辑永远落在详情里 -->
      <div class="dsection">
        <div class="tagheading"><p class="dseclabel tight">标签 <span>{{ cur.tags.length }}/8</span></p><span class="tagfeedback" :class="{ error: tagErrors[cur.id] }" role="status">{{ tagMessages[cur.id] }}</span></div>
        <div class="tagrow" :aria-busy="!!tagSaving[cur.id]">
          <span v-for="t in cur.tags" :key="t" class="tagpick" :title="t"><span>{{ t }}</span><button class="x" :aria-label="'移除标签：' + t" :disabled="tagSaving[cur.id]" @click="removeTag(cur, t)">×</button></span>
          <span v-if="!cur.tags.length" class="tag-empty">还没有标签</span>
        </div>
        <form class="tagadd" @submit.prevent="!tagComposing && addTag(cur)">
          <input v-model="tagDrafts[cur.id]" class="tagin" aria-label="添加账号标签" :placeholder="cur.tags.length >= 8 ? '标签已满，可先移除一个' : '输入新标签，或选择下方已有标签'" :disabled="!!tagSaving[cur.id] || cur.tags.length >= 8" @compositionstart="tagComposing = true" @compositionend="tagComposing = false" @keydown.enter="onTagEnter($event, cur)" />
          <button class="tagaddbtn" type="submit" :disabled="!!tagSaving[cur.id] || cur.tags.length >= 8 || !tagDrafts[cur.id]?.trim()"><Plus />添加</button>
        </form>
        <div v-if="tagSuggestions.length" class="tagsuggestions"><span>已有标签</span><button v-for="t in tagSuggestions" :key="t" :disabled="!!tagSaving[cur.id]" :title="'添加标签：' + t" @click="addTag(cur, t)">{{ t }}</button></div>
        <p class="regionnote">每个标签最多 16 个字；仅保存在本机，副本单独设置。</p>
      </div>

      <!-- 其余信息默认收起来：日常只看"这个号是什么、什么时候加的、上次什么时候登的"，
           排查凭证过期时才展开 -->
      <button class="dmore" :aria-expanded="detailOpen" @click="detailOpen = !detailOpen">
        {{ detailOpen ? "收起详细信息" : "更多信息" }}
        <ChevronDown class="chev" :class="{ open: detailOpen }" />
      </button>

      <template v-if="detailOpen">
        <div class="dinfo">
          <div><span>等级</span><b>{{ cur.level || "—" }}</b></div>
          <div><span>登录方式</span><b>{{ loginLabel(cur) }}</b></div>
          <div>
            <span>凭证</span>
            <b class="credline">
              {{ expired ? "已过期" : "正常，启动时自动刷新" }}
              <button v-if="expired" class="btn sm danger" @click="store.refreshCred(cur.id)">刷新凭证</button>
            </b>
          </div>
          <div v-if="cur.tokenSaLabel"><span>凭证等级</span><b>{{ cur.tokenSaLabel }}</b></div>
          <div v-if="cur.tokenAt"><span>签发时间</span><b class="mono">{{ cur.tokenAt }}</b></div>
          <div v-if="lastCheckText"><span>上次验证</span><b class="mono">{{ lastCheckText }}</b></div>
          <div><span>状态</span><b>{{ runningSelf ? "已运行 " + fmtRun(cur.runSeconds) : "空闲" }}</b></div>
        </div>

        <!-- 登录环境：排查"这个号的凭证为什么更容易过期"时看这一段 -->
        <p class="dseclabel tight">登录环境</p>
        <div class="dinfo second">
          <div><span>平台身份</span><b class="mono">{{ cur.gameId || "—" }}</b></div>
          <div><span>mpay</span><b class="mono" :title="cur.mpayPath || '这个渠道不走官方登录窗'">{{ mpayLabel }}</b></div>
        </div>
        <p v-if="cur.lastError" class="derr">{{ cur.lastError }}</p>
      </template>

      <!-- 游戏区：只有网易官方账号能选。选了之后启动时按这个区声明平台，
           游戏就不再弹选哪个区；未选则保持旧行为。

           被寄宿的号（hosted）不显示选择器：它的渠道在扫码那一刻就固定了，
           槽位票直接继承，再让它选区只会误导——它没有平台凭证，
           走不了"声明 + 确认"那条路。 -->
      <div v-if="cur.channel === 'netease' && !cur.hosted" class="dsection">
        <p class="dseclabel tight">游戏区</p>
        <div class="regionpick">
          <button :class="{ on: !cur.region }" @click="store.setAccountRegion(cur.id, '')">先不选</button>
          <button :class="{ on: cur.region === 'android' }" @click="store.setAccountRegion(cur.id, 'android')">安卓</button>
          <button :class="{ on: cur.region === 'ios' }" @click="store.setAccountRegion(cur.id, 'ios')">iOS</button>
        </div>
        <p class="regionnote" :class="{ warn: !!cur.regionBlocked }">
          {{
            cur.regionBlocked
              ? cur.regionBlocked
              : cur.region
                ? "启动时光遇直接进" + (cur.regionLabel || "") + "，不再问选哪个区"
                : "启动时光遇会自己问选哪个区"
          }}
        </p>
      </div>
      <div v-else-if="cur.channel === 'netease' && cur.hosted" class="dsection">
        <p class="dseclabel tight">寄宿槽位</p>
        <p class="regionnote" :class="{ warn: expired }">
          {{
            expired
              ? `票来自「${hostName}」的寄宿槽；点「刷新凭证」会用这个槽自动续期（不用重扫），只有宿主自己也失效时才需要去刷新「${hostName}」`
              : `寄宿在「${hostName}」的槽位里，渠道由扫码时的手机决定，启动不再询问`
          }}
        </p>
      </div>

      <div class="dacts">
        <button v-if="runningSelf" class="btn" @click="store.stop(cur.id)"><Square />停止运行</button>
        <button v-else class="btn primary" @click="store.launch(cur.id)"><Play />启动光遇</button>
        <button class="btn" @click="store.refreshCred(cur.id)"><RefreshCw />刷新凭证</button>
        <button class="btn" title="创建这条账号的副本（可以在另一个区玩，互不影响）" @click="demoUi.openDuplicate(cur.id)"><CopyPlus />创建副本</button>
        <button class="btn" @click="demoUi.openMulti()"><Layers />多开</button>
      </div>

      <p class="dseclabel">运行中的客户端</p>
      <p v-if="!store.runningAccounts.length" class="dnone">没有客户端在运行</p>
      <div v-for="a in store.runningAccounts" :key="a.id" class="drun">
        <span class="ltxt">
          <b>{{ store.maskAll ? a.maskedName : a.name }}</b>
          <i>{{ channelLabel(a.channel) }} · 已运行 {{ fmtRun(a.runSeconds) }}</i>
        </span>
        <button class="btn sm" @click="store.stop(a.id)"><Square />停止</button>
      </div>
    </div>
    </template>

    <!-- 启动组视图：换内容不盖层。rail 保持可见，所以"我在哪一页"一直清楚 -->
    <LaunchGroups v-else embedded :store="store" @close="view = 'accounts'" />
  </div>
</template>

<style scoped>
.splitter {
  flex: none;
  width: 5px;
  cursor: col-resize;
  background: transparent;
  transition: background 0.15s;
}
.splitter:hover,
.splitter.dragging {
  background: var(--acc);
  opacity: 0.5;
}
.win {
  --bg: #09090a;
  --s1: #101012;
  --s2: #161619;
  --tx: #f4f2ef;
  --dim: #a5a29c;
  --faint: #8f8c85;
  --line: rgba(255, 255, 255, 0.08);
  --line2: rgba(255, 255, 255, 0.14);
  --acc: #d8b47c;
  --on: #211c15;
  --ok: #7fc9a0;
  --okbg: rgba(127, 201, 160, 0.1);
  --bad: #e08d7c;
  --badbg: rgba(224, 141, 124, 0.1);
  display: flex;
  flex-direction: row;
  height: 100%;
  background: var(--bg);
  color: var(--tx);
}

.rail {
  flex: none;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 6px;
  width: 56px;
  padding: 12px 0;
  border-right: 1px solid var(--line);
}
.rlogo {
  display: grid;
  place-items: center;
  height: 40px;
  width: 40px;
  color: var(--acc);
}
.rlogo svg {
  width: 18px;
  height: 18px;
}
.rico {
  display: grid;
  place-items: center;
  width: 38px;
  height: 38px;
  border: 0;
  border-radius: 10px;
  background: transparent;
  padding: 0;
  color: var(--faint);
  cursor: pointer;
}
.rico svg {
  width: 17px;
  height: 17px;
}
.rico:hover {
  color: var(--tx);
  background: color-mix(in srgb, var(--tx) 5%, transparent);
}
.rico.on {
  background: color-mix(in srgb, var(--acc) 14%, transparent);
  color: var(--acc);
}
.rico.bottom {
  margin-top: auto;
}

.list {
  flex: none;
  display: flex;
  flex-direction: column;
  width: 264px;
  min-height: 0;
  min-width: 220px;
  overflow: hidden;
  border-right: 1px solid var(--line);
  background: var(--s1);
}
/* ── 列表头部：两行，位置稳定 ──
   第一行 标题+排序+打码；第二行 视图分段。排序按钮在非平铺视图
   禁用而不是消失——消失会让后面的打码键跳位。 */
.lhead {
  display: flex;
  align-items: center;
  gap: 8px;
  height: 40px;
  padding: 0 8px 0 14px;
}
.lname {
  margin-right: auto;
  font-size: 11.5px;
  letter-spacing: 0.06em;
  color: var(--faint);
}
/* 视图切换：文字分段。图标版（列表/标签/箱子/时钟）实测没人看得出
   哪个是哪个，文字是最便宜的说明书。等宽四段，切换时不跳。 */
.vseg {
  display: flex;
  gap: 2px;
  margin: 0 8px 8px;
  padding: 2px;
  border-radius: 8px;
  background: color-mix(in srgb, var(--tx) 4%, transparent);
}
.vtab {
  flex: 1;
  height: 24px;
  border: 0;
  border-radius: 6px;
  background: transparent;
  color: var(--faint);
  font-family: inherit;
  font-size: 12px;
  cursor: pointer;
}
.vtab:hover {
  color: var(--tx);
}
.vtab.on {
  background: color-mix(in srgb, var(--tx) 10%, transparent);
  color: var(--acc);
  font-weight: 500;
}
.sortwrap {
  position: relative;
}
.sortbtn {
  display: flex;
  align-items: center;
  gap: 4px;
  height: 24px;
  padding: 0 7px;
  border: 1px solid transparent;
  border-radius: 5px;
  background: transparent;
  color: var(--faint);
  font-family: inherit;
  font-size: 11px;
  cursor: pointer;
}
.sortbtn svg {
  width: 11px;
  height: 11px;
}
.sortbtn:hover,
.sortbtn.on {
  color: var(--tx);
  border-color: var(--line);
}
.sortbtn:disabled {
  opacity: 0.35;
  cursor: default;
}
.sortbtn:disabled:hover {
  color: var(--faint);
  border-color: transparent;
}
.sortmenu {
  position: absolute;
  top: 26px;
  right: 0;
  z-index: 20;
  display: flex;
  flex-direction: column;
  gap: 1px;
  min-width: 88px;
  padding: 4px;
  border: 1px solid var(--line2);
  border-radius: 8px;
  background: var(--s2);
  box-shadow: 0 12px 28px rgba(0, 0, 0, 0.5);
}
.sortmenu button {
  height: 24px;
  padding: 0 8px;
  border: 0;
  border-radius: 5px;
  background: transparent;
  color: var(--dim);
  font-family: inherit;
  font-size: 11.5px;
  text-align: left;
  cursor: pointer;
}
.sortmenu button:hover {
  background: color-mix(in srgb, var(--tx) 6%, transparent);
  color: var(--tx);
}
.sortmenu button.on {
  color: var(--acc);
}
.ico.sm {
  width: 26px;
  height: 26px;
}
.lempty {
  margin: 18px 0;
  text-align: center;
  font-size: 12.5px;
  color: var(--faint);
}

/* ── 列表行：整行铺满 + 1px 分隔线（规范里"列表行不做卡片"）；
      选中 = 左侧 2px 金条 + 浮起底色。
      没有头像方块：它只重复名字首字，不承载信息，去掉后名字是唯一主角。 ── */
.rows {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  overflow-x: hidden;
  scrollbar-gutter: stable;
}
.sect + .sect {
  /* 段间只留 4px 呼吸：分隔本身交给段头色带 + 色带底边的线 */
  margin-top: 4px;
}
.rlist {
  display: flex;
  flex-direction: column;
}
.rlist .lrow:last-child {
  border-bottom: 0; /* 段的收尾交给段间的线，别叠出 2px */
}
.lrow {
  position: relative;
  display: flex;
  align-items: center;
  gap: 6px;
  min-height: 44px;
  height: 44px;
  box-sizing: border-box;
  padding: 0 10px;
  border-bottom: 1px solid var(--line);
  cursor: pointer;
  transition: background-color 0.14s ease-out;
}
.lrow:hover {
  background: color-mix(in srgb, var(--tx) 3.5%, transparent);
}
.lrow.on {
  background: var(--s2);
}
.lrow.on::before {
  content: "";
  position: absolute;
  left: 0;
  top: 0;
  bottom: 0;
  width: 2px;
  background: var(--acc);
}
/* 拖动中的行：抬起来一点，配合其它行的让位动画 */
.lrow.dragging {
  z-index: 2;
  background: var(--s2);
  box-shadow: 0 10px 24px rgba(0, 0, 0, 0.45);
}
.rl-move {
  transition: transform 0.16s ease-out;
}
.sel {
  flex: none;
  width: 15px;
  height: 15px;
  margin: 0;
  accent-color: var(--acc);
  cursor: pointer;
  opacity: 0;
  transition: opacity 0.15s ease-out;
}
.lrow:hover .sel,
.sel:checked {
  opacity: 1;
}
.ltxt {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}
/* 两行封顶：第一行 = 身份（名字 + 副本标记 + 标签贴右缘），
   第二行 = 状态（点 + 着色状态词 + 时长/渠道）。 */
.l1 {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
}
.l1 b {
  flex: 1;
  min-width: 0;
  font-size: 13px;
  font-weight: 600;
  letter-spacing: -0.01em;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.l2 {
  display: flex;
  align-items: center;
  min-width: 0;
}
.st {
  flex: 1;
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 5px;
  font-size: 11px;
  /* 副行整体用 dim 而不是 faint：faint 贴着 3:1 对比度下限，
     这行承载"能不能用"，暗到要凑近看就失去意义；再弱的层级交给状态词去做 */
  color: var(--dim);
  white-space: nowrap;
  overflow: hidden;
}
.st .dot {
  flex: none;
  width: 5px;
  height: 5px;
}
/* 状态词跟着圆点着色：绿=在跑、红=要处理；
   就绪是多数态，用"比渠道亮一档"的中性色——不抢运行/过期的戏，
   但要能和后面的渠道灰区分开 */
.sv {
  color: color-mix(in srgb, var(--tx) 42%, var(--dim));
}
.sv.run {
  color: var(--ok);
}
.sv.exp {
  color: var(--bad);
}
.chips {
  display: flex;
  gap: 3px;
  flex: 0 1 auto;
  min-width: 0;
  max-width: 38%;
  margin-left: auto;
}
.ladd {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 7px;
  height: 34px;
  margin: 10px 12px 14px;
  border: 1px dashed var(--line2);
  border-radius: 9px;
  background: transparent;
  font-family: inherit;
  font-size: 12.5px;
  color: var(--faint);
  cursor: pointer;
}
.ladd svg {
  width: 14px;
  height: 14px;
}
.ladd:hover {
  color: var(--tx);
}

.det {
  flex: 1;
  min-width: 0;
  padding: 26px 32px;
  overflow-y: auto;
}
.dhead {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
}
.deyebrow {
  margin: 0;
  font-size: 11.5px;
  letter-spacing: 0.04em;
  color: var(--faint);
}
.dtitle {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 6px;
}
.dtitle h3 {
  margin: 0;
  font-size: 24px;
  font-weight: 600;
  letter-spacing: -0.02em;
}
/* 游戏区切换：三态按钮组（先不选 / 安卓 / iOS） */
.dsection {
  margin: 2px 0 12px;
}
.regionpick {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
}
.regionpick button {
  flex: 1;
  min-width: 64px;
  padding: 7px 10px;
  border: 1px solid var(--line, #e3e5eb);
  border-radius: 8px;
  background: transparent;
  color: var(--text, #333);
  font-size: 13px;
  cursor: pointer;
}
.regionpick button.on {
  border-color: var(--gold, #c8a24a);
  background: rgba(200, 162, 74, 0.14);
  font-weight: 600;
}
.regionnote {
  margin: 6px 0 0;
  font-size: 12px;
  color: var(--faint);
}
/* 选了渠道但凭证用不了：用告警色，别让用户以为已经生效 */
.regionnote.warn {
  color: #d98b8b;
}

.dinfo {
  margin-top: 22px;
  border-top: 1px solid var(--line);
}
.dinfo > div {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 9px 0;
  border-bottom: 1px solid var(--line);
}
.dinfo span {
  font-size: 12.5px;
  color: var(--faint);
}
.dinfo b {
  font-weight: 400;
  font-size: 13px;
  color: var(--tx);
}
.credline {
  display: inline-flex;
  align-items: center;
  gap: 10px;
}
/* 「登录环境」那段紧跟在标签下面，不再重复画一条上边框 */
.dinfo.second {
  margin-top: 6px;
  border-top: none;
}
.derr {
  margin: 12px 0 0;
  padding: 8px 10px;
  border: 1px solid color-mix(in srgb, var(--bad) 24%, transparent);
  border-radius: 9px;
  background: var(--badbg);
  font-size: 12px;
  line-height: 1.5;
  color: var(--bad);
}
.dacts {
  display: flex;
  gap: 10px;
  margin-top: 22px;
}
/* 「更多信息」展开按钮：贴着详情列表底部，轻量一点 */
.dmore {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  margin-top: 12px;
  padding: 4px 0;
  border: 0;
  background: transparent;
  color: var(--faint);
  font-family: inherit;
  font-size: 12.5px;
  cursor: pointer;
}
.dmore:hover {
  color: var(--tx);
}
.dmore .chev {
  width: 14px;
  height: 14px;
  transition: transform 0.16s ease;
}
.dmore .chev.open {
  transform: rotate(180deg);
}
.dseclabel {
  margin: 34px 0 0;
  font-size: 11.5px;
  letter-spacing: 0.04em;
  color: var(--faint);
}
/* 展开区里的分组标签不用那么大间隔 */
.dseclabel.tight {
  margin-top: 22px;
}
.dnone {
  margin: 10px 0 0;
  font-size: 12.5px;
  color: var(--faint);
}
.drun {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 10px;
  padding: 10px 12px;
  border: 1px solid color-mix(in srgb, var(--ok) 24%, transparent);
  border-radius: 11px;
  background: var(--okbg);
}
.drun .btn {
  margin-left: auto;
}
.drun b {
  font-size: 13px;
  font-weight: 600;
}
.drun i {
  font-style: normal;
  font-size: 12px;
  color: var(--faint);
}
</style>
<style scoped>
.qpills {
  display: flex;
  flex-wrap: wrap;
  gap: 5px;
}
.qpills button {
  padding: 4px 12px;
  border: 1px solid var(--line2);
  border-radius: 999px;
  background: transparent;
  color: var(--dim);
  font-size: 12px;
  cursor: pointer;
  transition: all 0.12s;
}
.qpills button:hover {
  color: var(--tx);
  border-color: var(--faint);
}
.qpills button.on {
  background: var(--acc);
  border-color: var(--acc);
  color: var(--on);
  font-weight: 500;
}
.qsegs button {
  padding: 3px 10px;
  border: 1px solid var(--line);
  border-radius: 8px;
  background: transparent;
  color: var(--dim);
  font-size: 12px;
  cursor: pointer;
}
.qsegs button.on {
  border-color: var(--acc);
  color: var(--tx);
  background: var(--on);
}

/* ── 分段头（标签 / 渠道 / 时间共用）──
   一条可点击的色带：比行高矮、底色比列表底亮、字比行信息疏，
   三个维度同时拉开，滚动时吸顶——"我在哪个组"永远不用猜。
   背景必须不透明：吸顶时行从它底下穿过去，半透明会透字。 */
.shead {
  position: sticky;
  top: 0;
  z-index: 3; /* 高于拖动中的行(z2)：行滑过头时从段头底下穿过去 */
  display: flex;
  align-items: center;
  gap: 6px;
  height: 28px;
  padding: 0 12px 0 8px;
  background: color-mix(in srgb, var(--tx) 5%, var(--s1));
  border-bottom: 1px solid var(--line2);
  color: var(--dim);
  font-size: 12px;
  letter-spacing: 0.04em;
  cursor: pointer;
  user-select: none;
}
.shead:hover {
  background: color-mix(in srgb, var(--tx) 8%, var(--s1));
  color: var(--tx);
}
/* 整组已选中：段头亮起，和行内勾选框同一语义 */
.shead.allon .sname,
.shead.allon em {
  color: var(--acc);
}
.chevbtn {
  display: grid;
  place-items: center;
  width: 20px;
  height: 20px;
  margin-left: -4px;
  border: 0;
  border-radius: 4px;
  background: transparent;
  color: var(--dim);
  cursor: pointer;
}
.chevbtn:hover {
  background: color-mix(in srgb, var(--tx) 10%, transparent);
  color: var(--tx);
}
.sname {
  flex: 1;
  min-width: 0;
  margin: 0;
  padding: 0;
  border: 0;
  background: transparent;
  color: inherit;
  font: inherit;
  font-weight: 600;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  text-align: left;
  cursor: pointer;
}
.shead em {
  font-style: normal;
  font-size: 11px;
  color: var(--faint);
  font-variant-numeric: tabular-nums;
}
.chev {
  width: 12px;
  height: 12px;
  flex: none;
  color: var(--dim); /* 折叠箭头与组选择分开，点击含义保持明确 */
  transition: transform 0.15s ease-out;
}
.chev.closed {
  transform: rotate(-90deg);
}

/* 行内的标签：无彩色小块。为什么不用彩色：这个界面里颜色已经代表
   "可操作 / 当前"（金色），再给标签一套颜色就是第二套语义。
   字号 11px 是规范下限——标签是用户自己起的名，缩到 9.5px 就认不出了。 */
.tagchip {
  height: 16px;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  padding: 0 5px;
  border-radius: 4px;
  background: rgba(255, 255, 255, 0.07);
  color: var(--dim);
  font-size: 11px;
  line-height: 16px;
  font-style: normal;
  white-space: nowrap;
}
.tagchip.more {
  flex: none;
  background: transparent;
  padding: 0 2px;
}
.copybadge {
  display: inline-block;
  flex: none;
  height: 16px;
  padding: 0 5px;
  margin-left: 4px;
  border: 1px solid var(--line2);
  border-radius: 4px;
  color: var(--faint);
  font-size: 11px;
  font-weight: 400;
  line-height: 14px;
  vertical-align: 1px;
}

/* 详情里的标签编辑 */
.tagrow {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
}
.tagpick {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  height: 22px;
  padding: 0 4px 0 8px;
  border-radius: 5px;
  background: rgba(255, 255, 255, 0.06);
  color: var(--dim);
  font-size: 11.5px;
}
.tagpick .x {
  width: 15px;
  height: 15px;
  border: 0;
  border-radius: 4px;
  background: transparent;
  color: var(--faint);
  font-size: 12px;
  line-height: 1;
  cursor: pointer;
}
.tagpick .x:hover {
  background: rgba(255, 255, 255, 0.1);
  color: var(--tx);
}
.tagin {
  width: 132px;
  height: 22px;
  padding: 0 8px;
  border: 1px solid var(--line2);
  border-radius: 5px;
  background: transparent;
  color: var(--tx);
  font-family: inherit;
  font-size: 11.5px;
  outline: none;
}
.tagin:focus {
  border-color: rgba(216, 180, 124, 0.5);
}
/* 控件保留稳定位置，长名字优先占用空间，辅助操作在悬停或键盘聚焦时显露。 */
.lname { white-space: nowrap; color: var(--dim); font-size: 12px; letter-spacing: 0; }
.lname em { margin-left: 4px; font-size: 11px; font-style: normal; color: var(--faint); font-variant-numeric: tabular-nums; }
.lhead, .vseg, .listsearch, .listcontext, .listselection, .ladd { flex-shrink: 0; }
.sortmenu { min-width: 128px; }
.sortmenu button { display: flex; align-items: center; justify-content: space-between; gap: 12px; height: 30px; }
.sortmenu button svg { width: 13px; height: 13px; }
.listsearch { display: flex; align-items: center; gap: 7px; height: 30px; margin: 0 10px; padding: 0 8px; border: 1px solid var(--line); border-radius: 7px; background: var(--bg); }
.listsearch:focus-within { border-color: color-mix(in srgb, var(--acc) 65%, transparent); }
.listsearch > svg { width: 13px; height: 13px; flex: none; color: var(--faint); }
.listsearch input { width: 100%; min-width: 0; border: 0; outline: 0; background: transparent; color: var(--tx); font: inherit; font-size: 11.5px; }
.listsearch input::placeholder { color: var(--faint); }
.searchclear { display: grid; place-items: center; flex: none; width: 18px; height: 20px; padding: 0; border: 0; border-radius: 4px; background: transparent; color: var(--dim); cursor: pointer; }
.searchclear svg { width: 12px; height: 12px; }
.listcontext { display: flex; align-items: center; min-height: 29px; padding: 3px 12px; box-sizing: border-box; color: var(--faint); font-size: 11px; }
.listcontext span { overflow: hidden; white-space: nowrap; text-overflow: ellipsis; }
.listcontext.error, .tagfeedback.error { color: var(--bad); }
.listselection { display: flex; align-items: center; justify-content: space-between; gap: 5px; padding: 5px 12px; border-top: 1px solid var(--line); background: color-mix(in srgb, var(--acc) 7%, var(--s1)); color: var(--acc); font-size: 11px; }
.listselection button, .search-empty button { padding: 0; border: 0; background: transparent; color: var(--dim); font: inherit; cursor: pointer; }
.listselection button:hover, .search-empty button:hover { color: var(--acc); }
.lempty { flex: 1; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 10px; margin: 15px; min-height: 80px; line-height: 1.6; }
.rowgrip { display: grid; place-items: center; flex: none; width: 14px; height: 28px; padding: 0; margin: 0; border: 0; border-radius: 3px; background: transparent; color: var(--faint); opacity: 0.42; cursor: grab; touch-action: none; }
.rowgrip svg { width: 13px; height: 13px; }
.rowgrip:active { cursor: grabbing; }
.rowgrip:disabled { cursor: wait; opacity: 0.25; }
.lrow:hover .rowgrip, .rowgrip:focus-visible { opacity: 1; }
.lrow:focus-visible { outline: 1px solid var(--acc); outline-offset: -2px; }
.lrow:focus-within .sel, .rows:has(.sel:checked) .sel { opacity: 1; }
.lrow.picked:not(.on) { background: color-mix(in srgb, var(--acc) 5%, var(--s1)); }
.shead { gap: 5px; min-width: 0; height: 32px; padding-right: 8px; }
.chevbtn, .shead em { flex-shrink: 0; }
.section-check { flex: none; width: 12px; height: 12px; margin: 0 2px 0 0; accent-color: var(--acc); cursor: pointer; }
.sname small { margin-left: 5px; font-size: 11px; font-weight: 400; color: var(--faint); }
.groupmoves { display: flex; gap: 1px; flex: none; }
.groupmoves button { display: grid; place-items: center; width: 18px; height: 22px; padding: 0; border: 0; border-radius: 4px; background: transparent; color: var(--dim); opacity: 0.6; cursor: pointer; }
.groupmoves svg { width: 11px; height: 11px; }
.groupmoves button:hover:not(:disabled) { opacity: 1; color: var(--acc); background: color-mix(in srgb, var(--acc) 10%, transparent); }
.groupmoves button:disabled { opacity: 0.2; cursor: default; }
.shead.partial .sname { color: var(--tx); }
.tagheading { display: flex; align-items: baseline; flex-wrap: wrap; gap: 8px; margin-bottom: 10px; }
.tagheading .dseclabel { margin: 0; }
.tagheading .dseclabel > span { margin-left: 6px; font-size: 11px; color: var(--faint); letter-spacing: 0; }
.tagfeedback { min-height: 16px; margin-left: auto; font-size: 11px; color: var(--dim); }
.tag-empty { font-size: 12px; color: var(--faint); }
.tagpick { max-width: 100%; box-sizing: border-box; height: 27px; font-size: 12px; }
.tagpick > span { min-width: 0; overflow: hidden; white-space: nowrap; text-overflow: ellipsis; }
.tagpick .x { flex: none; width: 20px; height: 20px; }
.tagadd { display: flex; gap: 7px; max-width: 520px; margin-top: 10px; }
.tagadd .tagin { flex: 1; min-width: 0; width: auto; height: 32px; font-size: 12px; border-radius: 7px; box-sizing: border-box; }
.tagaddbtn { display: flex; align-items: center; justify-content: center; gap: 4px; flex: none; height: 32px; padding: 0 10px; border: 1px solid var(--line2); border-radius: 7px; background: var(--s2); color: var(--acc); font: inherit; font-size: 12px; cursor: pointer; }
.tagaddbtn svg { width: 13px; height: 13px; }
.tagaddbtn:disabled, .tagpick button:disabled { opacity: 0.4; cursor: default; }
.tagsuggestions { display: flex; align-items: center; flex-wrap: wrap; gap: 6px; margin-top: 9px; font-size: 11px; color: var(--faint); }
.tagsuggestions button { max-width: 155px; overflow: hidden; white-space: nowrap; text-overflow: ellipsis; padding: 3px 7px; border: 1px dashed var(--line2); border-radius: 5px; background: transparent; color: var(--dim); font: inherit; cursor: pointer; }
.tagsuggestions button:hover:not(:disabled) { color: var(--acc); border-color: color-mix(in srgb, var(--acc) 45%, transparent); }
.dhead > div { min-width: 0; width: 100%; }
.dtitle { flex-wrap: wrap; min-width: 0; }
.dtitle h3 { min-width: 0; overflow-wrap: anywhere; line-height: 1.4; }
.dtitle .ico, .dtitle .pill { flex-shrink: 0; }
button:focus-visible, input:focus-visible { outline: 1px solid var(--acc); outline-offset: 2px; }
@media (prefers-reduced-motion: reduce) { .rl-move { transition: none; } }
</style>
