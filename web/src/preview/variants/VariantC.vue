<script setup lang="ts">
/**
 * 风格 C · 光晕 3.0（保留发光与玻璃质感，重做布局与配色）
 *
 * 前一版的问题不在效果，在布局和配色：
 *   效果   —— 发光、玻璃模糊、氛围光都保留
 *   配色   —— 蓝紫光改成跟着主题色走（暖金/苔绿/银蓝…），底色偏暖不偏蓝
 *   布局   —— 右栏改成一张玻璃卡片：账号信息在上，设置在卡片底部，
 *             整块居中，不再让文字散落在空荡的背景上
 */
import { computed, ref } from "vue";
import { accounts } from "../data";
import { palettes, type Palette } from "../palettes";

const props = defineProps<{ palette?: Palette }>();

const p = computed(() => props.palette || palettes[0]);
const sheet = ref(false);
</script>

<template>
  <div
    class="wrap"
    :style="{
      '--bg': p.bg,
      '--panel': p.panel,
      '--raised': p.raised,
      '--text': p.text,
      '--dim': p.dim,
      '--faint': p.faint,
      '--accent': p.accent,
      '--on-accent': p.onAccent,
      '--line': p.hairline,
      '--line-strong': p.hairlineStrong,
    }"
  >
    <!-- 两层氛围光：只为主题色着色，不再用蓝紫 -->
    <div class="glow glow-a" />
    <div class="glow glow-b" />

    <aside class="rail">
      <p class="rail-label">账号</p>

      <div class="list">
        <button v-for="a in accounts" :key="a.id" class="card" :class="{ on: a.current }">
          <span class="avatar">{{ a.name.slice(0, 1) }}</span>
          <span class="txt">
            <b>{{ a.name }}</b>
            <i>{{ a.channel === "huawei" ? "华为账号" : "网易账号" }}</i>
          </span>
        </button>
      </div>

      <div class="adds">
        <button class="btn">添加渠道账号</button>
        <button class="btn accent">添加网易账号</button>
      </div>
    </aside>

    <main class="pane">
      <!-- 玻璃卡片：把账号信息和设置收进同一块，布局才有结构 -->
      <section class="glass">
        <p class="eyebrow">华为账号</p>
        <h1>示例玩家</h1>
        <p class="meta">账号 <span class="mono">1100000000000001</span> · 等级 7</p>

        <p class="status"><i />已登录，可直接启动</p>

        <button class="cta">启动光遇</button>

        <footer class="card-foot">
          <button class="foot-link" @click="sheet = true">设置</button>
          <span class="foot-sep" />
          <span class="foot-text">启动时自动关闭发烧平台</span>
        </footer>
      </section>
    </main>

    <div v-if="sheet" class="scrim" @click.self="sheet = false">
      <div class="sheet">
        <h2>设置</h2>
        <div class="srow"><span>游戏路径</span><code>D:\FeverApps\sky\sky.exe</code></div>
        <div class="srow"><span>登录设备</span><code>amdemo0000000000-d</code></div>
        <div class="srow"><span>启动前关闭发烧平台</span><em>已开启</em></div>
        <div class="srow"><span>版本</span><em>v1.0.0</em></div>
        <div class="sheet-foot"><button class="btn" @click="sheet = false">关闭</button></div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.wrap {
  position: relative;
  display: grid;
  grid-template-columns: 252px 1fr;
  height: 100%;
  background: var(--bg);
  color: var(--text);
  font-family: "Segoe UI Variable Text", "Segoe UI", "Microsoft YaHei UI", sans-serif;
  font-size: 14px;
  line-height: 1.5;
  -webkit-font-smoothing: antialiased;
  overflow: hidden;
}

/* ── 氛围光：两层，颜色跟随主题 ───────── */
.glow {
  position: absolute;
  pointer-events: none;
  filter: blur(2px);
}
.glow-a {
  inset: -30% 30% 30% -20%;
  background: radial-gradient(closest-side, color-mix(in srgb, var(--accent) 22%, transparent), transparent 70%);
}
.glow-b {
  inset: 40% -10% -20% 45%;
  background: radial-gradient(closest-side, color-mix(in srgb, var(--accent) 10%, transparent), transparent 70%);
  opacity: 0.7;
}

/* ── 左栏：玻璃 ──────────────────────── */
.rail {
  position: relative;
  display: flex;
  flex-direction: column;
  padding: 56px 16px 20px;
  background: color-mix(in srgb, var(--panel) 72%, transparent);
  border-right: 1px solid var(--line);
  backdrop-filter: blur(18px) saturate(130%);
}

.rail-label {
  margin: 0 0 12px 8px;
  font-size: 12px;
  color: var(--faint);
}

