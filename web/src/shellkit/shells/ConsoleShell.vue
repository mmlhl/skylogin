<script setup lang="ts">
/**
 * 壳 03 · 控制台：图标栏 + 账号列表 + 详情面板，类 Linear。
 * 信息密度最高，运行中的客户端在详情页统一管理。
 */
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import {
  Flame, LayoutGrid, Monitor, Settings, Eye, EyeOff, Plus, Pencil, Play, Square,
  Layers, RefreshCw, CopyPlus, SlidersHorizontal, ChevronDown,
  // 视图切换是文字分段（.vseg），不用图标——图标表意实测失败
  ArrowUpDown,
} from "lucide-vue-next";
import type { Account, LauncherStore } from "../contract";

const props = defineProps<{ store: LauncherStore }>();
const store = props.store;import { demoUi } from "../ui";
import { state as appState } from "../../composables/useAppState";
import { channelLabel, fmtRun } from "../contract";

const cur = computed(() => store.current);

// 账号列表宽度：可拖拽调节，存后端（设置里的 railWidth）
const railWidth = ref(appState.settings.railWidth >= 220 ? appState.settings.railWidth : 264);
const dragging = ref(false);
function onDividerDown(e: MouseEvent) {
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
    store.setRailWidth(railWidth.value);
  };
  window.addEventListener("mousemove", move);
  window.addEventListener("mouseup", up);
}
const expired = computed(() => cur.value?.cred === "expired");
const runningSelf = computed(() => !!cur.value?.running);

/* ───────────────── 账号列表的四种视图 ─────────────────
 * 用户 2026-11-15 定的规则：
 *   渠道 / 添加时间 是**单值**维度 → 用分段：每个号只出现一次
 *   标签 是**多值**维度 → 用分组：一个号有几个标签就在几个组里出现
 *   平铺 是不分组的老样子，顺序可以自己拖
 * 所以"重复出现"只可能来自标签视图，而且只是同一行的投影，不是新数据。
 */
type ListView = "flat" | "tag" | "channel" | "time";
const VIEW_TABS: { k: ListView; label: string; tip: string }[] = [
  { k: "flat", label: "平铺", tip: "一行一个号，顺序可拖动" },
  { k: "tag", label: "标签", tip: "多标签的号每组各出现一次" },
  { k: "channel", label: "渠道", tip: "按渠道分组" },
  { k: "time", label: "时间", tip: "按添加时间分组" },
];
const SORTS: { k: "custom" | "name" | "recent"; label: string; tip: string }[] = [
  { k: "custom", label: "自定义", tip: "手动拖动的顺序" },
  { k: "name", label: "名称", tip: "按名称排序" },
  { k: "recent", label: "最近登录", tip: "按最近登录排序" },
];
const listView = ref<ListView>("flat");
const flatSort = ref<"custom" | "name" | "recent">("custom");
const sortOpen = ref(false);
const sortLabel = computed(() => SORTS.find((s) => s.k === flatSort.value)?.label ?? "自定义");
const collapsedTags = ref<string[]>([]);
const UNTAGGED = "未分组";
const CHANNEL_ORDER = ["华为渠道", "网易官方", "B站渠道", "4399渠道", "vivo渠道"];
const TIME_ORDER = ["最近 7 天", "8–30 天", "30 天以上"];

// 排序菜单：点外面关掉。只挂一个 document 监听，比给每个按钮做 blur 可靠
function closeSort() {
  sortOpen.value = false;
}
onMounted(() => document.addEventListener("click", closeSort));
onUnmounted(() => document.removeEventListener("click", closeSort));

/** 标签的显示顺序：跟后端聚合清单一致（首次出现），"未分组"永远在最后 */
const groupOrder = ref<string[]>([]);
watch(
  () => (appState.tags ?? []).map((t) => t.name).join("|"),
  () => {
    // "未分组"不进 kept：它是在数据到达前就播进去的占位段，
    // 如果跟着 kept 走，第一次真实标签到来时它还赖在第一位。
    const names = [...new Set((appState.tags ?? []).map((t) => t.name))];
    const kept = groupOrder.value.filter((t) => t !== UNTAGGED && names.includes(t));
    groupOrder.value = [...kept, ...names.filter((t) => !kept.includes(t)), UNTAGGED];
  },
  { immediate: true }
);

