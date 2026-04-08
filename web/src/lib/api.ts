/**
 * REST API client for the Riverbank gateway.
 * All requests include the Authorization header from next-auth session.
 */

const BASE_URL =
  process.env.NEXT_PUBLIC_GATEWAY_HTTP_URL ?? "http://localhost:18789";

/**
 * getSessionToken retrieves a valid JWT for the gateway.
 * In dev mode, fetches a dev token from the gateway's /api/v1/dev/token endpoint.
 * In production, retrieves the access token from the next-auth session.
 */
let cachedDevToken: string | undefined;

export async function getSessionToken(): Promise<string | undefined> {
  if (typeof window === "undefined") return undefined;

  // Return cached dev token if still valid
  if (cachedDevToken) return cachedDevToken;

  try {
    // Try to get a dev token from the gateway
    const devRes = await fetch(`${BASE_URL}/api/v1/dev/token`, {
      method: "POST",
    });
    if (devRes.ok) {
      const data = await devRes.json();
      if (data.token) {
        cachedDevToken = data.token;
        return cachedDevToken;
      }
    }
  } catch {
    // Dev token endpoint not available, fall through
  }

  // Fallback: get token from next-auth session (production OIDC)
  try {
    const res = await fetch("/api/auth/session");
    const session = await res.json();
    return session?.accessToken ?? undefined;
  } catch {
    return undefined;
  }
}

// --- Types ---

export interface Agent {
  id: string;
  tenant_id: string;
  name: string;
  slug: string;
  model: string;
  fallback_models: string[];
  system_prompt: string | null;
  settings: Record<string, unknown>;
  tool_policy: Record<string, unknown>;
  status: "active" | "inactive" | "error";
  created_at: string;
  updated_at: string;
}

export interface Session {
  id: string;
  agent_id: string;
  session_key: string;
  status: string;
  total_input_tokens: number;
  total_output_tokens: number;
  total_cost_usd: number;
  compaction_count: number;
  created_at: string;
  updated_at: string;
}

export interface Message {
  id: string;
  session_id: string;
  role: "user" | "assistant" | "tool";
  content: unknown;
  model: string | null;
  created_at: string;
}

export interface Skill {
  id: string;
  name: string;
  version: string;
  description: string | null;
  source: string;
  verified: boolean;
  sandbox_tier: string;
  installed_at: string;
}

export interface SkillDetail extends Skill {
  manifest: Record<string, unknown>;
  content_md: string;
  updated_at: string;
}

export interface ClawHubSearchResult {
  score: number;
  slug: string;
  displayName: string;
  summary: string;
  version: string;
  updatedAt: string;
}

export interface ClawHubSkillDetail {
  slug: string;
  displayName: string;
  summary: string;
  latestVersion: { version: string };
  [key: string]: unknown;
}

export interface ModelInfo {
  id: string;
  provider: string;
  default: boolean;
}

export interface Memory {
  id: string;
  agent_id: string;
  memory_type: string;
  content: string;
  importance_score: number;
  access_count: number;
  created_at: string;
}

