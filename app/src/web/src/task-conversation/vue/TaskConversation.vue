<script setup lang="ts">
import type { Component } from "vue";
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  watch,
} from "vue";

import type {
  ConnectionStatus,
  ConversationAdapter,
  ConversationAttachment,
  ConversationCapabilities,
  ConversationItem,
  ConversationLocale,
  ConversationMessages,
  ConversationStatus,
  ConversationSurface,
  ConversationTheme,
  ConversationTurn,
} from "../core";
import {
  MAX_ATTACHMENTS,
  MAX_ID_LENGTH,
  MAX_MESSAGE_CONTENT_LENGTH,
} from "../core";
import ConversationContent from "./ConversationContent.vue";
import { resolveMessages } from "./messages";
import { useConversation } from "./useConversation";

defineOptions({ name: "TaskConversation" });

export type ConversationRendererRegistry = Readonly<Record<string, Component>>;

const props = withDefaults(
  defineProps<{
    conversationKey: string;
    adapter: ConversationAdapter;
    capabilities?: ConversationCapabilities;
    locale?: ConversationLocale;
    messages?: Partial<ConversationMessages>;
    theme?: ConversationTheme;
    surface?: ConversationSurface;
    renderers?: ConversationRendererRegistry;
    initialAnchor?: "latest" | string;
    showProcess?: boolean;
  }>(),
  {
    capabilities: () => ({ send: true }),
    locale: "zh-CN",
    messages: () => ({}),
    theme: "auto",
    surface: "auto",
    renderers: () => ({}),
    initialAnchor: "latest",
    showProcess: true,
  },
);

const emit = defineEmits<{
  "open-artifact": [payload: unknown];
  "open-diagnostics": [payload?: unknown];
  share: [payload: unknown];
  feedback: [payload: unknown];
  navigate: [payload: unknown];
  "connection-change": [status: ConnectionStatus];
  "status-change": [status: ConversationStatus];
  error: [error: Error];
  "fallback-legacy": [];
}>();

defineSlots<{
  "conversation-leading"(): unknown;
  "conversation-trailing"(): unknown;
  "turn-leading"(props: {
    turn: ConversationTurn;
    previousTurn?: ConversationTurn;
    turns: readonly ConversationTurn[];
    index: number;
  }): unknown;
  "user-item"(props: { item: ConversationItem }): unknown;
  "process-item"(props: { item: ConversationItem }): unknown;
  "assistant-item"(props: { item: ConversationItem }): unknown;
  artifact(props: {
    attachment: ConversationAttachment;
    item: ConversationItem;
    open: () => void;
  }): unknown;
  "assistant-actions"(props: {
    item: ConversationItem;
    capabilities: ConversationCapabilities;
  }): unknown;
  "composer-toolbar-leading"(): unknown;
  toolbar(): unknown;
  "composer-toolbar-trailing"(): unknown;
  "composer-actions-leading"(): unknown;
  "composer-actions-trailing"(): unknown;
}>();

const text = computed(() => resolveMessages(props.locale, props.messages));
const composer = ref("");
const attachments = ref<ConversationAttachment[]>([]);
const sending = ref(false);
const uploading = ref(false);
const scrollRegion = ref<HTMLElement>();
const bottomSentinel = ref<HTMLElement>();
const scrollMode = ref<"following" | "detached">("following");
const loadingOlder = ref(false);
const submittingInteractionIds = ref<ReadonlySet<string>>(new Set());
const expandedProcessIds = ref<ReadonlySet<string>>(new Set());
let scrollFrame: number | undefined;
let bottomObserver: IntersectionObserver | undefined;

const controller = useConversation({
  conversationKey: () => props.conversationKey,
  adapter: () => props.adapter,
  onConnectionChange: (status) => emit("connection-change", status),
  onStatusChange: (status) => emit("status-change", status),
  onError: (error) => emit("error", error),
});

const rootClasses = computed(() => [
  `atc-surface-${props.surface}`,
  `atc-theme-${props.theme}`,
  { "atc-is-detached": scrollMode.value === "detached" },
]);

