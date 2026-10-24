<script setup lang="ts">
/**
 * 左栏：账号列表 + 两个添加入口。
 *
 * 交互：点账号切换；每张卡片右侧常驻三个按钮——启动/停止、显示/隐藏、更多。
 * 按钮不只在选中或悬停时才出现，方便直接操作列表里的任意账号。
 */
import { ref } from "vue";
import { NDropdown, useMessage } from "naive-ui";
import { api } from "../api/client";
import { isRevealed, reveal, state, toggleReveal, toggleRevealAll } from "../composables/useAppState";
import { maskId, maskName } from "../composables/useMask";
import type { AccountView } from "../api/types";

// 两个添加入口分开：渠道账号（华为等第三方）走选渠道，网易账号直接进扫码
const emit = defineEmits<{
  (e: "add-channel"): void;
  (e: "add-netease"): void;
  (e: "rename"): void;
  (e: "refresh", acc: AccountView): void;
}>();
const message = useMessage();

/** 正在操作哪个账号（防止连点） */
const busyId = ref("");

async function selectAccount(acc: AccountView) {
  if (acc.current) return;
  const r = await api.selectAccount(acc.id);
  if (r.ok === false) message.error(r.message || "切换失败");
}

/** 卡片右侧：启动 / 停止（账号在跑就显示停止） */
async function startOrStop(acc: AccountView) {
  if (busyId.value) return;
  busyId.value = acc.id;
  try {
    if (acc.running) {
      const r = await api.stopAccount(acc.id);
      if (r.ok === false) message.error(r.message || "停止失败");
      return;
    }
    // 启动用的是"当前账号"，所以先切过去
    if (!acc.current) {
      const s = await api.selectAccount(acc.id);
      if (s.ok === false) {
        message.error(s.message || "切换失败");
        return;
      }
    }
    const r = await api.launch();
    if (r.ok === false) message.error(r.message || "启动失败");
  } finally {
    setTimeout(() => (busyId.value = ""), 800);
  }
}

const options = [
  { label: "修改别名", key: "rename" },
  { label: "刷新凭证", key: "refresh" },
  { label: "删除账号", key: "delete" },
];

async function onMenu(key: string, acc: AccountView) {
  if (key === "rename") return emit("rename");
  if (key === "refresh") return emit("refresh", acc);
  const r = await api.deleteAccount(acc.id);
  if (r.ok === false) message.error(r.message || "删除失败");
}

/** 每条账号的显示文本：默认打码 */
function shown(a: AccountView) {
  const open = isRevealed(a.id);
  return {
    open,
    name: open ? a.name : maskName(a.name),
    id: open ? a.accountId : maskId(a.accountId),
  };
}
</script>

<template>
  <aside class="rail">
    <header class="head">
      <span class="label">账号</span>
      <button class="eye" :class="{ off: !reveal.all }" :title="reveal.all ? '全部隐藏' : '全部显示'" @click="toggleRevealAll">
        <svg v-if="reveal.all" viewBox="0 0 16 16" width="15" height="15">
          <ellipse cx="8" cy="8" rx="6.2" ry="4" fill="none" stroke="currentColor" stroke-width="1.2" />
          <circle cx="8" cy="8" r="1.7" fill="currentColor" />
        </svg>
        <svg v-else viewBox="0 0 16 16" width="15" height="15">
          <ellipse cx="8" cy="8" rx="6.2" ry="4" fill="none" stroke="currentColor" stroke-width="1.2" />
          <line x1="2.5" y1="13.2" x2="13.5" y2="2.8" stroke="currentColor" stroke-width="1.2" />
        </svg>
      </button>
    </header>

    <div class="list">
      <p v-if="!state.accounts.length" class="empty">还没有账号</p>

      <div
        v-for="a in state.accounts"
        :key="a.id"
        class="card"
        :class="{ on: a.current }"
        role="button"
        tabindex="0"
        @click="selectAccount(a)"
        @keydown.enter="selectAccount(a)"
      >
        <span class="avatar">{{ a.name.slice(0, 1) }}</span>
        <span class="txt">
          <b :title="shown(a).name">{{ shown(a).name }}</b>
          <i :title="a.channelLabel + ' · ' + shown(a).id">
            <span v-if="a.credState && a.credState !== 'ok'" class="dot" :class="a.credState" :title="a.lastError || (a.credState === 'expired' ? '凭证已过期，需要刷新' : '凭证待验证')" />
            {{ a.channelLabel }} · {{ shown(a).id }}
          </i>
        </span>

        <span class="acts">
          <button
            class="act"
            :class="{ stop: a.running }"
            :disabled="busyId === a.id"
            :title="a.running ? '停止运行' : '启动光遇'"
            @click.stop="startOrStop(a)"
          >
            <svg v-if="a.running" viewBox="0 0 16 16" width="14" height="14">
              <rect x="4.5" y="4.5" width="7" height="7" rx="1.2" fill="currentColor" />
            </svg>
            <svg v-else viewBox="0 0 16 16" width="14" height="14">
              <path d="M5.5 3.6v8.8l6.8-4.4z" fill="currentColor" />
            </svg>
          </button>

          <button class="act" :class="{ off: !isRevealed(a.id) }" :title="isRevealed(a.id) ? '隐藏账号信息' : '查看账号信息'" @click.stop="toggleReveal(a.id)">
            <svg v-if="isRevealed(a.id)" viewBox="0 0 16 16" width="14" height="14">
              <ellipse cx="8" cy="8" rx="6.2" ry="4" fill="none" stroke="currentColor" stroke-width="1.3" />
              <circle cx="8" cy="8" r="1.7" fill="currentColor" />
            </svg>
            <svg v-else viewBox="0 0 16 16" width="14" height="14">
              <ellipse cx="8" cy="8" rx="6.2" ry="4" fill="none" stroke="currentColor" stroke-width="1.3" />
              <line x1="2.5" y1="13.2" x2="13.5" y2="2.8" stroke="currentColor" stroke-width="1.3" />
            </svg>
          </button>

          <n-dropdown trigger="click" :options="options" placement="bottom-end" @select="(k: string) => onMenu(k, a)">
            <button class="act more" title="更多" @click.stop>⋯</button>
          </n-dropdown>
        </span>
      </div>
    </div>

    <div class="adds">
      <button class="btn" @click="emit('add-channel')">添加渠道账号</button>
      <button class="btn accent" @click="emit('add-netease')">添加网易账号</button>
    </div>
  </aside>
