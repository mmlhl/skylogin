<script setup lang="ts">
/**
 * 启动组（原来那句"「运行」聚合页规划中"）。
 *
 * 一套启动组 = 可重复执行的启动配方：
 *   组级  排列方式、默认画质/帧率/显示、启动行为（先停后启、间隔、自动开墙）
 *   成员  启动顺序、窗口位置（x/y/宽/高）、画质覆盖、显示覆盖、这次是否启用
 *   账号  它自己的画质（组和成员都没设时才用它）
 *
 * 分工：左栏只放"有哪些组"（能搜索、能新建/复制/删除）；组级配置收在右上角一条
 * 摘要里（点一下展开），成员表因此拿到整行宽度——组可能有很多，成员参数列也很多，
 * 这两件事不能挤在同一列里抢位置。
 *
 * 位置只允许一种来源：自动（按排列 + 启动顺序算，坐标只读）或手动（每个窗口自己坐标）。
 * 两种同时存在，就会出现"我改了顺序，位置却没变"的解释不通。
 */
import { computed, ref, watch } from "vue";
import {
  Play, Plus, Save, Trash2, Copy, X, Search, Crosshair, Grid2x2, ChevronUp, ChevronDown,
  Settings2, Monitor, Power,
} from "lucide-vue-next";
import type { Account, LaunchGroup, LaunchSlot, LauncherStore } from "../shellkit/contract";

const props = defineProps<{ store: LauncherStore }>();

/** 画质档位的中文名（游戏认识的档位名见 internal/prefs） */
const qualityLabelOf = (v: string) =>
  v === "LowPower" ? "低功耗" : v === "Default" ? "默认" : v === "HighQuality" ? "高画质" : v === "Presentation" ? "极致" : "跟随";
const store = props.store;

const q = ref("");
const gid = ref(store.groups[0]?.id ?? "");
const draft = ref<LaunchGroup | null>(null);
const slotKey = ref("");
const showSettings = ref(false);
const posMode = ref<"auto" | "manual">("auto");
const note = ref("");

const list = computed(() => (q.value ? store.groups.filter((g) => g.name.includes(q.value)) : store.groups));

/** 选中哪套 → 复制一份到 draft（直接改 store 里的对象会让"取消"变得不可能） */
function pick(id: string) {
  gid.value = id;
  const g = store.groups.find((x) => x.id === id);
  draft.value = g ? JSON.parse(JSON.stringify(g)) : null;
  slotKey.value = draft.value?.slots[0]?.accountKey ?? "";
  posMode.value = "auto";
  note.value = "";
}
watch(
  () => store.groups,
  () => {
    if (!draft.value || !store.groups.some((g) => g.id === gid.value)) pick(store.groups[0]?.id ?? "");
  },
  { immediate: true, deep: true }
);

const slots = computed(() => draft.value?.slots ?? []);
const curSlot = computed(() => slots.value.find((s) => s.accountKey === slotKey.value) ?? slots.value[0] ?? null);

/* ── 组级 ── */
const GRIDS: [number, number][] = [[1, 1], [2, 2], [3, 3], [4, 4]];
function cycleGrid() {
  if (!draft.value) return;
  const i = GRIDS.findIndex(([c, r]) => c === draft.value!.cols && r === draft.value!.rows);
  const [c, r] = GRIDS[(i + 1) % GRIDS.length];
  draft.value.cols = c;
  draft.value.rows = r;
}
const QUALITIES: { v: string; label: string }[] = [
  { v: "", label: "跟随账号" },
  { v: "LowPower", label: "低功耗" },
  { v: "Default", label: "默认" },
  { v: "HighQuality", label: "高画质" },
  { v: "Presentation", label: "极致" },
];
const FPSS = [0, 30, 60, 120];
const DISPLAYS: { v: string; label: string }[] = [
  { v: "", label: "跟随账号" },
  { v: "windowed", label: "窗口" },
  { v: "fullscreen", label: "全屏" },
];

/* ── 成员 ── */
const available = computed(() => {
  const used = new Set(slots.value.map((s) => s.accountKey));
  return props.store.accounts.filter((a) => !used.has(a.id));
});

/** 标签 → 还能加进来的账号数（"把大号全加进来"是这个界面最常用的一次操作） */
const tagPicks = computed(() => {
  const m = new Map<string, number>();
  for (const a of available.value) {
    for (const t of a.tags) m.set(t, (m.get(t) ?? 0) + 1);
  }
  return [...m.entries()].map(([name, count]) => ({ name, count }));
});

