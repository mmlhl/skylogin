<script setup lang="ts">
/**
 * 设计演示：同一套功能（demo/store.ts 的 mock 实现），四套可切换的界面壳。
 * 正式界面的壳宿主见 shellkit/ShellHost.vue（由 App.vue 挂载）。
 * 访问方式：npm run dev 后打开 http://127.0.0.1:5173/demo.html
 */
import ShellHost from "../shellkit/ShellHost.vue";
import { store } from "./store";
</script>

<template>
  <ShellHost :store="store" :default-index="0" persist-key="sky-demo-shell" />
</template>
