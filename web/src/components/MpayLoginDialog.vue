<script setup lang="ts">
/**
 * 账号密码登录进度窗口。
 *
 * 真正的登录界面是网易官方的登录窗（由 mpay.dll 在自己的子进程里创建），
 * 这里只负责：提示用户去那个窗口登录 + 轮询结果。
 *
 * 为什么不自己画输入框：MPay 的密码是原生 SDK 加密的，密钥在 DLL 里，
 * 纯 Go 复现不了；让官方 DLL 自己完成登录是最稳的做法。
 */
import { onUnmounted, ref, watch } from "vue";
import { NModal, NButton, useMessage } from "naive-ui";
import { api } from "../api/client";

const props = defineProps<{ show: boolean }>();
const emit = defineEmits<{ (e: "update:show", v: boolean): void }>();

const message = useMessage();
const phase = ref<"waiting" | "done" | "failed">("waiting");
const text = ref("请在弹出的网易登录窗口里完成登录");
const canCancel = ref(true);
let timer: number | undefined;

async function poll() {
  const r = await api.mpayLoginStatus();
  if (r.phase === "done") {
    phase.value = "done";
    text.value = "登录成功，已保存账号";
    canCancel.value = false;
    message.success("登录成功");
    stop();
    window.setTimeout(() => emit("update:show", false), 600);
  } else if (r.phase === "failed") {
    phase.value = "failed";
    text.value = r.message || "登录失败";
    canCancel.value = false;
    stop();
    // 用户自己关掉了网易窗口：当成取消，弹窗自动收起，别挡着界面
    if ((r.message || "").includes("已取消")) {
      window.setTimeout(() => emit("update:show", false), 700);
    }
  } else if (r.phase === "waiting") {
    text.value = r.message || "请在弹出的网易登录窗口里完成登录";
  }
}

function stop() {
  if (timer) window.clearInterval(timer);
  timer = undefined;
}

/** 关掉登录窗口：后端会结束子进程 */
async function cancel() {
  await api.mpayLoginCancel();
  phase.value = "failed";
  text.value = "已取消";
  canCancel.value = false;
  stop();
}

watch(
  () => props.show,
  (v) => {
    if (v) {
      phase.value = "waiting";
      text.value = "正在打开网易登录窗口…";
      canCancel.value = true;
      stop();
      timer = window.setInterval(poll, 1200);
      poll();
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
    title="账号密码登录"
    style="width: 420px"
    display-directive="if"
    :mask-closable="false"
    @update:show="(v: boolean) => emit('update:show', v)"
  >
    <div class="body">
      <span class="dot" :class="phase" />
      <span class="text">{{ text }}</span>
    </div>

    <div class="tips">
      <p>在网易自己的登录窗口里可以选：</p>
      <ul>
        <li><b>邮箱账号登录</b> —— 网易邮箱 + 密码</li>
        <li><b>手机账号登录</b> —— 手机号 + 短信验证码</li>
      </ul>
    </div>

    <p class="hint">登录完成后这个窗口会自动关闭，账号会保存到左侧列表</p>

    <template #footer>
      <div class="footer">
        <n-button v-if="canCancel" quaternary @click="cancel">取消登录</n-button>
        <n-button v-else type="primary" @click="emit('update:show', false)">关闭</n-button>
      </div>
    </template>
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

.tips {
  margin-top: 12px;
  padding: 10px 12px;
  border: 1px solid var(--line);
  border-radius: var(--r-sm);
  background: rgba(255, 255, 255, 0.03);
}
.tips p {
  margin: 0 0 6px;
  font-size: 12px;
  color: var(--dim);
}
.tips ul {
  margin: 0;
  padding-left: 18px;
}
.tips li {
  font-size: 12px;
  line-height: 1.9;
  color: var(--faint);
}
.tips b {
  color: var(--text);
  font-weight: 400;
}

.hint {
  margin: 12px 0 0;
  font-size: 11.5px;
  line-height: 1.7;
  color: var(--faint);
}

.footer {
  display: flex;
  justify-content: flex-end;
}
</style>
