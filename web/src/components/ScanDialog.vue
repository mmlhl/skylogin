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
/** start 请求返回前不认服务端状态：上一次登录留下的 done 会让弹窗一开就显示"已添加" */
const ready = ref(false);
const done = computed(() => ready.value && !starting.value && scan.value.status === "done");

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

/**
 * 展示在二维码区的提示。
 *
 * 除了 start 本身失败，还有一种情况：码扫了、也确认了，但换凭证/存账号失败
 * （status=ready 且没有二维码）。这时不能落回"正在生成二维码"——那会让人以为
 * 卡住了，得告诉用户重扫。
 */
const notice = computed(() => {
  if (error.value) return error.value;
  if (!scan.value.qr && !done.value && !starting.value && scan.value.status === "ready") {
    return scan.value.message || "账号没保存成功，请重新扫码";
  }
  return "";
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
  ready.value = false;
  countdown.value = 0;
  try {
    const r = await api.startScan();
    if (r.ok === false) error.value = r.message || "申请二维码失败";
  } finally {
    starting.value = false;
    ready.value = true;
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
  // 已有账号只更新了凭证时，多留一会儿让用户看清提示
  const wait = scan.value.existing ? 2600 : 700;
  window.setTimeout(() => {
    emit("update:show", false);
    // 自动收起时也要停掉轮询，否则后端会一直停在 done，下次打开又是"已添加"的假象
    void api.stopScan();
  }, wait);
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
    :error="notice"
    :done="done"
    @update:show="(v: boolean) => (v ? emit('update:show', true) : close())"
    @retry="start"
  />
</template>
