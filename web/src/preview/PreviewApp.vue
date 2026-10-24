<script setup lang="ts">
/**
 * 预览容器：按地址 hash 切换风格与配色。
 *
 *   #a #b #d          三种布局
 *   #c1 ~ #c5         「光晕」布局的五套配色
 *
 * 打开方式（开发服务器）：
 *   http://127.0.0.1:5173/preview.html#c1
 *
 * 这些页面只在浏览器里看设计，不会打包进 exe。
 */
import { computed, ref } from "vue";
import VariantA from "./variants/VariantA.vue";
import VariantB from "./variants/VariantB.vue";
import VariantC from "./variants/VariantC.vue";
import VariantD from "./variants/VariantD.vue";
import { findPalette, palettes } from "./palettes";

const hash = ref(location.hash.replace("#", "").toLowerCase() || "c1");
window.addEventListener("hashchange", () => {
  hash.value = location.hash.replace("#", "").toLowerCase() || "c1";
});

/** 当前要渲染的组件与参数 */
const view = computed(() => {
  const h = hash.value;
  if (h.startsWith("c")) return { comp: VariantC, palette: findPalette(h) };
  if (h === "b") return { comp: VariantB, palette: undefined };
  if (h === "d") return { comp: VariantD, palette: undefined };
  return { comp: VariantA, palette: undefined };
});

/** 顶部两组标签：布局 / 配色 */
const layouts = [
  { key: "a", label: "A 工具台" },
  { key: "b", label: "B 极简" },
  { key: "c1", label: "C 光晕" },
  { key: "d", label: "D 暖石" },
];
</script>

<template>
  <div class="preview">
    <nav class="tabs">
      <a v-for="l in layouts" :key="l.key" :href="`#${l.key}`" :class="{ on: hash.startsWith(l.key[0]) && (l.key[0] !== 'c' || hash.startsWith('c')) }">
        {{ l.label }}
      </a>

      <span class="divider" />

      <a
        v-for="p in palettes"
        :key="p.key"
        :href="`#${p.key}`"
        class="swatch"
        :class="{ on: hash === p.key }"
        :title="p.desc"
      >
        <i :style="{ background: p.accent }" />
        {{ p.name }}
      </a>

      <span class="note">预览模式 · 不会打包进 exe</span>
    </nav>

    <div class="stage">
      <component :is="view.comp" :palette="view.palette" />
    </div>
  </div>
</template>

<style scoped>
.preview {
  height: 100vh;
  display: flex;
  flex-direction: column;
  background: #060607;
}

.tabs {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 8px 14px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.08);
  font-size: 12px;
  font-family: "Segoe UI", "Microsoft YaHei UI", sans-serif;
}

.tabs a {
  padding: 5px 10px;
  border-radius: 6px;
  color: #8b8b93;
  text-decoration: none;
}
.tabs a:hover {
  background: rgba(255, 255, 255, 0.06);
  color: #e8e8ea;
}
.tabs a.on {
  background: rgba(255, 255, 255, 0.11);
  color: #fff;
}

.divider {
  width: 1px;
  height: 16px;
  margin: 0 8px;
  background: rgba(255, 255, 255, 0.12);
}

/* 给变体一个确定高度：组件内部用的是 height:100%，
   父级若只有 flex:1 会被内容撑开，百分比就失效 */
.stage {
  height: calc(100vh - 41px);
}

.swatch {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.swatch i {
  width: 9px;
  height: 9px;
  border-radius: 50%;
  display: inline-block;
}

.note {
  margin-left: auto;
  color: #5c5c64;
}
</style>



