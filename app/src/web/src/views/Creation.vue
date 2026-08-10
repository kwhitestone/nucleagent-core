<script setup lang="ts">
/**
 * 创作视图 - 对齐 design/nucleagent-design.html 第 1929–1978 行。
 *
 * 5 张 .creation-card（data-color 五色变体 + hover 箭头）。
 *
 * 数据来源：GET /api/v1/addons/agent/templates，按 config.category 映射到五色卡片。
 * 接口不可用时降级到 i18n 前端常量 + console.warn。
 */
import { computed, onMounted, ref } from "vue";
import { useRouter } from "vue-router";
import { useI18n } from "vue-i18n";
import { listAgentTemplates } from "@/api/agent";
import type { AgentTemplate } from "@/api/types";

const router = useRouter();
const { t } = useI18n();

type CardColor = "image" | "ppt" | "write" | "video" | "music";

interface CreationCard {
  color: CardColor;
  title: string;
  desc: string;
  prompt: string;
}

/** category -> color 映射；未知 category 按 index 轮转五色。 */
const CATEGORY_COLOR_MAP: Record<string, CardColor> = {
  image: "image",
  ppt: "ppt",
  write: "write",
  video: "video",
  music: "music",
  general: "write",
  assistant: "write",
};

const COLOR_FALLBACK: CardColor[] = ["image", "ppt", "write", "video", "music"];

/** 接口不可用时的 i18n 降级常量。 */
const typeKeys: { color: CardColor; key: string }[] = [
  { color: "image", key: "image" },
  { color: "ppt", key: "ppt" },
  { color: "write", key: "write" },
  { color: "video", key: "video" },
  { color: "music", key: "music" },
];

const fallbackCards = computed<CreationCard[]>(() =>
  typeKeys.map(({ color, key }) => ({
    color,
    title: t(`creation.types.${key}.title`),
    desc: t(`creation.types.${key}.desc`),
    prompt: t(`creation.types.${key}.prompt`),
  })),
);

const cards = ref<CreationCard[]>([...fallbackCards.value]);

/** 把后端 AgentTemplate 转成前端 CreationCard。 */
function templateToCard(tpl: AgentTemplate, index: number): CreationCard {
  const cfg = tpl.config ?? {};
  const category = (cfg.category as string) ?? "";
  const color = CATEGORY_COLOR_MAP[category] ?? COLOR_FALLBACK[index % COLOR_FALLBACK.length];
  return {
    color,
    title: tpl.name,
    desc: (cfg.personality as string) || (cfg.role as string) || "",
    prompt: `${tpl.name}：`,
  };
}

onMounted(async () => {
  try {
    const templates = await listAgentTemplates();
    if (templates.length > 0) {
      cards.value = templates.map((tpl, i) => templateToCard(tpl, i));
    }
  } catch (e) {
    console.warn("[Creation] agent/templates 接口不可用，降级到前端常量", e);
  }
});

function pick(c: CreationCard): void {
  // 跳回首页并把类型预填进 composer。首页通过 query 携带预填文本。
  // 路由名是 "chat"（见 router/index.ts）—— 此前写的 "home" 并不存在，
  // vue-router 对未知 name 直接抛错，导致这五张卡片点了全都没反应。
  router.push({ name: "chat", query: { prefill: c.prompt } });
}
</script>

<template>
  <div class="view active">
    <div class="creation-view">
      <div class="creation-header">
        <h2>{{ t('creation.title') }}</h2>
        <p>{{ t('creation.subtitle') }}</p>
      </div>
      <div class="creation-grid">
        <div
          v-for="c in cards"
          :key="c.color"
          class="creation-card"
          :data-color="c.color"
          @click="pick(c)"
        >
          <div class="card-icon">
            <svg v-if="c.color === 'image'" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="18" height="18" rx="2" /><circle cx="8.5" cy="8.5" r="1.5" /><polyline points="21 15 16 10 5 21" /></svg>
            <svg v-else-if="c.color === 'ppt'" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="3" width="20" height="14" rx="2" /><line x1="8" y1="21" x2="16" y2="21" /><line x1="12" y1="17" x2="12" y2="21" /></svg>
            <svg v-else-if="c.color === 'write'" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 00-2 2v16a2 2 0 002 2h12a2 2 0 002-2V8z" /><polyline points="14 2 14 8 20 8" /><line x1="16" y1="13" x2="8" y2="13" /><line x1="16" y1="17" x2="8" y2="17" /></svg>
            <svg v-else-if="c.color === 'video'" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polygon points="23 7 16 12 23 17 23 7" /><rect x="1" y="5" width="15" height="14" rx="2" /></svg>
            <svg v-else viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M9 18V5l12-2v13" /><circle cx="6" cy="18" r="3" /><circle cx="18" cy="16" r="3" /></svg>
          </div>
          <div class="card-title">{{ c.title }}</div>
          <div class="card-desc">{{ c.desc }}</div>
          <div class="card-arrow">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><line x1="5" y1="12" x2="19" y2="12" /><polyline points="12 5 19 12 12 19" /></svg>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style>
