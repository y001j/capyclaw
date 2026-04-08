"use client";

import { useParams } from "next/navigation";
import Link from "next/link";
import { MemoryBrowser } from "@/components/MemoryBrowser";

export default function AgentMemoriesPage() {
  const params = useParams();
  const agentId = params.id as string;

  return (
    <div className="p-8">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Memory Browser</h1>
        <Link
          href={`/agents/${agentId}`}
          className="text-sm text-primary hover:underline"
        >
          Back to agent
        </Link>
      </div>
      <p className="mt-1 text-muted-foreground">
        Search episodic, semantic, and procedural memories
      </p>

      <div className="mt-6">
        <MemoryBrowser agentId={agentId} />
      </div>
    </div>
  );
}
