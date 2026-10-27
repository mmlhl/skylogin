/**
 * 界面层的本地状态：弹层开关。
 * 壳组件只调 open*()，弹层本体由 Overlays.vue 统一渲染——
 * 这样四种壳共享同一套「添加 / 多开 / 改名」交互，不必各写一遍。
 */
import { reactive } from "vue";

export const demoUi = reactive({
  addOpen: false,
  multiOpen: false,
  renameId: "",
  /** 多开弹层要预选的账号；空数组 = 用默认预选逻辑 */
  multiPreset: [] as string[],
  openAdd() {
    this.multiOpen = false;
    this.addOpen = true;
  },
  openMulti(preset: string[] = []) {
    this.addOpen = false;
    this.multiPreset = preset;
    this.multiOpen = true;
  },
  openRename(id: string) {
    this.renameId = id;
  },
  closeAll() {
    this.addOpen = false;
    this.multiOpen = false;
    this.renameId = "";
  },
});
