<script setup lang="ts">
/**
 * 共享弹层 + 吐司：添加账号 / 多开（含窗口排列方案）/ 改名。
 * 四种界面壳共用这一层；强调色跟随当前壳（--demo-acc，由 DesignDemo 注入）。
 */
import { computed, ref, watch } from "vue";
import {
  X,
  QrCode,
  MessageSquare,
  KeyRound,
  LayoutGrid,
} from "lucide-vue-next";
import type { LauncherStore } from "./contract";
import LaunchGroups from "../components/LaunchGroups.vue";
import { demoUi } from "./ui";
import { channelLabel, fmtRun, layoutLabel } from "./contract";
import { useLayoutPlan } from "../composables/useLayoutPlan";

const props = defineProps<{ store: LauncherStore }>();
const store = props.store;

/* ---- 添加账号 ---- */
const addChannel = ref<"huawei" | "netease">("huawei");
const addMethod = ref<"qrcode" | "sms" | "mpay">("qrcode");
watch(addChannel, (c) => {
  if (c === "netease" && addMethod.value !== "qrcode")
    addMethod.value = "qrcode";
});
type AddMethod = "qrcode" | "sms" | "mpay";
// 渠道与方式的对应关系和真实后端一致：华为只有扫码；短信 / 密码窗口都是网易的
const methods = computed<
  { v: AddMethod; label: string; desc: string; icon: unknown }[]
>(() =>
  addChannel.value === "huawei"
    ? [
        {
          v: "qrcode",
          label: "扫码登录",
          desc: "手机华为帐号扫码",
          icon: QrCode,
        },
      ]
    : [
        {
          v: "qrcode",
          label: "扫码登录",
          desc: "用光遇手游扫码",
          icon: QrCode,
        },
        {
          v: "sms",
          label: "手机号登录",
          desc: "手机号 + 短信验证码",
          icon: MessageSquare,
        },
        {
          v: "mpay",
          label: "账号密码登录",
          desc: "账号 / 手机号 + 密码",
          icon: KeyRound,
        },
      ],
);
function confirmAdd() {
  store.addAccount({ channel: addChannel.value });
  demoUi.addOpen = false;
}

/* ---- 多开 ---- */
const picked = ref<string[]>([]);
const chosenMode = ref("");
const submitting = ref(false);
const multiError = ref("");
const previewAccounts = computed(() =>
  store.accounts.filter((a) => picked.value.includes(a.id) && !a.running),
);
const layoutRequest = computed(() => ({
  count: demoUi.multiOpen ? previewAccounts.value.length : 0,
  mode: chosenMode.value,
}));
const planner = useLayoutPlan(store, layoutRequest);
const layoutPlan = planner.result;
const planLoading = planner.loading;
const planError = planner.error;
const shownName = (a: (typeof store.accounts)[number]) =>
  store.maskAll ? a.maskedName : a.name;
