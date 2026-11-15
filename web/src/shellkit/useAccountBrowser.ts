import { computed, ref } from "vue";
import { channelLabel, type Account, type LauncherStore } from "./contract";
import { accountTimeBucket, matchesAccount, orderedAccountTags, replaceAccountSubsequence, tagIdentity, type AccountListView, type AccountSection, type AccountSort } from "./accountList";

/** 各主题共享分组与保存规则，只保留查看状态，不另建账号数据来源。 */
export function useAccountBrowser(store: LauncherStore) {
  const search = ref("");
  const view = ref<AccountListView>("flat");
  const sort = ref<AccountSort>("custom");
  const collapsed = ref<string[]>([]);
  const busy = ref(false);
  const message = ref("");
  const error = ref(false);
  const groupOrder = computed(() => orderedAccountTags(store.accounts, store.tagOrder ?? []));
  const visible = computed(() => store.accounts.filter(a => matchesAccount(a, search.value, channelLabel)));
  const visibleCount = computed(() => visible.value.length);
  const selectedCount = computed(() => store.accounts.filter(a => store.selectedIds.includes(a.id)).length);
  const displayName = (a: Account) => store.maskAll ? a.maskedName : a.name;
  const displayId = (a: Account) => store.maskAll ? a.maskedId : a.fullId;
  const sections = computed<AccountSection[]>(() => {
    const rows = visible.value;
    if (view.value === "flat") {
      const ordered = [...rows];
      if (sort.value === "name") ordered.sort((a, b) => a.name.localeCompare(b.name, "zh"));
      if (sort.value === "recent") ordered.sort((a, b) => (b.lastLoginTs ?? 0) - (a.lastLoginTs ?? 0));
      if (sort.value === "running") ordered.sort((a, b) => Number(b.running) - Number(a.running));
      return [{ key: "flat", label: "", rows: ordered, draggable: sort.value === "custom" }];
    }
    if (view.value === "tag") {
      const groups: AccountSection[] = groupOrder.value.map(tag => ({ key: "tag:" + tagIdentity(tag), label: tag, tag, rows: rows.filter(a => a.tags.some(t => tagIdentity(t) === tagIdentity(tag))), draggable: true }));
      groups.push({ key: "system:untagged", label: "未分组", untagged: true, rows: rows.filter(a => !a.tags.length), draggable: true });
      return groups.filter(s => s.rows.length);
    }
    if (view.value === "channel") {
      const names = [...new Set(rows.map(a => channelLabel(a.channel)))];
      const preferred = ["华为渠道", "网易官方", "B站渠道", "4399渠道", "vivo渠道"];
      const order = [...preferred.filter(n => names.includes(n)), ...names.filter(n => !preferred.includes(n))];
      return order.map(label => ({ key: "channel:" + label, label, rows: rows.filter(a => channelLabel(a.channel) === label), draggable: false }));
    }
    return ["最近 7 天", "8–30 天", "30 天以上"].map(label => ({ key: "time:" + label, label, rows: rows.filter(a => accountTimeBucket(a) === label), draggable: false })).filter(s => s.rows.length);
  });
  const isCollapsed = (sec: AccountSection) => collapsed.value.includes(sec.key);
  function toggleSection(sec: AccountSection) { collapsed.value = isCollapsed(sec) ? collapsed.value.filter(key => key !== sec.key) : [...collapsed.value, sec.key]; }
  const sectionSelected = (sec: AccountSection) => sec.rows.filter(a => store.selectedIds.includes(a.id)).length;
  function selectSection(sec: AccountSection) {
    const target = sectionSelected(sec) !== sec.rows.length;
    for (const a of sec.rows) if (store.selectedIds.includes(a.id) !== target) store.toggleSelect(a.id);
  }
  function clearSelection() { for (const id of [...store.selectedIds]) store.toggleSelect(id); }
  async function save(action: () => Promise<boolean>, label: string) {
    if (busy.value) return;
    busy.value = true; message.value = "正在保存…"; error.value = false;
    try { const ok = await action(); message.value = ok ? `${label}已保存` : `${label}未保存，请重试`; error.value = !ok; }
    catch { message.value = `${label}未保存，请重试`; error.value = true; }
    finally { busy.value = false; }
  }
  async function moveGroup(sec: AccountSection, delta: number) {
    if (!sec.tag || search.value.trim() || busy.value) return;
    const next = [...groupOrder.value];
    const at = next.findIndex(t => tagIdentity(t) === tagIdentity(sec.tag!));
    const to = at + delta;
    if (at < 0 || to < 0 || to >= next.length) return;
    [next[at], next[to]] = [next[to]!, next[at]!];
    await save(() => store.setTagOrder(next), "标签组顺序");
  }
  async function moveAccount(sec: AccountSection, id: string, delta: number) {
    if (!sec.draggable || busy.value) return;
    const ids = sec.rows.map(a => a.id);
    const at = ids.indexOf(id), to = at + delta;
    if (at < 0 || to < 0 || to >= ids.length) return;
    [ids[at], ids[to]] = [ids[to]!, ids[at]!];
    const next = replaceAccountSubsequence(store.accounts.map(a => a.id), ids);
    await save(() => store.reorder(next), "账号顺序");
  }
  return { search, view, sort, sections, visibleCount, selectedCount, busy, message, error, groupOrder, toggleSection, isCollapsed, sectionSelected, selectSection, clearSelection, moveGroup, moveAccount, displayName, displayId };
}
export type AccountBrowser = ReturnType<typeof useAccountBrowser>;
