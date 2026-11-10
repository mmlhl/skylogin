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
import { computed, onMounted, onUnmounted, ref } from "vue";
import { NConfigProvider, NMessageProvider, NDialogProvider, darkTheme, zhCN, dateZhCN } from "naive-ui";
import type { GlobalThemeOverrides } from "naive-ui";

import ShellHost from "./shellkit/ShellHost.vue";
import { createRealStore } from "./realStore";
import { api } from "./api/client";
import { startPolling, state } from "./composables/useAppState";
import LaunchAlertDialog from "./components/LaunchAlertDialog.vue";
import ReleaseNotice from "./components/ReleaseNotice.vue";
import type { LoginEntryView } from "./api/types";

import ChannelPicker from "./components/ChannelPicker.vue";
import ChannelWebLoginDialog from "./components/ChannelWebLoginDialog.vue";
import ChannelAskDialog from "./components/ChannelAskDialog.vue";
import MpayLoginDialog from "./components/MpayLoginDialog.vue";
import HeadlessLoginDialog from "./components/HeadlessLoginDialog.vue";
import SMSLoginDialog from "./components/SMSLoginDialog.vue";
import ScanDialog from "./components/ScanDialog.vue";
import HuaweiDialog from "./components/HuaweiDialog.vue";
import SettingsPanel from "./components/SettingsPanel.vue";
import TitleBar from "./components/TitleBar.vue";

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
/* 添加账号的入口清单也由后端给：界面不认识渠道，只按 action + kind 路由 */
const loginEntries = computed<LoginEntryView[]>(() => state.loginChannels?.entries ?? []);
const pickerMode = ref<string>("add");
const showBiliLogin = ref(false);
const showM4399Login = ref(false);
const showVivoLogin = ref(false);
const showSMS = ref(false);
const showScan = ref(false);
const showHuawei = ref(false);
const showSettings = ref(false);
const showMpayLogin = ref(false);
const showHeadless = ref(false);
/** 渠道协议登录（九游密码 / 验证码）：kind 是渠道 key，空表示关着 */
const askKind = ref("");
/** 具体是哪种方式：password / sms */
const askMethod = ref("password");

/** 刷新凭证时的目标账号（空表示新增账号） */
const refreshTarget = ref<{ accountId: string; name: string; loginUser?: string } | null>(null);

/**
 * 打开某个来源的登录方式列表。
 * mode "add" = 添加账号（渠道 + 网易全在一个弹窗里选）；
 * acc 非空表示"给这个账号刷新凭证"，登录时会校验账号是否一致。
 */
function openPicker(mode: string, acc?: { accountId: string; name: string; loginUser?: string }) {
  refreshTarget.value = acc || null;
  pickerMode.value = mode;
  showChannelPicker.value = true;
}

/**
 * 选完登录方式 → 进对应界面。
 *
 * 只认 action + kind，不认识具体渠道名：新增渠道时后端多一项，
 * 这里最多加一个 case（甚至不用改，如果方式已经覆盖）。
 */
