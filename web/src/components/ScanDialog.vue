<script setup lang="ts">
/**
 * 网易账号：手机扫码。
 *
 * 和 Go 的交互：
 *   打开 → POST /api/scan/start（Go 去 MPay 建码，返回二维码）
 *   轮询 → `/api/state` 的 scan 字段：waiting → scanned → done
 *   关闭 → POST /api/scan/stop
 *
 * 扫码成功后 Go 会自动：换票 → 存账号（含 token）→ status 变 done。
 * 这里同样停 700ms 显示「完成」再关窗。
 */
import { computed, ref, watch } from "vue";
import { api } from "../api/client";
import { state } from "../composables/useAppState";
import ScanDialogShell from "./ScanDialogShell.vue";

const props = defineProps<{ show: boolean }>();
const emit = defineEmits<{ (e: "update:show", v: boolean): void }>();

const starting = ref(false);
const error = ref("");
const countdown = ref(0);
let timer: number | undefined;

const scan = computed(() => state.scan);
const done = computed(() => scan.value.status === "done");

/** 当前在第几步 */
const step = computed(() => {
  switch (scan.value.status) {
    case "done":
      return 3;
    case "scanned":
    case "ready":
      return 2;
    default:
      return 1;
  }
});

const tip = computed(() => {
  if (scan.value.status === "scanned") return "已扫码，请在手机上确认";
  if (countdown.value > 0) return `用光遇手游扫码，${countdown.value} 秒后失效`;
  return "用光遇手游或网易大神扫一扫";
});

/** 倒计时（只用于提示，不影响逻辑） */
function startCountdown() {
  window.clearInterval(timer);
  timer = window.setInterval(() => {
    if (!scan.value.expireAt) {
      countdown.value = 0;
      return;
    }
    countdown.value = Math.max(0, Math.round((scan.value.expireAt - Date.now()) / 1000));
  }, 500);
}

async function start() {
  error.value = "";
  starting.value = true;
  countdown.value = 0;
  try {
    const r = await api.startScan();
    if (r.ok === false) error.value = r.message || "申请二维码失败";
  } finally {
    starting.value = false;
    startCountdown();
  }
}

watch(
  () => props.show,
  (open) => {
    if (open) void start();
  }
);

watch(done, (v) => {
  if (!v) return;
  window.setTimeout(() => emit("update:show", false), 700);
});

function close() {
  emit("update:show", false);
  window.clearInterval(timer);
  void api.stopScan();
}
</script>

<template>
  <ScanDialogShell
    :show="show"
    title="网易账号 · 扫码登录"
    :qr="scan.qr"
    :step="step"
    :tip="tip"
    :loading="starting"
    :error="error"
    :done="done"
    @update:show="(v: boolean) => (v ? emit('update:show', true) : close())"
    @retry="start"
  />
</template>
