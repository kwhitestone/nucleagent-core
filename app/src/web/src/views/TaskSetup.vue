<script setup lang="ts">
/**
 * 任务视图 —— 对齐 design/nucleagent-design.html 第 1981–2046 行。
 *
 * 3 张 .template-card + .task-form（名称/描述/执行模式/输出格式）。
 *
 * 降级策略：模板暂为前端常量（后端无任务模板接口）。启动任务走已存在的
 * POST /conversation，执行模式/输出格式暂存进 input 文本（后端暂无 metadata
 * 字段），不阻塞流程。
 */
import { reactive, ref } from "vue";
import { useRouter } from "vue-router";
import { ApiError } from "@/api/http";
import { useConversationStore } from "@/store/conversation";
import { toast } from "@/composables/useToast";
import type { ConversationMode } from "@/api/types";

const router = useRouter();
const store = useConversationStore();

interface TaskTemplate {
  icon: string;
  name: string;
  desc: string;
  defaultName: string;
  defaultDesc: string;
}

const templates: TaskTemplate[] = [
  { icon: "📊", name: "竞品分析", desc: "自动收集与对比", defaultName: "AI Agent 市场竞品分析", defaultDesc: "分析当前 AI Agent 市场的主要玩家，对比它们的技术路线、定价策略和目标客户群体，输出一份结构化的竞品分析报告。" },
  { icon: "📝", name: "内容创作", desc: "文章与报告", defaultName: "内容创作任务", defaultDesc: "围绕指定主题撰写一篇结构清晰的长文。" },
  { icon: "🔍", name: "数据调研", desc: "深度信息搜集", defaultName: "数据调研任务", defaultDesc: "针对指定问题搜集并整理关键数据与来源。" },
];

const selected = ref(0);

const form = reactive({
  name: templates[0].defaultName,
  desc: templates[0].defaultDesc,
  execMode: "全自动执行",
  outputFormat: "Markdown 文档",
});

function selectTemplate(i: number): void {
  selected.value = i;
  form.name = templates[i].defaultName;
  form.desc = templates[i].defaultDesc;
}

const submitting = ref(false);

async function launch(): Promise<void> {
  if (submitting.value) return;
  const name = form.name.trim();
  if (!name) {
    toast.warning("请填写任务名称");
    return;
  }
  submitting.value = true;
  try {
    // 执行模式/输出格式暂拼进 input（后端暂无独立字段），降级但不丢信息。
    const input = `${form.desc.trim()}\n\n[执行模式: ${form.execMode} | 输出格式: ${form.outputFormat}]`;
    const mode: ConversationMode = "a2a_agent";
    const created = await store.create({ mode, input, model: "" });
    router.push(`/c/${created.id}`);
  } catch (error) {
    toast.error(error instanceof ApiError ? error.message : "启动任务失败");
  } finally {
    submitting.value = false;
  }
}
</script>

<template>
  <div class="view active">
    <div class="task-setup-view">
      <div class="task-setup-header">
        <h2>创建任务</h2>
        <p>选择模板或自定义任务，Agent 会自动规划执行</p>
      </div>

      <div class="task-templates">
        <div
          v-for="(t, i) in templates"
          :key="t.name"
          class="template-card"
          :class="{ selected: selected === i }"
          @click="selectTemplate(i)"
        >
          <div class="tpl-icon">{{ t.icon }}</div>
          <div class="tpl-name">{{ t.name }}</div>
          <div class="tpl-desc">{{ t.desc }}</div>
        </div>
      </div>

      <div class="task-form">
        <div class="form-group">
          <label>任务名称 <span class="label-hint">- 给你的任务起个名字</span></label>
          <input v-model="form.name" type="text" class="form-input" placeholder="输入任务名称" />
        </div>

        <div class="form-group">
          <label>任务描述 <span class="label-hint">- 越详细，Agent 执行越精准</span></label>
          <textarea v-model="form.desc" class="form-textarea" placeholder="描述任务目标、范围和期望输出" />
        </div>

        <div class="form-row">
          <div class="form-group">
            <label>执行模式</label>
            <select v-model="form.execMode" class="form-select">
              <option>全自动执行</option>
              <option>逐步确认</option>
              <option>仅规划不执行</option>
            </select>
          </div>
          <div class="form-group">
            <label>输出格式</label>
            <select v-model="form.outputFormat" class="form-select">
              <option>Markdown 文档</option>
              <option>PDF 报告</option>
              <option>PPT 演示</option>
              <option>Excel 表格</option>
            </select>
          </div>
        </div>

        <div class="form-actions">
          <button class="btn btn-secondary" type="button">保存草稿</button>
          <button class="btn btn-primary" type="button" :disabled="submitting" @click="launch">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polygon points="5 3 19 12 5 21 5 3" /></svg>
            <span>{{ submitting ? "启动中…" : "启动任务" }}</span>
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<style>
/* 设计稿第 1204–1366 行。 */
.task-setup-view { padding: 32px 24px; max-width: 680px; margin: 0 auto; width: 100%; }

