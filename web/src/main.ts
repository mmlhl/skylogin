/**
 * 应用入口：挂载 Vue + 注册 Naive UI 的暗色主题。
 *
 * 开发预览：URL 带 `?mock=1` 时启用假数据（api/mock.ts），
 * 不需要 Go 后端、不需要编译 exe、不会弹 UAC。
 * 这是纯前端的本地开发模式，发布产物里不带任何假数据，正常使用永远不会进到它。
 */
import { createApp } from "vue";
import App from "./App.vue";
import "./styles/global.css";
import "./shellkit/shellkit.css";

/** 是否处于"只看界面"的预览模式 */
const mock = new URLSearchParams(location.search).has("mock");

async function boot() {
  if (mock) {
    const { installMock } = await import("./api/mock");
    installMock();
  }
  createApp(App).mount("#app");
}

boot();
