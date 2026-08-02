import http from "./http";
import type { Skill } from "./types";

const BASE = "/api/v1/addons/skill";

/**
 * GET /skill — list available skills.
 */
export async function listSkills(): Promise<Skill[]> {
  const response = await http.get<Skill[]>(BASE);
  return response.data;
}
