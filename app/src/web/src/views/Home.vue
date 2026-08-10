<script setup lang="ts">
/**
 * 首页视图 —— 对齐 design/nucleagent-design.html 第 1760–1846 行。
 *
 * .home-hero（问候 + 标题 + 副标题）+ .home-composer（输入框 + 发送）+
 * .suggestion-grid（9 张建议卡，带 .delay-1..9 交错入场）。
 *
 * 替代旧 Workbench.vue：去掉自带的品牌头部与侧栏（chrome 已上移到壳），
 * 仅保留内容。创建对话的逻辑沿用 useConversationStore。
 */
import { computed, onMounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useI18n } from "vue-i18n";
import { ApiError } from "@/api/http";
import { useConversationStore } from "@/store/conversation";
import { toast } from "@/composables/useToast";
import AttachmentPicker from "@/components/AttachmentPicker.vue";
import AttachmentChips from "@/components/AttachmentChips.vue";
import ModelPicker from "@/components/ModelPicker.vue";
import type { ConversationMode, MessageAttachment, ModelChoice } from "@/api/types";

const router = useRouter();
const route = useRoute();
const store = useConversationStore();
const { t } = useI18n();

const input = ref("");
const submitting = ref(false);
/** 待发送附件。选中即已上传到 storage，这里持有的只是引用（fileId 等）。 */
const attachments = ref<MessageAttachment[]>([]);
/** 选定的模型；null = 用服务端默认。 */
const modelChoice = ref<ModelChoice | null>(null);

/** 9 张建议卡。点击后把标题填入输入框。 */
const suggestionKeys = [
  { key: "competitiveAnalysis", delay: 1, icon: "check", bg: "var(--teal-50)", color: "var(--teal-600)" },
  { key: "presentation", delay: 2, icon: "ppt", bg: "#fef3c7", color: "#d97706" },
  { key: "freeChat", delay: 3, icon: "chat", bg: "#e0e7ff", color: "var(--indigo-500)" },
  { key: "videoGen", delay: 4, icon: "video", bg: "#ffe4e6", color: "#e11d48" },
  { key: "musicCreation", delay: 5, icon: "music", bg: "#ede9fe", color: "#7c3aed" },
  { key: "dataAnalysis", delay: 6, icon: "chart", bg: "var(--teal-50)", color: "var(--teal-600)" },
  { key: "docWriting", delay: 7, icon: "doc", bg: "#e0e7ff", color: "var(--indigo-500)" },
  { key: "workflowAutomation", delay: 8, icon: "clock", bg: "#fef3c7", color: "#d97706" },
  { key: "knowledgeQA", delay: 9, icon: "layers", bg: "var(--teal-50)", color: "var(--teal-600)" },
] as const;

const suggestions = computed(() =>
  suggestionKeys.map((s) => ({
    ...s,
    title: t(`home.suggestions.${s.key}.title`),
    desc: t(`home.suggestions.${s.key}.desc`),
  })),
);

async function handleCreate(): Promise<void> {
  const text = input.value.trim();
  if (!text || submitting.value) return;
  submitting.value = true;
  try {
    const mode: ConversationMode = "a2a_agent";
    const created = await store.create({
      mode,
      input: text,
      // 模型与 provider 成对下发；未选则都不带，由服务端用默认。
      model: modelChoice.value?.model ?? "",
      providerId: modelChoice.value?.providerId,
      // 只传引用，字节早在选中时就直传给 storage 了。
      attachments: attachments.value.map((a) => ({ fileId: a.fileId, name: a.name })),
    });
    input.value = "";
    attachments.value = [];
    router.push(`/c/${created.id}`);
  } catch (error) {
    toast.error(error instanceof ApiError ? error.message : t("home.createFailed"));
  } finally {
    submitting.value = false;
  }
}

function fillSuggestion(title: string): void {
  input.value = `${title}：`;
}

onMounted(() => {
  // Creation 页点卡片会带 ?prefill=... 跳过来，预填进 composer。
  // 此前没有这段读取，即使修好路由名，卡片点击也只是跳转、看不出任何效果。
  const prefill = route.query.prefill;
  if (typeof prefill === "string" && prefill.trim() && !input.value) {
    input.value = prefill;
  }

  // 首页挂载时拉一次历史，用于推给壳侧栏（桥接在 store 变化时 dispatch）。
  store.load().catch((e: unknown) => {
    toast.error(e instanceof ApiError ? e.message : t("home.loadHistoryFailed"));
  });
});
</script>

