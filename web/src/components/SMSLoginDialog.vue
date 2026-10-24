<script setup lang="ts">
/**
 * 手机号验证码登录。
 *
 * 流程：输入手机号 → 发送验证码 → 输入 6 位码 → 登录。
 * 验证码换到的票据是一次性的，所以「登录」只在拿到码之后才可用；
 * 任何一步失败都要重新发码（后端也会拒绝旧票据）。
 */
import { computed, onUnmounted, ref, watch } from "vue";
import { NModal, NInput, useMessage } from "naive-ui";
import { api } from "../api/client";

const props = defineProps<{ show: boolean; targetUserId?: string; defaultMobile?: string }>();
const emit = defineEmits<{
  (e: "update:show", v: boolean): void;
  (e: "done"): void;
}>();

const message = useMessage();
const mobile = ref("");
const code = ref("");
const sending = ref(false);
const loggingIn = ref(false);
const countdown = ref(0);
let timer: number | undefined;

const mobileOK = computed(() => /^1\d{10}$/.test(mobile.value));
const codeOK = computed(() => /^\d{6}$/.test(code.value));

function startCountdown() {
  countdown.value = 60;
  timer = window.setInterval(() => {
    countdown.value--;
    if (countdown.value <= 0) stopTimer();
  }, 1000);
}

function stopTimer() {
  if (timer) window.clearInterval(timer);
  timer = undefined;
}

async function send() {
  if (!mobileOK.value || sending.value || countdown.value > 0) return;
  sending.value = true;
  try {
    const r = await api.smsSend(mobile.value);
    if (r.ok === false) {
      message.error(r.message || "发送失败");
      return;
    }
    message.success("验证码已发送");
    startCountdown();
  } finally {
    sending.value = false;
  }
}

async function submit() {
  if (!mobileOK.value || !codeOK.value || loggingIn.value) return;
  loggingIn.value = true;
  try {
    const r = await api.smsLogin(mobile.value, code.value, props.targetUserId || "");
    if (r.ok === false) {
      message.error(r.message || "登录失败");
      return;
    }
    message.success("登录成功，账号已保存");
    emit("done");
    emit("update:show", false);
  } finally {
    loggingIn.value = false;
  }
}

watch(
  () => props.show,
  (v) => {
    if (v) {
      mobile.value = props.defaultMobile || "";
      code.value = "";
      stopTimer();
      countdown.value = 0;
    } else {
      stopTimer();
    }
  }
);

onUnmounted(stopTimer);
</script>

<template>
  <n-modal
    :show="show"
    preset="card"
    title="手机号登录"
    style="width: 400px"
    display-directive="if"
    :mask-closable="false"
    @update:show="(v: boolean) => emit('update:show', v)"
  >
    <div class="form">
      <label class="field">
        <span class="label">手机号</span>
        <n-input v-model:value="mobile" placeholder="11 位手机号" :maxlength="11" />
      </label>

      <label class="field">
        <span class="label">验证码</span>
        <div class="code-row">
          <n-input v-model:value="code" placeholder="6 位验证码" :maxlength="6" />
          <button class="send" :disabled="!mobileOK || sending || countdown > 0" @click.prevent="send">
            {{ countdown > 0 ? `${countdown}s` : sending ? "发送中…" : "发送验证码" }}
          </button>
        </div>
      </label>
    </div>

    <p class="hint">验证码只用于换取登录凭证，之后启动游戏不需要再输</p>

    <template #footer>
      <div class="foot">
        <button class="primary" :disabled="!mobileOK || !codeOK || loggingIn" @click="submit">
          {{ loggingIn ? "登录中…" : "登录" }}
        </button>
      </div>
    </template>
  </n-modal>
</template>

<style scoped>
.form {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.field {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.label {
  font-size: 12px;
  color: var(--faint);
}

.code-row {
  display: flex;
  gap: 8px;
}

.code-row :deep(.n-input) {
  flex: 1;
}

.send {
  flex: none;
  padding: 0 12px;
  height: 34px;
  border: 1px solid var(--line);
  border-radius: var(--r-sm);
  background: transparent;
  color: var(--dim);
  font-family: inherit;
  font-size: 12.5px;
  cursor: pointer;
  transition: border-color 0.16s ease-out, color 0.16s ease-out;
}
.send:hover:not(:disabled) {
  border-color: var(--line-strong);
  color: var(--text);
}
.send:disabled {
  opacity: 0.45;
  cursor: default;
}

.hint {
  margin: 14px 0 0;
  font-size: 11.5px;
  line-height: 1.7;
  color: var(--faint);
}

.foot {
  display: flex;
  justify-content: flex-end;
}

.primary {
  height: 34px;
  padding: 0 22px;
  border: 0;
  border-radius: 9px;
  background: var(--accent);
  color: var(--on-accent);
  font-family: inherit;
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  transition: filter 0.16s ease-out;
}
.primary:hover:not(:disabled) {
  filter: brightness(1.06);
}
.primary:disabled {
  background: rgba(255, 255, 255, 0.07);
  color: var(--faint);
  cursor: default;
}
</style>

