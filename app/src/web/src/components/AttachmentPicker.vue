<script setup lang="ts">
/**
 * 附件选择器 —— 隐藏的 file input + 回形针按钮（chip 列表见 AttachmentChips.vue）。
 *
 * 三个入口（Home / Conversation / TaskSetup）共用本组件，避免三份重复实现。
 *
 * 为什么手写而不用 el-upload：element-plus 虽然还在 package.json 里，但已被
 * 刻意弃用（见 main.ts 注释：其样式与 Aurora 设计冲突）。
 *
 * 上传时机是**选中即传**，不是等提交时才传：
 *   - 用户能立刻看到进度与失败，而不是点了发送才卡住；
 *   - 拿到 fileId 后再发对话请求，后端可即时校验附件归属。
 */
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";
import { MAX_UPLOAD_BYTES, UploadError, uploadFile } from "@/api/storage";
import { toast } from "@/composables/useToast";
import type { MessageAttachment } from "@/api/types";

const props = withDefaults(
  defineProps<{
    /** 已选附件（由父组件持有，本组件通过 v-model 更新）。 */
    modelValue: MessageAttachment[];
    /** 附件数量上限。与后端 maxAttachmentsPerMessage 对齐。 */
    max?: number;
    /** 是否显示按钮文字（Conversation 工具条是带文字态，Home 是纯图标态）。 */
    showLabel?: boolean;
    disabled?: boolean;
  }>(),
  {
    max: 10,
    showLabel: false,
    disabled: false,
  },
);

const emit = defineEmits<{
  "update:modelValue": [value: MessageAttachment[]];
}>();

const { t } = useI18n();
const fileInput = ref<HTMLInputElement | null>(null);
/** 正在上传的文件名 → 进度百分比。 */
const uploading = ref<Record<string, number>>({});

const busy = computed(() => Object.keys(uploading.value).length > 0);
const maxMB = Math.floor(MAX_UPLOAD_BYTES / 1024 / 1024);

/** 供父组件显示"上传中"状态。 */
defineExpose({ busy });

function pick(): void {
  if (props.disabled) return;
  fileInput.value?.click();
}

async function onFilesSelected(event: Event): Promise<void> {
  const target = event.target as HTMLInputElement;
  const files = Array.from(target.files ?? []);
  // 立刻清空 input：否则连续选同一个文件不会触发 change 事件。
  target.value = "";
  if (files.length === 0) return;

  for (const file of files) {
    if (props.modelValue.length + Object.keys(uploading.value).length >= props.max) {
      toast.warning(t("common.attachmentTooMany", { max: props.max }));
      break;
    }
    if (file.size > MAX_UPLOAD_BYTES) {
      toast.warning(t("common.attachmentTooLarge", { size: maxMB }));
      continue;
    }
    await upload(file);
  }
}

async function upload(file: File): Promise<void> {
  uploading.value = { ...uploading.value, [file.name]: 0 };
  try {
    const att = await uploadFile(file, (loaded, total) => {
      const pct = total > 0 ? Math.round((loaded / total) * 100) : 0;
      uploading.value = { ...uploading.value, [file.name]: pct };
    });
    emit("update:modelValue", [...props.modelValue, att]);
  } catch (error) {
    const msg = error instanceof UploadError ? error.message : t("common.attachmentUploadFailed");
    toast.error(`${file.name}: ${msg}`);
  } finally {
    // 用新对象而非 delete，保证 Vue 侦测到变化。
    const next = { ...uploading.value };
    delete next[file.name];
    uploading.value = next;
  }
}
</script>

<template>
  <div class="attachment-picker">
    <!-- 隐藏的真实 input。CDP 的 DOM.setFileInputFiles 也是驱动它。 -->
    <input ref="fileInput" type="file" multiple class="attachment-input" @change="onFilesSelected" />
    <button
      class="attachment-btn"
      :class="{ active: modelValue.length > 0 }"
      type="button"
      :title="t('common.attachment')"
      :disabled="disabled || busy"
      @click="pick"
    >
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21.44 11.05l-9.19 9.19a6 6 0 01-8.49-8.49l9.19-9.19a4 4 0 015.66 5.66l-9.2 9.19a2 2 0 01-2.83-2.83l8.49-8.48" /></svg>
      <span v-if="showLabel">{{ busy ? t('common.attachmentUploading') : t('common.attachment') }}</span>
      <span v-if="modelValue.length > 0" class="attachment-count">{{ modelValue.length }}</span>
    </button>
  </div>
</template>

<style scoped>
.attachment-picker {
  display: inline-flex;
  align-items: center;
}

.attachment-input {
  display: none;
}

.attachment-btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  background: transparent;
  border: none;
  color: var(--text-tertiary);
  cursor: pointer;
  border-radius: var(--r-sm);
  padding: 6px 8px;
  font-size: 13px;
  font-family: var(--font-body);
  transition:
    color 0.2s var(--ease-out),
    background 0.2s var(--ease-out);
}

.attachment-btn:hover:not(:disabled) {
  color: var(--text-primary);
  background: var(--bg-hover);
}

.attachment-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.attachment-btn.active {
  color: var(--teal-600);
}

.attachment-btn svg {
  width: 18px;
  height: 18px;
}

.attachment-count {
  min-width: 16px;
  height: 16px;
  padding: 0 4px;
  border-radius: 8px;
  background: var(--teal-600);
  color: #fff;
  font-size: 11px;
  line-height: 16px;
  text-align: center;
}
</style>
