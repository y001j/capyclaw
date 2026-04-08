"use client";

import { useEffect, useState, useCallback } from "react";
import { useParams } from "next/navigation";
import Link from "next/link";
import { api, getSessionToken, type Session as ApiSession } from "@/lib/api";

export default function AgentSessionsPage() {
  const params = useParams();
  const agentId = params.id as string;
  const [sessions, setSessions] = useState<ApiSession[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchSessions = useCallback(async () => {
    try {
      setLoading(true);
      const token = await getSessionToken();
      const result = await api.listSessions(agentId, token);
      setSessions(result);
      setError(null);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setLoading(false);
    }
  }, [agentId]);

  useEffect(() => {
    fetchSessions();
  }, [fetchSessions]);

  const handleArchive = async (sessionId: string) => {
    try {
      const token = await getSessionToken();
      await api.delete(`/api/v1/sessions/${sessionId}`, token);
      await fetchSessions();
    } catch (err) {
      setError((err as Error).message);
    }
  };

  return (
    <div className="p-8">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Sessions</h1>
        <Link
          href={`/agents/${agentId}`}
          className="text-sm text-primary hover:underline"
        >
          Back to agent
        </Link>
      </div>

      {error && (
        <div className="mt-4 rounded-md bg-destructive/10 p-3 text-sm text-destructive">
          {error}
        </div>
      )}

      {loading && (
        <p className="mt-6 text-muted-foreground">Loading sessions...</p>
      )}

      <div className="mt-6 space-y-2">
        {sessions.map((session) => (
          <div
            key={session.id}
            className="flex items-center justify-between rounded-lg border border-border p-4 hover:bg-accent/50 transition-colors"
          >
            <Link
              href={`/agents/${agentId}/sessions/${session.id}`}
              className="flex-1 min-w-0"
            >
              <div className="font-medium font-mono text-sm">
                {session.session_key}
              </div>
              <div className="mt-1 text-xs text-muted-foreground">
                {new Date(session.created_at).toLocaleString()}
                {session.total_input_tokens > 0 && (
                  <> &middot; {(session.total_input_tokens + session.total_output_tokens).toLocaleString()} tokens</>
                )}
                {session.total_cost_usd > 0 && (
                  <> &middot; ${session.total_cost_usd.toFixed(4)}</>
                )}
                {session.compaction_count > 0 && (
                  <> &middot; {session.compaction_count} compactions</>
                )}
              </div>
            </Link>
            <div className="flex items-center gap-2 ml-4">
              <span
                className={`rounded-full px-2 py-0.5 text-xs ${
                  session.status === "active"
                    ? "bg-green-100 text-green-700"
                    : session.status === "archived"
                      ? "bg-gray-100 text-gray-500"
                      : "bg-yellow-100 text-yellow-700"
                }`}
              >
                {session.status}
              </span>
              {session.status !== "archived" && (
                <button
                  onClick={(e) => {
                    e.preventDefault();
                    handleArchive(session.id);
                  }}
                  className="rounded-md border border-border px-2 py-0.5 text-xs text-muted-foreground hover:bg-accent"
                >
                  Archive
                </button>
              )}
            </div>
          </div>
        ))}
        {!loading && sessions.length === 0 && (
          <p className="py-8 text-center text-muted-foreground">
            No sessions yet
          </p>
        )}
      </div>
    </div>
  );
}
