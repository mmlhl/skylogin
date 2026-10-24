/**
 * 预览页入口（只在浏览器里看设计用）。
 *
 * 打开：http://127.0.0.1:5173/preview.html#a
 * 这个页面不会被打进 exe —— `npm run build` 只构建 index.html。
 */
import { createApp } from "vue";
import PreviewApp from "./PreviewApp.vue";

createApp(PreviewApp).mount("#preview");
