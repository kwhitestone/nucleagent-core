<script setup lang="ts">
/**
 * 对话视图 —— 对齐 design/nucleagent-design.html 第 1849–1926 行。
 *
 * 消息列表（用户/助手气泡）+ 输入框工具栏。
 * 替代旧版：去掉自带侧栏与头部（chrome 上移到壳），换上设计稿的气泡样式。
 */
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { marked } from "marked";
import DOMPurify from "dompurify";
import { ApiError } from "@/api/http";
import { followUp as followUpApi, getMessages } from "@/api/conversation";
import type { Message, MessageAttachment } from "@/api/types";
import { useStreamConversation } from "@/composables/useStreamConversation";
import { toast } from "@/composables/useToast";
import AttachmentPicker from "@/components/AttachmentPicker.vue";
import AttachmentChips from "@/components/AttachmentChips.vue";

/**
 * 读取一条消息上的附件清单。
 *
 * 后端写在 metadata.attachments（model/message.go 预留的 key）。metadata 的类型是
 * Record<string, unknown>，这里做一次运行时窄化 —— 坏数据按无附件处理，不让一条
 * 脏记录把整个消息列表炸掉。
 */
function messageAttachments(m: Message): MessageAttachment[] {
  const raw = m.metadata?.attachments;
  if (!Array.isArray(raw)) return [];
  return raw.filter(
    (a): a is MessageAttachment =>
      typeof a === "object" && a !== null && typeof (a as MessageAttachment).fileId === "string",
  );
}

/** Markdown → 安全 HTML（防 XSS）。 */
function renderMarkdown(text: string): string {
  if (!text) return "";
  const html = marked.parse(text, { breaks: true, async: false }) as string;
  return DOMPurify.sanitize(html);
}

const props = defineProps<{ id: string }>();
const { t } = useI18n();

const messages = ref<Message[]>([]);
const loading = ref(true);
const followUp = ref("");
const sending = ref(false);
/** 本轮追问要带的附件（选中即已上传，这里只持有引用）。 */
const followUpAttachments = ref<MessageAttachment[]>([]);

/** 显示用户消息（text）、流式回复（streaming）、最终回复（result）、
 *  工具调用（tool_call）、错误（error）。
 *  agent.thinking 的 streaming 行（含子代理输出）保留，但**同轮已出错时隐藏**——
 *  否则后端失败后前端仍挂着「正在处理…」转圈，永远不告诉用户出了什么事。
 *  主代理的 streaming 行在有同轮 result 后隐藏（避免重复）。 */
const visibleMessages = computed(() => {
  const resultDelegations = new Set(
    messages.value
      .filter((m) => m.msgType === "result" && m.metadata?.delegation_id)
      .map((m) => m.metadata?.delegation_id as string),
  );
  // 已出错的 delegation：其 streaming 行（含 agent.thinking）不再显示，
  // 由 error 气泡取代，避免「转圈 + 无输出」的静默失败。
  const erroredDelegations = new Set(
    messages.value
      .filter((m) => m.msgType === "error" && m.metadata?.delegation_id)
      .map((m) => m.metadata?.delegation_id as string),
  );
  return messages.value.filter((m) => {
    if (m.msgType === "text" || m.msgType === "result") return true;
    // error 必须渲染：这是用户唯一能看到失败原因的地方。
    if (m.msgType === "error") return true;
    if (m.msgType === "tool_call") {
      return (m.content || "").trim() !== "";
    }
    if (m.msgType === "streaming") {
      const del = m.metadata?.delegation_id as string;
      // 本轮已出错 → 隐藏残留的 streaming 行
      if (del && erroredDelegations.has(del)) return false;
      // agent.thinking 行（思考过程 + 子代理输出）保留
      if (m.senderName === "agent.thinking") return true;
      // 主代理 streaming 行：同轮 result 存在则隐藏
      if (del && resultDelegations.has(del)) return false;
      return true;
    }
    return false;
  });
});

const scroller = ref<HTMLElement | null>(null);
let abort: AbortController | null = null;

const { streamingId, consumeStream } = useStreamConversation(props.id, messages);

async function scrollToBottom(): Promise<void> {
  await nextTick();
  const el = scroller.value;
  if (el) el.scrollTop = el.scrollHeight;
}

/** 格式化消息时间（精确到秒）：2026-08-09 12:34:56 */
function formatTime(iso: string): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}

// 消息列表变化时自动滚到底（SSE 推送的 agent 消息异步到达）。
watch(
  () => visibleMessages.value.length,
  () => { void scrollToBottom(); },
);

async function loadHistory(): Promise<void> {
  loading.value = true;
  try {
    messages.value = await getMessages(props.id);
    await scrollToBottom();
  } catch (error) {
    toast.error(error instanceof ApiError ? error.message : t("conversation.loadMessagesFailed"));
  } finally {
    loading.value = false;
  }
}

