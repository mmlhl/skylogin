/**
 * 界面壳的共享元数据与当前选择。
 *
 * ShellHost（渲染当前壳）和 SettingsPanel（设置里切换界面方案）
 * 都从这里取壳列表和当前值——两边天然同步，不会出现"切换器在 A、设置显示 B"。
 */
import { ref } from "vue";

export interface ShellMeta {
  name: string;
  desc: string;
  /** 主题色（壳根节点与共享弹层的强调色） */
  acc: string;
  /** 主题色上的文字色 */
  on: string;
}

export const SHELLS: ShellMeta[] = [
  { name: "账号库", desc: "卡片墙，多账号一目了然", acc: "#d9b57c", on: "#191206" },
  { name: "此刻", desc: "单账号舞台，一键启动", acc: "#e3c28a", on: "#1a1408" },
  { name: "控制台", desc: "列表 + 详情，信息最全", acc: "#6d9aef", on: "#0b1526" },
  { name: "晨白", desc: "浅色横幅，清爽轻盈", acc: "#a3762b", on: "#ffffff" },
];

/** 当前激活的壳（下标对应 SHELLS） */
export const activeShell = ref(2);
