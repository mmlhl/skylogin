<script setup lang="ts">
/**
 * 壳 04 · 晨白：浅色主题 + 横幅手风琴。
 * 每个账号一条全宽横幅，展开的那条露出操作，运行 / 过期内联处理。
 */
import { ref, watch } from "vue";
import { Flame, Minus, Square, X, Layers, Plus, Eye, EyeOff, Settings, Pencil, Play, RefreshCw, ChevronDown, LayoutDashboard } from "lucide-vue-next";
import type { LauncherStore } from "../contract";

const props = defineProps<{ store: LauncherStore }>();
const store = props.store;import { demoUi } from "../ui";
import { channelLabel, fmtRun, type Account } from "../contract";

const openId = ref(store.current?.id ?? "");
watch(
  () => store.current?.id,
  (id) => {
    if (id) openId.value = id;
  }
);
function toggle(a: Account) {
  openId.value = openId.value === a.id ? "" : a.id;
}
</script>

<template>
  <div class="win">
    <div class="tbar">
      <span class="brand"><Flame />光遇登录器</span>
      <span class="wc" aria-hidden="true"><i><Minus /></i><i><Square /></i><i><X /></i></span>
    </div>

    <div class="toolbar">
      <h2>账号</h2>
      <div class="tools">
        <button class="btn" @click="demoUi.openMulti()"><Layers />多开</button>
        <button class="btn soft" @click="store.addAccount()"><Plus />添加账号</button>
        <span class="tsep" />
        <button class="ico" :title="store.maskAll ? '显示完整信息' : '打码'" @click="store.toggleMask()">
          <EyeOff v-if="store.maskAll" /><Eye v-else />
        </button>
        <button class="ico" title="监控墙（实时小窗）" @click="store.toggleWall()"><LayoutDashboard /></button>
        <button class="ico" title="设置" @click="store.openSettings()"><Settings /></button>
      </div>
    </div>

    <div class="deck">
      <div v-for="a in store.accounts" :key="a.id" class="banner" :class="{ open: openId === a.id, run: a.running, bad: a.cred === 'expired' }">
        <div class="brow" @click="toggle(a)">
          <input
            type="checkbox"
            class="sel"
            :checked="store.selectedIds.includes(a.id)"
            title="选中该账号"
            @click.stop
            @change="store.toggleSelect(a.id)"
          />
          <span class="bava">{{ a.name.slice(0, 1) }}</span>
          <span class="btxt">
            <b>{{ store.maskAll ? a.maskedName : a.name }}</b>
            <i>{{ channelLabel(a.channel) }} · <span class="mono">{{ store.maskAll ? a.maskedId : a.fullId }}</span></i>
          </span>
          <span v-if="a.running" class="bpill live"><em class="dot ok live" />运行中 · {{ fmtRun(a.runSeconds) }}</span>
          <span v-else-if="a.cred === 'expired'" class="bpill exp"><em class="dot bad" />凭证过期</span>
          <span v-else class="bpill ok"><em class="dot ok" />就绪</span>
          <button v-if="a.running" class="btn sm" @click.stop="store.stop(a.id)"><Square />停止</button>
          <button v-else-if="a.cred === 'expired'" class="btn sm danger" @click.stop="store.refreshCred(a.id)"><RefreshCw />刷新凭证</button>
          <ChevronDown v-else-if="openId !== a.id" class="bchev" />
        </div>
        <div v-if="openId === a.id" class="bbody">
          <p class="bmeta">
            <template v-if="a.level">等级 {{ a.level }} · </template>
            {{ a.cred === "expired" ? "登录凭证已过期" : "已登录，凭证会在启动时自动刷新" }}
          </p>
          <div class="bacts">
            <button v-if="a.running" class="btn primary stop" @click="store.stop(a.id)"><Square />停止运行</button>
            <button v-else class="btn primary" @click="store.launch(a.id)"><Play />启动光遇</button>
            <button class="btn" @click="store.refreshCred(a.id)"><RefreshCw />刷新凭证</button>
            <button class="btn" @click="demoUi.openMulti()"><Layers />多开</button>
            <button class="ico" title="修改别名" @click="demoUi.openRename(a.id)"><Pencil /></button>
          </div>
        </div>
      </div>
      <button class="addcard" @click="store.addAccount()"><Plus />添加账号</button>
    </div>
  </div>