</template>

<style scoped>
/* 玻璃面板 */
.rail {
  position: relative;
  display: flex;
  flex-direction: column;
  padding: 56px 16px 20px;
  background: color-mix(in srgb, var(--panel) 72%, transparent);
  border-right: 1px solid var(--line);
  backdrop-filter: blur(18px) saturate(130%);
}

.head {
  display: flex;
  align-items: center;
  padding: 0 8px 12px;
}

.label {
  font-size: 12px;
  color: var(--faint);
}

.eye {
  margin-left: auto;
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
.eye:hover {
  background: rgba(255, 255, 255, 0.06);
  color: var(--text);
}
.eye.off {
  opacity: 0.55;
}

.list {
  flex: 1;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 6px;
  /* 账号多的时候要能上下滚：细滚动条，不抢视觉 */
  scrollbar-width: thin;
  scrollbar-color: rgba(255, 255, 255, 0.16) transparent;
  padding-right: 2px;
}
.list::-webkit-scrollbar {
  width: 6px;
}
.list::-webkit-scrollbar-thumb {
  background: rgba(255, 255, 255, 0.16);
  border-radius: 3px;
}
.list::-webkit-scrollbar-track {
  background: transparent;
}

.empty {
  margin: 24px 0;
  text-align: center;
  font-size: 13px;
  color: var(--faint);
}

/* 账号卡片：玻璃底 + 选中金色描边和淡光 */
.card {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 9px 8px 9px 10px;
  border: 1px solid transparent;
  border-radius: 12px;
  background: rgba(255, 255, 255, 0.035);
  color: inherit;
  font-family: inherit;
  text-align: left;
  cursor: pointer;
  transition: background 0.16s ease-out, border-color 0.16s ease-out, box-shadow 0.16s ease-out;
}
.card:hover {
  background: rgba(255, 255, 255, 0.06);
}
.card.on {
  border-color: color-mix(in srgb, var(--accent) 50%, transparent);
  background: color-mix(in srgb, var(--accent) 10%, transparent);
  box-shadow: 0 8px 26px -18px color-mix(in srgb, var(--accent) 90%, transparent),
    inset 0 1px 0 rgba(255, 255, 255, 0.08);
}

.avatar {
  display: grid;
  place-items: center;
  width: 30px;
  height: 30px;
  border-radius: 9px;
  background: rgba(255, 255, 255, 0.08);
  font-size: 12px;
  color: var(--dim);
  flex: none;
}
.card.on .avatar {
  background: color-mix(in srgb, var(--accent) 20%, transparent);
  color: var(--accent);
}

.txt {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.txt b {
  font-size: 14px;
  font-weight: 400;
  color: var(--text);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.txt i {
  display: flex;
  align-items: center;
  gap: 4px;
  font-style: normal;
  font-size: 12px;
  color: var(--faint);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* 凭证异常：小圆点，不占地方 */
.dot {
  flex: none;
  width: 5px;
  height: 5px;
  border-radius: 50%;
  background: var(--dim);
}
.dot.expired {
  background: #e0a3a3;
}
.dot.stale {
  background: #d9c08a;
}

/* 右侧常驻按钮：启动/停止、查看/隐藏、更多 */
.acts {
  flex: none;
  display: flex;
  align-items: center;
  gap: 2px;
}
.act {
  display: grid;
  place-items: center;
  width: 22px;
  height: 22px;
  padding: 0;
  border: 0;
  border-radius: 6px;
  background: transparent;
  color: var(--faint);
  font-family: inherit;
  font-size: 14px;
  line-height: 1;
  cursor: pointer;
  transition: color 0.16s ease-out, background 0.16s ease-out;
}
.act:hover {
  background: rgba(255, 255, 255, 0.09);
  color: var(--text);
}
.act.stop:hover {
  color: #e0a3a3;
}
.act.off {
  opacity: 0.6;
}
.act:disabled {
  opacity: 0.35;
  cursor: default;
}
.act.more {
  font-size: 15px;
}

/* 底部两个添加入口 */
.adds {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding-top: 16px;
  border-top: 1px solid var(--line);
}

.btn {
  height: 36px;
  border: 1px solid var(--line);
  border-radius: 9px;
  background: rgba(255, 255, 255, 0.03);
  color: var(--dim);
  font-family: inherit;
  font-size: 13px;
  text-align: center;
  cursor: pointer;
  transition: background 0.16s ease-out, border-color 0.16s ease-out, color 0.16s ease-out;
}
.btn:hover {
  background: rgba(255, 255, 255, 0.07);
  border-color: var(--line-strong);
  color: var(--text);
}
.btn.accent {
  color: var(--accent);
  border-color: color-mix(in srgb, var(--accent) 42%, transparent);
  background: color-mix(in srgb, var(--accent) 8%, transparent);
}
.btn.accent:hover {
  background: color-mix(in srgb, var(--accent) 16%, transparent);
  border-color: color-mix(in srgb, var(--accent) 70%, transparent);
}
</style>
