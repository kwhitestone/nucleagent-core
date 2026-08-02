<script setup lang="ts">
import { onMounted, ref } from "vue";
import { useRouter } from "vue-router";
import { useI18n } from "vue-i18n";
import { ElMessage } from "element-plus";
import { ApiError } from "@/api/http";
import { useConversationStore } from "@/store/conversation";
import type { ConversationMode } from "@/api/types";
import LanguageSwitcher from "@/components/LanguageSwitcher.vue";
import ConversationSidebar from "@/components/ConversationSidebar.vue";

const { t } = useI18n();
const router = useRouter();
const store = useConversationStore();

const input = ref("");
const submitting = ref(false);

async function handleCreate(): Promise<void> {
  const text = input.value.trim();
  if (!text || submitting.value) return;
  submitting.value = true;
  try {
    const mode: ConversationMode = "a2a_agent";
    const created = await store.create({ mode, input: text, model: "" });
    input.value = "";
    router.push(`/c/${created.id}`);
  } catch (error) {
    const message = error instanceof ApiError ? error.message : t("workbench.createFailed");
    ElMessage.error(message);
  } finally {
    submitting.value = false;
  }
}

async function loadHistory(): Promise<void> {
  try {
    await store.load();
  } catch (error) {
    const message =
      error instanceof ApiError ? error.message : t("workbench.loadHistoryFailed");
    ElMessage.error(message);
  }
}

onMounted(loadHistory);
</script>

<template>
  <div class="workbench">
    <ConversationSidebar />

    <main class="workbench__main">
      <header class="workbench__header">
        <span class="workbench__brand-mark">N</span>
        <span class="workbench__brand-name">Nucleagent</span>
        <div class="workbench__header-actions">
          <LanguageSwitcher />
        </div>
      </header>

      <section class="workbench__hero">
        <h1 class="workbench__title">{{ t("workbench.pageTitle") }}</h1>
        <p class="workbench__subtitle">{{ t("workbench.pageSubtitle") }}</p>

        <div class="workbench__composer">
          <el-input
            v-model="input"
            type="textarea"
            :autosize="{ minRows: 2, maxRows: 8 }"
            :placeholder="t('workbench.inputPlaceholder')"
            :disabled="submitting"
            resize="none"
            class="workbench__input"
            @keydown.enter.exact.prevent="handleCreate"
          />
          <button
            class="na-send-btn workbench__send"
            :disabled="submitting || !input.trim()"
            @click="handleCreate"
          >
            {{ submitting ? t("common.sending") : t("workbench.send") }}
          </button>
        </div>
      </section>
    </main>
  </div>
</template>
