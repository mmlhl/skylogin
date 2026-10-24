<script setup lang="ts">
/**
 * 多开弹窗：勾选要同时启动的账号。
 *
 * 为什么要用户来勾：多开有风险（官方可能判定异常），
 * 而且账号多了全开也没必要，默认只勾前两个。
 *
 * 上限 4 个：再多互斥检查的竞态窗口会不够稳，也没意义。
 */
import { computed, ref, watch } from "vue";
import { NModal, NCheckbox, useMessage } from "naive-ui";
import { api } from "../api/client";
import { state } from "../composables/useAppState";

const props = defineProps<{ show: boolean }>();
const emit = defineEmits<{
  (e: "update:show", v: boolean): void;
  (e: "launched"): void;
}>();

// 不限制数量：能开几个取决于机器，不去人为设上限
const message = useMessage();
const picked = ref<string[]>([]);
const busy = ref(false);

/** 每次打开时默认选中前两个账号 */
watch(
  () => props.show,
  (v) => {
    if (v) picked.value = state.accounts.slice(0, 2).map((a) => a.id);
  }
);

const canLaunch = computed(() => picked.value.length >= 2);

function toggle(id: string) {
  const i = picked.value.indexOf(id);
  if (i >= 0) picked.value.splice(i, 1);
  else picked.value.push(id);
}

async function launch() {
  if (!canLaunch.value || busy.value) return;
  busy.value = true;
  try {
    const r = await api.launchMulti(picked.value);
    if (r.ok === false) {
      message.error(r.message || "多开失败");
      return;
    }
    message.success(r.message || "已启动");
    message.info("客户端会各自独立登录，可以同时点", { duration: 5000 });
    emit("launched");
    emit("update:show", false);
  } catch (e) {
    // 接口异常也要让用户看到，不能"点了没反应"
    message.error(e instanceof Error ? e.message : "多开失败");
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <n-modal
    :show="show"
    preset="card"
    title="多开"
    style="width: 420px"
    display-directive="if"
    @update:show="(v: boolean) => emit('update:show', v)"
  >
    <p class="tip">每个账号会自动开一个客户端，数量不限（按机器性能来）。</p>
    <p v-if="state.settings.mirrorDir" class="tip">
      第 2 个起从 <code>{{ state.settings.mirrorDir }}</code> 启动：
      {{
        state.settings.mirrorSameVolume
          ? "硬链接，不额外占空间"
          : `与游戏不同盘，会复制约 ${state.settings.mirrorSizeText}`
      }}，客户端全部退出后自动删除。
    </p>
    
    <div class="list">
      <button
        v-for="a in state.accounts"
        :key="a.id"
        class="row"
        :class="{ on: picked.includes(a.id) }"
        @click="toggle(a.id)"
      >
        <n-checkbox :checked="picked.includes(a.id)" @click.stop />
        <span class="txt">
          <b>{{ a.name }}</b>
          <i>{{ a.channelLabel }}</i>
        </span>
      </button>
    </div>

    <p v-if="!state.accounts.length" class="empty">还没有账号</p>
    <p v-else-if="state.accounts.length < 2" class="empty">多开至少需要 2 个账号</p>

    <template #footer>
      <div class="foot">
        <span class="count">已选 {{ picked.length }} 个</span>
        <button class="primary" :disabled="!canLaunch || busy" @click="launch">
          {{ busy ? "正在启动…" : "启动" }}
        </button>
      </div>
    </template>
  </n-modal>
</template>

<style scoped>
.tip {
  margin: 0 0 14px;
  font-size: 12.5px;
  color: var(--faint);
}

/* 这条是操作要点，用主题色点出来，别让人漏看 */
.tip.warn {
  margin-top: -6px;
  color: var(--dim);
}
.tip.warn strong {
  color: var(--accent);
  font-weight: 600;
}

.list {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 12px;
  border: 1px solid var(--line);
  border-radius: var(--r-sm);
  background: rgba(255, 255, 255, 0.02);
  color: var(--text);
  font-family: inherit;
  text-align: left;
  cursor: pointer;
  transition: border-color 0.16s ease-out, background 0.16s ease-out;
}
.row:hover {
  background: rgba(255, 255, 255, 0.05);
}
.row.on {
  border-color: color-mix(in srgb, var(--accent) 45%, transparent);
  background: color-mix(in srgb, var(--accent) 8%, transparent);
}

.txt {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.txt b {
  font-size: 13.5px;
  font-weight: 400;
}
.txt i {
  font-style: normal;
  font-size: 12px;
  color: var(--faint);
}

.empty {
  margin: 12px 0;
  text-align: center;
  font-size: 13px;
  color: var(--faint);
}

.foot {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 14px;
}

.count {
  font-size: 12px;
  color: var(--faint);
}

.primary {
  height: 34px;
  padding: 0 20px;
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

