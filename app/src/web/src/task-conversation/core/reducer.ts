import type {
  ConnectionStatus,
  ConversationEvent,
  ConversationItem,
  ConversationPage,
  ConversationStatus,
} from "./types";
import {
  MAX_MESSAGE_CONTENT_BYTES,
  MAX_MESSAGE_CONTENT_LENGTH,
} from "./schema";

export const DEFAULT_PAGE_SIZE = 50;
export const MAX_RENDERED_ITEMS = 200;

interface StreamVersion {
  revision: number;
  seq: number;
}

export interface ConversationConnectionState {
  status: ConnectionStatus;
  needsSnapshot: boolean;
  reason?:
    | "event-gap"
    | "content-limit"
    | "conversation-mismatch"
    | "protocol-mismatch";
  error?: string;
}

export interface ConversationState {
  conversationKey: string;
  items: Readonly<Record<string, ConversationItem>>;
  loadedOrder: readonly string[];
  order: readonly string[];
  windowStart: number;
  status: ConversationStatus;
  cursor?: string;
  olderCursor?: string;
  hasOlder: boolean;
  connection: ConversationConnectionState;
  seenEventIds: readonly string[];
  streamVersions: Readonly<Record<string, StreamVersion>>;
  statusVersion?: StreamVersion;
}

export interface ConversationTurn {
  id: string;
  items: readonly ConversationItem[];
}

export type ConversationAction =
  | { type: "event.received"; event: ConversationEvent }
  | { type: "item.optimistic"; item: ConversationItem }
  | { type: "item.optimistic.removed"; itemId: string }
  | { type: "history.prepended"; page: ConversationPage }
  | { type: "window.latest" }
  | { type: "connection.changed"; status: ConnectionStatus; error?: string }
  | {
      type: "snapshot.requested";
      reason: ConversationConnectionState["reason"];
    };

export const createConversationState = (
  conversationKey: string,
): ConversationState => ({
  conversationKey,
  items: {},
  loadedOrder: [],
  order: [],
  windowStart: 0,
  status: "idle",
  hasOlder: false,
  connection: { status: "idle", needsSnapshot: false },
  seenEventIds: [],
  streamVersions: {},
});

const latestWindowStart = (order: readonly string[]): number =>
  Math.max(0, order.length - MAX_RENDERED_ITEMS);

const windowOrder = (
  order: readonly string[],
  start: number,
): readonly string[] => order.slice(start, start + MAX_RENDERED_ITEMS);

const withSeenEvent = (
  state: ConversationState,
  event: ConversationEvent,
): ConversationState => ({
  ...state,
  cursor: event.cursor,
  seenEventIds: [...state.seenEventIds.slice(-499), event.eventId],
  connection: { ...state.connection, needsSnapshot: false, reason: undefined },
});

const requestSnapshot = (
  state: ConversationState,
  reason: ConversationConnectionState["reason"],
): ConversationState => ({
  ...state,
  connection: { ...state.connection, needsSnapshot: true, reason },
});

const findItemByStream = (
  state: ConversationState,
  streamId: string,
): ConversationItem | undefined =>
  Object.values(state.items).find(
    (candidate) => candidate.streamId === streamId,
  );

const findItemByClientId = (
  state: ConversationState,
  clientMessageId?: string,
): ConversationItem | undefined =>
  clientMessageId
    ? Object.values(state.items).find(
        (candidate) => candidate.clientMessageId === clientMessageId,
      )
    : undefined;

const eventVersion = (event: ConversationEvent): StreamVersion => ({
  revision: event.revision,
  seq: event.seq,
});

const compareVersion = (
  previous: StreamVersion | undefined,
  next: StreamVersion,
): "apply" | "duplicate" | "gap" => {
  if (!previous) return next.revision > 1 || next.seq > 1 ? "gap" : "apply";
  if (next.revision <= previous.revision || next.seq <= previous.seq)
    return "duplicate";
  if (next.revision !== previous.revision + 1 || next.seq !== previous.seq + 1)
    return "gap";
  return "apply";
};

