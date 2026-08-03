import {
  createRouter,
  createWebHistory,
  createWebHashHistory,
  type RouteRecordRaw,
} from "vue-router";
import { getAccessToken } from "@/utils/token";

// core 子应用在壳里挂在根路径 "/"，因此路由 base 始终为 "/"（与独立运行一致）。
// 嵌入探测：被主壳加载时（micro-app 沙箱 或 iframe）不做 window.location 硬跳转，
// 让壳决定何时弹登录。独立 dev 时才跳 /auth。
const isEmbedded =
  (globalThis as Record<string, unknown>).__MICRO_APP_ENVIRONMENT__ === true ||
  (typeof window !== "undefined" && window.parent !== window);
const routerBase = "/";

const routes: RouteRecordRaw[] = [
  // 「对话」是默认入口。/ 重定向到 /chat，/chat 用 Home.vue（新建对话的 composer + 建议卡）。
  { path: "/", redirect: "/chat" },
  {
    path: "/chat",
    name: "chat",
    component: () => import("@/views/Home.vue"),
    meta: { requiresAuth: true },
  },
  {
    path: "/creation",
    name: "creation",
    component: () => import("@/views/Creation.vue"),
    meta: { requiresAuth: true },
  },
  {
    path: "/tasks",
    name: "tasks",
    component: () => import("@/views/TaskSetup.vue"),
    meta: { requiresAuth: true },
  },
  {
    path: "/c/:id",
    name: "conversation",
    component: () => import("@/views/Conversation.vue"),
    meta: { requiresAuth: true },
    props: (route) => ({ id: route.params.id as string }),
  },
  {
    path: "/:pathMatch(.*)*",
    redirect: "/chat",
  },
];

const router = createRouter({
  history: isEmbedded ? createWebHashHistory(routerBase) : createWebHistory(routerBase),
  routes,
});

// core 假定 JWT 已由主壳登录弹窗写入 localStorage（沙箱/iframe 同源共享）。
// 嵌入模式下不做 window.location 跳转（iframe 里会导航到不存在的 /auth 导致白屏），
// 让壳决定何时弹登录。独立 dev 时才跳 /auth。
router.beforeEach((to) => {
  if (to.meta.requiresAuth && !getAccessToken()) {
    if (!isEmbedded && typeof window !== "undefined") {
      window.location.href = "/auth";
    }
    // 嵌入模式下放行：首页/创作/任务在未登录时可浏览（无数据时显示空态），
    // 对话视图在未登录时由后端 401 兜底。
    return true;
  }
  return true;
});

export default router;
