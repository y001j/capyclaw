"use client";

import type { AuditEvent } from "@/lib/api";

interface AuditTableProps {
  events: AuditEvent[];
  loading?: boolean;
}

export function AuditTable({ events, loading }: AuditTableProps) {
  if (loading) {
    return <p className="py-4 text-muted-foreground">Loading audit events...</p>;
  }

  if (events.length === 0) {
    return <p className="py-4 text-center text-muted-foreground">No audit events found</p>;
  }

  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-border text-left">
            <th className="pb-2 pr-4 font-medium text-muted-foreground">ID</th>
            <th className="pb-2 pr-4 font-medium text-muted-foreground">Action</th>
            <th className="pb-2 pr-4 font-medium text-muted-foreground">Actor</th>
            <th className="pb-2 pr-4 font-medium text-muted-foreground">Resource</th>
            <th className="pb-2 pr-4 font-medium text-muted-foreground">IP</th>
            <th className="pb-2 font-medium text-muted-foreground">Time</th>
          </tr>
        </thead>
        <tbody>
          {events.map((event) => (
            <tr key={event.id} className="border-b border-border/50">
              <td className="py-2 pr-4 font-mono text-xs text-muted-foreground">
                {event.id}
              </td>
              <td className="py-2 pr-4">
                <span className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">
                  {event.action}
                </span>
              </td>
              <td className="py-2 pr-4 text-xs">
                <span className="text-muted-foreground">{event.actor_type}</span>
                {event.actor_id && (
                  <span className="ml-1 font-mono">
                    {event.actor_id.slice(0, 8)}...
                  </span>
                )}
              </td>
              <td className="py-2 pr-4 text-xs">
                {event.resource_type && (
                  <span className="font-mono">{event.resource_type}</span>
                )}
                {event.resource_id && (
                  <span className="ml-1 font-mono text-muted-foreground">
                    {event.resource_id.slice(0, 8)}...
                  </span>
                )}
              </td>
              <td className="py-2 pr-4 font-mono text-xs text-muted-foreground">
                {event.ip_address}
              </td>
              <td className="py-2 text-xs text-muted-foreground">
                {new Date(event.created_at).toLocaleString()}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