function sorted(rows: Account[]) {
  const out = [...rows];
  // 排序只作用于"平铺"：标签/渠道/时间视图里顺序 = 文件顺序，
  // 否则会出现"按名称排好了、拖一下又被排回去"的假拖动。
  if (listView.value !== "flat") return out;
  if (flatSort.value === "name") out.sort((a, b) => a.name.localeCompare(b.name, "zh"));
  // 最近登录：没有时间戳的（从没成功启动过）排在最后
  if (flatSort.value === "recent") out.sort((a, b) => (b.lastLoginTs ?? 0) - (a.lastLoginTs ?? 0));
  return out;
}

/** 添加时间分成三段：最近一周 / 一个月内 / 更早 */
function timeBucket(a: Account) {
  const t = a.createdAt ?? 0;
  if (!t) return "30 天以上";
  const days = (Date.now() / 1000 - t) / 86400;
  return days <= 7 ? "最近 7 天" : days <= 30 ? "8–30 天" : "30 天以上";
}

const sections = computed<{ key: string; rows: Account[]; draggable: boolean }[]>(() => {
  const accs = store.accounts;
  if (listView.value === "flat") {
    return [{ key: "", rows: sorted(accs), draggable: true }];
  }
  if (listView.value === "tag") {
    return groupOrder.value.map((tag) => ({
      key: tag,
      rows: sorted(tag === UNTAGGED ? accs.filter((a) => !a.tags.length) : accs.filter((a) => a.tags.includes(tag))),
      // 组内可以调顺序；跨组移动没有意义（那等于改标签），所以只认同组
      draggable: true,
    })).filter((s) => s.rows.length); // 标签删空后段要消失，别留一个"0"
  }
  if (listView.value === "channel") {
    const names = Array.from(new Set(accs.map((a) => channelLabel(a.channel))));
    const order = [...CHANNEL_ORDER.filter((n) => names.includes(n)), ...names.filter((n) => !CHANNEL_ORDER.includes(n))];
    return order.map((name) => ({
      key: name,
      rows: sorted(accs.filter((a) => channelLabel(a.channel) === name)),
      draggable: false,
    }));
  }
  return TIME_ORDER.map((k) => ({
    key: k,
    rows: sorted(accs.filter((a) => timeBucket(a) === k)),
    draggable: false,
  })).filter((s) => s.rows.length);
});

/** 标签视图里，行内只显示"别的标签"：当前这一组的标签已经由组头表达了 */
function tagsOf(a: Account, group: string) {
  return listView.value === "tag" ? a.tags.filter((t) => t !== group) : a.tags;
}

/** 行的悬停提示：只解释两类"看起来不对劲"的时刻——
 *  标签视图里同一个号出现多次（是投影不是重复数据），
 *  和排序视图里拖不动（顺序由规则决定，不是卡了）。 */
function rowTitle(a: Account) {
  if (listView.value === "tag" && a.tags.length > 1) {
    return "多标签账号，每组各出现一次";
  }
  if (listView.value === "flat" && flatSort.value !== "custom") {
    return `按${sortLabel.value}排序，不能拖动`;
  }
  if (listView.value === "channel" || listView.value === "time") {
    return "分组视图，不能拖动";
  }
  return undefined;
}

function toggleTag(tag: string) {
  const i = collapsedTags.value.indexOf(tag);
  if (i >= 0) collapsedTags.value.splice(i, 1);
  else collapsedTags.value.push(tag);
}

/* ── 点段头 = 选中这一组 ──
 * 段头是列表里最大的点击目标，留给最常用的动作：把整组变成批量操作的目标
 * （配合底部批量条：多开 / 停止 / 删除）。折叠退到左侧箭头上。
 * 再点一次 = 取消这一组；与其它组的选择互不影响，可以连点多组。 */
