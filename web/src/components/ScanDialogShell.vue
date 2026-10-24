<script setup lang="ts">
/**
 * 扫码弹窗的公共外壳：二维码区 + 一句提示 + 步骤条。
 *
 * 华为和网易两个渠道的流程完全一样（扫码 → 确认 → 完成），
 * 区别只在数据和接口，所以外观抽到这里，两个弹窗只管自己的数据。
 *
 * 二维码区的四种状态：
 *   生成中 → 转圈 + 「正在生成二维码」
 *   失败   → 提示 + 「重新获取」按钮
 *   已出码 → 二维码图片（留静区）
 *   已完成 → 金色对勾
 */
import { NModal, NSpin } from "naive-ui";
import ScanSteps from "./ScanSteps.vue";

const props = defineProps<{
  show: boolean;
  title: string;
  /** 二维码图片（data URL，可能为空） */
  qr: string;
  /** 1 扫码 / 2 确认 / 3 完成 */
  step: number;
  /** 提示语 */
  tip: string;
  /** 正在申请二维码 */
  loading?: boolean;
  /** 失败信息（有值时显示重试） */
  error?: string;
  /** 是否已完成（显示对勾，不再显示二维码） */
  done?: boolean;
}>();

const emit = defineEmits<{
  (e: "update:show", v: boolean): void;
  (e: "retry"): void;
}>();
</script>

<template>
  <n-modal
    :show="show"
    preset="card"
    :title="title"
    style="width: 400px"
    :bordered="false"
    display-directive="if"
    @update:show="(v: boolean) => emit('update:show', v)"
  >
    <div class="body">
      <div class="qr" :class="{ empty: !qr && !done, finished: done }">
        <!-- 完成：金色对勾 -->
        <svg v-if="done" class="check" viewBox="0 0 48 48" width="48" height="48">
          <circle cx="24" cy="24" r="22" fill="none" stroke="currentColor" stroke-width="1.2" opacity="0.45" />
          <path d="M15 24.5l6.5 6.5L33 18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
        </svg>

        <!-- 生成中 -->
        <div v-else-if="loading" class="placeholder">
          <n-spin :size="18" />
          <span>正在生成二维码</span>
        </div>

        <!-- 失败 -->
        <div v-else-if="error" class="placeholder">
          <span>{{ error }}</span>
          <button class="retry" @click="emit('retry')">重新获取</button>
        </div>

        <!-- 还没出码 -->
        <div v-else-if="!qr" class="placeholder">
          <span>正在生成二维码</span>
        </div>

        <!-- 二维码 -->
        <img v-else :src="qr" alt="扫码登录二维码" />
      </div>

      <p class="tip">{{ done ? "登录成功，账号已添加" : tip }}</p>

      <ScanSteps :current="done ? 3 : step" />
    </div>
  </n-modal>
</template>

<style scoped>
.body {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 20px;
}

/* 二维码区：出码时白底留静区；空态用极淡虚线框，避免一块死白 */
.qr {
  display: grid;
  place-items: center;
  width: 208px;
  height: 208px;
  padding: 10px;
  border-radius: var(--r-md);
  background: #fff;
  overflow: hidden;
}

.qr.empty {
  background: rgba(255, 255, 255, 0.03);
  border: 1px dashed rgba(255, 255, 255, 0.12);
  padding: 0;
}

.qr.finished {
  background: rgba(255, 255, 255, 0.03);
  border: 1px solid var(--accent-line);
  color: var(--accent);
}

.qr img {
  width: 100%;
  height: 100%;
  object-fit: contain;
}

.check {
  color: var(--accent);
}

.placeholder {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
  padding: 0 16px;
  font-size: 12.5px;
  color: var(--dim);
  text-align: center;
}

.retry {
  padding: 4px 10px;
  border: 0;
  border-radius: var(--r-sm);
  background: transparent;
  color: var(--accent);
  font-family: inherit;
  font-size: 12.5px;
  cursor: pointer;
}
.retry:hover {
  background: var(--gold-soft);
}

.tip {
  margin: 0;
  min-height: 18px;
  font-size: 12.5px;
  color: var(--dim);
  text-align: center;
}
</style>