</template>

<style scoped>
.win {
  --bg: #f5f3ee;
  --s1: #ffffff;
  --s2: #efece4;
  --tx: #26211a;
  --dim: #6e6759;
  --faint: #8b8574;
  --line: rgba(38, 31, 20, 0.11);
  --line2: rgba(38, 31, 20, 0.18);
  --acc: #a3762b;
  --on: #ffffff;
  --ok: #3e8e63;
  --okbg: rgba(62, 142, 99, 0.1);
  --bad: #bb5a45;
  --badbg: rgba(187, 90, 69, 0.09);
  display: flex;
  flex-direction: column;
  height: 100vh;
  background: var(--bg);
  color: var(--tx);
}

.toolbar {
  display: flex;
  align-items: center;
  flex: none;
  padding: 18px 36px 14px;
}
.toolbar h2 {
  margin: 0;
  font-size: 19px;
  font-weight: 600;
  letter-spacing: -0.015em;
}
.tools {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: 8px;
}
.tsep {
  width: 1px;
  height: 18px;
  margin: 0 4px;
  background: var(--line2);
}

.deck {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 4px 36px 24px;
}
.banner {
  flex: none;
  border: 1px solid var(--line);
  border-radius: 14px;
  background: var(--s1);
  cursor: pointer;
  transition: box-shadow 0.15s ease-out;
}
.banner.open {
  cursor: default;
  box-shadow: 0 12px 32px -20px rgba(70, 55, 25, 0.35);
}
.banner.run {
  border-color: color-mix(in srgb, var(--ok) 35%, transparent);
}
.banner.bad {
  border-color: color-mix(in srgb, var(--bad) 30%, transparent);
}
.brow {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 13px 16px;
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
.banner:hover .sel,
.sel:checked,
.banner.open .sel {
  opacity: 1;
}
.bava {
  display: grid;
  place-items: center;
  flex: none;
  width: 36px;
  height: 36px;
  border-radius: 11px;
  background: var(--s2);
  font-size: 13px;
  color: var(--dim);
}
.btxt {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 1px;
}
.btxt b {
  font-size: 14.5px;
  font-weight: 600;
  letter-spacing: -0.01em;
}
.btxt i {
  font-style: normal;
  font-size: 12px;
  color: var(--faint);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.bpill {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 22px;
  padding: 0 9px;
  border-radius: 999px;
  border: 1px solid var(--line);
  font-size: 11.5px;
  color: var(--dim);
  white-space: nowrap;
}
.bpill.ok {
  color: var(--ok);
  border-color: color-mix(in srgb, var(--ok) 32%, transparent);
  background: var(--okbg);
}
.bpill.live {
  color: var(--ok);
  border-color: color-mix(in srgb, var(--ok) 32%, transparent);
  background: var(--okbg);
}
.bpill.exp {
  color: var(--bad);
  border-color: color-mix(in srgb, var(--bad) 32%, transparent);
  background: var(--badbg);
}
.bchev {
  width: 16px;
  height: 16px;
  color: var(--faint);
}
.bbody {
  padding: 2px 16px 16px;
  border-top: 1px solid var(--line);
}
.bmeta {
  margin: 12px 0 0;
  font-size: 12.5px;
  color: var(--dim);
}
.bacts {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 12px;
}
.btn.primary.stop {
  background: color-mix(in srgb, var(--tx) 85%, transparent);
}
.addcard {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  height: 52px;
  flex: none;
  border: 1px dashed var(--line2);
  border-radius: 14px;
  background: transparent;
  font-family: inherit;
  font-size: 13px;
  color: var(--faint);
  cursor: pointer;
}
.addcard svg {
  width: 15px;
  height: 15px;
}
.addcard:hover {
  color: var(--acc);
  border-color: color-mix(in srgb, var(--acc) 40%, transparent);
}
</style>
