import { createApp, type App as VueApp } from "vue";
import { createPinia } from "pinia";

import App from "./App.vue";
import router from "./router";
import i18n from "./i18n";
// Aurora 设计 token 必须先于 global.css 引入（global.css 内的规则依赖这些变量）。
// aurora.css 由 nucleagent-deploy/scripts/sync-design-tokens.sh 从设计稿生成，勿手改。
import "./styles/aurora.css";
import "./styles/global.css";

const MOUNT_ID = "core-app";

let app: VueApp | null = null;

function mount() {
  app = createApp(App);
  app.use(createPinia());
  app.use(router);
  app.use(i18n);
  // 移除 Element Plus：设计稿全自绘组件，EP 的样式与 Aurora 冲突。
  // 提示/反馈改用 composables/useToast.ts（轻量 DOM toast）。
  app.mount(`#${MOUNT_ID}`);
}

function unmount() {
  if (app) {
    app.unmount();
    app = null;
  }
}

const w = globalThis as Record<string, unknown>;
if (w.__MICRO_APP_ENVIRONMENT__) {
  w.mount = mount;
  w.unmount = unmount;
} else {
  mount();
}

// Chrome 4K 高 DPI 首帧布局竞态修复：core 在 iframe 内运行，iframe 文档是独立
// 渲染上下文，DPI 竞态可能在 iframe 内独立发生（shell 侧也同步处理）。mount 后
// 延迟两帧（首帧合成 + DPI 校正完成）触发重排，让 .view.active 用正确尺寸布局。
// 对 Edge/标准 DPI 无副作用。仅独立运行时需要（iframe 模式由 shell 侧 resize 传递）。
if (!w.__MICRO_APP_ENVIRONMENT__ && typeof window !== "undefined") {
  requestAnimationFrame(() => {
    requestAnimationFrame(() => {
      window.dispatchEvent(new Event("resize"));
    });
  });
}
