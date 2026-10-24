<script setup lang="ts">
/**
 * 通用输入弹窗（改别名用）。
 *
 * 用法：
 *   const name = await promptRef.value.open("修改别名", "旧名字", "提示文字");
 *   // 点保存返回输入值，点取消返回 null
 *
 * 这样调用方拿到的是一个 Promise，不用关心弹窗内部怎么实现。
 */
import { ref } from "vue";
import { NModal, NInput } from "naive-ui";

const show = ref(false);
const title = ref("");
const hint = ref("");
const value = ref("");
let resolve: ((v: string | null) => void) | null = null;

/** 打开弹窗，返回用户输入（取消为 null） */
function open(t: string, initial = "", h = ""): Promise<string | null> {
  title.value = t;
  hint.value = h;
  value.value = initial;
  show.value = true;
  return new Promise((res) => {
    resolve = res;
  });
}

function confirm() {
  show.value = false;
  resolve?.(value.value.trim());
  resolve = null;
}

function cancel() {
  show.value = false;
  resolve?.(null);
  resolve = null;
}

defineExpose({ open });
</script>

<template>
  <n-modal
    :show="show"
    preset="card"
    :title="title"
    style="width: 400px"
    display-directive="if"
    @update:show="(v: boolean) => !v && cancel()"
  >
    <n-input
      v-model:value="value"
      placeholder="请输入"
      :maxlength="24"
      show-count
      @keyup.enter="confirm"
    />
    <p v-if="hint" class="hint">{{ hint }}</p>

    <div class="actions">
      <button class="ghost" @click="cancel">取消</button>
      <button class="primary" @click="confirm">保存</button>
    </div>
  </n-modal>
</template>

<style scoped>
.hint {
  margin: 10px 0 0;
  font-size: 11.5px;
  color: var(--faint);
}

.actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 18px;
}

.ghost,
.primary {
  padding: 7px 16px;
  border-radius: 7px;
  border: 1px solid var(--line-strong);
  font-size: 12.5px;
  cursor: pointer;
}
.ghost {
  background: transparent;
  color: var(--dim);
}
.ghost:hover {
  color: var(--text);
}
.primary {
  border-color: transparent;
  background: var(--gold);
  color: #1a1305;
  font-weight: 600;
}
</style>


