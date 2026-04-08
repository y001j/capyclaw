import { create } from "zustand";
import { api, getSessionToken, type Message as ApiMessage } from "@/lib/api";

export interface Message {
  id: string;
  role: "user" | "assistant" | "tool";
  content: string;
  createdAt: string;
}

export interface Session {
  id: string;
  agentId: string;
  title: string;
  messages: Message[];
  status: "idle" | "thinking" | "tool_call" | "error";
  createdAt: string;
}

interface SessionStore {
  sessions: Record<string, Session>;
  activeSessionId: string | null;
  loading: boolean;
  setActiveSession: (id: string) => void;
  upsertSession: (session: Partial<Session> & { id: string }) => void;
  appendMessage: (sessionId: string, msg: Message) => void;
  updateLastMessage: (sessionId: string, content: string) => void;
  setStatus: (sessionId: string, status: Session["status"]) => void;
  fetchSessions: (agentId: string, token?: string) => Promise<void>;
  fetchMessages: (sessionId: string, token?: string) => Promise<void>;
}

export const useSessionStore = create<SessionStore>((set) => ({
  sessions: {},
  activeSessionId: null,
  loading: false,

  setActiveSession: (id) => set({ activeSessionId: id }),

  upsertSession: (partial) =>
    set((state) => ({
      sessions: {
        ...state.sessions,
        [partial.id]: {
          ...state.sessions[partial.id],
          ...partial,
        } as Session,
      },
    })),

  appendMessage: (sessionId, msg) =>
    set((state) => {
      const session = state.sessions[sessionId];
      if (!session) return state;
      return {
        sessions: {
          ...state.sessions,
          [sessionId]: {
            ...session,
            messages: [...session.messages, msg],
          },
        },
      };
    }),

  updateLastMessage: (sessionId, content) =>
    set((state) => {
      const session = state.sessions[sessionId];
      if (!session || session.messages.length === 0) return state;
      const messages = [...session.messages];
      const last = messages[messages.length - 1];
      messages[messages.length - 1] = { ...last, content: last.content + content };
      return {
        sessions: {
          ...state.sessions,
          [sessionId]: { ...session, messages },
        },
      };
    }),

  setStatus: (sessionId, status) =>
    set((state) => {
      const session = state.sessions[sessionId];
      if (!session) return state;
      return {
        sessions: {
          ...state.sessions,
          [sessionId]: { ...session, status },
        },
      };
    }),

  fetchSessions: async (agentId, token) => {
    set({ loading: true });
    try {
      const t = token ?? (await getSessionToken());
      const result = await api.listSessions(agentId, t);
      const sessionMap: Record<string, Session> = {};
      for (const s of result) {
        sessionMap[s.id] = {
          id: s.id,
          agentId: s.agent_id,
          title: s.session_key,
          messages: [],
          status: "idle",
          createdAt: s.created_at,
        };
      }
      set((state) => ({
        sessions: { ...state.sessions, ...sessionMap },
        loading: false,
      }));
    } catch {
      set({ loading: false });
    }
  },

  fetchMessages: async (sessionId, token) => {
    try {
      const t = token ?? (await getSessionToken());
      const result = await api.listMessages(sessionId, t);
      const messages: Message[] = result.map((m: ApiMessage) => ({
        id: m.id,
        role: m.role,
        content: typeof m.content === "string" ? m.content : JSON.stringify(m.content),
        createdAt: m.created_at,
      }));
      set((state) => {
        const session = state.sessions[sessionId];
        if (!session) return state;
        return {
          sessions: {
            ...state.sessions,
            [sessionId]: { ...session, messages },
          },
        };
      });
    } catch {
      // silently fail
    }
  },
}));
