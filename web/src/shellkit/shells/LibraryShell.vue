<script setup lang="ts">
/**
 * 壳 01 · 账号库：卡片网格，类游戏平台。
 * 卡片即账号，启动 / 停止 / 刷新都在卡上，多账号时最直观。
 */
import { ref } from "vue";
import { Flame, Minus, Square, X, Layers, Plus, Eye, EyeOff, Settings, MoreHorizontal, Play, RefreshCw, LayoutDashboard } from "lucide-vue-next";
import type { LauncherStore } from "../contract";

const props = defineProps<{ store: LauncherStore }>();
const store = props.store;import { demoUi } from "../ui";
import { channelLabel, fmtRun, type Account } from "../contract";

const menuFor = ref("");
function menuOf(a: Account) {
  return menuFor.value === a.id;
}
function cardAction(a: Account) {
  if (a.running) store.stop(a.id);
  else if (a.cred === "expired") store.refreshCred(a.id);
  else store.launch(a.id);
}
</script>

<template>
  <div class="win">
    <div class="tbar">
      <span class="brand"><Flame />光遇登录器</span>
      <span class="wc" aria-hidden="true"><i><Minus /></i><i><Square /></i><i><X /></i></span>
    </div>

    <div class="toolbar">
      <h2>账号库<span class="count mono">{{ store.accounts.length }}</span></h2>
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

    <p v-if="!store.accounts.length" class="empty">还没有账号，点右上「添加账号」开始</p>

    <div class="grid">
      <div
        v-for="a in store.accounts"
        :key="a.id"
        class="gcard"
        :class="{ on: a.current, run: a.running, bad: a.cred === 'expired' }"
        @click="store.select(a.id)"
      >
        <div class="gtop">
          <input
            type="checkbox"
            class="sel"
            :checked="store.selectedIds.includes(a.id)"
            title="选中该账号"
            @click.stop
            @change="store.toggleSelect(a.id)"
          />
          <span class="gava">{{ a.name.slice(0, 1) }}</span>
          <span class="pill">{{ channelLabel(a.channel) }}</span>
          <button class="ico gmore" @click.stop="menuFor = menuFor === a.id ? '' : a.id"><MoreHorizontal /></button>
          <template v-if="menuOf(a)">
            <div class="menubg" @click.stop="menuFor = ''" />
            <div class="menu" @click.stop>
              <button @click="store.refreshCred(a.id); menuFor = ''">刷新凭证</button>
              <button @click="demoUi.openRename(a.id); menuFor = ''">修改别名</button>
              <button class="del" @click="store.remove(a.id); menuFor = ''">删除账号</button>
            </div>
          </template>
        </div>
        <p class="gname">{{ store.maskAll ? a.maskedName : a.name }}</p>
        <p class="gid mono">{{ store.maskAll ? a.maskedId : a.fullId }}<template v-if="a.level"> · 等级 {{ a.level }}</template></p>
        <div class="gfoot">
          <span v-if="a.running" class="gstate live"><em class="dot ok live" />运行中 · {{ fmtRun(a.runSeconds) }}</span>
          <span v-else-if="a.cred === 'expired'" class="gstate exp"><em class="dot bad" />凭证过期</span>
          <span v-else class="gstate"><em class="dot ok" />就绪</span>
          <button v-if="a.running" class="btn sm" @click.stop="store.stop(a.id)"><Square />停止</button>
          <button v-else-if="a.cred === 'expired'" class="btn sm danger" @click.stop="store.refreshCred(a.id)"><RefreshCw />刷新凭证</button>
          <button v-else class="btn sm primary" @click.stop="store.launch(a.id)"><Play />启动</button>
        </div>
      </div>
    </div>

    <div class="sbar">
      <span><em class="dot ok" :class="{ live: store.runningAccounts.length }" />{{ store.runningAccounts.length ? store.runningAccounts.length + " 个客户端正在运行" : "没有客户端在运行" }}</span>
      <span class="mono">sky.exe · D:\FeverApps\sky\sky.exe</span>
    </div>
  </div>
