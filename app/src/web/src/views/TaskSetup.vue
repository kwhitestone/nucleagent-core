<script setup lang="ts">
/**
 * 任务视图 - 对齐 design/nucleagent-design.html 第 1981–2046 行。
 *
 * 3 张 .template-card + .task-form（名称/描述/执行模式/输出格式）。
 *
 * 数据来源：GET /api/v1/addons/agent/templates，映射为任务模板卡片。
 * 接口不可用时降级到 i18n 前端常量 + console.warn。
 *
 * 启动任务走 POST /conversation，执行模式/输出格式暂存 metadata 字段
 * （后端暂未持久化，预留给未来字段），不阻塞流程。
 */
import { computed, onMounted, reactive, ref } from "vue";
import { useRouter } from "vue-router";
import { useI18n } from "vue-i18n";
import { ApiError } from "@/api/http";
import { listAgentTemplates } from "@/api/agent";
import { useConversationStore } from "@/store/conversation";
import { toast } from "@/composables/useToast";
import AttachmentPicker from "@/components/AttachmentPicker.vue";
import AttachmentChips from "@/components/AttachmentChips.vue";
import ModelPicker from "@/components/ModelPicker.vue";
import type {
  AgentTemplate,
  ConversationMode,
  MessageAttachment,
  ModelChoice,
} from "@/api/types";

const router = useRouter();
const store = useConversationStore();
const { t } = useI18n();

interface TaskTemplate {
  icon: string;
  name: string;
  desc: string;
  defaultName: string;
  defaultDesc: string;
}

/** 接口不可用时的 i18n 降级常量。 */
const templateKeys: { icon: string; key: string }[] = [
  { icon: "📊", key: "competitiveAnalysis" },
  { icon: "📝", key: "contentCreation" },
  { icon: "🔍", key: "dataResearch" },
];

const fallbackTemplates = computed<TaskTemplate[]>(() =>
  templateKeys.map(({ icon, key }) => ({
    icon,
    name: t(`task.templates.${key}.name`),
    desc: t(`task.templates.${key}.desc`),
    defaultName: t(`task.templates.${key}.defaultName`),
    defaultDesc: t(`task.templates.${key}.defaultDesc`),
  })),
);

const templates = ref<TaskTemplate[]>([...fallbackTemplates.value]);

const ICONS = ["📊", "📝", "🔍", "🤖", "📋", "🔬"];

/** 把后端 AgentTemplate 转成前端 TaskTemplate。 */
function templateToTask(tpl: AgentTemplate, index: number): TaskTemplate {
  const cfg = tpl.config ?? {};
  return {
    icon: ICONS[index % ICONS.length],
    name: tpl.name,
    desc: (cfg.role as string) || (cfg.personality as string) || "",
    defaultName: tpl.name,
    defaultDesc: (cfg.prompt as string) || `使用 ${tpl.name} 执行任务。`,
  };
}

onMounted(async () => {
  try {
    const tpls = await listAgentTemplates();
    if (tpls.length > 0) {
      templates.value = tpls.map((tpl, i) => templateToTask(tpl, i));
      // 选中第一个模板填充表单。
      selectTemplate(0);
    }
  } catch (e) {
    console.warn("[TaskSetup] agent/templates 接口不可用，降级到前端常量", e);
  }
});

const selected = ref(0);
/** 任务附件（选中即已上传到 storage，这里持有引用）。 */
const attachments = ref<MessageAttachment[]>([]);
/** 选定的模型；null = 用服务端默认。 */
const modelChoice = ref<ModelChoice | null>(null);

const form = reactive({
  name: "",
  desc: "",
  execMode: "auto",
  outputFormat: "markdown",
});

// Sync form defaults when templates load or selection changes
function syncFormDefaults(): void {
  const tpl = templates.value[selected.value];
  form.name = tpl.defaultName;
  form.desc = tpl.defaultDesc;
}

// Initialize with first template
syncFormDefaults();

function selectTemplate(i: number): void {
  selected.value = i;
  syncFormDefaults();
}

const execModeOptions = computed(() => [
  { value: "auto", label: t("task.execModes.auto") },
  { value: "stepByStep", label: t("task.execModes.stepByStep") },
  { value: "planOnly", label: t("task.execModes.planOnly") },
]);

