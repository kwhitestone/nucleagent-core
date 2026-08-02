<script setup lang="ts">
import { nextTick, onMounted, onUnmounted, ref } from "vue";
import { useRouter } from "vue-router";
import { useI18n } from "vue-i18n";
import { ElMessage } from "element-plus";
import http, { ApiError } from "@/api/http";
import { getMessages } from "@/api/conversation";
import type { Message } from "@/api/types";
import { useStreamConversation } from "@/composables/useStreamConversation";
import LanguageSwitcher from "@/components/LanguageSwitcher.vue";
import ConversationSidebar from "@/components/ConversationSidebar.vue";
import MessageBubble from "@/components/MessageBubble.vue";

const props = defineProps<{ id: string }>();

const { t } = useI18n();
const router = useRouter();

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
    const message = error instanceof ApiError ? error.message : t("conversation.loadFailed");
    ElMessage.error(message);
  } finally {
    loading.value = false;
  }
}

/**
 * Send a follow-up message (POST /conversation/:id/follow-up) and re-subscribe
 * to the stream to receive the agent's reply.
 */
async function handleFollowUp(): Promise<void> {
  const text = followUp.value.trim();
  if (!text || sending.value) return;
  sending.value = true;

  appendMessage({
    id: Date.now(),
    conversation_id: Number(props.id),
    sender_type: "user",
    sender_name: t("conversation.you"),
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
    const message = error instanceof ApiError ? error.message : t("conversation.sendFailed");
    ElMessage.error(message);
  } finally {
    sending.value = false;
  }
}

function back(): void {
  router.push("/");
}

onMounted(async () => {
  await loadHistory();
  // Begin streaming any in-flight reply for this conversation.
  abort = new AbortController();
  void consumeStream(abort.signal);
});

onUnmounted(() => {
  abort?.abort();
});
</script>

<template>
  <div class="conv">
    <ConversationSidebar />

    <main class="conv__main">
      <header class="conv__header">
        <el-button link class="conv__back" @click="back">
          {{ t("conversation.backToWorkbench") }}
        </el-button>
        <LanguageSwitcher />
      </header>

      <div ref="scroller" v-loading="loading" class="conv__stream">
        <div class="conv__stream-inner">
          <p v-if="!loading && messages.length === 0" class="conv__empty">
            {{ t("conversation.empty") }}
          </p>
          <MessageBubble
            v-for="m in messages"
            :key="m.id"
            :message="m"
            :streaming="streamingId === m.id"
          />
        </div>
      </div>

      <footer class="conv__composer">
        <div class="conv__composer-inner">
          <el-input
            v-model="followUp"
            type="textarea"
            :autosize="{ minRows: 1, maxRows: 6 }"
            :placeholder="t('conversation.inputPlaceholder')"
            :disabled="sending"
            resize="none"
            class="conv__input"
            @keydown.enter.exact.prevent="handleFollowUp"
          />
          <button
            class="na-send-btn conv__send"
            :disabled="sending || !followUp.trim()"
            @click="handleFollowUp"
          >
            {{ sending ? t("common.sending") : t("conversation.send") }}
          </button>
        </div>
      </footer>
    </main>
  </div>
</template>