function previewRectStyle(r: {
  x: number;
  y: number;
  w: number;
  h: number;
  zOrder?: number;
}) {
  const p = layoutPlan.value;
  return p
    ? {
        left: `${(r.x / p.screenW) * 100}%`,
        top: `${(r.y / p.screenH) * 100}%`,
        width: `${(r.w / p.screenW) * 100}%`,
        height: `${(r.h / p.screenH) * 100}%`,
        zIndex: r.zOrder || 1,
      }
    : {};
}
/* 画质 / 帧率 / 鼠标仲裁：和设置面板里的「多开默认」是同一份 */
const qMode = ref("");
const qFps = ref(0);
const arbiterEnabled = ref(true);
const arbiterMinText = ref("3");
// 多开时是否自动点播放按钮（存到全局设置，账号详情里可单独覆盖）
const autoEnterMulti = ref(true);
// 每个窗口的点击计时档位（和设置面板共用同一份设置）
const autoEnterSlotMs = ref(10000);
const autoEnterSlotOptions = [5000, 10000, 20000, 30000];
const autoEnterSlotSec = computed(() => Math.round(autoEnterSlotMs.value / 1000));
const arbiterMin = computed(() =>
  Math.min(60, Math.max(1, Math.round(Number(arbiterMinText.value) || 3))),
);
watch(
  () => demoUi.multiOpen,
  (open) => {
    if (!open) return;
    picked.value = demoUi.multiPreset.length
      ? store.accounts
          .filter((a) => demoUi.multiPreset.includes(a.id))
          .map((a) => a.id)
      : store.accounts
          .filter((a) => a.current || (!a.running && a.cred === "ok"))
          .map((a) => a.id);
    chosenMode.value = "";
    multiError.value = "";
    const d = store.multiDefaults();
    qMode.value = d.qualityMode;
    qFps.value = d.fps;
    arbiterEnabled.value = d.arbiter;
    arbiterMinText.value = String(d.arbiterMin > 0 ? d.arbiterMin : 3);
    autoEnterMulti.value = d.autoEnter ?? true;
    autoEnterSlotMs.value = d.autoEnterSlotMs || 10000;
  },
);
function isRunning(id: string) {
  return store.accounts.find((a) => a.id === id)?.running ?? false;
}
const toLaunch = computed(() => previewAccounts.value.length);
function togglePick(id: string) {
  const i = picked.value.indexOf(id);
  if (i >= 0) picked.value.splice(i, 1);
  else picked.value.push(id);
}
// 档位名与游戏内一致（2026-10-29 逐档实测）：四档画质 + 30/60/120 帧。
// 第一项是"账号配置"：用每个账号自己的画质/帧率（账号详情里设置），
// 没配过的账号跟随游戏；其余档位是本次多开对所有客户端的硬覆盖。
const qualityOpts = [
  {
    v: "",
    label: "跟随游戏",
    tip: "按账号在「游戏设置」里的配置，没设则以游戏内为准",
  },
  { v: "LowPower", label: "省电", tip: "游戏内部档位 LowPower" },
  { v: "Default", label: "默认", tip: "游戏内部档位 Default" },
  { v: "HighQuality", label: "高画质", tip: "游戏内部档位 HighQuality" },
  { v: "Presentation", label: "最高", tip: "游戏内部档位 Presentation" },
];
const fpsOpts = [
  { v: 0, label: "账号配置", tip: "用每个账号自己的帧率设置" },
  { v: 30, label: "30" },
  { v: 60, label: "60" },
  { v: 120, label: "120" },
];
function pickQuality(v: string) {
  // 画质/帧率是"本次多开的覆盖"，不写全局配置：账号各自的有专门入口
  qMode.value = v;
}
function pickFps(v: number) {
  qFps.value = v;
}
function pickArbiter(v: boolean) {
  arbiterEnabled.value = v;
  store.saveMultiDefaults("", 0, v, arbiterMin.value);
}
function pickArbiterMin() {
  arbiterMinText.value = String(arbiterMin.value);
  store.saveMultiDefaults("", 0, arbiterEnabled.value, arbiterMin.value);
}
// 自动进入：每个窗口单独计时，轮到它才开始算；排队等待不计时
function pickAutoEnterMulti(v: boolean) {
  autoEnterMulti.value = v;
  store.saveMultiDefaults(
    "",
    0,
    arbiterEnabled.value,
    arbiterMin.value,
    v,
    autoEnterSlotMs.value,
  );
}
function pickAutoEnterSlot(ms: number) {
  autoEnterSlotMs.value = ms;
  store.saveMultiDefaults(
    "",
    0,
    arbiterEnabled.value,
    arbiterMin.value,
    autoEnterMulti.value,
    ms,
  );
}

async function confirmMulti() {
  if (!toLaunch.value || submitting.value) return;
  submitting.value = true;
  multiError.value = "";
  try {
    const plan = await planner.ensure();
    const ordered = previewAccounts.value.map((a) => a.id);
    if (!ordered.length) throw new Error("没有待启动账号");
    store.multiLaunch(
      ordered,
      undefined,
      undefined,
      plan.mode,
      qMode.value,
      qFps.value,
    );
    demoUi.multiOpen = false;
  } catch (e) {
    multiError.value = e instanceof Error ? e.message : "窗口规划失败，请重试";
  } finally {
    submitting.value = false;
  }
}

