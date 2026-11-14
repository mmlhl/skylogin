/**
 * 预览用的假数据。
 *
 * 只在 URL 带 `?mock=1` 时启用（见 main.ts）。
 * 作用：改界面时不需要启动 Go 后端、不需要编译 exe、不会弹 UAC——
 * 直接 `npm run dev` 打开 http://localhost:5173/?mock=1 就能看效果。
 *
 * 注意：这里的数据仅用于看布局，不参与任何真实逻辑。
 */
import type { AccountView, AppState } from "./types";

/** 假状态：两个账号、一个处于选中、设置填满 */
/**
 * 造一条预览账号：字段够界面用就行，不参与任何真实逻辑。
 * 为什么要有：只有两三个号时列表大片空白，看观感会被"空"误导——
 * 评审排版至少得有 8~10 行才判断得出密度。
 */
function mk(
  n: number,
  name: string,
  channel: AccountView["channel"],
  channelLabel: string,
  tags: string[],
  extra: Partial<AccountView> = {}
): AccountView {
  return {
    id: `mock${n}`,
    name,
    nick: name,
    channel,
    channelLabel,
    idLabel: "账号 ID",
    accountId: `mock-${n}`,
    playerId: `mock-${n}`,
    masked: name.slice(0, 1) + "***",
    level: String((n * 7) % 40),
    updated: "11-14 20:12",
    tags,
    current: false,
    createdAt: Math.round(Date.now() / 1000) - n * 6 * 86400,
    ...extra,
  };
}

