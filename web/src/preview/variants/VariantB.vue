<script setup lang="ts">
/**
 * 风格 B · 极简留白（Vercel / Apple 那种）
 *
 * 特征：
 *   - 大量留白，几乎无描边
 *   - 只有一处深色实心按钮（白底黑字）
 *   - 字号拉开层次，靠灰阶而不是线框分区
 *   - 主界面只保留：账号、状态、一个动作
 */
import { ref } from "vue";
import { accounts } from "../data";

const showMore = ref(false);
</script>

<template>
  <div class="wrap">
    <aside class="rail">
      <div class="list">
        <button v-for="a in accounts" :key="a.id" class="item" :class="{ on: a.current }">
          <span class="name">{{ a.name }}</span>
          <span class="sub">{{ a.channel === "huawei" ? "华为账号" : "网易账号" }}</span>
        </button>
      </div>

      <div class="adds">
        <button class="add">＋ 添加渠道账号</button>
        <button class="add">＋ 添加网易账号</button>
      </div>
    </aside>

    <main class="pane">
      <div class="block">
        <p class="eyebrow">华为账号 · 1100********0001</p>
        <h1>示例玩家</h1>
        <p class="status">已登录</p>

        <button class="cta">启动光遇</button>
      </div>

      <button class="more" @click="showMore = !showMore">设置</button>

      <div v-if="showMore" class="drawer">
        <div class="row"><span>游戏路径</span><code>D:\FeverApps\sky\sky.exe</code></div>
        <div class="row"><span>登录设备</span><code>amdemo0000000000-d</code></div>
        <div class="row"><span>启动前关闭平台</span><span class="on-dot">已开</span></div>
      </div>
    </main>
  </div>
</template>

<style scoped>
.wrap {
  display: grid;
  grid-template-columns: 260px 1fr;
  height: 100%;
  background: #000;
  color: #ededed;
  font-family: "Segoe UI Variable Display", "Segoe UI", "Microsoft YaHei UI", sans-serif;
}

.rail {
  display: flex;
  flex-direction: column;
  padding: 40px 20px 28px;
  border-right: 1px solid #1a1a1a;
}

.list {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.item {
  display: flex;
  flex-direction: column;
  gap: 3px;
  padding: 12px 12px;
  border: 0;
  border-radius: 10px;
  background: transparent;
  text-align: left;
  cursor: pointer;
  color: inherit;
  font-family: inherit;
}
.item:hover {
  background: #0d0d0d;
}
.item.on {
  background: #141414;
}

.name {
  font-size: 14px;
  font-weight: 500;
  letter-spacing: -0.01em;
}
.sub {
  font-size: 12px;
  color: #6f6f6f;
}

/* 两个添加按钮：文字 + 细线，居中对齐 */
.adds {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding-top: 14px;
}

/* 两个添加按钮：细描边 + 文字居中（之前是纯文字左对齐，太简陋） */
.add {
  height: 34px;
  padding: 0 12px;
  border: 1px solid #232323;
  border-radius: 8px;
  background: transparent;
  color: #a8a8a8;
  font-family: inherit;
  font-size: 12.5px;
  cursor: pointer;
  text-align: center;
}
.add:hover {
  background: #0d0d0d;
  color: #ededed;
}

.pane {
  position: relative;
  display: flex;
  flex-direction: column;
  justify-content: center;
  padding: 0 72px;
}

.block {
  max-width: 420px;
}

.eyebrow {
  margin: 0;
  font-size: 12px;
  letter-spacing: 0.02em;
  color: #6f6f6f;
}

h1 {
  margin: 10px 0 0;
  font-size: 56px;
  line-height: 1.05;
  font-weight: 600;
  letter-spacing: -0.035em;
}

.status {
  margin: 14px 0 0;
  font-size: 13px;
  color: #8a8a8a;
}

/* 唯一实心按钮：白底黑字 */
.cta {
  margin-top: 40px;
  height: 46px;
  padding: 0 36px;
  border: 0;
  border-radius: 999px;
  background: #ededed;
  color: #000;
  font-family: inherit;
  font-size: 14px;
  font-weight: 600;
  cursor: pointer;
  transition: transform 0.12s, opacity 0.12s;
}
.cta:hover {
  opacity: 0.88;
}
.cta:active {
  transform: scale(0.99);
}

.more {
  position: absolute;
  right: 32px;
  bottom: 28px;
  border: 0;
  background: transparent;
  color: #6f6f6f;
  font-family: inherit;
  font-size: 12px;
  cursor: pointer;
  padding: 6px 8px;
  border-radius: 6px;
}
.more:hover {
  color: #ededed;
  background: #0d0d0d;
}

.drawer {
  position: absolute;
  right: 28px;
  bottom: 64px;
  width: 340px;
  padding: 16px;
  border-radius: 12px;
  background: #0d0d0d;
  border: 1px solid #1f1f1f;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  font-size: 12px;
  color: #8a8a8a;
}
.row code {
  font-family: Consolas, monospace;
  font-size: 11px;
  color: #c9c9c9;
}
.on-dot {
  color: #ededed;
}
</style>

