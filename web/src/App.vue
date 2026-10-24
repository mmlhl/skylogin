<script setup lang="ts">
/**
 * 应用根组件。
 *
 * 视觉：.ai/06-界面设计规范.md + 「玄夜」配色
 *   - 两层氛围光（跟随主题色）
 *   - 左栏、右侧卡片都是玻璃质感
 *   - 主界面只留一个动作，设置收在弹层里
 *
 * 数据：composables/useAppState.ts 每秒从 Go 的 /api/state 拉一次，
 * 界面不自己攒状态，Go 是唯一真相来源。
 */
import { onMounted, onUnmounted, ref, watch } from "vue";
import { NConfigProvider, NMessageProvider, NDialogProvider, darkTheme, zhCN, dateZhCN } from "naive-ui";
import type { GlobalThemeOverrides } from "naive-ui";

import AccountRail from "./components/AccountRail.vue";
import LaunchPanel from "./components/LaunchPanel.vue";
import SettingsPanel from "./components/SettingsPanel.vue";
import MultiLaunchDialog from "./components/MultiLaunchDialog.vue";
import MpayLoginDialog from "./components/MpayLoginDialog.vue";
import SMSLoginDialog from "./components/SMSLoginDialog.vue";
import ChannelPicker from "./components/ChannelPicker.vue";
import ScanDialog from "./components/ScanDialog.vue";
import HuaweiDialog from "./components/HuaweiDialog.vue";
import type { AccountView } from "./api/types";
import PromptDialog from "./components/PromptDialog.vue";
import { startPolling, state } from "./composables/useAppState";
import { api } from "./api/client";
import type { Channel } from "./api/types";

/** 把 Naive UI 默认外观压到规范里 */
const themeOverrides: GlobalThemeOverrides = {
  common: {
    primaryColor: "#d8b47c",
    primaryColorHover: "#e6c88f",
    primaryColorPressed: "#c8a468",
    primaryColorSuppl: "#d8b47c",
    bodyColor: "transparent",
    cardColor: "#161619",
    modalColor: "#161619",
    popoverColor: "#1b1b1f",
    borderColor: "rgba(255,255,255,0.10)",
    dividerColor: "rgba(255,255,255,0.08)",
    textColorBase: "#f4f2ef",
    textColor2: "#c9c6c0",
    textColor3: "#a5a29c",
    borderRadius: "12px",
    borderRadiusSmall: "10px",
    fontSize: "14px",
  },
  Button: { fontWeightStrong: "600" },
  Modal: { borderRadius: "16px", titleFontSize: "16px", titleFontWeight: "600" },
  Switch: { railColorActive: "#d8b47c" },
};

/* ---------------- 左栏宽度：可拖动，存到后端配置 ---------------- */
const RAIL_DEFAULT = 288;
const RAIL_MIN = 220;
const RAIL_MAX = 520;
const railWidth = ref(RAIL_DEFAULT);
const railResizing = ref(false);
let railDragged = false; // 用户拖过之后就别再被轮询回来的旧值覆盖
let dragStartX = 0;
let dragStartW = 0;

function clampRail(w: number) {
  return Math.min(RAIL_MAX, Math.max(RAIL_MIN, Math.round(w)));
}

// 后端存过值就套用一次（首次加载时 state 是空对象，等它回来）
watch(
  () => state.settings.railWidth,
  (w) => {
    if (!railDragged && w && w > 0) railWidth.value = clampRail(w);
  },
  { immediate: true }
);

function onRailDragStart(e: MouseEvent) {
  e.preventDefault();
  railResizing.value = true;
  railDragged = true;
  dragStartX = e.clientX;
  dragStartW = railWidth.value;
  window.addEventListener("mousemove", onRailDragMove);
  window.addEventListener("mouseup", onRailDragEnd);
}

function onRailDragMove(e: MouseEvent) {
  railWidth.value = clampRail(dragStartW + (e.clientX - dragStartX));
}

function onRailDragEnd() {
  railResizing.value = false;
  window.removeEventListener("mousemove", onRailDragMove);
  window.removeEventListener("mouseup", onRailDragEnd);
  void api.setRailWidth(railWidth.value).catch(() => {});
}

// 双击分隔条恢复默认
function resetRailWidth() {
  railDragged = true;
  railWidth.value = RAIL_DEFAULT;
  void api.setRailWidth(RAIL_DEFAULT).catch(() => {});
}

const showChannelPicker = ref(false);
const pickerMode = ref<"channel" | "netease">("channel");
const showSMS = ref(false);
const showScan = ref(false);
const showHuawei = ref(false);
const showSettings = ref(false);
const showMulti = ref(false);
const showMpayLogin = ref(false);

let stopPolling: (() => void) | undefined;

onMounted(() => {
  stopPolling = startPolling(1000);
});
onUnmounted(() => {
  stopPolling?.();
  window.removeEventListener("mousemove", onRailDragMove);
  window.removeEventListener("mouseup", onRailDragEnd);
});

/** 刷新凭证时的目标账号（空表示新增账号） */
const refreshTarget = ref<{ accountId: string; name: string } | null>(null);

/**
 * 打开某个来源的登录方式列表。
 * acc 非空表示"给这个账号刷新凭证"，登录时会校验账号是否一致。
 */
function openPicker(mode: "channel" | "netease", acc?: { accountId: string; name: string }) {
  refreshTarget.value = acc || null;
  pickerMode.value = mode;
  showChannelPicker.value = true;
}

/** 侧栏 ⋯ 菜单里的「刷新凭证」：按账号自己的渠道选登录方式 */
function onRefresh(acc: AccountView) {
  openPicker(acc.channel === "netease" ? "netease" : "channel", acc);
}

