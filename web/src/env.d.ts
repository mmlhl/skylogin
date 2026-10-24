/// <reference types="vite/client" />

// 让 TypeScript 认识 .vue 文件
declare module "*.vue" {
  import type { DefineComponent } from "vue";
  const component: DefineComponent<Record<string, never>, Record<string, never>, unknown>;
  export default component;
}