/** 按标签批量加入：一次把这一类账号全加进这套组 */
function addByTag(tag: string) {
  if (!draft.value) return;
  let n = 0;
  for (const a of [...available.value]) {
    if (!a.tags.includes(tag)) continue;
    addSlot(a);
    n++;
  }
  note.value = n ? `按「${tag}」加了 ${n} 个账号（顺序可再调）` : `「${tag}」里没有可加入的账号`;
}
function addSlot(a: Account) {
  if (!draft.value) return;
  draft.value.slots.push({
    accountKey: a.id,
    name: a.name,
    missing: false,
    mode: "",
    fps: 0,
    display: "",
    disabled: false,
    x: 0,
    y: 0,
    w: 0,
    h: 0,
    hasRect: false,
  });
  slotKey.value = a.id;
}
function removeSlot(i: number) {
  if (!draft.value) return;
  const s = draft.value.slots[i];
  draft.value.slots.splice(i, 1);
  if (slotKey.value === s.accountKey) slotKey.value = draft.value.slots[0]?.accountKey ?? "";
}
function move(i: number, dir: -1 | 1) {
  const to = i + dir;
  if (!draft.value || to < 0 || to >= draft.value.slots.length) return;
  const a = draft.value.slots;
  a.splice(to, 0, a.splice(i, 1)[0]);
}
const cycle = <T,>(arr: T[], v: T): T => arr[(arr.indexOf(v) + 1) % arr.length];

/** 按网格铺满：纯前端算（屏幕尺寸用保存时记的那个，没记就用 1920×1080） */
function fillGrid() {
  if (!draft.value) return;
  const W = draft.value.screenW || 1920;
  const H = draft.value.screenH || 1080;
  const cw = Math.floor(W / draft.value.cols);
  const ch = Math.floor(H / draft.value.rows);
  draft.value.slots.forEach((s, i) => {
    s.x = (i % draft.value!.cols) * cw;
    s.y = Math.floor(i / draft.value!.cols) * ch;
    s.w = cw;
    s.h = ch;
    s.hasRect = true;
  });
  posMode.value = "manual";
  note.value = `已按 ${draft.value.cols}×${draft.value.rows} 铺满（${W}×${H}）`;
}

function setManual() {
  if (!draft.value) return;
  // 切到手动：把"自动算出来的"先落到每个槽位，用户从这一刻开始接管
  const W = draft.value.screenW || 1920;
  const H = draft.value.screenH || 1080;
  const cw = Math.floor(W / draft.value.cols);
  const ch = Math.floor(H / draft.value.rows);
  draft.value.slots.forEach((s, i) => {
    if (s.hasRect) return;
    s.x = (i % draft.value!.cols) * cw;
    s.y = Math.floor(i / draft.value!.cols) * ch;
    s.w = cw;
    s.h = ch;
    s.hasRect = true;
  });
  posMode.value = "manual";
}

/* ── 新建 / 复制 / 删除 / 保存 ── */
function createGroup() {
  const base: LaunchGroup = {
    id: "",
    name: `新启动组 ${props.store.groups.length + 1}`,
    layout: "tile",
    cols: 2,
    rows: 2,
    defMode: "",
    defFps: 0,
    defDisplay: "",
    stopFirst: true,
    autoWall: true,
    gapSec: 1,
    screenW: 0,
    screenH: 0,
    slots: [],
  };
  props.store.saveGroup(base);
  showSettings.value = true;
}
function copyGroup() {
  if (!draft.value) return;
  props.store.saveGroup({ ...JSON.parse(JSON.stringify(draft.value)), id: "", name: `${draft.value.name} 副本` });
}
function removeGroup() {
  if (!draft.value) return;
  props.store.deleteGroup(draft.value.id);
}
function commit() {
  if (!draft.value) return;
  props.store.saveGroup(draft.value);
  note.value = "已保存";
}

/**
 * 启动这套：先把当前编辑落盘（否则用户改了位置直接点启动，启动的还是旧配置），
 * 再让后端去停旧客户端、拉起、按位置摆。
 */
async function launch() {
  if (!draft.value) return;
  if (!draft.value.id) {
    note.value = "先保存这套再启动";
    return;
  }
  props.store.saveGroup(draft.value);
  note.value = "正在启动…";
  const r = await props.store.launchGroup(draft.value.id);
  note.value = r.message || (r.ok ? "已启动" : "启动失败");
}

/**
 * 记住当前各窗口位置：读正在运行的窗口，按账号键对回槽位。
 *
 * 用账号键对而不是按顺序对：用户可能只开了这套里的一部分，
 * 按顺序对会把 B 的位置写到 C 身上。
 */
async function capture() {
  if (!draft.value) return;
  const rects = await props.store.captureRects();
  if (!rects.length) {
    note.value = "没有正在运行的客户端：先把这套启动一次、摆好窗口，再回来记";
    return;
  }
  let hit = 0;
  let full = 0;
  for (const r of rects) {
    const slot = draft.value.slots.find((s) => s.accountKey === r.id);
    if (!slot) continue;
    slot.x = r.x;
    slot.y = r.y;
    slot.w = r.w;
    slot.h = r.h;
    slot.hasRect = true;
    hit++;
    if (r.fullscreen) full++;
  }
  posMode.value = "manual";
  note.value = hit
    ? `已记住 ${hit} 个窗口的位置${full ? `（其中 ${full} 个是全屏，记下来就是整屏大小）` : ""}`
    : "这套里没有正在运行的账号";
}

/* ── 画布换算 ── */
const W = computed(() => draft.value?.screenW || 1920);
const H = computed(() => draft.value?.screenH || 1080);

