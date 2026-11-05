<script setup lang="ts">
/**
 * 设置弹层（shellkit 设计语言）。
 *
 * 「登录设备」有三档，对应 Go 侧的三种设备号来源：
 *   按本机  fingerprint —— 按本机硬件算，同一台机器永远一样
 *   随机    random      —— 重新生成一个，相当于换一个客户端身份
 *   填入已有 custom    —— 自己填，适合从别处抄来的设备号
 *
 * 和 Go 的交互（都走本地 /api）：
 *   /api/ui/shell                  界面方案偏好
 *   /api/device/register { mode }  按本机 / 重新申请
 *   /api/device/custom   ?id=      填入已有
 *   /api/settings/pick-path        选游戏 exe（Go 弹系统文件框）
 *   /api/settings/pick-folder      选多开镜像目录
 *   /api/settings { closeLauncher / mirrorDir }
 *   /api/open-logs、/api/cleanup
 */
import { computed, h, ref } from "vue";
import { X, Check } from "lucide-vue-next";
import { useDialog, useMessage } from "naive-ui";
import { api } from "../api/client";
import { state } from "../composables/useAppState";
import { SHELLS, activeShell } from "../shellkit/appearance";
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

function close() {
  emit("update:show", false);
}

/* ---- 界面方案 ---- */
function chooseShell(i: number) {
  if (activeShell.value === i) return;
  activeShell.value = i;
  void api.saveShell(i).then((r) => {
    if (r.ok === false) message.error(r.message || "保存偏好失败");
  });
}

/* ---- 启动 / 多开 ---- */
async function pickPath() {
  const r = await api.pickSkyPath();
  if (r.ok === false) message.error(r.message || "选择失败");
  else if (r.path) message.success("已更新游戏路径");
}

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

/* ---- 网盘与关于 ---- */
const checking = ref(false);
const checkResult = ref<null | { hasUpdate: boolean; error?: string }>(null);

async function checkUpdate() {
  checking.value = true;
  checkResult.value = null;
  try {
    const r = await api.releaseCheck();
    if (r.ok === false) {
      checkResult.value = { hasUpdate: false, error: r.message || "检查失败" };
      return;
    }
    const latest = (r as { latest?: { version?: string; changelog?: string; force?: boolean; download_url?: string } }).latest;
    if (r.hasUpdate && latest) {
      // 结果用弹窗展示：更新公告 + 两个下载按钮
      dialog[latest.force ? "error" : "info"]({
        title: `发现新版本 v${latest.version}${latest.force ? "（必须更新）" : ""}`,
        content: () =>
          h("div", [
            h("p", { style: "margin:6px 0 2px;white-space:pre-wrap" }, latest.changelog || "暂无更新说明"),
            h("div", { style: "margin-top:10px;display:flex;gap:8px" }, [
              h(
                "button",
                {
                  onClick: () => openChannel(latest.download_url || state.settings.release.github),
                  style: "padding:6px 14px;border-radius:8px;border:1px solid rgba(255,255,255,.2);background:transparent;color:inherit;cursor:pointer",
                },
                "GitHub 下载",
              ),
              h(
                "button",
                {
                  onClick: () => openChannel(state.settings.release.pan),
                  style: "padding:6px 14px;border-radius:8px;border:0;background:#d8b47c;color:#161619;cursor:pointer",
                },
                "123 网盘",
              ),
            ]),
          ]),
        closable: !latest.force,
        maskClosable: !latest.force,
      });
    } else {
      message.success("已是最新版本");
    }
  } finally {
    checking.value = false;
  }
}

function openChannel(url: string) {
  void api.openExternal(url);
}

/** 交流群：复制群号（WebView2 里 navigator.clipboard 可用，失败退回 execCommand） */
const QQ_GROUP = "1053415841";

