import http from "./http";
import type { CreateProviderRequest, Provider, UpdateProviderRequest } from "./types";

const BASE = "/api/v1/addons/provider";

/**
 * 后端 provider 路由返回 { code, message, data } 信封（见 provider/router/router.go
 * 的 ProviderListOutputBody 等），与 agent.ts 同款，需解包取 data。
 */
interface Envelope<T> {
  code?: number;
  message?: string;
  data?: T;
}

/** GET /provider —— 列出全部 Provider（apiKey 永不回传）。 */
export async function listProviders(): Promise<Provider[]> {
  const response = await http.get<Envelope<Provider[]>>(BASE);
  return response.data?.data ?? [];
}

/** POST /provider —— 创建 Provider，apiKey 明文提交，后端加密入库。 */
export async function createProvider(body: CreateProviderRequest): Promise<Provider | null> {
  const response = await http.post<Envelope<Provider>>(BASE, body);
  return response.data?.data ?? null;
}

/**
 * PATCH /provider/:id —— 部分更新。
 *
 * apiKey 省略或为空字符串时后端**不修改**已存密钥（router.go:184 显式判空）。
 * 调用方据此实现「留空 = 保持原密钥」的编辑语义。
 */
export async function updateProvider(
  id: number,
  body: UpdateProviderRequest,
): Promise<Provider | null> {
  const response = await http.patch<Envelope<Provider>>(`${BASE}/${id}`, body);
  return response.data?.data ?? null;
}

/** DELETE /provider/:id */
export async function deleteProvider(id: number): Promise<void> {
  await http.delete(`${BASE}/${id}`);
}
