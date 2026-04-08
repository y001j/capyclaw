"use client";

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { api, getSessionToken, type CronJobWithAgent } from "@/lib/api";

export default function CronsPage() {
  const [jobs, setJobs] = useState<CronJobWithAgent[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchJobs = useCallback(async () => {
    try {
      setLoading(true);
      const token = await getSessionToken();
      const result = await api.listAllCronJobs(token);
      setJobs(result);
      setError(null);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchJobs();
  }, [fetchJobs]);

  const handleToggle = async (job: CronJobWithAgent) => {
    try {
      const token = await getSessionToken();
      await api.updateCronJob(job.agent_id, job.id, { enabled: !job.enabled }, token);
      await fetchJobs();
    } catch (err) {
      setError((err as Error).message);
    }
  };

  const handleDelete = async (job: CronJobWithAgent) => {
    try {
      const token = await getSessionToken();
      await api.deleteCronJob(job.agent_id, job.id, token);
      await fetchJobs();
    } catch (err) {
      setError((err as Error).message);
    }
  };

  return (
    <div className="p-8">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold">Cron Jobs</h1>
          <p className="mt-1 text-muted-foreground">
            All scheduled tasks across agents
          </p>
        </div>
      </div>

      {error && (
        <div className="mt-4 rounded-md bg-destructive/10 p-3 text-sm text-destructive">
          {error}
        </div>
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
              <div className="mt-1 flex items-center gap-3 text-xs text-muted-foreground">
                <span className="font-mono">{job.schedule}</span>
                <span>&middot;</span>
                <Link
                  href={`/agents/${job.agent_id}/crons`}
                  className="text-primary hover:underline"
                >
                  {job.agent_name}
                </Link>
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
                onClick={() => handleDelete(job)}
                className="rounded-md border border-destructive/30 px-3 py-1 text-xs text-destructive hover:bg-destructive/10"
              >
                Delete
              </button>
            </div>
          </div>
        ))}
        {!loading && jobs.length === 0 && (
          <p className="py-8 text-center text-muted-foreground">
            No cron jobs yet. Create one from an agent&apos;s detail page.
          </p>
        )}
      </div>
    </div>
  );
}
