"use client";

import Link from "next/link";

interface AgentCardProps {
  id: string;
  name: string;
  slug: string;
  model: string;
  status: string;
  onDelete?: (id: string) => void;
}

export function AgentCard({ id, name, slug, model, status, onDelete }: AgentCardProps) {
  return (
    <div className="rounded-lg border border-border bg-card p-5 transition-shadow hover:shadow-md">
      <div className="flex items-start justify-between">
        <div>
          <Link
            href={`/agents/${id}`}
            className="text-lg font-semibold hover:text-primary"
          >
            {name}
          </Link>
          <p className="mt-1 text-sm text-muted-foreground">{slug}</p>
        </div>
        <span
          className={`rounded-full px-2 py-1 text-xs font-medium ${
            status === "active"
              ? "bg-green-100 text-green-700"
              : "bg-gray-100 text-gray-600"
          }`}
        >
          {status}
        </span>
      </div>

      <div className="mt-3 text-sm text-muted-foreground">
        Model: <span className="font-mono text-foreground">{model}</span>
      </div>

      <div className="mt-4 flex gap-2">
        <Link
          href={`/agents/${id}/chat`}
          className="rounded-md bg-primary px-3 py-1.5 text-sm text-primary-foreground hover:bg-primary/90"
        >
          Chat
        </Link>
        <Link
          href={`/agents/${id}/sessions`}
          className="rounded-md border border-border px-3 py-1.5 text-sm hover:bg-accent"
        >
          Sessions
        </Link>
        <Link
          href={`/agents/${id}/memories`}
          className="rounded-md border border-border px-3 py-1.5 text-sm hover:bg-accent"
        >
          Memories
        </Link>
        {onDelete && (
          <button
            onClick={() => onDelete(id)}
            className="ml-auto rounded-md border border-destructive/30 px-3 py-1.5 text-sm text-destructive hover:bg-destructive/10"
          >
            Delete
          </button>
        )}
      </div>
    </div>
  );
}
