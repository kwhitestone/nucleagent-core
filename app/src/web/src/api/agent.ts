import http from "./http";
import type { AgentTemplate } from "./types";

const BASE = "/api/v1/addons/agent/templates";

/**
 * GET /agent/templates — list available agent templates.
 */
export async function listAgentTemplates(): Promise<AgentTemplate[]> {
  const response = await http.get<AgentTemplate[]>(BASE);
  return response.data;
}