/* ---- 创建副本 ---- */
const dupName = ref("");
// 副本自己的渠道：空 = 先不选（保留游戏里手选的旧行为）。
// 只有网易官方账号能选——渠道服账号的渠道由渠道 SDK 决定。
const dupRegion = ref("");
const dupAccount = computed(() =>
  store.accounts.find((a) => a.id === demoUi.dupId),
);
const dupCanPickRegion = computed(
  () => dupAccount.value?.channel === "netease",
);
// 已有副本：同 PlayerID 且带副本键的记录（创建第 2 条起在弹窗里提醒）
const dupExisting = computed(() =>
  dupAccount.value
    ? store.accounts.filter(
        (a) => a.playerId === dupAccount.value!.playerId && a.copyKey,
      )
    : [],
);
const dupDefaultName = computed(
  () => `${dupAccount.value?.name ?? ""}-副本${dupExisting.value.length + 1}`,
);
watch(
  () => demoUi.dupId,
  () => {
    if (demoUi.dupId) {
      dupName.value = dupDefaultName.value;
      dupRegion.value = "";
    }
  },
);
function confirmDup() {
  const name = dupName.value.trim();
  if (!demoUi.dupId) return;
  store.duplicate(demoUi.dupId, name, dupRegion.value);
  demoUi.dupId = "";
}

/* ---- 游戏设置（账号级画质/帧率/显示方式，四种壳共用这一个弹窗） ---- */
const qAccount = computed(() =>
  store.accounts.find((a) => a.id === demoUi.qualityId),
);
const qModeRow = ref("");
const qFpsRow = ref(0);
const qDisplayRow = ref("");
// 自动进入：只有开关两态，默认开（没有"跟随"这回事）
const qAutoEnterRow = ref(true);
// 三项都保留「跟随游戏」：没设过的号不该被我们按一个默认档位钉死
// （以前默认落到 Default/60，等于用户点一下保存就把游戏里的最高画质降级了）
const accountQualityOpts = qualityOpts;
const accountFpsOpts = [
  { v: 0, label: "跟随游戏", tip: "不写帧率，用游戏里的设置" },
  { v: 30, label: "30" },
  { v: 60, label: "60" },
  { v: 120, label: "120" },
];
const displayOpts = [
  { v: "", label: "跟随游戏", tip: "不动游戏的窗口 / 全屏状态" },
  { v: "windowed", label: "窗口", tip: "启动前写成窗口模式" },
  { v: "fullscreen", label: "全屏", tip: "启动前写成全屏" },
];
watch(
  () => demoUi.qualityId,
  () => {
    qModeRow.value = qAccount.value?.qualityMode ?? "";
    qFpsRow.value = qAccount.value?.qualityFps ?? 0;
    qDisplayRow.value = qAccount.value?.displayMode ?? "";
    qAutoEnterRow.value = qAccount.value?.autoEnter !== false;
  },
);
function confirmQuality() {
  if (!demoUi.qualityId) return;
  store.setAccountQuality(
    demoUi.qualityId,
    qModeRow.value,
    qFpsRow.value,
    qDisplayRow.value,
    qAutoEnterRow.value,
  );
  demoUi.qualityId = "";
}

