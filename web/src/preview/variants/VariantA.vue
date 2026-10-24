<script setup lang="ts">
/**
 * 风格 A · 工具台（线性/命令面板那种感觉）
 *
 * 特征：
 *   - 深灰近黑，元素密集但克制
 *   - 按钮一律细描边，不用大面积色块
 *   - ID、路径用等宽字体
 *   - 设置收进「设置」弹层，主界面只留账号和启动
 */
import { ref } from "vue";
import { accounts } from "../data";

const settingsOpen = ref(false);
</script>

<template>
  <div class="wrap">
    <aside class="rail">
      <div class="rail-head">
        <span>账号</span>
        <span class="count">2</span>
      </div>

      <div class="list">
        <div v-for="a in accounts" :key="a.id" class="item" :class="{ on: a.current }">
          <span class="dot" />
          <div class="meta">
            <b>{{ a.name }}</b>
            <span>{{ a.channel === "huawei" ? "华为" : "网易" }} · {{ a.id }}</span>
          </div>
        </div>
      </div>

      <div class="adds">
        <button class="btn">添加渠道账号</button>
        <button class="btn primary-ghost">添加网易账号</button>
      </div>
    </aside>

    <main class="pane">
      <div class="top">
        <h1>示例玩家</h1>
        <span class="badge">华为</span>
      </div>
      <p class="id">1100000000000001</p>

      <div class="status"><span class="ok" />已登录，可直接启动</div>

      <button class="cta">启动光遇</button>

      <div class="foot">
        <button class="link" @click="settingsOpen = true">设置</button>
        <span>v1.0.0</span>
      </div>

      <div v-if="settingsOpen" class="sheet" @click.self="settingsOpen = false">
        <div class="sheet-body">
          <h3>设置</h3>
          <label>游戏路径 <code>D:\FeverApps\sky\sky.exe</code></label>
          <label>登录设备 <code>amdemo0000000000-d</code></label>
          <label>启动前关闭发烧平台 <input type="checkbox" checked /></label>
          <button class="btn" @click="settingsOpen = false">关闭</button>
        </div>
      </div>
    </main>
  </div>
</template>

<style scoped>
.wrap {
  display: grid;
  grid-template-columns: 240px 1fr;
  height: 100%;
  background: #0b0b0c;
  color: #e9e9ec;
  font-family: "Segoe UI Variable Text", "Segoe UI", "Microsoft YaHei UI", sans-serif;
  font-size: 12.5px;
}

.rail {
  display: flex;
  flex-direction: column;
  padding: 18px 12px 14px;
  background: #0e0e10;
  border-right: 1px solid rgba(255, 255, 255, 0.06);
}

.rail-head {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 0 8px 10px;
  font-size: 11px;
  letter-spacing: 0.08em;
  color: #6d6d76;
}
.count {
  color: #4d4d55;
}

.list {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.item {
  display: flex;
  align-items: center;
  gap: 9px;
  padding: 8px 8px;
  border-radius: 6px;
  cursor: pointer;
}
.item:hover {
  background: rgba(255, 255, 255, 0.04);
}
.item.on {
  background: rgba(255, 255, 255, 0.07);
}

.dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: #3a3a42;
  flex: none;
}
.item.on .dot {
  background: #d8b57f;
}

.meta {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.meta b {
  font-size: 12.5px;
  font-weight: 500;
}
.meta span {
  font-size: 10.5px;
  color: #6d6d76;
  font-family: Consolas, monospace;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* 两个添加按钮：细描边、等宽、居中 */
.adds {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin-top: 12px;
}

.btn {
  width: 100%;
  height: 30px;
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 6px;
  background: transparent;
  color: #b9b9c0;
  font-family: inherit;
  font-size: 12px;
  cursor: pointer;
}
.btn:hover {
  border-color: rgba(255, 255, 255, 0.22);
  background: rgba(255, 255, 255, 0.04);
  color: #fff;
}
.primary-ghost {
  border-color: rgba(216, 181, 127, 0.35);
  color: #d8b57f;
}
.primary-ghost:hover {
  border-color: rgba(216, 181, 127, 0.6);
  background: rgba(216, 181, 127, 0.1);
  color: #e7c894;
}

.pane {
  position: relative;
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  padding: 52px 48px 24px;
}

.top {
  display: flex;
  align-items: center;
  gap: 10px;
}

h1 {
  margin: 0;
  font-size: 28px;
  font-weight: 600;
  letter-spacing: -0.02em;
}

.badge {
  padding: 2px 7px;
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 4px;
  font-size: 10.5px;
  color: #8b8b93;
}

.id {
  margin: 6px 0 0;
  font-family: Consolas, monospace;
  font-size: 11.5px;
  color: #5f5f68;
}

.status {
  display: flex;
  align-items: center;
  gap: 7px;
  margin-top: 26px;
  font-size: 12px;
  color: #9a9aa2;
}
.ok {
  width: 5px;
  height: 5px;
  border-radius: 50%;
  background: #7bbf94;
}

/* 主 CTA：不铺满，细描边 + 极淡金底 */
.cta {
  margin-top: 26px;
  width: 200px;
  height: 38px;
  border: 1px solid rgba(216, 181, 127, 0.45);
  border-radius: 6px;
  background: rgba(216, 181, 127, 0.12);
  color: #e7c894;
  font-family: inherit;
  font-size: 12.5px;
  font-weight: 500;
  cursor: pointer;
}
.cta:hover {
  background: rgba(216, 181, 127, 0.2);
}

.foot {
  margin-top: auto;
  display: flex;
  align-items: center;
  gap: 14px;
  font-size: 11px;
  color: #4d4d55;
}

.link {
  border: 0;
  background: transparent;
  color: #6d6d76;
  font-family: inherit;
  font-size: 11px;
  cursor: pointer;
  padding: 0;
}
.link:hover {
  color: #b9b9c0;
}

/* 设置弹层 */
.sheet {
  position: absolute;
  inset: 0;
  display: grid;
  place-items: center;
  background: rgba(0, 0, 0, 0.55);
}
.sheet-body {
  width: 380px;
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 20px;
  border-radius: 10px;
  background: #141416;
  border: 1px solid rgba(255, 255, 255, 0.1);
}
.sheet-body h3 {
  margin: 0 0 4px;
  font-size: 13px;
  font-weight: 600;
}
.sheet-body label {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  font-size: 11.5px;
  color: #8b8b93;
}
.sheet-body code {
  font-family: Consolas, monospace;
  font-size: 10.5px;
  color: #b9b9c0;
}
</style>
