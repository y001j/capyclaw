"use client";

import { useEffect } from "react";
import Link from "next/link";
import { useAgentStore } from "@/stores/agent";

export default function DashboardPage() {
  const { agents, loading, fetchAgents } = useAgentStore();
  const agentList = Object.values(agents);

  useEffect(() => {
    fetchAgents();
  }, [fetchAgents]);

  const activeAgents = agentList.filter((a) => a.status === "active");

  return (
    <div className="p-8">
      <h1 className="text-2xl font-bold">Dashboard</h1>
      <p className="mt-1 text-muted-foreground">
        CapyClaw platform overview
      </p>

      <div className="mt-8 grid grid-cols-1 gap-4 sm:grid-cols-3">
        <div className="rounded-lg border border-border bg-card p-6">
          <div className="text-sm text-muted-foreground">Total Agents</div>
          <div className="mt-2 text-3xl font-bold">
            {loading ? "…" : agentList.length}
          </div>
        </div>
        <div className="rounded-lg border border-border bg-card p-6">
          <div className="text-sm text-muted-foreground">Active Agents</div>
          <div className="mt-2 text-3xl font-bold text-green-600">
            {loading ? "…" : activeAgents.length}
          </div>
        </div>
        <div className="rounded-lg border border-border bg-card p-6">
          <div className="text-sm text-muted-foreground">Platform</div>
          <div className="mt-2 text-3xl font-bold">v0.1.0</div>
        </div>
      </div>

      <div className="mt-8">
        <div className="flex items-center justify-between">
          <h2 className="text-lg font-semibold">Recent Agents</h2>
          <Link
            href="/agents"
            className="text-sm text-primary hover:underline"
          >
            View all
          </Link>
        </div>
        <div className="mt-4 space-y-2">
          {agentList.slice(0, 5).map((agent) => (
            <Link
              key={agent.id}
              href={`/agents/${agent.id}`}
              className="flex items-center justify-between rounded-lg border border-border p-4 transition-colors hover:bg-accent"
            >
              <div>
                <div className="font-medium">{agent.name}</div>
                <div className="text-sm text-muted-foreground">
                  {agent.llmModel}
                </div>
              </div>
              <span
                className={cn(
                  "rounded-full px-2 py-1 text-xs",
                  agent.status === "active"
                    ? "bg-green-100 text-green-700"
                    : "bg-gray-100 text-gray-600",
                )}
              >
                {agent.status}
              </span>
            </Link>
          ))}
          {!loading && agentList.length === 0 && (
            <p className="py-8 text-center text-muted-foreground">
              No agents yet.{" "}
              <Link href="/agents/new" className="text-primary hover:underline">
                Create one
              </Link>
            </p>
          )}
        </div>
      </div>
    </div>
  );
}

function cn(...classes: (string | boolean | undefined)[]) {
  return classes.filter(Boolean).join(" ");
}