const outputFormatOptions = computed(() => [
  { value: "markdown", label: t("task.outputFormats.markdown") },
  { value: "pdf", label: t("task.outputFormats.pdf") },
  { value: "ppt", label: t("task.outputFormats.ppt") },
  { value: "excel", label: t("task.outputFormats.excel") },
]);

const submitting = ref(false);

async function launch(): Promise<void> {
  if (submitting.value) return;
  const name = form.name.trim();
  if (!name) {
    toast.warning(t("task.fillName"));
    return;
  }
  submitting.value = true;
  try {
    // 执行模式/输出格式暂存 metadata（后端暂未持久化，预留给未来字段）。
    const input = form.desc.trim();
    const mode: ConversationMode = "a2a_agent";
    const created = await store.create({
      mode,
      input,
      metadata: {
        execMode: form.execMode,
        outputFormat: form.outputFormat,
        taskName: name,
      },
      attachments: attachments.value.map((a) => ({ fileId: a.fileId, name: a.name })),
      // 模型与 provider 成对下发；未选则都不带，由服务端用默认。
      model: modelChoice.value?.model ?? "",
      providerId: modelChoice.value?.providerId,
    });
    router.push(`/c/${created.id}`);
  } catch (error) {
    toast.error(error instanceof ApiError ? error.message : t("task.launchFailed"));
  } finally {
    submitting.value = false;
  }
}
</script>

<template>
  <div class="view active">
    <div class="task-setup-view">
      <div class="task-setup-header">
        <h2>{{ t('task.title') }}</h2>
        <p>{{ t('task.subtitle') }}</p>
      </div>

      <div class="task-templates">
        <div
          v-for="(tpl, i) in templates"
          :key="i"
          class="template-card"
          :class="{ selected: selected === i }"
          @click="selectTemplate(i)"
        >
          <div class="tpl-icon">{{ tpl.icon }}</div>
          <div class="tpl-name">{{ tpl.name }}</div>
          <div class="tpl-desc">{{ tpl.desc }}</div>
        </div>
      </div>

      <div class="task-form">
        <div class="form-group">
          <label>{{ t('task.form.nameLabel') }} <span class="label-hint">{{ t('task.form.nameHint') }}</span></label>
          <input v-model="form.name" type="text" class="form-input" :placeholder="t('task.form.namePlaceholder')" />
        </div>

        <div class="form-group">
          <label>{{ t('task.form.descLabel') }} <span class="label-hint">{{ t('task.form.descHint') }}</span></label>
          <textarea v-model="form.desc" class="form-textarea" :placeholder="t('task.form.descPlaceholder')" />
        </div>

        <div class="form-group">
          <label>{{ t('common.model') }}</label>
          <ModelPicker v-model="modelChoice" :disabled="submitting" />
        </div>

        <div class="form-group">
          <label>{{ t('common.attachment') }}</label>
          <div>
            <AttachmentPicker v-model="attachments" :disabled="submitting" show-label />
            <AttachmentChips
              :attachments="attachments"
              removable
              @remove="(id: string) => (attachments = attachments.filter((a) => a.fileId !== id))"
            />
          </div>
        </div>

        <div class="form-row">
          <div class="form-group">
            <label>{{ t('task.form.execModeLabel') }}</label>
            <select v-model="form.execMode" class="form-select">
              <option v-for="opt in execModeOptions" :key="opt.value" :value="opt.value">{{ opt.label }}</option>
            </select>
          </div>
          <div class="form-group">
            <label>{{ t('task.form.outputFormatLabel') }}</label>
            <select v-model="form.outputFormat" class="form-select">
              <option v-for="opt in outputFormatOptions" :key="opt.value" :value="opt.value">{{ opt.label }}</option>
            </select>
          </div>
        </div>

        <div class="form-actions">
          <button class="btn btn-secondary" type="button">{{ t('task.saveDraft') }}</button>
          <button class="btn btn-primary" type="button" :disabled="submitting" @click="launch">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polygon points="5 3 19 12 5 21 5 3" /></svg>
            <span>{{ submitting ? t('task.launching') : t('task.launch') }}</span>
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
