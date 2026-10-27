<script setup lang="ts">
/**
 * 壳宿主：当前界面壳 + 共享批量条/弹层。
 *
 * 界面壳通过 props 拿到 LauncherStore（demo 传 mock，正式版传 realStore），
 * 壳切换偏好优先写后端（WebView2 端口随机，localStorage 不可靠），
 * persistKey 仅在 demo 这类纯前端环境里使用。
 *
 * 浮动切换器只在设计演示页显示（showSwitcher）；
 * 正式界面的切换入口在「设置 → 界面方案」（SettingsPanel）。
 */
import { onMounted, markRaw } from "vue";
import type { LauncherStore } from "./contract";
import { SHELLS, activeShell } from "./appearance";
import LibraryShell from "./shells/LibraryShell.vue";
import FocusShell from "./shells/FocusShell.vue";
import ConsoleShell from "./shells/ConsoleShell.vue";
import PaperShell from "./shells/PaperShell.vue";
import BatchBar from "./BatchBar.vue";
import Overlays from "./Overlays.vue";
import HintBanner from "./HintBanner.vue";

const props = withDefaults(
  defineProps<{
    store: LauncherStore;
    /** 首选壳（后端/localStorage 都没有记录时用） */
    defaultIndex?: number;
    /** 纯前端环境的 localStorage 键；正式版传空，走 loadPref/savePref */
    persistKey?: string;
    loadPref?: () => Promise<number>;
    savePref?: (i: number) => void;
    /** 是否显示浮动切换器（正式版隐藏，切换入口在设置里） */
    showSwitcher?: boolean;
  }>(),
  { defaultIndex: 0, persistKey: "", showSwitcher: true }
);

const shells = SHELLS.map((s, i) => ({
  ...s,
  comp: markRaw([LibraryShell, FocusShell, ConsoleShell, PaperShell][i]),
}));

const active = activeShell;

onMounted(async () => {
  active.value = props.defaultIndex;
  if (props.persistKey) {
    const saved = Number(localStorage.getItem(props.persistKey));
    if (saved >= 0 && saved < shells.length) active.value = saved;
  }
  if (props.loadPref) {
    try {
      const v = await props.loadPref();
      if (v >= 0 && v < shells.length) active.value = v;
    } catch {
      /* 读取偏好失败就用默认壳 */
    }
  }
});

function pick(i: number) {
  active.value = i;
  if (props.persistKey) localStorage.setItem(props.persistKey, String(i));
  props.savePref?.(i);
}
</script>

<template>
  <div class="shell-host" :style="{ '--demo-acc': shells[active].acc, '--demo-on': shells[active].on }">
    <component :is="shells[active].comp" :store="store" />

    <div v-if="showSwitcher" class="switcher" role="tablist" aria-label="界面方案切换">
      <span class="slabel">界面方案</span>
      <button
        v-for="(s, i) in shells"
        :key="s.name"
        role="tab"
        :aria-selected="i === active"
        :class="{ on: i === active }"
        @click="pick(i)"
      >
        {{ s.name }}
      </button>
    </div>

    <HintBanner :store="store" />
    <BatchBar :store="store" />
    <Overlays :store="store" />
  </div>
</template>

<style scoped>
.shell-host {
  height: 100%;
}
.switcher {
  position: fixed;
  top: 14px;
  left: 50%;
  transform: translateX(-50%);
  z-index: 50;
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 5px 6px 5px 12px;
  border: 1px solid rgba(255, 255, 255, 0.14);
  border-radius: 999px;
  background: rgba(20, 20, 24, 0.86);
  backdrop-filter: blur(14px);
  box-shadow: 0 10px 32px rgba(0, 0, 0, 0.4);
}
.slabel {
  font-size: 11.5px;
  letter-spacing: 0.03em;
  color: #8f8a80;
  margin-right: 4px;
  white-space: nowrap;
}
.switcher button {
  height: 26px;
  padding: 0 11px;
  border: 1px solid transparent;
  border-radius: 999px;
  background: transparent;
  color: #a29d92;
  font-family: inherit;
  font-size: 12px;
  white-space: nowrap;
  cursor: pointer;
}
.switcher button:hover {
  color: #f3f1ea;
}
.switcher button.on {
  border-color: rgba(227, 194, 138, 0.5);
  background: rgba(227, 194, 138, 0.12);
  color: #e3c28a;
}
</style>