function fakeState(): AppState {
  return {
    tags: [
      { name: "大号", count: 3 },
      { name: "朋友", count: 3 },
      { name: "小号", count: 3 },
      { name: "待续期", count: 2 },
    ],
    // 两套启动组：一套两开（记过位置），一套单开全屏（没记位置）。
    // 有这两套，预览才看得出"组列表 + 成员表 + 位置"是什么样。
    groups: [
      {
        id: "g1",
        name: "日常双开",
        layout: "tile",
        cols: 2,
        rows: 2,
        defMode: "HighQuality",
        defFps: 60,
        defDisplay: "windowed",
        stopFirst: true,
        autoWall: true,
        gapSec: 1,
        screenW: 1920,
        screenH: 1080,
        updated: "今天 21:04",
        slots: [
          {
            accountKey: "1100000000000001", name: "示例玩家", mode: "", fps: 0, display: "",
            disabled: false, x: 0, y: 0, w: 960, h: 540, hasRect: true,
          },
          {
            accountKey: "aedemo0000000000", name: "13800138000", mode: "LowPower", fps: 30, display: "",
            disabled: false, x: 960, y: 540, w: 960, h: 540, hasRect: true,
          },
        ],
      },
      {
        id: "g2",
        name: "四开练小号",
        layout: "cascade",
        cols: 2,
        rows: 2,
        defMode: "LowPower",
        defFps: 30,
        defDisplay: "",
        stopFirst: true,
        autoWall: true,
        gapSec: 2,
        screenW: 0,
        screenH: 0,
        updated: "昨天 22:31",
        slots: [
          { accountKey: "mock1", name: "归墟行者", mode: "", fps: 0, display: "", disabled: false, x: 0, y: 0, w: 0, h: 0, hasRect: false },
          { accountKey: "mock2", name: "云野常客", mode: "", fps: 0, display: "", disabled: false, x: 0, y: 0, w: 0, h: 0, hasRect: false },
          { accountKey: "mock3", name: "星野 · 第二区", mode: "", fps: 0, display: "", disabled: true, x: 0, y: 0, w: 0, h: 0, hasRect: false },
          { accountKey: "已删除的键", name: "已删除的账号", missing: true, mode: "", fps: 0, display: "", disabled: false, x: 0, y: 0, w: 0, h: 0, hasRect: false },
        ],
      },
    ],
    accounts: [
      {
        id: "1100000000000001",
        name: "示例玩家",
        tags: ["大号", "朋友"],
        nick: "示例玩家",
        channel: "huawei",
        channelLabel: "华为渠道",
        idLabel: "账号 ID",
        accountId: "1100000000000001",
        playerId: "1100000000000001",
        masked: "1100********0001",
        running: true,
        level: "7",
        updated: "10-24 17:50",
        current: true,
        // 详情页的登录环境 / 时间（假数据，只为看布局）
        loginMethod: "",
        source: "channel",
        gameId: "",
        mpayPath: "",
        mpayVersion: "",
        created: "2026-09-18 20:31",
        lastLoginText: "2026-11-05 20:58",
        tokenSaLabel: "",
        tokenAtText: "",
      },
      {
        id: "aedemo0000000000",
        name: "13800138000",
        nick: "13800138000",
        channel: "netease",
        channelLabel: "网易官方",
        idLabel: "账号 ID",
        accountId: "aedemo0000000000",
        playerId: "aedemo0000000000",
        masked: "aede********0000",
        level: "",
        updated: "10-24 17:50",
        current: false,
        // 网易账号：官方登录窗 + 会话级凭证，详情页能看到环境与时间
        loginMethod: "password",
        source: "mpaydll",
        gameId: "aecfs5n2y4aaaakd-g-ma75",
        mpayPath: "D:\\FeverApps\\sky\\mpay.dll",
        mpayVersion: "4.20.0",
        created: "2026-10-21 19:12",
        lastLoginText: "2026-11-05 21:04",
        tokenSaLabel: "会话级（可续期）",
        tokenAtText: "2026-11-05 21:04",
      },
      // 其余账号：只为把列表填到 9 行，覆盖各种状态（过期 / 运行中 / 副本 / 未分组）
      mk(1, "归墟行者", "huawei", "华为渠道", ["小号", "待续期"], { credState: "expired" }),
      mk(2, "云野常客", "netease", "网易官方", ["大号", "朋友"]),
      mk(3, "星野 · 第二区", "netease", "网易官方", ["大号"], { copyKey: "aedemo0000000000-2" }),
      mk(4, "阿岚", "bilibili", "B站渠道", ["朋友"]),
      mk(5, "六月", "huawei", "华为渠道", ["小号"], { running: true }),
      mk(6, "雾里", "netease", "网易官方", ["待续期"]),
      mk(7, "沉船", "huawei", "华为渠道", []),
    ],
    game: "未启动",
    login: { status: "idle", message: "", qr: "", nick: "", player: "", expireAt: 0, attempt: 0, steps: [] },
    scan: { status: "idle", message: "", qr: "", channel: "", uuid: "", expireAt: 0 },
    settings: {
      skyExe: "D:\\FeverApps\\sky\\sky.exe",
      skyFound: true,
      skyAuto: true,
      deviceId: "amdemo0000000000-d",
      deviceIdSet: true,
      deviceIdSource: "fingerprint",
      deviceInfo: { fingerprint: "0123456789abcdef", uniqueId: "0123456789abcdef", hasKey: true, mode: "fingerprint" },
      closeLauncher: true,
      multiQualityMode: "",
      multiQualityFps: 0,
  mouseArbiter: true,
  mouseArbiterMin: 3,
      railWidth: 0,
      appBuild: "123",
      appBuildTime: "11-01 20:00",
      release: { github: "https://github.com/mmlhl/skylogin/releases/latest", pan: "https://example.com/pan" },
      releaseAnnSeen: 0,
      updateDismissed: 0,
      updatePending: 0,
      maskTitles: true,
      mirrorDir: "D:\\SkyLoginMirror",
      mirrorDefault: "D:\\SkyLoginMirror",
      mirrorSameVolume: true,
      mirrorSizeText: "5.06 GB",
      dataDir: "C:\\Users\\you\\AppData\\Roaming\\SkyLogin",
    version: "1.0.0",
      buildTime: "01-02 15:04",
      repo: "https://github.com/mmlhl/skylogin",
      goVersion: "go1.27.1",
      protocol: "内置默认",
    mpayDebug: { dll: "game", game: "a50", skipLogout: false, dataPath: "" },
    },
    authHint: {},
    launchAlert: null,
    gameUpdate: { checked: false, update_needed: false, local_version: "", latest_version: "" },
    version: "1.0.0",
  };
}

/** 当前假状态（截图脚本可以直接改它来模拟不同界面） */
export const mockState: AppState = fakeState();

/** 拦截 fetch，把 /api/* 全部就地应答 */
export function installMock() {
  const origin = window.fetch.bind(window);
  // 暴露出去，方便截图脚本切换状态（运行中 / 凭证过期 / 无账号）
  (window as unknown as { __mock: AppState }).__mock = mockState;
  window.fetch = async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
    if (!url.includes("/api/")) return origin(input as RequestInfo, init);

    // 状态接口：返回假数据（每次给新对象，模拟真实刷新）
    if (url.includes("/api/state")) {
      // 深拷贝一份，模拟真实的"每次都是新数据"
      return json(JSON.parse(JSON.stringify(mockState)));
    }
    // 其它接口统一成功，避免点按钮报错
    return json({ ok: true });
  };
}

function json(data: unknown): Response {
  return new Response(JSON.stringify(data), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}

