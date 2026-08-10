/// <reference types="vite/client" />

interface ImportMetaEnv {
  /** 运行时后端地址（注入 import.meta.env）。留空则回退到相对路径 /api。 */
  readonly VITE_CORE_BACKEND_URL?: string;
  /**
   * storage 服务地址（:26610），附件上传用。
   *
   * 必须是**浏览器可达**的绝对地址：storage 与 core 共用 /api/v1 前缀，
   * 走 vite 的 /api 代理会被 core 吞掉，所以只能跨域直连。
   */
  readonly VITE_STORAGE_URL?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}

declare module "*.vue" {
  import type { DefineComponent } from "vue";
  const component: DefineComponent<Record<string, unknown>, Record<string, unknown>, unknown>;
  export default component;
}
