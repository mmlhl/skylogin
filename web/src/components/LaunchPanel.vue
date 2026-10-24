<script setup lang="ts">
/**
 * 右侧：一张玻璃卡片 = 当前账号 + 状态 + 启动 + 底部入口。
 *
 * 布局为什么这么排：
 *   之前是几行字散落在空背景上，没有结构。现在把信息收进一张卡片，
 *   从上到下依次是 身份 → 状态 → 动作 → 次要入口，视觉重心落在主按钮上。
 *
 * 状态三种形态（形状区分，不只换颜色）：
 *   正常   实心小圆点
 *   运行中 带呼吸动画的圆环，主按钮降级为次级按钮，避免误点重启
 *   过期   变一条横幅，给出「刷新凭证」入口（进对应渠道的登录方式选择）
 */
import { computed, ref } from "vue";
import { useMessage } from "naive-ui";
import { api } from "../api/client";
import { currentAccount, state, isRevealed } from "../composables/useAppState";
import { maskId, maskName } from "../composables/useMask";

const emit = defineEmits<{
  (e: "rename"): void;
  (e: "refresh"): void;
  (e: "settings"): void;
  (e: "multi"): void;
}>();

const message = useMessage();
const busy = ref(false);

const acc = computed(() => currentAccount.value);
const open = computed(() => (acc.value ? isRevealed(acc.value.id) : false));

const shownName = computed(() => {
  if (!acc.value) return "还没有账号";
  return open.value ? acc.value.name : maskName(acc.value.name);
});

const shownId = computed(() => {
  if (!acc.value) return "";
  return open.value ? acc.value.accountId : maskId(acc.value.accountId);
});

// 只看"当前账号"的进程：别的账号在玩不影响这一张卡片
const running = computed(() => !!acc.value?.running);
// 游戏是全局单实例：任何一个客户端在跑，别的账号都起不来
const anyRunning = computed(() => state.game === "运行中");
const launchHint = computed(() => (anyRunning.value ? "游戏限制只能同时启动一次：请先停止正在运行的光遇，或用多开一次性启动" : ""));
// 过期有两种来源：运行时的提示（authHint，只在内存里）
// 和账号自己记的状态（credState，重启后还在）。两者都要显示横幅。
const expired = computed(() => !!state.authHint.kind || acc.value?.credState === "expired");

const expiredText = computed(() => {
  if (state.authHint.message) return state.authHint.message;
  if (acc.value?.credState === "expired") return acc.value.lastError || "此账号的登录凭证已过期，需要重新登录";
  return "登录信息已过期，需要重新登录";
});

const launchLabel = computed(() => {
  if (busy.value) return "正在启动…";
  return "启动光遇";
});

/** 停止这个账号的光遇（同账号不允许并行，会互相顶掉） */
async function stop() {
  if (busy.value || !acc.value) return;
  busy.value = true;
  try {
    const r = await api.stopAccount(acc.value.id);
    if (r.ok === false) message.error(r.message || "停止失败");
  } finally {
    setTimeout(() => (busy.value = false), 800);
  }
}

async function launch() {
  if (busy.value || !acc.value) return;
  busy.value = true;
  try {
    const r = await api.launch();
    if (r.ok === false) message.error(r.message || "启动失败");
  } finally {
    setTimeout(() => (busy.value = false), 800);
  }
}
</script>

<template>
  <section class="glass">
    <p class="eyebrow">{{ acc ? acc.channelLabel : "还没有账号" }}</p>

    <div class="title-row">
      <h1>{{ shownName }}</h1>
      <button v-if="acc" class="rename" title="修改别名" @click="emit('rename')">
        <svg viewBox="0 0 16 16" width="14" height="14">
          <path
            d="M11.2 2.3a1.4 1.4 0 0 1 2 2L6 11.5l-2.7.7.7-2.7z"
            fill="none"
            stroke="currentColor"
            stroke-width="1.2"
            stroke-linejoin="round"
          />
        </svg>
      </button>
    </div>

    <p v-if="acc" class="meta">账号 <span class="mono">{{ shownId }}</span><template v-if="acc.level"> · 等级 {{ acc.level }}</template></p>
    <p v-else class="meta">扫码一次，之后一键启动</p>

    <!-- 过期：横幅 + 直接给入口 -->
    <div v-if="expired" class="banner">
      <span class="banner-icon">!</span>
      <span class="banner-text">{{ expiredText }}</span>
      <button class="banner-act" @click="emit('refresh')">刷新凭证</button>
    </div>

    <p v-else class="status" :class="{ running }">
      <i />{{ running ? "这个账号的光遇正在运行" : anyRunning ? "已有账号在运行，需先停止才能启动其他账号" : acc ? "已登录，可直接启动" : "还没有添加账号" }}
    </p>

    <button v-if="running" class="cta secondary" :disabled="busy" @click="stop">
      {{ busy ? "正在停止…" : "停止运行" }}
    </button>
    <button
      v-else
      class="cta"
      :disabled="!acc || busy || anyRunning"
      :title="launchHint"
      @click="launch"
    >
      {{ launchLabel }}
    </button>

    <footer class="foot">
      <button class="foot-link" @click="emit('settings')">设置</button>
      <span class="foot-sep" />
      <button class="foot-link" @click="emit('multi')">多开</button>
      <span class="foot-sep" />
      <span class="foot-text">启动时自动关闭发烧平台</span>
    </footer>
  </section>
