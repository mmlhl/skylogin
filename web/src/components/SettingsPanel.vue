<script setup lang="ts">
/**
 * 设置弹层。
 *
 * 「登录设备」有三档，对应 Go 侧的三种设备号来源：
 *   固定算法  fingerprint —— 按本机硬件算，同一台机器永远一样
 *   随机      random      —— 重新生成一个，相当于换一个客户端身份
 *   手动      custom      —— 自己填，适合从别处抄来的设备号
 *
 * 和 Go 的交互（都走本地 /api）：
 *   /api/device/register { mode }  固定算法 / 随机
 *   /api/device/custom   ?id=      手动指定
 *   /api/settings/pick-path        选游戏 exe（Go 弹系统文件框）
 *   /api/settings { closeLauncher } 启动前是否关闭发烧平台
 *   /api/open-logs、/api/cleanup
 */
import { computed, ref } from "vue";
import { NModal, NRadioGroup, NRadioButton, NSwitch, useDialog, useMessage } from "naive-ui";
import { api } from "../api/client";
import { state } from "../composables/useAppState";
import PromptDialog from "./PromptDialog.vue";

const props = defineProps<{ show: boolean }>();
const emit = defineEmits<{ (e: "update:show", v: boolean): void }>();

const message = useMessage();
const dialog = useDialog();
const working = ref(false);
const promptRef = ref<InstanceType<typeof PromptDialog> | null>(null);

const s = computed(() => state.settings);

/** 当前设备号来源，直接映射成三档 */
const mode = computed(() => (s.value.deviceIdSource === "custom" ? "custom" : s.value.deviceIdSource === "random" ? "random" : "fingerprint"));

async function pickPath() {
  const r = await api.pickSkyPath();
  if (r.ok === false) message.error(r.message || "选择失败");
  else if (r.path) message.success("已更新游戏路径");
}

/** 选多开镜像目录（第 2 个客户端从这里启动） */
async function pickMirrorDir() {
  const r = await api.pickFolder();
  if (r.ok === false) {
    message.error(r.message || "选择失败");
    return;
  }
  if (!r.path) return;
  const save = await api.saveSettings({ mirrorDir: r.path });
  if (save.ok === false) message.error(save.message || "保存失败");
  else message.success("已更新多开目录");
}

async function toggleLauncher(v: boolean) {
  const r = await api.saveSettings({ closeLauncher: v });
  if (r.ok === false) message.error(r.message || "保存失败");
}

/** 重新申请：向服务器要一个新设备号，等于换一台设备登录，所以先二次确认 */
function confirmRenew() {
  dialog.warning({
    title: "重新申请设备号",
    content: "会向服务器申请一个新的设备号，等同换一台设备登录，可能触发验证。确定继续？",
    positiveText: "申请",
    negativeText: "取消",
    onPositiveClick: async () => {
      working.value = true;
      try {
        const r = await api.registerDevice("random");
        if (r.ok === false) message.error(r.message || "申请失败");
        else message.success("已换用新设备号");
      } finally {
        working.value = false;
      }
    },
  });
}

/** 切换来源：按本机是复用的；手动填写的会先弹输入框 */
async function changeMode(v: string) {
  if (v === "custom") {
    const input = await promptRef.value?.open(
      "手动指定设备号",
      s.value.deviceId,
      "服务器发过的设备号，可写成 设备号:key。留空取消"
    );
    if (!input) return;
    working.value = true;
    try {
      const r = await api.setCustomDevice(input.trim());
      if (r.ok === false) message.error(r.message || "设置失败");
      else message.success("已使用手动设备号");
    } finally {
      working.value = false;
    }
    return;
  }

  working.value = true;
  try {
    const r = await api.registerDevice("fingerprint");
    if (r.ok === false) message.error(r.message || "切换失败");
    else message.success("已改回按本机");
  } finally {
    working.value = false;
  }
}

function confirmCleanup() {
  dialog.warning({
    title: "清理工具数据",
    content: "会删除账号、设备标识和设置，无法撤销。",
    positiveText: "清理",
    negativeText: "取消",
    onPositiveClick: async () => {
      const r = await api.cleanup();
      if (r.ok === false) message.error(r.message || "清理失败");
    },
  });
}

