<script setup lang="ts">
/**
 * 壳 03 · 控制台：图标栏 + 账号列表 + 详情面板，类 Linear。
 * 信息密度最高，运行中的客户端在详情页统一管理。
 */
import { computed, ref, watch } from "vue";
import { Flame, LayoutGrid, LayoutDashboard, Monitor, Settings, Eye, EyeOff, Plus, Pencil, Play, Square, Layers, RefreshCw, CopyPlus, SlidersHorizontal, ChevronDown } from "lucide-vue-next";
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
const VIEW_TABS: { k: ListView; label: string }[] = [
  { k: "flat", label: "平铺" },
  { k: "tag", label: "标签" },
  { k: "channel", label: "渠道" },
  { k: "time", label: "时间" },
];
const listView = ref<ListView>("flat");
const flatSort = ref<"custom" | "name" | "recent">("custom");
const collapsedTags = ref<string[]>([]);
const UNTAGGED = "未分组";
const CHANNEL_ORDER = ["华为渠道", "网易官方", "B站渠道", "4399渠道", "vivo渠道"];
const TIME_ORDER = ["最近 7 天", "8–30 天", "30 天以上"];

/** 标签的显示顺序：跟后端聚合清单一致（首次出现），"未分组"永远在最后 */
const groupOrder = ref<string[]>([]);
watch(
  () => (appState.tags ?? []).map((t) => t.name).join("|"),
  () => {
    const all = [...(appState.tags ?? []).map((t) => t.name), UNTAGGED];
    const kept = groupOrder.value.filter((t) => all.includes(t));
    groupOrder.value = [...kept, ...all.filter((t) => !kept.includes(t))];
  },
  { immediate: true }
);

function sorted(rows: Account[]) {
  const out = [...rows];
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
    }));
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

function toggleTag(tag: string) {
  const i = collapsedTags.value.indexOf(tag);
  if (i >= 0) collapsedTags.value.splice(i, 1);
  else collapsedTags.value.push(tag);
}

