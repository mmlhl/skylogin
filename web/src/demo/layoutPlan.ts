import {
  canonicalLayout,
  type LayoutPlan,
  type LayoutRequest,
  type LayoutRect,
} from "../shellkit/contract";

/** 仅供离线演示；正式画布和启动始终采用 Go 规划接口的结果。 */
export function mockLayoutPlan(
  request: LayoutRequest,
  area = { x: 0, y: 0, w: 2560, h: 1390 },
  screenW = 2560,
  screenH = 1440,
): LayoutPlan {
  const count = Math.max(0, Math.floor(request.count));
  const minW = 1024,
    minH = 720;
  const cols = Math.floor(area.w / minW),
    rows = Math.floor(area.h / minH);
  const fits = count <= Math.max(0, cols * rows);
  const recommendedMode = count <= 2 && fits ? "tile" : "cascade:cols";
  const requested = request.mode
    ? canonicalLayout(request.mode)
    : recommendedMode;
  const feasible = requested !== "tile" || fits;
  const mode = feasible ? requested : "cascade:cols";
  const reason = feasible
    ? ""
    : `当前可用桌面 ${area.w}×${area.h} 无法并排放下 ${count} 个最小 ${minW}×${minH} 的窗口，已改为标题易切换。`;
  const rects: LayoutRect[] = [];
  if (mode !== "fullscreen") {
    for (let i = 0; i < count; i++) {
      let x = area.x,
        y = area.y;
      if (mode === "tile") {
        const c = Math.max(1, Math.min(cols, count));
        const usedRows = Math.ceil(count / c);
        x += Math.floor((area.w - c * minW) / 2) + (i % c) * minW;
        y +=
          Math.floor((area.h - usedRows * minH) / 2) + Math.floor(i / c) * minH;
      } else if (mode === "cascade:cols") {
        const split = Math.ceil(count / 2),
          col = i < split ? 0 : 1,
          index = col ? i - split : i;
        const group = col ? count - split : split;
        x +=
          count > 1
            ? col * Math.max(0, area.w - minW)
            : Math.floor((area.w - minW) / 2);
        y +=
          group > 1
            ? Math.round((index * Math.max(0, area.h - minH)) / (group - 1))
            : Math.floor((area.h - minH) / 2);
      } else {
        const split = Math.ceil(count / 2),
          row = i < split ? 0 : 1,
          index = row ? i - split : i;
        const group = row ? count - split : split;
        x +=
          group > 1
            ? Math.round((index * Math.max(0, area.w - minW)) / (group - 1))
            : Math.floor((area.w - minW) / 2);
        y += row * Math.max(0, area.h - minH);
      }
      rects.push({ x, y, w: minW, h: minH, zOrder: i + 1 });
    }
  }
  return {
    ok: true,
    mode,
    requestedMode: request.mode,
    recommendedMode,
    feasible,
    reason,
    message: reason,
    screenW,
    screenH,
    workArea: { ...area },
    minW,
    minH,
    minSource: "measured-default",
    rects,
    options: [
      {
        id: "tile",
        label: "窗口并排",
        description: "窗口互不遮挡；仅桌面容得下最小窗口时可用。",
        enabled: fits,
        reason: fits ? "" : `可用桌面最多并排 ${cols * rows} 个窗口`,
      },
      {
        id: "cascade:cols",
        label: "标题易切换",
        description: "左右分列、纵向错开，优先露出标题便于点选。",
        enabled: true,
      },
      {
        id: "cascade:rows",
        label: "画面多露出",
        description: "上下两行、横向摊开，尽量多露出游戏画面。",
        enabled: true,
      },
      {
        id: "fullscreen",
        label: "专注全屏",
        description: "当前操作一个账号，其他账号在后台，用监控墙切换。",
        enabled: true,
      },
    ],
  };
}
