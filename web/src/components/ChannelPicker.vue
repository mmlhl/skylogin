<script setup lang="ts">
/**
 * 登录入口选择。
 *
 * 两个按钮分别对应两组入口：
 *   添加渠道账号 → 第三方渠道（华为等）
 *   添加网易账号 → 网易自己的登录方式（扫码 / 手机号 / 账号密码）
 *
 * 之前四种入口平铺在一起，用户分不清"渠道"和"网易账号"，这里按来源分开。
 */
import { computed } from "vue";
import { NModal } from "naive-ui";
import type { Channel } from "../api/types";

/** 一个可选入口 */
interface Entry {
  key: string;
  name: string;
  hint: string;
  initial: string;
  /** qrcode 扫码 / sms 手机号验证码 / mpay 网易官方登录窗（账号密码等） */
  kind: "qrcode" | "sms" | "mpay";
  channel: Channel;
}

const ALL: Entry[] = [
  { key: "huawei", name: "华为账号", hint: "用华为手机扫码", initial: "华", kind: "qrcode", channel: "huawei" },
  { key: "netease-qr", name: "扫码登录", hint: "用光遇手游扫码", initial: "码", kind: "qrcode", channel: "netease" },
  { key: "netease-sms", name: "手机号登录", hint: "手机号 + 短信验证码", initial: "手", kind: "sms", channel: "netease" },
  { key: "netease-pwd", name: "账号密码登录", hint: "网易官方登录窗：账号密码 / 手机号", initial: "密", kind: "mpay", channel: "netease" },
];

const props = defineProps<{ show: boolean; mode: "channel" | "netease" }>();
const emit = defineEmits<{
  (e: "update:show", v: boolean): void;
  (e: "pick", entry: Entry): void;
}>();

const list = computed(() => (props.mode === "channel" ? ALL.filter((e) => e.channel === "huawei") : ALL.filter((e) => e.channel === "netease")));
const title = computed(() => (props.mode === "channel" ? "添加渠道账号" : "添加网易账号"));
</script>

<template>
  <n-modal
    :show="show"
    preset="card"
    :title="title"
    style="width: 420px"
    display-directive="if"
    @update:show="(v: boolean) => emit('update:show', v)"
  >
    <div class="list">
      <button v-for="e in list" :key="e.key" class="entry" @click="emit('pick', e)">
        <span class="icon">{{ e.initial }}</span>
        <span class="text">
          <b>{{ e.name }}</b>
          <small>{{ e.hint }}</small>
        </span>
      </button>
    </div>

    <p v-if="mode === 'netease'" class="tip">三种方式最终都会保存同样的登录凭证，之后启动不用再输</p>
    <p v-else class="tip">目前支持华为渠道，其它渠道陆续加</p>
  </n-modal>
</template>

<style scoped>
.list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.entry {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 13px 16px;
  border: 1px solid var(--line);
  border-radius: var(--r-md);
  background: rgba(255, 255, 255, 0.03);
  color: var(--text);
  font-family: inherit;
  text-align: left;
  cursor: pointer;
  transition: background 0.16s ease-out, border-color 0.16s ease-out;
}
.entry:hover {
  background: rgba(255, 255, 255, 0.06);
  border-color: var(--line-strong);
}

.icon {
  display: grid;
  place-items: center;
  width: 30px;
  height: 30px;
  border-radius: var(--r-sm);
  background: rgba(255, 255, 255, 0.07);
  color: var(--dim);
  font-size: 12.5px;
  font-weight: 600;
  flex: none;
}

.text {
  display: flex;
  flex-direction: column;
  gap: 3px;
}
.text b {
  font-size: 13.5px;
  font-weight: 400;
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
