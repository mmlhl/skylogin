<script setup lang="ts">
/**
 * 更新与公告（启动时自动检查一次，非阻塞）。
 *
 * 必须是独立组件、放在 <n-dialog-provider> 内部（同 LaunchAlertDialog 的原因）。
 * 逻辑全部来自独立模块 internal/release：fork 移除该模块 + 本组件即可。
 *
 * - 构建号为 0（本地开发版）时不自动检查，不打扰开发；
 * - 有更新 → 弹窗展示版本号 + 更新日志 + 下载渠道（GitHub / 123 网盘）；
 *   force = 必须更新时弹窗不可点遮罩关闭；
 * - 公告按 localStorage 记住的序号增量拉取，展示后更新序号。
 */
import { onMounted, ref } from "vue";
import { useDialog } from "naive-ui";
import { h } from "vue";
import { api } from "../api/client";

const dialog = useDialog();

interface Latest {
  version: string;
  build: number;
  download_url: string;
  changelog: string;
  force: boolean;
}

const ANN_KEY = "annSeenVersion";

function openChannel(url: string) {
  void api.openExternal(url);
}

function changelogLines(list: string[]) {
  return h("ul", { style: "margin:4px 0 0;padding-left:18px" }, list.map((t) => h("li", t)));
}

/** 有更新时的弹窗（force 时不给取消按钮、点遮罩不关）。 */
function showUpdate(latest: Latest) {
  const buttons = [
    h(
      "button",
      {
        onClick: () => openChannel(latest.download_url || ""),
        style: "margin-right:8px;padding:6px 14px;border-radius:8px;border:1px solid rgba(255,255,255,.2);background:transparent;color:inherit;cursor:pointer",
      },
      "GitHub 下载",
    ),
    h(
      "button",
      {
        onClick: () => openChannel("https://1822397226.share.123pan.cn/123pan/87OJjv-vhMl?pwd=l5sX"),
        style: "padding:6px 14px;border-radius:8px;border:0;background:#d8b47c;color:#161619;cursor:pointer",
      },
      "123 网盘",
    ),
  ];
  dialog[latest.force ? "error" : "info"]({
    title: `发现新版本 v${latest.version}`,
    content: () =>
      h("div", [
        h("p", { style: "margin:6px 0 2px" }, latest.changelog || "暂无更新说明"),
        h("div", { style: "margin-top:10px" }, buttons),
      ]),
    positiveText: latest.force ? "知道了，去更新" : undefined,
    closable: !latest.force,
    maskClosable: !latest.force,
  });
}

/** 公告弹窗：多条合并展示（标题 + 作者的话 + 修复/更新列表）。 */
function showAnnouncements(list: {
  title: string;
  content: { author_words?: string; fixes?: string[]; updates?: string[] };
}[]) {
  if (!list.length) return;
  const blocks = list.map((a) => {
    const parts = [a.content.author_words, ...[a.content.fixes, a.content.updates].flat().filter(Boolean)];
    return h("div", { style: "margin-bottom:10px" }, [
      h("b", a.title),
      h(
        "div",
        { style: "white-space:pre-wrap" },
        parts.filter(Boolean).join("\n"),
      ),
    ]);
  });
  dialog.info({
    title: "公告",
    content: () => h("div", blocks),
    positiveText: "知道了",
  });
}

/** 启动自动检查（仅构建版；本地开发版 build=0 跳过，不打扰开发）。 */
async function autoCheck() {
  try {
    const r = await api.releaseCheck();
    if (r.ok === false) return;
    const build = Number((r as { build?: number }).build ?? 0);
    const latest = (r as { latest?: Latest }).latest;
    if (build > 0 && r.hasUpdate && latest) showUpdate(latest);
    // 公告：增量拉取（本地未看过任何公告从 0 开始）
    const seen = Number(localStorage.getItem(ANN_KEY) || "0");
    const ar = (await api.releaseAnnouncements(seen)) as {
      ok: boolean;
      list?: { id: number; title: string; content: { author_words?: string; fixes?: string[]; updates?: string[] } }[];
    };
    if (ar.ok !== false) {
      const list = ar.list ?? [];
      if (list.length) {
        showAnnouncements(list);
        localStorage.setItem(ANN_KEY, String(Math.max(...list.map((a) => a.id))));
      }
    }
  } catch {
    /* 离线 / 云端不可达：静默，关于页可手动检查 */
  }
}

/** 关于页手动检查的结果（SettingsPanel 自己调 api，这里只处理启动自动化） */
const started = ref(false);
onMounted(() => {
  if (started.value) return;
  started.value = true;
  void autoCheck();
});
</script>

<template></template>
