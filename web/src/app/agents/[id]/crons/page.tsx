"use client";

import { useCallback, useEffect, useState } from "react";
import { useParams } from "next/navigation";
import Link from "next/link";
import { api, getSessionToken, type CronJob, type CreateCronJobReq } from "@/lib/api";

export default function AgentCronsPage() {
  const params = useParams();
  const agentId = params.id as string;

  const [jobs, setJobs] = useState<CronJob[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Form state
  const [showForm, setShowForm] = useState(false);
  const [formName, setFormName] = useState("");
  const [formSchedule, setFormSchedule] = useState("");
  const [formPrompt, setFormPrompt] = useState("");
  const [submitting, setSubmitting] = useState(false);

  const fetchJobs = useCallback(async () => {
    try {
      setLoading(true);
      const token = await getSessionToken();
      const result = await api.listCronJobs(agentId, token);
      setJobs(result);
      setError(null);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setLoading(false);
    }
  }, [agentId]);

  useEffect(() => {
    fetchJobs();
  }, [fetchJobs]);

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    setSubmitting(true);
    try {
      const token = await getSessionToken();
      const data: CreateCronJobReq = {
        name: formName,
        schedule: formSchedule,
        prompt: formPrompt,
      };
      await api.createCronJob(agentId, data, token);
      setFormName("");
      setFormSchedule("");
      setFormPrompt("");
      setShowForm(false);
      await fetchJobs();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setSubmitting(false);
    }
  };

  const handleToggle = async (job: CronJob) => {
    try {
      const token = await getSessionToken();
      await api.updateCronJob(agentId, job.id, { enabled: !job.enabled }, token);
      await fetchJobs();
    } catch (err) {
      setError((err as Error).message);
    }
  };

  const handleDelete = async (jobId: string) => {
    try {
      const token = await getSessionToken();
      await api.deleteCronJob(agentId, jobId, token);
      await fetchJobs();
    } catch (err) {
      setError((err as Error).message);
    }
  };

  return (
    <div className="p-8">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Cron Jobs</h1>
        <div className="flex gap-2">
          <button
            onClick={() => setShowForm(!showForm)}
            className="rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90"
          >
            {showForm ? "Cancel" : "New Cron Job"}
          </button>
          <Link
            href={`/agents/${agentId}`}
            className="rounded-md border border-border px-4 py-2 text-sm hover:bg-accent"
          >
            Back to agent
          </Link>
        </div>
      </div>

      {error && (
        <div className="mt-4 rounded-md bg-destructive/10 p-3 text-sm text-destructive">
          {error}
        </div>
      )}

      {showForm && (
        <form onSubmit={handleCreate} className="mt-6 rounded-lg border border-border p-5 space-y-4">
          <div>
            <label className="block text-sm font-medium text-muted-foreground">Name</label>
            <input
              type="text"
              value={formName}
              onChange={(e) => setFormName(e.target.value)}
              placeholder="e.g. daily-summary"
              required
              className="mt-1 w-full rounded-md border border-border bg-background px-3 py-2 text-sm"
            />
          </div>
          <div>
            <label className="block text-sm font-medium text-muted-foreground">
              Schedule (cron expression)
            </label>
            <input
              type="text"
              value={formSchedule}
              onChange={(e) => setFormSchedule(e.target.value)}
              placeholder="e.g. 0 9 * * * (every day at 9am)"
              required
              className="mt-1 w-full rounded-md border border-border bg-background px-3 py-2 text-sm font-mono"
            />
          </div>
          <div>
            <label className="block text-sm font-medium text-muted-foreground">
              Prompt (action to execute)
            </label>
            <textarea
              value={formPrompt}
              onChange={(e) => setFormPrompt(e.target.value)}
              placeholder="What should the agent do when this cron triggers?"
              required
              rows={3}
              className="mt-1 w-full rounded-md border border-border bg-background px-3 py-2 text-sm"
            />
          </div>
          <button
            type="submit"
            disabled={submitting}
            className="rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
          >
            {submitting ? "Creating..." : "Create"}
          </button>
        </form>
      )}

      {loading && (
        <p className="mt-6 text-muted-foreground">Loading cron jobs...</p>
      )}

      <div className="mt-6 space-y-2">
        {jobs.map((job) => (
          <div
            key={job.id}
            className="flex items-center justify-between rounded-lg border border-border p-4"
          >
            <div className="flex-1 min-w-0">
              <div className="flex items-center gap-2">
                <span className="font-medium text-sm">{job.name}</span>
                <span
                  className={`rounded-full px-2 py-0.5 text-xs ${
                    job.enabled
                      ? "bg-green-100 text-green-700"
                      : "bg-gray-100 text-gray-600"
                  }`}
                >
                  {job.enabled ? "enabled" : "disabled"}
                </span>
                {job.last_status && (
                  <span
                    className={`rounded-full px-2 py-0.5 text-xs ${
                      job.last_status === "success"
                        ? "bg-blue-100 text-blue-700"
                        : "bg-red-100 text-red-700"
                    }`}
                  >
                    {job.last_status}
                  </span>
                )}
              </div>
              <div className="mt-1 font-mono text-xs text-muted-foreground">
                {job.schedule}
              </div>
              <div className="mt-1 text-xs text-muted-foreground truncate">
                {job.prompt}
              </div>
              <div className="mt-1 text-xs text-muted-foreground">
                Runs: {job.run_count}
                {job.last_run_at && (
                  <> &middot; Last: {new Date(job.last_run_at).toLocaleString()}</>
                )}
              </div>
            </div>
            <div className="flex items-center gap-2 ml-4">
              <button
                onClick={() => handleToggle(job)}
                className="rounded-md border border-border px-3 py-1 text-xs hover:bg-accent"
              >
                {job.enabled ? "Disable" : "Enable"}
              </button>
              <button
                onClick={() => handleDelete(job.id)}
                className="rounded-md border border-destructive/30 px-3 py-1 text-xs text-destructive hover:bg-destructive/10"
              >
                Delete
              </button>
            </div>
          </div>
        ))}
        {!loading && jobs.length === 0 && (
          <p className="py-8 text-center text-muted-foreground">
            No cron jobs yet
          </p>
        )}
      </div>
    </div>
  );
}
