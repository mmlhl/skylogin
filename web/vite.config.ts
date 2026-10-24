import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";

// 构建产物直接输出到 Go 的 web 包里，由 go:embed 打进 exe。
// 改完界面跑 `npm run build`，再 `go build` 就是最新界面。
export default defineConfig({
  plugins: [vue()],
  // 页面由 Go 在根路径提供，用相对路径引用资源，避免端口/路径写死
  base: "./",
  build: {
    outDir: "../internal/web/dist",
    emptyOutDir: true,
    // 关掉 sourcemap：exe 里不需要，体积也小
    sourcemap: false,
  },
  server: {
    // 固定端口 + 只监听 IPv4：截图脚本和 WebView 都用 127.0.0.1
    host: "127.0.0.1",
    port: 5173,
    strictPort: true,
    // 开发时把 /api 转发给 Go 起的本地服务（端口见 %APPDATA%\SkyLogin\log.txt）
    // 只想看界面的话不用管它：打开 /?mock=1 会用假数据。
    proxy: {
      "/api": {
        target: "http://127.0.0.1:8080",
        changeOrigin: true,
      },
    },
  },
});