.task-setup-header { margin-bottom: 28px; animation: fade-in-up 0.4s var(--ease-out) both; }

.task-setup-header h2 {
  font-family: var(--font-display);
  font-size: 36px;
  background: var(--grad-teal-indigo);
  -webkit-background-clip: text; background-clip: text; -webkit-text-fill-color: transparent;
  margin-bottom: 6px;
}

.task-setup-header p { font-size: 14px; color: var(--text-secondary); }

.task-templates {
  display: grid; grid-template-columns: repeat(3, 1fr); gap: 10px; margin-bottom: 24px;
}

.template-card {
  padding: 16px;
  background: rgba(255, 255, 255, 0.9);
  backdrop-filter: blur(12px); -webkit-backdrop-filter: blur(12px);
  border: 1.5px solid var(--border); border-radius: var(--r-lg);
  cursor: pointer; transition: all 0.3s var(--ease-spring);
  text-align: center;
  animation: fade-in-up 0.4s var(--ease-out) both;
}

.template-card:nth-child(1) { animation-delay: 0.1s; }
.template-card:nth-child(2) { animation-delay: 0.15s; }
.template-card:nth-child(3) { animation-delay: 0.2s; }

.template-card:hover { border-color: var(--teal-300); box-shadow: var(--shadow-sm); transform: translateY(-2px); }

.template-card.selected {
  border-color: var(--teal-500);
  background: var(--grad-brand-soft);
  box-shadow: var(--shadow-glow-teal);
}

.template-card .tpl-icon { font-size: 22px; margin-bottom: 8px; transition: transform 0.3s var(--ease-spring); }
.template-card:hover .tpl-icon { transform: scale(1.2); }
.template-card .tpl-name { font-size: 13px; font-weight: 600; color: var(--text-primary); }
.template-card .tpl-desc { font-size: 11px; color: var(--text-tertiary); margin-top: 2px; }

.task-form {
  background: rgba(255, 255, 255, 0.9);
  backdrop-filter: blur(16px); -webkit-backdrop-filter: blur(16px);
  border: 1px solid var(--border); border-radius: var(--r-lg);
  padding: 24px;
  display: flex; flex-direction: column; gap: 18px;
  animation: scale-in 0.4s var(--ease-spring) 0.25s both;
}

.form-group label {
  display: block; font-size: 13px; font-weight: 600;
  color: var(--text-primary); margin-bottom: 6px;
}

.form-group label .label-hint { font-weight: 400; color: var(--text-tertiary); font-size: 12px; }

.form-input, .form-select, .form-textarea {
  width: 100%; padding: 10px 14px;
  border: 1.5px solid var(--border); border-radius: var(--r-md);
  font-family: var(--font-body); font-size: 14px;
  color: var(--text-primary); background: rgba(255, 255, 255, 0.8);
  transition: all 0.2s var(--ease); outline: none;
}

.form-input:focus, .form-select:focus, .form-textarea:focus {
  border-color: var(--teal-400);
  box-shadow: 0 0 0 4px rgba(20, 184, 166, 0.08);
  background: white;
}

.form-textarea { resize: vertical; min-height: 80px; line-height: 1.5; }

.form-row { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; }

.form-actions { display: flex; justify-content: flex-end; gap: 10px; padding-top: 4px; }

.btn {
  padding: 10px 20px; border-radius: var(--r-md);
  font-family: var(--font-body); font-size: 13.5px; font-weight: 600;
  cursor: pointer; transition: all 0.2s var(--ease);
  border: 1px solid transparent;
  display: inline-flex; align-items: center; gap: 6px;
}

.btn-secondary { background: var(--bg-card); border-color: var(--border); color: var(--text-primary); }
.btn-secondary:hover { background: var(--bg-hover); transform: translateY(-1px); }

.btn-primary {
  background: var(--grad-teal-indigo); background-size: 200% 200%;
  animation: gradient-flow 5s var(--ease) infinite;
  color: white; box-shadow: var(--shadow-glow-teal);
  position: relative; overflow: hidden;
}

.btn-primary::before {
  content: ''; position: absolute; inset: 0;
  background: linear-gradient(90deg, transparent, rgba(255, 255, 255, 0.25), transparent);
  background-size: 200% 100%; animation: shimmer 3s linear infinite;
}

.btn-primary:hover:not(:disabled) { transform: translateY(-2px); box-shadow: var(--shadow-glow-indigo); }
.btn-primary:disabled { opacity: 0.6; cursor: not-allowed; }

.btn svg { width: 16px; height: 16px; position: relative; z-index: 1; }
.btn span { position: relative; z-index: 1; }
</style>
