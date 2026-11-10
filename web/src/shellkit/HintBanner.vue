<script setup lang="ts">
/**
 * 后端提示横幅：凭证过期这类「需要用户处理」的事，不能只躺在日志里。
 *
 * 一个账号一条提示（后端按账号存）。界面上：
 *   - 只有一条：一行胶囊，写清"哪个号 + 什么原因 + 刷新它"；
 *   - 多条：胶囊先给数量，点开是逐条清单，每条单独刷新。
 *
 * 为什么不是一条全局横幅：以前只有一条，多个号先后过期时后一个会盖掉前一个，
 * 用户只看到最后一个，还以为只有一个号有问题（2026-11-11 反馈）。
 */
import { computed, ref, watch } from "vue";
import { ChevronDown, X } from "lucide-vue-next";
import type { AuthHintItem, LauncherStore } from "./contract";

const props = defineProps<{ store: LauncherStore }>();
const store = props.store;

const hint = computed(() => (store.authHint.kind ? store.authHint : null));
const items = computed<AuthHintItem[]>(() => hint.value?.items ?? []);
const expanded = ref(false);

/** 用户手动关掉的那条"快照"：只有提示集合真的变了才重新出现 */
const dismissed = ref("");
const signature = computed(() =>
  items.value.map((it) => `${it.accountId ?? it.account ?? ""}|${it.message ?? ""}`).join("§"),
);
watch(signature, () => {
  dismissed.value = "";
});
watch(
  () => items.value.length,
  (n) => {
    if (n <= 1) expanded.value = false; // 只剩一条时没必要保持展开
  },
);
const visible = computed(() => items.value.length > 0 && dismissed.value !== signature.value);

/** 这条提示说的是哪个账号：ID 优先（重名不会串），其次名字，最后当前选中 */
function accountOf(it: AuthHintItem) {
  if (it.accountId) {
    const byId = store.accounts.find((a) => a.id === it.accountId);
    if (byId) return byId;
  }
  if (it.account) {
    return (
      store.accounts.find((a) => a.name === it.account) ??
      store.accounts.find((a) => a.name.includes(it.account!))
    );
  }
  return store.current;
}

function nameOf(it: AuthHintItem) {
  return accountOf(it)?.name ?? it.account ?? "这个账号";
}

function close() {
  dismissed.value = signature.value;
}

/** 单条刷新：刷完就把横幅收掉（后端成功后会撤掉这条提示） */
function refreshOne(it: AuthHintItem) {
  const acc = accountOf(it);
  if (acc) store.refreshCred(acc.id);
  if (items.value.length === 1) close();
}
</script>

<template>
  <Transition name="hintbar">
    <div v-if="visible" class="hintbar" :class="{ open: expanded && items.length > 1 }" role="alert">
      <div class="hrow">
        <!-- 一条：一行说清是哪个号 -->
        <template v-if="items.length === 1">
          <span class="hmsg">{{ items[0].message }}</span>
          <button
            class="hact"
            :title="`刷新「${nameOf(items[0])}」的凭证`"
            @click="refreshOne(items[0])"
          >
            刷新「{{ nameOf(items[0]) }}」
          </button>
        </template>

        <!-- 多条：先给数量，点开是清单 -->
        <template v-else>
          <button class="hsum" @click="expanded = !expanded">
            <b>{{ items.length }}</b> 个账号需要处理
            <ChevronDown class="hchev" :class="{ flip: expanded }" />
          </button>
          <button v-if="!expanded" class="hact" @click="expanded = true">查看</button>
        </template>

        <button class="hclose" aria-label="关闭" @click="close"><X /></button>
      </div>

      <ul v-if="expanded && items.length > 1" class="hlist">
        <li v-for="it in items" :key="it.accountId || it.message" class="hitem">
          <span class="hwho" :title="nameOf(it)">{{ nameOf(it) }}</span>
          <span class="hwhy" :title="it.message">{{ it.title || it.message }}</span>
          <button class="hact sm" @click="refreshOne(it)">刷新</button>
        </li>
      </ul>
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
  flex-direction: column;
  gap: 6px;
  max-width: min(720px, calc(100vw - 32px));
  padding: 8px 10px 8px 14px;
  border: 1px solid color-mix(in srgb, var(--warn) 40%, transparent);
  border-radius: 999px;
  background: color-mix(in srgb, var(--warn) 14%, #17171b);
  box-shadow: 0 10px 32px rgba(0, 0, 0, 0.45);
}
/* 展开成清单时改成卡片形状：胶囊圆角装不下一列 */
.hintbar.open {
  padding: 8px 10px 10px 14px;
  border-radius: 14px;
  /* 清单比胶囊宽：不给最小宽度的话，长名字 / 长原因会被压成一小截 */
  min-width: min(380px, calc(100vw - 32px));
}
.hrow {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  min-width: 0;
}
.hmsg {
  flex: 1 1 auto;
  min-width: 0;
  font-size: 12.5px;
  color: #f0eee8;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.hsum {
  flex: 1 1 auto;
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 0;
  border: 0;
  background: transparent;
  color: #f0eee8;
  font-family: inherit;
  font-size: 12.5px;
  text-align: left;
  cursor: pointer;
}
.hsum b {
  color: var(--warn);
  font-size: 13px;
}
.hchev {
  width: 13px;
  height: 13px;
  color: #b7b1a6;
  transition: transform 0.15s ease-out;
}
.hchev.flip {
  transform: rotate(180deg);
}
.hact {
  flex: 0 1 auto;
  max-width: 42%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
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
.hact.sm {
  height: 22px;
  padding: 0 10px;
  font-size: 11.5px;
  max-width: none;
}
.hact:hover {
  filter: brightness(1.08);
}
.hclose {
  flex: none;
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
/* 逐条清单：一行一个号，左边名字、中间原因、右边刷新 */
.hlist {
  margin: 0;
  padding: 0;
  list-style: none;
  display: flex;
  flex-direction: column;
  max-height: 40vh;
  overflow-y: auto;
}
.hitem {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 5px 2px;
  border-top: 1px solid rgba(255, 255, 255, 0.07);
}
.hitem:first-child {
  border-top: 0;
}
.hwho {
  flex: 0 1 auto;
  max-width: 44%;
  font-size: 12px;
  font-weight: 600;
  color: #f0eee8;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.hwhy {
  flex: 1 1 auto;
  min-width: 0;
  font-size: 11.5px;
  color: #b7b1a6;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
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
