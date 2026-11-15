import {
  computed,
  onBeforeUnmount,
  ref,
  shallowRef,
  watch,
  type ComputedRef,
} from "vue";
import type {
  LauncherStore,
  LayoutPlan,
  LayoutRequest,
} from "../shellkit/contract";

/** 请求只随数量和用户选择变化，轮询账号状态不会反复改写当前模式。 */
export function useLayoutPlan(
  store: LauncherStore,
  request: ComputedRef<LayoutRequest>,
) {
  const result = shallowRef<LayoutPlan | null>(null);
  const loading = ref(false);
  const error = ref("");
  const key = computed(() => JSON.stringify(request.value));
  let requestedKey = "";
  let sequence = 0;
  let task: Promise<void> = Promise.resolve();
  let disposed = false;
  function refresh() {
    const sentKey = key.value;
    if (requestedKey === sentKey && (loading.value || result.value)) return;
    requestedKey = sentKey;
    const token = ++sequence;
    loading.value = true;
    error.value = "";
    const body = { ...request.value };
    task = store
      .planLayout(body)
      .then((value) => {
        // 快切数量或方案后，旧请求只能结束自己，不能覆盖新画布。
        if (disposed || token !== sequence || sentKey !== key.value) return;
        result.value = value;
      })
      .catch((reason: unknown) => {
        if (disposed || token !== sequence) return;
        result.value = null;
        error.value =
          reason instanceof Error ? reason.message : "窗口规划失败，请重试";
      })
      .finally(() => {
        if (!disposed && token === sequence) loading.value = false;
      });
  }
  watch(key, refresh, { immediate: true, flush: "sync" });
  async function ensure(): Promise<LayoutPlan> {
    for (;;) {
      if (requestedKey !== key.value || (!loading.value && !result.value))
        refresh();
      const token = sequence;
      await task;
      if (token !== sequence || requestedKey !== key.value) continue;
      if (error.value || !result.value)
        throw new Error(error.value || "暂时没有可用的窗口规划");
      return result.value;
    }
  }
  function retry() {
    requestedKey = "";
    refresh();
  }
  onBeforeUnmount(() => {
    disposed = true;
    sequence++;
  });
  return { result, loading, error, ensure, retry };
}
