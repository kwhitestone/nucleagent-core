<script setup lang="ts">
/**
 * Provider 管理视图 —— LLM 提供商的增删改查。
 *
 * 设计约束（来自后端契约，不是 UI 偏好）：
 *   apiKey 永远读不回来。model.Provider 的 APIKey json tag 是 "-"，列表/详情
 *   都不含密钥。因此：
 *     - 新建时 apiKey 必填；
 *     - 编辑时留空 = 保持原密钥不变（后端 router.go:184 判空跳过），
 *       填了才会重新加密覆盖。
 *   UI 必须如实表达这一点，绝不能显示假的掩码值让人误以为读到了密钥。
 *
 * config 是自由 JSON，但 llmproxy 只认四个键（baseUrl/apiFormat/authScheme/
 * models），所以这里用结构化表单而非裸 JSON 编辑框——baseUrl 拼错会导致
 * 代理 410/404，交给表单校验比交给用户手写 JSON 可靠。
 */
import { computed, onMounted, reactive, ref } from "vue";
import { useI18n } from "vue-i18n";
import { ApiError } from "@/api/http";
import {
  createProvider,
  deleteProvider,
  listProviders,
  updateProvider,
} from "@/api/provider";
import { toast } from "@/composables/useToast";
import type { Provider } from "@/api/types";

const { t } = useI18n();

const providers = ref<Provider[]>([]);
const loading = ref(true);
const saving = ref(false);

/** 编辑中的 provider id；null = 新建模式；undefined = 表单未打开。 */
const editingId = ref<number | null | undefined>(undefined);
const formOpen = computed(() => editingId.value !== undefined);
const isCreate = computed(() => editingId.value === null);

const API_FORMATS = ["openai", "anthropic"] as const;
const AUTH_SCHEMES = ["bearer", "api_key"] as const;

const form = reactive({
  name: "",
  apiKey: "",
  baseUrl: "",
  apiFormat: "openai" as string,
  authScheme: "bearer" as string,
  /** 逗号/换行分隔的模型列表，提交时切成数组。 */
  models: "",
  isActive: true,
});

function resetForm(): void {
  form.name = "";
  form.apiKey = "";
  form.baseUrl = "";
  form.apiFormat = "openai";
  form.authScheme = "bearer";
  form.models = "";
  form.isActive = true;
}

async function load(): Promise<void> {
  loading.value = true;
  try {
    providers.value = await listProviders();
  } catch (error) {
    toast.error(error instanceof ApiError ? error.message : t("provider.loadFailed"));
  } finally {
    loading.value = false;
  }
}

onMounted(load);

function openCreate(): void {
  resetForm();
  editingId.value = null;
}

function openEdit(p: Provider): void {
  const cfg = p.config ?? {};
  form.name = p.name;
  form.apiKey = ""; // 读不回来，留空表示不修改
  form.baseUrl = cfg.baseUrl ?? "";
  form.apiFormat = cfg.apiFormat || "openai";
  form.authScheme = cfg.authScheme || "bearer";
  form.models = Array.isArray(cfg.models) ? cfg.models.join(", ") : "";
  form.isActive = p.isActive;
  editingId.value = p.id;
}

function closeForm(): void {
  editingId.value = undefined;
}

/** 逗号/换行分隔 → 去空白、去重的数组。 */
function parseModels(raw: string): string[] {
  const out: string[] = [];
  for (const part of raw.split(/[,\n]/)) {
    const s = part.trim();
    if (s !== "" && !out.includes(s)) out.push(s);
  }
  return out;
}

function buildConfig() {
  return {
    baseUrl: form.baseUrl.trim(),
    apiFormat: form.apiFormat,
    authScheme: form.authScheme,
    models: parseModels(form.models),
  };
}

