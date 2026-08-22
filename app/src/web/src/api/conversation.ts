import http, { ApiError, apiBase, authHeaders } from "./http";
import type {
  AttachmentRef,
  Conversation,
  CreateConversationRequest,
  Message,
  ModelChoice,
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

/** 列表分页参数：beforeId 为上一页最小 id 游标，limit 控制每页大小。 */
export interface ListConversationsParams {
  beforeId?: number;
  limit?: number;
}

/** 列表响应：data 为本页对话，hasMore 指示是否还有下一页。 */
export interface ListConversationsResult {
  data: Conversation[];
  hasMore: boolean;
}

/**
 * GET /conversation — list conversations for the current user (游标分页).
 *
 * 后端返回 { code, data: Conversation[], hasMore }。beforeId 省略时拉首页；
 * 翻页时传上一页最小 id。解包取 { data, hasMore }。
 */
export async function listConversations(
  params?: ListConversationsParams,
): Promise<ListConversationsResult> {
  const response = await http.get<Envelope<Conversation[]> & { hasMore?: boolean }>(BASE, {
    params: {
      beforeId: params?.beforeId,
      limit: params?.limit,
    },
  });
  return {
    data: response.data?.data ?? [],
    hasMore: response.data?.hasMore ?? false,
  };
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
  model?: ModelChoice,
): Promise<Conversation> {
  // 无附件/未切换模型时不带对应字段，请求体与改动前逐字节一致。
  const body: Record<string, unknown> = { input };
  if (attachments?.length) body.attachments = attachments;
  if (model) {
    body.providerId = model.providerId;
    body.model = model.model;
  }
  const response = await http.post<Envelope<Conversation>>(
    `${BASE}/${conversationId}/follow-up`,
    body,
  );
  return response.data?.data as Conversation;
}

/**
 * POST /conversation/:id/cancel — 取消正在执行的对话（后端取消 runner，
 * 对话置 cancelled）。对话未在执行时后端返回 404。
 */
export async function cancelConversation(conversationId: number | string): Promise<void> {
  await http.post(`${BASE}/${conversationId}/cancel`);
}

/**
 * PATCH /conversation/:id — 切换对话使用的模型/提供商。
 *
 * 只落库，下一轮执行才生效（后端会在下次 dispatch 时让 executor 重建
 * hermes session —— 模型是建 session 时固化的，不重建改不掉）。
 */
export async function updateConversationModel(
  conversationId: number | string,
  model: ModelChoice,
): Promise<Conversation> {
  const response = await http.patch<Envelope<Conversation>>(`${BASE}/${conversationId}`, {
    providerId: model.providerId,
    model: model.model,
  });
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
