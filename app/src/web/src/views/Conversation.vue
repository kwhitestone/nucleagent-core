<script setup lang="ts">
/**
 * 对话视图 —— 对齐 design/nucleagent-design.html 第 1849–1926 行。
 *
 * 消息列表（用户/助手气泡）+ 输入框工具栏。
 * 替代旧版：去掉自带侧栏与头部（chrome 上移到壳），换上设计稿的气泡样式。
 */
import { nextTick, onMounted, onUnmounted, ref } from "vue";
import http, { ApiError } from "@/api/http";
import { getMessages } from "@/api/conversation";
import type { Message } from "@/api/types";
import { useStreamConversation } from "@/composables/useStreamConversation";
import { toast } from "@/composables/useToast";

const props = defineProps<{ id: string }>();

const BASE = `/api/v1/addons/conversation/${props.id}`;

const messages = ref<Message[]>([]);
const loading = ref(true);
const followUp = ref("");
const sending = ref(false);

const scroller = ref<HTMLElement | null>(null);
let abort: AbortController | null = null;

const { streamingId, consumeStream } = useStreamConversation(props.id, messages);

async function scrollToBottom(): Promise<void> {
  await nextTick();
  const el = scroller.value;
  if (el) el.scrollTop = el.scrollHeight;
}

function appendMessage(message: Message): void {
  messages.value = [...messages.value, message];
}

async function loadHistory(): Promise<void> {
  loading.value = true;
  try {
    messages.value = await getMessages(props.id);
    await scrollToBottom();
  } catch (error) {
    toast.error(error instanceof ApiError ? error.message : "加载消息失败");
  } finally {
    loading.value = false;
  }
}

async function handleFollowUp(): Promise<void> {
  const text = followUp.value.trim();
  if (!text || sending.value) return;
  sending.value = true;

  appendMessage({
    id: Date.now(),
    conversation_id: Number(props.id),
    sender_type: "user",
    sender_name: "你",
    msg_type: "text",
    content: text,
    created_at: new Date().toISOString(),
  });
  followUp.value = "";
  await scrollToBottom();

  abort?.abort();
  abort = new AbortController();
  try {
    await http.post(`${BASE}/follow-up`, { input: text });
    await consumeStream(abort.signal);
    await scrollToBottom();
  } catch (error) {
    toast.error(error instanceof ApiError ? error.message : "发送失败");
  } finally {
    sending.value = false;
  }
}

onMounted(async () => {
  await loadHistory();
  abort = new AbortController();
  void consumeStream(abort.signal);
});

onUnmounted(() => {
  abort?.abort();
});

// 引用 streamingId 避免 TS 未使用告警（SSE 流式消息高亮由 MessageBubble 内部用）。
void streamingId;
</script>

<template>
  <div class="view active" style="height:100%">
    <div class="chat-view">
      <div ref="scroller" class="chat-messages">
        <p v-if="!loading && messages.length === 0" class="chat-empty">开始你的第一次对话吧</p>

        <div
          v-for="m in messages"
          :key="m.id"
          class="message"
          :class="m.sender_type === 'user' ? 'user' : 'assistant'"
        >
          <div class="message-avatar">
            <template v-if="m.sender_type === 'user'">你</template>
            <svg v-else viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 2L4 7v10l8 5 8-5V7l-8-5z" /><path d="M12 22V12" /><path d="M4 7l8 5 8-5" /></svg>
          </div>
          <div class="message-content">
            <div class="message-author">{{ m.sender_type === "user" ? "你" : "NucleAgent" }}</div>
            <div class="message-bubble" v-html="m.content || ''" />
          </div>
        </div>
      </div>

      <div class="chat-composer-wrap">
        <div class="chat-composer">
          <textarea
            v-model="followUp"
            placeholder="继续对话..."
            :disabled="sending"
            @keydown.enter.exact.prevent="handleFollowUp"
          />
          <div class="composer-toolbar">
            <button class="tool-btn" type="button" title="附件">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21.44 11.05l-9.19 9.19a6 6 0 01-8.49-8.49l9.19-9.19a4 4 0 015.66 5.66l-9.2 9.19a2 2 0 01-2.83-2.83l8.49-8.48" /></svg>
              附件
            </button>
            <button class="tool-btn active" type="button" title="Agent 模式">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10" /><path d="M12 6v6l4 2" /></svg>
              Agent 模式
            </button>
            <button class="composer-btn send" type="button" title="发送" :disabled="sending || !followUp.trim()" @click="handleFollowUp">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="22" y1="2" x2="11" y2="13" /><polygon points="22 2 15 22 11 13 2 9 22 2" /></svg>
            </button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style>
