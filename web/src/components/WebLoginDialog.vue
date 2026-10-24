<script setup lang="ts">
/**
 * 网页登录进度窗口。
 *
 * 真正的登录窗口是独立子进程（一个进程一个 WebView2），
 * 这里只负责轮询后端状态并给用户提示，避免重复打开。
 */
import { onUnmounted, ref, watch } from "vue";
import { NModal, useMessage } from "naive-ui";
import { api } from "../api/client";
import { state } from "../composables/useAppState";

const props = defineProps<{ show: boolean }>();
const emit = defineEmits<{ (e: "update:show", v: boolean): void }>();

const message = useMessage();
const phase = ref<"waiting" | "done" | "failed">("waiting");
const text = ref("请在弹出的窗口里登录");
let timer: number | undefined;

async function poll() {
  const r = await api.webLoginStatus();
  if (r.phase === "done") {
    phase.value = "done";
    text.value = "登录成功，已保存账号";
    message.success("登录成功");
    stop();
    emit("update:show", false);
  } else if (r.phase === "failed") {
    phase.value = "failed";
    text.value = r.message || "登录失败";
    stop();
  } else if (r.phase === "waiting") {
    text.value = r.message || "请在弹出的窗口里登录";
  }
}

function stop() {
  if (timer) window.clearInterval(timer);
  timer = undefined;
}

watch(
  () => props.show,
  (v) => {
    if (v) {
      phase.value = "waiting";
      text.value = "请在弹出的窗口里登录（网易官方页面）";
      stop();
      timer = window.setInterval(poll, 1200);
    } else {
      stop();
    }
  }
);

onUnmounted(stop);
</script>

<template>
  <n-modal
    :show="show"
    preset="card"
    title="网页登录"
    style="width: 400px"
    display-directive="if"
    :mask-closable="false"
    @update:show="(v: boolean) => emit('update:show', v)"
  >
    <div class="body">
      <span class="dot" :class="phase" />
      <span class="text">{{ text }}</span>
    </div>
    <p class="hint">登录完成后这个窗口会自动关闭，账号会保存到左侧列表</p>
  </n-modal>
</template>

<style scoped>
.body {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 6px 0 2px;
}

.dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--accent);
  flex: none;
  animation: breathe 1.8s ease-in-out infinite;
}
.dot.done {
  background: #7bbf94;
  animation: none;
}
.dot.failed {
  background: #d98b8b;
  animation: none;
}

@keyframes breathe {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.35; }
}

.text {
  font-size: 13.5px;
  color: var(--text);
}

.hint {
  margin: 12px 0 0;
  font-size: 11.5px;
  line-height: 1.7;
  color: var(--faint);
}
</style>