/** baseUrl 必须是 http(s)——后端 ResolveProvider 以此做 SSRF 防护，会直接拒绝。 */
function validate(): string | null {
  if (!form.name.trim()) return t("provider.errNameRequired");
  const url = form.baseUrl.trim();
  if (!url) return t("provider.errBaseUrlRequired");
  if (!/^https?:\/\//.test(url)) return t("provider.errBaseUrlScheme");
  if (isCreate.value && !form.apiKey.trim()) return t("provider.errApiKeyRequired");
  return null;
}

async function submit(): Promise<void> {
  if (saving.value) return;
  const invalid = validate();
  if (invalid) {
    toast.error(invalid);
    return;
  }
  saving.value = true;
  try {
    if (isCreate.value) {
      await createProvider({
        name: form.name.trim(),
        apiKey: form.apiKey.trim(),
        config: buildConfig(),
        isActive: form.isActive,
      });
      toast.success(t("provider.created"));
    } else {
      const key = form.apiKey.trim();
      await updateProvider(editingId.value as number, {
        name: form.name.trim(),
        // 留空则不传该字段，后端保持原密钥。
        ...(key ? { apiKey: key } : {}),
        config: buildConfig(),
        isActive: form.isActive,
      });
      toast.success(t("provider.updated"));
    }
    closeForm();
    await load();
  } catch (error) {
    toast.error(error instanceof ApiError ? error.message : t("common.operationFailed"));
  } finally {
    saving.value = false;
  }
}

/** 快速启停：直接 PATCH isActive，不进表单。 */
async function toggleActive(p: Provider): Promise<void> {
  try {
    await updateProvider(p.id, { isActive: !p.isActive });
    await load();
  } catch (error) {
    toast.error(error instanceof ApiError ? error.message : t("common.operationFailed"));
  }
}

async function remove(p: Provider): Promise<void> {
  if (!window.confirm(t("provider.confirmDelete", { name: p.name }))) return;
  try {
    await deleteProvider(p.id);
    toast.success(t("provider.deleted"));
    await load();
  } catch (error) {
    toast.error(error instanceof ApiError ? error.message : t("common.operationFailed"));
  }
}

function modelsOf(p: Provider): string[] {
  const m = p.config?.models;
  return Array.isArray(m) ? m : [];
}
</script>

<template>
  <div class="view active">
    <div class="providers-view">
      <div class="providers-header">
        <div>
          <h1 class="page-title">{{ t('provider.title') }}</h1>
          <p class="page-subtitle">{{ t('provider.subtitle') }}</p>
        </div>
        <button class="btn-primary" type="button" @click="openCreate">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="12" y1="5" x2="12" y2="19" /><line x1="5" y1="12" x2="19" y2="12" /></svg>
          {{ t('provider.add') }}
        </button>
      </div>

      <!-- 密钥只写不读，明确告知用户，避免「为什么看不到我的 key」的困惑 -->
      <div class="key-notice">
        <span class="notice-icon">🔒</span>
        <span>{{ t('provider.keyNotice') }}</span>
      </div>

      <p v-if="loading" class="providers-empty">{{ t('common.loading') }}</p>
      <p v-else-if="providers.length === 0" class="providers-empty">{{ t('provider.empty') }}</p>

      <div v-else class="provider-list">
        <div v-for="p in providers" :key="p.id" class="provider-card" :class="{ inactive: !p.isActive }">
          <div class="provider-main">
            <div class="provider-title-row">
              <span class="provider-name">{{ p.name }}</span>
              <span class="provider-badge" :class="p.isActive ? 'on' : 'off'">
                {{ p.isActive ? t('provider.active') : t('provider.inactive') }}
              </span>
              <span v-if="p.config?.apiFormat" class="provider-tag">{{ p.config.apiFormat }}</span>
            </div>
            <div class="provider-url">{{ p.config?.baseUrl || t('provider.noBaseUrl') }}</div>
            <div v-if="modelsOf(p).length" class="provider-models">
              <span v-for="m in modelsOf(p)" :key="m" class="model-chip">{{ m }}</span>
            </div>
          </div>
          <div class="provider-actions">
            <button class="btn-ghost" type="button" @click="toggleActive(p)">
              {{ p.isActive ? t('provider.disable') : t('provider.enable') }}
            </button>
            <button class="btn-ghost" type="button" @click="openEdit(p)">{{ t('provider.edit') }}</button>
            <button class="btn-ghost danger" type="button" @click="remove(p)">{{ t('provider.delete') }}</button>
          </div>
        </div>
      </div>
    </div>

    <!-- 表单弹层 -->
    <div v-if="formOpen" class="modal-mask" @click.self="closeForm">
      <div class="modal-card">
        <div class="modal-header">
          <h2>{{ isCreate ? t('provider.add') : t('provider.editTitle') }}</h2>
          <button class="modal-close" type="button" @click="closeForm">×</button>
        </div>

        <div class="modal-body">
          <label class="field">
            <span class="field-label">{{ t('provider.fieldName') }}</span>
            <input v-model="form.name" type="text" :placeholder="t('provider.phName')" />
          </label>

          <label class="field">
            <span class="field-label">
              {{ t('provider.fieldApiKey') }}
              <em v-if="!isCreate" class="field-hint">{{ t('provider.apiKeyEditHint') }}</em>
            </span>
            <input
              v-model="form.apiKey"
              type="password"
              autocomplete="new-password"
              :placeholder="isCreate ? t('provider.phApiKey') : t('provider.phApiKeyEdit')"
            />
          </label>

          <label class="field">
            <span class="field-label">{{ t('provider.fieldBaseUrl') }}</span>
            <input v-model="form.baseUrl" type="text" placeholder="https://open.bigmodel.cn/api/paas/v4" />
          </label>

          <div class="field-row">
            <label class="field">
              <span class="field-label">{{ t('provider.fieldApiFormat') }}</span>
              <select v-model="form.apiFormat">
                <option v-for="f in API_FORMATS" :key="f" :value="f">{{ f }}</option>
              </select>
            </label>
            <label class="field">
              <span class="field-label">{{ t('provider.fieldAuthScheme') }}</span>
              <select v-model="form.authScheme">
                <option v-for="s in AUTH_SCHEMES" :key="s" :value="s">{{ s }}</option>
              </select>
            </label>
          </div>

          <label class="field">
            <span class="field-label">{{ t('provider.fieldModels') }}</span>
            <textarea v-model="form.models" rows="2" :placeholder="t('provider.phModels')" />
          </label>

          <label class="field-check">
            <input v-model="form.isActive" type="checkbox" />
            <span>{{ t('provider.fieldActive') }}</span>
          </label>
        </div>

        <div class="modal-footer">
          <button class="btn-ghost" type="button" @click="closeForm">{{ t('common.cancel') }}</button>
          <button class="btn-primary" type="button" :disabled="saving" @click="submit">
            {{ saving ? t('common.sending') : t('provider.save') }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.providers-view {
  max-width: 880px; margin: 0 auto; width: 100%;
  padding: 32px 24px 48px;
}

.providers-header {
  display: flex; align-items: flex-start; justify-content: space-between;
  gap: 16px; margin-bottom: 16px;
}

.page-title { font-size: 22px; font-weight: 700; color: var(--text-primary); margin-bottom: 4px; }
.page-subtitle { font-size: 13px; color: var(--text-secondary); }

.key-notice {
  display: flex; align-items: center; gap: 8px;
  padding: 10px 14px; margin-bottom: 20px;
  background: var(--grad-brand-soft); border-radius: var(--r-md);
  font-size: 12.5px; color: var(--text-secondary);
}
.notice-icon { font-size: 13px; }

.providers-empty { color: var(--text-tertiary); text-align: center; padding: 40px 0; font-size: 14px; }

.provider-list { display: flex; flex-direction: column; gap: 12px; }

.provider-card {
  display: flex; align-items: center; justify-content: space-between; gap: 16px;
  padding: 16px 18px;
  background: var(--bg-card); border: 1px solid var(--border);
  border-radius: var(--r-lg); box-shadow: var(--shadow-xs);
  transition: all 0.25s var(--ease);
}
.provider-card:hover { border-color: var(--border-strong); box-shadow: var(--shadow-md); }
.provider-card.inactive { opacity: 0.6; }

.provider-main { min-width: 0; flex: 1; }
.provider-title-row { display: flex; align-items: center; gap: 8px; margin-bottom: 4px; flex-wrap: wrap; }
.provider-name { font-size: 15px; font-weight: 600; color: var(--text-primary); }

.provider-badge {
  font-size: 11px; padding: 2px 8px; border-radius: 999px; font-weight: 500;
}
.provider-badge.on { background: #d1fae5; color: #047857; }
.provider-badge.off { background: var(--slate-100); color: var(--text-tertiary); }

.provider-tag {
  font-size: 11px; padding: 2px 8px; border-radius: 999px;
  background: var(--grad-brand-soft); color: var(--indigo-600); font-family: var(--font-mono);
}

.provider-url {
  font-family: var(--font-mono); font-size: 12px; color: var(--text-tertiary);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}

.provider-models { display: flex; flex-wrap: wrap; gap: 5px; margin-top: 7px; }
.model-chip {
  font-size: 11px; padding: 2px 7px; border-radius: var(--r-sm);
  background: var(--bg-hover); color: var(--text-secondary); font-family: var(--font-mono);
}

.provider-actions { display: flex; gap: 4px; flex-shrink: 0; }

.btn-primary {
  display: flex; align-items: center; gap: 6px;
  padding: 8px 16px; border: none; border-radius: var(--r-md);
  background: var(--grad-teal-indigo); background-size: 200% 200%;
  color: white; font-size: 13px; font-weight: 600; cursor: pointer;
  box-shadow: var(--shadow-sm); transition: all 0.25s var(--ease);
  white-space: nowrap;
}
.btn-primary:hover:not(:disabled) { transform: translateY(-1px); box-shadow: var(--shadow-md); }
.btn-primary:disabled { opacity: 0.6; cursor: not-allowed; }
.btn-primary svg { width: 15px; height: 15px; }

.btn-ghost {
  padding: 6px 12px; border: 1px solid var(--border); border-radius: var(--r-sm);
  background: transparent; color: var(--text-secondary);
  font-size: 12.5px; cursor: pointer; transition: all 0.2s var(--ease); white-space: nowrap;
}
.btn-ghost:hover { background: var(--bg-hover); color: var(--text-primary); border-color: var(--border-strong); }
.btn-ghost.danger:hover { background: #fef2f2; color: #dc2626; border-color: #fecaca; }

/* 弹层 */
.modal-mask {
  position: fixed; inset: 0; z-index: 1000;
  background: rgba(15, 23, 42, 0.4);
  backdrop-filter: blur(4px); -webkit-backdrop-filter: blur(4px);
  display: flex; align-items: center; justify-content: center; padding: 24px;
}

.modal-card {
  width: 100%; max-width: 520px; max-height: 90vh; overflow-y: auto;
  background: var(--bg-card); border-radius: var(--r-xl);
  box-shadow: var(--shadow-xl); border: 1px solid var(--border);
  animation: fade-in-up 0.3s var(--ease-out) both;
}

.modal-header {
  display: flex; align-items: center; justify-content: space-between;
  padding: 18px 22px 12px;
}
.modal-header h2 { font-size: 16px; font-weight: 600; color: var(--text-primary); }
.modal-close {
  border: none; background: transparent; font-size: 22px; line-height: 1;
  color: var(--text-tertiary); cursor: pointer; padding: 0 4px;
}
.modal-close:hover { color: var(--text-primary); }

.modal-body { padding: 4px 22px 8px; display: flex; flex-direction: column; gap: 14px; }

.field { display: flex; flex-direction: column; gap: 5px; flex: 1; }
.field-row { display: flex; gap: 12px; }
.field-label { font-size: 12.5px; font-weight: 500; color: var(--text-secondary); }
.field-hint { font-style: normal; font-size: 11.5px; color: var(--text-tertiary); margin-left: 6px; }

.field input[type="text"],
.field input[type="password"],
.field select,
.field textarea {
  width: 100%; padding: 9px 12px;
  border: 1.5px solid var(--border); border-radius: var(--r-sm);
  background: var(--bg); color: var(--text-primary);
  font-family: var(--font-body); font-size: 13.5px;
  outline: none; transition: all 0.2s var(--ease); resize: vertical;
}
.field input:focus, .field select:focus, .field textarea:focus {
  border-color: var(--teal-300); box-shadow: 0 0 0 3px rgba(20, 184, 166, 0.08);
}

.field-check { display: flex; align-items: center; gap: 8px; font-size: 13px; color: var(--text-secondary); cursor: pointer; }
.field-check input { width: 15px; height: 15px; cursor: pointer; }

.modal-footer {
  display: flex; justify-content: flex-end; gap: 8px;
  padding: 12px 22px 20px;
}
</style>
