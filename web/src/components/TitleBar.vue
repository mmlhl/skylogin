<script setup lang="ts">
/**
 * 自绘标题栏。
 *
 * 为什么需要它：窗口去掉了系统标题栏（见 internal/win/window.go 的
 * MakeBorderless），因为系统标题栏的深浅由 DWM 决定，浅色主题下强制的
 * 深色会在拖动时被冲掉、标题栏反白。自己画就没有这个问题。
 *
 * 拖动不能靠 CSS：WebView2 的客户区被 Chromium 吃掉鼠标事件，页面里的
 * 拖拽不会变成窗口拖动，所以 mousedown 时要让 Go 那边补一次
 * WM_NCLBUTTONDOWN（HTCAPTION），由系统接管拖动循环。
 */
import { ref } from "vue";
import { Minus, Square, Copy, X } from "lucide-vue-next";
import { api } from "../api/client";

/** 本地记录最大化状态：只影响图标形状（系统快捷键触发的最大化可能不同步，
 *  下次点按钮时会自己纠正）。 */
const zoomed = ref(false);

function onDragStart(e: MouseEvent) {
  // 只响应左键；点在窗口按钮上时不启动拖动（否则点最小化会先抖一下）
  if (e.button !== 0) return;
  if ((e.target as HTMLElement).closest("button")) return;
  void api.windowDrag();
}

function onDblClick(e: MouseEvent) {
  if ((e.target as HTMLElement).closest("button")) return;
  void toggleMax();
}

async function toggleMax() {
  await api.windowControl("max");
  zoomed.value = !zoomed.value;
}

function minimize() {
  void api.windowControl("min");
}

function close() {
  void api.windowControl("close");
}
</script>

<template>
  <header class="titlebar" @mousedown="onDragStart" @dblclick="onDblClick">
    <span class="dot" aria-hidden="true" />
    <span class="name">光遇登录器</span>

    <div class="ctl">
      <button class="wbtn" title="最小化" @click="minimize"><Minus :size="14" :stroke-width="1.6" /></button>
      <button class="wbtn" :title="zoomed ? '还原' : '最大化'" @click="toggleMax">
        <Copy v-if="zoomed" :size="13" :stroke-width="1.5" />
        <Square v-else :size="12" :stroke-width="1.6" />
      </button>
      <button class="wbtn close" title="关闭" @click="close"><X :size="14" :stroke-width="1.8" /></button>
    </div>
  </header>
</template>

<style scoped>
.titlebar {
  flex: none;
  height: 36px;
  display: flex;
  align-items: center;
  gap: 8px;
  /* 16px = 界面壳里内容区的左右内边距（.rail 的标题行 padding: 14px 16px），
     标题条的文字与下面界面的内容左边缘对齐 */
  padding-left: 16px;
  background: #0b0b0d;
  border-bottom: 1px solid rgba(255, 255, 255, 0.06);
  user-select: none;
  /* 拖动时不要出现文本选择光标 */
  cursor: default;
}
.dot {
  /* flex 容器里不写 flex:none 会被压缩，圆点会缩没 */
  flex: none;
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: #d8b47c;
  opacity: 0.9;
}
.name {
  font-size: 12.5px;
  letter-spacing: 0.02em;
  color: #8f8a80;
}
.ctl {
  margin-left: auto;
  display: flex;
  height: 100%;
}
.wbtn {
  width: 46px;
  height: 100%;
  display: grid;
  place-items: center;
  border: 0;
  background: transparent;
  color: #a5a29c;
  cursor: default;
}
.wbtn:hover {
  background: rgba(255, 255, 255, 0.07);
  color: #f4f2ef;
}
.wbtn.close:hover {
  background: #c0392b;
  color: #fff;
}
</style>