export interface Tenant {
  id: string;
  name: string;
  slug: string;
  plan: string;
  status: string;
  quota: Record<string, unknown>;
  suspended_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface AuditEvent {
  id: number;
  tenant_id: string;
  actor_id: string | null;
  actor_type: string;
  action: string;
  resource_type: string;
  resource_id: string | null;
  details: Record<string, unknown>;
  ip_address: string;
  created_at: string;
}

export interface UsageReport {
  tenant_id: string;
  start_date: string;
  end_date: string;
  models: ModelUsage[];
  totals: {
    session_count: number;
    input_tokens: number;
    output_tokens: number;
    cost_usd: number;
  };
}

export interface ModelUsage {
  model: string;
  session_count: number;
  input_tokens: number;
  output_tokens: number;
  cost_usd: number;
}

export interface CronJobWithAgent {
  id: string;
  tenant_id: string;
  agent_id: string;
  agent_name: string;
  name: string;
  schedule: string;
  prompt: string;
  enabled: boolean;
  last_run_at: string | null;
  next_run_at: string | null;
  run_count: number;
  last_status: string | null;
  created_at: string;
}

export interface CronJob {
  id: string;
  tenant_id: string;
  agent_id: string;
  name: string;
  schedule: string;
  prompt: string;
  enabled: boolean;
  last_run_at: string | null;
  next_run_at: string | null;
  run_count: number;
  last_status: string | null;
  created_at: string;
}

export interface CreateCronJobReq {
  name: string;
  schedule: string;
  prompt: string;
}

export interface CreateAgentReq {
  name: string;
  slug: string;
  model?: string;
  system_prompt?: string;
}

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

// --- Core request function ---

async function request<T>(
  path: string,
  init?: RequestInit & { token?: string },
  _retry = false,
): Promise<T> {
  const { token, ...fetchInit } = init ?? {};
  const res = await fetch(`${BASE_URL}${path}`, {
    ...fetchInit,
    headers: {
      "Content-Type": "application/json",
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      ...fetchInit.headers,
    },
  });

  // On 401, clear cached token and retry once
  if (res.status === 401 && !_retry) {
    cachedDevToken = undefined;
    const newToken = await getSessionToken();
    if (newToken && newToken !== token) {
      return request<T>(path, { ...init, token: newToken }, true);
    }
  }

  if (!res.ok) {
    const err = await res.text();
    throw new ApiError(res.status, `API ${res.status}: ${err}`);
  }

  return res.json() as Promise<T>;
}

// --- SSE streaming helper ---

export async function* streamChat(
  agentId: string,
  message: string,
  options?: { token?: string; sessionKey?: string; model?: string },
): AsyncGenerator<{ content?: string; done?: boolean; error?: string }> {
  const token = options?.token ?? (await getSessionToken());
  const res = await fetch(`${BASE_URL}/v1/chat/completions`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      ...(token
        ? { Authorization: `Bearer ${token}` }
        : {}),
    },
    body: JSON.stringify({
      model: options?.model ?? "claude-sonnet-4-20250514",
      stream: true,
      agent_id: agentId,
      session_key: options?.sessionKey,
      messages: [{ role: "user", content: message }],
    }),
  });

  if (!res.ok) {
    const err = await res.text();
    yield { error: `API ${res.status}: ${err}` };
    return;
  }

  const reader = res.body!.getReader();
  const decoder = new TextDecoder();
  let buffer = "";

  while (true) {
    const { done, value } = await reader.read();
    if (done) break;

    buffer += decoder.decode(value, { stream: true });
    const lines = buffer.split("\n");
    buffer = lines.pop() ?? "";

    for (const line of lines) {
      if (!line.startsWith("data: ")) continue;
      const data = line.slice(6);
      if (data === "[DONE]") {
        yield { done: true };
        return;
      }
      try {
        const chunk = JSON.parse(data);
        const content = chunk.choices?.[0]?.delta?.content;
        if (content) yield { content };
      } catch {
        // skip malformed chunks
      }
    }
  }
}

// --- API methods ---