/* 设计稿第 838–1067 行。 */
.chat-view {
  display: flex; flex-direction: column; height: 100%;
  max-width: 820px; margin: 0 auto; width: 100%;
}

.chat-messages {
  flex: 1; overflow-y: auto; padding: 32px 24px;
  display: flex; flex-direction: column; gap: 24px;
}

.chat-empty { color: var(--text-tertiary); text-align: center; padding-top: 40px; }

.message {
  display: flex; gap: 14px; max-width: 100%;
  animation: fade-in-up 0.4s var(--ease-out) both;
}

.message-avatar {
  width: 34px; height: 34px; border-radius: var(--r-md);
  display: flex; align-items: center; justify-content: center;
  flex-shrink: 0; font-weight: 600; font-size: 13px;
}

.message.user .message-avatar {
  background: var(--grad-teal-indigo); background-size: 200% 200%;
  animation: gradient-flow 5s var(--ease) infinite;
  color: white; box-shadow: var(--shadow-glow-teal);
}

.message.assistant .message-avatar {
  background: var(--grad-violet-fuchsia); background-size: 200% 200%;
  animation: gradient-flow 5s var(--ease) infinite;
  color: white; box-shadow: var(--shadow-glow-violet);
}

.message.assistant .message-avatar svg { width: 18px; height: 18px; }

.message-content { flex: 1; min-width: 0; }

.message-author { font-size: 13px; font-weight: 600; color: var(--text-primary); margin-bottom: 4px; }

.message-bubble { font-size: 14px; color: var(--slate-700); line-height: 1.65; }
.message-bubble p { margin-bottom: 8px; }
.message-bubble p:last-child { margin-bottom: 0; }
.message-bubble code {
  font-family: var(--font-mono); font-size: 12.5px;
  background: var(--grad-brand-soft);
  padding: 1px 6px; border-radius: 4px; color: var(--indigo-600);
}

.chat-composer-wrap {
  padding: 12px 24px 20px;
  background: linear-gradient(to top, var(--bg) 60%, transparent);
}

.chat-composer {
  background: rgba(255, 255, 255, 0.9);
  backdrop-filter: blur(20px); -webkit-backdrop-filter: blur(20px);
  border: 1.5px solid var(--border); border-radius: var(--r-xl);
  padding: 6px; box-shadow: var(--shadow-md);
  display: flex; align-items: flex-end; gap: 8px;
  transition: all 0.3s var(--ease);
}

.chat-composer:focus-within {
  border-color: var(--teal-300);
  box-shadow: var(--shadow-lg), 0 0 0 4px rgba(20, 184, 166, 0.08);
  transform: translateY(-2px);
}

.chat-composer textarea {
  flex: 1; border: none; outline: none; resize: none; background: transparent;
  font-family: var(--font-body); font-size: 14.5px; color: var(--text-primary);
  padding: 12px 14px; max-height: 160px; line-height: 1.5;
}

.chat-composer textarea::placeholder { color: var(--text-tertiary); }
.chat-composer textarea:disabled { opacity: 0.6; }

.composer-toolbar {
  display: flex; align-items: center; gap: 2px;
  padding: 4px 4px 4px 8px; border-left: 1px solid var(--border);
}

.tool-btn {
  display: flex; align-items: center; gap: 5px;
  padding: 6px 10px; border-radius: var(--r-sm);
  border: none; background: transparent; cursor: pointer;
  font-size: 12px; font-weight: 500; color: var(--text-tertiary);
  transition: all 0.2s var(--ease); white-space: nowrap;
}

.tool-btn:hover { background: var(--bg-hover); color: var(--text-primary); transform: scale(1.05); }

.tool-btn.active {
  background: var(--grad-brand-soft); color: var(--indigo-600); box-shadow: var(--shadow-xs);
}

.tool-btn svg { width: 14px; height: 14px; }
</style>
