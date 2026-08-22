<script setup lang="ts">
/**
 * error 消息的自定义 renderer：红底错误气泡，原样展示后端错误文本
 * （不渲染 markdown —— 排查时要能选中复制原文）。样式沿自旧 Conversation.vue。
 */
import type { ConversationItem } from "@/task-conversation/core";
import { useI18n } from "vue-i18n";

defineProps<{ item: ConversationItem }>();
const { t } = useI18n();

function formatTime(iso: string): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}
</script>

<template>
  <div class="error-bubble">
    <span class="error-icon">⚠</span>
    <div class="error-body">
      <div class="error-title">{{ t('conversation.failed') }}</div>
      <div class="error-detail">{{ item.content || t('conversation.sendFailed') }}</div>
      <div v-if="item.timestamp" class="error-time">{{ formatTime(item.timestamp) }}</div>
    </div>
  </div>
</template>

<style scoped>
.error-bubble {
  display: flex; gap: 10px; align-items: flex-start;
  width: fit-content; max-width: 85%;
  padding: 10px 14px;
  background: #fef2f2; border: 1px solid #fecaca;
  border-radius: var(--r-md);
}
.error-icon { font-size: 14px; line-height: 1.5; color: #dc2626; flex-shrink: 0; }
.error-body { min-width: 0; }
.error-title { font-size: 13px; font-weight: 600; color: #b91c1c; margin-bottom: 2px; }
.error-detail {
  font-family: var(--font-mono); font-size: 12.5px; line-height: 1.55;
  color: #7f1d1d; word-break: break-word; user-select: text;
}
.error-time { font-size: 11px; color: #b91c1c; opacity: 0.7; margin-top: 4px; font-variant-numeric: tabular-nums; }
</style>
