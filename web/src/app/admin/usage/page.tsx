"use client";

import { useEffect, useState } from "react";
import { useAdminStore } from "@/stores/admin";
import { UsageChart } from "@/components/UsageChart";

export default function UsagePage() {
  const { tenants, usageReport, loading, fetchTenants, fetchUsage } =
    useAdminStore();
  const [selectedTenant, setSelectedTenant] = useState("");

  useEffect(() => {
    fetchTenants();
  }, [fetchTenants]);

  useEffect(() => {
    if (selectedTenant) {
      fetchUsage(selectedTenant);
    }
  }, [selectedTenant, fetchUsage]);

  // Auto-select first tenant
  useEffect(() => {
    if (!selectedTenant && tenants.length > 0) {
      setSelectedTenant(tenants[0].id);
    }
  }, [tenants, selectedTenant]);

  return (
    <div className="p-8">
      <h1 className="text-2xl font-bold">Usage & Billing</h1>
      <p className="mt-1 text-muted-foreground">
        Token usage and cost breakdown by model
      </p>

      <div className="mt-6">
        <label className="text-sm font-medium">Tenant</label>
        <select
          value={selectedTenant}
          onChange={(e) => setSelectedTenant(e.target.value)}
          className="ml-3 rounded-md border border-input bg-background px-3 py-1.5 text-sm"
        >
          <option value="">Select tenant...</option>
          {tenants.map((t) => (
            <option key={t.id} value={t.id}>
              {t.name} ({t.slug})
            </option>
          ))}
        </select>
      </div>

      <div className="mt-6">
        {loading && <p className="text-muted-foreground">Loading...</p>}
        {usageReport && <UsageChart report={usageReport} />}
        {!loading && !usageReport && selectedTenant && (
          <p className="text-muted-foreground">No usage data available</p>
        )}
      </div>
    </div>
  );
}
