<script setup lang="ts">
/**
 * 登录入口选择（shellkit 设计语言）。
 *
 * 这个组件不再认识任何具体渠道——列表完全由后端 `/api/state` 的
 * `loginChannels.entries` 驱动：
 *
 *   后端多一项 → 这里多一行
 *   后端少一项 → 这里自动消失
 *
 * 每一项可能带多个登录方式（methods）。只有一个方式的，点了直接走；
 * 标了 group 的（目前只有「网易账号」）点了进二级菜单。
 *
 * mode:
 *   add      —— 添加账号，显示全部入口
 *   其它值   —— 给某个渠道刷新凭证，只显示该渠道的方式（action 与 mode 相同）
 */
import { computed, ref, watch } from "vue";
import { ChevronLeft, X } from "lucide-vue-next";
import type { LoginEntryView, LoginMethodView } from "../api/types";

/** 渲染用的扁平项 */
interface Item {
  id: string;
  action: string;
  label: string;
  hint: string;
  initial: string;
  kind: string;
  /** 方式标识（应用宝的微信/QQ 都是 kind=qr，靠它区分） */
  method: string;
}

const props = defineProps<{
  show: boolean;
  mode: string;
  entries: LoginEntryView[];
}>();

const emit = defineEmits<{
  (e: "update:show", v: boolean): void;
  (e: "pick", v: { action: string; kind: string; method: string }): void;
}>();

/** 展开中的二级分组 id（弹窗内部状态，不惊动父层） */
const groupId = ref<string | null>(null);
watch(
  () => props.show,
  (v) => {
    if (!v) groupId.value = null;
  }
);

/** 把一个入口的某个方式转成可点项 */
function toItem(entry: LoginEntryView, m: LoginMethodView): Item {
  return {
    id: `${entry.id}:${m.kind}`,
    action: entry.action,
    label: m.label,
    hint: m.hint,
    initial: entry.initial,
    kind: m.kind,
    method: m.method || m.label,
  };
}

const items = computed<Item[]>(() => {
  const entries = props.entries ?? [];
  if (props.mode !== "add") {
    // 刷新凭证：只看这个渠道的登录方式，分组直接摊平
    const out: Item[] = [];
    for (const e of entries) {
      if (e.action !== props.mode) continue;
      for (const m of e.methods) out.push(toItem(e, m));
    }
    return out;
  }
  if (groupId.value) {
    const g = entries.find((e) => e.id === groupId.value);
    return g ? g.methods.map((m) => toItem(g, m)) : [];
  }
  return entries
    .filter((e) => !e.onlyRefresh)
    .map((e) => ({
      id: e.id,
      action: e.action,
      label: e.label,
      hint: e.hint,
      initial: e.initial,
      kind: e.group ? "group" : (e.methods[0]?.kind ?? ""),
      method: e.group ? "" : (e.methods[0]?.method || e.methods[0]?.label || ""),
    }));
});

/** 二级菜单时显示的父项（用于标题） */
const groupEntry = computed(() => props.entries.find((e) => e.id === groupId.value) || null);

const title = computed(() => {
  if (props.mode === "add") {
    return groupEntry.value ? `添加账号 · ${groupEntry.value.label.replace("账号", "")}` : "添加账号";
  }
  const e = props.entries.find((x) => x.action === props.mode);
  return e ? `刷新凭证 · ${e.label.replace("账号", "")}` : "刷新凭证";
});

const tip = computed(() => {
  if (props.mode === "add" && !groupId.value) {
    const scan = props.entries.find((e) => e.id === "netease-channel");
    return scan?.hint || "";
  }
  const first = items.value[0];
  return first ? first.hint : "";
});

function onPick(item: Item) {
  if (item.kind === "group") {
    groupId.value = item.id;
    return;
  }
  emit("pick", { action: item.action, kind: item.kind, method: item.method });
}

function close() {
  emit("update:show", false);
}
</script>

<template>
  <div v-if="props.show" class="mask" @click.self="close">
    <div class="modal">
      <div class="mhead">
        <button v-if="groupId" class="mback" title="返回" @click="groupId = null"><ChevronLeft /></button>
        <b>{{ title }}</b>
        <button class="mclose" @click="close"><X /></button>
      </div>

      <div class="list">
        <button v-for="e in items" :key="e.id" class="entry" @click="onPick(e)">
          <span class="icon">{{ e.initial }}</span>
          <span class="text">
            <b>{{ e.label }}</b>
            <small>{{ e.hint }}</small>
          </span>
        </button>
      </div>

      <p class="tip">{{ tip }}</p>
    </div>
  </div>
</template>

<style scoped>
.mask {
  position: fixed;
  inset: 0;
  z-index: 60;
  display: grid;
  place-items: center;
  background: rgba(0, 0, 0, 0.55);
}
.modal {
  --acc: #d8b47c;
  --tx: #f0eee8;
  --dim: #9aa0ac;
  --faint: #6b7382;
  --line: rgba(255, 255, 255, 0.08);
  --line2: rgba(255, 255, 255, 0.14);
  width: 420px;
  max-width: calc(100vw - 48px);
  padding: 20px;
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 16px;
  background: #17171b;
  color: var(--tx);
  box-shadow: 0 32px 80px rgba(0, 0, 0, 0.6);
}
.mhead {
  display: flex;
  align-items: center;
  margin-bottom: 14px;
}
.mhead b {
  flex: 1;
  font-size: 16px;
  font-weight: 600;
}
.mback {
  display: grid;
  place-items: center;
  width: 28px;
  height: 28px;
  margin-right: 8px;
  border: 0;
  border-radius: 8px;
  background: transparent;
  color: #8f8a80;
  cursor: pointer;
}
.mback:hover {
  background: rgba(255, 255, 255, 0.07);
  color: var(--tx);
}
.mback svg {
  width: 15px;
  height: 15px;
}
.mclose {
  display: grid;
  place-items: center;
  width: 28px;
  height: 28px;
  border: 0;
  border-radius: 8px;
  background: transparent;
  color: #8f8a80;
  cursor: pointer;
}
.mclose:hover {
  background: rgba(255, 255, 255, 0.07);
  color: var(--tx);
}
.mclose svg {
  width: 15px;
  height: 15px;
}

.list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.entry {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 14px;
  border: 1px solid var(--line);
  border-radius: 12px;
  background: rgba(255, 255, 255, 0.03);
  color: var(--tx);
  font-family: inherit;
  text-align: left;
  cursor: pointer;
  transition: background 0.15s ease-out, border-color 0.15s ease-out;
}
.entry:hover {
  background: rgba(255, 255, 255, 0.06);
  border-color: var(--line2);
}
.icon {
  display: grid;
  place-items: center;
  width: 32px;
  height: 32px;
  border-radius: 9px;
  background: rgba(255, 255, 255, 0.07);
  color: var(--dim);
  font-size: 12.5px;
  font-weight: 600;
  flex: none;
}
.text {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.text b {
  font-size: 13.5px;
  font-weight: 500;
}
.text small {
  font-size: 12px;
  color: var(--faint);
}
.tip {
  margin: 14px 0 0;
  font-size: 11.5px;
  line-height: 1.7;
  color: var(--faint);
}
</style>




