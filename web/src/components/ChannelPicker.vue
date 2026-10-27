<script setup lang="ts">
/**
 * 登录入口选择（shellkit 设计语言）。
 *
 * mode:
 *   add      —— 添加账号：顶层是 B站/4399/vivo/华为/网易；
 *              点「网易账号」进入第二级，再选 扫码/手机号/账号密码
 *   channel  —— 给华为账号刷新凭证时只看华为的入口
 *   netease  —— 给网易账号刷新凭证时只看网易的三种入口
 *   bilibili / m4399 / vivo —— 对应渠道账号的刷新入口
 *
 * 各入口最终路由（App.vue 的 onPick）：
 *   华为扫码 → HuaweiDialog；网易扫码 → ScanDialog；
 *   手机号 → SMSLoginDialog；账号密码 → mpay.dll 官方登录窗；
 *   B站/4399/vivo → 渠道网页登录子进程
 */
import { computed, ref, watch } from "vue";
import { ChevronLeft, X } from "lucide-vue-next";
import type { Channel } from "../api/types";

/** 一个可选入口 */
interface Entry {
  key: string;
  name: string;
  hint: string;
  initial: string;
  /** qrcode 扫码 / sms 手机号验证码 / mpay 网易官方登录窗（账号密码等）/ 渠道网页登录；group 是二级菜单入口 */
  kind: "qrcode" | "sms" | "mpay" | "biliweb" | "m4399web" | "vivoweb" | "group";
  channel: Channel;
}

/** 网易的入口（添加时的第二级 / 刷新时的直接列表）。
 *  手机号登录不再单独列出：官方登录窗里自带手机号登录（2026-10-26 起我们自己的
 *  短信接口被网易卡住，官方窗口是唯一稳定的路） */
const NETEASE_ENTRIES: Entry[] = [
  { key: "netease-qr", name: "扫码登录", hint: "用光遇手游扫码", initial: "码", kind: "qrcode", channel: "netease" },
  { key: "netease-pwd", name: "账号密码登录", hint: "网易官方登录窗：账号密码 / 手机号", initial: "密", kind: "mpay", channel: "netease" },
];

/** 添加账号的顶层入口 */
const ADD_TOP: Entry[] = [
  { key: "bili", name: "B站账号", hint: "在弹出的窗口里登录 B站账号", initial: "B", kind: "biliweb", channel: "bilibili" },
  { key: "m4399", name: "4399账号", hint: "在弹出的窗口里登录 4399 账号", initial: "4", kind: "m4399web", channel: "m4399" },
  { key: "vivo", name: "vivo账号", hint: "在弹出的窗口里登录 vivo 账号", initial: "V", kind: "vivoweb", channel: "vivo" },
  { key: "huawei", name: "华为账号", hint: "用华为手机扫码", initial: "华", kind: "qrcode", channel: "huawei" },
  { key: "netease-group", name: "网易账号", hint: "扫码 / 账号密码（含手机号）", initial: "网", kind: "group", channel: "netease" },
];

const props = defineProps<{ show: boolean; mode: "channel" | "netease" | "bilibili" | "m4399" | "vivo" | "add" }>();
const emit = defineEmits<{
  (e: "update:show", v: boolean): void;
  (e: "pick", entry: Entry): void;
}>();

/** 「添加账号」里网易是否已展开到第二级（弹窗内部状态，不惊动父层） */
const neteaseOpen = ref(false);
watch(
  () => props.show,
  (v) => {
    if (!v) neteaseOpen.value = false;
  }
);

const FILTER: Record<string, Entry["channel"]> = {
  channel: "huawei",
  netease: "netease",
  bilibili: "bilibili",
  m4399: "m4399",
  vivo: "vivo",
};