/**
 * 发送 follow-up（多轮对话）。
 *
 * 只 POST 触发后端执行——流式 agent 回复由 onMounted 建立的**长期 SSE 连接**
 * 自动推送并渲染（见 useStreamConversation）。不要在这里再开第二个 SSE 订阅，
 * 那会阻塞 sending 永不解锁（broker channel 在连接存活期间持续阻塞）。
 *
 * sending 在收到本轮终态消息（result/error）后由下方 watch 解锁。
 */
async function handleFollowUp(): Promise<void> {
  const text = followUp.value.trim();
  if (!text || sending.value) return;
  sending.value = true;
  followUp.value = "";
  // 先取快照再清空：失败时要能原样恢复，不能让用户重新选一遍附件。
  const atts = followUpAttachments.value.map((a) => ({ fileId: a.fileId, name: a.name }));
  const attsBackup = followUpAttachments.value;
  followUpAttachments.value = [];

  try {
    await followUpApi(props.id, text, atts);
    // 不 await consumeStream；agent 回复经长期 SSE 连接到达。
  } catch (error) {
    toast.error(error instanceof ApiError ? error.message : t("conversation.sendFailed"));
    // 复原输入与附件，用户可直接重试。
    followUp.value = text;
    followUpAttachments.value = attsBackup;
    sending.value = false;
  }
}

// 标记本轮是否已出现过 streaming（用于判断执行周期，避免被初始历史误触发）。
let sawStreamingThisCycle = false;
watch(
  () => streamingId.value,
  (cur, prev) => {
    // 从 null -> 有值：新一轮 streaming 开始。
    if (cur !== null) sawStreamingThisCycle = true;
    // 从有值 -> null 且本轮有过 streaming：终态到达，解锁输入框。
    if (cur === null && prev !== null && sawStreamingThisCycle && sending.value) {
      sending.value = false;
      sawStreamingThisCycle = false;
    }
  },
);

// error 是终态：后端已置 conversation.status=failed，本轮不会再有消息。
// 单独解锁输入框——否则失败后输入框一直 disabled，用户既看不到原因也重试不了。
watch(
  () => messages.value.filter((m) => m.msgType === "error").length,
  (cur, prev) => {
    if (cur > prev && sending.value) {
      sending.value = false;
      sawStreamingThisCycle = false;
    }
  },
);

onMounted(async () => {
  await loadHistory();
  abort = new AbortController();
  void consumeStream(abort.signal);
});

// 路由在同组件内切换（/chat/75 → /chat/73）时 onMounted 不再触发，
// 需 watch props.id 重新加载消息 + 重建 SSE 流，否则主区域停在旧对话内容。
watch(
  () => props.id,
  async (newId, oldId) => {
    if (newId === oldId) return;
    // 中断旧 SSE 流。
    abort?.abort();
    messages.value = [];
    sawStreamingThisCycle = false;
    sending.value = false;
    // 重新加载新对话消息 + 重建 SSE（传新 id）。
    await loadHistory();
    abort = new AbortController();
    void consumeStream(abort.signal, newId);
  },
);

onUnmounted(() => {
  abort?.abort();
});

// 引用 streamingId 避免 TS 未使用告警（SSE 流式消息高亮由 MessageBubble 内部用）。
void streamingId;
</script>

