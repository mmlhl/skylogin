<script setup lang="ts">
import { computed } from "vue";
import { ArrowUp, ArrowDown, ChevronDown } from "lucide-vue-next";
import type { AccountSection } from "./accountList";
import { tagIdentity } from "./accountList";
import type { AccountBrowser } from "./useAccountBrowser";
const props = defineProps<{ browser: AccountBrowser; section: AccountSection }>();
const index = computed(() => props.browser.groupOrder.value.findIndex(t => tagIdentity(t) === tagIdentity(props.section.tag ?? "")));
const disabled = computed(() => props.browser.busy.value || !!props.browser.search.value.trim());
</script>
<template>
  <div class="account-section-header" :data-section="section.key">
    <input type="checkbox" :checked="!!section.rows.length && browser.sectionSelected(section) === section.rows.length" :indeterminate="browser.sectionSelected(section) > 0 && browser.sectionSelected(section) < section.rows.length" :aria-label="'勾选分组：' + section.label" @change="browser.selectSection(section)" />
    <button class="section-label" type="button" :aria-expanded="!browser.isCollapsed(section)" :title="section.label" @click="browser.toggleSection(section)"><ChevronDown :class="{ collapsed: browser.isCollapsed(section) }" /><b>{{ section.label }}</b><small v-if="section.untagged">无标签</small><span>{{ section.rows.length }}</span></button>
    <div v-if="section.tag" class="section-moves" :title="browser.search.value.trim() ? '清空搜索后可调整标签组顺序' : '调整标签组顺序'">
      <button type="button" title="标签组上移" :disabled="disabled || index <= 0" @click="browser.moveGroup(section, -1)"><ArrowUp /></button>
      <button type="button" title="标签组下移" :disabled="disabled || index >= browser.groupOrder.value.length - 1" @click="browser.moveGroup(section, 1)"><ArrowDown /></button>
    </div>
  </div>
</template>
<style scoped>
.account-section-header { display:flex; align-items:center; gap:8px; min-width:0; padding:7px 2px; color:var(--dim); }
input { flex:none; width:14px; height:14px; margin:0; accent-color:var(--acc); cursor:pointer; }
button { font:inherit; color:inherit; background:transparent; border:0; cursor:pointer; }
.section-label { flex:1; min-width:0; padding:2px 0; display:flex; align-items:center; gap:7px; text-align:left; }
.section-label b { font-size:12px; font-weight:600; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
.section-label small,.section-label span { flex:none; font-size:10px; font-weight:400; }
svg { width:13px; height:13px; flex:none; }
.collapsed { transform:rotate(-90deg); }
.section-moves { display:flex; flex:none; }
.section-moves button { display:grid; place-items:center; width:24px; height:24px; border-radius:5px; }
button:hover { color:var(--acc); }
button:disabled { opacity:.3; cursor:default; }
button:focus-visible,input:focus-visible { outline:2px solid var(--acc); outline-offset:2px; }
</style>
