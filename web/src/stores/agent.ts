import { create } from "zustand";
import { api, getSessionToken, type Agent as ApiAgent, type CreateAgentReq } from "@/lib/api";

export interface Agent {
  id: string;
  tenantId: string;
  name: string;
  slug: string;
  description: string;
  llmProvider: string;
  llmModel: string;
  systemPrompt: string;
  skills: string[];
  status: "active" | "inactive" | "error";
  createdAt: string;
}

function fromApi(a: ApiAgent): Agent {
  return {
    id: a.id,
    tenantId: a.tenant_id,
    name: a.name,
    slug: a.slug,
    description: "",
    llmProvider: "",
    llmModel: a.model,
    systemPrompt: a.system_prompt ?? "",
    skills: [],
    status: a.status,
    createdAt: a.created_at,
  };
}

interface AgentStore {
  agents: Record<string, Agent>;
  loading: boolean;
  error: string | null;
  setAgents: (agents: Agent[]) => void;
  upsertAgent: (agent: Agent) => void;
  removeAgent: (id: string) => void;
  setLoading: (v: boolean) => void;
  fetchAgents: (token?: string) => Promise<void>;
  createAgent: (data: CreateAgentReq, token?: string) => Promise<Agent>;
  deleteAgent: (id: string, token?: string) => Promise<void>;
}

export const useAgentStore = create<AgentStore>((set) => ({
  agents: {},
  loading: false,
  error: null,

  setAgents: (agents) =>
    set({
      agents: Object.fromEntries(agents.map((a) => [a.id, a])),
    }),

  upsertAgent: (agent) =>
    set((state) => ({
      agents: { ...state.agents, [agent.id]: agent },
    })),

  removeAgent: (id) =>
    set((state) => {
      const agents = { ...state.agents };
      delete agents[id];
      return { agents };
    }),

  setLoading: (loading) => set({ loading }),

  fetchAgents: async (token) => {
    set({ loading: true, error: null });
    try {
      const t = token ?? (await getSessionToken());
      const result = await api.listAgents(t);
      const agents = result.map(fromApi);
      set({
        agents: Object.fromEntries(agents.map((a) => [a.id, a])),
        loading: false,
      });
    } catch (err) {
      set({ loading: false, error: (err as Error).message });
    }
  },

  createAgent: async (data, token) => {
    const t = token ?? (await getSessionToken());
    const result = await api.createAgent(data, t);
    const agent = fromApi(result);
    set((state) => ({
      agents: { ...state.agents, [agent.id]: agent },
    }));
    return agent;
  },

  deleteAgent: async (id, token) => {
    const t = token ?? (await getSessionToken());
    await api.deleteAgent(id, t);
    set((state) => {
      const agents = { ...state.agents };
      delete agents[id];
      return { agents };
    });
  },
}));