/**
 * 某个槽位此刻"实际生效"的矩形。
 * 自动模式按排列+顺序算；手动模式（且记过位置）用槽位自己的坐标。
 * 画布、拖拽、坐标输入三处都走这个函数，避免三套算法算出来不一样。
 */
function effRect(s: LaunchSlot, i: number) {
  const d = draft.value!;
  if (posMode.value === "manual" && s.hasRect) {
    return { x: s.x, y: s.y, w: s.w, h: s.h };
  }
  const cw = Math.floor(W.value / d.cols);
  const ch = Math.floor(H.value / d.rows);
  return { x: (i % d.cols) * cw, y: Math.floor(i / d.cols) * ch, w: cw, h: ch };
}
function blockStyle(s: LaunchSlot, i: number) {
  const r = effRect(s, i);
  return {
    left: `${(r.x / W.value) * 100}%`,
    top: `${(r.y / H.value) * 100}%`,
    width: `${(r.w / W.value) * 100}%`,
    height: `${(r.h / H.value) * 100}%`,
  };
}

/* ── 画布上拖方块改位置 ──
 * 为什么允许拖：位置是空间信息，用四个数字改是在翻译自己脑子里的画面。
 * 拖的时候立刻切到"手动位置"——否则会出现"我拖了但它在自动排列下又弹回去"。
 */
const dragInfo = ref<{ key: string; startX: number; startY: number; origX: number; origY: number; cw: number; ch: number } | null>(null);

function onBlockDown(e: PointerEvent, s: LaunchSlot, i: number) {
  if (!draft.value) return;
  const host = (e.currentTarget as HTMLElement).parentElement;
  if (!host) return;
  const box = host.getBoundingClientRect();
  if (box.width < 8 || box.height < 8) return;
  // 自动模式下的位置是算出来的，先把它固化到槽位里，用户才拖得动
  if (!s.hasRect || posMode.value !== "manual") {
    const r = effRect(s, i);
    s.x = r.x;
    s.y = r.y;
    s.w = r.w;
    s.h = r.h;
    s.hasRect = true;
  }
  posMode.value = "manual";
  slotKey.value = s.accountKey;
  dragInfo.value = {
    key: s.accountKey,
    startX: e.clientX,
    startY: e.clientY,
    origX: s.x,
    origY: s.y,
    cw: box.width,
    ch: box.height,
  };
  window.addEventListener("pointermove", onBlockMove);
  window.addEventListener("pointerup", onBlockUp, { once: true });
}

function onBlockMove(e: PointerEvent) {
  const d = dragInfo.value;
  const g = draft.value;
  if (!d || !g) return;
  const slot = g.slots.find((s) => s.accountKey === d.key);
  if (!slot) return;
  // 画布像素 → 屏幕像素：画布是缩略图，位移要按比例放大回真实坐标
  const dx = ((e.clientX - d.startX) / d.cw) * W.value;
  const dy = ((e.clientY - d.startY) / d.ch) * H.value;
  const maxX = Math.max(0, W.value - slot.w);
  const maxY = Math.max(0, H.value - slot.h);
  slot.x = Math.min(maxX, Math.max(0, Math.round(d.origX + dx)));
  slot.y = Math.min(maxY, Math.max(0, Math.round(d.origY + dy)));
}

function onBlockUp() {
  window.removeEventListener("pointermove", onBlockMove);
  if (dragInfo.value) note.value = "位置已改，记得点保存";
  dragInfo.value = null;
}
</script>