<template>
  <div class="view active">
    <div class="home-view">
      <div class="home-hero">
        <div class="home-greeting">{{ t('home.greeting') }}</div>
        <h1 class="home-title" v-html="t('home.title')"></h1>
        <p class="home-subtitle">{{ t('home.subtitle') }}</p>

        <div class="home-composer">
          <textarea
            v-model="input"
            :placeholder="t('home.inputPlaceholder')"
            :disabled="submitting"
            @keydown.enter.exact.prevent="handleCreate"
          />
          <div class="composer-actions">
            <ModelPicker v-model="modelChoice" :disabled="submitting" compact />
            <AttachmentPicker v-model="attachments" :disabled="submitting" />
            <button class="composer-btn send" :title="t('common.send')" type="button" :disabled="submitting || !input.trim()" @click="handleCreate">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="22" y1="2" x2="11" y2="13" /><polygon points="22 2 15 22 11 13 2 9 22 2" /></svg>
            </button>
          </div>
        </div>

        <AttachmentChips
          :attachments="attachments"
          removable
          @remove="(id: string) => (attachments = attachments.filter((a) => a.fileId !== id))"
        />

        <div class="suggestion-grid">
          <div
            v-for="s in suggestions"
            :key="s.title"
            class="suggestion-chip"
            :class="`delay-${s.delay}`"
            @click="fillSuggestion(s.title)"
          >
            <div class="chip-icon" :style="{ background: s.bg, color: s.color }">
              <svg v-if="s.icon === 'check'" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M9 11l3 3L22 4" /><path d="M21 12v7a2 2 0 01-2 2H5a2 2 0 01-2-2V5a2 2 0 012-2h11" /></svg>
              <svg v-else-if="s.icon === 'ppt'" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="3" width="20" height="14" rx="2" /><line x1="8" y1="21" x2="16" y2="21" /><line x1="12" y1="17" x2="12" y2="21" /></svg>
              <svg v-else-if="s.icon === 'chat'" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15a2 2 0 01-2 2H7l-4 4V5a2 2 0 012-2h14a2 2 0 012 2z" /></svg>
              <svg v-else-if="s.icon === 'video'" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polygon points="23 7 16 12 23 17 23 7" /><rect x="1" y="5" width="15" height="14" rx="2" /></svg>
              <svg v-else-if="s.icon === 'music'" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M9 18V5l12-2v13" /><circle cx="6" cy="18" r="3" /><circle cx="18" cy="16" r="3" /></svg>
              <svg v-else-if="s.icon === 'chart'" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 3v18h18" /><path d="M18.7 8l-5.1 5.2-2.8-2.7L7 14.3" /></svg>
              <svg v-else-if="s.icon === 'doc'" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 00-2 2v16a2 2 0 002 2h12a2 2 0 002-2V8z" /><polyline points="14 2 14 8 20 8" /><line x1="16" y1="13" x2="8" y2="13" /><line x1="16" y1="17" x2="8" y2="17" /></svg>
              <svg v-else-if="s.icon === 'clock'" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10" /><path d="M12 6v6l4 2" /></svg>
              <svg v-else viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 2L2 7l10 5 10-5-10-5z" /><path d="M2 17l10 5 10-5" /><path d="M2 12l10 5 10-5" /></svg>
            </div>
            <div class="chip-title">{{ s.title }}</div>
            <div class="chip-desc">{{ s.desc }}</div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style>
/* 设计稿第 618–833 行。非 scoped：core 子应用以 micro-app disable-scopecss 加载，
   scoped 样式不生效；这些类名是 core 私有，不与壳冲突。 */
.home-view {
  display: flex; flex-direction: column; align-items: center; justify-content: center;
  min-height: 100%; padding: 48px 24px; text-align: center; position: relative;
}

.home-view::before,
.home-view::after {
  content: ''; position: absolute; border-radius: 50%;
  filter: blur(60px); pointer-events: none; z-index: 0;
}

.home-view::before {
  width: 350px; height: 350px;
  background: radial-gradient(circle, rgba(20, 184, 166, 0.18), transparent 60%);
  top: 10%; left: 15%;
  animation: float 8s var(--ease) infinite;
}

.home-view::after {
  width: 300px; height: 300px;
  background: radial-gradient(circle, rgba(139, 92, 246, 0.15), transparent 60%);
  bottom: 5%; right: 15%;
  animation: float 10s var(--ease) infinite reverse;
}

