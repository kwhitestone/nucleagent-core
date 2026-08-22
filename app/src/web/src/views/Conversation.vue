<script setup lang="ts">
/**
 * 对话视图 —— 基于 src/task-conversation 组件（自 agentia 拷贝自维护）。
 *
 * 组件负责：消息列表渲染（含 process 折叠/展开）、流式 upsert、乐观发送、
 * 滚动跟随、Composer（发送/停止/附件）。宿主职责见 adapter（映射规则在
 * composables/useConversationAdapter.ts 头注释）。
 *
 * 宿主补充：
 *   - ModelPicker 经 composer-toolbar-leading 插槽注入（仍走 PATCH 落库）。
 *   - error 消息用自定义 renderer（红底错误气泡，不渲染 markdown）。
 *   - tool_call / thinking 走 process lane，组件渲染为可折叠过程条目
 *     （标题取 senderName），不需要自定义 renderer。
 *   - 附件上传复用组件的 uploadAttachment 边界 → api/storage.uploadFile。
 */
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";
import { ApiError } from "@/api/http";
import { updateConversationModel } from "@/api/conversation";
import type { ModelChoice } from "@/api/types";
import type { ConversationAdapter } from "@/task-conversation/core";
import type { ConversationRendererRegistry } from "@/task-conversation/vue";
import { TaskConversation } from "@/task-conversation/vue";
import "@/task-conversation/styles.css";
import { createConversationAdapter } from "@/composables/useConversationAdapter";
import { toast } from "@/composables/useToast";
import ModelPicker from "@/components/ModelPicker.vue";
import ErrorBubble from "@/components/conversation/ErrorBubble.vue";
import MessageItem from "@/components/conversation/MessageItem.vue";

const props = defineProps<{ id: string }>();
const { t } = useI18n();

/** conversationKey 变化时组件 controller 会自动重新 initialize。 */
const adapter = computed<ConversationAdapter>(() => createConversationAdapter(() => props.id));

const modelChoice = ref<ModelChoice | null>(null);

async function switchModel(next: ModelChoice | null): Promise<void> {
  const prev = modelChoice.value;
  modelChoice.value = next;
  if (!next) return; // 清空选择只影响本地，不 PATCH（后端没有"恢复默认"语义）
  try {
    await updateConversationModel(props.id, next);
    toast.success(t("common.modelSwitched", { model: next.model }));
  } catch (error) {
    modelChoice.value = prev;
    toast.error(error instanceof ApiError ? error.message : t("common.modelSwitchFailed"));
  }
}

/** 自定义 renderer：error → 红底气泡（system lane）。 */
const renderers: ConversationRendererRegistry = {
  error: ErrorBubble,
};

function onError(error: Error): void {
  toast.error(error.message);
}
</script>

<template>
  <div class="view active view--scroll-hidden">
    <div class="chat-view">
      <TaskConversation
        :conversation-key="id"
        :adapter="adapter"
        :capabilities="{ send: true, stop: true, attachments: true }"
        :renderers="renderers"
        :show-process="true"
        locale="zh-CN"
        @error="onError"
      >
        <template #user-item="{ item }">
          <MessageItem :item="item" role="user" />
        </template>
        <template #assistant-item="{ item }">
          <MessageItem :item="item" role="assistant" />
        </template>
        <template #composer-toolbar-leading>
          <ModelPicker
            :model-value="modelChoice"
            compact
            @update:model-value="switchModel"
          />
        </template>
      </TaskConversation>
    </div>
  </div>
</template>

<style>
/* 对话主区由组件渲染（.atc-* 命名空间）。这里做两件事：
   1) 外壳布局；
   2) 把 --atc-* 设计变量接到 aurora token —— 组件默认是自带的一套
      白底蓝色主题，不重定向会跟全站视觉完全脱节。 */
.chat-view {
  display: flex; flex-direction: column; height: 100%;
  max-width: 820px; margin: 0 auto; width: 100%;
}

.chat-view .atc-root {
  --atc-bg: var(--bg);
  --atc-surface: var(--bg-subtle);
  --atc-surface-strong: var(--bg-hover);
  --atc-text: var(--text-primary);
  --atc-text-muted: var(--text-tertiary);
  --atc-border: var(--border);
  --atc-accent: var(--indigo-500);
  --atc-accent-hover: var(--indigo-600);
  --atc-accent-contrast: #ffffff;
  --atc-danger: #dc2626;
  --atc-content-width: 820px;
  --atc-radius: var(--r-lg);
  flex: 1; min-height: 0;
  font-family: var(--font-body);
}


.chat-view .atc-assistant-item { margin-bottom: 18px; }
.chat-view .atc-item + .atc-assistant-item,
.chat-view .atc-process + .atc-assistant-item { margin-top: 0; }

