"use client";

import { useEffect, useState, useCallback } from "react";
import { useParams } from "next/navigation";
import Link from "next/link";
import {
  api,
  getSessionToken,
  type Session as ApiSession,
  type Message as ApiMessage,
} from "@/lib/api";

interface DisplayMessage {
  id: string;
  role: "user" | "assistant" | "tool";
  content: string;
  model: string | null;
  createdAt: string;
}

export default function SessionDetailPage() {
  const params = useParams();
  const agentId = params.id as string;
  const sessionId = params.sessionId as string;

  const [session, setSession] = useState<ApiSession | null>(null);
  const [messages, setMessages] = useState<DisplayMessage[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchData = useCallback(async () => {
    try {
      setLoading(true);
      const token = await getSessionToken();
      const [sess, msgs] = await Promise.all([
        api.getSession(sessionId, token),
        api.listMessages(sessionId, token),
      ]);
      setSession(sess);
      setMessages(
        msgs.map((m: ApiMessage) => ({
          id: m.id,
          role: m.role,
          content:
            typeof m.content === "string" ? m.content : JSON.stringify(m.content, null, 2),
          model: m.model,
          createdAt: m.created_at,
        })),
      );
      setError(null);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setLoading(false);
    }
  }, [sessionId]);

  useEffect(() => {
    fetchData();
  }, [fetchData]);

  if (loading) {
    return <div className="p-8 text-muted-foreground">Loading session...</div>;
  }

  if (error) {
    return (
      <div className="p-8">
        <div className="rounded-md bg-destructive/10 p-3 text-sm text-destructive">{error}</div>
        <Link
          href={`/agents/${agentId}/sessions`}
          className="mt-4 inline-block text-sm text-primary hover:underline"
        >
          Back to sessions
        </Link>
      </div>
    );
  }

  return (
    <div className="p-8">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold">Session Detail</h1>
          {session && (
            <p className="mt-1 font-mono text-sm text-muted-foreground">
              {session.session_key}
            </p>
          )}
        </div>
        <Link
          href={`/agents/${agentId}/sessions`}
          className="text-sm text-primary hover:underline"
        >
          Back to sessions
        </Link>
      </div>

      {session && (
        <div className="mt-4 flex flex-wrap gap-3 text-xs text-muted-foreground">
          <span>Status: {session.status}</span>
          <span>&middot;</span>
          <span>Created: {new Date(session.created_at).toLocaleString()}</span>
          {session.total_input_tokens > 0 && (
            <>
              <span>&middot;</span>
              <span>
                Tokens: {(session.total_input_tokens + session.total_output_tokens).toLocaleString()}
              </span>
            </>
          )}
          {session.total_cost_usd > 0 && (
            <>
              <span>&middot;</span>
              <span>Cost: ${session.total_cost_usd.toFixed(4)}</span>
            </>
          )}
        </div>
      )}

      <div className="mt-6 space-y-3">
        {messages.length === 0 && (
          <p className="py-8 text-center text-muted-foreground">
            No messages in this session
          </p>
        )}
        {messages.map((msg) => (
          <div
            key={msg.id}
            className={`flex ${msg.role === "user" ? "justify-end" : "justify-start"}`}
          >
            <div
              className={`max-w-[80%] rounded-lg px-4 py-2.5 text-sm ${
                msg.role === "user"
                  ? "bg-primary text-primary-foreground"
                  : msg.role === "tool"
                    ? "bg-muted font-mono text-xs"
                    : "bg-card border border-border"
              }`}
            >
              <div className="whitespace-pre-wrap break-words">{msg.content}</div>
              <div
                className={`mt-1 text-xs ${
                  msg.role === "user" ? "text-primary-foreground/60" : "text-muted-foreground"
                }`}
              >
                {new Date(msg.createdAt).toLocaleTimeString()}
                {msg.model && <> &middot; {msg.model}</>}
              </div>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
