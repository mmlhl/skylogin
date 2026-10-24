<script setup lang="ts">
/**
 * 风格 D · 暖石（编辑感 / 纸质）
 *
 * 特征：
 *   - 暖灰底 + 米白文字，不用纯黑
 *   - 标题用衬线字体，接近杂志排版
 *   - 没有卡片，全靠留白和细线
 *   - 启动按钮是深色描边 + 米白底
 */
import { ref } from "vue";
import { accounts } from "../data";

const open = ref(false);
</script>

<template>
  <div class="wrap">
    <aside class="rail">
      <p class="label">账号</p>

      <div class="list">
        <button v-for="a in accounts" :key="a.id" class="item" :class="{ on: a.current }">
          <span class="n">{{ a.name }}</span>
          <span class="c">{{ a.channel === "huawei" ? "华为" : "网易" }}</span>
        </button>
      </div>

      <div class="adds">
        <button class="add">添加渠道账号</button>
        <span class="sep">或</span>
        <button class="add">添加网易账号</button>
      </div>
    </aside>

    <main class="pane">
      <div class="article">
        <p class="kicker">华为账号</p>
        <h1>示例玩家</h1>
        <p class="lede">账号 1100000000000001 · 已登录</p>

        <div class="rule" />

        <button class="cta">启动光遇</button>

        <button class="more" @click="open = !open">设置</button>

        <div v-if="open" class="rows">
          <div><span>游戏路径</span><code>D:\FeverApps\sky\sky.exe</code></div>
          <div><span>登录设备</span><code>amdemo0000000000-d</code></div>
        </div>
      </div>
    </main>
  </div>
</template>

<style scoped>
.wrap {
  display: grid;
  grid-template-columns: 250px 1fr;
  height: 100%;
  background: #14120f;
  color: #ece7df;
  font-family: "Segoe UI Variable Text", "Segoe UI", "Microsoft YaHei UI", sans-serif;
}

.rail {
  display: flex;
  flex-direction: column;
  padding: 32px 18px 22px;
  background: #17140f;
  border-right: 1px solid rgba(236, 231, 223, 0.08);
}

.label {
  margin: 0 0 14px 6px;
  font-size: 10px;
  letter-spacing: 0.18em;
  color: #7d7468;
}

.list {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.item {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 8px;
  padding: 10px 8px;
  border: 0;
  border-bottom: 1px solid rgba(236, 231, 223, 0.06);
  background: transparent;
  color: inherit;
  font-family: inherit;
  text-align: left;
  cursor: pointer;
}
.item:hover .n {
  color: #fff;
}
.item.on .n {
  color: #fff;
}
.item.on .c {
  color: #c8a97a;
}

.n {
  font-size: 13.5px;
  color: #cfc7ba;
}
.c {
  font-size: 10.5px;
  color: #7d7468;
}

.adds {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 6px;
  padding-top: 16px;
}

.add {
  width: 100%;
  height: 32px;
  border: 1px solid rgba(236, 231, 223, 0.16);
  border-radius: 4px;
  background: transparent;
  color: #cfc7ba;
  font-family: inherit;
  font-size: 12px;
  cursor: pointer;
  transition: border-color 0.16s, color 0.16s;
}
.add:hover {
  border-color: rgba(236, 231, 223, 0.34);
  color: #fff;
}

.sep {
  font-size: 10px;
  color: #5b544a;
}

.pane {
  display: flex;
  align-items: center;
  padding: 0 68px;
}

.article {
  max-width: 460px;
}

.kicker {
  margin: 0;
  font-size: 10.5px;
  letter-spacing: 0.18em;
  color: #7d7468;
}

h1 {
  margin: 14px 0 0;
  font-family: "Songti SC", "SimSun", Georgia, serif;
  font-size: 52px;
  line-height: 1.1;
  font-weight: 600;
  letter-spacing: 0.01em;
  color: #f4efe6;
}

.lede {
  margin: 12px 0 0;
  font-size: 12.5px;
  color: #8f8678;
}

.rule {
  width: 44px;
  height: 1px;
  margin: 26px 0;
  background: rgba(200, 169, 122, 0.6);
}

.cta {
  height: 44px;
  padding: 0 30px;
  border: 1px solid rgba(236, 231, 223, 0.22);
  border-radius: 4px;
  background: #efe9df;
  color: #17140f;
  font-family: inherit;
  font-size: 13px;
  font-weight: 600;
  letter-spacing: 0.02em;
  cursor: pointer;
  transition: background 0.16s;
}
.cta:hover {
  background: #fff;
}

.more {
  display: block;
  margin-top: 28px;
  padding: 0;
  border: 0;
  background: transparent;
  color: #7d7468;
  font-family: inherit;
  font-size: 11.5px;
  cursor: pointer;
  border-bottom: 1px solid rgba(236, 231, 223, 0.18);
  padding-bottom: 2px;
}
.more:hover {
  color: #cfc7ba;
}

.rows {
  margin-top: 18px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.rows div {
  display: flex;
  justify-content: space-between;
  gap: 16px;
  font-size: 11.5px;
  color: #8f8678;
  padding-bottom: 8px;
  border-bottom: 1px solid rgba(236, 231, 223, 0.06);
}
.rows code {
  font-family: Consolas, monospace;
  font-size: 10.5px;
  color: #cfc7ba;
}
</style>