/* 拖动排序：只改顺序，不改内容（后端按整表顺序落盘） */
const dragRow = ref<{ sec: string; id: string } | null>(null);
const dropTarget = ref("");
function onDropRow(secKey: string, targetId: string) {
  const d = dragRow.value;
  if (!d || d.sec !== secKey) return;
  const ids = store.accounts.map((a) => a.id);
  const from = ids.indexOf(d.id);
  const to = ids.indexOf(targetId);
  if (from < 0 || to < 0 || from === to) return;
  ids.splice(to, 0, ids.splice(from, 1)[0]);
  store.reorder(ids);
  dragRow.value = null;
  dropTarget.value = "";
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
      <div class="lhead">
        <span>账号</span>
        <button class="ico sm" :title="store.maskAll ? '显示完整信息' : '打码'" @click="store.toggleMask()">
          <EyeOff v-if="store.maskAll" :style="{ width: '14px', height: '14px' }" /><Eye v-else :style="{ width: '14px', height: '14px' }" />
        </button>
      </div>

      <!-- 四种视图：平铺 / 标签 / 渠道 / 时间（四个都改同一个列表，不开新页面） -->
      <div class="vbar">
        <button v-for="v in VIEW_TABS" :key="v.k" class="vbtn" :class="{ on: listView === v.k }" @click="listView = v.k">
          {{ v.label }}
        </button>
      </div>
      <div class="sbar">
        <template v-if="listView === 'flat'">
          <span>顺序</span>
          <button :class="{ on: flatSort === 'custom' }" @click="flatSort = 'custom'">自定义</button>
          <button :class="{ on: flatSort === 'name' }" @click="flatSort = 'name'">名称</button>
          <button :class="{ on: flatSort === 'recent' }" @click="flatSort = 'recent'">最近登录</button>
        </template>
        <span v-else-if="listView === 'tag'" class="hint">多标签的号会在每个标签下各出现一次</span>
        <span v-else class="hint">{{ listView === "channel" ? "按渠道排：每个号只出现一次" : "按添加时间排：每个号只出现一次" }}</span>
      </div>

      <p v-if="!store.accounts.length" class="lempty">还没有账号</p>
      <div v-else class="rows">
        <section v-for="sec in sections" :key="listView + sec.key" class="sect">
          <div v-if="sec.key" class="shead" :class="{ nograb: !sec.draggable }" @click="toggleTag(sec.key)">
            <ChevronDown v-if="listView === 'tag'" class="chev" :class="{ closed: collapsedTags.includes(sec.key) }" />
            <span class="sname">{{ sec.key }}</span>
            <em>{{ sec.rows.length }}</em>
          </div>
          <template v-if="listView !== 'tag' || !collapsedTags.includes(sec.key)">
            <div
              v-for="a in sec.rows"
              :key="sec.key + a.id"
              class="lrow"
              :class="{ on: a.current, dragging: dragRow?.id === a.id, over: dropTarget === sec.key + a.id }"
              :draggable="sec.draggable"
              @dragstart="sec.draggable && (dragRow = { sec: sec.key, id: a.id })"
              @dragend="dragRow = null; dropTarget = ''"
              @dragover.prevent="dragRow && dragRow.sec === sec.key && (dropTarget = sec.key + a.id)"
              @drop="dragRow && onDropRow(sec.key, a.id)"
              @click="store.select(a.id)"
            >
              <input
                type="checkbox"
                class="sel"
                :checked="store.selectedIds.includes(a.id)"
                title="选中该账号"
                @click.stop
                @change="store.toggleSelect(a.id)"
              />
              <span class="lava">{{ a.name.slice(0, 1) }}</span>
              <span class="ltxt">
                <b>
                  {{ store.maskAll ? a.maskedName : a.name }}
                  <span v-if="a.copyKey" class="copybadge" title="这是一个副本：独立记录，凭证与原件共享">副本</span>
                </b>
                <i v-if="tagsOf(a, sec.key).length" class="tagline">
                  <span v-for="t in tagsOf(a, sec.key)" :key="t" class="tagchip">{{ t }}</span>
                </i>
                <i>
                  <em v-if="a.running" class="dot ok live" />
                  <em v-else-if="a.cred === 'expired'" class="dot bad" />
                  <em v-else class="dot ok" />
                  {{ a.running ? "运行中 · " + fmtRun(a.runSeconds) : a.cred === "expired" ? "凭证过期" : "就绪" }} · {{ channelLabel(a.channel) }}
                </i>
              </span>
            </div>
          </template>
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
        <span class="lava">{{ a.name.slice(0, 1) }}</span>
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
  --line: rgba(255, 255, 255, 0.07);
  --line2: rgba(255, 255, 255, 0.12);
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
.lhead {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 14px 16px 6px;
  font-size: 11.5px;
  letter-spacing: 0.04em;
  color: var(--faint);
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
.lrow {
  position: relative;
  display: flex;
  align-items: center;
  gap: 10px;
  margin: 1px 8px;
  padding: 8px 10px;
  border-radius: 9px;
  cursor: pointer;
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
.lrow:hover {
  background: color-mix(in srgb, var(--tx) 4%, transparent);
}
.lrow.on {
  background: var(--s2);
}
.lrow.on::before {
  content: "";
  position: absolute;
  left: -8px;
  top: 9px;
  bottom: 9px;
  width: 2px;
  border-radius: 1px;
  background: var(--acc);
}
.lava {
  display: grid;
  place-items: center;
  flex: none;
  width: 30px;
  height: 30px;
  border-radius: 8px;
  background: color-mix(in srgb, var(--tx) 8%, transparent);
  font-size: 12px;
  color: var(--dim);
}
.ltxt {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 1px;
}
.ltxt b {
  font-size: 13.5px;
  font-weight: 500;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.ltxt i {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-style: normal;
  font-size: 11.5px;
  color: var(--faint);
  white-space: nowrap;
  overflow: hidden;
}
.ltxt .dot {
  width: 5px;
  height: 5px;
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

/* ── 账号列表：视图切换 / 排序条 / 分段 ── */
.vbar {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 2px;
  margin: 8px 10px 0;
  padding: 2px;
  border-radius: 7px;
  background: rgba(255, 255, 255, 0.04);
}
.vbtn {
  height: 22px;
  border: 0;
  border-radius: 5px;
  background: transparent;
  color: var(--faint);
  font-family: inherit;
  font-size: 11.5px;
  cursor: pointer;
}
.vbtn:hover {
  color: var(--dim);
}
.vbtn.on {
  background: rgba(255, 255, 255, 0.09);
  color: var(--tx);
}
.sbar {
  display: flex;
  align-items: center;
  gap: 4px;
  min-height: 26px;
  padding: 0 11px;
  border-bottom: 1px solid var(--line);
}
.sbar > span {
  font-size: 10.5px;
  color: var(--faint);
}
.sbar .hint {
  line-height: 1.3;
}
.sbar button {
  height: 20px;
  padding: 0 6px;
  border: 1px solid transparent;
  border-radius: 4px;
  background: transparent;
  color: var(--faint);
  font-family: inherit;
  font-size: 10.5px;
  cursor: pointer;
}
.sbar button.on {
  border-color: var(--line2);
  color: var(--tx);
}
.rows {
  flex: 1;
  overflow: auto;
}
.sect + .sect {
  border-top: 1px solid var(--line);
}
.shead {
  display: flex;
  align-items: center;
  gap: 6px;
  height: 26px;
  padding: 0 10px;
  background: rgba(255, 255, 255, 0.015);
  color: var(--faint);
  font-size: 11px;
  letter-spacing: 0.04em;
  cursor: pointer;
  user-select: none;
}
.shead:hover {
  color: var(--dim);
}
.shead.nograb {
  cursor: default;
}
.sname {
  font-weight: 600;
}
.shead em {
  font-style: normal;
  font-size: 10px;
}
.chev {
  width: 11px;
  height: 11px;
  flex: none;
  transition: transform 0.15s ease-out;
}
.chev.closed {
  transform: rotate(-90deg);
}

/* 行：拖动中的样子与插入位置线 */
.lrow.dragging {
  opacity: 0.55;
}
.lrow.over {
  box-shadow: inset 0 2px 0 var(--acc);
}

/* 行内的标签：无彩色小块。为什么不用彩色：颜色在本界面已经代表
   "可操作/当前"（金色），再给标签一套颜色就成了第二套语义。 */
.tagline {
  display: flex;
  flex-wrap: wrap;
  gap: 3px;
  margin: 1px 0;
}
.tagchip {
  height: 15px;
  padding: 0 5px;
  border-radius: 3px;
  background: rgba(255, 255, 255, 0.055);
  color: var(--faint);
  font-size: 10px;
  line-height: 15px;
  font-style: normal;
  white-space: nowrap;
}
.copybadge {
  display: inline-block;
  height: 15px;
  padding: 0 4px;
  margin-left: 4px;
  border: 1px solid var(--line2);
  border-radius: 3px;
  color: var(--faint);
  font-size: 9.5px;
  font-weight: 400;
  line-height: 13px;
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
