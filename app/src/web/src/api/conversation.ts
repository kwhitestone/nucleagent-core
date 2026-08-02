import http, { ApiError, apiBase, authHeaders } from "./http";
import type {
  Conversation,
  CreateConversationRequest,
  Message,
  StreamEvent,
} from "./types";

const BASE = "/api/v1/addons/conversation";

/**
 * GET /conversation — list conversations for the current user.
 */
export async function listConversations(): Promise<Conversation[]> {
  const response = await http.get<Conversation[]>(BASE);
  return response.data;
}

/**
 * POST /conversation — create a conversation and kick off execution.
 * Body: { mode, input, model? }.
 */
export async function createConversation(
  payload: CreateConversationRequest,
): Promise<Conversation> {
  const response = await http.post<Conversation>(BASE, payload);
  return response.data;
}

/**
 * GET /conversation/:id/messages — message history for a conversation.
 */
export async function getMessages(conversationId: number | string): Promise<Message[]> {
  const response = await http.get<Message[]>(`${BASE}/${conversationId}/messages`);
  return response.data;
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
 * Parse a single SSE frame block into a StreamEvent. A frame is the text
 * between two blank lines; its data lines are JSON-joined and parsed.
 * Returns undefined for keep-alive/comment frames (no data).
 */
function parseFrame(block: string): StreamEvent | undefined {
  const dataLines = block
    .split("\n")
    .filter((line) => line.startsWith("data:"))
    .map((line) => line.slice(5).trimStart());
  if (dataLines.length === 0) return undefined;
  const payload = dataLines.join("\n");
  if (payload === "[DONE]") {
    return { type: "done" };
  }
  try {
    return JSON.parse(payload) as StreamEvent;
  } catch {
    // Malformed JSON — surface as a synthetic error so callers can react.
    return { type: "error", message: `Malformed SSE frame: ${payload}` };
  }
}

/**
 * GET /conversation/:id/messages/stream — SSE subscription.
 *
 * Yields decoded StreamEvents as they arrive. Uses the raw fetch +
 * ReadableStream decoder (axios cannot stream). Handles frames split across
 * chunk boundaries by buffering until a frame terminator (\n\n) is seen.
 *
 * Non-OK responses are thrown as ApiError so callers share the same error path
 * as the REST calls.
 */
export async function* streamMessages(
  conversationId: number | string,
  signal?: AbortSignal,
): AsyncGenerator<StreamEvent, void, unknown> {
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
