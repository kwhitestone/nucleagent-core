<script setup lang="ts">
import { storeToRefs } from "pinia";
import { useRoute } from "vue-router";
import { useI18n } from "vue-i18n";
import { computed } from "vue";
import { useConversationStore } from "@/store/conversation";

const { t } = useI18n();
const route = useRoute();
const store = useConversationStore();
const { sorted, loading } = storeToRefs(store);

const activeId = computed(() => {
  const id = route.params.id;
  return id ? Number(id) : null;
});
</script>

<template>
  <aside class="sidebar">
    <div class="sidebar__head">
      <span class="sidebar__title">{{ t("workbench.historyTitle") }}</span>
    </div>

    <div v-loading="loading" class="sidebar__list">
      <p v-if="!loading && sorted.length === 0" class="sidebar__empty">
        {{ t("workbench.emptyHistory") }}
      </p>

      <router-link
        v-for="c in sorted"
        :key="c.id"
        :to="`/c/${c.id}`"
        class="sidebar__item"
        :class="{ 'sidebar__item--active': activeId === c.id }"
      >
        <span class="sidebar__item-title">{{ c.title || `#${c.id}` }}</span>
        <span class="sidebar__item-meta">{{ c.status }}</span>
      </router-link>
    </div>
  </aside>
</template>