/**
 * 启动面板过期横幅上的「刷新凭证」。
 *
 * 之前这里直接进扫码界面，等于替用户选好了方式；
 * 现在打开当前账号所属渠道的方式列表，让用户自己挑。
 */
function onRefreshCurrent() {
  const cur = state.accounts.find((a) => a.current);
  if (!cur) return;
  openPicker(cur.channel === "netease" ? "netease" : "channel", { accountId: cur.accountId, name: cur.name });
}

/** 选完登录方式 → 进对应界面 */
async function onPick(entry: { kind: "qrcode" | "sms" | "mpay"; channel: Channel }) {
  showChannelPicker.value = false;
  if (entry.kind === "sms") {
    showSMS.value = true;
    return;
  }
  if (entry.kind === "mpay") {
    // 官方 mpay.dll 弹窗登录：打不开也把进度窗口打开，里面会显示原因
    await api.mpayLoginStart(refreshTarget.value?.accountId || "");
    showMpayLogin.value = true;
    return;
  }
  if (entry.channel === "netease") showScan.value = true;
  else showHuawei.value = true;
}

const promptRef = ref<InstanceType<typeof PromptDialog> | null>(null);

async function renameCurrent() {
  const acc = state.accounts.find((a) => a.current);
  if (!acc) return;
  const name = await promptRef.value?.open("修改别名", acc.name, "最多 24 个字");
  if (name === null || name === undefined) return;
  await api.renameAccount(acc.id, name);
}
</script>

<template>
  <n-config-provider :theme="darkTheme" :theme-overrides="themeOverrides" :locale="zhCN" :date-locale="dateZhCN">
    <n-message-provider>
      <n-dialog-provider>
        <div class="app" :class="{ resizing: railResizing }" :style="{ '--rail-w': railWidth + 'px' }">
          <!-- 氛围光：两层，只为主题色着色 -->
          <div class="glow glow-a" />
          <div class="glow glow-b" />

          <AccountRail
            @add-channel="openPicker('channel')"
            @add-netease="openPicker('netease')"
            @rename="renameCurrent"
            @refresh="onRefresh"
          />

          <!-- 分隔条：拖动调左栏宽度，双击复位 -->
          <div
            class="split"
            title="拖动调整左栏宽度，双击恢复默认"
            @mousedown="onRailDragStart"
            @dblclick="resetRailWidth"
          />

          <main class="pane">
            <LaunchPanel
              @rename="renameCurrent"
              @refresh="onRefreshCurrent"
              @settings="showSettings = true"
              @multi="showMulti = true"
            />
          </main>
        </div>

        <ChannelPicker v-model:show="showChannelPicker" :mode="pickerMode" @pick="onPick" />
        <MpayLoginDialog v-model:show="showMpayLogin" />
        <SMSLoginDialog v-model:show="showSMS" :target-user-id="refreshTarget?.accountId || ''" :default-mobile="refreshTarget?.name || ''" />
        <ScanDialog v-model:show="showScan" />
        <HuaweiDialog v-model:show="showHuawei" />
        <SettingsPanel v-model:show="showSettings" />
        <MultiLaunchDialog v-model:show="showMulti" />
        <PromptDialog ref="promptRef" />
      </n-dialog-provider>
    </n-message-provider>
  </n-config-provider>
</template>

<style scoped>
/* 用 100vh：外面套了 provider，百分比高度会退化成内容高度 */
.app {
  position: relative;
  display: grid;
  grid-template-columns: var(--rail-w, 288px) 6px 1fr;
  height: 100vh;
  background: var(--bg);
  overflow: hidden;
  /* 让 app 自成一个层叠上下文，弹层（z-index 2000）始终在它上面；
     不加的话带 backdrop-filter 的元素在某些渲染路径下会盖住弹层 */
  isolation: isolate;
  z-index: 0;
}

/* 分隔条：平时透明，悬停/拖动时露出金色细线 */
.split {
  position: relative;
  cursor: col-resize;
  z-index: 2;
}
.split::after {
  content: "";
  position: absolute;
  left: 2px;
  top: 0;
  bottom: 0;
  width: 2px;
  background: transparent;
  transition: background 0.16s ease-out;
}
.split:hover::after,
.resizing .split::after {
  background: color-mix(in srgb, var(--accent) 55%, transparent);
}
.app.resizing {
  cursor: col-resize;
  user-select: none;
}

/* 两层氛围光，颜色跟随主题色 */
.glow {
  position: absolute;
  pointer-events: none;
}
.glow-a {
  inset: -30% 30% 30% -20%;
  background: radial-gradient(closest-side, color-mix(in srgb, var(--accent) 20%, transparent), transparent 70%);
}
.glow-b {
  inset: 45% -10% -20% 45%;
  background: radial-gradient(closest-side, color-mix(in srgb, var(--accent) 9%, transparent), transparent 70%);
  opacity: 0.7;
}

/* 分隔条：平时透明，悬停/拖动时露出金色细线 */
.split {
  position: relative;
  cursor: col-resize;
  z-index: 2;
}
.split::after {
  content: "";
  position: absolute;
  left: 2px;
  top: 0;
  bottom: 0;
  width: 2px;
  background: transparent;
  transition: background 0.16s ease-out;
}
.split:hover::after,
.resizing .split::after {
  background: color-mix(in srgb, var(--accent) 55%, transparent);
}
.app.resizing {
  cursor: col-resize;
  user-select: none;
}

.pane {
  position: relative;
  display: grid;
  place-items: center;
  padding: 40px;
  overflow-y: auto;
}
</style>








