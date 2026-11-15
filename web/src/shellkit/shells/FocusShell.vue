<script setup lang="ts">
/**
 * 壳 02 · 此刻：单一焦点舞台 + 底部账号队列，类播放器。
 * 一屏只有一个动作，账号切换收进底部 chips。
 */
import { computed } from "vue";
import { Square, Layers, Eye, EyeOff, Settings, Pencil, Play, Plus, RefreshCw, LayoutDashboard, SlidersHorizontal, ArrowUp, ArrowDown } from "lucide-vue-next";
import { useAccountBrowser } from "../useAccountBrowser";
import AccountBrowserBar from "../AccountBrowserBar.vue";
import AccountSectionHeader from "../AccountSectionHeader.vue";
import AccountTags from "../AccountTags.vue";
import type { LauncherStore } from "../contract";

const props = defineProps<{ store: LauncherStore }>();
const store = props.store;import { demoUi } from "../ui";
import { channelLabel, fmtRun } from "../contract";

const browser = useAccountBrowser(store);
const cur = computed(() => store.current);
const runningSelf = computed(() => !!cur.value?.running);
const expired = computed(() => cur.value?.cred === "expired");

</script>

<template>
  <div class="win">
    <div class="stage">
      <!-- 监控墙 / 设置并排：之前两个都是绝对定位同一坐标，互相盖住 -->
      <div class="c2row">
        <button class="btn groups-entry" @click="demoUi.openGroups()"><Layers />启动组</button>
        <button class="ico c2" title="监控墙（实时小窗）" @click="store.toggleWall()"><LayoutDashboard /></button>
        <button class="ico c2" title="设置" @click="store.openSettings()"><Settings /></button>
      </div>
      <button class="ico corner" :title="store.maskAll ? '显示完整信息' : '打码'" @click="store.toggleMask()">
        <EyeOff v-if="store.maskAll" /><Eye v-else />
      </button>

      <div v-if="cur" class="focus">
        <p class="feyebrow">
          <span class="pill">{{ channelLabel(cur.channel) }}</span>
          <span v-if="runningSelf" class="pill live"><em class="dot ok live" />运行中</span>
          <span v-else-if="expired" class="pill exp"><em class="dot bad" />凭证过期</span>
          <span v-else class="pill ok"><em class="dot ok" />就绪</span>
        </p>
        <div class="ftitle">
          <h1>{{ store.maskAll ? cur.maskedName : cur.name }}</h1>
          <button class="ico" title="游戏设置（画质 / 帧率）" @click="demoUi.openQuality(cur.id)"><SlidersHorizontal /></button>
          <button class="ico" title="修改别名" @click="demoUi.openRename(cur.id)"><Pencil /></button>
        </div>
        <p class="fmeta mono">{{ store.maskAll ? cur.maskedId : cur.fullId }}<template v-if="cur.level"> · 等级 {{ cur.level }}</template></p>
        <AccountTags :store="store" :account="cur" editable />

        <p v-if="expired" class="fstatus bad">登录凭证已过期，刷新后即可启动</p>
        <p v-else-if="runningSelf" class="fstatus ok">已运行 {{ fmtRun(cur.runSeconds) }}，游戏正在路上</p>
        <p v-else class="fstatus">已登录，凭证会在启动时自动刷新</p>

        <div class="facts">
          <button v-if="runningSelf" class="big ghost" @click="store.stop(cur.id)"><Square />停止运行</button>
          <button v-else class="big" :class="{ warn: expired }" @click="expired ? store.refreshCred(cur.id) : store.launch(cur.id)">
            <RefreshCw v-if="expired" /><Play v-else />{{ expired ? "刷新凭证" : "启动光遇" }}
          </button>
          <button class="ghostbig" @click="demoUi.openMulti()"><Layers />多开</button>
        </div>
      </div>
      <div v-else class="focus">
        <div class="ftitle"><h1>还没有账号</h1></div>
        <p class="fstatus">扫码登录一次，之后这里就是一键启动台</p>
        <div class="facts"><button class="big" @click="store.addAccount()"><Plus />添加账号</button></div>
      </div>
    </div>

    <div class="queue">
      <div class="queue-head"><span class="qlabel">切换账号</span><button class="ico" title="添加账号" @click="store.addAccount()"><Plus /></button></div>
      <AccountBrowserBar :browser="browser" :store="store" />
      <div class="queue-sections">
        <section v-for="sec in browser.sections.value" :key="sec.key" class="queue-section">
          <AccountSectionHeader v-if="browser.view.value !== 'flat'" :browser="browser" :section="sec" />
          <div v-if="!browser.isCollapsed(sec)" class="qchips">
            <div v-for="(a, index) in sec.rows" :key="a.id" class="queue-item" :data-account-id="a.id">
              <input type="checkbox" :checked="store.selectedIds.includes(a.id)" :aria-label="'勾选：' + browser.displayName(a)" @change="store.toggleSelect(a.id)" />
              <button class="qchip" :class="{ on: a.current, warn: a.cred === 'expired' }" :aria-pressed="a.current" :title="browser.displayName(a) + ' · ' + channelLabel(a.channel) + (a.tags.length ? ' · ' + a.tags.join('、') : '')" @click="store.select(a.id)">
                <i>{{ browser.displayName(a).slice(0, 1) }}</i><span class="qname">{{ browser.displayName(a) }}</span>
                <em v-if="a.running" class="dot ok live qdot" />
              </button>
              <div v-if="sec.draggable" class="queue-moves"><button title="账号上移" :disabled="browser.busy.value || index === 0" @click="browser.moveAccount(sec, a.id, -1)"><ArrowUp /></button><button title="账号下移" :disabled="browser.busy.value || index === sec.rows.length - 1" @click="browser.moveAccount(sec, a.id, 1)"><ArrowDown /></button></div>
            </div>
          </div>
        </section>
        <p v-if="!browser.visibleCount.value" class="queue-empty">{{ store.accounts.length ? '没有找到匹配账号，试试其他关键词' : '还没有账号，点击上方加号添加' }}</p>
      </div>
    </div>
  </div>
