<script setup lang="ts">
import { RouterView } from "vue-router";
import { useShellBridge } from "@/composables/useShellBridge";

// 接通主壳的数据通道：壳切换视图时驱动 core 路由；core 对话列表变化时推回壳。
// 独立运行（非 micro-app）时该 composable 内部是 no-op。
useShellBridge();
</script>

<template>
  <router-view />
</template>
