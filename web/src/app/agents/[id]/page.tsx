"use client";

import { useEffect, useState } from "react";
import { useParams } from "next/navigation";
import Link from "next/link";
import { api, type Agent } from "@/lib/api";

export default function AgentDetailPage() {
  const params = useParams();
  const agentId = params.id as string;
  const [agent, setAgent] = useState<Agent | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    api.getAgent(agentId).then((a) => {
      setAgent(a);
      setLoading(false);
    }).catch(() => setLoading(false));
  }, [agentId]);

  if (loading) {
    return <div className="p-8 text-muted-foreground">Loading...</div>;
  }

  if (!agent) {
    return <div className="p-8 text-destructive">Agent not found</div>;
  }

  return (
    <div className="p-8">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold">{agent.name}</h1>
          <p className="mt-1 font-mono text-sm text-muted-foreground">
            {agent.slug}
          </p>
        </div>
        <div className="flex gap-2">
          <Link
            href={`/agents/${agentId}/chat`}
            className="rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90"
          >
            Open Chat
          </Link>
        </div>
      </div>

      <div className="mt-8 grid grid-cols-1 gap-6 lg:grid-cols-2">
        <div className="rounded-lg border border-border p-5">
          <h2 className="text-sm font-semibold uppercase text-muted-foreground">
            Configuration
          </h2>
          <dl className="mt-4 space-y-3">
            <div>
              <dt className="text-sm text-muted-foreground">Model</dt>
              <dd className="font-mono text-sm">{agent.model}</dd>
            </div>
            <div>
              <dt className="text-sm text-muted-foreground">Status</dt>
              <dd>
                <span
                  className={`rounded-full px-2 py-0.5 text-xs ${
                    agent.status === "active"
                      ? "bg-green-100 text-green-700"
                      : "bg-gray-100 text-gray-600"
                  }`}
                >
                  {agent.status}
                </span>
              </dd>
            </div>
            <div>
              <dt className="text-sm text-muted-foreground">Fallback Models</dt>
              <dd className="font-mono text-sm">
                {agent.fallback_models?.length
                  ? agent.fallback_models.join(", ")
                  : "None"}
              </dd>
            </div>
            <div>
              <dt className="text-sm text-muted-foreground">Created</dt>
              <dd className="text-sm">
                {new Date(agent.created_at).toLocaleString()}
              </dd>
            </div>
          </dl>
        </div>

        <div className="rounded-lg border border-border p-5">
          <h2 className="text-sm font-semibold uppercase text-muted-foreground">
            System Prompt
          </h2>
          <pre className="mt-4 max-h-48 overflow-auto whitespace-pre-wrap rounded bg-muted p-3 text-sm">
            {agent.system_prompt || "(no system prompt)"}
          </pre>
        </div>
      </div>

      <div className="mt-6 flex gap-3">
        <Link
          href={`/agents/${agentId}/sessions`}
          className="rounded-md border border-border px-4 py-2 text-sm hover:bg-accent"
        >
          View Sessions
        </Link>
        <Link
          href={`/agents/${agentId}/memories`}
          className="rounded-md border border-border px-4 py-2 text-sm hover:bg-accent"
        >
          Browse Memories
        </Link>
        <Link
          href={`/agents/${agentId}/crons`}
          className="rounded-md border border-border px-4 py-2 text-sm hover:bg-accent"
        >
          Cron Jobs
        </Link>
      </div>
    </div>
  );
}
