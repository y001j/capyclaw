"use client";

import { useEffect, useState, useCallback } from "react";
import { useParams, useRouter } from "next/navigation";
import Link from "next/link";
import { api, getSessionToken, type SkillDetail } from "@/lib/api";

export default function SkillDetailPage() {
  const params = useParams();
  const router = useRouter();
  const id = params.id as string;

  const [skill, setSkill] = useState<SkillDetail | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Edit state
  const [editing, setEditing] = useState(false);
  const [editName, setEditName] = useState("");
  const [editDesc, setEditDesc] = useState("");
  const [editVersion, setEditVersion] = useState("");
  const [editContent, setEditContent] = useState("");
  const [saving, setSaving] = useState(false);

  const fetchSkill = useCallback(async () => {
    try {
      setLoading(true);
      const token = await getSessionToken();
      const data = await api.getSkill(id, token);
      setSkill(data);
      setEditName(data.name);
      setEditDesc(data.description ?? "");
      setEditVersion(data.version);
      setEditContent(data.content_md ?? "");
      setError(null);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setLoading(false);
    }
  }, [id]);

  useEffect(() => {
    fetchSkill();
  }, [fetchSkill]);

  const handleSave = async () => {
    setSaving(true);
    setError(null);
    try {
      const token = await getSessionToken();
      await api.updateSkill(
        id,
        {
          name: editName,
          description: editDesc,
          version: editVersion,
          content_md: editContent,
        },
        token,
      );
      await fetchSkill();
      setEditing(false);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setSaving(false);
    }
  };

  const handleUninstall = async () => {
    if (!confirm("Uninstall this skill?")) return;
    try {
      const token = await getSessionToken();
      await api.uninstallSkill(id, token);
      router.push("/skills");
    } catch (err) {
      setError((err as Error).message);
    }
  };

  if (loading) {
    return (
      <div className="p-8">
        <p className="text-muted-foreground">Loading skill...</p>
      </div>
    );
  }

  if (!skill) {
    return (
      <div className="p-8">
        <p className="text-destructive">{error ?? "Skill not found"}</p>
        <Link href="/skills" className="mt-4 inline-block text-sm text-primary hover:underline">
          &larr; Back to Skills
        </Link>
      </div>
    );
  }

  return (
    <div className="p-8">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <Link href="/skills" className="text-sm text-muted-foreground hover:text-foreground">
            &larr; Skills
          </Link>
          <h1 className="mt-1 text-2xl font-bold">{skill.name}</h1>
          <div className="mt-1 text-sm text-muted-foreground">
            v{skill.version} &middot; {skill.source}
            {skill.verified && <span className="ml-2 text-green-600">Verified</span>}
          </div>
        </div>
        <div className="flex gap-2">
          {!editing && (
            <button
              onClick={() => setEditing(true)}
              className="rounded-md border border-border px-4 py-2 text-sm hover:bg-accent"
            >
              Edit
            </button>
          )}
          <button
            onClick={handleUninstall}
            className="rounded-md border border-destructive/30 px-4 py-2 text-sm text-destructive hover:bg-destructive/10"
          >
            Uninstall
          </button>
        </div>
      </div>

      {error && (
        <div className="mt-4 rounded-md bg-destructive/10 p-3 text-sm text-destructive">
          {error}
        </div>
      )}

      {editing ? (
        /* Edit Mode */
        <div className="mt-6 space-y-4">
          <div>
            <label className="block text-sm font-medium text-muted-foreground">Name</label>
            <input
              type="text"
              value={editName}
              onChange={(e) => setEditName(e.target.value)}
              className="mt-1 w-full rounded-md border border-border bg-background px-3 py-2 text-sm"
            />
          </div>
          <div>
            <label className="block text-sm font-medium text-muted-foreground">Description</label>
            <input
              type="text"
              value={editDesc}
              onChange={(e) => setEditDesc(e.target.value)}
              className="mt-1 w-full rounded-md border border-border bg-background px-3 py-2 text-sm"
            />
          </div>
          <div>
            <label className="block text-sm font-medium text-muted-foreground">Version</label>
            <input
              type="text"
              value={editVersion}
              onChange={(e) => setEditVersion(e.target.value)}
              className="mt-1 w-full rounded-md border border-border bg-background px-3 py-2 text-sm"
            />
          </div>
          <div>
            <label className="block text-sm font-medium text-muted-foreground">
              Content (Markdown)
            </label>
            <textarea
              value={editContent}
              onChange={(e) => setEditContent(e.target.value)}
              rows={20}
              className="mt-1 w-full rounded-md border border-border bg-background px-3 py-2 text-sm font-mono"
            />
          </div>

          {/* Manifest (read-only display) */}
          {skill.manifest && Object.keys(skill.manifest).length > 0 && (
            <div>
              <label className="block text-sm font-medium text-muted-foreground">
                Manifest (from SKILL.md frontmatter)
              </label>
              <pre className="mt-1 overflow-auto rounded-md border border-border bg-muted/50 p-3 text-xs">
                {JSON.stringify(skill.manifest, null, 2)}
              </pre>
            </div>
          )}

          <div className="flex gap-2">
            <button
              onClick={handleSave}
              disabled={saving}
              className="rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
            >
              {saving ? "Saving..." : "Save"}
            </button>
            <button
              onClick={() => {
                setEditing(false);
                setEditName(skill.name);
                setEditDesc(skill.description ?? "");
                setEditVersion(skill.version);
                setEditContent(skill.content_md ?? "");
              }}
              className="rounded-md border border-border px-4 py-2 text-sm hover:bg-accent"
            >
              Cancel
            </button>
          </div>
        </div>
      ) : (
        /* View Mode */
        <div className="mt-6 space-y-6">
          {skill.description && (
            <div>
              <h2 className="text-sm font-medium text-muted-foreground">Description</h2>
              <p className="mt-1 text-sm">{skill.description}</p>
            </div>
          )}

          {/* Manifest */}
          {skill.manifest && Object.keys(skill.manifest).length > 0 && (
            <div>
              <h2 className="text-sm font-medium text-muted-foreground">Manifest</h2>
              <pre className="mt-1 overflow-auto rounded-md border border-border bg-muted/50 p-3 text-xs">
                {JSON.stringify(skill.manifest, null, 2)}
              </pre>
            </div>
          )}

          {/* Content */}
          <div>
            <h2 className="text-sm font-medium text-muted-foreground">Skill Content</h2>
            <pre className="mt-1 overflow-auto whitespace-pre-wrap rounded-md border border-border bg-muted/50 p-4 text-sm">
              {skill.content_md || "(empty)"}
            </pre>
          </div>

          {/* Metadata */}
          <div className="border-t border-border pt-4 text-xs text-muted-foreground">
            <p>ID: {skill.id}</p>
            <p>Sandbox Tier: {skill.sandbox_tier}</p>
            <p>Installed: {new Date(skill.installed_at).toLocaleString()}</p>
            {skill.updated_at && (
              <p>Updated: {new Date(skill.updated_at).toLocaleString()}</p>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