.list {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

/* 账号卡片：选中时金色描边 + 一圈很淡的光（保留原来的发光感） */
.card {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px;
  border: 1px solid transparent;
  border-radius: 12px;
  background: rgba(255, 255, 255, 0.035);
  color: inherit;
  font-family: inherit;
  text-align: left;
  cursor: pointer;
  transition: background 0.16s ease-out, border-color 0.16s ease-out, box-shadow 0.16s ease-out;
}
.card:hover {
  background: rgba(255, 255, 255, 0.06);
}
.card.on {
  border-color: color-mix(in srgb, var(--accent) 50%, transparent);
  background: color-mix(in srgb, var(--accent) 10%, transparent);
  box-shadow: 0 8px 26px -18px color-mix(in srgb, var(--accent) 90%, transparent),
    inset 0 1px 0 rgba(255, 255, 255, 0.08);
}

.avatar {
  display: grid;
  place-items: center;
  width: 30px;
  height: 30px;
  border-radius: 9px;
  background: rgba(255, 255, 255, 0.08);
  font-size: 12px;
  color: var(--dim);
  flex: none;
}
.card.on .avatar {
  background: color-mix(in srgb, var(--accent) 20%, transparent);
  color: var(--accent);
}

.txt {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.txt b {
  font-size: 14px;
  font-weight: 400;
  color: var(--text);
}
.txt i {
  font-style: normal;
  font-size: 13px;
  color: var(--faint);
}

.adds {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding-top: 16px;
  border-top: 1px solid var(--line);
}

/* 按钮：玻璃底 + 描边 */
.btn {
  height: 36px;
  border: 1px solid var(--line);
  border-radius: 9px;
  background: rgba(255, 255, 255, 0.03);
  color: var(--dim);
  font-family: inherit;
  font-size: 13px;
  text-align: center;
  cursor: pointer;
  transition: background 0.16s ease-out, border-color 0.16s ease-out, color 0.16s ease-out;
}
.btn:hover {
  background: rgba(255, 255, 255, 0.07);
  border-color: var(--line-strong);
  color: var(--text);
}
.btn.accent {
  color: var(--accent);
  border-color: color-mix(in srgb, var(--accent) 42%, transparent);
  background: color-mix(in srgb, var(--accent) 8%, transparent);
}
.btn.accent:hover {
  background: color-mix(in srgb, var(--accent) 16%, transparent);
  border-color: color-mix(in srgb, var(--accent) 70%, transparent);
}

/* ── 右栏：玻璃卡片 ──────────────────── */
.pane {
  position: relative;
  display: grid;
  place-items: center;
  padding: 40px;
}

.glass {
  width: 100%;
  max-width: 480px;
  padding: 32px;
  border-radius: 18px;
  border: 1px solid rgba(255, 255, 255, 0.1);
  background: rgba(255, 255, 255, 0.045);
  backdrop-filter: blur(24px) saturate(140%);
  box-shadow:
    0 32px 80px -40px rgba(0, 0, 0, 0.9),
    inset 0 1px 0 rgba(255, 255, 255, 0.09);
}

.eyebrow {
  margin: 0;
  font-size: 12px;
  letter-spacing: 0.02em;
  color: var(--faint);
}

h1 {
  margin: 10px 0 0;
  font-size: 40px;
  line-height: 1.1;
  font-weight: 600;
  letter-spacing: -0.025em;
}

.meta {
  margin: 10px 0 0;
  font-size: 13px;
  color: var(--dim);
}

.mono {
  font-family: Consolas, "Cascadia Mono", monospace;
  font-size: 11px;
  font-variant-numeric: tabular-nums;
}

.status {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 24px 0 0;
  font-size: 13px;
  color: var(--dim);
}
.status i {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--accent);
}

/* 主按钮：实心 + 一圈柔和外发光（这是原来"高级感"的来源，保留） */
.cta {
  width: 100%;
  height: 46px;
  margin-top: 24px;
  border: 0;
  border-radius: 11px;
  background: var(--accent);
  color: var(--on-accent);
  font-family: inherit;
  font-size: 14px;
  font-weight: 600;
  letter-spacing: -0.01em;
  cursor: pointer;
  box-shadow:
    0 14px 34px -16px color-mix(in srgb, var(--accent) 85%, transparent),
    inset 0 1px 0 rgba(255, 255, 255, 0.25);
  transition: filter 0.16s ease-out, transform 0.16s ease-out;
}
.cta:hover {
  filter: brightness(1.06);
}
.cta:active {
  transform: translateY(1px);
}
.cta:focus-visible {
  outline: 2px solid color-mix(in srgb, var(--accent) 55%, transparent);
  outline-offset: 3px;
}

/* 卡片底部：设置与一行说明，用细线与主体分开 */
.card-foot {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 24px;
  padding-top: 16px;
  border-top: 1px solid rgba(255, 255, 255, 0.08);
}

.foot-link {
  border: 0;
  background: transparent;
  color: var(--dim);
  font-family: inherit;
  font-size: 13px;
  cursor: pointer;
  padding: 0;
  transition: color 0.16s ease-out;
}
.foot-link:hover {
  color: var(--text);
}

.foot-sep {
  width: 1px;
  height: 12px;
  background: rgba(255, 255, 255, 0.12);
}

.foot-text {
  font-size: 12px;
  color: var(--faint);
}

/* ── 设置弹层（也是玻璃） ────────────── */
.scrim {
  position: absolute;
  inset: 0;
  display: grid;
  place-items: center;
  background: rgba(0, 0, 0, 0.45);
  backdrop-filter: blur(2px);
}

.sheet {
  width: 400px;
  padding: 24px;
  border-radius: 16px;
  border: 1px solid rgba(255, 255, 255, 0.11);
  background: color-mix(in srgb, var(--raised) 86%, transparent);
  backdrop-filter: blur(28px) saturate(140%);
  box-shadow: 0 32px 80px -32px rgba(0, 0, 0, 0.85);
}

.sheet h2 {
  margin: 0 0 12px;
  font-size: 16px;
  font-weight: 600;
  letter-spacing: -0.01em;
}

.srow {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  height: 40px;
  border-top: 1px solid rgba(255, 255, 255, 0.07);
  font-size: 13px;
  color: var(--dim);
}
.srow code {
  font-family: Consolas, monospace;
  font-size: 11px;
  color: var(--text);
}
.srow em {
  font-style: normal;
  color: var(--text);
}

.sheet-foot {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
}
.sheet-foot .btn {
  width: 88px;
}
</style>
