export const CONVERSATION_PROTOCOL_VERSION = 2 as const;

export type ConversationLane =
  | "answer"
  | "process"
  | "progress"
  | "tool"
  | "interaction"
  | "system";

export type ConversationRole = "user" | "assistant" | "system" | "tool";
export type ConversationItemStatus =
  | "pending"
  | "streaming"
  | "complete"
  | "failed"
  | "cancelled";
export type ConversationStatus =
  | "idle"
  | "running"
  | "completed"
  | "failed"
  | "cancelled";
export type ConnectionStatus =
  | "idle"
  | "connecting"
  | "connected"
  | "reconnecting"
  | "disconnected"
  | "error";

export interface ConversationAttachment {
  id: string;
  name: string;
  url?: string;
  mimeType?: string;
  size?: number;
  metadata?: Readonly<Record<string, unknown>>;
}

export interface ConversationItem {
  id: string;
  turnId: string;
  streamId: string;
  lane: ConversationLane;
  role: ConversationRole;
  kind: string;
  content: string;
  status: ConversationItemStatus;
  revision: number;
  seq: number;
  timestamp: string;
  clientMessageId?: string;
  title?: string;
  userReadable?: boolean;
  attachments?: readonly ConversationAttachment[];
  data?: Readonly<Record<string, unknown>>;
}

export interface ConversationSnapshot {
  items: readonly ConversationItem[];
  status: ConversationStatus;
  cursor?: string;
  olderCursor?: string;
  hasOlder: boolean;
}

export interface ConversationPage {
  items: readonly ConversationItem[];
  cursor?: string;
  hasOlder: boolean;
}

interface ConversationEventBase {
  protocolVersion: typeof CONVERSATION_PROTOCOL_VERSION;
  conversationKey: string;
  eventId: string;
  cursor: string;
  turnId: string;
  streamId: string;
  lane: ConversationLane;
  seq: number;
  revision: number;
  timestamp: string;
}

export interface SnapshotEvent extends ConversationEventBase {
  type: "snapshot";
  snapshot: ConversationSnapshot;
}

export interface ItemUpsertEvent extends ConversationEventBase {
  type: "item.upsert";
  item: ConversationItem;
}

export interface StreamAppendEvent extends ConversationEventBase {
  type: "stream.append";
  delta: string;
}

export interface StreamCompleteEvent extends ConversationEventBase {
  type: "stream.complete";
  content: string;
}

export interface ItemRemoveEvent extends ConversationEventBase {
  type: "item.remove";
  itemId: string;
}

export interface ConversationStatusEvent extends ConversationEventBase {
  type: "conversation.status";
  status: ConversationStatus;
  message?: string;
}

export type ConversationEvent =
  | SnapshotEvent
  | ItemUpsertEvent
  | StreamAppendEvent
  | StreamCompleteEvent
  | ItemRemoveEvent
  | ConversationStatusEvent;

export interface SnapshotRequest {
  conversationKey: string;
  limit?: number;
  signal: AbortSignal;
}

export interface OlderPageRequest {
  conversationKey: string;
  cursor?: string;
  limit?: number;
  signal: AbortSignal;
}

export interface SubscribeRequest {
  conversationKey: string;
  cursor?: string;
  signal: AbortSignal;
}

export interface UploadOptions {
  conversationKey: string;
  signal: AbortSignal;
}

export type ConversationCommand =
  | {
      type: "send";
      content: string;
      clientMessageId: string;
      attachments?: readonly ConversationAttachment[];
    }
  | { type: "stop" }
  | { type: "retry"; itemId?: string; turnId?: string }
  | { type: "rerun"; turnId?: string }
  | { type: "interaction.respond"; interactionId: string; value: unknown }
  | {
      type: "feedback";
      itemId: string;
      value: "up" | "down";
      reasons?: readonly string[];
      comment?: string;
    };

export interface CommandResult {
  accepted: boolean;
  message?: string;
  item?: ConversationItem;
}

export interface ConversationAdapter {
  loadSnapshot(request: SnapshotRequest): Promise<ConversationSnapshot>;
  loadOlder(request: OlderPageRequest): Promise<ConversationPage>;
  subscribe(request: SubscribeRequest): AsyncIterable<ConversationEvent>;
  execute(
    command: ConversationCommand,
    options: { signal: AbortSignal },
  ): Promise<CommandResult>;
  uploadAttachment?(
    file: File,
    options: UploadOptions,
  ): Promise<ConversationAttachment>;
}

export interface ConversationCapabilities {
  send?: boolean;
  stop?: boolean;
  retry?: boolean;
  rerun?: boolean;
  attachments?: boolean;
  feedback?: boolean;
  share?: boolean;
  diagnostics?: boolean;
}

export interface ConversationMessages {
  composerPlaceholder: string;
  send: string;
  stop: string;
  retry: string;
  rerun: string;
  attach: string;
  loading: string;
  loadOlder: string;
  jumpLatest: string;
  reconnecting: string;
  disconnected: string;
  empty: string;
  process: string;
  error: string;
  share: string;
  helpful: string;
  notHelpful: string;
  diagnostics: string;
  removeAttachment: string;
}

export type ConversationLocale = "zh-CN" | "en-US";
export type ConversationSurface = "auto" | "desktop" | "mobile";
export type ConversationTheme = "auto" | "light" | "dark";
