"use client";

import { useEffect } from "react";
import { useAdminStore } from "@/stores/admin";

export default function AdminPage() {
  const { tenants, loading, fetchTenants, suspendTenant } = useAdminStore();

  useEffect(() => {
    fetchTenants();
  }, [fetchTenants]);

  const handleSuspend = async (id: string) => {
    if (!confirm("Suspend this tenant? All their API access will be blocked.")) return;
    try {
      await suspendTenant(id);
    } catch (err) {
      alert((err as Error).message);
    }
  };

  return (
    <div className="p-8">
      <h1 className="text-2xl font-bold">Tenant Management</h1>
      <p className="mt-1 text-muted-foreground">
        Manage platform tenants
      </p>

      {loading && (
        <p className="mt-6 text-muted-foreground">Loading tenants...</p>
      )}

      <div className="mt-6">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-border text-left">
              <th className="pb-2 font-medium text-muted-foreground">Name</th>
              <th className="pb-2 font-medium text-muted-foreground">Slug</th>
              <th className="pb-2 font-medium text-muted-foreground">Plan</th>
              <th className="pb-2 font-medium text-muted-foreground">Status</th>
              <th className="pb-2 font-medium text-muted-foreground">Created</th>
              <th className="pb-2"></th>
            </tr>
          </thead>
          <tbody>
            {tenants.map((tenant) => (
              <tr key={tenant.id} className="border-b border-border/50">
                <td className="py-3 font-medium">{tenant.name}</td>
                <td className="py-3 font-mono text-muted-foreground">
                  {tenant.slug}
                </td>
                <td className="py-3">{tenant.plan}</td>
                <td className="py-3">
                  <span
                    className={`rounded-full px-2 py-0.5 text-xs ${
                      tenant.status === "active"
                        ? "bg-green-100 text-green-700"
                        : tenant.status === "suspended"
                          ? "bg-red-100 text-red-700"
                          : "bg-gray-100 text-gray-600"
                    }`}
                  >
                    {tenant.status}
                  </span>
                </td>
                <td className="py-3 text-muted-foreground">
                  {new Date(tenant.created_at).toLocaleDateString()}
                </td>
                <td className="py-3">
                  {tenant.status === "active" && (
                    <button
                      onClick={() => handleSuspend(tenant.id)}
                      className="text-xs text-destructive hover:underline"
                    >
                      Suspend
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        {!loading && tenants.length === 0 && (
          <p className="py-8 text-center text-muted-foreground">
            No tenants
          </p>
        )}
      </div>
    </div>
  );
}
