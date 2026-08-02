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
    // Newest first by created_at, then id as a tiebreaker.
    [...conversations.value].sort((a, b) => {
      const byTime = b.created_at.localeCompare(a.created_at);
      return byTime !== 0 ? byTime : b.id - a.id;
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
