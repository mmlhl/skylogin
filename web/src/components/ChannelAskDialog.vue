<script setup lang="ts">
/**
 * 渠道「协议登录」对话框（九游账号密码）。
 *
 * 流程是后端驱动的：打开时调 start，之后每秒轮询 status。
 * status 里带 ask 就渲染表单或选项，提交后流程继续；
 * 走到 done / failed 才结束。
 *
 * 这样加一种登录方式（比如短信），前端不用改：后端多一个 ask 而已。
 */
import { ref, watch } from "vue";
import { NModal } from "naive-ui";
import { api } from "../api/client";

interface AskField {
  name: string;
  label: string;
  type: string;
  placeholder: string;
  maxLen: number;
}
interface AskOption {
  value: string;
  label: string;
}
interface Ask {
  kind: string;
  title: string;
  fields: AskField[];
  options: AskOption[];
}

const props = defineProps<{ show: boolean; kind: string; method?: string; title?: string }>();
const emit = defineEmits<{ (e: "update:show", v: boolean): void; (e: "done"): void }>();

const message = ref("");
const error = ref("");
const ask = ref<Ask | null>(null);
/** 后端给的二维码（PNG 的 base64）：应用宝微信/QQ 扫码走这里 */
const qr = ref("");
const values = ref<Record<string, string>>({});
const busy = ref(false);
let timer: number | undefined;

function clearTimer() {
  if (timer) {
    window.clearInterval(timer);
    timer = undefined;
  }
}

/** 拉一次状态：有 ask 就渲染，到终态就收尾 */
async function poll() {
  try {
    const st = (await api.channelLoginStatus(props.kind)) as Record<string, any>;
    message.value = st.message || "";
    qr.value = st.qr || "";

    if (st.ask) {
      const next = st.ask as Ask;
      // 只在「换了一步」时才重置输入，否则轮询会把用户正在打的字冲掉
      if (!ask.value || ask.value.title !== next.title || ask.value.kind !== next.kind) {
        ask.value = next;
        const init: Record<string, string> = {};
        for (const f of next.fields) init[f.name] = values.value[f.name] ?? "";
        values.value = init;
      }
    } else {
      ask.value = null;
    }

    if (st.phase === "done") {
      clearTimer();
      emit("done");
      emit("update:show", false);
    } else if (st.phase === "failed") {
      clearTimer();
      error.value = st.message || "登录失败";
    }
  } catch (e: any) {
    error.value = e?.message || "查询登录进度失败";
  }
}

async function start() {
  message.value = "正在准备…";
  error.value = "";
  ask.value = null;
  values.value = {};
  qr.value = "";
  try {
    await api.startProtocolLogin(props.kind, props.method || "password");
    void poll();
    timer = window.setInterval(poll, 1200);
  } catch (e: any) {
    error.value = e?.message || "启动登录失败";
  }
}

async function submit() {
  if (!ask.value || busy.value) return;
  busy.value = true;
  error.value = "";
  try {
    await api.answerChannelAsk(props.kind, values.value);
    ask.value = null;
    message.value = "正在登录…";
  } catch (e: any) {
    error.value = e?.message || "提交失败";
  } finally {
    busy.value = false;
  }
}

/** 选项直接点选，不需要再点确定 */
function pick(v: string) {
  values.value = { value: v };
  void submit();
}

function close() {
  clearTimer();
  // 关掉就取消后端流程：扫码会一直等，不取消的话 tracker 会占着 5 分钟
  void api.cancelChannelLogin(props.kind).catch(() => {});
  emit("update:show", false);
}

watch(
  () => props.show,
  (v) => {
    if (v) void start();
    else clearTimer();
  },
  { immediate: true }
);
</script>

<template>
  <n-modal
    :show="show"
    preset="card"
    :title="title || '登录九游账号'"
    style="width: 420px"
    display-directive="if"
    :mask-closable="false"
    @update:show="(v: boolean) => !v && close()"
  >
    <p v-if="message" class="message">{{ message }}</p>

    <!-- 表单：手机号 + 密码 -->
    <form v-if="ask && ask.kind === 'form'" class="form" @submit.prevent="submit">
      <label v-for="f in ask.fields" :key="f.name" class="row">
        <span class="label">{{ f.label }}</span>
        <input
          v-model="values[f.name]"
          :type="f.type === 'password' ? 'password' : 'text'"
          :placeholder="f.placeholder"
          :maxlength="f.maxLen || undefined"
          autocomplete="off"
        />
      </label>
      <div class="actions">
        <button type="button" class="ghost" @click="close">取消</button>
        <button type="submit" class="primary" :disabled="busy">登录</button>
      </div>
    </form>

    <!-- 选项：选游戏账号 -->
    <div v-else-if="ask && ask.kind === 'choice'" class="choices">
      <button v-for="o in ask.options" :key="o.value" class="choice" :disabled="busy" @click="pick(o.value)">
        {{ o.label }}
      </button>
      <div class="actions">
        <button type="button" class="ghost" @click="close">取消</button>
      </div>
    </div>

    <!-- 扫码：二维码由后端生成后经 /status 送过来 -->
    <div v-else-if="qr" class="qrbox">
      <img :src="'data:image/png;base64,' + qr" alt="扫码登录" />
    </div>

    <!-- 等待后端 -->
    <div v-else class="waiting"><span class="dot" /><span class="dot" /><span class="dot" /></div>

    <p v-if="error" class="error">{{ error }}</p>
  </n-modal>
</template>

<style scoped>
.message {
  margin: 0 0 12px;
  font-size: 12.5px;
  color: var(--dim);
}

.form {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.row {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.label {
  font-size: 11.5px;
  color: var(--faint);
}

input {
  padding: 9px 12px;
  border-radius: 7px;
  border: 1px solid var(--line-strong);
  background: transparent;
  color: var(--text);
  font-size: 13px;
  outline: none;
}

input:focus {
  border-color: var(--gold);
}

.choices {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.choice {
  padding: 11px 14px;
  border-radius: 8px;
  border: 1px solid var(--line-strong);
  background: transparent;
  color: var(--text);
  font-size: 13px;
  text-align: left;
  cursor: pointer;
}

.choice:hover {
  border-color: var(--gold);
}

.qrbox {
  display: flex;
  justify-content: center;
  padding: 8px 0 4px;
}
.qrbox img {
  width: 220px;
  height: 220px;
  border-radius: 8px;
  background: #fff;
  padding: 8px;
}

.waiting {
  display: flex;
  justify-content: center;
  gap: 6px;
  padding: 18px 0;
}

.dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--dim);
  animation: blink 1.2s infinite ease-in-out;
}
.dot:nth-child(2) {
  animation-delay: 0.2s;
}
.dot:nth-child(3) {
  animation-delay: 0.4s;
}

@keyframes blink {
  0%,
  80%,
  100% {
    opacity: 0.25;
  }
  40% {
    opacity: 1;
  }
}

.error {
  margin: 12px 0 0;
  font-size: 11.5px;
  color: #e06c75;
}

.actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 6px;
}

.ghost,
.primary {
  padding: 7px 16px;
  border-radius: 7px;
  border: 1px solid var(--line-strong);
  font-size: 12.5px;
  cursor: pointer;
}
.ghost {
  background: transparent;
  color: var(--dim);
}
.ghost:hover {
  color: var(--text);
}
.primary {
  border-color: transparent;
  background: var(--gold);
  color: #1a1305;
  font-weight: 600;
}
.primary:disabled {
  opacity: 0.6;
  cursor: default;
}
</style>




