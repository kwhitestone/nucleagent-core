<script setup lang="ts">
/**
 * 附件 chip 列表。两种用法：
 *   - removable=true：composer 里的待发送附件，可移除；
 *   - removable=false：消息气泡里的已发送附件，只可点击下载。
 *
 * 为什么是独立组件而不是把 chip 拼进气泡 HTML：消息正文走 marked + DOMPurify
 * （Conversation.vue 的 renderMarkdown），DOMPurify 会剥掉未知属性和事件处理，
 * 注入的 chip 点不动。必须是真实 Vue 节点。
 */
import { useI18n } from "vue-i18n";
import { getDownloadUrl } from "@/api/storage";
import { toast } from "@/composables/useToast";
import type { MessageAttachment } from "@/api/types";

withDefaults(
  defineProps<{
    attachments: MessageAttachment[];
    removable?: boolean;
  }>(),
  { removable: false },
);

const emit = defineEmits<{ remove: [fileId: string] }>();

const { t } = useI18n();

/** 人类可读的文件大小。 */
function formatSize(bytes?: number): string {
  if (!bytes) return "";
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}

/**
 * 点击下载。签名 URL 现取现用（有效期 1800s），不预先取好挂在 DOM 上 ——
 * 页面开着几小时后那些链接就全失效了。
 */
async function download(att: MessageAttachment): Promise<void> {
  try {
    const url = await getDownloadUrl(att.fileId);
    window.open(url, "_blank", "noopener,noreferrer");
  } catch {
    toast.error(t("common.attachmentDownloadFailed"));
  }
}
</script>

<template>
  <div v-if="attachments.length > 0" class="attachment-chips">
    <div v-for="att in attachments" :key="att.fileId" class="attachment-chip">
      <span class="att-icon" aria-hidden="true">
        <svg v-if="att.kind === 'image'" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="18" height="18" rx="2" /><circle cx="8.5" cy="8.5" r="1.5" /><polyline points="21 15 16 10 5 21" /></svg>
        <svg v-else viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 00-2 2v16a2 2 0 002 2h12a2 2 0 002-2V8z" /><polyline points="14 2 14 8 20 8" /></svg>
      </span>
      <button class="att-name" type="button" :title="att.name" @click="download(att)">
        {{ att.name }}
      </button>
      <span v-if="att.size" class="att-size">{{ formatSize(att.size) }}</span>
      <button
        v-if="removable"
        class="att-remove"
        type="button"
        :title="t('common.attachmentRemove')"
        @click="emit('remove', att.fileId)"
      >
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18" /><line x1="6" y1="6" x2="18" y2="18" /></svg>
      </button>
    </div>
  </div>
</template>

<style scoped>
.attachment-chips {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 8px;
}

.attachment-chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  max-width: 260px;
  padding: 4px 8px;
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-card);
  font-size: 12.5px;
  font-family: var(--font-body);
}

.att-icon {
  display: inline-flex;
  color: var(--text-tertiary);
}

.att-icon svg {
  width: 14px;
  height: 14px;
}

.att-name {
  background: none;
  border: none;
  padding: 0;
  color: var(--text-primary);
  cursor: pointer;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font: inherit;
}

.att-name:hover {
  color: var(--teal-600);
  text-decoration: underline;
}

.att-size {
  color: var(--text-tertiary);
  flex-shrink: 0;
}

.att-remove {
  display: inline-flex;
  background: none;
  border: none;
  padding: 0;
  color: var(--text-tertiary);
  cursor: pointer;
  flex-shrink: 0;
}

.att-remove:hover {
  color: var(--rose-500);
}

.att-remove svg {
  width: 13px;
  height: 13px;
}
</style>
