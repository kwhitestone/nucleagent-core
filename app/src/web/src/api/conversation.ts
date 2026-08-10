import http, { ApiError, apiBase, authHeaders } from "./http";
import type {
  AttachmentRef,
  Conversation,
  CreateConversationRequest,
  Message,
  SSEEventName,
  SSEMessageEvent,
} from "./types";

const BASE = "/api/v1/addons/conversation";

/** 后端列表/详情返回 { code, message, data } 信封；data 才是业务载荷。 */
interface Envelope<T> {
  code?: number;
  message?: string;
  data?: T;
}

/**
 * GET /conversation — list conversations for the current user.
 * 后端返回 { code, data: Conversation[] }，解包取 data。
 */
export async function listConversations(): Promise<Conversation[]> {
  const response = await http.get<Envelope<Conversation[]>>(BASE);
  return response.data?.data ?? [];
}

/**
 * POST /conversation — create a conversation and kick off execution.
 * Body: { mode, input, model? }.
 */
export async function createConversation(
  payload: CreateConversationRequest,
): Promise<Conversation> {
  const response = await http.post<Envelope<Conversation>>(BASE, payload);
  return response.data?.data as Conversation;
}

/**
 * GET /conversation/:id/messages — message history for a conversation.
 */
export async function getMessages(conversationId: number | string): Promise<Message[]> {
  const response = await http.get<Envelope<Message[]>>(`${BASE}/${conversationId}/messages`);
  return response.data?.data ?? [];
}

/**
 * POST /conversation/:id/follow-up — append a message and re-execute (multi-turn).
 */
export async function followUp(
  conversationId: number | string,
  input: string,
  attachments?: AttachmentRef[],
): Promise<Conversation> {
  const response = await http.post<Envelope<Conversation>>(
    `${BASE}/${conversationId}/follow-up`,
    // 无附件时不带该字段，请求体与改动前逐字节一致。
    attachments?.length ? { input, attachments } : { input },
  );
  return response.data?.data as Conversation;
}

/**
 * Build the absolute SSE URL. When apiBase() is empty (standalone dev) the
 * relative URL is fine for fetch; under a micro-app shell apiBase() carries the
 * configured origin.
 */
function streamUrl(conversationId: number | string): string {
  const path = `${BASE}/${conversationId}/messages/stream`;
  return `${apiBase()}${path}`;
}

/**
 * Parse a single SSE frame block into an SSEMessageEvent.
 *
 * 后端帧格式（router.go writeSSEEvent）：
 *   id: <messageId>
 *   event: message-created | message-updated | message-deleted
 *   data: { ...完整 Message... }
 *
 * 返回 undefined 表示 keep-alive/注释帧（无 data）。
 */
function parseFrame(block: string): SSEMessageEvent | undefined {
  const lines = block.split("\n");
  const dataLines = lines
    .filter((l) => l.startsWith("data:"))
    .map((l) => l.slice(5).trimStart());
  if (dataLines.length === 0) return undefined;

  let id = 0;
  let eventName: SSEEventName = "message-created";
  for (const line of lines) {
    if (line.startsWith("id:")) {
      id = Number(line.slice(3).trim()) || 0;
    } else if (line.startsWith("event:")) {
      const v = line.slice(6).trim() as SSEEventName;
      if (v === "message-created" || v === "message-updated" || v === "message-deleted") {
        eventName = v;
      }
    }
  }

  const payload = dataLines.join("\n");
  let message: Message | undefined;
  try {
    message = JSON.parse(payload) as Message;
  } catch {
    return undefined;
  }
  return { event: eventName, id, message };
}

/**
 * GET /conversation/:id/messages/stream — SSE subscription.
 *
 * Yields decoded SSEMessageEvents as they arrive. Uses the raw fetch +
 * ReadableStream decoder (axios cannot stream). Handles frames split across
 * chunk boundaries by buffering until a frame terminator (\n\n) is seen.
 *
 * Non-OK responses are thrown as ApiError so callers share the same error path
 * as the REST calls.
 */
export async function* streamMessages(
  conversationId: number | string,
  signal?: AbortSignal,
): AsyncGenerator<SSEMessageEvent, void, unknown> {
  const response = await fetch(streamUrl(conversationId), {
    method: "GET",
    headers: authHeaders(),
    signal,
  });

  if (!response.ok) {
    let code = `HTTP_${response.status}`;
    let message = `Stream request failed (${response.status})`;
    try {
      const body = (await response.json()) as { code?: string; message?: string };
      if (body.code) code = body.code;
      if (body.message) message = body.message;
    } catch {
      // Body was not JSON; keep the default HTTP-level message.
    }
    throw new ApiError(message, code, response.status);
  }

  const body = response.body;
  if (!body) {
    throw new ApiError("No response body for stream", "NO_BODY", response.status);
  }

  const reader = body.getReader();
  const decoder = new TextDecoder("utf-8");
  let buffer = "";

  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      buffer += decoder.decode(value, { stream: true });
      // SSE frames are separated by a blank line. Split on \n\n and keep the
      // trailing partial frame in the buffer for the next chunk.
      const parts = buffer.split("\n\n");
      buffer = parts.pop() ?? "";
      for (const part of parts) {
        const trimmed = part.trim();
        if (!trimmed) continue;
        const event = parseFrame(trimmed);
        if (event) yield event;
      }
    }
    // Flush any trailing frame that lacked a final terminator.
    const trimmed = buffer.trim();
    if (trimmed) {
      const event = parseFrame(trimmed);
      if (event) yield event;
    }
  } finally {
    reader.releaseLock();
  }
}
