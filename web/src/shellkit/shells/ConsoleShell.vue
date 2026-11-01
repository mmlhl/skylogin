<script setup lang="ts">
/**
 * 壳 03 · 控制台：图标栏 + 账号列表 + 详情面板，类 Linear。
 * 信息密度最高，运行中的客户端在详情页统一管理。
 */
import { computed, ref } from "vue";
import { Flame, LayoutGrid, LayoutDashboard, Monitor, Settings, Eye, EyeOff, Plus, Pencil, Play, Square, Layers, RefreshCw, CopyPlus, Gauge } from "lucide-vue-next";
import type { LauncherStore } from "../contract";

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

// 游戏画质/帧率档位（与多开弹窗同一套，档位名 = 游戏内部名）
const qualityOpts = [
  { v: "", label: "跟随游戏", tip: "启动前不预写画质" },
  { v: "LowPower", label: "省电", tip: "游戏内部档位 LowPower" },
  { v: "Default", label: "默认", tip: "游戏内部档位 Default" },
  { v: "HighQuality", label: "高画质", tip: "游戏内部档位 HighQuality" },
  { v: "Presentation", label: "最高", tip: "游戏内部档位 Presentation" },
];
const fpsOpts = [
  { v: 0, label: "跟随游戏" },
  { v: 30, label: "30" },
  { v: 60, label: "60" },
  { v: 120, label: "120" },
];

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
      <p v-if="!store.accounts.length" class="lempty">还没有账号</p>
      <div
        v-for="a in store.accounts"
        :key="a.id"
        class="lrow"
        :class="{ on: a.current }"
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
          <b>{{ store.maskAll ? a.maskedName : a.name }}</b>
          <i>
            <em v-if="a.running" class="dot ok live" />
            <em v-else-if="a.cred === 'expired'" class="dot bad" />
            <em v-else class="dot ok" />
            {{ a.running ? "运行中 · " + fmtRun(a.runSeconds) : a.cred === "expired" ? "凭证过期" : "就绪" }} · {{ channelLabel(a.channel) }}
          </i>
        </span>
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
            <button class="ico" title="修改别名" @click="demoUi.openRename(cur.id)"><Pencil /></button>
            <span v-if="runningSelf" class="pill live"><em class="dot ok live" />运行中</span>
            <span v-else-if="expired" class="pill exp"><em class="dot bad" />凭证过期</span>
            <span v-else class="pill ok"><em class="dot ok" />就绪</span>
          </div>
        </div>
      </div>

      <div class="dinfo">
        <div><span>账号 ID</span><b class="mono">{{ store.maskAll ? cur.maskedId : cur.fullId }}</b></div>
        <div><span>等级</span><b>{{ cur.level || "—" }}</b></div>
        <div>
          <span>凭证</span>
          <b class="credline">
            {{ expired ? "已过期" : "正常，启动时自动刷新" }}
            <button v-if="expired" class="btn sm danger" @click="store.refreshCred(cur.id)">刷新凭证</button>
          </b>
        </div>
        <div><span>状态</span><b>{{ runningSelf ? "已运行 " + fmtRun(cur.runSeconds) : "空闲" }}</b></div>
      </div>

      <!-- 这个账号自己的游戏画质/帧率：单开和多开（账号配置档）都优先用它 -->
      <div class="qcard">
        <div class="qhead">
          <Gauge class="qicon" />
          <b>游戏画质</b>
          <span class="qhint">只对「{{ store.maskAll ? cur.maskedName : cur.name }}」生效，启动前写入</span>
        </div>
        <div class="qrow">
          <span class="qlabel">画质</span>
          <div class="qpills">
            <button v-for="o in qualityOpts" :key="'q' + o.v" :class="{ on: (cur.qualityMode || '') === o.v }"
              :title="o.tip" @click="store.setAccountQuality(cur.id, o.v, cur.qualityFps || 0)">{{ o.label }}</button>
          </div>
        </div>
        <div class="qrow">
          <span class="qlabel">帧率</span>
          <div class="qpills">
            <button v-for="o in fpsOpts" :key="'f' + o.v" :class="{ on: (cur.qualityFps || 0) === o.v }"
              @click="store.setAccountQuality(cur.id, cur.qualityMode || '', o.v)">{{ o.label }}</button>
          </div>
        </div>
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
  height: 100vh;
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
.dacts {
  display: flex;
  gap: 10px;
  margin-top: 22px;
}
.dseclabel {
  margin: 34px 0 0;
  font-size: 11.5px;
  letter-spacing: 0.04em;
  color: var(--faint);
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
.qcard {
  padding: 12px 14px;
  border: 1px solid var(--line);
  border-radius: 12px;
  background: var(--s1);
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.qhead {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
}
.qicon {
  width: 15px;
  height: 15px;
  color: var(--acc);
}
.qhint {
  margin-left: auto;
  font-size: 11px;
  color: var(--faint);
}
.qrow {
  display: flex;
  align-items: center;
  gap: 10px;
}
.qlabel {
  flex: none;
  width: 30px;
  font-size: 12px;
  color: var(--dim);
}
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
.qrow {
  display: flex;
  align-items: center;
  gap: 10px;
}
.qlabel {
  flex: none;
  width: 30px;
  font-size: 12px;
  color: var(--dim);
}
.qsegs {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
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
</style>