export const api = {
  // Generic methods
  get: <T>(path: string, token?: string) =>
    request<T>(path, { method: "GET", token }),

  post: <T>(path: string, body: unknown, token?: string) =>
    request<T>(path, {
      method: "POST",
      body: JSON.stringify(body),
      token,
    }),

  patch: <T>(path: string, body: unknown, token?: string) =>
    request<T>(path, {
      method: "PATCH",
      body: JSON.stringify(body),
      token,
    }),

  delete: <T>(path: string, token?: string) =>
    request<T>(path, { method: "DELETE", token }),

  // Config
  listModels: (token?: string) =>
    request<ModelInfo[]>("/api/v1/config/models", { token }),

  // Agent CRUD
  listAgents: (token?: string) =>
    request<Agent[]>("/api/v1/agents", { token }),

  getAgent: (id: string, token?: string) =>
    request<Agent>(`/api/v1/agents/${id}`, { token }),

  createAgent: (data: CreateAgentReq, token?: string) =>
    request<Agent>("/api/v1/agents", {
      method: "POST",
      body: JSON.stringify(data),
      token,
    }),

  updateAgent: (id: string, data: Partial<Agent>, token?: string) =>
    request<Agent>(`/api/v1/agents/${id}`, {
      method: "PATCH",
      body: JSON.stringify(data),
      token,
    }),

  deleteAgent: (id: string, token?: string) =>
    request<void>(`/api/v1/agents/${id}`, { method: "DELETE", token }),

  // Sessions
  listSessions: (agentId: string, token?: string) =>
    request<Session[]>(`/api/v1/agents/${agentId}/sessions`, { token }),

  getSession: (id: string, token?: string) =>
    request<Session>(`/api/v1/sessions/${id}`, { token }),

  listMessages: (sessionId: string, token?: string) =>
    request<Message[]>(`/api/v1/sessions/${sessionId}/messages`, { token }),

  // Skills
  listSkills: (token?: string) =>
    request<Skill[]>("/api/v1/skills", { token }),

  installSkill: (data: { name: string; source: string }, token?: string) =>
    request<Skill>("/api/v1/skills/install", {
      method: "POST",
      body: JSON.stringify(data),
      token,
    }),

  uninstallSkill: (id: string, token?: string) =>
    request<void>(`/api/v1/skills/${id}`, { method: "DELETE", token }),

  getSkill: (id: string, token?: string) =>
    request<SkillDetail>(`/api/v1/skills/${id}`, { token }),

  updateSkill: (id: string, data: { name?: string; description?: string; version?: string; content_md?: string; manifest?: Record<string, unknown> }, token?: string) =>
    request<{ status: string }>(`/api/v1/skills/${id}`, {
      method: "PATCH",
      body: JSON.stringify(data),
      token,
    }),

  parseSkillMD: (content: string, token?: string) =>
    request<{ frontmatter: Record<string, unknown>; content_md: string }>("/api/v1/skills/parse-skillmd", {
      method: "POST",
      body: JSON.stringify({ content }),
      token,
    }),

  // ClawHub
  searchClawHub: (query: string, limit?: number, token?: string) =>
    request<ClawHubSearchResult[] | { results: ClawHubSearchResult[] }>(`/api/v1/clawhub/search?q=${encodeURIComponent(query)}&limit=${limit ?? 20}`, { token }),

  getClawHubSkill: (slug: string, token?: string) =>
    request<ClawHubSkillDetail>(`/api/v1/clawhub/skills/${slug}`, { token }),

  getClawHubFile: (slug: string, path: string, token?: string) =>
    request<string>(`/api/v1/clawhub/skills/${slug}/file?path=${encodeURIComponent(path)}`, { token }),

  installFromClawHub: (slug: string, version?: string, token?: string) =>
    request<{ id: string; name: string; version: string; source: string }>("/api/v1/clawhub/install", {
      method: "POST",
      body: JSON.stringify({ slug, version }),
      token,
    }),

  // Memories
  listMemories: (agentId: string, token?: string) =>
    request<Memory[]>(`/api/v1/agents/${agentId}/memories`, { token }),

  searchMemories: (agentId: string, query: string, token?: string) =>
    request<Memory[]>(`/api/v1/agents/${agentId}/memories/search`, {
      method: "POST",
      body: JSON.stringify({ query }),
      token,
    }),

  // Cron Jobs
  listAllCronJobs: (token?: string) =>
    request<CronJobWithAgent[]>("/api/v1/crons", { token }),

  listCronJobs: (agentId: string, token?: string) =>
    request<CronJob[]>(`/api/v1/agents/${agentId}/crons`, { token }),

  createCronJob: (agentId: string, data: CreateCronJobReq, token?: string) =>
    request<CronJob>(`/api/v1/agents/${agentId}/crons`, {
      method: "POST",
      body: JSON.stringify(data),
      token,
    }),

  updateCronJob: (agentId: string, cronId: string, data: { enabled: boolean }, token?: string) =>
    request<{ id: string; enabled: boolean }>(`/api/v1/agents/${agentId}/crons/${cronId}`, {
      method: "PATCH",
      body: JSON.stringify(data),
      token,
    }),

  deleteCronJob: (agentId: string, cronId: string, token?: string) =>
    request<void>(`/api/v1/agents/${agentId}/crons/${cronId}`, { method: "DELETE", token }),

  // Admin
  listTenants: (token?: string) =>
    request<Tenant[]>("/api/v1/admin/tenants", { token }),

  createTenant: (
    data: { name: string; slug: string; plan?: string },
    token?: string,
  ) =>
    request<Tenant>("/api/v1/admin/tenants", {
      method: "POST",
      body: JSON.stringify(data),
      token,
    }),

  updateTenant: (id: string, data: Partial<Tenant>, token?: string) =>
    request<Tenant>(`/api/v1/admin/tenants/${id}`, {
      method: "PATCH",
      body: JSON.stringify(data),
      token,
    }),

  getTenantUsage: (
    id: string,
    params?: { start?: string; end?: string },
    token?: string,
  ) => {
    const qs = new URLSearchParams(params as Record<string, string>).toString();
    const path = `/api/v1/admin/tenants/${id}/usage${qs ? `?${qs}` : ""}`;
    return request<UsageReport>(path, { token });
  },

  queryAudit: (
    params?: {
      action?: string;
      tenant_id?: string;
      limit?: string;
      offset?: string;
    },
    token?: string,
  ) => {
    const qs = new URLSearchParams(
      params as Record<string, string>,
    ).toString();
    return request<AuditEvent[]>(
      `/api/v1/admin/audit${qs ? `?${qs}` : ""}`,
      { token },
    );
  },
};
