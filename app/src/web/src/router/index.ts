import {
  createRouter,
  createWebHistory,
  createWebHashHistory,
  type RouteRecordRaw,
} from "vue-router";
import { getAccessToken } from "@/utils/token";

// core 子应用在壳里挂在根路径 "/"，因此路由 base 始终为 "/"（与独立运行一致）。
// micro-app 环境探测仅用于将来需要按壳路径分桶时复用。
const isMicroApp =
  (globalThis as Record<string, unknown>).__MICRO_APP_ENVIRONMENT__ === true;
void isMicroApp; // 目前 base 固定，保留探测以便后续按需扩展。
const routerBase = "/";

const routes: RouteRecordRaw[] = [
  {
    path: "/",
    name: "workbench",
    component: () => import("@/views/Workbench.vue"),
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
    redirect: "/",
  },
];

const router = createRouter({
  history: isMicroApp ? createWebHashHistory(routerBase) : createWebHistory(routerBase),
  routes,
});

// core 假定 JWT 已由 auth 子应用写入 localStorage。
// 在 micro-app 子应用模式下，不做 window.location 跳转（会劫持整个壳），
// 让壳应用决定何时切到 auth 子应用。独立 dev 时跳 /auth 登录。
router.beforeEach((to) => {
  if (to.meta.requiresAuth && !getAccessToken()) {
    if (!isMicroApp && typeof window !== "undefined") {
      window.location.href = "/auth";
    }
    // micro-app 模式下放行，页面内自行处理未登录状态。
    return true;
  }
  return true;
});

export default router;
