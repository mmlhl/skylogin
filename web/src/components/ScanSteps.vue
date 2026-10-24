<script setup lang="ts">
/**
 * 扫码步骤条：① 扫码 → ② 确认 → ③ 完成。
 *
 * 为什么要单独抽出来：
 * 之前两个扫码弹窗各写一套，华为那套因为变量名写错（--gold 不存在）永远是灰的，
 * 网易那套干脆没有步骤条，用户看不出自己在第几步。现在统一走这里。
 *
 * 三种状态（current 之前的都算已完成）：
 *   已完成：实心金色圆点 + 深色对勾
 *   当前：金色描边圆环 + 呼吸动画，文字提亮加粗
 *   未到：极淡描边圆环，文字弱色
 *
 * 用法：<ScanSteps :current="2" />   （0 表示还没开始，只亮第一个）
 */
import { computed } from "vue";

const props = defineProps<{
  /** 当前步：1 扫码 / 2 确认 / 3 完成 */
  current: number;
}>();

const steps = ["扫码", "确认", "完成"];

const cur = computed(() => {
  const n = Number(props.current) || 0;
  // 0 表示刚打开弹窗，等价于"正在第 1 步"
  return Math.min(Math.max(n || 1, 1), 3);
});
</script>

<template>
  <ol class="steps">
    <li v-for="(label, i) in steps" :key="label" class="step" :class="{ done: i + 1 < cur, on: i + 1 === cur }">
      <span class="dot">
        <svg v-if="i + 1 < cur" viewBox="0 0 16 16" width="9" height="9">
          <path d="M3.2 8.4l3 3 6.6-7" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
        </svg>
      </span>
      <span class="label">{{ label }}</span>
      <span v-if="i < steps.length - 1" class="bar" :class="{ filled: i + 1 < cur }" />
    </li>
  </ol>
</template>

<style scoped>
.steps {
  display: flex;
  align-items: center;
  margin: 0;
  padding: 0;
  list-style: none;
}

.step {
  display: flex;
  align-items: center;
  gap: 8px;
}

/* 圆点：默认极淡描边 */
.dot {
  display: grid;
  place-items: center;
  width: 14px;
  height: 14px;
  border-radius: 50%;
  border: 1px solid var(--line-strong);
  color: #161208;
  transition: border-color 0.16s, background 0.16s;
}

/* 已完成：实心金 + 对勾 */
.step.done .dot {
  border-color: var(--accent);
  background: var(--accent);
}

/* 当前：金色圆环 + 中心实心点 + 呼吸，比"未到"明显 */
.step.on .dot {
  position: relative;
  width: 15px;
  height: 15px;
  border-color: var(--accent);
  border-width: 1.5px;
  animation: breathe 1.8s ease-in-out infinite;
}
.step.on .dot::after {
  content: "";
  width: 5px;
  height: 5px;
  border-radius: 50%;
  background: var(--accent);
}

@keyframes breathe {
  0%,
  100% {
    box-shadow: 0 0 0 0 rgba(213, 180, 131, 0.28);
  }
  50% {
    box-shadow: 0 0 0 4px rgba(213, 180, 131, 0);
  }
}

.label {
  font-size: 11.5px;
  color: var(--faint);
  transition: color 0.16s;
}
.step.on .label {
  color: var(--text);
  font-weight: 600;
}
.step.done .label {
  color: var(--dim);
}

/* 连接线：16px，已完成段染成淡金 */
.bar {
  width: 24px;
  height: 1px;
  margin: 0 10px;
  background: var(--line-strong);
  transition: background 0.16s;
}
.bar.filled {
  background: var(--accent-line);
}
</style>

