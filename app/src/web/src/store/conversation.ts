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
 *
 * 游标分页：conversations 累积全部已加载页，hasMore 标记是否还能向下翻。
 * load() 拉首页，loadMore() 用当前最小 id 作 beforeId 追加下一页。
 * 推给壳时（useShellBridge）把累积列表 + hasMore 一起推，壳据此控制加载态。
 */
export const useConversationStore = defineStore("conversation", () => {
  const conversations = ref<Conversation[]>([]);
  const loading = ref(false);
  const loaded = ref(false);

  /** 还有更早的对话可加载（后端 hasMore）。首屏前默认 true，避免侧栏提前显示「没有更多」。 */
  const hasMore = ref(true);
  /** loadMore 进行中，防止重复触发。 */
  const loadingMore = ref(false);
  /** 每页大小，与后端默认一致。 */
  const PAGE_SIZE = 20;

  const sorted = computed(() =>
    // Newest first by createdAt，then id 兜底。
    // 后端返回 camelCase（createdAt），类型定义已对齐。
    [...conversations.value].sort((a, b) => {
      const ta = a.createdAt ?? "";
      const tb = b.createdAt ?? "";
      const byTime = tb.localeCompare(ta);
      return byTime !== 0 ? byTime : (b.id ?? 0) - (a.id ?? 0);
    }),
  );

  async function load(force = false): Promise<void> {
    if (loaded.value && !force) return;
    loading.value = true;
    try {
      const res = await listConversations({ limit: PAGE_SIZE });
      conversations.value = res.data;
      hasMore.value = res.hasMore;
      loaded.value = true;
    } finally {
      loading.value = false;
    }
  }

  /**
   * 向下翻页：用当前累积列表里最小的 id 作 beforeId，拉下一页并追加（去重）。
   * 游标用最小 id（最旧一条），因为后端按 id DESC 返回，beforeId 取已加载的最旧 id。
   */
  async function loadMore(): Promise<void> {
    if (!hasMore.value || loadingMore.value) return;
    if (!conversations.value.length) return;
    const beforeId = conversations.value.reduce(
      (min, c) => Math.min(min, c.id ?? Number.MAX_SAFE_INTEGER),
      Number.MAX_SAFE_INTEGER,
    );
    loadingMore.value = true;
    try {
      const res = await listConversations({ beforeId, limit: PAGE_SIZE });
      // 去重追加：翻页间隙可能有新数据写入，按 id 去重避免重复。
      const seen = new Set(conversations.value.map((c) => c.id));
      const fresh = res.data.filter((c) => !seen.has(c.id));
      conversations.value = [...conversations.value, ...fresh];
      hasMore.value = res.hasMore;
    } finally {
      loadingMore.value = false;
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
    hasMore.value = true;
    loadingMore.value = false;
  }

  return {
    // state
    conversations,
    loading,
    loaded,
    hasMore,
    loadingMore,
    // getters
    sorted,
    // actions
    load,
    loadMore,
    create,
    upsert,
    reset,
  };
});
