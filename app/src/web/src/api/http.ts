import axios from "axios";
import type { AxiosResponse, InternalAxiosRequestConfig } from "axios";
import { getAccessToken, clearAccessToken } from "@/utils/token";
import type { ApiErrorBody } from "./types";

/**
 * Shared axios instance for the core backend (:6680).
 *
 * - baseURL comes from VITE_CORE_BACKEND_URL when set (e.g. cross-origin in a
 *   micro-app shell), otherwise it is empty and requests use relative
 *   `/api/...` URLs that the Vite dev server proxies to :6680.
 * - Unlike auth, core endpoints return resources directly (no numeric
 *   envelope), so the response interceptor resolves with `response.data`
 *   unchanged. Only HTTP-level errors are normalized into `ApiError`.
 * - HTTP 401 clears the token and bounces the user to the shell's /auth route
 *   so they can re-authenticate via the auth sub-app.
 */
const baseURL = import.meta.env.VITE_CORE_BACKEND_URL?.trim() || "";

const http = axios.create({
  baseURL,
  timeout: 15000,
  headers: {
    "Content-Type": "application/json",
  },
});

/** Error thrown on HTTP-level or business failure. */
export class ApiError extends Error {
  code: string;
  status: number;

  constructor(message: string, code: string, status: number) {
    super(message);
    this.name = "ApiError";
    this.code = code;
    this.status = status;
  }
}

http.interceptors.request.use((config: InternalAxiosRequestConfig) => {
  const token = getAccessToken();
  if (token) {
    config.headers.Authorization = `Bearer ${token}`;
  }
  return config;
});

function redirectToAuth(): void {
  clearAccessToken();
  if (typeof window === "undefined") return;
  // 在 micro-app 子应用模式下，不做 window.location 硬跳转（会劫持整个壳的 URL）。
  // 只清 token，让壳应用自行决定何时切到 auth 子应用。
  const w = globalThis as Record<string, unknown>;
  if (w.__MICRO_APP_ENVIRONMENT__) return;
  // 独立运行时跳 /auth。
  const path = window.location.pathname;
  if (!path.startsWith("/auth")) {
    window.location.href = "/auth";
  }
}

http.interceptors.response.use(
  (response) => response,
  (error: unknown) => {
    if (axios.isAxiosError(error)) {
      const status = error.response?.status ?? 0;
      if (status === 401) {
        redirectToAuth();
      }
      const body = error.response?.data as ApiErrorBody | undefined;
      const message = body?.message || error.message || "Network error";
      return Promise.reject(new ApiError(message, body?.code ?? "NETWORK_ERROR", status));
    }
    return Promise.reject(error);
  },
);

/**
 * Resolve a full URL + auth headers for a raw `fetch` call (used by the SSE
 * stream, which axios cannot consume incrementally). Mirrors the interceptor
 * logic so the SSE request carries the same Authorization header and base URL.
 */
export function apiBase(): string {
  return baseURL;
}

export function authHeaders(): Record<string, string> {
  const token = getAccessToken();
  const headers: Record<string, string> = {
    Accept: "text/event-stream",
  };
  if (token) {
    headers.Authorization = `Bearer ${token}`;
  }
  return headers;
}

export type { AxiosResponse };
export default http;