/* 设计稿第 1070–1201 行。 */
.creation-view { padding: 32px 24px; max-width: 960px; margin: 0 auto; width: 100%; }

.creation-header { margin-bottom: 28px; animation: fade-in-up 0.4s var(--ease-out) both; }

.creation-header h2 {
  font-family: var(--font-display);
  font-size: 36px;
  background: var(--grad-aurora); background-size: 200% 200%;
  -webkit-background-clip: text; background-clip: text; -webkit-text-fill-color: transparent;
  animation: gradient-flow 5s var(--ease) infinite;
  margin-bottom: 6px;
}

.creation-header p { font-size: 14px; color: var(--text-secondary); }

.creation-grid {
  display: grid; grid-template-columns: repeat(auto-fill, minmax(240px, 1fr)); gap: 16px;
}

.creation-card {
  background: rgba(255, 255, 255, 0.9);
  backdrop-filter: blur(16px); -webkit-backdrop-filter: blur(16px);
  border: 1px solid var(--border); border-radius: var(--r-lg);
  padding: 24px; cursor: pointer;
  transition: all 0.35s var(--ease-spring);
  position: relative; overflow: hidden;
  animation: scale-in 0.4s var(--ease-spring) both;
}

.creation-card:nth-child(1) { animation-delay: 0.1s; }
.creation-card:nth-child(2) { animation-delay: 0.15s; }
.creation-card:nth-child(3) { animation-delay: 0.2s; }
.creation-card:nth-child(4) { animation-delay: 0.25s; }
.creation-card:nth-child(5) { animation-delay: 0.3s; }

.creation-card::before {
  content: ''; position: absolute; top: 0; left: 0; right: 0; height: 3px;
  opacity: 0; transition: opacity 0.3s var(--ease);
}

.creation-card::after {
  content: ''; position: absolute; inset: 0;
  opacity: 0; transition: opacity 0.35s var(--ease);
  border-radius: inherit; pointer-events: none;
}

.creation-card:hover { border-color: var(--border-strong); box-shadow: var(--shadow-lg); transform: translateY(-4px) scale(1.02); }
.creation-card:hover::before { opacity: 1; }
.creation-card:hover::after { opacity: 0.04; }

.creation-card .card-icon {
  width: 48px; height: 48px; border-radius: var(--r-md);
  display: flex; align-items: center; justify-content: center;
  margin-bottom: 16px; transition: transform 0.35s var(--ease-spring);
}

.creation-card:hover .card-icon { transform: scale(1.1) rotate(-8deg); }
.creation-card .card-icon svg { width: 24px; height: 24px; }

.creation-card .card-title { font-size: 15px; font-weight: 600; color: var(--text-primary); margin-bottom: 4px; }
.creation-card .card-desc { font-size: 12.5px; color: var(--text-secondary); line-height: 1.5; }

.creation-card .card-arrow {
  position: absolute; bottom: 20px; right: 20px;
  width: 28px; height: 28px; border-radius: 50%;
  background: var(--slate-100);
  display: flex; align-items: center; justify-content: center;
  color: var(--text-tertiary);
  opacity: 0; transform: translateX(-8px);
  transition: all 0.3s var(--ease);
}

.creation-card:hover .card-arrow { opacity: 1; transform: translateX(0); }

/* 五色变体 */
.creation-card[data-color="image"]::before { background: var(--grad-cyan-teal); }
.creation-card[data-color="image"]::after { background: var(--grad-cyan-teal); }
.creation-card[data-color="image"] .card-icon { background: var(--teal-50); color: var(--teal-600); }

.creation-card[data-color="ppt"]::before { background: var(--grad-amber-rose); }
.creation-card[data-color="ppt"]::after { background: var(--grad-amber-rose); }
.creation-card[data-color="ppt"] .card-icon { background: #fef3c7; color: #d97706; }

.creation-card[data-color="write"]::before { background: var(--grad-teal-indigo); }
.creation-card[data-color="write"]::after { background: var(--grad-teal-indigo); }
.creation-card[data-color="write"] .card-icon { background: #e0e7ff; color: var(--indigo-500); }

.creation-card[data-color="video"]::before { background: linear-gradient(135deg, var(--rose-500), var(--violet-500)); }
.creation-card[data-color="video"]::after { background: linear-gradient(135deg, var(--rose-500), var(--violet-500)); }
.creation-card[data-color="video"] .card-icon { background: #ffe4e6; color: #e11d48; }

.creation-card[data-color="music"]::before { background: var(--grad-violet-fuchsia); }
.creation-card[data-color="music"]::after { background: var(--grad-violet-fuchsia); }
.creation-card[data-color="music"] .card-icon { background: #ede9fe; color: #7c3aed; }
</style>
