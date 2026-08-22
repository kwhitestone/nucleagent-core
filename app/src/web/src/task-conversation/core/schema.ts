import { z } from "zod";

import type {
  CommandResult,
  ConversationAttachment,
  ConversationEvent,
  ConversationPage,
  ConversationSnapshot,
} from "./types";

export const MAX_ID_LENGTH = 256;
export const MAX_CURSOR_LENGTH = 512;
export const MAX_MESSAGE_CONTENT_LENGTH = 100_000;
export const MAX_MESSAGE_CONTENT_BYTES = 450_000;
export const MAX_STREAM_DELTA_LENGTH = 65_536;
export const MAX_ATTACHMENTS = 10;
export const MAX_ATTACHMENT_SIZE_BYTES = 25 * 1024 * 1024;
export const MAX_METADATA_BYTES = 65_536;

const DANGEROUS_KEYS = new Set([
  "__proto__",
  "constructor",
  "prototype",
  "__defineGetter__",
  "__defineSetter__",
  "__lookupGetter__",
  "__lookupSetter__",
  "hasOwnProperty",
  "isPrototypeOf",
  "propertyIsEnumerable",
  "toLocaleString",
  "toString",
  "valueOf",
]);

const isSafeMetadataValue = (value: unknown, depth = 0): boolean => {
  if (depth > 8) return false;
  if (value === null || typeof value === "boolean") return true;
  if (typeof value === "number") return Number.isFinite(value);
  if (typeof value === "string") return value.length <= 16_384;
  if (Array.isArray(value)) {
    return (
      value.length <= 100 &&
      value.every((entry) => isSafeMetadataValue(entry, depth + 1))
    );
  }
  if (typeof value !== "object") return false;
  const prototype = Object.getPrototypeOf(value);
  if (prototype !== Object.prototype && prototype !== null) return false;
  const entries = Object.entries(value);
  return (
    entries.length <= 64 &&
    entries.every(
      ([key, entry]) =>
        key.length <= 128 &&
        !DANGEROUS_KEYS.has(key) &&
        isSafeMetadataValue(entry, depth + 1),
    )
  );
};

const hasBoundedSerializedSize = (value: unknown): boolean => {
  try {
    return (
      new TextEncoder().encode(JSON.stringify(value)).byteLength <=
      MAX_METADATA_BYTES
    );
  } catch {
    return false;
  }
};

const BoundedIdSchema = z
  .string()
  .min(1)
  .max(MAX_ID_LENGTH)
  .refine((value) => !DANGEROUS_KEYS.has(value), {
    message: "Reserved identifier is not allowed",
  });
const BoundedContentSchema = z
  .string()
  .refine((value) => Array.from(value).length <= MAX_MESSAGE_CONTENT_LENGTH, {
    message: "Content exceeds the Unicode code point limit",
  })
  .refine(
    (value) =>
      new TextEncoder().encode(value).byteLength <= MAX_MESSAGE_CONTENT_BYTES,
    { message: "Content exceeds the UTF-8 transport limit" },
  );

const isSafeMetadataRecord = (
  value: unknown,
): value is Readonly<Record<string, unknown>> =>
  value !== null &&
  typeof value === "object" &&
  !Array.isArray(value) &&
  isSafeMetadataValue(value);

const MetadataSchema = z
  .custom<
    Readonly<Record<string, unknown>>
  >((value) => isSafeMetadataRecord(value) && hasBoundedSerializedSize(value), { message: "Metadata exceeds safe structural limits" })
  .readonly();

export const ConversationAttachmentSchema = z
  .object({
    id: BoundedIdSchema,
    name: z.string().min(1).max(512),
    url: z
      .string()
      .max(2_048)
      .url()
      .refine(
        (value) => ["http:", "https:"].includes(new URL(value).protocol),
        {
          message: "Attachment URL protocol is not allowed",
        },
      )
      .optional(),
    mimeType: z.string().min(1).max(128).optional(),
    size: z
      .number()
      .int()
      .nonnegative()
      .max(MAX_ATTACHMENT_SIZE_BYTES)
      .optional(),
    metadata: MetadataSchema.optional(),
  })
  .strict();

