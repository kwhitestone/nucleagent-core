<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import type { Message } from "@/api/types";

const props = defineProps<{ message: Message; streaming?: boolean }>();

const { t } = useI18n();

const isUser = computed(() => props.message.sender_type === "user");
const isSystem = computed(() => props.message.sender_type === "system");

const label = computed(() => {
  switch (props.message.sender_type) {
    case "user":
      return t("conversation.you");
    case "agent":
      return props.message.sender_name || t("conversation.agent");
    case "system":
      return t("conversation.system");
    default:
      return props.message.sender_name || props.message.sender_type;
  }
});
</script>

<template>
  <div class="msg" :class="{ 'msg--user': isUser, 'msg--system': isSystem }">
    <div class="msg__bubble">
      <div class="msg__head">
        <span class="msg__sender">{{ label }}</span>
        <span v-if="streaming" class="msg__streaming">{{ t("conversation.streaming") }}</span>
      </div>
      <div class="msg__content">{{ message.content || "" }}</div>
    </div>
  </div>
</template>
