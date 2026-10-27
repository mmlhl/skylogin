<script setup lang="ts">
/**
 * 批量操作条：多选账号后出现，提供快捷预设与批量动作。
 * 同样跟随当前壳主题色（--demo-acc）。
 */
import { computed } from "vue";
import { Play, Square, Trash2, Layers, X, LayoutDashboard } from "lucide-vue-next";
import type { LauncherStore } from "./contract";
import { demoUi } from "./ui";

const props = defineProps<{ store: LauncherStore }>();
const store = props.store;

const count = computed(() => store.selectedIds.length);
</script>

<template>
  <Transition name="bbar">
    <div v-if="count" class="bbar">
      <span class="bcount">已选 {{ count }}</span>
      <span class="bsep" />
      <button class="bq" @click="store.selectAll()">全选</button>
      <button class="bq" @click="store.invertSelect()">反选</button>
      <button class="bq" @click="store.selectRunning()">选中运行中</button>
      <button class="bq" @click="store.clearSelect()"><X />清除</button>
      <span class="bsep" />
      <!-- 单选 = 直接启动；多选 = 主按钮就是多开（弹面板选显示方式 / 排列 / 画质） -->
      <button v-if="count === 1" class="bact primary" title="启动这个账号" @click="store.bulkLaunch()"><Play />启动</button>
      <button v-else class="bact primary" title="多开：一起启动，可选窗口排列或全屏堆叠" @click="demoUi.openMulti([...store.selectedIds])"><Layers />多开</button>
      <button class="bact" @click="store.bulkStop()"><Square />停止</button>
      <button class="bact" title="开/关监控墙：所有运行中客户端的实时小窗，点小窗切到前台" @click="store.toggleWall()"><LayoutDashboard />监控墙</button>
      <button class="bact danger" @click="store.bulkRemove()"><Trash2 />删除</button>
    </div>
  </Transition>
</template>

<style scoped>
.bbar {
  --acc: var(--demo-acc, #e3c28a);
  --on: var(--demo-on, #1a1408);
  position: fixed;
  left: 50%;
  bottom: 20px;
  transform: translateX(-50%);
  z-index: 55;
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 7px 10px;
  border: 1px solid rgba(255, 255, 255, 0.14);
  border-radius: 999px;
  background: rgba(23, 23, 27, 0.92);
  backdrop-filter: blur(14px);
  box-shadow: 0 16px 44px rgba(0, 0, 0, 0.5);
  white-space: nowrap;
}
.bcount {
  padding: 0 8px 0 6px;
  font-size: 12.5px;
  font-weight: 600;
  color: var(--acc);
}
.bsep {
  width: 1px;
  height: 16px;
  margin: 0 6px;
  background: rgba(255, 255, 255, 0.14);
}
.bq,
.bact {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  height: 28px;
  padding: 0 10px;
  border: 0;
  border-radius: 999px;
  background: transparent;
  color: #a29d92;
  font-family: inherit;
  font-size: 12.5px;
  cursor: pointer;
  transition: background-color 0.15s ease-out, color 0.15s ease-out;
}
.bq svg,
.bact svg {
  width: 12px;
  height: 12px;
}
.bq:hover,
.bact:hover {
  background: rgba(255, 255, 255, 0.08);
  color: #f3f1ea;
}
.bact.primary {
  background: var(--acc);
  color: var(--on);
  font-weight: 600;
}
.bact.primary:hover {
  filter: brightness(1.06);
  background: var(--acc);
  color: var(--on);
}
.bact.danger:hover {
  background: rgba(224, 141, 124, 0.16);
  color: #e08d7c;
}
.bbar-enter-active,
.bbar-leave-active {
  transition: opacity 0.16s ease-out, transform 0.16s ease-out;
}
.bbar-enter-from,
.bbar-leave-to {
  opacity: 0;
  transform: translateX(-50%) translateY(8px);
}
</style>