function secSelectedAll(sec: { rows: Account[] }) {
  return sec.rows.length > 0 && sec.rows.every((a) => store.selectedIds.includes(a.id));
}
function toggleSelectSec(sec: { rows: Account[] }) {
  const target = !secSelectedAll(sec);
  for (const a of sec.rows) {
    if (store.selectedIds.includes(a.id) !== target) store.toggleSelect(a.id);
  }
}

/* 拖动排序：只改顺序，不改内容（后端按整表顺序落盘） */
/* ───────────── 拖动排序（指针事件 + FLIP 位移） ─────────────
 * 为什么不用 HTML5 的 draggable：
 *   1. 它自带一张"整行截图"的幽灵图，样式不受控，松手前看不出落点；
 *   2. dragover 每移动一像素就触发一次，行一多就掉帧；
 *   3. 没有位移动画，行是"跳"过去的，手感像十年前的网页。
 * 现在的做法：pointerdown 记起点 → 移动超过 4px 才进入拖拽（否则当成点击选中）
 * → 实时把这一行插到指针位置，其余行用 CSS transform 过渡让位（FLIP）。
 * 只允许"自定义顺序"下拖：按名称/最近登录排出来的顺序是算出来的，
 * 让人拖完再被规则覆盖回去，才是真的手感差。
 */
const listScroll = ref<HTMLElement | null>(null);
const draggingId = ref("");
const dragSec = ref("");
/** 拖动过程中"这一段"的临时顺序（只影响显示，松手才落盘） */
const dragIds = ref<string[]>([]);
let dragStartY = 0;
let dragArmed = false;
let dragSecKey = "";
let dragAccountId = "";

function canDragSec(sec: { draggable: boolean }) {
  if (!sec.draggable) return false;
  // 平铺：只有"自定义"顺序能拖（按名称/最近登录是算出来的，拖了也会被排回去）
  // 标签：顺序恒为文件顺序，可以拖
  return listView.value === "tag" || flatSort.value === "custom";
}

function onRowPointerDown(e: PointerEvent, sec: { key: string; rows: Account[]; draggable: boolean }, a: Account) {
  if (e.button !== 0 || !canDragSec(sec)) return;
  const el = e.target as HTMLElement;
  // 复选框、按钮上的按下不算拖拽
  if (el.closest("input, button")) return;
  dragStartY = e.clientY;
  dragArmed = false;
  dragSecKey = sec.key;
  dragAccountId = a.id;
  dragIds.value = sec.rows.map((x) => x.id);
  window.addEventListener("pointermove", onRowPointerMove);
  window.addEventListener("pointerup", onRowPointerUp, { once: true });
}

function onRowPointerMove(e: PointerEvent) {
  if (!dragArmed && Math.abs(e.clientY - dragStartY) < 4) return;
  if (!dragArmed) {
    dragArmed = true;
    draggingId.value = dragAccountId;
    dragSec.value = dragSecKey;
    document.body.style.cursor = "grabbing";
  }
  // 指针在滚动区边缘时自动滚动：行多的时候不滚动根本拖不到顶/底
  const box = listScroll.value;
  if (box) {
    const r = box.getBoundingClientRect();
    if (e.clientY < r.top + 28) box.scrollTop -= 10;
    else if (e.clientY > r.bottom - 28) box.scrollTop += 10;
  }
  const rows = Array.from(box?.querySelectorAll<HTMLElement>(`[data-sec="${cssEscape(dragSecKey)}"] .lrow`) ?? []);
  if (!rows.length) return;
  // 落点 = 第一个"指针还没越过它中线"的行之前；都越过了就放最后
  let at = rows.length;
  for (let i = 0; i < rows.length; i++) {
    const r = rows[i].getBoundingClientRect();
    if (e.clientY < r.top + r.height / 2) {
      at = i;
      break;
    }
  }
  const rest = dragIds.value.filter((id) => id !== dragAccountId);
  const from = dragIds.value.indexOf(dragAccountId);
  if (from < at) at -= 1; // 自己占了一格，后面的位置要回退一位
  if (at < 0) at = 0;
  if (at > rest.length) at = rest.length;
  const next = [...rest];
  next.splice(at, 0, dragAccountId);
  if (next.join("|") !== dragIds.value.join("|")) dragIds.value = next;
}

