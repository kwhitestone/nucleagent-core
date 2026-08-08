import http from "./http";
import type { AgentTemplate } from "./types";

const BASE = "/api/v1/addons/agent/templates";

/** 后端返回 { code, message, data } 信封；data 才是业务载荷。 */
interface Envelope<T> {
  code?: number;
  message?: string;
  data?: T;
}

/**
 * GET /agent/templates - list available agent templates.
 * 后端返回 { code, message, data: AgentTemplate[] }，解包取 data。
 */
export async function listAgentTemplates(): Promise<AgentTemplate[]> {
  const response = await http.get<Envelope<AgentTemplate[]>>(BASE);
  return response.data?.data ?? [];
}
