"use client";

import { useEffect } from "react";
import Link from "next/link";
import { useAgentStore } from "@/stores/agent";
import { AgentCard } from "@/components/AgentCard";

export default function AgentsPage() {
  const { agents, loading, fetchAgents, deleteAgent } = useAgentStore();
  const agentList = Object.values(agents);

  useEffect(() => {
    fetchAgents();
  }, [fetchAgents]);

  const handleDelete = async (id: string) => {
    if (confirm("Are you sure you want to delete this agent?")) {
      await deleteAgent(id);
    }
  };

  return (
    <div className="p-8">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold">Agents</h1>
          <p className="mt-1 text-muted-foreground">
            Manage your AI agents
          </p>
        </div>
        <Link
          href="/agents/new"
          className="rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90"
        >
          + New Agent
        </Link>
      </div>

      {loading && (
        <div className="mt-8 text-center text-muted-foreground">Loading...</div>
      )}

      <div className="mt-6 grid grid-cols-1 gap-4 lg:grid-cols-2">
        {agentList.map((agent) => (
          <AgentCard
            key={agent.id}
            id={agent.id}
            name={agent.name}
            slug={agent.slug}
            model={agent.llmModel}
            status={agent.status}
            onDelete={handleDelete}
          />
        ))}
      </div>

      {!loading && agentList.length === 0 && (
        <div className="mt-16 text-center">
          <p className="text-muted-foreground">No agents created yet.</p>
          <Link
            href="/agents/new"
            className="mt-4 inline-block rounded-md bg-primary px-4 py-2 text-sm text-primary-foreground"
          >
            Create your first agent
          </Link>
        </div>
      )}
    </div>
  );
}
