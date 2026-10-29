<script setup lang="ts">
/**
 * 启动事件弹窗（客户端闪退等）。
 *
 * 必须是独立组件、放在 <n-dialog-provider> 内部：useDialog 只能拿到
 * 祖先 provider（App.vue 是 provider 的父级，在那儿调会直接抛错
 * 把整个应用挂成白屏）。watch /api/state 的 launchAlert，按 id 去重
 * 只弹一次。
 */
import { ref, watch } from "vue";
import { useDialog } from "naive-ui";
import { state } from "../composables/useAppState";

const dialog = useDialog();
const lastAlertId = ref(0);

watch(
  () => state.launchAlert,
  (a) => {
    if (!a || a.id <= lastAlertId.value) return;
    lastAlertId.value = a.id;
    dialog.warning({ title: a.title, content: a.message, positiveText: "知道了" });
  }
);
</script>

<template></template>
