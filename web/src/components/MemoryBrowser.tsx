"use client";

import { useState } from "react";
import { api, type Memory } from "@/lib/api";

interface MemoryBrowserProps {
  agentId: string;
}

export function MemoryBrowser({ agentId }: MemoryBrowserProps) {
  const [memories, setMemories] = useState<Memory[]>([]);
  const [query, setQuery] = useState("");
  const [loading, setLoading] = useState(false);
  const [searched, setSearched] = useState(false);

  const handleSearch = async () => {
    if (!query.trim()) {
      // List all memories
      setLoading(true);
      try {
        const result = await api.listMemories(agentId);
        setMemories(result);
      } catch {
        setMemories([]);
      }
      setLoading(false);
      setSearched(true);
      return;
    }

    setLoading(true);
    try {
      const result = await api.searchMemories(agentId, query);
      setMemories(result);
    } catch {
      setMemories([]);
    }
    setLoading(false);
    setSearched(true);
  };

  return (
    <div>
      <div className="flex gap-2">
        <input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && handleSearch()}
          placeholder="Search memories (or leave empty to list all)..."
          className="flex-1 rounded-md border border-input bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
        />
        <button
          onClick={handleSearch}
          disabled={loading}
          className="rounded-md bg-primary px-4 py-2 text-sm text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
        >
          {loading ? "..." : "Search"}
        </button>
      </div>

      <div className="mt-4 space-y-3">
        {memories.map((mem) => (
          <div
            key={mem.id}
            className="rounded-lg border border-border p-4"
          >
            <div className="flex items-center justify-between">
              <span className="rounded bg-muted px-2 py-0.5 text-xs font-medium">
                {mem.memory_type}
              </span>
              <span className="text-xs text-muted-foreground">
                score: {mem.importance_score.toFixed(2)} &middot; accessed:{" "}
                {mem.access_count}x
              </span>
            </div>
            <p className="mt-2 whitespace-pre-wrap text-sm">{mem.content}</p>
            <div className="mt-2 text-xs text-muted-foreground">
              {new Date(mem.created_at).toLocaleString()}
            </div>
          </div>
        ))}
        {searched && memories.length === 0 && (
          <p className="py-4 text-center text-sm text-muted-foreground">
            No memories found
          </p>
        )}
      </div>
    </div>
  );
}
