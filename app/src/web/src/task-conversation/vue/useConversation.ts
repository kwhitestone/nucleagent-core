import { computed, onBeforeUnmount, shallowRef } from "vue";

import {
  CONVERSATION_PROTOCOL_VERSION,
  createConversationState,
  createEventBatcher,
  DEFAULT_PAGE_SIZE,
  MAX_ATTACHMENTS,
  MAX_ATTACHMENT_SIZE_BYTES,
  MAX_ID_LENGTH,
  MAX_MESSAGE_CONTENT_LENGTH,
  MAX_MESSAGE_CONTENT_BYTES,
  parseCommandResult,
  parseConversationAttachment,
  parseConversationEvent,
  parseConversationPage,
  parseConversationSnapshot,
  reduceConversation,
  selectTurns,
  type ConnectionStatus,
  type ConversationAdapter,
  type ConversationAttachment,
  type ConversationCommand,
  type ConversationEvent,
  type ConversationItem,
  type ConversationState,
} from "../core";

const randomId = (): string =>
  globalThis.crypto?.randomUUID?.() ??
  `atc-${Date.now()}-${Math.random().toString(36).slice(2)}`;

interface ConversationControllerOptions {
  conversationKey: () => string;
  adapter: () => ConversationAdapter;
  onConnectionChange: (status: ConnectionStatus) => void;
  onStatusChange: (status: ConversationState["status"]) => void;
  onError: (error: Error) => void;
}

const toError = (value: unknown): Error =>
  value instanceof Error ? value : new Error(String(value));

