/**
 * Shared API type definitions matching the nucleagent-core backend contract
 * (see nucleagent-docs/03-data-models.md and 04-api-contracts.md).
 *
 * core endpoints return resources directly (not the numeric { code, message,
 * data } envelope used by auth). Errors use a string `code`.
 */

/** Error envelope returned by core on failure: { code, message }. */
export interface ApiErrorBody {
  code: string;
  message: string;
}

/** Conversation row (conversations table). */
export interface Conversation {
  id: number;
  user_id: number;
  agent_id?: number | null;
  project_id?: number | null;
  title: string;
  mode: ConversationMode;
  status: ConversationStatus;
  provider_id?: number | null;
  model?: string;
  created_at: string;
  updated_at: string;
  completed_at?: string | null;
}

export type ConversationMode = "a2a" | "a2a_agent" | "a2a_employee";
export type ConversationStatus =
  | "drafting"
  | "executing"
  | "blocked"
  | "completed"
  | "failed"
  | "cancelled";

/** Message row (messages table). */
export interface Message {
  id: number;
  conversation_id: number;
  sender_type: MessageSender;
  sender_name: string;
  msg_type: MessageType;
  content: string;
  metadata?: Record<string, unknown>;
  created_at: string;
}

export type MessageSender = "user" | "agent" | "system" | "tool";
export type MessageType =
  | "text"
  | "streaming"
  | "plan"
  | "result"
  | "error"
  | "tool_call"
  | "status";

/** POST /conversation body. */
export interface CreateConversationRequest {
  mode: ConversationMode;
  input: string;
  model?: string;
}

/** Agent template row (agent_templates table, GET /agent/templates item). */
export interface AgentTemplate {
  id: number;
  name: string;
  slug: string;
  config?: Record<string, unknown>;
  i18n?: Record<string, unknown>;
  is_active: boolean;
  created_at: string;
  updated_at: string;
}

/** Skill row (skills table, GET /skill item). */
export interface Skill {
  id: number;
  name: string;
  slug: string;
  config?: Record<string, unknown>;
  i18n?: Record<string, unknown>;
  is_active: boolean;
}

/**
 * Decoded SSE event from GET /conversation/:id/messages/stream.
 *
 * The executor streams delta types over the WebSocket relay; the SSE fan-out
 * surfaces them as `text_delta` / `thinking_delta` / `tool_use` / `done` /
 * `error` / `need_input` (see 04-api-contracts.md §6).
 */
export interface StreamEvent {
  type:
    | "text_delta"
    | "thinking_delta"
    | "tool_use"
    | "plan_uplink"
    | "need_input"
    | "done"
    | "error"
    | "status";
  content?: string;
  text?: string;
  question?: string;
  message?: string;
  tool?: string;
  plan?: unknown;
}
