<script setup lang="ts">
import type { ConversationItem } from "../core";
import MarkdownContent from "./MarkdownContent.vue";

defineProps<{
  item: ConversationItem;
}>();
</script>

<template>
  <MarkdownContent v-if="item.status === 'complete'" :content="item.content" />
  <pre v-else class="atc-stream-text">{{ item.content }}</pre>
</template>
