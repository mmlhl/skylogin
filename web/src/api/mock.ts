/**
 * 预览用的假数据。
 *
 * 只在 URL 带 `?mock=1` 时启用（见 main.ts）。
 * 作用：改界面时不需要启动 Go 后端、不需要编译 exe、不会弹 UAC——
 * 直接 `npm run dev` 打开 http://localhost:5173/?mock=1 就能看效果。
 *
 * 注意：这里的数据仅用于看布局，不参与任何真实逻辑。
 */
import type { AccountView, AppState, GroupView } from "./types";
import { mockLayoutPlan } from "../demo/layoutPlan";

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
  extra: Partial<AccountView> = {},
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
            accountKey: "1100000000000001",
            name: "示例玩家",
            mode: "",
            fps: 0,
            display: "",
            disabled: false,
            x: 0,
            y: 0,
            w: 960,
            h: 540,
            hasRect: true,
          },
          {
            accountKey: "aedemo0000000000",
            name: "13800138000",
            mode: "LowPower",
            fps: 30,
            display: "",
            disabled: false,
            x: 960,
            y: 540,
            w: 960,
            h: 540,
            hasRect: true,
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
          {
            accountKey: "mock1",
            name: "归墟行者",
            mode: "",
            fps: 0,
            display: "",
            disabled: false,
            x: 0,
            y: 0,
            w: 0,
            h: 0,
            hasRect: false,
          },
          {
            accountKey: "mock2",
            name: "云野常客",
            mode: "",
            fps: 0,
            display: "",
            disabled: false,
            x: 0,
            y: 0,
            w: 0,
            h: 0,
            hasRect: false,
          },
          {
            accountKey: "mock3",
            name: "星野 · 第二区",
            mode: "",
            fps: 0,
            display: "",
            disabled: true,
            x: 0,
            y: 0,
            w: 0,
            h: 0,
            hasRect: false,
          },
          {
            accountKey: "已删除的键",
            name: "已删除的账号",
            missing: true,
            mode: "",
            fps: 0,
            display: "",
            disabled: false,
            x: 0,
            y: 0,
            w: 0,
            h: 0,
            hasRect: false,
          },
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
      mk(1, "归墟行者", "huawei", "华为渠道", ["小号", "待续期"], {
        credState: "expired",
      }),
      mk(2, "云野常客", "netease", "网易官方", ["大号", "朋友"]),
      mk(3, "星野 · 第二区", "netease", "网易官方", ["大号"], {
        copyKey: "aedemo0000000000-2",
      }),
      mk(4, "阿岚", "bilibili", "B站渠道", ["朋友"]),
      mk(5, "六月", "huawei", "华为渠道", ["小号"], { running: true }),
      mk(6, "雾里", "netease", "网易官方", ["待续期"]),
      mk(7, "沉船", "huawei", "华为渠道", []),
    ],
    game: "未启动",
    login: {
      status: "idle",
      message: "",
      qr: "",
      nick: "",
      player: "",
      expireAt: 0,
      attempt: 0,
      steps: [],
    },
    scan: {
      status: "idle",
      message: "",
      qr: "",
      channel: "",
      uuid: "",
      expireAt: 0,
    },
    settings: {
      skyExe: "D:\\FeverApps\\sky\\sky.exe",
      skyFound: true,
      skyAuto: true,
      deviceId: "amdemo0000000000-d",
      deviceIdSet: true,
      deviceIdSource: "fingerprint",
      deviceInfo: {
        fingerprint: "0123456789abcdef",
        uniqueId: "0123456789abcdef",
        hasKey: true,
        mode: "fingerprint",
      },
      closeLauncher: true,
      multiQualityMode: "",
      multiQualityFps: 0,
      mouseArbiter: true,
      mouseArbiterMin: 3,
      autoEnterPlay: true,
      multiAutoEnter: true,
      railWidth: 0,
      appBuild: "123",
      appBuildTime: "11-01 20:00",
      release: {
        github: "https://github.com/mmlhl/skylogin/releases/latest",
        pan: "https://example.com/pan",
      },
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
    gameUpdate: {
      checked: false,
      update_needed: false,
      local_version: "",
      latest_version: "",
    },
    version: "1.0.0",
  };
}

