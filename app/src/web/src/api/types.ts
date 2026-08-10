/**
 * Shared API type definitions matching the nucleagent-core backend contract
 * (see nucleagent-docs/03-data-models.md and 04-api-contracts.md).
 *
 * 后端 Go struct 的 json tag 全部使用 camelCase（userId/createdAt/senderType
 * 等），前端类型定义必须与之对齐。后端列表/详情返回 { code, message, data }
 * 信封，由 api/conversation.ts 的 Envelope<T> 解包。
 *
 * Errors use a string `code`.
 */

/** Error envelope returned by core on failure: { code, message }. */
export interface ApiErrorBody {
  code: string;
  message: string;
}

/** Conversation row (conversations table). 后端 json tag 是 camelCase。 */
export interface Conversation {
  id: number;
  userId: number;
  agentId?: number | null;
  projectId?: number | null;
  title: string;
  mode: ConversationMode;
  status: ConversationStatus;
  providerId?: number | null;
  model?: string;
  createdAt: string;
  updatedAt: string;
  completedAt?: string | null;
}

export type ConversationMode = "a2a" | "a2a_agent" | "a2a_employee";
export type ConversationStatus =
  | "drafting"
  | "executing"
  | "blocked"
  | "completed"
  | "failed"
  | "cancelled";

/** Message row (messages table). 后端 json tag 是 camelCase。 */
export interface Message {
  id: number;
  conversationId: number;
  senderType: MessageSender;
  senderName: string;
  msgType: MessageType;
  content: string;
  metadata?: Record<string, unknown>;
  createdAt: string;
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

/**
 * 消息附件。与后端 a2a.Attachment 同形（camelCase json tag）。
 *
 * 注意没有 url 字段：下载链接由后端按需签发（有效期 1800s），不随消息存储；
 * 前端要下载时调 api/storage 的 getDownloadUrl 现取。
 */
export interface MessageAttachment {
  fileId: string;
  name: string;
  mimeType?: string;
  size?: number;
  sha256?: string;
  /** 由后端按 mimeType 归一，前端只用它选图标。 */
  kind?: "image" | "pdf" | "file";
}

/** 上传完成后回传给后端的附件引用（后端用 fileId 去 storage 核对真实元数据）。 */
export interface AttachmentRef {
  fileId: string;
  name?: string;
}

/** POST /conversation body. */
export interface CreateConversationRequest {
  mode: ConversationMode;
  input: string;
  model?: string;
  /** 暂存执行模式/输出格式等前端-only 元数据（后端暂未持久化，预留给未来字段）。 */
  metadata?: Record<string, unknown>;
  /** 附件引用（先经 storage 上传拿到 fileId）。 */
  attachments?: AttachmentRef[];
}

/** Agent template row (agent_templates table, GET /agent/templates item).
 *  后端返回 camelCase JSON（isActive/createdAt/updatedAt），类型与之对齐。 */
export interface AgentTemplate {
  id: number;
  name: string;
  slug: string;
  config?: {
    category?: string;
    role?: string;
    personality?: string;
    prompt?: string;
    avatar?: string;
    color?: string;
    sort_order?: number;
    [key: string]: unknown;
  };
  i18n?: Record<string, unknown>;
  isActive: boolean;
  createdAt: string;
  updatedAt: string;
}

/**
 * Provider row (providers table, GET /provider item)。
 *
 * ⚠️ 没有 apiKey 字段：后端 model.Provider 的 APIKey json tag 为 "-"，
 * 列表/详情永不回传密钥（见 provider/router/router.go 顶部注释）。
 * 前端只能「写入」新密钥，无法读回既有密钥——UI 必须据此设计
 * （编辑时留空 = 不修改，而不是显示掩码后再提交）。
 */
export interface Provider {
  id: number;
  name: string;
  config?: ProviderConfig;
  isActive: boolean;
  createdAt?: string;
  updatedAt?: string;
}

/** Provider.config JSON —— 与后端 llmproxy.ProviderConfig 同形。 */
export interface ProviderConfig {
  baseUrl?: string;
  /** openai / anthropic */
  apiFormat?: string;
  /** bearer / api_key */
  authScheme?: string;
  models?: string[];
  [key: string]: unknown;
}

/** POST /provider body（apiKey 明文，后端用 MASTER_KEY 加密入库）。 */
export interface CreateProviderRequest {
  name: string;
  apiKey: string;
  config?: ProviderConfig;
  isActive: boolean;
}

/** PATCH /provider/:id body —— 字段全可选；apiKey 省略/留空表示不修改。 */
export interface UpdateProviderRequest {
  name?: string;
  apiKey?: string;
  config?: ProviderConfig;
  isActive?: boolean;
}

/** Skill row (skills table, GET /skill item). 后端返回 camelCase JSON。 */
export interface Skill {
  id: number;
  name: string;
  slug: string;
  config?: Record<string, unknown>;
  i18n?: Record<string, unknown>;
  isActive: boolean;
}

/**
 * Decoded SSE frame from GET /conversation/:id/messages/stream.
 *
 * 后端 SSE 扇出（router.go serveSSE）按 broker 事件推送完整 Message 对象，
 * 帧格式为：
 *   id: <messageId>
 *   event: message-created | message-updated | message-deleted
 *   data: { ...完整 Message(camelCase)... }
 *
 * 即每个 SSE 事件携带的是整条消息（创建/更新/删除），不是增量 delta。
 * 前端按 event 类型 upsert 到消息列表即可。
 */
export type SSEEventName = "message-created" | "message-updated" | "message-deleted";

export interface SSEMessageEvent {
  /** SSE 帧的 event: 字段。 */
  event: SSEEventName;
  /** SSE 帧的 id: 字段（= message id），用于 Last-Event-ID 重连。 */
  id: number;
  /** message-created/updated 时是完整 Message；message-deleted 时是 {id}。 */
  message?: Message;
}