/* 用户消息外壳归零：组件默认会给 .atc-user-item 加气泡（灰底/padding/圆角），
   我们的气泡在 MessageItem 插槽里画，这里必须全部抵消，否则框中框。 */
.chat-view .atc-user-item {
  background: transparent;
  border: none;
  padding: 0;
  margin: 0 0 18px auto;
  width: auto;
  max-width: 78%;
  box-shadow: none;
  border-radius: 0;
  overflow: visible;
}

/* 过程条目（思考/工具）：紧凑标签条，对齐旧版 .tool-tag 观感 */
.chat-view .atc-process {
  border-left: none;
  /* 对齐助手气泡左缘：头像 28 + gap 12 = 40px */
  margin: 0 0 8px 40px;
  width: fit-content;
  max-width: calc(100% - 40px);
}
.chat-view .atc-process-summary {
  display: flex; align-items: center; gap: 6px;
  padding: 4px 12px;
  font-size: 12px; color: var(--text-tertiary);
  background: var(--bg-hover); border-radius: var(--r-sm);
  width: fit-content; max-width: 80%;
}
.chat-view .atc-process-summary:hover { background: var(--bg-subtle); }
/* 思考中（内容仍在流式更新）：label 前加呼吸点，一眼看出还活着 */
@keyframes atc-pulse { 0%, 100% { opacity: 0.35; } 50% { opacity: 1; } }
.chat-view .atc-process-marker {
  width: 7px; height: 7px; border-radius: 50%;
  background: var(--indigo-500);
  flex-shrink: 0;
}
/* 只有「活跃思考」才脉冲：data-status 由组件按 item.status 输出，
   思考结束（streaming→complete）动画自然停止。历史过程条不动画。 */
.chat-view details.atc-process[data-status="streaming"] .atc-process-marker {
  animation: atc-pulse 1.2s ease-in-out infinite;
}
.chat-view .atc-process-marker { color: var(--indigo-500); }
.chat-view .atc-process-label {
  font-weight: 600; color: var(--indigo-600); white-space: nowrap;
}
.chat-view .atc-process-preview {
  color: var(--text-tertiary); overflow: hidden;
  text-overflow: ellipsis; white-space: nowrap; font-weight: 400;
}
.chat-view .atc-process-content {
  margin: 4px 0 8px 0;
  font-size: 12.5px; color: var(--text-tertiary);
  background: var(--bg-subtle); border-radius: var(--r-md);
  padding: 8px 12px;
}
/* 流式中的过程条目（思考中）：呼吸动画，提示活跃 */
.chat-view details.atc-process:has(.atc-process-label) { transition: opacity 0.2s; }

/* 助手 markdown：行内 code / 代码块用全站样式 */
.chat-view .atc-item code {
  font-family: var(--font-mono); font-size: 12.5px;
  background: var(--grad-brand-soft);
  padding: 1px 6px; border-radius: 4px; color: var(--indigo-600);
}
.chat-view .atc-item pre {
  background: #1e293b; color: #e2e8f0; border-radius: 8px;
  padding: 12px 16px; overflow-x: auto; margin: 8px 0;
}
/* 流式文本也是 <pre>（.atc-stream-text）——必须排除深色代码块底，
   否则输出中深底、完成切 markdown 后变白，观感就是"输出时颜色不对"。 */
.chat-view .atc-item pre.atc-stream-text {
  background: none; color: var(--slate-700); padding: 0; margin: 0;
  font-family: var(--font-body); font-size: 14px; line-height: 1.7;
}
.chat-view .atc-item pre code { background: none; color: inherit; padding: 0; font-size: 13px; }
.chat-view .atc-item table { border-collapse: collapse; margin: 8px 0; width: 100%; font-size: 13px; }
.chat-view .atc-item th, .chat-view .atc-item td {
  border: 1px solid var(--border); padding: 6px 10px; text-align: left;
}
.chat-view .atc-item th { background: var(--bg-hover); font-weight: 600; }

/* Composer：毛玻璃 + 品牌聚焦（对齐旧版 .chat-composer） */
.chat-view .atc-composer-shell { background: transparent; }
.chat-view .atc-composer {
  background: rgb(255 255 255 / 90%);
  backdrop-filter: blur(20px); -webkit-backdrop-filter: blur(20px);
  border: 1.5px solid var(--border);
  border-radius: var(--r-xl);
  box-shadow: var(--shadow-md);
}
.chat-view .atc-composer:focus-within {
  border-color: var(--teal-300);
  box-shadow: var(--shadow-lg), 0 0 0 4px rgb(20 184 166 / 8%);
}
.chat-view .atc-composer-input { font-family: var(--font-body); }
</style>
