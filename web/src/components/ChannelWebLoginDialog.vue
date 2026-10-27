<script setup lang="ts">
/**
 * 渠道网页登录进度窗口（B站 / 4399 共用）。
 *
 * 真正的登录窗口是独立子进程（一个进程一个 WebView2，加载渠道官方登录页），
 * 这里只负责轮询后端状态并给用户提示，避免重复打开。
 * kind 决定轮询哪个渠道的进度接口和提示文案。
 */
import { onUnmounted, ref, watch } from "vue";
import { NModal, useMessage } from "naive-ui";
import { api } from "../api/client";

const props = defineProps<{ show: boolean; kind: "bili" | "m4399" | "vivo" }>();
const emit = defineEmits<{ (e: "update:show", v: boolean): void }>();

const message = useMessage();
const phase = ref<"waiting" | "done" | "failed">("waiting");
const text = ref("请在弹出的窗口里登录");
let timer: number | undefined;

const COPY = {
  bili: {
    title: "B站账号登录",
    waiting: "请在弹出的窗口里登录 B站账号（扫码 / 验证码 / 密码都可以）",
  },
  m4399: {
    title: "4399账号登录",
    waiting: "请在弹出的窗口里登录 4399 账号",
  },
  vivo: {
    title: "vivo账号登录",
    waiting: "请在弹出的窗口里登录 vivo 账号",
  },
} as const;

type ChannelKey = keyof typeof COPY;

async function poll() {
  const r = await api.channelLoginStatus(props.kind as ChannelKey);
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
      text.value = COPY[props.kind].waiting;
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
    :title="COPY[kind].title"
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
}
.dot {
  flex: none;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--faint);
}
.dot.waiting {
  background: #d8b47c;
  animation: blink 1.4s ease-in-out infinite;
}
.dot.done {
  background: #7fce9f;
}
.dot.failed {
  background: #e08d7c;
}
@keyframes blink {
  0%,
  100% {
    opacity: 1;
  }
  50% {
    opacity: 0.35;
  }
}
.text {
  font-size: 13px;
}
.hint {
  margin: 14px 0 0;
  font-size: 11.5px;
  line-height: 1.7;
  color: var(--faint);
}
</style>