<template>
  <div class="sheet">
    <header class="top">
      <div class="title">
        <Monitor class="ticon" />
        <h2>启动组</h2>
        <span class="sub">一套 = 账号组合 + 窗口位置 + 画质 + 启动行为</span>
      </div>
      <button class="close" title="关闭" @click="$emit('close')"><X /></button>
    </header>

    <div class="body">
      <!-- ── 左：只有组 ── -->
      <aside class="nav">
        <div class="nsearch">
          <Search class="si" />
          <input v-model="q" placeholder="搜索启动组" />
        </div>
        <div class="nlist">
          <button
            v-for="g in list"
            :key="g.id"
            class="nitem"
            :class="{ on: g.id === gid }"
            @click="pick(g.id)"
          >
            <span class="nname">{{ g.name }}</span>
            <span class="nmeta">
              {{ g.slots.length }} 个账号 · {{ g.layout === "tile" ? g.cols + "×" + g.rows : g.layout === "cascade" ? "堆叠" : "全屏" }}
              <em v-if="g.updated">· {{ g.updated }}</em>
            </span>
          </button>
          <p v-if="!list.length" class="nempty">{{ q ? "没有匹配的组" : "还没有启动组" }}</p>
        </div>
        <button class="nnew" @click="createGroup"><Plus />新建启动组</button>
      </aside>

      <!-- ── 右：一套组的配置 + 成员 ── -->
      <section v-if="draft" class="detail">
        <div class="dhead">
          <input v-model="draft.name" class="namein" />
          <div class="dacts">
            <button class="gbtn" title="复制这套" @click="copyGroup"><Copy /></button>
            <button class="gbtn" title="删除这套" @click="removeGroup"><Trash2 /></button>
            <button class="pbtn" @click="commit"><Save />保存</button>
            <button class="pbtn primary" @click="launch"><Play />启动这套</button>
          </div>
        </div>

        <!-- 组级配置：默认收起，只显示摘要；点摘要展开 -->
        <div class="sumrow">
          <button class="sumchip" @click="showSettings = !showSettings">
            <Settings2 />
            {{ draft.layout === "tile" ? `平铺 ${draft.cols}×${draft.rows}` : draft.layout === "cascade" ? "堆叠" : "全屏" }}
            · 默认 {{ draft.defMode ? qualityLabelOf(draft.defMode) : "跟随账号" }}{{ draft.defFps ? " " + draft.defFps + " 帧" : "" }}
            <ChevronDown :class="['chev', { open: showSettings }]" />
          </button>
          <span class="sumtext">
            {{ draft.stopFirst ? "先停后启" : "直接启动" }} · 间隔 {{ draft.gapSec }} 秒 · {{ draft.autoWall ? "自动开监控墙" : "不开监控墙" }}
          </span>
          <span v-if="note" class="note">{{ note }}</span>
        </div>

        <div v-if="showSettings" class="settings">
          <div class="srow">
            <span class="lb">排列</span>
            <div class="seg">
              <button :class="{ on: draft.layout === 'tile' }" @click="draft.layout = 'tile'">平铺</button>
              <button :class="{ on: draft.layout === 'cascade' }" @click="draft.layout = 'cascade'">堆叠</button>
              <button :class="{ on: draft.layout === 'fullscreen' }" @click="draft.layout = 'fullscreen'">全屏</button>
            </div>
            <button v-if="draft.layout === 'tile'" class="gridbtn" @click="cycleGrid">{{ draft.cols }}×{{ draft.rows }}</button>
          </div>
          <div class="srow">
            <span class="lb">默认画质</span>
            <div class="chips">
              <button v-for="x in QUALITIES" :key="x.v" :class="{ on: draft.defMode === x.v }" @click="draft.defMode = x.v">{{ x.label }}</button>
            </div>
            <span class="lb tiny">帧率</span>
            <div class="chips">
              <button v-for="f in FPSS" :key="f" :class="{ on: draft.defFps === f }" @click="draft.defFps = f">{{ f || "跟随" }}</button>
            </div>
          </div>
          <div class="srow">
            <span class="lb">默认显示</span>
            <div class="chips">
              <button v-for="d in DISPLAYS" :key="d.v" :class="{ on: draft.defDisplay === d.v }" @click="draft.defDisplay = d.v">{{ d.label }}</button>
            </div>
            <label class="chk"><input v-model="draft.stopFirst" type="checkbox" />启动前停掉正在跑的客户端</label>
            <label class="chk"><input v-model="draft.autoWall" type="checkbox" />全部就位后开监控墙</label>
            <label class="chk"><input v-model.number="draft.gapSec" type="number" min="0" max="30" class="gap" />秒间隔</label>
          </div>
        </div>

        <!-- 位置条：画布 + 选中窗口坐标。位置只允许一种来源 -->
        <div class="layoutbar">
          <div class="canvas">
            <div
              v-for="(s, i) in slots"
              :key="s.accountKey"
              class="wblock"
              :class="{ on: curSlot?.accountKey === s.accountKey, off: s.disabled, dragging: dragInfo?.key === s.accountKey }"
              :style="blockStyle(s, i)"
              @pointerdown="onBlockDown($event, s, i)"
              @click="slotKey = s.accountKey"
            >
              <b>{{ i + 1 }}</b>
            </div>
            <p v-if="!slots.length" class="cempty">还没有窗口</p>
          </div>

          <div class="lbar">
            <div class="modeseg">
              <button :class="{ on: posMode === 'auto' }" @click="posMode = 'auto'">自动排列</button>
              <button :class="{ on: posMode === 'manual' }" @click="setManual">手动位置</button>
            </div>
            <p class="modehint">
              {{ posMode === "auto"
                ? "位置由「排列方式 + 启动顺序」算出来，坐标只读；想自己摆就切到手动。"
                : "每个窗口用自己保存的坐标；启动顺序只决定谁先拉起，不决定落在哪。" }}
            </p>
            <div v-if="curSlot" class="coord">
              <span class="clabel">第 {{ slots.indexOf(curSlot) + 1 }} 个 · {{ curSlot.name }}</span>
              <label>X<input v-model.number="curSlot.x" type="number" :disabled="posMode === 'auto'" /></label>
              <label>Y<input v-model.number="curSlot.y" type="number" :disabled="posMode === 'auto'" /></label>
              <label>宽<input v-model.number="curSlot.w" type="number" :disabled="posMode === 'auto'" /></label>
              <label>高<input v-model.number="curSlot.h" type="number" :disabled="posMode === 'auto'" /></label>
              <label class="chk"><input v-model="curSlot.hasRect" type="checkbox" :disabled="posMode === 'auto'" />记住这个位置</label>
            </div>
            <div class="lacts">
              <button class="pbtn sm" @click="fillGrid"><Grid2x2 />按网格铺满</button>
              <button class="pbtn sm" title="读取正在运行的窗口坐标，按账号对回槽位" @click="capture">
                <Crosshair />记住当前位置
              </button>
              <span class="screensize">屏幕 {{ W }}×{{ H }}{{ draft.screenW ? "" : "（还没记过）" }}</span>
            </div>
          </div>
        </div>

        <!-- 成员表 -->
        <div class="thead">
          <span>#</span><span>账号</span><span>位置</span><span>画质</span><span>帧率</span><span>显示</span><span>启用</span><span />
        </div>
        <div class="tbody">
          <div
            v-for="(s, i) in slots"
            :key="s.accountKey"
            class="trow"
            :class="{ on: curSlot?.accountKey === s.accountKey, off: s.disabled }"
            @click="slotKey = s.accountKey"
          >
            <span class="idx"><b>{{ i + 1 }}</b></span>
            <span class="acc">
              <span class="ava">{{ s.name.slice(0, 1) }}</span>
              <span class="ameta">
                <b>{{ s.name }}</b>
                <span v-if="s.missing" class="miss">账号已删除</span>
              </span>
            </span>
            <span class="pos mono">
              {{ s.hasRect ? `${s.x},${s.y} · ${s.w}×${s.h}` : "未记录" }}
            </span>
            <span>
              <button class="cell" :class="{ inh: !s.mode }" @click.stop="s.mode = cycle(QUALITIES.map((x) => x.v), s.mode)">
                {{ s.mode ? qualityLabelOf(s.mode) : "跟随组" }}
              </button>
            </span>
            <span>
              <button class="cell" :class="{ inh: !s.fps }" @click.stop="s.fps = cycle(FPSS, s.fps)">
                {{ s.fps || "跟随" }}
              </button>
            </span>
            <span>
              <button class="cell" :class="{ inh: !s.display }" @click.stop="s.display = cycle(DISPLAYS.map((x) => x.v), s.display)">
                {{ s.display === "windowed" ? "窗口" : s.display === "fullscreen" ? "全屏" : "跟随组" }}
              </button>
            </span>
            <span class="en">
              <button class="sw" :class="{ on: !s.disabled }" :title="s.disabled ? '这次跳过' : '这次会启动'" @click.stop="s.disabled = !s.disabled"><i /></button>
            </span>
            <span class="ops">
              <button title="上移" @click.stop="move(i, -1)"><ChevronUp /></button>
              <button title="下移" @click.stop="move(i, 1)"><ChevronDown /></button>
              <button title="移出这套" class="del" @click.stop="removeSlot(i)"><X /></button>
            </span>
          </div>
          <p v-if="!slots.length" class="tempty">这套还没有账号，从下面挑几个加进来</p>
        </div>

        <footer class="addbar">
          <span class="atitle">加入这个组</span>
          <button v-for="a in available" :key="a.id" class="achip" @click="addSlot(a)">
            <Plus />{{ a.name }}
            <span v-for="t in a.tags.slice(0, 1)" :key="t" class="tag">{{ t }}</span>
          </button>
          <span v-if="!available.length" class="atitle">所有账号都在这套里了</span>
          <template v-if="tagPicks.length">
            <span class="asep" />
            <span class="atitle">按标签</span>
            <button v-for="t in tagPicks" :key="t.name" class="tagbtn" :title="`把「${t.name}」里的 ${t.count} 个账号全加进来`" @click="addByTag(t.name)">
              {{ t.name }} +{{ t.count }}
            </button>
          </template>
          <span class="tip"><Power />同一个账号只能占一个槽；副本是独立账号，可以各占一个</span>
        </footer>
      </section>

      <section v-else class="empty">
        <p>还没有启动组</p>
        <button class="pbtn primary" @click="createGroup"><Plus />新建启动组</button>
      </section>
    </div>
  </div>
