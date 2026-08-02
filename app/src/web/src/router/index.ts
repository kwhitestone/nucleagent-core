import { createRouter, createWebHistory, type RouteRecordRaw } from "vue-router";
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
  history: createWebHistory(routerBase),
  routes,
});

// core 假定 JWT 已由 auth 子应用写入 localStorage。无 token 时跳到壳应用的
// /auth 让用户登录；独立 dev 也落在 /auth（由本地反代或 dev server 处理）。
router.beforeEach((to) => {
  if (to.meta.requiresAuth && !getAccessToken()) {
    if (typeof window !== "undefined") {
      window.location.href = "/auth";
    }
    return false;
  }
  return true;
});

export default router;