async function onPick(entry: { action: string; kind: string; method: string }) {
  showChannelPicker.value = false;
  const target = refreshTarget.value?.accountId || "";

  switch (entry.kind) {
    case "web": {
      // 渠道网页登录：action 就是渠道标识（bilibili / m4399 / vivo）
      // 刷新已有账号时带上目标 uid：登录完成后校验是不是同一个号
      void api.startChannelLogin(entry.action, target);
      if (entry.action === "bilibili") showBiliLogin.value = true;
      else if (entry.action === "m4399") showM4399Login.value = true;
      else showVivoLogin.value = true;
      return;
    }
    case "scan":
      // 网易账号扫码：纯 Go 建码，不弹官方登录窗。
      // 只有自己建码才拿得到 login_info.code，才能带平台会话去换票，
      // 服务端才会把这个号写进记住槽（凭证过期后免扫出票）。
      // 打开弹窗放到请求之后：建码失败时由弹窗里的错误态显示原因。
      await api.startScan();
      showScan.value = true;
      return;
    case "channelscan":
      // 渠道服扫码：以游戏身份（ma75）开官方登录窗，默认用平台目录那份 mpay.dll。
      // 它要的是 DLL 回调里的渠道整包（sdk_info / SAUTH），纯 Go 扫码给不出那种形态，
      // 所以这条路保持官方窗不变。
      await api.mpayChannelStart();
      showMpayLogin.value = true;
      return;
    case "password": {
      // 九游：不走官方 DLL，直接用 Go 协议登录（手机号 + 密码），
      // 前端只负责把后端要的表单渲染出来
      if (entry.action === "uc_platform") {
        askKind.value = "uc_platform";
        askMethod.value = "password";
        return;
      }
      // 网易账号：改用本工具的无头登录输入框，不再弹官方登录窗。
      // 原因：官方窗的密码我们看不到、存不下来，而且它只给一份凭证，
      // 那个号当不了寄宿宿主，扫码号就没有槽位可用。
      showHeadless.value = true;
      return;
    }
    case "sms":
      // 九游的验证码登录同样走协议直连，复用同一个问答弹窗；
      // 其余渠道的短信登录还是官方那套独立窗口。
      if (entry.action === "uc_platform") {
        askKind.value = "uc_platform";
        askMethod.value = "sms";
        return;
      }
      showSMS.value = true;
      return;
    case "qr":
      // 应用宝（微信 / QQ）：纯 HTTP 扫码，不开窗、不需要浏览器。
      // 二维码由后端生成后经 /status 送过来，复用协议登录那个对话框。
      if (entry.action === "myapp") {
        askKind.value = "myapp";
        askMethod.value = entry.method || "微信扫码";
        return;
      }
      // 其余二维码类：华为（在 WebView 里扫码）
      showHuawei.value = true;
      return;
    default:
      // 未知方式不猜——要么后端加了新 kind，要么数据有问题，留给日志暴露
      return;
  }
}

/* ---------------- 壳的功能钩子 ---------------- */
const realStore = createRealStore({
  // 添加账号：B站/华为/网易都收进同一个选择弹窗（和设计稿一致）
  onAdd: () => openPicker("add"),
  onRefresh: (acc) => {
    // 账号的 channel 与后端登录入口的 action 是同一套取值，
    // 所以"刷新哪个渠道"直接把 channel 当 mode 传下去
    openPicker(acc.channel, { accountId: acc.accountId, name: acc.name, loginUser: acc.loginUser });
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
        <!-- 窗口去掉了系统标题栏（反白问题的根治），标题条自己画 -->
        <div class="app-frame">
          <TitleBar />
          <div class="app-body">
            <LaunchAlertDialog />
            <ReleaseNotice />
            <ShellHost :store="realStore" :default-index="2" :show-switcher="false" :load-pref="loadShellPref" :save-pref="saveShellPref" />

            <ChannelPicker v-model:show="showChannelPicker" :mode="pickerMode" :entries="loginEntries" @pick="onPick" />
            <ChannelWebLoginDialog v-model:show="showBiliLogin" kind="bilibili" />
            <ChannelWebLoginDialog v-model:show="showM4399Login" kind="m4399" />
            <ChannelWebLoginDialog v-model:show="showVivoLogin" kind="vivo" />
            <ChannelAskDialog
              :show="!!askKind"
              :kind="askKind"
              :method="askMethod"
              :title="askKind === 'myapp' ? '登录应用宝账号' : undefined"
              @update:show="(v: boolean) => !v && (askKind = '')"
            />
            <MpayLoginDialog v-model:show="showMpayLogin" />
            <HeadlessLoginDialog v-model:show="showHeadless" :default-user="refreshTarget?.loginUser || ''" />
            <SMSLoginDialog v-model:show="showSMS" :target-user-id="refreshTarget?.accountId || ''" :default-mobile="refreshTarget?.name || ''" />
            <ScanDialog v-model:show="showScan" />
            <HuaweiDialog v-model:show="showHuawei" />
            <SettingsPanel v-model:show="showSettings" />
          </div>
        </div>
      </n-dialog-provider>
    </n-message-provider>
  </n-config-provider>
</template>

<style scoped>
/* 标题条固定 36px，下面才是界面本体。界面壳原来用 100vh，
   现在容器有确定高度，壳改成 100% 就正好填满剩余空间。 */
.app-frame {
  height: 100vh;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.app-body {
  flex: 1;
  min-height: 0;
  position: relative;
}
</style>