<template>
  <div class="view active view--scroll-hidden">
    <div class="chat-view">
      <div ref="scroller" class="chat-messages">
        <p v-if="!loading && visibleMessages.length === 0" class="chat-empty">{{ t('conversation.empty') }}</p>

        <template v-for="m in visibleMessages" :key="m.id">
          <!-- tool_call：紧凑工具标签条（显示 agent 正在做什么） -->
          <div v-if="m.msgType === 'tool_call'" class="tool-tag">
            <span class="tool-icon">🔧</span>
            <span class="tool-name">{{ m.senderName }}</span>
            <span class="tool-detail">{{ m.content || '...' }}</span>
          </div>
          <!-- error：失败原因（不渲染 markdown，原样展示后端错误文本） -->
          <div v-else-if="m.msgType === 'error'" class="error-bubble">
            <span class="error-icon">⚠</span>
            <div class="error-body">
              <div class="error-title">{{ t('conversation.failed') }}</div>
              <div class="error-detail">{{ m.content || t('conversation.sendFailed') }}</div>
              <div v-if="m.createdAt" class="error-time">{{ formatTime(m.createdAt) }}</div>
            </div>
          </div>
          <!-- 正常消息气泡 -->
          <div v-else class="message" :class="m.senderType === 'user' ? 'user' : 'assistant'">
          <div class="message-avatar">
            <template v-if="m.senderType === 'user'">{{ t('conversation.you') }}</template>
            <svg v-else viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 2L4 7v10l8 5 8-5V7l-8-5z" /><path d="M12 22V12" /><path d="M4 7l8 5 8-5" /></svg>
          </div>
          <div class="message-content">
            <div class="message-header">
              <span class="message-author">{{ m.senderType === "user" ? t('conversation.you') : t('home.greeting') }}</span>
              <span v-if="m.createdAt" class="message-time">{{ formatTime(m.createdAt) }}</span>
            </div>
            <div class="message-bubble" v-html="renderMarkdown(m.content || '')" />
            <!-- 附件必须是真实 Vue 节点：气泡内容过 DOMPurify，注入的元素点不动。 -->
            <AttachmentChips :attachments="messageAttachments(m)" />
          </div>
          </div>
        </template>
      </div>

      <div class="chat-composer-wrap">
        <div class="chat-composer">
          <textarea
            v-model="followUp"
            :placeholder="t('conversation.followUpPlaceholder')"
            :disabled="sending"
            @keydown.enter.exact.prevent="handleFollowUp"
          />
          <div class="composer-toolbar">
            <AttachmentPicker v-model="followUpAttachments" :disabled="sending" show-label />
            <button class="tool-btn active" type="button" :title="t('conversation.agentMode')">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10" /><path d="M12 6v6l4 2" /></svg>
              {{ t('conversation.agentMode') }}
            </button>
            <button class="composer-btn send" type="button" :title="t('common.send')" :disabled="sending || !followUp.trim()" @click="handleFollowUp">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="22" y1="2" x2="11" y2="13" /><polygon points="22 2 15 22 11 13 2 9 22 2" /></svg>
            </button>
          </div>
          <AttachmentChips
            :attachments="followUpAttachments"
            removable
            @remove="(id: string) => (followUpAttachments = followUpAttachments.filter((a) => a.fileId !== id))"
          />
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

/* 工具调用标签条（紧凑显示 agent 正在做什么） */
.tool-tag {
  display: flex; align-items: center; gap: 6px;
  padding: 4px 12px; margin-left: 48px;
  font-size: 12px; color: var(--text-tertiary);
  background: var(--bg-hover); border-radius: var(--r-sm);
  width: fit-content; max-width: 80%;
}
.tool-icon { font-size: 12px; }
.tool-name { font-weight: 600; color: var(--indigo-600); }
.tool-detail { color: var(--text-tertiary); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

/* 错误气泡：失败原因必须显眼且可选中复制（排查用） */
.error-bubble {
  display: flex; gap: 10px; align-items: flex-start;
  margin-left: 48px; max-width: 85%; width: fit-content;
  padding: 10px 14px;
  background: #fef2f2; border: 1px solid #fecaca;
  border-radius: var(--r-md);
  animation: fade-in-up 0.4s var(--ease-out) both;
}
.error-icon { font-size: 14px; line-height: 1.5; color: #dc2626; flex-shrink: 0; }
.error-body { min-width: 0; }
.error-title { font-size: 13px; font-weight: 600; color: #b91c1c; margin-bottom: 2px; }
.error-detail {
  font-family: var(--font-mono); font-size: 12.5px; line-height: 1.55;
  color: #7f1d1d; word-break: break-word; user-select: text;
}
.error-time { font-size: 11px; color: #b91c1c; opacity: 0.7; margin-top: 4px; font-variant-numeric: tabular-nums; }

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

.message-author { font-size: 13px; font-weight: 600; color: var(--text-primary); }

.message-header { display: flex; align-items: baseline; gap: 8px; margin-bottom: 4px; }
.message-time { font-size: 11px; color: var(--text-tertiary); font-variant-numeric: tabular-nums; }

.message-bubble { font-size: 14px; color: var(--slate-700); line-height: 1.65; }
.message-bubble p { margin-bottom: 8px; }
.message-bubble p:last-child { margin-bottom: 0; }
.message-bubble code {
  font-family: var(--font-mono); font-size: 12.5px;
  background: var(--grad-brand-soft);
  padding: 1px 6px; border-radius: 4px; color: var(--indigo-600);
}
.message-bubble pre {
  background: #1e293b; color: #e2e8f0; border-radius: 8px;
  padding: 12px 16px; overflow-x: auto; margin: 8px 0;
}
.message-bubble pre code {
  background: none; color: inherit; padding: 0; font-size: 13px;
}
.message-bubble table {
  border-collapse: collapse; margin: 8px 0; width: 100%; font-size: 13px;
}
.message-bubble th, .message-bubble td {
  border: 1px solid var(--border); padding: 6px 10px; text-align: left;
}
.message-bubble th { background: var(--bg-hover); font-weight: 600; }
.message-bubble ul, .message-bubble ol { padding-left: 20px; margin: 6px 0; }
.message-bubble h1, .message-bubble h2, .message-bubble h3 {
  font-size: 15px; font-weight: 600; margin: 10px 0 6px;
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
