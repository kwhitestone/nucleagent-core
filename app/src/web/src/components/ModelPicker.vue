<script setup lang="ts">
/**
 * 模型选择器 —— 三处入口（Home / TaskSetup / Conversation）共用。
 *
 * 用原生 <select> 而不自绘浮层：本项目没有可复用的下拉组件（全仓库只有几处原生
 * select，且 element-plus 已被刻意弃用 —— 见 main.ts 注释，其样式与 Aurora 冲突）。
 * 原生 select 的键盘操作与可访问性天然正确，先用它，需要更花的样式再说。
 *
 * value 编码成 "{providerId}:{model}"：**光有模型名不足以定位 provider** ——
 * 后端 llmproxy 按 providerId 查库解密 API key，同名模型可能挂在不同 provider 下。
 */
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { listProviders } from "@/api/provider";
import { toast } from "@/composables/useToast";
import type { ModelChoice, Provider } from "@/api/types";

const props = withDefaults(
  defineProps<{
    /** 当前选择；null 表示「用服务端默认」。 */
    modelValue: ModelChoice | null;
    disabled?: boolean;
    /** 紧凑态：Home 的图标行空间紧张，不显示 label 文字。 */
    compact?: boolean;
  }>(),
  { disabled: false, compact: false },
);

const emit = defineEmits<{ "update:modelValue": [value: ModelChoice | null] }>();

const { t } = useI18n();
const providers = ref<Provider[]>([]);
const loading = ref(false);

/**
 * 取 provider 的模型清单。
 *
 * config 与 models 都是可选，且 config 是自由 JSON —— 必须做 Array.isArray 守卫，
 * 否则脏数据会让整个下拉炸掉（与 Providers.vue 的 modelsOf 同一处理）。
 */
function modelsOf(p: Provider): string[] {
  const m = p.config?.models;
  return Array.isArray(m) ? m.filter((x): x is string => typeof x === "string") : [];
}

/** 只保留启用且配了模型清单的 provider —— 没配 models 的选不出东西来。 */
const usable = computed(() => providers.value.filter((p) => p.isActive && modelsOf(p).length > 0));

/** 当前选中项编码。空串 = 用服务端默认。 */
const selected = computed(() =>
  props.modelValue ? `${props.modelValue.providerId}:${props.modelValue.model}` : "",
);

function onChange(event: Event): void {
  const raw = (event.target as HTMLSelectElement).value;
  if (!raw) {
    emit("update:modelValue", null);
    return;
  }
  // 模型名里可能含冒号，只按第一个冒号切分。
  const idx = raw.indexOf(":");
  const providerId = Number(raw.slice(0, idx));
  const model = raw.slice(idx + 1);
  if (!providerId || !model) {
    emit("update:modelValue", null);
    return;
  }
  emit("update:modelValue", { providerId, model });
}

onMounted(async () => {
  loading.value = true;
  try {
    providers.value = await listProviders();
  } catch {
    // 取不到清单不阻断发送：留空即用服务端默认模型。
    toast.warning(t("common.modelLoadFailed"));
  } finally {
    loading.value = false;
  }
});
</script>

<template>
  <label class="model-picker" :class="{ compact }">
    <span v-if="!compact" class="model-picker-label">{{ t('common.model') }}</span>
    <select
      class="model-picker-select"
      :value="selected"
      :disabled="disabled || loading"
      :title="t('common.model')"
      @change="onChange"
    >
      <!-- 空值 = 用服务端默认模型（executor 的兜底配置） -->
      <option value="">{{ t('common.modelDefault') }}</option>
      <optgroup v-for="p in usable" :key="p.id" :label="p.name">
        <option v-for="m in modelsOf(p)" :key="`${p.id}:${m}`" :value="`${p.id}:${m}`">
          {{ m }}
        </option>
      </optgroup>
    </select>
  </label>
</template>

<style scoped>
.model-picker {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  font-family: var(--font-body);
}

.model-picker-label {
  color: var(--text-secondary);
  white-space: nowrap;
}

.model-picker-select {
  max-width: 190px;
  padding: 5px 8px;
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-card);
  color: var(--text-primary);
  font: inherit;
  cursor: pointer;
  text-overflow: ellipsis;
}

.model-picker-select:focus {
  outline: none;
  border-color: var(--teal-300);
}

.model-picker-select:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

/* 紧凑态用于 Home 的 36px 图标行：再窄一点，避免挤压 textarea。 */
.model-picker.compact .model-picker-select {
  max-width: 132px;
  padding: 4px 6px;
  font-size: 12.5px;
  border-color: transparent;
  background: transparent;
  color: var(--text-tertiary);
}

.model-picker.compact .model-picker-select:hover:not(:disabled) {
  color: var(--text-primary);
  background: var(--bg-hover);
}
</style>
