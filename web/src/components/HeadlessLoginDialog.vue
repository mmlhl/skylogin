<script setup lang="ts">
/**
 * 网易账号 · 账号密码登录（无头）。
 *
 * 和「官方登录窗」的区别：
 *   - 密码在本工具里输入，所以能存下来供以后自动重登；
 *   - 一次登录拿**两份凭证**：ma75（启动用）+ a50（当寄宿宿主用）；
 *   - 全程没有窗口，失败原因直接显示在这里。
 *
 * 为什么不用官方窗：官方窗的密码我们看不到，存不下来；而且它只给一份凭证，
 * 那个号当不了宿主，扫码号就没有槽位可寄宿。
 */
import { computed, ref, watch } from "vue";
import { api } from "../api/client";
// 这个项目没有全局注册 Naive UI，用到的组件必须逐个显式 import，
// 漏一个就是"点了没反应"（组件未注册，整块渲染不出来）。
import { NButton, NInput, NModal, NRadioButton, NRadioGroup, useMessage } from "naive-ui";

const props = defineProps<{ show: boolean; defaultUser?: string }>();
const emit = defineEmits<{ (e: "update:show", v: boolean): void }>();
const message = useMessage();

const user = ref("");
const password = ref("");
const region = ref<"" | "android" | "ios">("");
const busy = ref(false);
const error = ref("");
const done = ref<null | { name: string; canHost: boolean; existed: boolean }>(null);

const canSubmit = computed(() => !busy.value && user.value.trim() !== "" && password.value !== "");

function reset() {
  // 刷新已有账号时把登录名预填进去：用户只需要补密码。
  // 之前这里一律清空，改过一次密码的用户得先把邮箱重新敲一遍。
  user.value = props.defaultUser || "";
  password.value = "";
  region.value = "";
  busy.value = false;
  error.value = "";
  done.value = null;
}

watch(
  () => props.show,
  (open) => {
    if (open) reset();
  }
);

async function submit() {
  if (!canSubmit.value) return;
  busy.value = true;
  error.value = "";
  try {
    const r: any = await api.headlessLogin(user.value.trim(), password.value, region.value);
    if (r?.ok === false) {
      error.value = r.message || "登录失败";
      return;
    }
    const res = r?.result ?? {};
    done.value = {
      name: res.name || user.value.trim(),
      canHost: !!res.canHost,
      existed: !!res.existed,
    };
    // 有宿主资格才能给扫码号提供槽位；没有的话要明确告诉用户后果
    message.success(
      done.value.canHost
        ? `${done.value.name} 已就绪，可以当寄宿宿主`
        : `${done.value.name} 已就绪（不能当寄宿宿主）`
    );
    window.setTimeout(() => close(), 1200);
  } catch (e: any) {
    error.value = e?.message || "登录失败";
  } finally {
    busy.value = false;
  }
}

function close() {
  emit("update:show", false);
}
</script>

<template>
  <n-modal
    :show="show"
    preset="card"
    title="网易账号 · 账号密码登录"
    style="width: 420px"
    :mask-closable="!busy"
    :closable="!busy"
    @update:show="(v: boolean) => !v && close()"
  >
    <div class="form">
      <n-input
        v-model:value="user"
        placeholder="网易账号（邮箱 / 手机号）"
        :disabled="busy"
        @keyup.enter="submit"
      />
      <n-input
        v-model:value="password"
        type="password"
        show-password-on="click"
        placeholder="密码"
        :disabled="busy"
        @keyup.enter="submit"
      />
      <div class="region">
        <span class="label">游戏区</span>
        <n-radio-group v-model:value="region" :disabled="busy" size="small">
          <n-radio-button value="">先不选</n-radio-button>
          <n-radio-button value="android">安卓</n-radio-button>
          <n-radio-button value="ios">iOS</n-radio-button>
        </n-radio-group>
      </div>
      <p class="hint">
        密码用 Windows DPAPI 加密保存在本机，凭证过期后会自动重登。登录会同时取得启动凭证与平台凭证。
      </p>
      <p v-if="error" class="error">{{ error }}</p>
      <p v-else-if="done" class="ok">
        已添加 {{ done.name }}{{ done.canHost ? "，可以当寄宿宿主" : "，但这个号不能当寄宿宿主" }}
      </p>
    </div>
    <template #footer>
      <div class="footer">
        <n-button :disabled="busy" @click="close">取消</n-button>
        <n-button type="primary" :loading="busy" :disabled="!canSubmit" @click="submit">
          登录
        </n-button>
      </div>
    </template>
  </n-modal>
</template>

<style scoped>
.form {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.region {
  display: flex;
  align-items: center;
  gap: 10px;
}
.label {
  font-size: 12px;
  opacity: 0.7;
}
.hint {
  margin: 0;
  font-size: 12px;
  line-height: 1.6;
  opacity: 0.6;
}
.error {
  margin: 0;
  font-size: 12px;
  color: #e88080;
}
.ok {
  margin: 0;
  font-size: 12px;
  color: #7ec699;
}
.footer {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
</style>
