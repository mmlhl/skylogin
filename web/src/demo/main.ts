/**
 * 设计演示入口：功能与 UI 解耦验证页。
 * 打开方式：npm run dev → http://127.0.0.1:5173/demo.html
 * 不走 Go 后端、不参与正式构建（vite build 只打包 index.html）。
 */
import { createApp } from "vue";
import DesignDemo from "./DesignDemo.vue";
import "../shellkit/shellkit.css";

createApp(DesignDemo).mount("#demo");
