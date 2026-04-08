"use client";

import { useEffect, useState } from "react";
import { useAdminStore } from "@/stores/admin";
import { AuditTable } from "@/components/AuditTable";

const actionOptions = [
  "",
  "agent.create",
  "agent.update",
  "agent.delete",
  "session.create",
  "session.archive",
  "auth.login",
  "auth.failed",
  "tenant.create",
  "tenant.suspend",
  "llm.request",
  "skill.install",
  "tool.execute",
];

export default function AuditPage() {
  const { auditEvents, loading, fetchAudit } = useAdminStore();
  const [action, setAction] = useState("");

  useEffect(() => {
    fetchAudit({ action: action || undefined, limit: "100" });
  }, [action, fetchAudit]);

  return (
    <div className="p-8">
      <h1 className="text-2xl font-bold">Audit Log</h1>
      <p className="mt-1 text-muted-foreground">
        View security and operational audit events
      </p>

      <div className="mt-6 flex items-center gap-4">
        <div>
          <label className="text-sm font-medium">Filter by Action</label>
          <select
            value={action}
            onChange={(e) => setAction(e.target.value)}
            className="ml-2 rounded-md border border-input bg-background px-3 py-1.5 text-sm"
          >
            <option value="">All actions</option>
            {actionOptions
              .filter((a) => a)
              .map((a) => (
                <option key={a} value={a}>
                  {a}
                </option>
              ))}
          </select>
        </div>
        <button
          onClick={() => fetchAudit({ action: action || undefined, limit: "100" })}
          className="rounded-md border border-border px-3 py-1.5 text-sm hover:bg-accent"
        >
          Refresh
        </button>
      </div>

      <div className="mt-6">
        <AuditTable events={auditEvents} loading={loading} />
      </div>
    </div>
  );
}
