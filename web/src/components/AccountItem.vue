<script setup lang="ts">
/**
 * 单个账号条目。
 *
 * 极简原则：不做卡片、不加边框。
 * 选中 = 左侧 2px 金色竖条 + 极淡背景；悬停才浮出小眼睛和「⋯」。
 */
import { computed } from "vue";
import { NDropdown } from "naive-ui";
import { isRevealed, toggleReveal } from "../composables/useAppState";
import { maskId, maskName } from "../composables/useMask";
import type { AccountView } from "../api/types";

const props = defineProps<{
  account: AccountView;
  options: { label: string; key: string }[];
}>();

const emit = defineEmits<{
  (e: "select"): void;
  (e: "menu", key: string): void;
}>();

const open = computed(() => isRevealed(props.account.id));
const shownName = computed(() => (open.value ? props.account.name : maskName(props.account.name)));
const shownId = computed(() => (open.value ? props.account.accountId : maskId(props.account.accountId)));
</script>

<template>
  <div class="item" :class="{ sel: account.current }" @click="emit('select')">
    <div class="main">
      <div class="name">{{ shownName }}</div>
      <div class="meta">
        {{ account.channelLabel }}<span class="sep">·</span>
        <span class="mono">{{ shownId }}</span>
      </div>
    </div>

    <div class="ops" @click.stop>
      <button class="op" :title="open ? '隐藏' : '显示'" @click="toggleReveal(account.id)">
        <svg v-if="open" viewBox="0 0 16 16" width="14" height="14">
          <ellipse cx="8" cy="8" rx="6.2" ry="4" fill="none" stroke="currentColor" stroke-width="1.2" />
          <circle cx="8" cy="8" r="1.7" fill="currentColor" />
        </svg>
        <svg v-else viewBox="0 0 16 16" width="14" height="14">
          <ellipse cx="8" cy="8" rx="6.2" ry="4" fill="none" stroke="currentColor" stroke-width="1.2" />
          <line x1="2.5" y1="13.2" x2="13.5" y2="2.8" stroke="currentColor" stroke-width="1.2" />
        </svg>
      </button>

      <n-dropdown trigger="click" :options="options" placement="bottom-end" @select="(k: string) => emit('menu', k)">
        <button class="op more">⋯</button>
      </n-dropdown>
    </div>
  </div>
</template>

<style scoped>
.item {
  position: relative;
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 8px 10px 12px;
  border-radius: var(--r-sm);
  cursor: pointer;
  transition: background 0.12s;
}
.item:hover {
  background: rgba(255, 255, 255, 0.025);
}
.item.sel {
  background: rgba(255, 255, 255, 0.04);
}
/* 选中指示：2px 金色竖条 */
.item.sel::before {
  content: "";
  position: absolute;
  left: 0;
  top: 10px;
  bottom: 10px;
  width: 2px;
  border-radius: 1px;
  background: var(--accent);
}

.main {
  flex: 1;
  min-width: 0;
}

/* 只在悬停时让正文淡一点，给操作图标让位；选中态不参与 */
.item:hover .meta {
  opacity: 0.6;
}

.name {
  font-size: 13.5px;
  color: var(--text);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.meta {
  margin-top: 4px;
  font-size: 11.5px;
  color: var(--faint);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.sep {
  margin: 0 5px;
  color: rgba(255, 255, 255, 0.16);
}

.ops {
  display: flex;
  align-items: center;
  gap: 2px;
  opacity: 0;
  transition: opacity 0.12s;
}
.item:hover .ops,
.item.sel .ops {
  opacity: 1;
}

.op {
  display: grid;
  place-items: center;
  width: 22px;
  height: 22px;
  padding: 0;
  border: 0;
  border-radius: 6px;
  background: transparent;
  color: var(--faint);
  cursor: pointer;
}
.op:hover {
  background: rgba(255, 255, 255, 0.07);
  color: var(--text);
}
.more {
  font-size: 13px;
  line-height: 1;
}
</style>