async function openLogs() {
  const r = await api.openLogs();
  if (r.ok === false) message.error(r.message || "打开失败");
}
</script>

<template>
  <n-modal
    :show="props.show"
    preset="card"
    title="设置"
    style="width: 460px"
    display-directive="if"
    @update:show="(v: boolean) => emit('update:show', v)"
  >
    <div class="rows">
      <div class="row">
        <span class="key">游戏路径</span>
        <span class="val mono ellipsis" :title="s.skyExe">{{ s.skyExe || "未设置" }}</span>
        <button class="act" @click="pickPath">修改</button>
      </div>

      <div class="row">
        <span class="key">多开目录</span>
        <span class="val mono ellipsis" :title="s.mirrorDir">{{ s.mirrorDir || s.mirrorDefault }}</span>
        <button class="act" @click="pickMirrorDir">修改</button>
      </div>
      <p class="note">
        第 2 个客户端从这里启动，退出后自动删除。
        <b>必须和游戏在同一个磁盘</b>（硬链接，不额外占空间），且只能选空文件夹。
        {{
          s.mirrorSameVolume
            ? ""
            : `当前不在同一磁盘：会复制一份约 ${s.mirrorSizeText}，占用真实空间。`
        }}
      </p>
      <div class="row">
        <span class="key">登录设备</span>
        <span class="val mono ellipsis" :title="s.deviceId">{{ s.deviceId }}</span>
      </div>

      <div class="row">
        <span class="key">来源</span>
        <span class="val">
          <n-radio-group :value="mode" size="small" :disabled="working" @update:value="changeMode">
            <n-radio-button value="fingerprint">按本机</n-radio-button>
            <n-radio-button value="custom">填入已有</n-radio-button>
          </n-radio-group>
        </span>
        <button class="act" :disabled="working" @click="confirmRenew">重新申请</button>
      </div>

      <div class="row">
        <span class="key">关闭发烧平台</span>
        <span class="val dim">启动前自动关闭，避免抢窗口</span>
        <n-switch :value="s.closeLauncher" size="small" @update:value="toggleLauncher" />
      </div>

      <div class="row">
        <span class="key">数据目录</span>
        <span class="val mono ellipsis dim" :title="s.dataDir">{{ s.dataDir }}</span>
        <button class="act" @click="openLogs">打开日志</button>
      </div>

      <div class="row">
        <span class="key">版本</span>
        <span class="val dim">v{{ s.version }}{{ s.buildTime ? " · 构建 " + s.buildTime : "" }} · {{ s.goVersion }}</span>
        <button class="act" @click="confirmCleanup">清理数据</button>
      </div>
    </div>

    <p class="hint">按本机：申请一次后一直复用，重启也不变；填入已有：只能用服务器发过的设备号（比如从备份或旧机器复制），随手编的会被服务器拒绝；重新申请：向服务器要一个新号。</p>

    <PromptDialog ref="promptRef" />
  </n-modal>
</template>

<style scoped>
.note b {
  color: var(--text);
  font-weight: 400;
}

.note {
  margin: -2px 0 10px;
  font-size: 11.5px;
  line-height: 1.65;
  color: var(--faint);
}
.rows {
  display: flex;
  flex-direction: column;
}

.row {
  display: flex;
  align-items: center;
  gap: 12px;
  min-height: 44px;
  border-top: 1px solid var(--line);
  font-size: 13px;
}
.row:first-child {
  border-top: 0;
}

.key {
  flex: none;
  width: 84px;
  font-size: 12px;
  color: var(--faint);
}

.val {
  flex: 1;
  min-width: 0;
  color: var(--dim);
}

.dim {
  color: var(--faint);
}

.ellipsis {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.act {
  flex: none;
  padding: 4px 8px;
  border: 0;
  border-radius: 7px;
  background: transparent;
  color: var(--dim);
  font-family: inherit;
  font-size: 12px;
  cursor: pointer;
  transition: background 0.16s ease-out, color 0.16s ease-out;
}
.act:hover {
  background: rgba(255, 255, 255, 0.06);
  color: var(--text);
}
.act:disabled {
  opacity: 0.4;
  cursor: default;
}

.hint {
  margin: 14px 0 0;
  font-size: 11.5px;
  line-height: 1.7;
  color: var(--faint);
}
</style>