/* ---- 改名 ---- */
const renameText = ref("");
watch(
  () => demoUi.renameId,
  (id) => {
    renameText.value = store.accounts.find((a) => a.id === id)?.name ?? "";
  },
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
        <button
          :class="{ on: addChannel === 'huawei' }"
          @click="addChannel = 'huawei'"
        >
          <b>华为渠道</b>
          <i>手机扫码</i>
        </button>
        <button
          :class="{ on: addChannel === 'netease' }"
          @click="addChannel = 'netease'"
        >
          <b>网易官方</b>
          <i>官网网页扫码</i>
        </button>
      </div>

      <p class="mlabel">登录方式</p>
      <div class="meths">
        <button
          v-for="m in methods"
          :key="m.v"
          class="meth"
          :class="{ on: addMethod === m.v }"
          @click="addMethod = m.v"
        >
          <span class="methico"><component :is="m.icon" /></span>
          <span class="mtxt2">
            <b>{{ m.label }}</b>
            <i>{{ m.desc }}</i>
          </span>
          <span class="methdot" />
        </button>
      </div>

      <p class="mhint">
        演示环境不弹真实二维码，点击「模拟扫码成功」完成添加。
      </p>
      <div class="mfoot">
        <button class="btn primary" @click="confirmAdd">模拟扫码成功</button>
      </div>
    </div>
  </div>

  <!-- 多开 -->
  <div
    v-if="demoUi.multiOpen"
    class="mask"
    @click.self="demoUi.multiOpen = false"
  >
    <div class="modal wide">
      <div class="mhead">
        <b
          >多开<em v-if="toLaunch" class="mcount"
            >{{ toLaunch }} 个待启动</em
          ></b
        >
        <button class="mclose" @click="demoUi.multiOpen = false"><X /></button>
      </div>

      <p class="mlabel">选择账号</p>
      <div class="mlist">
        <label v-for="a in store.accounts" :key="a.id" class="mrow">
          <input
            type="checkbox"
            :checked="picked.includes(a.id)"
            @change="togglePick(a.id)"
          />
          <span class="ava">{{ shownName(a).slice(0, 1) }}</span>
          <span class="mtxt">
            <b>{{ shownName(a) }}</b>
            <i>
              <em v-if="a.running" class="dot ok live" />
              <em v-else-if="a.cred === 'expired'" class="dot bad" />
              {{
                a.running
                  ? "运行中 · " + fmtRun(a.runSeconds)
                  : a.cred === "expired"
                    ? "凭证过期（将自动刷新）"
                    : "就绪"
              }}
            </i>
          </span>
          <span class="pill">{{ channelLabel(a.channel) }}</span>
        </label>
      </div>

      <p class="mlabel">
        窗口安排<em class="mrec">按实际可用桌面和游戏最小窗口推荐</em>
      </p>
      <div class="layout-choices">
        <button
          v-for="option in layoutPlan?.options || []"
          :key="option.id"
          :class="{ on: layoutPlan?.mode === option.id }"
          :disabled="!option.enabled || submitting"
          :title="option.reason || option.description"
          @click="chosenMode = option.id"
        >
          <b
            >{{ option.label
            }}<em v-if="layoutPlan?.recommendedMode === option.id">推荐</em></b
          ><span>{{ option.description }}</span
          ><small v-if="!option.enabled">{{ option.reason }}</small>
        </button>
      </div>
      <p v-if="planLoading" class="mhint" role="status">正在更新窗口安排…</p>
      <p v-if="planError || multiError" class="plan-warning" role="alert">
        {{ planError || multiError }}
        <button @click="planner.retry">重试</button>
      </p>
      <p v-else-if="layoutPlan?.reason" class="plan-warning">
        {{ layoutPlan.reason }}
      </p>
      <div v-if="layoutPlan && !planLoading" class="multi-preview-row">
        <div
          class="multi-canvas"
          :style="{
            aspectRatio: layoutPlan.screenW + '/' + layoutPlan.screenH,
          }"
        >
          <div
            v-for="(r, i) in layoutPlan.rects"
            :key="i"
            class="multi-window"
            :style="previewRectStyle(r)"
            :title="
              (previewAccounts[i] ? shownName(previewAccounts[i]) : '') +
              ' · 层级 ' +
              (r.zOrder || i + 1)
            "
          >
            <b>{{ i + 1 }}</b>
          </div>
          <p v-if="layoutPlan.mode === 'fullscreen'">
            专注操作一个账号<br />其他账号后台运行
          </p>
        </div>
        <p class="mhint">
          可用桌面 {{ layoutPlan.workArea.w }}×{{ layoutPlan.workArea.h
          }}<br />最小窗口 {{ layoutPlan.minW }}×{{ layoutPlan.minH }}<br />{{
            toLaunch === 1
              ? "单窗口居中，不强行铺满。"
              : "数字为启动顺序；遮挡时用监控墙切换。"
          }}
        </p>
      </div>
      <p v-if="toLaunch >= 5" class="mhint">
        账号较多时，标题与画面无法同时全部展开，建议配合监控墙使用。
      </p>

      <p class="mlabel">
        画质<em class="mrec">启动前写进每个客户端，也是默认值</em>
      </p>
      <div class="segs">
        <button
          v-for="o in qualityOpts"
          :key="'q' + o.v"
          :class="{ on: qMode === o.v }"
          :title="o.tip"
          @click="pickQuality(o.v)"
        >
          {{ o.label }}
        </button>
      </div>
      <p class="mlabel">帧率</p>
      <div class="segs">
        <button
          v-for="o in fpsOpts"
          :key="'f' + o.v"
          :class="{ on: qFps === o.v }"
          @click="pickFps(o.v)"
        >
          {{ o.label }}
        </button>
      </div>

      <p class="mlabel">
        鼠标仲裁<em class="mrec"
          >多开启动后的
          {{
            arbiterEnabled ? arbiterMin + " 分钟内" : "——已关"
          }}，自动释放后台客户端抢走的鼠标</em
        >
      </p>
      <div class="sizerow">
        <label class="form-check arbiter-toggle">
          <input
            type="checkbox"
            :checked="arbiterEnabled"
            @change="pickArbiter(!arbiterEnabled)"
          />
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

      <p class="mlabel">
        自动进入<em class="mrec"
          >等这批窗口排好、监控墙打开、所有客户端都发完票之后才开始。每个窗口单独计时：从轮到它开始算
          {{ autoEnterSlotSec }} 秒，期间识别播放按钮并点一次；等别的窗口的时间不计入</em
        >
      </p>
      <div class="sizerow">
        <label class="form-check">
          <input
            type="checkbox"
            :checked="autoEnterMulti"
            @change="pickAutoEnterMulti(!autoEnterMulti)"
          />
          <span>多开时自动点进游戏</span>
        </label>
      </div>
      <div class="sizerow" v-if="autoEnterMulti">
        <span class="sizehint">每窗口计时</span>
        <button
          v-for="ms in autoEnterSlotOptions"
          :key="ms"
          class="btn"
          :class="{ primary: autoEnterSlotMs === ms }"
          @click="pickAutoEnterSlot(ms)"
        >
          {{ ms / 1000 }}s
        </button>
      </div>

      <div class="mfoot">
        <span class="mhint">第 2 个起从镜像目录启动，不占额外磁盘</span>
        <button
          class="btn primary"
          :disabled="!toLaunch || submitting"
          @click="confirmMulti"
        >
          {{
            submitting
              ? "正在确认布局…"
              : `一起启动（${toLaunch} · ${layoutPlan ? layoutLabel(layoutPlan.mode) : "等待规划"}）`
          }}
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
      <input
        v-model="renameText"
        class="minput"
        maxlength="24"
        placeholder="最多 24 个字"
        @keyup.enter="confirmRename"
      />
      <div class="mfoot">
        <button class="btn primary" @click="confirmRename">保存</button>
      </div>
    </div>
  </div>

  <!-- 创建副本：默认名可改；已有副本时先提醒 -->
  <div v-if="demoUi.dupId" class="mask" @click.self="demoUi.dupId = ''">
    <div class="modal slim">
      <div class="mhead">
        <b>创建副本</b>
        <button class="mclose" @click="demoUi.dupId = ''"><X /></button>
      </div>
      <p class="mhint">
        为「{{
          dupAccount?.name
        }}」创建一条副本记录；副本与原记录共用凭证，游戏内互相独立。
      </p>
      <input
        v-model="dupName"
        class="minput"
        maxlength="24"
        placeholder="副本名称"
        @keyup.enter="confirmDup"
      />
      <!-- 副本可以单独选区：同一账号在安卓、iOS 各玩一个角色 -->
      <div v-if="dupCanPickRegion" class="qsec">
        <div class="qlabel2">渠道</div>
        <div class="qpills">
          <button :class="{ on: dupRegion === '' }" @click="dupRegion = ''">
            先不选
          </button>
          <button
            :class="{ on: dupRegion === 'android' }"
            @click="dupRegion = 'android'"
          >
            安卓
          </button>
          <button
            :class="{ on: dupRegion === 'ios' }"
            @click="dupRegion = 'ios'"
          >
            iOS
          </button>
        </div>
        <p class="qnote">选了渠道后，启动这个副本时游戏不会再问选哪个区。</p>
      </div>
      <p v-if="dupExisting.length" class="dupwarn">
        该账号已创建过 {{ dupExisting.length }} 条副本（{{
          dupExisting.map((a) => a.name).join("、")
        }}）。可以继续创建多条，确认继续吗？
      </p>
      <div class="mfoot">
        <button class="btn" @click="demoUi.dupId = ''">取消</button>
        <button class="btn primary" @click="confirmDup">
          {{ dupExisting.length ? "继续创建" : "创建" }}
        </button>
      </div>
    </div>
  </div>

  <!-- 游戏设置：账号级画质/帧率 -->
  <div v-if="demoUi.qualityId" class="mask" @click.self="demoUi.qualityId = ''">
    <div class="modal slim">
      <div class="mhead">
        <b>游戏设置</b>
        <button class="mclose" @click="demoUi.qualityId = ''"><X /></button>
      </div>
      <p class="qwho">
        <span class="qava">{{ (qAccount?.name || "?").slice(0, 1) }}</span>
        {{ qAccount?.name }}
      </p>

      <div class="qsec">
        <div class="qlabel2">画质</div>
        <div class="qpills">
          <button
            v-for="o in accountQualityOpts"
            :key="'qq' + o.v"
            :class="{ on: qModeRow === o.v }"
            :title="o.tip"
            @click="qModeRow = o.v"
          >
            {{ o.label }}
          </button>
        </div>
      </div>
      <div class="qsec">
        <div class="qlabel2">帧率</div>
        <div class="qpills">
          <button
            v-for="o in accountFpsOpts"
            :key="'qf' + o.v"
            :class="{ on: qFpsRow === o.v }"
            @click="qFpsRow = o.v"
          >
            {{ o.label }}
          </button>
        </div>
      </div>
      <div class="qsec">
        <div class="qlabel2">显示</div>
        <div class="qpills">
          <button
            v-for="o in displayOpts"
            :key="'qd' + o.v"
            :class="{ on: qDisplayRow === o.v }"
            :title="o.tip"
            @click="qDisplayRow = o.v"
          >
            {{ o.label }}
          </button>
        </div>
      </div>
      <div class="qsec">
        <div class="qlabel2">自动进入</div>
        <label class="form-check">
          <input
            type="checkbox"
            :checked="qAutoEnterRow"
            @change="
              qAutoEnterRow = ($event.target as HTMLInputElement).checked
            "
          />
          <span>拿到票后自动点进游戏（默认开）</span>
        </label>
      </div>

      <p class="qnote">
        启动前写进这台客户端（单开直接写游戏目录，多开写每个镜像）。
        「显示」只对单开生效：多开的窗口 / 全屏由多开弹窗统一决定。
      </p>
      <div class="mfoot">
        <button class="btn primary" @click="confirmQuality">保存</button>
      </div>
    </div>
  </div>

  <!-- 启动组：整屏面板（不是小弹窗）。它是一套配置编辑器，要摆表格和画布，
       塞进 400 宽的弹层里会变成两列都读不清 -->
  <LaunchGroups
    v-if="demoUi.groupsOpen"
    :store="store"
    @close="demoUi.closeGroups()"
  />

  <!-- 吐司：有批量操作条时往上抬 -->
  <div class="toasts" :class="{ lifted: store.selectedIds.length }">
    <div v-for="t in store.toasts" :key="t.id" class="toast">{{ t.text }}</div>
  </div>
