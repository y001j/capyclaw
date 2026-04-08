"use client";

import { useEffect, useCallback, useState, useRef } from "react";
import { useParams } from "next/navigation";
import { ChatView } from "@/components/ChatView";
import { useSessionStore, type Message } from "@/stores/session";
import { api, getSessionToken, streamChat } from "@/lib/api";

export default function AgentChatPage() {
  const params = useParams();
  const agentId = params.id as string;
  const sessionKey = `chat-${agentId}`;
  const sessionId = sessionKey; // local store key

  const { sessions, upsertSession, appendMessage, updateLastMessage, setStatus, fetchMessages } =
    useSessionStore();
  const session = sessions[sessionId];
  const [initialized, setInitialized] = useState(false);
  const historyLoaded = useRef(false);

  useEffect(() => {
    if (!sessions[sessionId]) {
      upsertSession({
        id: sessionId,
        agentId,
        title: "Chat",
        messages: [],
        status: "idle",
        createdAt: new Date().toISOString(),
      });
    }
    setInitialized(true);
  }, [sessionId, agentId, sessions, upsertSession]);

  // Load chat history from backend on mount
  useEffect(() => {
    if (!initialized || historyLoaded.current) return;
    historyLoaded.current = true;

    (async () => {
      try {
        const token = await getSessionToken();
        const backendSessions = await api.listSessions(agentId, token);
        const match = backendSessions.find((s) => s.session_key === sessionKey);
        if (match) {
          await fetchMessages(match.id, token);
          // Copy messages from backend session id to our local session key
          const store = useSessionStore.getState();
          const backendSession = store.sessions[match.id];
          if (backendSession && backendSession.messages.length > 0) {
            upsertSession({
              id: sessionId,
              messages: backendSession.messages,
            });
          }
        }
      } catch {
        // History load failed silently, user can still chat
      }
    })();
  }, [initialized, agentId, sessionKey, sessionId, fetchMessages, upsertSession]);

  const handleSend = useCallback(
    async (content: string) => {
      // Add user message
      const userMsg: Message = {
        id: crypto.randomUUID(),
        role: "user",
        content,
        createdAt: new Date().toISOString(),
      };
      appendMessage(sessionId, userMsg);
      setStatus(sessionId, "thinking");

      // Add placeholder for assistant
      const assistantId = crypto.randomUUID();
      const assistantMsg: Message = {
        id: assistantId,
        role: "assistant",
        content: "",
        createdAt: new Date().toISOString(),
      };
      appendMessage(sessionId, assistantMsg);

      // Stream response
      try {
        for await (const chunk of streamChat(agentId, content, {
          sessionKey: sessionId,
        })) {
          if (chunk.content) {
            updateLastMessage(sessionId, chunk.content);
          }
          if (chunk.error) {
            setStatus(sessionId, "error");
            updateLastMessage(sessionId, `Error: ${chunk.error}`);
            return;
          }
          if (chunk.done) break;
        }
        setStatus(sessionId, "idle");
      } catch (err) {
        setStatus(sessionId, "error");
        updateLastMessage(sessionId, `Error: ${(err as Error).message}`);
      }
    },
    [agentId, sessionId, appendMessage, updateLastMessage, setStatus],
  );

  if (!initialized || !session) {
    return <div className="p-8 text-muted-foreground">Loading...</div>;
  }

  return (
    <div className="flex h-full flex-col">
      <div className="flex h-14 items-center border-b border-border px-6">
        <h1 className="text-lg font-semibold">Chat</h1>
        <span className="ml-2 text-sm text-muted-foreground">
          Agent: {agentId.slice(0, 8)}...
        </span>
      </div>
      <div className="flex-1">
        <ChatView
          messages={session.messages}
          status={session.status}
          onSend={handleSend}
        />
      </div>
    </div>
  );
}
