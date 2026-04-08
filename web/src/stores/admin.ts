import { create } from "zustand";
import {
  api,
  getSessionToken,
  type Tenant,
  type AuditEvent,
  type UsageReport,
} from "@/lib/api";

interface AdminStore {
  tenants: Tenant[];
  auditEvents: AuditEvent[];
  usageReport: UsageReport | null;
  loading: boolean;
  error: string | null;

  fetchTenants: (token?: string) => Promise<void>;
  createTenant: (
    data: { name: string; slug: string; plan?: string },
    token?: string,
  ) => Promise<Tenant>;
  updateTenant: (
    id: string,
    data: Partial<Tenant>,
    token?: string,
  ) => Promise<void>;
  suspendTenant: (id: string, token?: string) => Promise<void>;

  fetchAudit: (
    params?: { action?: string; limit?: string },
    token?: string,
  ) => Promise<void>;

  fetchUsage: (
    tenantId: string,
    params?: { start?: string; end?: string },
    token?: string,
  ) => Promise<void>;
}

export const useAdminStore = create<AdminStore>((set) => ({
  tenants: [],
  auditEvents: [],
  usageReport: null,
  loading: false,
  error: null,

  fetchTenants: async (token) => {
    set({ loading: true, error: null });
    try {
      const t = token ?? (await getSessionToken());
      const tenants = await api.listTenants(t);
      set({ tenants, loading: false });
    } catch (err) {
      set({ loading: false, error: (err as Error).message });
    }
  },

  createTenant: async (data, token) => {
    const t = token ?? (await getSessionToken());
    const tenant = await api.createTenant(data, t);
    set((state) => ({ tenants: [...state.tenants, tenant] }));
    return tenant;
  },

  updateTenant: async (id, data, token) => {
    const t = token ?? (await getSessionToken());
    const updated = await api.updateTenant(id, data, t);
    set((state) => ({
      tenants: state.tenants.map((t) => (t.id === id ? updated : t)),
    }));
  },

  suspendTenant: async (id, token) => {
    const t = token ?? (await getSessionToken());
    const updated = await api.updateTenant(
      id,
      { status: "suspended" } as Partial<Tenant>,
      t,
    );
    set((state) => ({
      tenants: state.tenants.map((t) => (t.id === id ? updated : t)),
    }));
  },

  fetchAudit: async (params, token) => {
    set({ loading: true, error: null });
    try {
      const t = token ?? (await getSessionToken());
      const events = await api.queryAudit(params, t);
      set({ auditEvents: events, loading: false });
    } catch (err) {
      set({ loading: false, error: (err as Error).message });
    }
  },

  fetchUsage: async (tenantId, params, token) => {
    set({ loading: true, error: null });
    try {
      const t = token ?? (await getSessionToken());
      const report = await api.getTenantUsage(tenantId, params, t);
      set({ usageReport: report, loading: false });
    } catch (err) {
      set({ loading: false, error: (err as Error).message });
    }
  },
}));
