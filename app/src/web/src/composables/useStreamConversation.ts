import { ref, type Ref } from "vue";
import { useI18n } from "vue-i18n";
import { ApiError } from "@/api/http";
import { streamMessages } from "@/api/conversation";
import type { Message, SSEMessageEvent } from "@/api/types";

/**
 * 管理单个对话的实时 SSE 订阅。
 *
 * 后端 SSE 推送完整 Message 对象（message-created / message-updated /
 * message-deleted 事件），本 composable 把它们 upsert 到消息列表：
 *   - message-created：按 id 去重后追加（follow-up 的 user 消息已被视图手动
 *     append，SSE 也会推同一条 → 靠 id 去重，不重复）。
 *   - message-updated：替换同 id 消息（streaming 行边写边更新 content）。
 *   - message-deleted：移除。
 *
 * 这是一个**长期连接**：在 onMounted 时启动，整个对话页存活期间持续接收
 * 新消息。follow-up 只需 POST 触发后端执行，流式 agent 回复会经此连接
 * 自动推过来——不要在 follow-up 里再开第二个 consumeStream。
 */
export function useStreamConversation(
  conversationId: string,
  messages: Ref<Message[]>,
) {
  const { t } = useI18n();
  /** 当前正在流式输出的消息 id（用于 UI 高亮），无则为 null。 */
  const streamingId = ref<number | null>(null);

  function upsert(msg: Message): void {
    const idx = messages.value.findIndex((m) => m.id === msg.id);
    if (idx >= 0) {
      const next = messages.value.slice();
      next[idx] = msg;
      messages.value = next;
    } else {
      messages.value = [...messages.value, msg];
    }
  }

  function removeById(id: number): void {
    messages.value = messages.value.filter((m) => m.id !== id);
  }

  function handleEvent(ev: SSEMessageEvent): void {
    if (!ev.message) return;
    const msg = ev.message;
    switch (ev.event) {
      case "message-created":
      case "message-updated":
        upsert(msg);
        if (msg.msgType === "streaming") {
          streamingId.value = msg.id;
        } else if (streamingId.value !== null) {
          // 非 streaming 消息到达且当前有 streaming 高亮 → 清除（终态消息如 result）。
          streamingId.value = null;
        }
        break;
      case "message-deleted":
        removeById(ev.id);
        break;
    }
  }

  /**
   * 启动长期 SSE 订阅；signal abort 后安全退出。
   * 阻塞循环（broker channel 持续到连接断开），调用方不应 await 它来
   * 判断「执行完成」——完成仅表现为收到 msgType=result 的消息。
   */
  async function consumeStream(signal: AbortSignal, overrideId?: string): Promise<void> {
    const id = overrideId ?? conversationId;
    try {
      for await (const ev of streamMessages(id, signal)) {
        handleEvent(ev);
      }
    } catch (error) {
      if (signal.aborted) return;
      const text = error instanceof ApiError ? error.message : t("conversation.sendFailed");
      console.warn("[useStreamConversation] SSE error:", text);
    }
  }

  return { streamingId, consumeStream };
}
