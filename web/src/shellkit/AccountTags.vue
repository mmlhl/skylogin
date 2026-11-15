<script setup lang="ts">
import { computed, ref } from "vue";
import { Plus, X } from "lucide-vue-next";
import type { Account, LauncherStore } from "./contract";
import { orderedAccountTags, tagIdentity } from "./accountList";
import { accountTagState } from "./accountTagState";
const props = defineProps<{ store: LauncherStore; account: Account; editable?: boolean }>();
const { drafts, messages, errors, saving } = accountTagState(props.store);
const editing = ref(false);
const suggestions = computed(() => orderedAccountTags(props.store.accounts, props.store.tagOrder ?? []).filter(t => !props.account.tags.some(existing => tagIdentity(existing) === tagIdentity(t))).filter(t => !drafts.value[props.account.id] || tagIdentity(t).includes(tagIdentity(drafts.value[props.account.id]!))).slice(0, 5));
async function save(a: Account, tags: string[], clearDraft = false) {
  if (saving.value[a.id]) return;
  const draft = drafts.value[a.id];
  saving.value[a.id] = true; messages.value[a.id] = "正在保存…"; errors.value[a.id] = false;
  try {
    const ok = await props.store.setTags(a.id, tags);
    messages.value[a.id] = ok ? "标签已保存" : "未保存，请重试"; errors.value[a.id] = !ok;
    if (ok && clearDraft && drafts.value[a.id] === draft) drafts.value[a.id] = "";
  } catch { messages.value[a.id] = "未保存，请重试"; errors.value[a.id] = true; }
  finally { saving.value[a.id] = false; }
}
async function add(value?: string) {
  const a = props.account;
  const tag = (value ?? drafts.value[a.id] ?? "").trim();
  if (!tag || saving.value[a.id]) return;
  let error = "";
  if ([...tag].length > 16) error = "每个标签最多 16 个字";
  else if (a.tags.some(t => tagIdentity(t) === tagIdentity(tag))) error = "这个账号已有该标签";
  else if (a.tags.length >= 8) error = "每个账号最多 8 个标签";
  if (error) { messages.value[a.id] = error; errors.value[a.id] = true; return; }
  await save(a, [...a.tags, tag], true);
}
function enter(e: KeyboardEvent) { if (e.isComposing || e.keyCode === 229) return; e.preventDefault(); void add(); }
</script>
<template>
  <div class="account-tags" @click.stop @keydown.stop>
    <div class="tag-chips">
      <span v-for="tag in account.tags" :key="tag" class="account-tag" :title="tag"><span>{{ tag }}</span><button v-if="editable && editing" type="button" :aria-label="'移除标签：' + tag" :disabled="saving[account.id]" @click="save(account, account.tags.filter(t => t !== tag))"><X /></button></span>
      <span v-if="!account.tags.length && !editable" class="no-tags">无标签</span>
      <button v-if="editable" class="edit-tags" type="button" :aria-expanded="editing" @click="editing = !editing"><Plus v-if="!editing" />{{ editing ? '收起标签' : account.tags.length ? '编辑标签' : '添加标签' }}</button>
    </div>
    <template v-if="editable && editing">
      <div class="tag-entry"><input v-model="drafts[account.id]" :disabled="saving[account.id] || account.tags.length >= 8" :placeholder="account.tags.length >= 8 ? '已达 8 个标签上限' : '新标签（最多 16 字）'" :aria-label="'为' + (store.maskAll ? account.maskedName : account.name) + '添加标签'" @keydown.enter="enter" /><button type="button" :disabled="saving[account.id] || account.tags.length >= 8 || !drafts[account.id]?.trim()" @click="add()">添加</button></div>
      <div v-if="suggestions.length && account.tags.length < 8" class="tag-suggestions"><button v-for="tag in suggestions" :key="tag" type="button" :title="'添加标签：' + tag" :disabled="saving[account.id]" @click="add(tag)">{{ tag }}</button></div>
      <p v-if="messages[account.id]" class="tag-message" :class="{ error: errors[account.id] }" role="status">{{ messages[account.id] }}</p>
    </template>
  </div>
</template>
<style scoped>
.account-tags { min-width:0; margin:8px 0; }
.tag-chips { display:flex; align-items:center; flex-wrap:wrap; gap:5px; min-width:0; }
.account-tag { display:inline-flex; align-items:center; gap:3px; max-width:100%; min-width:0; border-radius:5px; background:color-mix(in srgb,var(--acc) 10%,transparent); color:var(--acc); font-size:10px; padding:2px 6px; }
.account-tag > span { min-width:0; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
button { display:inline-flex; align-items:center; justify-content:center; font:inherit; color:var(--dim); cursor:pointer; border:0; background:transparent; }
svg { width:12px; height:12px; flex:none; }
.account-tag button { flex:none; padding:0; color:inherit; }
.no-tags { font-size:11px; color:var(--dim); }
.edit-tags { gap:3px; font-size:10px; padding:2px 4px; }
.tag-entry { display:flex; gap:6px; margin-top:8px; min-width:0; }
.tag-entry input { flex:1; width:0; min-width:0; height:28px; padding:0 8px; color:var(--tx); background:var(--s1,var(--bg)); border:1px solid var(--line2,var(--line)); border-radius:6px; font:inherit; font-size:11px; }
.tag-entry input::placeholder { color:var(--dim); }
.tag-entry > button { flex:none; font-size:11px; color:var(--acc); }
.tag-suggestions { display:flex; flex-wrap:wrap; gap:4px; margin-top:6px; }
.tag-suggestions button { max-width:100%; overflow:hidden; display:block; text-overflow:ellipsis; white-space:nowrap; font-size:10px; padding:3px 6px; border:1px solid var(--line); border-radius:5px; }
.tag-message { margin:6px 0 0; color:var(--dim); font-size:11px; overflow-wrap:anywhere; }
.tag-message.error { color:var(--bad); }
button:disabled { opacity:.4; cursor:default; }
button:focus-visible,input:focus-visible { outline:2px solid var(--acc); outline-offset:2px; }
</style>
