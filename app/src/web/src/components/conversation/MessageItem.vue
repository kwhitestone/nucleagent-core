<script setup lang="ts">
/**
 * 消息气泡（TaskConversation 的 user-item / assistant-item 插槽实现）。
 *
 * 设计：单层气泡，全部视觉画在 .msg-bubble 上，外壳（.atc-user-item 等）
 * 由 Conversation.vue 归零，避免组件默认样式与插槽样式叠加成框中框。
 * 用户 = indigo→violet 渐变实底白字，右对齐，头像贴右；
 * 助手 = 半透明白轻量底，左对齐，头像贴左。
 */
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import type { ConversationItem } from "@/task-conversation/core";
import ConversationContent from "@/task-conversation/vue/ConversationContent.vue";

const props = defineProps<{ item: ConversationItem; role?: "user" | "assistant" }>();
const { t } = useI18n();

const isUser = computed(() => props.role === "user" || props.item.role === "user");

/** 格式化时间（精确到秒）。 */
const time = computed(() => {
  if (!props.item.timestamp) return "";
  const d = new Date(props.item.timestamp);
  if (Number.isNaN(d.getTime())) return "";
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
});
</script>

<template>
  <div class="msg" :class="isUser ? 'user' : 'assistant'">
    <div class="msg-avatar">
      <template v-if="isUser">{{ t('conversation.you') }}</template>
      <svg v-else viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 2L4 7v10l8 5 8-5V7l-8-5z" /><path d="M12 22V12" /><path d="M4 7l8 5 8-5" /></svg>
    </div>
    <div class="msg-main">
      <div class="msg-header">
        <span class="msg-author">{{ isUser ? t('conversation.you') : t('home.greeting') }}</span>
        <span v-if="time" class="msg-time">{{ time }}</span>
      </div>
      <div class="msg-bubble">
        <ConversationContent :item="item" />
      </div>
    </div>
  </div>
</template>

<style scoped>
.msg {
  display: flex;
  gap: 12px;
  max-width: 100%;
  animation: fade-in-up 0.4s var(--ease-out) both;
}

/* 头像：28px 圆角方块，用户 indigo 系 / 助手 violet 系（与各自气泡呼应） */
.msg-avatar {
  width: 28px; height: 28px; border-radius: 8px;
  display: flex; align-items: center; justify-content: center;
  flex-shrink: 0; font-weight: 600; font-size: 11px;
  margin-top: 20px; /* 对齐 header 下方的气泡顶 */
}
.msg.user { flex-direction: row-reverse; }
.msg.user .msg-avatar { background: var(--indigo-500); color: #fff; }
.msg.assistant .msg-avatar {
  background: linear-gradient(135deg, var(--violet-500), var(--fuchsia-500, #d946ef));
  color: #fff;
}
.msg.assistant .msg-avatar svg { width: 15px; height: 15px; }

.msg-main { flex: 1; min-width: 0; }
.msg.user .msg-main { flex: unset; max-width: 100%; display: flex; flex-direction: column; align-items: flex-end; }

.msg-header { display: flex; align-items: baseline; gap: 8px; margin-bottom: 5px; padding: 0 2px; }
.msg.user .msg-header { justify-content: flex-end; }
.msg-author { font-size: 12px; font-weight: 600; color: var(--text-secondary); }
.msg-time { font-size: 11px; color: var(--text-tertiary); font-variant-numeric: tabular-nums; }

/* 单层气泡 */
.msg-bubble { font-size: 14px; line-height: 1.7; border-radius: 14px; }
.msg.user .msg-bubble {
  background: linear-gradient(160deg, var(--indigo-500), var(--violet-500));
  color: #fff;
  padding: 10px 16px;
  border-radius: 16px 16px 5px 16px;
  box-shadow: 0 2px 8px rgb(99 102 241 / 20%);
}
.msg.assistant .msg-bubble {
  background: rgb(255 255 255 / 80%);
  border: 1px solid var(--border);
  padding: 10px 16px;
  border-radius: 5px 16px 16px 16px;
  box-shadow: var(--shadow-xs);
  color: var(--slate-700);
}

/* markdown 里的 code 不继承白字 */
.msg.user .msg-bubble :deep(code) { background: rgb(255 255 255 / 20%); color: #fff; }
.msg.user .msg-bubble :deep(a) { color: #fff; text-decoration: underline; }
</style>