const snapshotState = (
  state: ConversationState,
  event: Extract<ConversationEvent, { type: "snapshot" }>,
) => {
  const items = Object.fromEntries(
    event.snapshot.items.map((current) => [current.id, { ...current }]),
  );
  const loadedOrder = event.snapshot.items.map((current) => current.id);
  const windowStart = latestWindowStart(loadedOrder);
  const order = windowOrder(loadedOrder, windowStart);
  const streamVersions = Object.fromEntries(
    Object.values(items).map((current) => [
      current.streamId,
      { revision: current.revision, seq: current.seq },
    ]),
  );
  return {
    ...state,
    items,
    loadedOrder,
    order,
    windowStart,
    status: event.snapshot.status,
    cursor: event.snapshot.cursor ?? event.cursor,
    olderCursor: event.snapshot.olderCursor,
    hasOlder: event.snapshot.hasOlder,
    seenEventIds: [event.eventId],
    streamVersions,
    statusVersion: undefined,
    connection: { status: "connected" as const, needsSnapshot: false },
  };
};

const upsertItem = (
  state: ConversationState,
  incoming: ConversationItem,
  version: StreamVersion,
): ConversationState => {
  const matchingClientItem = findItemByClientId(
    state,
    incoming.clientMessageId,
  );
  const replacedId =
    matchingClientItem?.id !== incoming.id ? matchingClientItem?.id : undefined;
  const retainedItems = Object.fromEntries(
    Object.entries(state.items).filter(
      ([id]) => id !== replacedId && id !== incoming.id,
    ),
  );
  const nextItems = {
    ...retainedItems,
    [incoming.id]: {
      ...incoming,
      revision: version.revision,
      seq: version.seq,
    },
  };

  const loadedPosition = replacedId
    ? state.loadedOrder.indexOf(replacedId)
    : state.loadedOrder.indexOf(incoming.id);
  const withoutIncoming = state.loadedOrder.filter(
    (id) => id !== incoming.id && id !== replacedId,
  );
  const nextLoadedOrder =
    loadedPosition >= 0
      ? [
          ...withoutIncoming.slice(0, loadedPosition),
          incoming.id,
          ...withoutIncoming.slice(loadedPosition),
        ]
      : [...withoutIncoming, incoming.id];
  const wasAtLatest =
    state.windowStart + state.order.length >= state.loadedOrder.length;
  const windowStart = wasAtLatest
    ? latestWindowStart(nextLoadedOrder)
    : Math.min(state.windowStart, latestWindowStart(nextLoadedOrder));
  const order = windowOrder(nextLoadedOrder, windowStart);

  return {
    ...state,
    items: nextItems,
    loadedOrder: nextLoadedOrder,
    order,
    windowStart,
    streamVersions: { ...state.streamVersions, [incoming.streamId]: version },
  };
};

const applyEvent = (
  state: ConversationState,
  event: ConversationEvent,
): ConversationState => {
  if (event.type === "snapshot") return snapshotState(state, event);

  const itemForEvent =
    event.type === "item.upsert"
      ? (findItemByClientId(state, event.item.clientMessageId) ??
        state.items[event.item.id])
      : event.type === "item.remove"
        ? state.items[event.itemId]
        : findItemByStream(state, event.streamId);
  const previousVersion =
    event.type === "conversation.status"
      ? state.statusVersion
      : itemForEvent
        ? { revision: itemForEvent.revision, seq: itemForEvent.seq }
        : state.streamVersions[event.streamId];
  const comparison = compareVersion(previousVersion, eventVersion(event));
  if (comparison === "duplicate") return state;
  if (comparison === "gap") return requestSnapshot(state, "event-gap");

  let next = withSeenEvent(state, event);
  switch (event.type) {
    case "item.upsert":
      return upsertItem(next, event.item, eventVersion(event));
    case "stream.append": {
      if (!itemForEvent) return requestSnapshot(state, "event-gap");
      const content = itemForEvent.content + event.delta;
      if (
        Array.from(content).length > MAX_MESSAGE_CONTENT_LENGTH ||
        new TextEncoder().encode(content).byteLength > MAX_MESSAGE_CONTENT_BYTES
      ) {
        return requestSnapshot(state, "content-limit");
      }
      return upsertItem(
        next,
        {
          ...itemForEvent,
          content,
          status: "streaming",
        },
        eventVersion(event),
      );
    }
    case "stream.complete": {
      if (!itemForEvent) return requestSnapshot(state, "event-gap");
      return upsertItem(
        next,
        { ...itemForEvent, content: event.content, status: "complete" },
        eventVersion(event),
      );
    }
    case "item.remove": {
      if (!state.items[event.itemId]) return next;
      const { [event.itemId]: _removed, ...items } = state.items;
      const loadedOrder = state.loadedOrder.filter((id) => id !== event.itemId);
      const windowStart = Math.min(
        state.windowStart,
        latestWindowStart(loadedOrder),
      );
      return {
        ...next,
        items,
        loadedOrder,
        order: windowOrder(loadedOrder, windowStart),
        windowStart,
        streamVersions: {
          ...state.streamVersions,
          [event.streamId]: eventVersion(event),
        },
      };
    }
    case "conversation.status":
      return {
        ...next,
        status: event.status,
        statusVersion: eventVersion(event),
      };
  }
};