const list = computed(() => {
  if (props.mode === "add") return neteaseOpen.value ? NETEASE_ENTRIES : ADD_TOP;
  return NETEASE_ENTRIES.concat(ADD_TOP).filter((e) => e.channel === FILTER[props.mode] && e.kind !== "group");
});
const TITLE: Record<string, string> = {
  channel: "刷新凭证 · 华为",
  netease: "刷新凭证 · 网易",
  bilibili: "刷新凭证 · B站",
  m4399: "刷新凭证 · 4399",
  vivo: "刷新凭证 · vivo",
  add: "添加账号",
};
const title = computed(() => (props.mode === "add" && neteaseOpen.value ? "添加账号 · 网易" : TITLE[props.mode]));
const tip = computed(() => {
  if (props.mode === "netease") return "三种方式最终都会保存同样的登录凭证，之后启动不用再输";
  if (props.mode === "bilibili") return "重新登录同一个 B站账号，凭证会更新到这个账号上";
  if (props.mode === "m4399") return "重新登录同一个 4399 账号，凭证会更新到这个账号上";
  if (props.mode === "vivo") return "重新登录同一个小号，凭证会更新到这个账号上";
  if (props.mode === "add") {
    return neteaseOpen.value
      ? "三种方式最终都会保存同样的登录凭证，之后启动不用再输"
      : "华为账号靠扫码；B站/4399/vivo 在弹出的窗口里登录";
  }
  return "用华为手机重新扫码，凭证会更新到这个账号上";
});

function onPick(e: Entry) {
  if (e.kind === "group") {
    neteaseOpen.value = true;
    return;
  }
  emit("pick", e);
}

function close() {
  emit("update:show", false);
}
</script>

<template>
  <div v-if="props.show" class="mask" @click.self="close">
    <div class="modal">
      <div class="mhead">
        <button v-if="neteaseOpen" class="mback" title="返回" @click="neteaseOpen = false"><ChevronLeft /></button>
        <b>{{ title }}</b>
        <button class="mclose" @click="close"><X /></button>
      </div>

      <div class="list">
        <button v-for="e in list" :key="e.key" class="entry" @click="onPick(e)">
          <span class="icon">{{ e.initial }}</span>
          <span class="text">
            <b>{{ e.name }}</b>
            <small>{{ e.hint }}</small>
          </span>
        </button>
      </div>

      <p class="tip">{{ tip }}</p>
    </div>
  </div>
</template>

<style scoped>
.mask {
  position: fixed;
  inset: 0;
  z-index: 60;
  display: grid;
  place-items: center;
  background: rgba(0, 0, 0, 0.55);
}
.modal {
  --acc: #d8b47c;
  --tx: #f0eee8;
  --dim: #9aa0ac;
  --faint: #6b7382;
  --line: rgba(255, 255, 255, 0.08);
  --line2: rgba(255, 255, 255, 0.14);
  width: 420px;
  max-width: calc(100vw - 48px);
  padding: 20px;
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 16px;
  background: #17171b;
  color: var(--tx);
  box-shadow: 0 32px 80px rgba(0, 0, 0, 0.6);
}
.mhead {
  display: flex;
  align-items: center;
  margin-bottom: 14px;
}
.mhead b {
  flex: 1;
  font-size: 16px;
  font-weight: 600;
}
.mback {
  display: grid;
  place-items: center;
  width: 28px;
  height: 28px;
  margin-right: 8px;
  border: 0;
  border-radius: 8px;
  background: transparent;
  color: #8f8a80;
  cursor: pointer;
}
.mback:hover {
  background: rgba(255, 255, 255, 0.07);
  color: var(--tx);
}
.mback svg {
  width: 15px;
  height: 15px;
}
.mclose {
  display: grid;
  place-items: center;
  width: 28px;
  height: 28px;
  border: 0;
  border-radius: 8px;
  background: transparent;
  color: #8f8a80;
  cursor: pointer;
}
.mclose:hover {
  background: rgba(255, 255, 255, 0.07);
  color: var(--tx);
}
.mclose svg {
  width: 15px;
  height: 15px;
}

.list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.entry {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 14px;
  border: 1px solid var(--line);
  border-radius: 12px;
  background: rgba(255, 255, 255, 0.03);
  color: var(--tx);
  font-family: inherit;
  text-align: left;
  cursor: pointer;
  transition: background 0.15s ease-out, border-color 0.15s ease-out;
}
.entry:hover {
  background: rgba(255, 255, 255, 0.06);
  border-color: var(--line2);
}
.icon {
  display: grid;
  place-items: center;
  width: 32px;
  height: 32px;
  border-radius: 9px;
  background: rgba(255, 255, 255, 0.07);
  color: var(--dim);
  font-size: 12.5px;
  font-weight: 600;
  flex: none;
}
.text {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.text b {
  font-size: 13.5px;
  font-weight: 500;
}
.text small {
  font-size: 12px;
  color: var(--faint);
}
.tip {
  margin: 14px 0 0;
  font-size: 11.5px;
  line-height: 1.7;
  color: var(--faint);
}
</style>
