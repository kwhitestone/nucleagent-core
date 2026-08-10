/**
 * core 子应用 ↔ 主壳 的 postMessage 通道桥接（iframe 方案）。
 *
 * 两个方向：
 *   壳 → core：壳切换视图时 postMessage({source:'shell', type:'view', view, conversationId})。
 *             core 监听它，驱动内部路由跳转。
 *   core → 壳：core 对话列表变化时 postMessage({source:'sub', type:'conversations', ...})，
 *             壳侧栏渲染历史。
 *
 * 仅在被 iframe 嵌入时（window.parent !== window）生效；独立 dev 运行时是 no-op。
 * 协议见 nucleagent-web/src/views/MicroAppHost.vue 顶部注释。
 */
import { watch } from "vue";
import { useRouter } from "vue-router";
import { useConversationStore } from "@/store/conversation";

/** core 是否被主壳以 iframe 嵌入。 */
export function isInShell(): boolean {
  return typeof window !== "undefined" && window.parent !== window;
}

/**
 * 在 core 根组件 setup 时调用一次。
 * - 注册壳→core 的视图意图监听
 * - 把对话 store 变化推回壳
 */
export function useShellBridge(): void {
  if (!isInShell()) return;

  const router = useRouter();
  const store = useConversationStore();

  // 壳 → core：按 view 切换 core 路由 + 同步登录态。
  function onMessage(e: MessageEvent): void {
    const d = e.data as {
      source?: string;
      type?: string;
      view?: "home" | "chat" | "creation" | "tasks" | "providers";
      conversationId?: string | null;
      token?: string | null;
    };
    if (d?.source !== "shell") return;

    // 同步登录态：iframe 跨域 localStorage 不共享，壳登录后把 token 推过来，
    // 写入子应用域的 localStorage，否则所有接口 401。
    if (d.type === "auth") {
      const KEY = "nucleagent_access_token";
      const RKEY = "nucleagent_refresh_token";
      if (d.token) {
        localStorage.setItem(KEY, d.token);
        // token 写入后重新拉对话列表——首次 load() 在 iframe 启动时跑过，
        // 那时 token 还没同步过来（401）。强制 reload 才能拿到真实历史。
        void store.load(true);
      } else {
        localStorage.removeItem(KEY);
        localStorage.removeItem(RKEY);
      }
      return;
    }

    if (d.type !== "view" || !d.view) return;
    const target =
      d.view === "chat" && d.conversationId
        ? `/c/${d.conversationId}`
        : d.view === "chat" ? "/chat"
        : d.view === "creation" ? "/creation"
        : d.view === "tasks" ? "/tasks"
        : d.view === "providers" ? "/providers"
        : "/";
    if (router.currentRoute.value.path !== target) {
      void router.push(target);
    }
  }

  window.addEventListener("message", onMessage);

  /** 把当前对话列表 + 选中态推给壳。列表不变但选中项变时也要重推。 */
  function pushConversations(): void {
    const list = store.sorted;
    const m = router.currentRoute.value.path.match(/^\/c\/(\d+)/);
    const activeId = m ? Number(m[1]) : null;
    window.parent.postMessage(
      {
        source: "sub",
        type: "conversations",
        conversations: list.map((c) => ({ id: c.id, title: c.title, status: c.status })),
        activeId,
      },
      "*",
    );
  }

  // core → 壳：对话列表变化时推。
  watch(() => store.sorted, pushConversations, { deep: true });
  // 路由变化时也推（选中项变了，但列表数据没变，上面的 watch 不触发）。
  watch(() => router.currentRoute.value.path, pushConversations);
}