export const reduceConversation = (
  state: ConversationState,
  action: ConversationAction,
): ConversationState => {
  switch (action.type) {
    case "event.received": {
      const { event } = action;
      if (event.protocolVersion !== 2)
        return requestSnapshot(state, "protocol-mismatch");
      if (event.conversationKey !== state.conversationKey)
        return requestSnapshot(state, "conversation-mismatch");
      if (state.seenEventIds.includes(event.eventId)) return state;
      return applyEvent(state, event);
    }
    case "item.optimistic": {
      const loadedOrder = [
        ...state.loadedOrder.filter((id) => id !== action.item.id),
        action.item.id,
      ];
      const windowStart = latestWindowStart(loadedOrder);
      const order = windowOrder(loadedOrder, windowStart);
      const items = { ...state.items, [action.item.id]: { ...action.item } };
      return { ...state, items, loadedOrder, order, windowStart };
    }
    case "item.optimistic.removed": {
      const items = Object.fromEntries(
        Object.entries(state.items).filter(([id]) => id !== action.itemId),
      );
      const loadedOrder = state.loadedOrder.filter(
        (id) => id !== action.itemId,
      );
      const windowStart = Math.min(
        state.windowStart,
        latestWindowStart(loadedOrder),
      );
      return {
        ...state,
        items,
        loadedOrder,
        order: windowOrder(loadedOrder, windowStart),
        windowStart,
      };
    }
    case "history.prepended": {
      const incomingIds = action.page.items.map((item) => item.id);
      const loadedOrder = [
        ...incomingIds,
        ...state.loadedOrder.filter((id) => !incomingIds.includes(id)),
      ];
      const windowStart =
        state.loadedOrder.length === 0 ? latestWindowStart(loadedOrder) : 0;
      const order = windowOrder(loadedOrder, windowStart);
      const items = {
        ...Object.fromEntries(
          action.page.items.map((item) => [item.id, { ...item }]),
        ),
        ...state.items,
      };
      const versions = Object.fromEntries(
        action.page.items.map((item) => [
          item.streamId,
          { revision: item.revision, seq: item.seq },
        ]),
      );
      return {
        ...state,
        items,
        loadedOrder,
        order,
        windowStart,
        olderCursor: action.page.cursor,
        hasOlder: action.page.hasOlder,
        streamVersions: { ...versions, ...state.streamVersions },
      };
    }
    case "window.latest": {
      const windowStart = latestWindowStart(state.loadedOrder);
      return {
        ...state,
        windowStart,
        order: windowOrder(state.loadedOrder, windowStart),
      };
    }
    case "connection.changed":
      return {
        ...state,
        connection: {
          ...state.connection,
          status: action.status,
          error: action.error,
        },
      };
    case "snapshot.requested":
      return requestSnapshot(state, action.reason);
  }
};

export const selectTurns = (
  state: ConversationState,
): readonly ConversationTurn[] => {
  const turns: ConversationTurn[] = [];
  for (const id of state.order) {
    const item = state.items[id];
    if (
      !item ||
      item.userReadable === false ||
      (["process", "tool"].includes(item.lane) && item.userReadable !== true)
    ) {
      continue;
    }
    const existing = turns.at(-1);
    if (existing?.id === item.turnId) {
      turns[turns.length - 1] = {
        ...existing,
        items: [...existing.items, item],
      };
    } else {
      turns.push({ id: item.turnId, items: [item] });
    }
  }
  return turns;
};