</template>

<style scoped>
.win {
  --bg: #0a0a0d;
  --s2: #1a191f;
  --tx: #f3f1ea;
  --dim: #a29d92;
  --faint: #6f6a60;
  --line: rgba(255, 255, 255, 0.08);
  --line2: rgba(255, 255, 255, 0.14);
  --acc: #e3c28a;
  --on: #1a1408;
  --ok: #82cfa2;
  --okbg: rgba(130, 207, 162, 0.1);
  --bad: #e08d7c;
  --badbg: rgba(224, 141, 124, 0.1);
  display: flex;
  flex-direction: column;
  height: 100%;
  background: var(--bg);
  color: var(--tx);
}

.stage {
  position: relative;
  flex: 1;
  min-height: 0;
  display: flex;
  align-items: center;
  padding: 0 72px;
}
.corner {
  position: absolute;
  top: 12px;
  right: 14px;
}
.c2row {
  position: absolute;
  top: 12px;
  right: 52px;
  display: flex;
  gap: 8px;
}
.c2 {
  position: static;
}
.focus {
  max-width: 560px;
}
.feyebrow {
  display: flex;
  gap: 8px;
  margin: 0;
}
.ftitle {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-top: 16px;
}
.ftitle h1 {
  margin: 0;
  font-size: 54px;
  font-weight: 700;
  letter-spacing: -0.03em;
  line-height: 1.05;
}
.fmeta {
  margin: 12px 0 0;
  color: var(--dim);
}
.fstatus {
  margin: 20px 0 0;
  font-size: 13.5px;
  color: var(--faint);
}
.fstatus.bad {
  color: var(--bad);
}
.fstatus.ok {
  color: var(--ok);
}
.facts {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-top: 32px;
}
.big {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 9px;
  height: 52px;
  padding: 0 42px;
  border: 0;
  border-radius: 26px;
  background: var(--acc);
  color: var(--on);
  font-family: inherit;
  font-size: 15px;
  font-weight: 600;
  letter-spacing: -0.01em;
  cursor: pointer;
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.2);
}
.big svg {
  width: 16px;
  height: 16px;
}
.big:hover {
  filter: brightness(1.05);
}
.big.warn {
  background: color-mix(in srgb, var(--bad) 82%, #000);
  color: #fff;
}
.big.ghost {
  background: color-mix(in srgb, var(--tx) 8%, transparent);
  color: var(--tx);
}
.ghostbig {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  height: 52px;
  padding: 0 26px;
  border: 1px solid var(--line2);
  border-radius: 26px;
  background: transparent;
  color: var(--dim);
  font-family: inherit;
  font-size: 14px;
  cursor: pointer;
}
.ghostbig svg {
  width: 15px;
  height: 15px;
}
.ghostbig:hover {
  color: var(--tx);
  background: color-mix(in srgb, var(--tx) 4%, transparent);
}

.queue {
  flex: none;
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 14px 24px 18px;
  border-top: 1px solid var(--line);
}
.qlabel {
  font-size: 11.5px;
  letter-spacing: 0.04em;
  color: var(--faint);
  flex: none;
}
.qchips {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.qchip {
  position: relative;
  display: inline-flex;
  align-items: center;
  gap: 8px;
  height: 36px;
  padding: 0 14px 0 6px;
  border: 1px solid var(--line);
  border-radius: 999px;
  background: transparent;
  color: var(--dim);
  font-family: inherit;
  font-size: 13px;
  cursor: pointer;
}
.qchip:hover {
  color: var(--tx);
}
.qchip i {
  display: grid;
  place-items: center;
  width: 25px;
  height: 25px;
  border-radius: 50%;
  background: color-mix(in srgb, var(--tx) 9%, transparent);
  font-style: normal;
  font-size: 11px;
}
.qchip.on {
  border-color: color-mix(in srgb, var(--acc) 55%, transparent);
  color: var(--tx);
  background: color-mix(in srgb, var(--acc) 9%, transparent);
}
.qchip.on i {
  background: var(--acc);
  color: var(--on);
}
.qchip.warn i {
  background: color-mix(in srgb, var(--bad) 20%, transparent);
}
.qdot {
  position: absolute;
  top: 5px;
  right: 9px;
}
.qchip.add {
  padding: 0 12px;
  color: var(--faint);
  border-style: dashed;
}
.qchip.add svg {
  width: 14px;
  height: 14px;
}
/* 账号多时只滚动底部队列，主操作仍在可见范围内。 */
.stage { overflow:auto; padding:48px 48px 24px; }
.focus { width:100%; min-width:0; margin:auto 0; }
.ftitle { min-width:0; }
.ftitle h1 { min-width:0; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; font-size:clamp(26px,5vw,54px); }
.ftitle > button { flex:none; }
.fmeta { overflow-wrap:anywhere; }
.facts { flex-wrap:wrap; }
.queue { display:flex; flex-direction:column; align-items:stretch; gap:0; max-height:42%; min-height:168px; padding:9px 24px 12px; }
.queue-head { display:flex; align-items:center; justify-content:space-between; margin-bottom:4px; }
.queue :deep(.account-browser-bar) { padding:0 0 7px; }
.queue-sections { min-height:0; overflow:auto; }
.queue-section { min-width:0; }
.qchips { align-items:stretch; gap:7px; padding:3px 0 8px; }
.queue-item { display:flex; align-items:center; gap:5px; min-width:0; max-width:100%; }
.queue-item > input { flex:none; width:13px; height:13px; margin:0; accent-color:var(--acc); }
.qchip { min-width:0; max-width:260px; }
.qchip i { flex:none; }
.qname { min-width:0; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
.queue-moves { display:flex; flex-direction:column; flex:none; }
.queue-moves button { display:grid; place-items:center; padding:0; width:18px; height:18px; background:transparent; border:0; color:var(--dim); cursor:pointer; }
.queue-moves svg { width:12px; height:12px; }
.queue-moves button:disabled { opacity:.25; cursor:default; }
.queue-empty { color:var(--dim); font-size:12px; }
.queue button:focus-visible,.queue input:focus-visible { outline:2px solid var(--acc); outline-offset:2px; }
@media (max-width:700px) { .stage { padding:48px 24px 20px; } .queue { padding-left:16px; padding-right:16px; } .queue-item { max-width:100%; } .qchip { max-width:min(260px,calc(100vw - 85px)); } }
</style>