</template>

<style scoped>
.sheet {
  /* 令牌自己带一份：这个面板挂在壳外面，壳里的 --s1/--acc 是各壳私有变量，
     拿不到就会渲染成一片透明。这里用 .ai/06 规范里那套中性值。 */
  --s0: #09090a;
  --s1: #101012;
  --s2: #161619;
  --line: rgba(255, 255, 255, 0.08);
  --line2: rgba(255, 255, 255, 0.14);
  --tx: #f4f2ef;
  --dim: #a5a29c;
  --faint: #6d6a65;
  --acc: #d8b47c;
  --on: #14120c;
  --bad: #c98070;
  position: fixed;
  inset: 0;
  z-index: 60;
  display: flex;
  flex-direction: column;
  background: var(--s0);
}
.top {
  display: flex;
  align-items: center;
  gap: 10px;
  height: 48px;
  flex: none;
  padding: 0 10px 0 16px;
  border-bottom: 1px solid var(--line);
}
.title {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}
.ticon {
  width: 15px;
  height: 15px;
  color: var(--acc);
}
.title h2 {
  margin: 0;
  font-size: 14px;
  font-weight: 600;
  letter-spacing: -0.01em;
}
.sub {
  font-size: 11.5px;
  color: var(--faint);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.close {
  display: grid;
  place-items: center;
  width: 28px;
  height: 28px;
  margin-left: auto;
  border: 0;
  border-radius: 7px;
  background: transparent;
  color: var(--faint);
  cursor: pointer;
}
.close svg {
  width: 15px;
  height: 15px;
}
.close:hover {
  background: rgba(255, 255, 255, 0.07);
  color: var(--tx);
}
.body {
  flex: 1;
  min-height: 0;
  display: flex;
}

/* 左：组列表 */
.nav {
  width: 232px;
  flex: none;
  display: flex;
  flex-direction: column;
  border-right: 1px solid var(--line);
  background: var(--s1);
}
.nsearch {
  position: relative;
  padding: 10px;
}
.nsearch .si {
  position: absolute;
  left: 20px;
  top: 50%;
  width: 12px;
  height: 12px;
  transform: translateY(-50%);
  color: var(--faint);
}
.nsearch input {
  width: 100%;
  height: 26px;
  padding: 0 8px 0 26px;
  border: 1px solid var(--line);
  border-radius: 6px;
  background: rgba(255, 255, 255, 0.03);
  color: var(--tx);
  font-family: inherit;
  font-size: 11.5px;
  outline: none;
}
.nlist {
  flex: 1;
  overflow: auto;
  padding: 0 8px;
}
.nitem {
  display: flex;
  flex-direction: column;
  gap: 2px;
  width: 100%;
  padding: 8px;
  border: 1px solid transparent;
  border-left: 2px solid transparent;
  border-radius: 7px;
  background: transparent;
  text-align: left;
  font-family: inherit;
  cursor: pointer;
}
.nitem:hover {
  background: rgba(255, 255, 255, 0.04);
}
.nitem.on {
  border-color: var(--line);
  border-left-color: var(--acc);
  background: var(--s2);
}
.nname {
  font-size: 12.5px;
  font-weight: 600;
  color: var(--tx);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.nmeta {
  font-size: 10.5px;
  color: var(--faint);
}
.nmeta em {
  font-style: normal;
}
.nempty {
  margin: 14px 8px;
  font-size: 11.5px;
  color: var(--faint);
}
.nnew {
  display: flex;
  align-items: center;
  gap: 6px;
  height: 32px;
  margin: 8px 10px 10px;
  padding: 0 9px;
  border: 1px dashed var(--line2);
  border-radius: 7px;
  background: transparent;
  color: var(--dim);
  font-family: inherit;
  font-size: 11.5px;
  cursor: pointer;
}
.nnew svg {
  width: 12px;
  height: 12px;
}
.nnew:hover {
  color: var(--tx);
  border-color: var(--acc);
}

/* 右：详情 */
.detail {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  padding: 12px 16px 0;
}
.dhead {
  display: flex;
  align-items: center;
  gap: 10px;
}
.namein {
  flex: 1;
  min-width: 0;
  height: 30px;
  padding: 0 8px;
  border: 1px solid transparent;
  border-radius: 7px;
  background: transparent;
  color: var(--tx);
  font-family: inherit;
  font-size: 17px;
  font-weight: 600;
  letter-spacing: -0.02em;
  outline: none;
}
.namein:hover {
  background: rgba(255, 255, 255, 0.04);
}
.namein:focus {
  border-color: rgba(216, 180, 124, 0.5);
  background: rgba(255, 255, 255, 0.03);
}
.dacts {
  display: flex;
  align-items: center;
  gap: 4px;
}
.gbtn {
  display: grid;
  place-items: center;
  width: 26px;
  height: 26px;
  border: 0;
  border-radius: 6px;
  background: transparent;
  color: var(--faint);
  cursor: pointer;
}
.gbtn svg {
  width: 13px;
  height: 13px;
}
.gbtn:hover {
  background: rgba(255, 255, 255, 0.07);
  color: var(--tx);
}
.pbtn {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  height: 28px;
  padding: 0 10px;
  border: 1px solid var(--line2);
  border-radius: 7px;
  background: transparent;
  color: var(--dim);
  font-family: inherit;
  font-size: 11.5px;
  cursor: pointer;
  white-space: nowrap;
}
.pbtn svg {
  width: 12px;
  height: 12px;
}
.pbtn:hover {
  color: var(--tx);
  background: rgba(255, 255, 255, 0.05);
}
.pbtn.sm {
  height: 24px;
  padding: 0 8px;
  font-size: 11px;
}
.pbtn.primary {
  border-color: transparent;
  background: var(--acc);
  color: var(--on);
  font-weight: 600;
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.16);
}

.sumrow {
  display: flex;
  align-items: center;
  gap: 10px;
  margin: 8px 0 0;
  min-width: 0;
}
.sumchip {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  height: 24px;
  padding: 0 8px;
  border: 1px solid var(--line);
  border-radius: 6px;
  background: transparent;
  color: var(--dim);
  font-family: inherit;
  font-size: 11px;
  cursor: pointer;
  white-space: nowrap;
}
.sumchip svg {
  width: 11px;
  height: 11px;
}
.chev {
  transition: transform 0.15s ease-out;
}
.chev.open {
  transform: rotate(180deg);
}
.sumtext {
  font-size: 11px;
  color: var(--faint);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.note {
  margin-left: auto;
  font-size: 11px;
  color: var(--acc);
  white-space: nowrap;
}

.settings {
  margin-top: 8px;
  padding: 10px 12px;
  border: 1px solid var(--line);
  border-radius: 8px;
  background: rgba(255, 255, 255, 0.02);
}
.srow {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 7px;
}
.srow:last-child {
  margin-bottom: 0;
}
.lb {
  width: 56px;
  flex: none;
  font-size: 11px;
  color: var(--faint);
}
.lb.tiny {
  width: auto;
  margin-left: 6px;
}
.seg {
  display: flex;
  gap: 2px;
  padding: 2px;
  border-radius: 6px;
  background: rgba(255, 255, 255, 0.04);
}
.seg button {
  height: 20px;
  padding: 0 8px;
  border: 0;
  border-radius: 4px;
  background: transparent;
  color: var(--faint);
  font-family: inherit;
  font-size: 11px;
  cursor: pointer;
}
.seg button.on {
  background: rgba(255, 255, 255, 0.09);
  color: var(--tx);
}
.gridbtn {
  height: 21px;
  padding: 0 7px;
  border: 1px solid var(--line);
  border-radius: 5px;
  background: transparent;
  color: var(--dim);
  font-family: inherit;
  font-size: 11px;
  cursor: pointer;
}
.chips {
  display: flex;
  flex-wrap: wrap;
  gap: 3px;
}
.chips button {
  height: 20px;
  padding: 0 7px;
  border: 1px solid transparent;
  border-radius: 5px;
  background: rgba(255, 255, 255, 0.04);
  color: var(--faint);
  font-family: inherit;
  font-size: 11px;
  cursor: pointer;
}
.chips button.on {
  border-color: rgba(216, 180, 124, 0.45);
  background: rgba(216, 180, 124, 0.1);
  color: var(--acc);
}
.chk {
  display: flex;
  align-items: center;
  gap: 5px;
  font-size: 11px;
  color: var(--dim);
  white-space: nowrap;
}
.chk input[type="checkbox"] {
  accent-color: var(--acc);
}
.chk .gap {
  width: 40px;
  height: 20px;
  padding: 0 5px;
  border: 1px solid var(--line2);
  border-radius: 5px;
  background: transparent;
  color: var(--tx);
  font-family: Consolas, monospace;
  font-size: 11px;
  outline: none;
}

/* 位置条 */
.layoutbar {
  display: flex;
  gap: 14px;
  margin-top: 10px;
  padding-bottom: 10px;
  border-bottom: 1px solid var(--line);
}
.canvas {
  position: relative;
  width: 208px;
  flex: none;
  aspect-ratio: 16 / 9;
  border: 1px solid var(--line2);
  border-radius: 6px;
  background: #0c0c0e;
  overflow: hidden;
}
.wblock {
  position: absolute;
  border: 1px solid rgba(216, 180, 124, 0.4);
  border-radius: 3px;
  background: rgba(216, 180, 124, 0.1);
  cursor: grab;
  touch-action: none;
}
.wblock b {
  display: block;
  padding: 0 3px;
  font-size: 9px;
  font-weight: 700;
  color: var(--acc);
}
.wblock.on {
  border-color: var(--acc);
  background: rgba(216, 180, 124, 0.24);
  box-shadow: 0 0 0 1px var(--acc);
}
.wblock.off {
  opacity: 0.3;
  border-style: dashed;
}
.wblock.dragging {
  cursor: grabbing;
  z-index: 2;
  background: rgba(216, 180, 124, 0.3);
  box-shadow: 0 6px 16px rgba(0, 0, 0, 0.45);
}
.cempty {
  position: absolute;
  inset: 0;
  display: grid;
  place-items: center;
  margin: 0;
  font-size: 11px;
  color: var(--faint);
}
.lbar {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.modeseg {
  display: flex;
  gap: 2px;
  padding: 2px;
  border-radius: 6px;
  background: rgba(255, 255, 255, 0.04);
  align-self: flex-start;
}
.modeseg button {
  height: 21px;
  padding: 0 9px;
  border: 0;
  border-radius: 4px;
  background: transparent;
  color: var(--faint);
  font-family: inherit;
  font-size: 11px;
  cursor: pointer;
}
.modeseg button.on {
  background: rgba(255, 255, 255, 0.09);
  color: var(--tx);
}
.modehint {
  margin: 0;
  font-size: 11px;
  color: var(--faint);
  line-height: 1.45;
}
.coord {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}
.clabel {
  font-size: 10.5px;
  color: var(--faint);
  white-space: nowrap;
}
.coord label {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 10.5px;
  color: var(--faint);
}
.coord input[type="number"] {
  width: 56px;
  height: 22px;
  padding: 0 5px;
  border: 1px solid var(--line2);
  border-radius: 5px;
  background: transparent;
  color: var(--tx);
  font-family: Consolas, monospace;
  font-size: 10.5px;
  outline: none;
}
.coord input:disabled {
  opacity: 0.4;
}
.lacts {
  display: flex;
  align-items: center;
  gap: 6px;
}
.screensize {
  font-size: 10.5px;
  color: var(--faint);
}

/* 成员表 */
.thead,
.trow {
  display: grid;
  grid-template-columns: 26px minmax(120px, 1fr) 132px 84px 52px 62px 36px 50px;
  gap: 8px;
  align-items: center;
}
.thead {
  padding: 6px 2px;
  font-size: 10.5px;
  color: var(--faint);
  border-bottom: 1px solid var(--line);
}
.tbody {
  flex: 1;
  overflow: auto;
}
.trow {
  height: 44px;
  padding: 0 2px;
  border-bottom: 1px solid var(--line);
  cursor: default;
}
.trow:hover {
  background: rgba(255, 255, 255, 0.03);
}
.trow.on {
  background: var(--s2);
  box-shadow: inset 2px 0 0 var(--acc);
}
.trow.off {
  opacity: 0.5;
}
.idx b {
  font-size: 11px;
  color: var(--dim);
}
.acc {
  display: flex;
  align-items: center;
  gap: 7px;
  min-width: 0;
}
.ava {
  display: grid;
  place-items: center;
  width: 22px;
  height: 22px;
  flex: none;
  border-radius: 6px;
  background: rgba(255, 255, 255, 0.06);
  color: var(--dim);
  font-size: 11px;
}
.ameta {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
}
.ameta b {
  font-size: 12.5px;
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.miss {
  flex: none;
  padding: 0 4px;
  border: 1px solid var(--line2);
  border-radius: 3px;
  color: var(--faint);
  font-size: 9.5px;
}
.pos {
  font-size: 10.5px;
  color: var(--dim);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.cell {
  height: 23px;
  padding: 0 7px;
  border: 1px solid var(--line);
  border-radius: 5px;
  background: transparent;
  color: var(--tx);
  font-family: inherit;
  font-size: 11px;
  cursor: pointer;
  white-space: nowrap;
}
.cell.inh {
  color: var(--faint);
  border-style: dashed;
}
.cell:hover {
  border-color: var(--acc);
}
.en {
  display: flex;
  justify-content: center;
}
.sw {
  position: relative;
  width: 26px;
  height: 15px;
  padding: 0;
  border: 1px solid var(--line2);
  border-radius: 999px;
  background: transparent;
  cursor: pointer;
}
.sw i {
  position: absolute;
  top: 1px;
  left: 1px;
  width: 11px;
  height: 11px;
  border-radius: 50%;
  background: var(--faint);
  transition: transform 0.15s ease-out, background-color 0.15s ease-out;
}
.sw.on {
  border-color: rgba(216, 180, 124, 0.5);
  background: rgba(216, 180, 124, 0.16);
}
.sw.on i {
  transform: translateX(11px);
  background: var(--acc);
}
.ops {
  display: flex;
  gap: 1px;
  justify-content: flex-end;
  opacity: 0;
}
.trow:hover .ops {
  opacity: 1;
}
.ops button {
  display: grid;
  place-items: center;
  width: 16px;
  height: 16px;
  border: 0;
  border-radius: 4px;
  background: transparent;
  color: var(--faint);
  cursor: pointer;
}
.ops button svg {
  width: 11px;
  height: 11px;
}
.ops button:hover {
  background: rgba(255, 255, 255, 0.08);
  color: var(--tx);
}
.ops .del:hover {
  color: var(--bad);
}
.tempty {
  margin: 16px 2px;
  font-size: 11.5px;
  color: var(--faint);
}
.addbar {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
  padding: 8px 0 10px;
  border-top: 1px solid var(--line);
}
.atitle {
  font-size: 10.5px;
  color: var(--faint);
}
.achip {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  height: 22px;
  padding: 0 7px;
  border: 1px solid var(--line);
  border-radius: 6px;
  background: transparent;
  color: var(--dim);
  font-family: inherit;
  font-size: 11px;
  cursor: pointer;
}
.achip svg {
  width: 10px;
  height: 10px;
  color: var(--faint);
}
.achip:hover {
  color: var(--tx);
  border-color: var(--acc);
}
.asep {
  width: 1px;
  height: 14px;
  margin: 0 2px;
  background: var(--line2);
}
/* 按标签批量加入：一次把一类账号全拉进来（这是"标签"在启动组里最大的用处） */
.tagbtn {
  height: 22px;
  padding: 0 7px;
  border: 1px solid transparent;
  border-radius: 6px;
  background: rgba(216, 180, 124, 0.1);
  color: var(--acc);
  font-family: inherit;
  font-size: 11px;
  cursor: pointer;
}
.tagbtn:hover {
  background: rgba(216, 180, 124, 0.18);
}
.tag {
  padding: 0 4px;
  border-radius: 3px;
  background: rgba(255, 255, 255, 0.05);
  color: var(--faint);
  font-size: 9.5px;
}
.tip {
  display: flex;
  align-items: center;
  gap: 4px;
  margin-left: auto;
  font-size: 10.5px;
  color: var(--faint);
}
.tip svg {
  width: 10px;
  height: 10px;
}
.empty {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 12px;
  color: var(--faint);
  font-size: 12px;
}
</style>
