import { ref, type Ref } from "vue";
import { useI18n } from "vue-i18n";
import { ApiError } from "@/api/http";
import { streamMessages } from "@/api/conversation";
import type { Message, StreamEvent } from "@/api/types";

/**
 * Owns the live SSE stream for a single conversation: appends a placeholder
 * agent bubble, folds `text_delta` events into it, and finalizes on `done`.
 *
 * Extracted from Conversation.vue so the view stays under the file-size budget.
 * Callers pass the reactive message list they own; this composable mutates it
 * immutably and tracks which message id is currently streaming.
 */
export function useStreamConversation(
  conversationId: string,
  messages: Ref<Message[]>,
) {
  const { t } = useI18n();
  const streamingId = ref<number | null>(null);

  function appendMessage(message: Message): void {
    messages.value = [...messages.value, message];
  }

  function patchMessage(id: number, patch: Partial<Message>): void {
    messages.value = messages.value.map((m) => (m.id === id ? { ...m, ...patch } : m));
  }

  function handleEvent(event: StreamEvent, messageId: number): void {
    const current = messages.value.find((m) => m.id === messageId);
    const base = current?.content ?? "";
    switch (event.type) {
      case "text_delta":
      case "thinking_delta":
        if (event.content) patchMessage(messageId, { content: base + event.content });
        break;
      case "done":
        patchMessage(messageId, { content: event.text ?? base });
        break;
      case "error":
        patchMessage(messageId, {
          msg_type: "error",
          content: base + (base ? "\n\n" : "") + `⚠️ ${event.message ?? ""}`,
        });
        break;
      case "need_input":
        patchMessage(messageId, {
          content: base + (base ? "\n\n" : "") + `❓ ${event.question ?? ""}`,
        });
        break;
      default:
        // tool_use / plan_uplink / status surface only via metadata.
        break;
    }
  }

  /** Subscribe to the SSE stream; safe to abort via the supplied signal. */
  async function consumeStream(signal: AbortSignal): Promise<void> {
    const placeholder: Message = {
      id: -Date.now(),
      conversation_id: Number(conversationId),
      sender_type: "agent",
      sender_name: t("conversation.agent"),
      msg_type: "streaming",
      content: "",
      created_at: new Date().toISOString(),
    };
    appendMessage(placeholder);
    streamingId.value = placeholder.id;

    try {
      for await (const event of streamMessages(conversationId, signal)) {
        handleEvent(event, placeholder.id);
      }
    } catch (error) {
      if (signal.aborted) return;
      const text = error instanceof ApiError ? error.message : t("conversation.sendFailed");
      patchMessage(placeholder.id, {
        msg_type: "error",
        content: (placeholder.content ? placeholder.content + "\n\n" : "") + `⚠️ ${text}`,
      });
    } finally {
      if (streamingId.value === placeholder.id) {
        patchMessage(placeholder.id, { msg_type: "text" });
        streamingId.value = null;
      }
    }
  }

  return { streamingId, consumeStream };
}
