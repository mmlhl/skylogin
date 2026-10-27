<script setup lang="ts">
/**
 * 共享弹层 + 吐司：添加账号 / 多开（含窗口排列方案）/ 改名。
 * 四种界面壳共用这一层；强调色跟随当前壳（--demo-acc，由 DesignDemo 注入）。
 */
import { computed, ref, watch } from "vue";
import { X, QrCode, MessageSquare, KeyRound, LayoutGrid } from "lucide-vue-next";
import type { LauncherStore } from "./contract";
import { demoUi } from "./ui";
import { LAYOUTS, LAYOUT_NONE, recommendLayout, channelLabel, fmtRun } from "./contract";

const props = defineProps<{ store: LauncherStore }>();
const store = props.store;

/* ---- 添加账号 ---- */
const addChannel = ref<"huawei" | "netease">("huawei");
const addMethod = ref<"qrcode" | "sms" | "mpay">("qrcode");
watch(addChannel, (c) => {
  if (c === "netease" && addMethod.value !== "qrcode") addMethod.value = "qrcode";
});
type AddMethod = "qrcode" | "sms" | "mpay";
// 渠道与方式的对应关系和真实后端一致：华为只有扫码；短信 / 密码窗口都是网易的
const methods = computed<{ v: AddMethod; label: string; desc: string; icon: unknown }[]>(() =>
  addChannel.value === "huawei"
    ? [{ v: "qrcode", label: "扫码登录", desc: "手机华为帐号扫码", icon: QrCode }]
    : [
        { v: "qrcode", label: "扫码登录", desc: "用光遇手游扫码", icon: QrCode },
        { v: "sms", label: "手机号登录", desc: "手机号 + 短信验证码", icon: MessageSquare },
        { v: "mpay", label: "账号密码登录", desc: "网易官方登录窗", icon: KeyRound },
      ]
);
function confirmAdd() {
  store.addAccount({ channel: addChannel.value });
  demoUi.addOpen = false;
}

/* ---- 多开 ---- */
const picked = ref<string[]>([]);
const layoutKey = ref<string>("quad");
// 显示方式：窗口平铺（网格各占一块）/ 窗口堆叠（文件窗式错开）/ 全屏。
// 超过 4 个只允许堆叠：平铺下去每格互相咬，不如堆叠每个都露标题条。
const display = ref<"tile" | "cascade" | "fullscreen">("tile");
const cascadeOnly = computed(() => toLaunch.value > 4);
/* 画质 / 帧率 / 鼠标仲裁：和设置面板里的「多开默认」是同一份 */
const qMode = ref("");
const qFps = ref(0);
const arbiterEnabled = ref(true);
const arbiterMinText = ref("3");
const arbiterMin = computed(() => Math.min(60, Math.max(1, Math.round(Number(arbiterMinText.value) || 3))));
watch(
  () => demoUi.multiOpen,
  (open) => {
    if (!open) return;
    picked.value = demoUi.multiPreset.length
      ? store.accounts.filter((a) => demoUi.multiPreset.includes(a.id)).map((a) => a.id)
      : store.accounts.filter((a) => a.current || (!a.running && a.cred === "ok")).map((a) => a.id);
    layoutKey.value = recommendLayout(Math.max(picked.value.filter((id) => !isRunning(id)).length, 1)).key;
    display.value = "tile";
    const d = store.multiDefaults();
    qMode.value = d.qualityMode;
    qFps.value = d.fps;
    arbiterEnabled.value = d.arbiter;
    arbiterMinText.value = String(d.arbiterMin > 0 ? d.arbiterMin : 3);
  }
);
function isRunning(id: string) {
  return store.accounts.find((a) => a.id === id)?.running ?? false;
}
const pickedCount = computed(() => picked.value.length);
const toLaunch = computed(() => picked.value.filter((id) => !isRunning(id)).length);
const recKey = computed(() => (toLaunch.value > 1 ? recommendLayout(toLaunch.value).key : LAYOUT_NONE.key));
watch(pickedCount, () => {
  layoutKey.value = recKey.value;
  if (toLaunch.value > 4) display.value = "cascade"; // 超过 4 个只能窗口堆叠
});
const curLayout = computed(() => [...LAYOUTS, LAYOUT_NONE].find((l) => l.key === layoutKey.value) ?? LAYOUT_NONE);
/* 自定义窗口尺寸：两项都 >0 才生效，缺省铺满格子 */
const customW = ref("");
const customH = ref("");
const customSize = computed(() => {
  const w = Math.round(Number(customW.value));
  const h = Math.round(Number(customH.value));
  return w > 0 && h > 0 ? { w, h } : undefined;
});
function togglePick(id: string) {
  const i = picked.value.indexOf(id);
  if (i >= 0) picked.value.splice(i, 1);
  else picked.value.push(id);
}
const qualityOpts = [
  { v: "", label: "跟随游戏", tip: "" },
  { v: "LowPower", label: "省电", tip: "游戏内部档位 LowPower" },
  { v: "Presentation", label: "高画质", tip: "游戏内部档位 Presentation" },
];
const fpsOpts = [
  { v: 0, label: "跟随游戏" },
  { v: 30, label: "30" },
  { v: 60, label: "60" },
];
function pickQuality(v: string) {
  qMode.value = v;
  store.saveMultiDefaults(v, qFps.value, arbiterEnabled.value, arbiterMin.value);
}
function pickFps(v: number) {
  qFps.value = v;
  store.saveMultiDefaults(qMode.value, v, arbiterEnabled.value, arbiterMin.value);
}
function pickArbiter(v: boolean) {
  arbiterEnabled.value = v;
  store.saveMultiDefaults(qMode.value, qFps.value, v, arbiterMin.value);
}
function pickArbiterMin() {
  arbiterMinText.value = String(arbiterMin.value);
  store.saveMultiDefaults(qMode.value, qFps.value, arbiterEnabled.value, arbiterMin.value);
}

