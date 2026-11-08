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
// 没有全局注册 Naive UI：模板里用到 n-select 就必须显式引入
import { NSelect } from "naive-ui";

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
  const base =
    countdown.value > 0
      ? `用光遇手游扫码，${countdown.value} 秒后失效`
      : "用光遇手游或网易大神扫一扫";
  return hostHint.value ? `${base}｜${hostHint.value}` : base;
});

/**
 * 寄宿提示：这次扫码会把号写进哪个平台账号的槽。
 *
 * 只有平台身份凭证（a50）的号能当宿主，一个宿主只能带一个扫码号。
 * 没有宿主时扫码照样能加号，但不会写槽，凭证过期后只能重扫——必须提前说清。
 *
 * 槽位全满时给出下拉，让用户选"顶掉哪一个"，而不是默默顶掉第一个。
 */
const hostHint = ref("");
const hosts = ref<any[]>([]);
const pickHost = ref("");
async function loadHostHint() {
  try {
    const r: any = await api.hostPool();
    if (r?.ok === false) return;
    const pool = r?.pool;
    if (!pool) return;
    const list: any[] = pool.hosts ?? [];
    hosts.value = list.filter((h) => h.canHost);
    if (pool.note) {
      hostHint.value = pool.note;
      // 槽满时预选第一个占用者，用户可以直接确认或改选
      const occupied = hosts.value.filter((h) => !h.available);
      pickHost.value = occupied[0]?.accountId || "";
      return;
    }
    const free = hosts.value.filter((h) => h.available);
    if (free.length > 0) {
      hostHint.value = `将寄宿到 ${free[0].name} 的空槽，过期后免扫`;
    } else if (hosts.value.length > 0) {
      hostHint.value = "宿主槽位已满，本次会顶掉一个已寄宿的号";
      pickHost.value = hosts.value[0]?.accountId || "";
    }
  } catch {
    // 只是提示，取不到不影响扫码
  }
}

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
    // 槽满时把用户选的宿主带下去；没选就交给后端自动挑（优先空槽）
    const r = await api.startScan(pickHost.value);
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
    if (open) {
      void loadHostHint();
      void start();
    }
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
  >
    <!-- 槽位全满时让用户选顶掉哪一个：默认顶掉第一个会踩到别人的号 -->
    <div v-if="hosts.length > 0 && hosts.every((h) => !h.available) && !done" class="hostpick">
      <span class="hlabel">替换哪个槽位</span>
      <n-select
        v-model:value="pickHost"
        size="small"
        :options="hosts.map((h) => ({ label: `${h.name}（${h.hostedName || '空'}）`, value: h.accountId }))"
      />
    </div>
  </ScanDialogShell>
</template>

<style scoped>
.hostpick {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 10px;
}
.hlabel {
  font-size: 12px;
  opacity: 0.7;
  white-space: nowrap;
}
</style>