export const ConversationItemSchema = z
  .object({
    id: BoundedIdSchema,
    turnId: BoundedIdSchema,
    streamId: BoundedIdSchema,
    lane: z.enum([
      "answer",
      "process",
      "progress",
      "tool",
      "interaction",
      "system",
    ]),
    role: z.enum(["user", "assistant", "system", "tool"]),
    kind: z.string().min(1).max(128),
    content: BoundedContentSchema,
    status: z.enum(["pending", "streaming", "complete", "failed", "cancelled"]),
    revision: z.number().int().nonnegative(),
    seq: z.number().int().nonnegative(),
    timestamp: z.iso.datetime({ offset: true }),
    clientMessageId: BoundedIdSchema.optional(),
    title: z.string().max(512).optional(),
    userReadable: z.boolean().optional(),
    attachments: z
      .array(ConversationAttachmentSchema)
      .max(MAX_ATTACHMENTS)
      .readonly()
      .optional(),
    data: MetadataSchema.optional(),
  })
  .strict();

export const ConversationSnapshotSchema = z
  .object({
    items: z.array(ConversationItemSchema).max(200).readonly(),
    status: z.enum(["idle", "running", "completed", "failed", "cancelled"]),
    cursor: z.string().max(MAX_CURSOR_LENGTH).optional(),
    olderCursor: z.string().max(MAX_CURSOR_LENGTH).optional(),
    hasOlder: z.boolean(),
  })
  .strict();

export const ConversationPageSchema = z
  .object({
    items: z.array(ConversationItemSchema).max(200).readonly(),
    cursor: z.string().max(MAX_CURSOR_LENGTH).optional(),
    hasOlder: z.boolean(),
  })
  .strict();

export const CommandResultSchema = z
  .object({
    accepted: z.boolean(),
    message: z.string().max(4_096).optional(),
    item: ConversationItemSchema.optional(),
  })
  .strict();

const ConversationEventBaseSchema = z
  .object({
    protocolVersion: z.literal(2),
    conversationKey: BoundedIdSchema,
    eventId: BoundedIdSchema,
    cursor: z.string().max(MAX_CURSOR_LENGTH),
    turnId: BoundedIdSchema,
    streamId: BoundedIdSchema,
    lane: z.enum([
      "answer",
      "process",
      "progress",
      "tool",
      "interaction",
      "system",
    ]),
    seq: z.number().int().nonnegative(),
    revision: z.number().int().nonnegative(),
    timestamp: z.iso.datetime({ offset: true }),
  })
  .strict();

export const SnapshotEventSchema = ConversationEventBaseSchema.extend({
  type: z.literal("snapshot"),
  snapshot: ConversationSnapshotSchema,
});

export const ItemUpsertEventSchema = ConversationEventBaseSchema.extend({
  type: z.literal("item.upsert"),
  item: ConversationItemSchema,
});

export const StreamAppendEventSchema = ConversationEventBaseSchema.extend({
  type: z.literal("stream.append"),
  delta: z
    .string()
    .refine((value) => Array.from(value).length <= MAX_STREAM_DELTA_LENGTH, {
      message: "Delta exceeds the Unicode code point limit",
    }),
});

export const StreamCompleteEventSchema = ConversationEventBaseSchema.extend({
  type: z.literal("stream.complete"),
  content: BoundedContentSchema,
});

export const ItemRemoveEventSchema = ConversationEventBaseSchema.extend({
  type: z.literal("item.remove"),
  itemId: BoundedIdSchema,
});

export const ConversationStatusEventSchema = ConversationEventBaseSchema.extend(
  {
    type: z.literal("conversation.status"),
    status: z.enum(["idle", "running", "completed", "failed", "cancelled"]),
    message: z.string().max(4_096).optional(),
  },
);

export const ConversationEventSchema = z.discriminatedUnion("type", [
  SnapshotEventSchema,
  ItemUpsertEventSchema,
  StreamAppendEventSchema,
  StreamCompleteEventSchema,
  ItemRemoveEventSchema,
  ConversationStatusEventSchema,
]);

export const parseConversationEvent = (input: unknown): ConversationEvent =>
  ConversationEventSchema.parse(input) as ConversationEvent;

export const parseConversationSnapshot = (
  input: unknown,
): ConversationSnapshot =>
  ConversationSnapshotSchema.parse(input) as ConversationSnapshot;

export const parseConversationPage = (input: unknown): ConversationPage =>
  ConversationPageSchema.parse(input) as ConversationPage;

export const parseConversationAttachment = (
  input: unknown,
): ConversationAttachment =>
  ConversationAttachmentSchema.parse(input) as ConversationAttachment;

export const parseCommandResult = (input: unknown): CommandResult =>
  CommandResultSchema.parse(input) as CommandResult;
