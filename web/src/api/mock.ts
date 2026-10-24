/**
 * 预览用的假数据。
 *
 * 只在 URL 带 `?mock=1` 时启用（见 main.ts）。
 * 作用：改界面时不需要启动 Go 后端、不需要编译 exe、不会弹 UAC——
 * 直接 `npm run dev` 打开 http://localhost:5173/?mock=1 就能看效果。
 *
 * 注意：这里的数据仅用于看布局，不参与任何真实逻辑。
 */
import type { AppState } from "./types";

/** 假状态：两个账号、一个处于选中、设置填满 */
function fakeState(): AppState {
  return {
    accounts: [
      {
        id: "1100000000000001",
        name: "示例玩家",
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
      },
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
      railWidth: 0,
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
    },
    authHint: {},
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

