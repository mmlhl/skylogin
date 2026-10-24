<script setup lang="ts">
/**
 * 华为渠道：扫码登录。
 *
 * 和 Go 的交互：
 *   打开 → POST /api/login（Go 去华为申请二维码）
 *   轮询 → `/api/state` 的 login 字段：status / steps(1,2,3) / message
 *   关闭 → POST /api/login/cancel
 *
 * 登录成功后停 700ms 让「完成」那一步显示出来，再关窗——
 * 之前是状态一变立刻关，用户永远看不到成功确认。
 */
import { computed, ref, watch } from "vue";
import { api } from "../api/client";
import { state } from "../composables/useAppState";
import ScanDialogShell from "./ScanDialogShell.vue";

const props = defineProps<{ show: boolean }>();
const emit = defineEmits<{ (e: "update:show", v: boolean): void }>();

const starting = ref(false);
const error = ref("");

const login = computed(() => state.login);

/** 当前在第几步：Go 侧 steps 是数字数组，取最大值 */
const step = computed(() => {
  const arr = login.value.steps || [];
  const max = arr.length ? Math.max(...arr.map((n) => Number(n) || 0)) : 0;
  if (login.value.status === "scanned") return 2;
  if (login.value.status === "success") return 3;
  return max || 1;
});

const done = computed(() => login.value.status === "success");

/** 申请二维码 */
async function start() {
  error.value = "";
  starting.value = true;
  try {
    const r = await api.startHuaweiLogin();
    if (r.ok === false) error.value = r.message || "申请二维码失败";
  } finally {
    starting.value = false;
  }
}

watch(
  () => props.show,
  (open) => {
    if (open) void start();
  }
);

/** 成功后先显示完成，再关闭 */
watch(done, (v) => {
  if (!v) return;
  window.setTimeout(() => emit("update:show", false), 700);
});

function close() {
  emit("update:show", false);
  void api.cancelHuaweiLogin();
}
</script>

<template>
  <ScanDialogShell
    :show="show"
    title="华为账号 · 扫码登录"
    :qr="login.qr"
    :step="step"
    :tip="login.message || '用华为手机扫一扫'"
    :loading="starting"
    :error="error"
    :done="done"
    @update:show="(v: boolean) => (v ? emit('update:show', true) : close())"
    @retry="start"
  />
</template>