.home-hero { max-width: 640px; width: 100%; position: relative; z-index: 1; }

.home-greeting {
  font-size: 12px; font-weight: 600; letter-spacing: 0.1em; text-transform: uppercase;
  background: var(--grad-teal-indigo);
  -webkit-background-clip: text; background-clip: text; -webkit-text-fill-color: transparent;
  margin-bottom: 16px;
  animation: fade-in-up 0.5s var(--ease-out) both;
}

.home-title {
  font-family: var(--font-display);
  font-size: 52px; line-height: 1.1; letter-spacing: -0.5px;
  color: var(--text-primary); margin-bottom: 12px;
  animation: fade-in-up 0.5s var(--ease-out) 0.1s both;
}

.home-title em {
  font-style: italic;
  background: var(--grad-aurora); background-size: 200% 200%;
  -webkit-background-clip: text; background-clip: text; -webkit-text-fill-color: transparent;
  animation: gradient-flow 4s var(--ease) infinite;
}

.home-subtitle {
  font-size: 15px; color: var(--text-secondary);
  margin-bottom: 36px; line-height: 1.6;
  animation: fade-in-up 0.5s var(--ease-out) 0.2s both;
}

.home-composer {
  background: rgba(255, 255, 255, 0.9);
  backdrop-filter: blur(20px); -webkit-backdrop-filter: blur(20px);
  border: 1.5px solid var(--border); border-radius: var(--r-xl);
  padding: 6px; box-shadow: var(--shadow-md);
  display: flex; align-items: flex-end; gap: 8px;
  margin-bottom: 32px;
  transition: all 0.3s var(--ease);
  animation: scale-in 0.5s var(--ease-spring) 0.3s both;
}

.home-composer:focus-within {
  border-color: var(--teal-300);
  box-shadow: var(--shadow-lg), 0 0 0 4px rgba(20, 184, 166, 0.08);
  transform: translateY(-2px);
}

.home-composer textarea {
  flex: 1; border: none; outline: none; resize: none; background: transparent;
  font-family: var(--font-body); font-size: 14.5px; color: var(--text-primary);
  padding: 12px 14px; max-height: 160px; line-height: 1.5;
}

.home-composer textarea::placeholder { color: var(--text-tertiary); }
.home-composer textarea:disabled { opacity: 0.6; }

.composer-actions { display: flex; align-items: center; gap: 4px; padding: 4px; }

/* .composer-btn 系列已移到 styles/global.css —— Conversation.vue 也在用它，
 * 放在这里只有先访问过 /chat 才生效（详见 global.css 里的说明）。 */

.suggestion-grid {
  display: grid; grid-template-columns: repeat(3, 1fr); gap: 10px;
  width: 100%; max-width: 640px;
}

.suggestion-chip {
  text-align: left; padding: 14px 16px;
  background: rgba(255, 255, 255, 0.9);
  backdrop-filter: blur(12px); -webkit-backdrop-filter: blur(12px);
  border: 1px solid var(--border); border-radius: var(--r-lg);
  cursor: pointer; transition: all 0.3s var(--ease);
  display: flex; flex-direction: column; gap: 4px;
  position: relative; overflow: hidden;
  animation: fade-in-up 0.4s var(--ease-out) both;
}

.suggestion-chip::before {
  content: ''; position: absolute; inset: 0;
  background: var(--grad-brand-soft);
  opacity: 0; transition: opacity 0.3s var(--ease); border-radius: inherit;
}

.suggestion-chip:hover { border-color: var(--teal-300); box-shadow: var(--shadow-md); transform: translateY(-3px) scale(1.02); }
.suggestion-chip:hover::before { opacity: 0.5; }
.suggestion-chip > * { position: relative; z-index: 1; }

.suggestion-chip .chip-icon {
  width: 30px; height: 30px; border-radius: var(--r-sm);
  display: flex; align-items: center; justify-content: center;
  margin-bottom: 4px; transition: transform 0.3s var(--ease-spring);
}

.suggestion-chip:hover .chip-icon { transform: scale(1.15) rotate(-5deg); }
.suggestion-chip .chip-icon svg { width: 16px; height: 16px; }
.suggestion-chip .chip-title { font-size: 13px; font-weight: 600; color: var(--text-primary); }
.suggestion-chip .chip-desc { font-size: 11.5px; color: var(--text-tertiary); }
</style>
