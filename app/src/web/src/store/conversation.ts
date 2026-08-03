import { defineStore } from "pinia";
import { computed, ref } from "vue";
import {
  listConversations,
  createConversation as createConversationApi,
} from "@/api/conversation";
import type { Conversation, CreateConversationRequest } from "@/api/types";

/**
 * Conversation list store.
 *
 * Owns the sidebar history shared between Workbench and Conversation views.
 * State is mutated only via reassignment (immutable-style); actions wrap the
 * API layer so views stay thin.
 */
export const useConversationStore = defineStore("conversation", () => {
  const conversations = ref<Conversation[]>([]);
  const loading = ref(false);
  const loaded = ref(false);

  const sorted = computed(() =>
    // Newest first by createdAt，then id 兜底。
    // 后端返回 camelCase（createdAt），但 TS 类型仍是 snake_case（created_at），
    // 用 as any 兼容两者；空值兜底避免 localeCompare 炸。
    [...conversations.value].sort((a, b) => {
      const ax = a as any;
      const bx = b as any;
      const ta = (ax.created_at ?? ax.createdAt) ?? "";
      const tb = (bx.created_at ?? bx.createdAt) ?? "";
      const byTime = tb.localeCompare(ta);
      return byTime !== 0 ? byTime : (b.id ?? 0) - (a.id ?? 0);
    }),
  );

  async function load(force = false): Promise<void> {
    if (loaded.value && !force) return;
    loading.value = true;
    try {
      conversations.value = await listConversations();
      loaded.value = true;
    } finally {
      loading.value = false;
    }
  }

  async function create(payload: CreateConversationRequest): Promise<Conversation> {
    const created = await createConversationApi(payload);
    // Immutable prepend so the sidebar updates immediately.
    conversations.value = [created, ...conversations.value];
    return created;
  }

  function upsert(conversation: Conversation): void {
    const existing = conversations.value.findIndex((c) => c.id === conversation.id);
    if (existing >= 0) {
      const next = conversations.value.slice();
      next[existing] = conversation;
      conversations.value = next;
    } else {
      conversations.value = [conversation, ...conversations.value];
    }
  }

  function reset(): void {
    conversations.value = [];
    loaded.value = false;
  }

  return {
    // state
    conversations,
    loading,
    loaded,
    // getters
    sorted,
    // actions
    load,
    create,
    upsert,
    reset,
  };
});