</template>

<style scoped>
.win {
  --bg: #0d0e12;
  --s1: #14161c;
  --s2: #1a1d25;
  --tx: #f0eee8;
  --dim: #9aa0ac;
  --faint: #6b7382;
  --line: rgba(255, 255, 255, 0.07);
  --line2: rgba(255, 255, 255, 0.12);
  --acc: #d9b57c;
  --on: #191206;
  --ok: #7fce9f;
  --okbg: rgba(127, 206, 159, 0.1);
  --bad: #e08d7c;
  --badbg: rgba(224, 141, 124, 0.1);
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
  padding: 18px 28px 14px;
}
.toolbar h2 {
  margin: 0;
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 19px;
  font-weight: 600;
  letter-spacing: -0.015em;
}
.count {
  display: inline-grid;
  place-items: center;
  min-width: 22px;
  height: 20px;
  padding: 0 6px;
  border-radius: 6px;
  background: color-mix(in srgb, var(--tx) 8%, transparent);
  font-size: 11px;
  color: var(--dim);
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

.empty {
  margin: auto;
  font-size: 13px;
  color: var(--faint);
}
.grid {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(296px, 1fr));
  gap: 14px;
  align-content: start;
  padding: 4px 28px 20px;
}
.gcard {
  display: flex;
  flex-direction: column;
  padding: 16px 16px 14px;
  border: 1px solid var(--line);
  border-radius: 14px;
  background: var(--s1);
  cursor: pointer;
  transition: border-color 0.15s ease-out, background-color 0.15s ease-out;
}
.gcard:hover {
  border-color: var(--line2);
  background: var(--s2);
}
.gcard.on {
  border-color: color-mix(in srgb, var(--acc) 40%, transparent);
}
.gcard.run {
  border-color: color-mix(in srgb, var(--ok) 30%, transparent);
}
.gcard.bad {
  border-color: color-mix(in srgb, var(--bad) 28%, transparent);
}
.gtop {
  position: relative;
  display: flex;
  align-items: center;
  gap: 8px;
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
.gcard:hover .sel,
.sel:checked {
  opacity: 1;
}
.gava {
  display: grid;
  place-items: center;
  width: 34px;
  height: 34px;
  border-radius: 10px;
  background: color-mix(in srgb, var(--tx) 8%, transparent);
  font-size: 13px;
  color: var(--dim);
}
.gmore {
  margin-left: auto;
}
.menubg {
  position: fixed;
  inset: 0;
  z-index: 9;
}
.menu {
  position: absolute;
  top: 36px;
  right: 0;
  z-index: 10;
  min-width: 128px;
  padding: 5px;
  border: 1px solid var(--line2);
  border-radius: 10px;
  background: var(--s2);
  box-shadow: 0 16px 40px rgba(0, 0, 0, 0.5);
}
.menu button {
  display: block;
  width: 100%;
  padding: 7px 10px;
  border: 0;
  border-radius: 7px;
  background: transparent;
  color: var(--dim);
  font-family: inherit;
  font-size: 12.5px;
  text-align: left;
  cursor: pointer;
}
.menu button:hover {
  background: color-mix(in srgb, var(--tx) 6%, transparent);
  color: var(--tx);
}
.menu button.del:hover {
  color: var(--bad);
}
.gname {
  margin: 12px 0 0;
  font-size: 16.5px;
  font-weight: 600;
  letter-spacing: -0.01em;
}
.gid {
  margin: 3px 0 0;
  color: var(--faint);
}
.gfoot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-top: 16px;
  padding-top: 12px;
  border-top: 1px solid var(--line);
}
.gstate {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  font-size: 12px;
  color: var(--dim);
}
.gstate.live {
  color: var(--ok);
}
.gstate.exp {
  color: var(--bad);
}
.sbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex: none;
  height: 38px;
  padding: 0 28px;
  border-top: 1px solid var(--line);
  font-size: 12px;
  color: var(--faint);
}
.sbar > span {
  display: inline-flex;
  align-items: center;
  gap: 7px;
}
</style>
