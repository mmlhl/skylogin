<script setup lang="ts">
/**
 * 应用根组件：界面壳宿主（默认「控制台」，可切其他壳）+ 真实登录流程。
 *
 * 结构：
 *   - ShellHost（shellkit/）负责全部界面：四种壳共享同一套功能接口
 *   - realStore 把壳的动作翻译成 /api/* 调用；数据由 useAppState 每秒轮询
 *   - 下面这些弹层是真实登录/设置流程，由 realStore 的钩子打开：
 *     渠道选择 → 华为扫码 / 网易扫码 / 短信 / mpay 密码窗口
 *
 * 数据：composables/useAppState.ts 每秒从 Go 的 /api/state 拉一次，
 * Go 是唯一真相来源。
 */
import { onMounted, onUnmounted, ref } from "vue";
import { NConfigProvider, NMessageProvider, NDialogProvider, darkTheme, zhCN, dateZhCN } from "naive-ui";
import type { GlobalThemeOverrides } from "naive-ui";

import ShellHost from "./shellkit/ShellHost.vue";
import { createRealStore } from "./realStore";
import { api } from "./api/client";
import { startPolling } from "./composables/useAppState";
import type { Channel } from "./api/types";

import ChannelPicker from "./components/ChannelPicker.vue";
import ChannelWebLoginDialog from "./components/ChannelWebLoginDialog.vue";
import MpayLoginDialog from "./components/MpayLoginDialog.vue";
import SMSLoginDialog from "./components/SMSLoginDialog.vue";
import ScanDialog from "./components/ScanDialog.vue";
import HuaweiDialog from "./components/HuaweiDialog.vue";
import SettingsPanel from "./components/SettingsPanel.vue";

/** 把 Naive UI 默认外观压到设计系统里 */
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

/* ---------------- 登录流程弹层的状态 ---------------- */
const showChannelPicker = ref(false);
const pickerMode = ref<"channel" | "netease" | "bilibili" | "m4399" | "vivo" | "add">("add");
const showBiliLogin = ref(false);
const showM4399Login = ref(false);
const showVivoLogin = ref(false);
const showSMS = ref(false);
const showScan = ref(false);
const showHuawei = ref(false);
const showSettings = ref(false);
const showMpayLogin = ref(false);

/** 刷新凭证时的目标账号（空表示新增账号） */
const refreshTarget = ref<{ accountId: string; name: string } | null>(null);

/**
 * 打开某个来源的登录方式列表。
 * mode "add" = 添加账号（渠道 + 网易全在一个弹窗里选）；
 * acc 非空表示"给这个账号刷新凭证"，登录时会校验账号是否一致。
 */
function openPicker(mode: "channel" | "netease" | "bilibili" | "m4399" | "vivo" | "add", acc?: { accountId: string; name: string }) {
  refreshTarget.value = acc || null;
  pickerMode.value = mode;
  showChannelPicker.value = true;
}

/** 选完登录方式 → 进对应界面 */
async function onPick(entry: { kind: "qrcode" | "sms" | "mpay" | "biliweb" | "m4399web" | "vivoweb" | "group"; channel: Channel }) {
  showChannelPicker.value = false;
  if (entry.kind === "biliweb") {
    // 刷新已有账号时带上目标 uid：登录完成后校验是不是同一个号，防止登错账号覆盖
    void api.startChannelLogin("bili", refreshTarget.value?.accountId || "");
    showBiliLogin.value = true;
    return;
  }
  if (entry.kind === "m4399web") {
    void api.startChannelLogin("m4399", refreshTarget.value?.accountId || "");
    showM4399Login.value = true;
    return;
  }
  if (entry.kind === "vivoweb") {
    // Cookie 会话按小号分 profile 存：刷新指定小号时自动选它
    void api.startChannelLogin("vivo", refreshTarget.value?.accountId || "");
    showVivoLogin.value = true;
    return;
  }
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

/* ---------------- 壳的功能钩子 ---------------- */
const realStore = createRealStore({
  // 添加账号：B站/华为/网易都收进同一个选择弹窗（和设计稿一致）
  onAdd: () => openPicker("add"),
  onRefresh: (acc) => {
    const mode =
      acc.channel === "netease"
        ? "netease"
        : acc.channel === "bilibili"
          ? "bilibili"
          : acc.channel === "m4399"
            ? "m4399"
            : acc.channel === "vivo"
              ? "vivo"
              : "channel";
    openPicker(mode, { accountId: acc.accountId, name: acc.name });
  },
  onOpenSettings: () => {
    showSettings.value = true;
  },
});

/** 壳偏好：存后端配置（WebView2 端口随机，localStorage 记不住） */
async function loadShellPref(): Promise<number> {
  try {
    const r = await api.loadShell();
    return typeof r.shell === "number" ? r.shell : -1;
  } catch {
    return -1;
  }
}
function saveShellPref(i: number) {
  void api.saveShell(i);
}

let stopPolling: (() => void) | undefined;
onMounted(() => {
  stopPolling = startPolling(1000);
});
onUnmounted(() => {
  stopPolling?.();
});
</script>

<template>
  <n-config-provider :theme="darkTheme" :theme-overrides="themeOverrides" :locale="zhCN" :date-locale="dateZhCN">
    <n-message-provider>
      <n-dialog-provider>
        <ShellHost :store="realStore" :default-index="2" :show-switcher="false" :load-pref="loadShellPref" :save-pref="saveShellPref" />

        <ChannelPicker v-model:show="showChannelPicker" :mode="pickerMode" @pick="onPick" />
        <ChannelWebLoginDialog v-model:show="showBiliLogin" kind="bili" />
        <ChannelWebLoginDialog v-model:show="showM4399Login" kind="m4399" />
        <ChannelWebLoginDialog v-model:show="showVivoLogin" kind="vivo" />
        <MpayLoginDialog v-model:show="showMpayLogin" />
        <SMSLoginDialog v-model:show="showSMS" :target-user-id="refreshTarget?.accountId || ''" :default-mobile="refreshTarget?.name || ''" />
        <ScanDialog v-model:show="showScan" />
        <HuaweiDialog v-model:show="showHuawei" />
        <SettingsPanel v-model:show="showSettings" />
      </n-dialog-provider>
    </n-message-provider>
  </n-config-provider>
</template>
