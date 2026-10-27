<script setup lang="ts">
/**
 * 后端提示横幅：凭证过期这类「需要用户处理」的事，不能只躺在日志里。
 * 出现在窗口顶部，带一键刷新；点刷新会按提示里的账号打开对应登录流程。
 */
import { computed, ref, watch } from "vue";
import { X } from "lucide-vue-next";
import type { LauncherStore } from "./contract";

const props = defineProps<{ store: LauncherStore }>();
const store = props.store;

const hint = computed(() => (store.authHint.kind ? store.authHint : null));
/** 用户手动关掉的那条提示；只有提示内容真的变了才重新出现 */
const dismissed = ref("");
const hintKey = computed(() => {
  const h = hint.value;
  return h ? `${h.kind ?? ""}|${h.message ?? ""}|${h.account ?? ""}` : "";
});
watch(hintKey, () => {
  dismissed.value = "";
});
const visible = computed(() => !!hint.value && dismissed.value !== (hint.value!.message ?? ""));

function close() {
  if (hint.value) dismissed.value = hint.value.message ?? "";
}

function refresh() {
  const h = hint.value;
  if (!h) return;
  // 先按 ID 精确定位（重名账号不会串）；老提示没有 ID 再按名字兜底
  const acc =
    (h.accountId && store.accounts.find((a) => a.id === h.accountId)) ??
    store.accounts.find((a) => a.name === h.account) ??
    store.accounts.find((a) => h.account && a.name.includes(h.account)) ??
    store.current;
  if (acc) store.refreshCred(acc.id);
  close();
}
</script>

<template>
  <Transition name="hintbar">
    <div v-if="visible && hint" class="hintbar" role="alert">
      <span class="hmsg">{{ hint.message || "登录凭证已过期，需要重新登录" }}</span>
      <button class="hact" @click="refresh">刷新凭证</button>
      <button class="hclose" aria-label="关闭" @click="close"><X /></button>
    </div>
  </Transition>
</template>

<style scoped>
.hintbar {
  --acc: var(--demo-acc, #d8b47c);
  --warn: #e08d7c;
  position: fixed;
  top: 8px;
  left: 50%;
  transform: translateX(-50%);
  z-index: 45;
  display: flex;
  align-items: center;
  gap: 10px;
  max-width: min(720px, calc(100vw - 32px));
  padding: 8px 10px 8px 14px;
  border: 1px solid color-mix(in srgb, var(--warn) 40%, transparent);
  border-radius: 999px;
  background: color-mix(in srgb, var(--warn) 14%, #17171b);
  box-shadow: 0 10px 32px rgba(0, 0, 0, 0.45);
}
.hmsg {
  font-size: 12.5px;
  color: #f0eee8;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.hact {
  flex: none;
  height: 26px;
  padding: 0 12px;
  border: 0;
  border-radius: 999px;
  background: var(--warn);
  color: #1a0e0a;
  font-family: inherit;
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
}
.hact:hover {
  filter: brightness(1.08);
}
.hclose {
  display: grid;
  place-items: center;
  width: 22px;
  height: 22px;
  border: 0;
  border-radius: 6px;
  background: transparent;
  color: #8f8a80;
  cursor: pointer;
}
.hclose:hover {
  background: rgba(255, 255, 255, 0.08);
  color: #f0eee8;
}
.hclose svg {
  width: 13px;
  height: 13px;
}
.hintbar-enter-active,
.hintbar-leave-active {
  transition: opacity 0.16s ease-out, transform 0.16s ease-out;
}
.hintbar-enter-from,
.hintbar-leave-to {
  opacity: 0;
  transform: translateX(-50%) translateY(-6px);
}
</style>