</template>

<style scoped>
.layout-choices {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
}
.layout-choices button {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 12px;
  border: 1px solid rgba(255, 255, 255, 0.14);
  border-radius: 8px;
  text-align: left;
  background: transparent;
  color: #a5a29c;
  font: inherit;
  cursor: pointer;
}
.layout-choices button.on {
  border-color: #d8b47c;
  background: rgba(216, 180, 124, 0.07);
}
.layout-choices button:disabled {
  opacity: 0.5;
  cursor: default;
}
.layout-choices b {
  color: #f4f2ef;
  font-size: 13px;
}
.layout-choices span,
.layout-choices small {
  font-size: 12px;
  line-height: 1.5;
}
.layout-choices em {
  color: #d8b47c;
  font-size: 11px;
  font-style: normal;
  margin-left: 8px;
}
.multi-preview-row {
  display: flex;
  gap: 16px;
  align-items: center;
  margin-top: 12px;
}
.multi-canvas {
  position: relative;
  flex: none;
  width: 200px;
  background: #101012;
  border: 1px solid rgba(255, 255, 255, 0.14);
  overflow: hidden;
  border-radius: 4px;
}
.multi-window {
  position: absolute;
  border: 1px solid #a58c69;
  background: #29251f;
}
.multi-window b {
  display: block;
  padding: 1px 3px;
  font-size: 11px;
  color: #d8b47c;
  background: #393127;
}
.multi-canvas > p {
  position: absolute;
  inset: 0;
  display: grid;
  place-content: center;
  text-align: center;
  margin: 0;
  color: #a5a29c;
  font-size: 12px;
}
.plan-warning {
  color: #d49788;
  font-size: 12px;
  line-height: 1.5;
}

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
  transition:
    border-color 0.15s ease-out,
    background-color 0.15s ease-out;
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
  transition:
    border-color 0.15s ease-out,
    background-color 0.15s ease-out;
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
  transition:
    border-color 0.15s ease-out,
    background-color 0.15s ease-out;
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
.segs button.off {
  opacity: 0.35;
  cursor: not-allowed;
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
.schemes {
  display: flex;
  gap: 6px;
  margin: 2px 0 6px;
}
.qwho {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 2px 0 12px;
  font-weight: 500;
}
.qava {
  display: grid;
  place-items: center;
  width: 26px;
  height: 26px;
  border-radius: 8px;
  background: var(--s2, rgba(255, 255, 255, 0.08));
  font-size: 12px;
}
.qsec {
  margin-bottom: 12px;
}
.qlabel2 {
  font-size: 12px;
  color: var(--muted-foreground, #98a2ae);
  margin-bottom: 6px;
}
.qpills {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
.qpills button {
  padding: 5px 14px;
  border: 1px solid var(--border, rgba(255, 255, 255, 0.12));
  border-radius: 999px;
  background: transparent;
  color: var(--muted-foreground, #98a2ae);
  font-size: 12px;
  cursor: pointer;
  transition: all 0.12s;
}
.qpills button:hover {
  color: var(--foreground, #eef1f4);
  border-color: var(--muted-foreground, #98a2ae);
}
.qpills button.on {
  background: var(--primary, #d8b47c);
  border-color: var(--primary, #d8b47c);
  color: var(--primary-foreground, #161619);
  font-weight: 500;
}
.qnote {
  margin: 2px 0 10px;
  font-size: 11px;
  color: var(--muted-foreground, #98a2ae);
}
.dupwarn {
  margin: 4px 0 10px;
  padding: 8px 10px;
  border-radius: 8px;
  background: rgba(227, 194, 138, 0.08);
  border: 1px solid rgba(227, 194, 138, 0.3);
  font-size: 12px;
  color: #d8b47c;
}
.scheme {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 4px 9px;
  border: 1px solid var(--border, rgba(255, 255, 255, 0.12));
  border-radius: 8px;
  background: transparent;
  color: inherit;
  font-size: 12px;
  cursor: pointer;
}
.scheme svg {
  fill: none;
  stroke: currentColor;
  stroke-width: 1.2;
  opacity: 0.85;
}
.scheme svg rect {
  fill: currentColor;
  fill-opacity: 0.18;
}
.scheme.on {
  border-color: currentColor;
  opacity: 1;
}
.scheme:not(.on) {
  opacity: 0.55;
}
</style>
