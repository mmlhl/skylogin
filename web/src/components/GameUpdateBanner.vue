<script setup lang="ts">
/**
 * 游戏版本提示（独立组件，放在 <n-dialog-provider> 内部，同 LaunchAlertDialog）。
 *
 * 背景：我们绕过发烧平台直接启动游戏，平台不会替用户打更新。
 * 启动器打开时 Go 侧会比对本地包版本与官方最新包版本（/api/state 的 gameUpdate），
 * 落后就在这里弹一次提示：版本不一致进游戏会出各种 bug，
 * 需要去发烧平台把游戏更新到最新版。
 */
import { watch } from "vue";
import { h } from "vue";
import { useDialog } from "naive-ui";
import { state } from "../composables/useAppState";
import { api } from "../api/client";

const dialog = useDialog();
/** 只提示一次：用户明确点了「下次再说」之前不再重复弹 */
let prompted = false;

watch(
  () => state.gameUpdate,
  (g) => {
    if (!g || prompted) return;
    if (!g.checked || !g.update_needed) return;
    prompted = true;
    dialog.warning({
      title: "游戏有新版本",
      content: () =>
        h("div", [
          h("p", `当前版本：${g.local_version || "未知"}　最新版本：${g.latest_version}`),
          h("p", "版本不一致可能导致进不了大厅或出现各种异常。请打开发烧平台，把《光·遇》更新到最新版后再启动。"),
        ]),
      positiveText: "打开发烧平台",
      negativeText: "下次再说",
      onPositiveClick: () => {
        void api.gameUpdateOpenPlatform();
      },
    });
  }
);
</script>

<template></template>
