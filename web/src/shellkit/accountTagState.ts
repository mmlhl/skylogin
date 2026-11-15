import { ref } from "vue";
import type { LauncherStore } from "./contract";

function createState() {
  return {
    drafts: ref<Record<string, string>>({}),
    messages: ref<Record<string, string>>({}),
    errors: ref<Record<string, boolean>>({}),
    saving: ref<Record<string, boolean>>({}),
  };
}
const states = new WeakMap<LauncherStore, ReturnType<typeof createState>>();
/** 同一账号在多个标签组里出现时共享保存锁与草稿，避免旧标签覆盖刚保存的改动。 */
export function accountTagState(store: LauncherStore) {
  let state = states.get(store);
  if (!state) { state = createState(); states.set(store, state); }
  return state;
}