</template>

<style scoped>
/* 玻璃卡片 */
.glass {
  position: relative;
  width: 100%;
  max-width: 480px;
  padding: 32px;
  border-radius: var(--r-lg);
  border: 1px solid rgba(255, 255, 255, 0.1);
  background: rgba(255, 255, 255, 0.045);
  backdrop-filter: blur(24px) saturate(140%);
  box-shadow:
    0 32px 80px -40px rgba(0, 0, 0, 0.9),
    inset 0 1px 0 rgba(255, 255, 255, 0.09);
}

.eyebrow {
  margin: 0;
  font-size: 12px;
  color: var(--faint);
}

.title-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 10px;
}

h1 {
  margin: 0;
  font-size: 40px;
  line-height: 1.1;
  font-weight: 600;
  letter-spacing: -0.025em;
}

.rename {
  display: grid;
  place-items: center;
  width: 24px;
  height: 24px;
  padding: 0;
  border: 0;
  border-radius: 6px;
  background: transparent;
  color: var(--faint);
  cursor: pointer;
  transition: color 0.16s ease-out, background 0.16s ease-out;
}
.rename:hover {
  background: rgba(255, 255, 255, 0.07);
  color: var(--text);
}

.meta {
  margin: 10px 0 0;
  font-size: 13px;
  color: var(--dim);
}

/* 状态 */
.status {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 24px 0 0;
  font-size: 13px;
  color: var(--dim);
}
.status i {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--accent);
}
.status.running i {
  background: transparent;
  border: 1.5px solid var(--accent);
  animation: ring 1.8s ease-in-out infinite;
}

@keyframes ring {
  0%,
  100% {
    box-shadow: 0 0 0 0 color-mix(in srgb, var(--accent) 30%, transparent);
  }
  50% {
    box-shadow: 0 0 0 4px transparent;
  }
}

/* 过期横幅 */
.banner {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 24px;
  padding: 10px 12px;
  border-radius: var(--r-sm);
  background: color-mix(in srgb, var(--accent) 10%, transparent);
  border: 1px solid color-mix(in srgb, var(--accent) 32%, transparent);
}
.banner-icon {
  display: grid;
  place-items: center;
  flex: none;
  width: 16px;
  height: 16px;
  border-radius: 50%;
  border: 1.2px solid var(--accent);
  color: var(--accent);
  font-size: 10.5px;
  line-height: 1;
}
.banner-text {
  flex: 1;
  font-size: 12.5px;
  color: var(--text);
}
.banner-act {
  flex: none;
  padding: 4px 10px;
  border: 0;
  border-radius: 7px;
  background: transparent;
  color: var(--accent);
  font-family: inherit;
  font-size: 12.5px;
  cursor: pointer;
}
.banner-act:hover {
  background: color-mix(in srgb, var(--accent) 16%, transparent);
}

/* 主按钮：实心 + 柔和外发光 */
.cta {
  width: 100%;
  height: 46px;
  margin-top: 24px;
  border: 0;
  border-radius: 11px;
  background: var(--accent);
  color: var(--on-accent);
  font-family: inherit;
  font-size: 14px;
  font-weight: 600;
  letter-spacing: -0.01em;
  cursor: pointer;
  box-shadow:
    0 14px 34px -16px color-mix(in srgb, var(--accent) 85%, transparent),
    inset 0 1px 0 rgba(255, 255, 255, 0.25);
  transition: filter 0.16s ease-out, transform 0.16s ease-out, background 0.16s ease-out;
}
.cta:hover:not(:disabled) {
  filter: brightness(1.06);
}
.cta:active:not(:disabled) {
  transform: translateY(1px);
}
.cta:disabled {
  background: rgba(255, 255, 255, 0.07);
  color: var(--faint);
  box-shadow: none;
  cursor: default;
}
.cta:focus-visible {
  outline: 2px solid color-mix(in srgb, var(--accent) 55%, transparent);
  outline-offset: 3px;
}

/* 运行中：降级为次级按钮 */
.cta.secondary {
  height: 42px;
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid var(--line-strong);
  color: var(--dim);
  box-shadow: none;
  font-weight: 400;
}

/* 卡片底部 */
.foot {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 24px;
  padding-top: 16px;
  border-top: 1px solid rgba(255, 255, 255, 0.08);
}

.foot-link {
  border: 0;
  background: transparent;
  color: var(--dim);
  font-family: inherit;
  font-size: 13px;
  cursor: pointer;
  padding: 0;
  transition: color 0.16s ease-out;
}
.foot-link:hover {
  color: var(--text);
}

.foot-sep {
  width: 1px;
  height: 12px;
  background: rgba(255, 255, 255, 0.12);
}

.foot-text {
  font-size: 12px;
  color: var(--faint);
}
</style>