/** 当前假状态（截图脚本可以直接改它来模拟不同界面） */
export const mockState: AppState = fakeState();

/** 启动组的离线交互控制，供界面回归模拟慢请求与失败。 */
export const groupMock = {
  delay: 0,
  saveError: "",
  deleteError: "",
  launchError: "",
  screenW: 2560,
  screenH: 1440,
  planDelay: 0,
  planError: "",
  planDelays: {} as Record<number, number>,
  workArea: null as { x: number; y: number; w: number; h: number } | null,
  calls: [] as { path: string; body: unknown; at: number }[],
};
export const accountMock = {
  delay: 0, tagsError: "", reorderError: "", tagOrderError: "", selectError: "",
  stateDelay: 0, stateDelays: [] as number[],
  calls: [] as { path: string; body: unknown; at: number }[],
};
function normalizeMockTags(tags: string[], limit = 8): string[] {
  const clean: string[] = [], seen = new Set<string>();
  for (const raw of tags) {
    const tag = [...raw.trim()].slice(0, 16).join("");
    const key = tag.toLowerCase();
    if (!tag || seen.has(key)) continue;
    seen.add(key); clean.push(tag);
    if (clean.length >= limit) break;
  }
  return clean;
}

let groupSequence = 2;

/** 拦截 fetch，把 /api/* 全部就地应答 */
export function installMock() {
  const origin = window.fetch.bind(window);
  // 暴露出去，方便截图脚本切换状态（运行中 / 凭证过期 / 无账号）
  (window as unknown as { __mock: AppState }).__mock = mockState;
  (window as unknown as { __groupMock: typeof groupMock }).__groupMock =
    groupMock;
  (window as unknown as { __accountMock: typeof accountMock }).__accountMock = accountMock;
  window.fetch = async (input: RequestInfo | URL, init?: RequestInit) => {
    const url =
      typeof input === "string"
        ? input
        : input instanceof URL
          ? input.toString()
          : input.url;
    if (!url.includes("/api/")) return origin(input as RequestInfo, init);

    // 状态接口：返回假数据（每次给新对象，模拟真实刷新）
    if (url.includes("/api/state")) {
      // 深拷贝一份，模拟真实的"每次都是新数据"
      const snapshot = JSON.parse(JSON.stringify(mockState));
      const delay = accountMock.stateDelays.shift() ?? accountMock.stateDelay;
      if (delay) await new Promise((resolve) => setTimeout(resolve, delay));
      return json(snapshot);
    }
    if (url.includes("/api/accounts/select")) {
      const id = new URL(url, window.location.href).searchParams.get("id");
      const error = accountMock.selectError;
      accountMock.calls.push({ path: "/api/accounts/select", body: { id }, at: Date.now() });
      if (accountMock.delay) await new Promise((resolve) => setTimeout(resolve, accountMock.delay));
      if (error) return json({ ok: false, message: error });
      if (!mockState.accounts.some((a) => a.id === id)) return json({ ok: false, message: "找不到这个账号" });
      for (const a of mockState.accounts) a.current = a.id === id;
      return json({ ok: true });
    }
    if (/\/api\/accounts\/(tags|reorder|tag-order)(?:[?#]|$)/.test(url)) {
      const path = new URL(url, window.location.href).pathname;
      const body = typeof init?.body === "string" ? JSON.parse(init.body) : {};
      accountMock.calls.push({ path, body: JSON.parse(JSON.stringify(body)), at: Date.now() });
      const error = path.endsWith("/tags") ? accountMock.tagsError : path.endsWith("/reorder") ? accountMock.reorderError : accountMock.tagOrderError;
      if (accountMock.delay) await new Promise((resolve) => setTimeout(resolve, accountMock.delay));
      if (error) return json({ ok: false, message: error });
      if (path.endsWith("/tags")) {
        const a = mockState.accounts.find((a) => a.id === body.id);
        if (!a) return json({ ok: false, message: "找不到这个账号" });
        a.tags = normalizeMockTags(body.tags ?? []);
        return json({ ok: true, account: { id: a.id, tags: a.tags } });
      }
      if (path.endsWith("/tag-order")) {
        const tagOrder = normalizeMockTags(body.tags ?? [], Infinity);
        mockState.settings.accountTagOrder = tagOrder;
        return json({ ok: true, tagOrder });
      }
      if (!Array.isArray(body.ids) || !body.ids.length) return json({ ok: false, message: "缺少顺序" });
      const ranks = new Map((body.ids as string[]).filter((id: string, i: number, ids: string[]) => ids.indexOf(id) === i).map((id: string, i: number) => [id, i]));
      mockState.accounts.sort((a, b) => (ranks.get(a.id) ?? body.ids.length) - (ranks.get(b.id) ?? body.ids.length));
      return json({ ok: true, ids: mockState.accounts.map((a) => a.id) });
    }
    if (url.includes("/api/layout/plan")) {
      const body = typeof init?.body === "string" ? JSON.parse(init.body) : {};
      groupMock.calls.push({
        path: "/api/layout/plan",
        body: { ...body },
        at: Date.now(),
      });
      const delay = groupMock.planDelays[body.count] ?? groupMock.planDelay;
      if (delay) await new Promise((resolve) => setTimeout(resolve, delay));
      if (groupMock.planError)
        return json({ ok: false, message: groupMock.planError });
      return json(
        mockLayoutPlan(
          body,
          groupMock.workArea ?? {
            x: 0,
            y: 0,
            w: groupMock.screenW,
            h: groupMock.screenH - 50,
          },
          groupMock.screenW,
          groupMock.screenH,
        ),
      );
    }
    if (url.includes("/api/groups/") || url.includes("/api/clients/rects")) {
      const path = new URL(url, window.location.href).pathname;
      const body = typeof init?.body === "string" ? JSON.parse(init.body) : {};
      groupMock.calls.push({
        path,
        body: JSON.parse(JSON.stringify(body)),
        at: Date.now(),
      });
      if (groupMock.delay > 0)
        await new Promise((resolve) => setTimeout(resolve, groupMock.delay));
      if (path.endsWith("/groups/save")) {
        if (groupMock.saveError)
          return json({ ok: false, message: groupMock.saveError });
        const value = body as GroupView;
        const saved = JSON.parse(
          JSON.stringify({
            ...value,
            id: value.id || `g${++groupSequence}`,
            gapSec: 0,
            updated: "刚刚",
          }),
        ) as GroupView;
        saved.slots = saved.slots.map((s) => ({
          ...s,
          name:
            mockState.accounts.find((a) => a.id === s.accountKey)?.name ||
            s.name,
        }));
        const groups = mockState.groups ?? (mockState.groups = []);
        const at = groups.findIndex((g) => g.id === saved.id);
        if (at >= 0) groups[at] = saved;
        else groups.push(saved);
        return json({ ok: true, id: saved.id, groups });
      }
      if (path.endsWith("/groups/delete")) {
        if (groupMock.deleteError)
          return json({ ok: false, message: groupMock.deleteError });
        mockState.groups = (mockState.groups ?? []).filter(
          (g) => g.id !== body.id,
        );
        return json({ ok: true, groups: mockState.groups });
      }
      if (path.endsWith("/groups/launch")) {
        if (groupMock.launchError)
          return json({ ok: false, message: groupMock.launchError });
        const g = mockState.groups?.find((x) => x.id === body.id);
        if (!g) return json({ ok: false, message: "这套启动组不存在" });
        const enabled = new Set(
          g.slots.filter((s) => !s.disabled).map((s) => s.accountKey),
        );
        if (g.stopFirst) for (const a of mockState.accounts) a.running = false;
        for (const a of mockState.accounts)
          if (enabled.has(a.id)) a.running = true;
        return json({
          ok: true,
          message: "预览模式：已模拟启动组操作，不会启动真实游戏",
        });
      }
      return json({
        ok: true,
        screenW: groupMock.screenW,
        screenH: groupMock.screenH,
        workArea: groupMock.workArea ?? {
          x: 0,
          y: 0,
          w: groupMock.screenW,
          h: Math.max(1, groupMock.screenH - 40),
        },
        rects: mockState.accounts
          .filter((a) => a.running)
          .map((a, i) => ({
            id: a.id,
            name: a.name,
            x: (i % 2) * 960,
            y: Math.floor(i / 2) * 540,
            w: 960,
            h: 540,
            fullscreen: false,
            zOrder: i + 1,
          })),
      });
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
