import { useCallback } from "react";
import { gatewayWS } from "@/lib/ws";
import { useSessionStore } from "@/stores/session";

/**
 * Returns a sendMessage function that streams chat responses for the
 * given session via the Riverbank WebSocket gateway.
 */
export function useStreamingChat(sessionId: string) {
  const appendMessage = useSessionStore((s) => s.appendMessage);
  const setStatus = useSessionStore((s) => s.setStatus);

  const sendMessage = useCallback(
    (content: string) => {
      appendMessage(sessionId, {
        id: crypto.randomUUID(),
        role: "user",
        content,
        createdAt: new Date().toISOString(),
      });
      setStatus(sessionId, "thinking");
      gatewayWS.send("chat.send", { sessionId, content });
    },
    [sessionId, appendMessage, setStatus],
  );

  return { sendMessage };
}