const connectionMessage = computed(() => {
  if (controller.state.value.connection.status === "reconnecting")
    return text.value.reconnecting;
  if (
    ["disconnected", "error"].includes(controller.state.value.connection.status)
  )
    return text.value.disconnected;
  return "";
});
const hasFailedItem = computed(() =>
  controller.turns.value.some((turn) =>
    turn.items.some((item) => item.status === "failed"),
  ),
);
const activeProcessItemId = computed(() => {
  const latestTurn = controller.turns.value.at(-1);
  if (!latestTurn) return undefined;

  const latestProcess = latestTurn.items
    .filter((item) => item.lane === "process")
    .at(-1);
  if (!latestProcess) return undefined;

  const latestAnswer = latestTurn.items
    .filter((item) => item.role === "assistant" && item.lane === "answer")
    .at(-1);
  const answerIsPending = latestAnswer
    ? latestAnswer.status === "pending" || latestAnswer.status === "streaming"
    : latestProcess.status === "pending" ||
      latestProcess.status === "streaming" ||
      controller.state.value.status === "running";

  return answerIsPending ? latestProcess.id : undefined;
});

const scheduleScrollToLatest = (behavior: ScrollBehavior = "auto") => {
  if (scrollFrame !== undefined) cancelAnimationFrame(scrollFrame);
  scrollFrame = requestAnimationFrame(() => {
    if (scrollMode.value === "following")
      bottomSentinel.value?.scrollIntoView({ block: "end", behavior });
    scrollFrame = undefined;
  });
};

const jumpToLatest = () => {
  scrollMode.value = "following";
  controller.executeWindowLatest();
  scheduleScrollToLatest("smooth");
};

const loadOlder = async () => {
  const region = scrollRegion.value;
  if (!region || loadingOlder.value || !controller.state.value.hasOlder) return;
  const previousHeight = region.scrollHeight;
  const previousTop = region.scrollTop;
  const anchorId = controller.state.value.order[0];
  const previousAnchorTop = anchorId
    ? document.getElementById(`atc-item-${anchorId}`)?.getBoundingClientRect()
        .top
    : undefined;
  loadingOlder.value = true;
  await controller.loadOlder();
  await nextTick();
  const nextAnchorTop = anchorId
    ? document.getElementById(`atc-item-${anchorId}`)?.getBoundingClientRect()
        .top
    : undefined;
  region.scrollTop =
    previousAnchorTop !== undefined && nextAnchorTop !== undefined
      ? previousTop + nextAnchorTop - previousAnchorTop
      : previousTop + (region.scrollHeight - previousHeight);
  loadingOlder.value = false;
};

const onScroll = () => {
  const region = scrollRegion.value;
  if (!region) return;
  const distanceFromBottom =
    region.scrollHeight - region.clientHeight - region.scrollTop;
  scrollMode.value = distanceFromBottom <= 48 ? "following" : "detached";
  if (scrollMode.value === "detached" && scrollFrame !== undefined) {
    cancelAnimationFrame(scrollFrame);
    scrollFrame = undefined;
  }
  if (region.scrollTop <= 24) void loadOlder();
};

const submit = async () => {
  const content = composer.value.trim();
  if (!content || sending.value) return;
  composer.value = "";
  const currentAttachments = attachments.value;
  attachments.value = [];
  sending.value = true;
  try {
    await controller.send(content, currentAttachments);
    scrollMode.value = "following";
    scheduleScrollToLatest();
  } catch {
    // The controller already emitted the normalized error to the host.
    composer.value = content;
    attachments.value = currentAttachments;
  } finally {
    sending.value = false;
  }
};

const onComposerKeydown = (event: KeyboardEvent) => {
  if (event.key !== "Enter" || event.shiftKey || event.isComposing) return;
  event.preventDefault();
  void submit();
};

const onAttachment = async (event: Event) => {
  const input = event.target as HTMLInputElement;
  const file = input.files?.[0];
  input.value = "";
  if (!file || !props.capabilities.send) return;
  if (attachments.value.length >= MAX_ATTACHMENTS) {
    emit(
      "error",
      new Error(`At most ${MAX_ATTACHMENTS} attachments are allowed`),
    );
    return;
  }
  uploading.value = true;
  try {
    attachments.value = [
      ...attachments.value,
      await controller.uploadAttachment(file),
    ];
  } catch {
    // The controller already emitted the normalized error to the host.
  } finally {
    uploading.value = false;
  }
};

const removeAttachment = (id: string) => {
  attachments.value = attachments.value.filter(
    (attachment) => attachment.id !== id,
  );
};

const rendererFor = (item: ConversationItem): Component | undefined => {
  return props.renderers[item.kind];
};