async function copyGroup() {
  try {
    await navigator.clipboard.writeText(QQ_GROUP);
    message.success("群号已复制：" + QQ_GROUP);
    return;
  } catch {
    /* 退回到下面的兜底 */
  }
  const ta = document.createElement("textarea");
  ta.value = QQ_GROUP;
  document.body.appendChild(ta);
  ta.select();
  try {
    document.execCommand("copy");
    message.success("群号已复制：" + QQ_GROUP);
  } catch {
    message.warning("复制失败，请手动记下：" + QQ_GROUP);
  }
  document.body.removeChild(ta);
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

/** 切换来源：按本机是复用的；填入已有的会先弹输入框 */
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
  <div v-if="props.show" class="mask" @click.self="close">
    <div class="modal">
      <div class="mhead">
        <b>设置</b>
        <button class="mclose" @click="close"><X /></button>
      </div>

      <div class="body">
        <!-- 界面方案 -->
        <p class="sect">界面方案</p>
        <div class="shellgrid">
          <button v-for="(sh, i) in SHELLS" :key="sh.name" class="shell" :class="{ on: i === activeShell }" @click="chooseShell(i)">
            <span class="sdot" :style="{ background: sh.acc }" />
            <span class="stxt">
              <b>{{ sh.name }}</b>
              <i>{{ sh.desc }}</i>
            </span>
            <span v-if="i === activeShell" class="scheck"><Check /></span>
          </button>
        </div>

        <!-- 启动 -->
        <p class="sect">启动</p>
        <div class="row">
          <span class="key">游戏路径</span>
          <span class="val mono ellipsis" :title="s.skyExe">{{ s.skyExe || "未设置" }}</span>
          <button class="act" @click="pickPath">修改</button>
        </div>
        <div class="row">
          <span class="key">关闭发烧平台</span>
          <span class="val dim">启动前自动关闭，避免抢窗口</span>
          <input type="checkbox" class="switch" :checked="s.closeLauncher" @change="toggleLauncher(($event.target as HTMLInputElement).checked)" />
        </div>

        <!-- 多开 -->
        <p class="sect">多开</p>
        <div class="row">
          <span class="key">镜像目录 <span class="iinfo" title="多开时第 2 个客户端从这里启动（硬链接，不额外占空间），退出后自动删除；必须和游戏同一个磁盘，且只能选空文件夹。多开启动前会强制窗口模式，并按账号设置预写画质/帧率。">i</span></span>
          <span class="val mono ellipsis" :title="s.mirrorDir">{{ s.mirrorDir || s.mirrorDefault }}</span>
          <button class="act" @click="pickMirrorDir">修改</button>
        </div>
        <p v-if="!s.mirrorSameVolume" class="note warn">{{ `当前不在同一磁盘：会复制一份约 ${s.mirrorSizeText}，占用真实空间。` }}</p>

        <!-- 登录设备 -->
        <p class="sect">登录设备</p>
        <div class="row">
          <span class="key">设备号 <span class="iinfo" title="这台机器在网易 / 华为侧登记的身份指纹：同一个设备号登录不会被当成新设备。重置或更换后，账号可能需要重新验证。清理数据会一并清掉它。">i</span></span>
          <span class="val mono ellipsis" :title="s.deviceId">{{ s.deviceId }}</span>
        </div>
        <div class="row">
          <span class="key">来源 <span class="iinfo" title="按本机：申请一次后一直复用，重启也不变。填入已有：只能用服务器发过的设备号（比如从备份或旧机器复制），随手编的会被服务器拒绝。重新申请：向服务器要一个新号，等于换一台设备登录。">i</span></span>
          <span class="val segs">
            <button :class="{ on: mode === 'fingerprint' }" :disabled="working" @click="changeMode('fingerprint')">按本机</button>
            <button :class="{ on: mode === 'custom' }" :disabled="working" @click="changeMode('custom')">填入已有</button>
          </span>
          <button class="act" :disabled="working" @click="confirmRenew">重新申请</button>
        </div>

        <!-- 数据 -->
        <p class="sect">网盘与关于</p>
        <div class="row">
          <span class="key">当前版本</span>
          <span class="val mono">v{{ state.version }}<template v-if="state.settings.appBuild !== '0'"> · 构建 {{ state.settings.appBuild }}</template><template v-if="state.settings.appBuildTime"> · {{ state.settings.appBuildTime }}</template> · {{ s.goVersion }}</span>
        </div>
        <div class="row">
          <span class="key">检查更新</span>
          <span class="val">
            <button class="btn sm" :disabled="checking" @click="checkUpdate">{{ checking ? "检查中…" : "检查更新" }}</button>
          </span>
        </div>
        <div class="row">
          <span class="key">源码</span>
          <span class="val">
            <button class="btn sm" @click="openChannel('https://github.com/mmlhl/skylogin')">GitHub 仓库</button>
          </span>
        </div>
        <div class="row">
          <span class="key">查看发布版本</span>
          <span class="val">
            <button class="btn sm" @click="openChannel(state.settings.release.github)">GitHub 发布页</button>
            <button class="btn sm" @click="openChannel(state.settings.release.pan)">123 网盘</button>
          </span>
        </div>
        <div class="row">
          <span class="key">交流群</span>
          <span class="val mono">{{ QQ_GROUP }}</span>
          <button class="act" @click="copyGroup">复制群号</button>
        </div>

        <p class="sect">数据</p>
        <div class="row">
          <span class="key">数据目录</span>
          <span class="val mono ellipsis dim" :title="s.dataDir">{{ s.dataDir }}</span>
          <button class="act" @click="openLogs">打开日志</button>
        </div>
        <div class="row">
          <span class="key">清理数据</span>
          <span class="val dim">删除账号、设备标识与设置，无法撤销</span>
          <button class="act" @click="confirmCleanup">清理</button>
        </div>
      </div>


      <PromptDialog ref="promptRef" />
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
  --on: #17110a;
  --tx: #f0eee8;
  --dim: #9aa0ac;
  --faint: #6b7382;
  --line: rgba(255, 255, 255, 0.08);
  --line2: rgba(255, 255, 255, 0.14);
  width: 480px;
  max-width: calc(100vw - 48px);
  max-height: calc(100vh - 80px);
  display: flex;
  flex-direction: column;
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
  justify-content: space-between;
  flex: none;
}
.mhead b {
  font-size: 16px;
  font-weight: 600;
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
.body {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding-right: 2px;
}

.iinfo {
  display: inline-grid;
  place-items: center;
  width: 14px;
  height: 14px;
  margin-left: 2px;
  border: 1px solid var(--line2, rgba(255, 255, 255, 0.15));
  border-radius: 50%;
  font-size: 10px;
  font-style: normal;
  color: var(--dim, #98a2ae);
  cursor: help;
  vertical-align: 1px;
}
.note.warn {
  color: #e8b45c;
}
.sect {
  margin: 18px 0 8px;
  font-size: 11.5px;
  letter-spacing: 0.04em;
  color: var(--faint);
}
.sect:first-child {
  margin-top: 14px;
}

/* 界面方案卡片 */
.shellgrid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px;
}
.shell {
  position: relative;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 11px 12px;
  border: 1px solid var(--line);
  border-radius: 12px;
  background: rgba(255, 255, 255, 0.03);
  color: inherit;
  font-family: inherit;
  text-align: left;
  cursor: pointer;
  transition: border-color 0.15s ease-out, background-color 0.15s ease-out;
}
.shell:hover {
  background: rgba(255, 255, 255, 0.06);
}
.shell.on {
  border-color: color-mix(in srgb, var(--acc) 55%, transparent);
  background: color-mix(in srgb, var(--acc) 9%, transparent);
}
.sdot {
  flex: none;
  width: 10px;
  height: 10px;
  border-radius: 50%;
}
.stxt {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 1px;
}
.stxt b {
  font-size: 13px;
  font-weight: 600;
}
.stxt i {
  font-style: normal;
  font-size: 11px;
  color: var(--faint);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.scheck {
  position: absolute;
  top: 8px;
  right: 8px;
  color: var(--acc);
}
.scheck svg {
  width: 13px;
  height: 13px;
}

/* 行 */
.row {
  display: flex;
  align-items: center;
  gap: 12px;
  min-height: 42px;
  border-top: 1px solid var(--line);
  font-size: 13px;
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
  height: 26px;
  padding: 0 10px;
  border: 1px solid var(--line);
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
  color: var(--tx);
}
.act:disabled {
  opacity: 0.4;
  cursor: default;
}

/* 开关（原生 checkbox 画成 pill） */
.switch {
  appearance: none;
  flex: none;
  width: 34px;
  height: 20px;
  margin: 0;
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.14);
  position: relative;
  cursor: pointer;
  transition: background 0.16s ease-out;
}
.switch::before {
  content: "";
  position: absolute;
  top: 2px;
  left: 2px;
  width: 16px;
  height: 16px;
  border-radius: 50%;
  background: #f0eee8;
  transition: transform 0.16s ease-out;
}
.switch:checked {
  background: var(--acc);
}
.switch:checked::before {
  transform: translateX(14px);
  background: var(--on);
}

/* 分段按钮 */
.segs {
  display: flex;
  gap: 6px;
}
.segs button {
  height: 28px;
  padding: 0 12px;
  border: 1px solid var(--line);
  border-radius: 8px;
  background: transparent;
  color: var(--dim);
  font-family: inherit;
  font-size: 12px;
  cursor: pointer;
}
.segs button.on {
  border-color: color-mix(in srgb, var(--acc) 55%, transparent);
  background: color-mix(in srgb, var(--acc) 12%, transparent);
  color: var(--acc);
}
.segs button:disabled {
  opacity: 0.5;
  cursor: default;
}

.note {
  margin: 6px 0 2px;
  font-size: 11.5px;
  line-height: 1.65;
  color: var(--faint);
}
.note b {
  color: var(--tx);
  font-weight: 400;
}
.hint {
  flex: none;
  margin: 12px 0 0;
  padding-top: 10px;
  border-top: 1px solid var(--line);
  font-size: 11.5px;
  line-height: 1.7;
  color: var(--faint);
}
</style>
