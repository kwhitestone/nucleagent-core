import axios from "axios";
import { getAccessToken } from "@/utils/token";
import type { MessageAttachment } from "./types";

/**
 * nucleagent-storage (:26610) client — presign → 直传 → register 三步上传。
 *
 * 关键设计：**字节不经 core，也不经本模块中转**。storage 只签凭证，浏览器把
 * 文件直接 POST 给 CS（cs.101.com）。已实测确认 CS 允许来自本前端源的跨域上传。
 *
 * 为什么不复用 api/http.ts 的实例：
 *   - 那个实例固定 `Content-Type: application/json`（http.ts:23-25），
 *     而 CS 直传要 FormData，得让浏览器自己带 multipart boundary；
 *   - storage 的每个端点都要 `X-Namespace` 头，core 的实例不发这个头。
 *
 * 为什么不走 vite proxy：vite.config.ts 的 `/api` 是 catch-all 指向 core，
 * 而 storage 也用 `/api/v1/*`，挂上去会被 core 代理吞掉。故走跨域直连
 * （storage 的 CORS 白名单已含本前端源）。
 */

/** storage 的命名空间。与后端 storage config.yaml 的 namespaces 对应。 */
const NAMESPACE = "core";

/** storage 服务地址。未配置时回落到本机默认端口。 */
const STORAGE_BASE = import.meta.env.VITE_STORAGE_URL?.trim() || "http://localhost:26610";

/**
 * 单文件大小上限（100MB），与 storage 的 max-size 对齐。
 *
 * 前端也校验一遍是为了即时反馈：等把 100MB 传完再被后端拒绝，体验很差。
 */
export const MAX_UPLOAD_BYTES = 100 * 1024 * 1024;

const storageHttp = axios.create({
  baseURL: STORAGE_BASE,
  timeout: 20000,
});

storageHttp.interceptors.request.use((config) => {
  const token = getAccessToken();
  if (token) {
    config.headers.Authorization = `Bearer ${token}`;
  }
  // storage 用它做命名空间隔离（决定文件落在 CS 的哪个路径前缀下）。
  config.headers["X-Namespace"] = NAMESPACE;
  return config;
});

/** storage 的 { code, message, data } 信封。 */
interface Envelope<T> {
  code?: number;
  message?: string;
  data?: T;
}

/** POST /upload/presign 的响应数据。 */
interface PresignData {
  fileId: string;
  objectKey: string;
  method: string;
  uploadUrl: string;
  headers?: Record<string, string>;
  /** CS 后端必填的 multipart 表单字段（path/scope/name 等）。 */
  formFields?: Record<string, string>;
  /** 文件内容的表单字段名（CS 实测为 "filename"，不要硬编码）。 */
  fileField?: string;
  storedUrl?: string;
  /** 凭证过期时间（Unix 毫秒）。 */
  expiresAt: number;
}

/** 上传进度回调。 */
export type UploadProgress = (loaded: number, total: number) => void;

/** 上传过程中的可读错误。 */
export class UploadError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "UploadError";
  }
}

/**
 * 计算文件的 SHA-256（hex）。
 *
 * 用途是端到端完整性校验：storage 存进元数据，后续可与下载内容比对。
 * crypto.subtle 只在安全上下文（https 或 localhost）可用，取不到时返回空串 ——
 * 校验和是增强项，不能因为它缺失就阻断上传。
 */
async function sha256Hex(file: File): Promise<string> {
  if (!globalThis.crypto?.subtle) return "";
  try {
    const buf = await file.arrayBuffer();
    const digest = await crypto.subtle.digest("SHA-256", buf);
    return Array.from(new Uint8Array(digest))
      .map((b) => b.toString(16).padStart(2, "0"))
      .join("");
  } catch {
    return "";
  }
}

/**
 * 上传一个文件，返回可直接塞进对话请求的附件引用。
 *
 * 三步：
 *   1. presign  —— 向 storage 换上传凭证（拿 fileId + 已签名的 CS 地址）
 *   2. 直传     —— 浏览器把字节 POST 给 CS，拿回 dentry_id
 *   3. register —— 把 dentry_id 回填给 storage，记录置为 active
 *
 * 第 3 步必须回传 dentryId 而非签名 URL：storage 会把 dentryId 收敛成
 * `cs-dentry://`（服务端 resolveStoredURL 的正路）。若回传签名 URL，那条
 * 带过期 token 的地址会被存进 DB，几小时后文件永久 403。
 */