const processPreview = (item: ConversationItem): string => {
  const lines = item.content
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(
      (line) =>
        line.length > 0 &&
        !/^[-*_]{3,}$/.test(line) &&
        !/^```/.test(line) &&
        !/^#{1,6}\s*$/.test(line),
    );
  const latestLine = lines.at(-1) ?? "";

  return latestLine
    .replace(/!\[([^\]]*)\]\([^)]+\)/g, "$1")
    .replace(/\[([^\]]+)\]\([^)]+\)/g, "$1")
    .replace(/`([^`]+)`/g, "$1")
    .replace(/\*\*([^*]+)\*\*/g, "$1")
    .replace(/^#{1,6}\s+/, "")
    .replace(/^>\s*/, "")
    .replace(/^[-*+]\s+/, "")
    .replace(/^\d+\.\s+/, "")
    .replace(/\t+/g, " ")
    .replace(/\s+/g, " ")
    .trim();
};

const isProcessExpanded = (itemId: string): boolean =>
  expandedProcessIds.value.has(itemId);

const shouldDisplayProcess = (item: ConversationItem): boolean =>
  props.showProcess || item.id === activeProcessItemId.value;

const toggleProcess = (itemId: string) => {
  const next = new Set(expandedProcessIds.value);
  if (next.has(itemId)) next.delete(itemId);
  else next.add(itemId);
  expandedProcessIds.value = next;
};

const rendererAttachmentProps = computed(() =>
  props.capabilities.attachments && props.adapter.uploadAttachment
    ? {
        attachmentUploadEnabled: true,
        uploadAttachment: controller.uploadAttachment,
      }
    : {},
);

const setInteractionSubmitting = (interactionId: string, value: boolean) => {
  const next = new Set(submittingInteractionIds.value);
  if (value) next.add(interactionId);
  else next.delete(interactionId);
  submittingInteractionIds.value = next;
};

const isInteractionSubmitting = (item: ConversationItem) => {
  const interactionId = item.data?.interactionId;
  return (
    typeof interactionId === "string" &&
    submittingInteractionIds.value.has(interactionId)
  );
};

const isPendingInteraction = (item: ConversationItem) =>
  item.data?.interactionStatus === "pending" ||
  item.data?.planStatus === "pending";

const clearSettledInteractions = () => {
  if (submittingInteractionIds.value.size === 0) return;
  const pending = new Set<string>();
  for (const item of Object.values(controller.state.value.items)) {
    const interactionId = item.data?.interactionId;
    if (
      typeof interactionId === "string" &&
      submittingInteractionIds.value.has(interactionId) &&
      isPendingInteraction(item)
    ) {
      pending.add(interactionId);
    }
  }
  submittingInteractionIds.value = pending;
};

const forwardInteraction = async (item: ConversationItem, value: unknown) => {
  const interactionId = item.data?.interactionId;
  if (
    typeof interactionId !== "string" ||
    interactionId.length === 0 ||
    interactionId.length > MAX_ID_LENGTH
  )
    return;
  if (submittingInteractionIds.value.has(interactionId)) return;
  setInteractionSubmitting(interactionId, true);
  try {
    await controller.respond(interactionId, value);
    const current = controller.state.value.items[item.id];
    if (!current || !isPendingInteraction(current)) {
      setInteractionSubmitting(interactionId, false);
    }
  } catch {
    setInteractionSubmitting(interactionId, false);
  }
};
const fallbackLegacy = () => {
  controller.dispose();
  emit("fallback-legacy");
};
const openArtifact = (attachment: ConversationAttachment) => {
  emit("open-artifact", attachment);
};

watch(
  () => props.conversationKey,
  () => void controller.initialize(),
);

watch(
  () => controller.state.value,
  () => {
    clearSettledInteractions();
    if (scrollMode.value === "following") scheduleScrollToLatest();
  },
);

onMounted(async () => {
  await controller.initialize();
  await nextTick();
  if (typeof IntersectionObserver !== "undefined" && bottomSentinel.value) {
    bottomObserver = new IntersectionObserver(
      ([entry]) => {
        if (entry?.isIntersecting) scrollMode.value = "following";
      },
      { root: scrollRegion.value, threshold: 1 },
    );
    bottomObserver.observe(bottomSentinel.value);
  }
  if (props.initialAnchor === "latest") scheduleScrollToLatest();
  else
    document
      .getElementById(`atc-item-${props.initialAnchor}`)
      ?.scrollIntoView({ block: "center" });
});

onBeforeUnmount(() => {
  if (scrollFrame !== undefined) cancelAnimationFrame(scrollFrame);
  bottomObserver?.disconnect();
});
</script>

<template>
  <section
    class="atc-root"
    :class="rootClasses"
    :data-conversation-key="conversationKey"
  >
    <div v-if="connectionMessage" class="atc-connection-banner" role="status">
      <span class="atc-connection-dot" aria-hidden="true" />
      {{ connectionMessage }}
    </div>

    <div
      ref="scrollRegion"
      class="atc-scroll-region"
      @scroll.passive="onScroll"
    >
      <slot name="conversation-leading" />

      <button
        v-if="controller.state.value.hasOlder"
        class="atc-load-older"
        type="button"
        :disabled="loadingOlder"
        @click="loadOlder"
      >
        {{ loadingOlder ? text.loading : text.loadOlder }}
      </button>

      <div v-if="controller.turns.value.length === 0" class="atc-empty">
        {{ text.empty }}
      </div>

      <article
        v-for="(turn, turnIndex) in controller.turns.value"
        :key="turn.id"
        class="atc-turn"
      >
        <slot
          name="turn-leading"
          :turn="turn"
          :previous-turn="controller.turns.value[turnIndex - 1]"
          :turns="controller.turns.value"
          :index="turnIndex"
        />
        <template v-for="item in turn.items" :key="item.id">
          <div
            v-if="item.role === 'user'"
            :id="`atc-item-${item.id}`"
            class="atc-item atc-user-item"
            :data-status="item.status"
          >
            <slot name="user-item" :item="item">
              <ConversationContent :item="item" />
              <ul v-if="item.attachments?.length" class="atc-attachment-list">
                <li
                  v-for="attachment in item.attachments"
                  :key="attachment.id"
                  class="atc-attachment"
                >
                  {{ attachment.name }}
                </li>
              </ul>
            </slot>
          </div>

          <template v-else-if="item.lane === 'process'">
            <details
              v-if="shouldDisplayProcess(item)"
              :id="`atc-item-${item.id}`"
              class="atc-process"
              :data-status="item.status"
              :open="isProcessExpanded(item.id)"
            >
              <summary
                class="atc-process-summary"
                :aria-expanded="isProcessExpanded(item.id)"
                @click.prevent="toggleProcess(item.id)"
              >
                <span class="atc-process-marker" aria-hidden="true" />
                <span class="atc-process-label">{{
                  item.title || text.process
                }}</span>
                <span v-if="processPreview(item)" class="atc-process-preview">{{
                  processPreview(item)
                }}</span>
              </summary>
              <div class="atc-process-content">
                <slot name="process-item" :item="item">
                  <ConversationContent :item="item" />
                </slot>
              </div>
            </details>
          </template>

          <component
            :is="rendererFor(item)"
            v-else-if="rendererFor(item)"
            :id="`atc-item-${item.id}`"
            class="atc-item atc-custom-item"
            :item="item"
            :locale="locale"
            :conversation-status="controller.state.value.status"
            :interaction-submitting="isInteractionSubmitting(item)"
            v-bind="rendererAttachmentProps"
            @respond="forwardInteraction(item, $event)"
            @open-artifact="emit('open-artifact', $event)"
            @open-diagnostics="emit('open-diagnostics', $event)"
            @share="emit('share', $event)"
            @feedback="emit('feedback', $event)"
            @navigate="emit('navigate', $event)"
            @fallback-legacy="fallbackLegacy"
          />

          <div
            v-else
            :id="`atc-item-${item.id}`"
            class="atc-item atc-assistant-item"
            :data-lane="item.lane"
            :data-status="item.status"
          >
            <slot name="assistant-item" :item="item">
              <ConversationContent :item="item" />
            </slot>
            <ul v-if="item.attachments?.length" class="atc-artifact-list">
              <li v-for="attachment in item.attachments" :key="attachment.id">
                <slot
                  name="artifact"
                  :attachment="attachment"
                  :item="item"
                  :open="() => openArtifact(attachment)"
                >
                  <button
                    type="button"
                    class="atc-artifact-card"
                    @click="openArtifact(attachment)"
                  >
                    <span class="atc-artifact-mark" aria-hidden="true">↗</span>
                    <span class="atc-artifact-copy">
                      <strong>{{ attachment.name }}</strong>
                      <small v-if="attachment.mimeType">{{
                        attachment.mimeType
                      }}</small>
                    </span>
                  </button>
                </slot>
              </li>
            </ul>
            <slot
              name="assistant-actions"
              :item="item"
              :capabilities="capabilities"
            >
              <div
                v-if="
                  item.status === 'complete' &&
                  item.role === 'assistant' &&
                  item.lane === 'answer' &&
                  (capabilities.feedback ||
                    capabilities.share ||
                    capabilities.diagnostics)
                "
                class="atc-item-actions"
              >
                <button
                  v-if="capabilities.feedback"
                  class="atc-icon-action"
                  type="button"
                  :aria-label="text.helpful"
                  @click="emit('feedback', { itemId: item.id, value: 'up' })"
                >
                  ↑
                </button>
                <button
                  v-if="capabilities.feedback"
                  class="atc-icon-action"
                  type="button"
                  :aria-label="text.notHelpful"
                  @click="emit('feedback', { itemId: item.id, value: 'down' })"
                >
                  ↓
                </button>
                <button
                  v-if="capabilities.share"
                  class="atc-icon-action atc-text-action"
                  type="button"
                  @click="
                    emit('share', { itemId: item.id, turnId: item.turnId })
                  "
                >
                  {{ text.share }}
                </button>
                <button
                  v-if="capabilities.diagnostics"
                  class="atc-icon-action atc-text-action"
                  type="button"
                  @click="
                    emit('open-diagnostics', {
                      itemId: item.id,
                      turnId: item.turnId,
                    })
                  "
                >
                  {{ text.diagnostics }}
                </button>
              </div>
            </slot>
            <div v-if="item.status === 'failed'" class="atc-item-actions">
              <button
                v-if="capabilities.retry"
                class="atc-action-button"
                type="button"
                @click="controller.retry(item.id, item.turnId)"
              >
                {{ text.retry }}
              </button>
            </div>
          </div>
        </template>
      </article>
      <slot name="conversation-trailing" />
      <div
        ref="bottomSentinel"
        class="atc-bottom-sentinel"
        aria-hidden="true"
      />
    </div>

    <button
      v-if="scrollMode === 'detached'"
      class="atc-jump-latest"
      type="button"
      @click="jumpToLatest"
    >
      <span aria-hidden="true">↓</span> {{ text.jumpLatest }}
    </button>

    <footer class="atc-composer-shell">
      <ul v-if="attachments.length" class="atc-composer-attachments">
        <li
          v-for="attachment in attachments"
          :key="attachment.id"
          class="atc-composer-attachment"
        >
          <span>{{ attachment.name }}</span>
          <button
            type="button"
            :aria-label="`${text.removeAttachment}: ${attachment.name}`"
            @click="removeAttachment(attachment.id)"
          >
            ×
          </button>
        </li>
      </ul>
      <div class="atc-composer">
        <textarea
          v-model="composer"
          class="atc-composer-input"
          rows="1"
          :maxlength="MAX_MESSAGE_CONTENT_LENGTH * 2"
          :placeholder="text.composerPlaceholder"
          :disabled="!capabilities.send || sending"
          @keydown="onComposerKeydown"
        />
        <div class="atc-composer-toolbar">
          <div class="atc-composer-tools">
            <slot name="composer-toolbar-leading" />
            <label
              v-if="capabilities.attachments && adapter.uploadAttachment"
              class="atc-attach-button"
              :aria-disabled="!capabilities.send || uploading"
            >
              <span aria-hidden="true">＋</span>
              <span class="atc-visually-hidden">{{ text.attach }}</span>
              <input
                type="file"
                :disabled="uploading || !capabilities.send"
                @change="onAttachment"
              />
            </label>
            <slot name="toolbar" />
            <slot name="composer-toolbar-trailing" />
          </div>
          <div class="atc-composer-actions">
            <slot name="composer-actions-leading" />
            <button
              v-if="
                capabilities.retry &&
                controller.state.value.status === 'failed' &&
                !hasFailedItem
              "
              class="atc-action-button atc-task-retry-button"
              type="button"
              @click="controller.retry()"
            >
              {{ text.retry }}
            </button>
            <button
              v-if="capabilities.rerun"
              class="atc-secondary-button"
              type="button"
              @click="controller.rerun()"
            >
              {{ text.rerun }}
            </button>
            <button
              v-if="capabilities.stop"
              class="atc-stop-button"
              type="button"
              @click="controller.stop()"
            >
              <span class="atc-stop-icon" aria-hidden="true" />
              {{ text.stop }}
            </button>
            <button
              class="atc-send-button"
              type="button"
              :disabled="!composer.trim() || sending || !capabilities.send"
              @click="submit"
            >
              {{ text.send }}
            </button>
            <slot name="composer-actions-trailing" />
          </div>
        </div>
      </div>
    </footer>
  </section>
</template>