export const useConversation = (options: ConversationControllerOptions) => {
  const state = shallowRef(createConversationState(options.conversationKey()));
  let lifecycleController: AbortController | undefined;
  let snapshotInFlightGeneration: number | undefined;
  let subscriptionGeneration = 0;
  let subscription:
    | {
        controller: AbortController;
        generation: number;
        promise: Promise<void>;
      }
    | undefined;

  const dispatch = (action: Parameters<typeof reduceConversation>[1]) => {
    const previousConnection = state.value.connection.status;
    const previousStatus = state.value.status;
    state.value = reduceConversation(state.value, action);
    if (previousConnection !== state.value.connection.status) {
      options.onConnectionChange(state.value.connection.status);
    }
    if (previousStatus !== state.value.status)
      options.onStatusChange(state.value.status);
  };

  const loadSnapshot = async (signal: AbortSignal, generation: number) => {
    if (snapshotInFlightGeneration === generation || signal.aborted) return;
    snapshotInFlightGeneration = generation;
    dispatch({ type: "connection.changed", status: "connecting" });
    try {
      const snapshot = parseConversationSnapshot(
        await options.adapter().loadSnapshot({
          conversationKey: options.conversationKey(),
          limit: DEFAULT_PAGE_SIZE,
          signal,
        }),
      );
      if (signal.aborted || generation !== subscriptionGeneration) return;
      const newest = snapshot.items.at(-1);
      const event: ConversationEvent = {
        protocolVersion: CONVERSATION_PROTOCOL_VERSION,
        type: "snapshot",
        conversationKey: options.conversationKey(),
        eventId: randomId(),
        cursor: snapshot.cursor ?? "",
        turnId: newest?.turnId ?? "snapshot",
        streamId: newest?.streamId ?? "snapshot",
        lane: newest?.lane ?? "system",
        revision: newest?.revision ?? 0,
        seq: newest?.seq ?? 0,
        timestamp: new Date().toISOString(),
        snapshot,
      };
      dispatch({ type: "event.received", event });
    } finally {
      if (snapshotInFlightGeneration === generation)
        snapshotInFlightGeneration = undefined;
    }
  };

  const batcher = createEventBatcher((events) => {
    for (const event of events) {
      dispatch({ type: "event.received", event });
      if (state.value.connection.needsSnapshot) {
        restartSubscription(true);
        break;
      }
    }
  });

  const subscribe = async (
    signal: AbortSignal,
    generation: number,
    refreshSnapshot: boolean,
  ) => {
    if (refreshSnapshot) {
      let snapshotLoaded = false;
      while (!signal.aborted && !snapshotLoaded) {
        try {
          await loadSnapshot(signal, generation);
          snapshotLoaded = true;
        } catch (error) {
          if (signal.aborted) return;
          const normalized = toError(error);
          dispatch({
            type: "connection.changed",
            status: "reconnecting",
            error: normalized.message,
          });
          options.onError(normalized);
          await new Promise<void>((resolve) => setTimeout(resolve, 750));
        }
      }
    }
    if (signal.aborted || generation !== subscriptionGeneration) return;
    while (!signal.aborted) {
      try {
        const events = options.adapter().subscribe({
          conversationKey: options.conversationKey(),
          cursor: state.value.cursor,
          signal,
        });
        for await (const event of events) {
          if (signal.aborted) break;
          batcher.push(parseConversationEvent(event));
        }
        batcher.flush();
        if (signal.aborted) return;
        if (["completed", "failed", "cancelled"].includes(state.value.status)) {
          dispatch({ type: "connection.changed", status: "disconnected" });
          if (subscription?.generation === generation) subscription = undefined;
          return;
        }
        throw new Error("Conversation stream disconnected");
      } catch (error) {
        if (signal.aborted) return;
        const normalized = toError(error);
        dispatch({
          type: "connection.changed",
          status: "reconnecting",
          error: normalized.message,
        });
        options.onError(normalized);
      }

      let snapshotLoaded = false;
      while (!signal.aborted && !snapshotLoaded) {
        await new Promise<void>((resolve) => setTimeout(resolve, 750));
        if (signal.aborted) return;
        try {
          await loadSnapshot(signal, generation);
          snapshotLoaded = true;
        } catch (error) {
          if (signal.aborted) return;
          const normalized = toError(error);
          dispatch({
            type: "connection.changed",
            status: "reconnecting",
            error: normalized.message,
          });
          options.onError(normalized);
        }
      }
    }
  };

  function ensureSubscription(refreshSnapshot = false): void {
    const lifecycle = lifecycleController;
    if (!lifecycle || lifecycle.signal.aborted || subscription) return;
    const controller = new AbortController();
    const generation = ++subscriptionGeneration;
    const abortSubscription = () => controller.abort();
    lifecycle.signal.addEventListener("abort", abortSubscription, {
      once: true,
    });
    const current = {
      controller,
      generation,
      promise: Promise.resolve(),
    };
    current.promise = subscribe(
      controller.signal,
      generation,
      refreshSnapshot,
    ).finally(() => {
      lifecycle.signal.removeEventListener("abort", abortSubscription);
      if (subscription === current) subscription = undefined;
    });
    subscription = current;
  }

  function restartSubscription(refreshSnapshot: boolean): void {
    batcher.cancel();
    subscription?.controller.abort();
    subscription = undefined;
    ensureSubscription(refreshSnapshot);
  }

  const initialize = async () => {
    lifecycleController?.abort();
    subscription?.controller.abort();
    subscription = undefined;
    subscriptionGeneration += 1;
    batcher.cancel();
    lifecycleController = new AbortController();
    state.value = createConversationState(options.conversationKey());
    try {
      await loadSnapshot(lifecycleController.signal, subscriptionGeneration);
      if (!lifecycleController.signal.aborted) ensureSubscription();
    } catch (error) {
      if (lifecycleController.signal.aborted) return;
      const normalized = toError(error);
      dispatch({
        type: "connection.changed",
        status: "error",
        error: normalized.message,
      });
      options.onError(normalized);
    }
  };

  const loadOlder = async (): Promise<void> => {
    if (
      !state.value.hasOlder ||
      !lifecycleController ||
      lifecycleController.signal.aborted
    )
      return;
    try {
      const page = parseConversationPage(
        await options.adapter().loadOlder({
          conversationKey: options.conversationKey(),
          cursor: state.value.olderCursor,
          limit: DEFAULT_PAGE_SIZE,
          signal: lifecycleController.signal,
        }),
      );
      if (!lifecycleController.signal.aborted)
        dispatch({ type: "history.prepended", page });
    } catch (error) {
      options.onError(toError(error));
    }
  };

  const execute = async (command: ConversationCommand) => {
    const controller = lifecycleController;
    if (!controller || controller.signal.aborted) return;
    try {
      const result = parseCommandResult(
        await options.adapter().execute(command, { signal: controller.signal }),
      );
      if (!result.accepted)
        throw new Error(result.message || "Command was not accepted");
      if (result.item) dispatch({ type: "item.optimistic", item: result.item });
      if (
        command.type === "send" ||
        command.type === "retry" ||
        command.type === "rerun" ||
        command.type === "interaction.respond"
      ) {
        ensureSubscription();
      }
    } catch (error) {
      options.onError(toError(error));
      throw error;
    }
  };

  const send = async (
    content: string,
    attachments: readonly ConversationAttachment[] = [],
  ) => {
    if (!content.trim()) {
      const error = new Error("Message content is required");
      options.onError(error);
      throw error;
    }
    if (
      Array.from(content).length > MAX_MESSAGE_CONTENT_LENGTH ||
      new TextEncoder().encode(content).byteLength > MAX_MESSAGE_CONTENT_BYTES
    ) {
      const error = new Error(
        `Message content exceeds ${MAX_MESSAGE_CONTENT_LENGTH} characters`,
      );
      options.onError(error);
      throw error;
    }
    if (attachments.length > MAX_ATTACHMENTS) {
      const error = new Error(
        `At most ${MAX_ATTACHMENTS} attachments are allowed`,
      );
      options.onError(error);
      throw error;
    }
    const clientMessageId = randomId();
    const timestamp = new Date().toISOString();
    const optimistic: ConversationItem = {
      id: `optimistic-${clientMessageId}`,
      turnId: `turn-${clientMessageId}`,
      streamId: `optimistic-${clientMessageId}`,
      lane: "interaction",
      role: "user",
      kind: "message",
      content,
      attachments,
      clientMessageId,
      status: "pending",
      revision: 0,
      seq: 0,
      timestamp,
    };
    dispatch({ type: "item.optimistic", item: optimistic });
    try {
      await execute({ type: "send", content, clientMessageId, attachments });
    } catch (error) {
      dispatch({ type: "item.optimistic.removed", itemId: optimistic.id });
      throw error;
    }
  };

  const uploadAttachment = async (
    file: File,
  ): Promise<ConversationAttachment> => {
    if (!lifecycleController || !options.adapter().uploadAttachment) {
      throw new Error("Attachment upload is not supported");
    }
    if (file.size > MAX_ATTACHMENT_SIZE_BYTES) {
      const error = new Error(
        `Attachment exceeds ${MAX_ATTACHMENT_SIZE_BYTES} bytes`,
      );
      options.onError(error);
      throw error;
    }
    if (file.name.length > 512 || file.type.length > 128) {
      const error = new Error(
        "Attachment name or media type exceeds the supported limit",
      );
      options.onError(error);
      throw error;
    }
    try {
      return parseConversationAttachment(
        await options.adapter().uploadAttachment!(file, {
          conversationKey: options.conversationKey(),
          signal: lifecycleController.signal,
        }),
      );
    } catch (error) {
      options.onError(toError(error));
      throw error;
    }
  };

  const stop = () => execute({ type: "stop" });
  const retry = (itemId?: string, turnId?: string) =>
    execute({ type: "retry", itemId, turnId });
  const rerun = (turnId?: string) => execute({ type: "rerun", turnId });
  const respond = (interactionId: string, value: unknown) => {
    if (interactionId.length === 0 || interactionId.length > MAX_ID_LENGTH) {
      const error = new Error("Interaction ID exceeds the supported limit");
      options.onError(error);
      return Promise.reject(error);
    }
    return execute({ type: "interaction.respond", interactionId, value });
  };

  onBeforeUnmount(() => {
    lifecycleController?.abort();
    subscription?.controller.abort();
    batcher.cancel();
  });

  return {
    state,
    turns: computed(() => selectTurns(state.value)),
    initialize,
    loadOlder,
    send,
    stop,
    retry,
    rerun,
    respond,
    execute,
    uploadAttachment,
    executeWindowLatest: () => dispatch({ type: "window.latest" }),
    dispose: () => {
      lifecycleController?.abort();
      subscription?.controller.abort();
    },
  };
};
