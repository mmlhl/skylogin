/**
 * 配色方案：同一套布局换主题用。
 *
 * 遵循 .ai/06-界面设计规范.md 的「颜色分角色」：
 *   每套配色给出 3 级表面 + 3 级文字 + 1 个 accent，
 *   组件只从这些角色里取值，不自己写死颜色。
 */
export interface Palette {
  key: string;
  name: string;
  desc: string;
  /** 表面三级：底 / 面板 / 浮起 */
  bg: string;
  panel: string;
  raised: string;
  /** 文字三级 */
  text: string;
  dim: string;
  faint: string;
  /** 强调色 + 它上面的文字色 */
  accent: string;
  onAccent: string;
  /** 分隔线 */
  hairline: string;
  hairlineStrong: string;
  /** 背景附加质感：top 上缘光 / grid 细网格 / flat 纯色 */
  bgStyle: "top" | "grid" | "flat";
}

export const palettes: Palette[] = [
  {
    key: "c1",
    name: "玄夜",
    desc: "近黑三级表面 + 沙金",
    bg: "#09090a",
    panel: "#101012",
    raised: "#161619",
    text: "#f4f2ef",
    dim: "#a5a29c",
    faint: "#6d6a65",
    accent: "#d8b47c",
    onAccent: "#14120c",
    hairline: "rgba(255,255,255,0.08)",
    hairlineStrong: "rgba(255,255,255,0.14)",
    bgStyle: "top",
  },
  {
    key: "c2",
    name: "青苔",
    desc: "墨绿表面 + 苔绿",
    bg: "#0a0b0a",
    panel: "#101210",
    raised: "#161916",
    text: "#f0f2ee",
    dim: "#9ea79c",
    faint: "#69726a",
    accent: "#a8c496",
    onAccent: "#0e1409",
    hairline: "rgba(255,255,255,0.08)",
    hairlineStrong: "rgba(255,255,255,0.14)",
    bgStyle: "top",
  },
  {
    key: "c3",
    name: "暮蓝",
    desc: "冷灰蓝表面 + 银蓝",
    bg: "#08090b",
    panel: "#0e1014",
    raised: "#141821",
    text: "#f0f2f6",
    dim: "#9ba3b0",
    faint: "#68707d",
    accent: "#9fb6d4",
    onAccent: "#0b0f16",
    hairline: "rgba(255,255,255,0.08)",
    hairlineStrong: "rgba(255,255,255,0.14)",
    bgStyle: "top",
  },
  {
    key: "c4",
    name: "暖石",
    desc: "暖棕表面 + 米金",
    bg: "#0e0c0a",
    panel: "#15120f",
    raised: "#1c1814",
    text: "#f4efe6",
    dim: "#a99e8f",
    faint: "#71685c",
    accent: "#e0bb86",
    onAccent: "#171106",
    hairline: "rgba(255,255,255,0.08)",
    hairlineStrong: "rgba(255,255,255,0.14)",
    bgStyle: "flat",
  },
  {
    key: "c5",
    name: "薄雾",
    desc: "中性灰表面 + 细网格",
    bg: "#0a0b0c",
    panel: "#101113",
    raised: "#17191c",
    text: "#f2f3f4",
    dim: "#a0a3a7",
    faint: "#6d7176",
    accent: "#cdd2d8",
    onAccent: "#0b0c0d",
    hairline: "rgba(255,255,255,0.08)",
    hairlineStrong: "rgba(255,255,255,0.14)",
    bgStyle: "grid",
  },
];

export function findPalette(key: string): Palette {
  return palettes.find((p) => p.key === key) || palettes[0];
}
