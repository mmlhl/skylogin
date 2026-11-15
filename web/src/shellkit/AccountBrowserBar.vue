<script setup lang="ts">
import { Search, X } from "lucide-vue-next";
import type { LauncherStore } from "./contract";
import type { AccountBrowser } from "./useAccountBrowser";
const props = defineProps<{ browser: AccountBrowser; store: LauncherStore }>();
const views = [{ key: "flat", label: "平铺" }, { key: "tag", label: "标签" }, { key: "channel", label: "渠道" }, { key: "time", label: "时间" }] as const;
</script>
<template>
  <div class="account-browser-bar">
    <div class="browser-controls">
      <label class="browser-search"><Search /><input v-model="browser.search.value" type="search" placeholder="搜索账号、标签或渠道" aria-label="搜索账号、标签或渠道" /><button v-if="browser.search.value" type="button" title="清空搜索" @click="browser.search.value = ''"><X /></button></label>
      <div class="browser-views" role="group" aria-label="账号分组方式"><button v-for="item in views" :key="item.key" type="button" :aria-pressed="browser.view.value === item.key" :class="{ on: browser.view.value === item.key }" @click="browser.view.value = item.key">{{ item.label }}</button></div>
      <select v-model="browser.sort.value" aria-label="账号查看顺序" :disabled="browser.view.value !== 'flat' || browser.busy.value"><option value="custom">手动顺序</option><option value="name">名称</option><option value="recent">最近登录</option><option value="running">运行优先</option></select>
    </div>
    <div class="browser-summary">
      <span>{{ browser.visibleCount.value }} / {{ store.accounts.length }} 个账号<template v-if="browser.selectedCount.value"> · 已选 {{ browser.selectedCount.value }}</template></span>
      <button v-if="browser.selectedCount.value" type="button" @click="browser.clearSelection()">清空勾选</button>
      <span v-if="browser.message.value" class="browser-message" :class="{ error: browser.error.value }" role="status">{{ browser.message.value }}</span>
      <span v-else class="browser-hint">{{ browser.view.value === 'tag' ? '多标签账号会在各组显示' : browser.view.value === 'flat' && browser.sort.value !== 'custom' ? '仅改变查看方式，手动顺序保留' : '顺序调整后自动保存' }}</span>
    </div>
  </div>
</template>
<style scoped>
.account-browser-bar { flex: none; min-width: 0; padding: 0 28px 12px; color: var(--dim); }
.browser-controls { display:flex; align-items:center; flex-wrap:wrap; gap:8px; min-width:0; }
.browser-search { display:flex; align-items:center; gap:7px; flex:1 1 210px; min-width:120px; height:32px; padding:0 9px; border:1px solid var(--line2, var(--line)); border-radius:8px; background:var(--s1, var(--bg)); }
.browser-search svg { width:14px; height:14px; flex:none; }
.browser-search input { width:0; min-width:0; flex:1; border:0; outline:none; background:transparent; color:var(--tx); font:inherit; font-size:12px; }
.browser-search input::placeholder { color:var(--dim); }
.browser-search:focus-within { border-color:var(--acc); }
.browser-search input::-webkit-search-cancel-button { display:none; }
button, select { font:inherit; font-size:12px; color:var(--dim); cursor:pointer; }
button { border:0; background:transparent; }
.browser-search button { display:grid; place-items:center; padding:2px; }
.browser-views { display:flex; flex:none; gap:2px; padding:3px; border:1px solid var(--line); border-radius:8px; }
.browser-views button { padding:3px 9px; border-radius:5px; white-space:nowrap; }
.browser-views button.on { background:color-mix(in srgb,var(--acc) 14%,transparent); color:var(--acc); }
select { min-width:90px; max-width:100%; height:32px; padding:0 6px; border:1px solid var(--line); border-radius:8px; background:var(--s1,var(--bg)); }
select:disabled { opacity:.5; cursor:default; }
.browser-summary { display:flex; align-items:center; flex-wrap:wrap; gap:5px 10px; margin-top:7px; font-size:11px; min-height:18px; }
.browser-summary button { color:var(--acc); font-size:11px; padding:0; }
.browser-hint,.browser-message { margin-left:auto; overflow-wrap:anywhere; }
.browser-message.error { color:var(--bad); }
button:focus-visible,select:focus-visible { outline:2px solid var(--acc); outline-offset:2px; }
</style>
