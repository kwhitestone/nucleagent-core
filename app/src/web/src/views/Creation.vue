<script setup lang="ts">
/**
 * 创作视图 —— 对齐 design/nucleagent-design.html 第 1929–1978 行。
 *
 * 5 张 .creation-card（data-color 五色变体 + hover 箭头）。
 *
 * 降级策略：创作类型暂为前端常量（后端无对应分类接口）。点击后把类型作为
 * 预填内容跳到首页 composer，让用户补充描述后创建对话——避免点进去空白。
 */
import { useRouter } from "vue-router";

const router = useRouter();

interface CreationType {
  color: "image" | "ppt" | "write" | "video" | "music";
  title: string;
  desc: string;
  prompt: string;
}

const types: CreationType[] = [
  { color: "image", title: "AI 绘画", desc: "文字描述生成高质量图片，支持多种风格", prompt: "AI 绘画：" },
  { color: "ppt", title: "演示文稿", desc: "从大纲到精美 PPT，自动排版与配图", prompt: "演示文稿：" },
  { color: "write", title: "文档撰写", desc: "自动生成报告、文章、技术文档", prompt: "文档撰写：" },
  { color: "video", title: "视频生成", desc: "文字脚本自动生成短视频内容", prompt: "视频生成：" },
  { color: "music", title: "音乐创作", desc: "AI 辅助编曲、配器与音乐生成", prompt: "音乐创作：" },
];

function pick(c: CreationType): void {
  // 跳回首页并把类型预填进 composer。首页通过 query 携带预填文本。
  router.push({ name: "home", query: { prefill: c.prompt } });
}
</script>

<template>
  <div class="view active">
    <div class="creation-view">
      <div class="creation-header">
        <h2>创作工坊</h2>
        <p>选择创作类型，AI 帮你从零到一</p>
      </div>
      <div class="creation-grid">
        <div
          v-for="c in types"
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