function onRowPointerUp() {
  window.removeEventListener("pointermove", onRowPointerMove);
  document.body.style.cursor = "";
  const wasArmed = dragArmed;
  const secKey = dragSecKey;
  const tmp = dragIds.value;
  draggingId.value = "";
  dragSec.value = "";
  dragArmed = false;
  if (!wasArmed) return;

  // 段内顺序 → 全局顺序：只在这段占用的那几个位置上做置换，
  // 别的段的位置一律不动（标签视图里跨段移动没有意义，那等于改标签）。
  const members = new Set(tmp);
  let k = 0;
  const global = store.accounts.map((a) => a.id).map((id) => (members.has(id) ? tmp[k++] : id));
  // 拖完立刻生效；下一次轮询会拿服务端的顺序对齐
  store.reorder(global);
  void secKey;
}

/** CSS.escape 在极老的 WebView 里没有，这里只处理标签名里可能出现的引号 */
function cssEscape(s: string) {
  return s.replace(/["\\]/g, "\\$&");
}

/** 某一段当前要显示的顺序（拖动中用它，松手后回到 store 的顺序） */
function rowsOf(sec: { key: string; rows: Account[] }) {
  if (dragSec.value !== sec.key || !dragIds.value.length) return sec.rows;
  const byId = new Map(sec.rows.map((a) => [a.id, a]));
  return dragIds.value.map((id) => byId.get(id)).filter((a): a is Account => !!a);
}

/* 标签编辑：改哪个号就只动哪个号（副本与原件的标签互相独立） */
const tagDraft = ref("");
function addTag(a: Account) {
  const v = tagDraft.value.trim();
  if (!v || a.tags.includes(v)) return;
  store.setTags(a.id, [...a.tags, v]);
  tagDraft.value = "";
}
function removeTag(a: Account, t: string) {
  store.setTags(a.id, a.tags.filter((x) => x !== t));
}

// 被寄宿的号：找出提供槽位的那个宿主账号，给用户看名字而不是一串 ID。
// 宿主可能已经被删掉了，那种情况下标出来——用户需要知道这条关系已经悬空。
const hostName = computed(() => {
  const id = cur.value?.hostedBy;
  if (!id) return "";
  const hit = store.accounts.find((x) => x.playerId === id || x.fullId === id);
  return hit ? hit.name : `已删除的账号 ${id.slice(0, 6)}…`;
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
      <button class="rico on" title="账号"><LayoutGrid /></button>
      <button class="rico" title="运行" @click="store.openRunning()"><Monitor /></button>
      <button class="rico" title="监控墙（实时小窗）" @click="store.toggleWall()"><LayoutDashboard /></button>
      <button class="rico bottom" title="设置" @click="store.openSettings()"><Settings /></button>
    </div>

    <div class="list" :style="{ width: railWidth + 'px' }">
      <!-- 头部两行，控件位置在任何视图下都不动：
           第一行：标题 + 排序（非平铺时禁用占位，不消失——消失会让打码键跳位）
           第二行：文字分段切换视图。图标版实测看不出功能，文字不用猜。 -->
      <div class="lhead">
        <span class="lname">账号</span>
        <div class="sortwrap">
          <button
            class="sortbtn"
            :class="{ on: sortOpen }"
            :disabled="listView !== 'flat'"
            :title="listView === 'flat' ? '列表排序' : '排序只在平铺视图可用'"
            @click.stop="sortOpen = !sortOpen"
          >
            <ArrowUpDown /><span>{{ sortLabel }}</span>
          </button>
          <div v-if="sortOpen && listView === 'flat'" class="sortmenu">
            <button
              v-for="o in SORTS"
              :key="o.k"
              :class="{ on: flatSort === o.k }"
              :title="o.tip"
              @click.stop="flatSort = o.k; sortOpen = false"
            >
              {{ o.label }}
            </button>
          </div>
        </div>
        <button class="ico sm" :title="store.maskAll ? '显示完整信息' : '打码'" @click="store.toggleMask()">
          <EyeOff v-if="store.maskAll" :style="{ width: '14px', height: '14px' }" /><Eye v-else :style="{ width: '14px', height: '14px' }" />
        </button>
      </div>
      <div class="vseg">
        <button
          v-for="v in VIEW_TABS"
          :key="v.k"
          class="vtab"
          :class="{ on: listView === v.k }"
          :title="v.tip"
          @click="listView = v.k"
        >
          {{ v.label }}
        </button>
      </div>

      <p v-if="!store.accounts.length" class="lempty">还没有账号</p>
      <div v-else ref="listScroll" class="rows">
        <section v-for="sec in sections" :key="listView + sec.key" class="sect" :data-sec="sec.key">
          <div
            v-if="sec.key"
            class="shead"
            :class="{ allon: secSelectedAll(sec) }"
            :title="listView === 'tag' ? '点击选中整组；箭头折叠' : '点击选中整组'"
            @click="toggleSelectSec(sec)"
          >
            <button
              v-if="listView === 'tag'"
              class="chevbtn"
              title="折叠 / 展开这一组"
              @click.stop="toggleTag(sec.key)"
            >
              <ChevronDown class="chev" :class="{ closed: collapsedTags.includes(sec.key) }" />
            </button>
            <span class="sname">{{ sec.key }}</span>
            <em>{{ sec.rows.length }}</em>
          </div>
          <!-- TransitionGroup 提供位移过渡：拖动时其余行是"让位"而不是"跳位" -->
          <TransitionGroup
            v-if="listView !== 'tag' || !collapsedTags.includes(sec.key)"
            name="rl"
            tag="div"
            class="rlist"
          >
            <div
              v-for="a in rowsOf(sec)"
              :key="a.id"
              class="lrow"
              :class="{ on: a.current, dragging: draggingId === a.id }"
              :title="rowTitle(a)"
              @pointerdown="onRowPointerDown($event, sec, a)"
              @click="store.select(a.id)"
            >
              <input
                class="sel"
                type="checkbox"
                :checked="store.selectedIds.includes(a.id)"
                title="选中该账号"
                @click.stop
                @change="store.toggleSelect(a.id)"
              />
              <span class="ltxt">
                <!-- 第一行：身份。名字是唯一主角，标签贴右缘——
                     身份和分类读成一条水平线，第二行只留状态 -->
                <span class="l1">
                  <b>{{ store.maskAll ? a.maskedName : a.name }}</b>
                  <span v-if="a.copyKey" class="copybadge" title="副本：独立记录，凭证与原件共享">副本</span>
                  <span v-if="tagsOf(a, sec.key).length" class="chips">
                    <span v-for="t in tagsOf(a, sec.key).slice(0, 2)" :key="t" class="tagchip">{{ t }}</span>
                    <span v-if="tagsOf(a, sec.key).length > 2" class="tagchip more">+{{ tagsOf(a, sec.key).length - 2 }}</span>
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
    <div class="splitter" :class="{ dragging }" @mousedown="onDividerDown"></div>

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
        <p class="dseclabel tight">标签</p>
        <div class="tagrow">
          <span v-for="t in cur.tags" :key="t" class="tagpick">
            {{ t }}<button class="x" title="移除这个标签" @click="removeTag(cur, t)">×</button>
          </span>
          <input v-model="tagDraft" class="tagin" placeholder="输入标签后回车" @keyup.enter="addTag(cur)" />
        </div>
        <p class="regionnote">标签只存在本机，用来分组和筛选；副本与原件各存各的。</p>
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
  --bg: #0e1013;
  --s1: #15181d;
  --s2: #1b1f26;
  --tx: #eef1f4;
  --dim: #98a2ae;
  --faint: #64707c;
  --line: rgba(255, 255, 255, 0.08);
  --line2: rgba(255, 255, 255, 0.14);
  --acc: #6d9aef;
  --on: #0b1526;
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
  overflow: auto;
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
  gap: 10px;
  height: 44px;
  padding: 0 12px 0 10px;
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
  gap: 4px;
  flex: none;
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
  font-weight: 600;
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
  color: var(--dim); /* 折叠是段头唯一的动作，箭头别淡到看不见 */
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
  background: transparent;
  padding: 0 2px;
}
.copybadge {
  display: inline-block;
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
</style>
