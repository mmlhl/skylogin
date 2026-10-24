/**
 * 预览用的假数据。
 *
 * 只服务于 web/preview.html（浏览器里看风格用），
 * 不会打进 exe：`npm run build` 只构建 index.html。
 */
export interface PreviewAccount {
  name: string;
  channel: "huawei" | "netease";
  id: string;
  level?: string;
  current?: boolean;
}

export const accounts: PreviewAccount[] = [
  { name: "示例玩家", channel: "huawei", id: "1100000000000001", level: "7", current: true },
  { name: "138****8000", channel: "netease", id: "aede********0000" },
];

export const game = {
  running: false,
  status: "已登录，可直接启动",
};
