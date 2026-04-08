"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { useAgentStore } from "@/stores/agent";
import { api, getSessionToken, type ModelInfo } from "@/lib/api";

export default function NewAgentPage() {
  const router = useRouter();
  const createAgent = useAgentStore((s) => s.createAgent);
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [models, setModels] = useState<ModelInfo[]>([]);

  useEffect(() => {
    (async () => {
      const token = await getSessionToken();
      try {
        const list = await api.listModels(token);
        setModels(list);
      } catch {
        // fallback if endpoint unavailable
      }
    })();
  }, []);

  const defaultModel = models.find((m) => m.default)?.id ?? models[0]?.id ?? "";

  const handleSubmit = async (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    setError(null);
    setSubmitting(true);

    const form = new FormData(e.currentTarget);
    const name = form.get("name") as string;
    const slug = form.get("slug") as string;
    const model = (form.get("model") as string) || undefined;
    const systemPrompt = (form.get("system_prompt") as string) || undefined;

    try {
      const agent = await createAgent({ name, slug, model, system_prompt: systemPrompt });
      router.push(`/agents/${agent.id}`);
    } catch (err) {
      setError((err as Error).message);
      setSubmitting(false);
    }
  };

  return (
    <div className="mx-auto max-w-xl p-8">
      <h1 className="text-2xl font-bold">Create Agent</h1>
      <p className="mt-1 text-muted-foreground">Configure a new AI agent</p>

      <form onSubmit={handleSubmit} className="mt-8 space-y-5">
        <div>
          <label className="block text-sm font-medium">Name</label>
          <input
            name="name"
            required
            className="mt-1 w-full rounded-md border border-input bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
            placeholder="My Agent"
          />
        </div>

        <div>
          <label className="block text-sm font-medium">Slug</label>
          <input
            name="slug"
            required
            pattern="[a-z0-9-]+"
            className="mt-1 w-full rounded-md border border-input bg-background px-3 py-2 text-sm font-mono focus:outline-none focus:ring-2 focus:ring-ring"
            placeholder="my-agent"
          />
          <p className="mt-1 text-xs text-muted-foreground">
            Lowercase letters, numbers, and hyphens only
          </p>
        </div>

        <div>
          <label className="block text-sm font-medium">Model</label>
          <select
            name="model"
            defaultValue={defaultModel}
            className="mt-1 w-full rounded-md border border-input bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
          >
            {models.map((m) => (
              <option key={m.id} value={m.id}>
                {m.id} ({m.provider}){m.default ? " — default" : ""}
              </option>
            ))}
            {models.length === 0 && (
              <option value="">Loading models...</option>
            )}
          </select>
        </div>

        <div>
          <label className="block text-sm font-medium">System Prompt</label>
          <textarea
            name="system_prompt"
            rows={4}
            className="mt-1 w-full rounded-md border border-input bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
            placeholder="You are a helpful assistant..."
          />
        </div>

        {error && (
          <div className="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive">
            {error}
          </div>
        )}

        <div className="flex gap-3">
          <button
            type="submit"
            disabled={submitting || models.length === 0}
            className="rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
          >
            {submitting ? "Creating..." : "Create Agent"}
          </button>
          <button
            type="button"
            onClick={() => router.back()}
            className="rounded-md border border-border px-4 py-2 text-sm hover:bg-accent"
          >
            Cancel
          </button>
        </div>
      </form>
    </div>
  );
}