function confirmMulti() {
  if (display.value === "fullscreen") {
    store.multiLaunch(picked.value, undefined, undefined, "fullscreen");
  } else if (display.value === "cascade") {
    store.multiLaunch(picked.value, undefined, customSize.value, "cascade");
  } else {
    store.multiLaunch(picked.value, curLayout.value, customSize.value);
  }
  demoUi.multiOpen = false;
}

/* ---- 改名 ---- */
const renameText = ref("");
watch(
  () => demoUi.renameId,
  (id) => {
    renameText.value = store.accounts.find((a) => a.id === id)?.name ?? "";
  }
);
function confirmRename() {
  if (demoUi.renameId) store.rename(demoUi.renameId, renameText.value);
  demoUi.renameId = "";
}
</script>

<template>
  <!-- 添加账号 -->
  <div v-if="demoUi.addOpen" class="mask" @click.self="demoUi.addOpen = false">
    <div class="modal">
      <div class="mhead">
        <b>添加账号</b>
        <button class="mclose" @click="demoUi.addOpen = false"><X /></button>
      </div>

      <p class="mlabel">选择渠道</p>
      <div class="chans">
        <button :class="{ on: addChannel === 'huawei' }" @click="addChannel = 'huawei'">
          <b>华为渠道</b>
          <i>手机扫码</i>
        </button>
        <button :class="{ on: addChannel === 'netease' }" @click="addChannel = 'netease'">
          <b>网易官方</b>
          <i>官网网页扫码</i>
        </button>
      </div>

      <p class="mlabel">登录方式</p>
      <div class="meths">
        <button v-for="m in methods" :key="m.v" class="meth" :class="{ on: addMethod === m.v }" @click="addMethod = m.v">
          <span class="methico"><component :is="m.icon" /></span>
          <span class="mtxt2">
            <b>{{ m.label }}</b>
            <i>{{ m.desc }}</i>
          </span>
          <span class="methdot" />
        </button>
      </div>

      <p class="mhint">演示环境不弹真实二维码，点击「模拟扫码成功」完成添加。</p>
      <div class="mfoot">
        <button class="btn primary" @click="confirmAdd">模拟扫码成功</button>
      </div>
    </div>
  </div>

  <!-- 多开 -->
  <div v-if="demoUi.multiOpen" class="mask" @click.self="demoUi.multiOpen = false">
    <div class="modal wide">
      <div class="mhead">
        <b>多开<em v-if="toLaunch" class="mcount">{{ toLaunch }} 个待启动</em></b>
        <button class="mclose" @click="demoUi.multiOpen = false"><X /></button>
      </div>

      <p class="mlabel">选择账号</p>
      <div class="mlist">
        <label v-for="a in store.accounts" :key="a.id" class="mrow">
          <input type="checkbox" :checked="picked.includes(a.id)" @change="togglePick(a.id)" />
          <span class="ava">{{ a.name.slice(0, 1) }}</span>
          <span class="mtxt">
            <b>{{ a.name }}</b>
            <i>
              <em v-if="a.running" class="dot ok live" />
              <em v-else-if="a.cred === 'expired'" class="dot bad" />
              {{ a.running ? "运行中 · " + fmtRun(a.runSeconds) : a.cred === "expired" ? "凭证过期（将自动刷新）" : "就绪" }}
            </i>
          </span>
          <span class="pill">{{ channelLabel(a.channel) }}</span>
        </label>
      </div>

      <p class="mlabel">显示方式<em class="mrec">{{ cascadeOnly ? "超过 4 个不建议平铺，堆叠 / 全屏都可以" : "全屏 = 每个都占整屏，用监控墙切换" }}</em></p>
      <div class="segs">
        <button :class="{ on: display === 'tile' }" :disabled="cascadeOnly" @click="display = 'tile'">窗口平铺</button>
        <button :class="{ on: display === 'cascade' }" @click="display = 'cascade'">窗口堆叠</button>
        <button :class="{ on: display === 'fullscreen' }" @click="display = 'fullscreen'">全屏</button>
      </div>

      <template v-if="display === 'tile'">
        <p class="mlabel">窗口平铺<em class="mrec">全屏客户端会自动转窗口模式 · 每格保持 16:9 · 窗口出一个排一个</em></p>
        <div class="lays">
          <button
            v-for="l in [...LAYOUTS, LAYOUT_NONE]"
            :key="l.key"
            class="lay"
            :class="{ on: layoutKey === l.key, dim: l.max < toLaunch }"
            @click="layoutKey = l.key"
          >
            <em v-if="l.key === recKey && toLaunch > 1" class="recl">推荐</em>
            <span v-if="l.cols > 0" class="mini" :style="{ gridTemplateColumns: `repeat(${l.cols}, 1fr)` }">
              <i v-for="n in l.cols * l.rows" :key="n" :class="{ fill: n <= toLaunch }" />
            </span>
            <span v-else class="mini none"><LayoutGrid /></span>
            <b>{{ l.label }}</b>
            <i v-if="l.max < 99">≤ {{ l.max }} 个</i>
          </button>
        </div>
      </template>
      <p v-else-if="display === 'cascade'" class="mhint">窗口堆叠：全部窗口像文件一样从左上角错开叠放，每个都露出标题条，点标题切换；也可用监控墙。</p>
      <p v-else class="mhint">全屏：每个客户端都占满整屏叠在一起，只露最上面那个；点监控墙的小窗切换到对应的号。</p>

      <template v-if="display !== 'fullscreen'">
        <p class="mlabel">窗口大小<em class="mrec">留空 = {{ display === 'cascade' ? '引擎最小尺寸（1024×720）' : '铺满格子' }}</em></p>
        <div class="sizerow">
          <input v-model="customW" class="minput size" type="number" min="100" placeholder="宽" />
          <span class="sizex">×</span>
          <input v-model="customH" class="minput size" type="number" min="100" placeholder="高" />
          <span class="sizehint">游戏有最小尺寸限制时，实际大小以游戏为准</span>
        </div>
      </template>

      <p class="mlabel">画质<em class="mrec">启动前写进每个客户端，也是默认值</em></p>
      <div class="segs">
        <button v-for="o in qualityOpts" :key="'q' + o.v" :class="{ on: qMode === o.v }" :title="o.tip" @click="pickQuality(o.v)">{{ o.label }}</button>
      </div>
      <p class="mlabel">帧率</p>
      <div class="segs">
        <button v-for="o in fpsOpts" :key="'f' + o.v" :class="{ on: qFps === o.v }" @click="pickFps(o.v)">{{ o.label }}</button>
      </div>

      <p class="mlabel">鼠标仲裁<em class="mrec">多开启动后的 {{ arbiterEnabled ? arbiterMin + " 分钟内" : "——已关" }}，自动释放后台客户端抢走的鼠标</em></p>
      <div class="sizerow">
        <label class="form-check arbiter-toggle">
          <input type="checkbox" :checked="arbiterEnabled" @change="pickArbiter(!arbiterEnabled)" />
          <span>进大厅后自动释放被抢走的鼠标</span>
        </label>
        <input
          v-model.number="arbiterMinText"
          class="minput size"
          type="number"
          min="1"
          max="60"
          :disabled="!arbiterEnabled"
          @change="pickArbiterMin()"
        />
        <span class="sizehint">分钟（1~60）</span>
      </div>

      <div class="mfoot">
        <span class="mhint">第 2 个起从镜像目录启动，不占额外磁盘</span>
        <button class="btn primary" :disabled="!toLaunch" @click="confirmMulti">
          一起启动（{{ toLaunch }}{{ display === "fullscreen" ? " · 全屏" : display === "cascade" ? " · 窗口堆叠" : curLayout.cols > 0 && toLaunch > 1 ? " · " + curLayout.label : "" }}{{ display !== "fullscreen" && customSize ? " · " + customSize.w + "×" + customSize.h : "" }}）
        </button>
      </div>
    </div>
  </div>

  <!-- 改名 -->
  <div v-if="demoUi.renameId" class="mask" @click.self="demoUi.renameId = ''">
    <div class="modal slim">
      <div class="mhead">
        <b>修改别名</b>
        <button class="mclose" @click="demoUi.renameId = ''"><X /></button>
      </div>
      <input v-model="renameText" class="minput" maxlength="24" placeholder="最多 24 个字" @keyup.enter="confirmRename" />
      <div class="mfoot">
        <button class="btn primary" @click="confirmRename">保存</button>
      </div>
    </div>
  </div>

  <!-- 吐司：有批量操作条时往上抬 -->
  <div class="toasts" :class="{ lifted: store.selectedIds.length }">
    <div v-for="t in store.toasts" :key="t.id" class="toast">{{ t.text }}</div>
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
  /* 跟随当前壳的主题色（DesignDemo 注入），缺省用烛金 */
  --acc: var(--demo-acc, #e3c28a);
  --on: var(--demo-on, #1a1408);
  width: 420px;
  max-width: calc(100vw - 48px);
  max-height: calc(100vh - 80px);
  overflow-y: auto;
  padding: 20px;
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 16px;
  background: #17171b;
  color: #f3f1ea;
  box-shadow: 0 32px 80px rgba(0, 0, 0, 0.6);
}
.modal.wide {
  width: 480px;
}
.modal.slim {
  width: 340px;
}
.mhead {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.mhead b {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  font-size: 16px;
  font-weight: 600;
}
.mcount {
  font-style: normal;
  font-size: 11.5px;
  font-weight: 500;
  color: var(--acc);
  background: color-mix(in srgb, var(--acc) 14%, transparent);
  padding: 2px 8px;
  border-radius: 999px;
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
  color: #f3f1ea;
}
.mclose svg {
  width: 15px;
  height: 15px;
}
.mlabel {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 16px 0 8px;
  font-size: 12px;
  color: #8f8a80;
}
.mrec {
  font-style: normal;
  font-size: 11px;
  color: #6f6a60;
}

/* 渠道卡片 */
.chans {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px;
}
.chans button {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 12px 14px;
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 12px;
  background: rgba(255, 255, 255, 0.03);
  color: inherit;
  font-family: inherit;
  text-align: left;
  cursor: pointer;
  transition: border-color 0.15s ease-out, background-color 0.15s ease-out;
}
.chans button:hover {
  background: rgba(255, 255, 255, 0.06);
}
.chans button.on {
  border-color: color-mix(in srgb, var(--acc) 55%, transparent);
  background: color-mix(in srgb, var(--acc) 9%, transparent);
}
.chans b {
  font-size: 13.5px;
  font-weight: 600;
}
.chans i {
  font-style: normal;
  font-size: 11.5px;
  color: #8f8a80;
}

/* 登录方式列表 */
.meths {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.meth {
  position: relative;
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 12px;
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 11px;
  background: rgba(255, 255, 255, 0.03);
  color: inherit;
  font-family: inherit;
  text-align: left;
  cursor: pointer;
  transition: border-color 0.15s ease-out, background-color 0.15s ease-out;
}
.meth:hover {
  background: rgba(255, 255, 255, 0.06);
}
.meth.on {
  border-color: color-mix(in srgb, var(--acc) 55%, transparent);
  background: color-mix(in srgb, var(--acc) 8%, transparent);
}
.methico {
  display: grid;
  place-items: center;
  width: 32px;
  height: 32px;
  border-radius: 9px;
  background: rgba(255, 255, 255, 0.07);
  color: #a29d92;
}
.meth.on .methico {
  background: color-mix(in srgb, var(--acc) 18%, transparent);
  color: var(--acc);
}
.methico svg {
  width: 16px;
  height: 16px;
}
.mtxt2 {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 1px;
}
.mtxt2 b {
  font-size: 13.5px;
  font-weight: 500;
}
.mtxt2 i {
  font-style: normal;
  font-size: 11.5px;
  color: #8f8a80;
}
.methdot {
  width: 14px;
  height: 14px;
  border-radius: 50%;
  border: 1.5px solid rgba(255, 255, 255, 0.2);
}
.meth.on .methdot {
  border-color: var(--acc);
  background: radial-gradient(circle, var(--acc) 40%, transparent 45%);
}

/* 多开账号列表 */
.mlist {
  max-height: 200px;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.mrow {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 7px 10px;
  border-radius: 10px;
  cursor: pointer;
}
.mrow:hover {
  background: rgba(255, 255, 255, 0.05);
}
.mrow input {
  width: 15px;
  height: 15px;
  accent-color: var(--acc);
}
.ava {
  display: grid;
  place-items: center;
  width: 28px;
  height: 28px;
  border-radius: 8px;
  background: rgba(255, 255, 255, 0.08);
  font-size: 12px;
  color: #a29d92;
}
.mtxt {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
}
.mtxt b {
  font-size: 13.5px;
  font-weight: 500;
}
.mtxt i {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-style: normal;
  font-size: 11.5px;
  color: #8f8a80;
}
.mtxt .dot {
  width: 5px;
  height: 5px;
}
.mrow .pill {
  border-color: rgba(255, 255, 255, 0.14);
}

/* 排列方案 */
.lays {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.lay {
  position: relative;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 6px;
  width: 86px;
  padding: 12px 6px 9px;
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 11px;
  background: rgba(255, 255, 255, 0.03);
  color: inherit;
  font-family: inherit;
  cursor: pointer;
  transition: border-color 0.15s ease-out, background-color 0.15s ease-out;
}
.lay:hover {
  background: rgba(255, 255, 255, 0.06);
}
.lay.on {
  border-color: color-mix(in srgb, var(--acc) 55%, transparent);
  background: color-mix(in srgb, var(--acc) 9%, transparent);
}
.lay.dim {
  opacity: 0.4;
}
.recl {
  position: absolute;
  top: -8px;
  right: 6px;
  padding: 1px 7px;
  border-radius: 999px;
  background: var(--acc);
  color: var(--on);
  font-style: normal;
  font-size: 10.5px;
  font-weight: 600;
  line-height: 1.5;
}
.mini {
  display: grid;
  gap: 2px;
  width: 30px;
  height: 30px;
}
.mini i {
  border-radius: 2px;
  background: rgba(255, 255, 255, 0.12);
}
.mini i.fill {
  background: var(--acc);
}
.mini.none {
  display: grid;
  place-items: center;
}
.mini.none svg {
  width: 18px;
  height: 18px;
  color: #8f8a80;
}
.lay b {
  font-size: 11.5px;
  font-weight: 500;
  white-space: nowrap;
}
.lay i {
  font-style: normal;
  font-size: 10.5px;
  color: #6f6a60;
}

.sizerow {
  display: flex;
  align-items: center;
  gap: 8px;
}
.minput.size {
  width: 96px;
  height: 32px;
  margin: 0;
}
.sizex {
  color: #8f8a80;
  font-size: 12px;
}
.sizehint {
  flex: 1;
  min-width: 0;
  margin-left: 4px;
  font-size: 11px;
  line-height: 1.5;
  color: #6f6a60;
}
.mfoot {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 12px;
  margin-top: 18px;
}
.mfoot .mhint {
  margin: 0;
  flex: 1;
  font-size: 12px;
  color: #8f8a80;
}
.minput {
  width: 100%;
  height: 38px;
  margin-top: 10px;
  padding: 0 12px;
  border: 1px solid rgba(255, 255, 255, 0.14);
  border-radius: 9px;
  background: rgba(255, 255, 255, 0.04);
  color: inherit;
  font-family: inherit;
  font-size: 14px;
  outline: none;
}
.minput:focus {
  border-color: color-mix(in srgb, var(--acc) 60%, transparent);
}

.segs {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
}
.segs button {
  height: 28px;
  padding: 0 12px;
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 8px;
  background: transparent;
  color: #a29d92;
  font-family: inherit;
  font-size: 12px;
  cursor: pointer;
}
.segs button.on {
  border-color: rgba(227, 194, 138, 0.55);
  background: rgba(227, 194, 138, 0.12);
  color: #e3c28a;
}
.segs button:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}
.arbiter-toggle {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  font-size: 12.5px;
  color: #c9c6c0;
  cursor: pointer;
  white-space: nowrap;
}
.arbiter-toggle input {
  accent-color: #e3c28a;
}
.toasts {
  position: fixed;
  left: 50%;
  bottom: 22px;
  transform: translateX(-50%);
  z-index: 80;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  pointer-events: none;
  transition: bottom 0.2s ease-out;
}
.toasts.lifted {
  bottom: 84px;
}
.toast {
  padding: 9px 16px;
  border: 1px solid rgba(255, 255, 255, 0.14);
  border-radius: 999px;
  background: #1a191f;
  color: #f3f1ea;
  font-size: 13px;
  box-shadow: 0 12px 32px rgba(0, 0, 0, 0.4);
}
</style>