export async function uploadFile(
  file: File,
  onProgress?: UploadProgress,
): Promise<MessageAttachment> {
  if (file.size > MAX_UPLOAD_BYTES) {
    throw new UploadError(`文件超过 ${Math.floor(MAX_UPLOAD_BYTES / 1024 / 1024)}MB 上限`);
  }

  // 1. presign
  const presignResp = await storageHttp.post<Envelope<PresignData>>("/api/v1/upload/presign", {
    filename: file.name,
    contentType: file.type || "application/octet-stream",
    size: file.size,
  });
  const presign = presignResp.data?.data;
  if (!presign?.uploadUrl || !presign.fileId) {
    throw new UploadError("获取上传凭证失败");
  }

  // 2. 直传存储后端（CS 或 local blob 端点）。
  const dentryId = await uploadBytes(file, presign, onProgress);

  // 3. register：补齐元数据并置 active。
  const sha256 = await sha256Hex(file);
  const mimeType = file.type || "application/octet-stream";
  const registerResp = await storageHttp.post<Envelope<{ fileId: string; status: string }>>(
    "/api/v1/files",
    {
      fileId: presign.fileId,
      dentryId,
      name: file.name,
      size: file.size,
      mimeType,
      sha256,
    },
  );
  if (!registerResp.data?.data?.fileId) {
    throw new UploadError("注册文件元数据失败");
  }

  return {
    fileId: presign.fileId,
    name: file.name,
    mimeType,
    size: file.size,
    sha256,
  };
}

/**
 * 把字节直传到存储后端，返回 dentry_id（CS 后端才有；local 后端返回空串）。
 *
 * 用原生 XHR 而非 fetch：需要上传进度（fetch 没有 upload progress 事件）。
 * 刻意**不带 Authorization**：签名已在 URL 里，多带头会触发 CORS 预检从而失败。
 */
function uploadBytes(
  file: File,
  presign: PresignData,
  onProgress?: UploadProgress,
): Promise<string> {
  return new Promise((resolve, reject) => {
    const form = new FormData();
    // 表单字段与文件字段名都必须用 presign 返回的值 —— CS 的签名 policy 里
    // 含 path 等字段，自己拼会与签名不符。
    for (const [k, v] of Object.entries(presign.formFields ?? {})) {
      form.append(k, v);
    }
    form.append(presign.fileField || "filename", file, file.name);

    const xhr = new XMLHttpRequest();
    xhr.open(presign.method || "POST", presign.uploadUrl, true);
    for (const [k, v] of Object.entries(presign.headers ?? {})) {
      xhr.setRequestHeader(k, v);
    }
    if (onProgress) {
      xhr.upload.onprogress = (e) => {
        if (e.lengthComputable) onProgress(e.loaded, e.total);
      };
    }
    xhr.onload = () => {
      if (xhr.status < 200 || xhr.status >= 300) {
        reject(new UploadError(`上传失败（${xhr.status}）`));
        return;
      }
      // CS 返回 { dentry_id, inode: {...} }（snake_case 是 CS 自己的约定）。
      // local 后端没有 dentry_id，返回空串让 storage 用 presign 预置地址。
      try {
        const body = JSON.parse(xhr.responseText || "{}") as { dentry_id?: string };
        resolve(body.dentry_id ?? "");
      } catch {
        resolve("");
      }
    };
    xhr.onerror = () => reject(new UploadError("上传失败（网络错误或跨域被拒）"));
    xhr.ontimeout = () => reject(new UploadError("上传超时"));
    xhr.send(form);
  });
}

/**
 * 取一个附件的下载地址（用于消息里的附件 chip 点击下载）。
 */
export async function getDownloadUrl(fileId: string): Promise<string> {
  const resp = await storageHttp.get<Envelope<{ url: string }>>(
    `/api/v1/files/${encodeURIComponent(fileId)}/download`,
  );
  const url = resp.data?.data?.url;
  if (!url) throw new UploadError("获取下载地址失败");
  return url;
}
