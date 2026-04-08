"use client";

import { useEffect, useState, useCallback } from "react";
import Link from "next/link";
import {
  api,
  getSessionToken,
  type Skill,
  type ClawHubSearchResult,
} from "@/lib/api";

type Tab = "installed" | "clawhub" | "create";

export default function SkillsPage() {
  const [tab, setTab] = useState<Tab>("installed");
  const [skills, setSkills] = useState<Skill[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // ClawHub state
  const [searchQuery, setSearchQuery] = useState("");
  const [searchResults, setSearchResults] = useState<ClawHubSearchResult[]>([]);
  const [searching, setSearching] = useState(false);
  const [installing, setInstalling] = useState<string | null>(null);

  // Create state
  const [createName, setCreateName] = useState("");
  const [createDesc, setCreateDesc] = useState("");
  const [createContent, setCreateContent] = useState("");
  const [createMode, setCreateMode] = useState<"form" | "skillmd">("form");
  const [creating, setCreating] = useState(false);

  const fetchSkills = useCallback(async () => {
    try {
      setLoading(true);
      const token = await getSessionToken();
      const list = await api.listSkills(token);
      setSkills(list);
      setError(null);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchSkills();
  }, [fetchSkills]);

  const handleUninstall = async (id: string) => {
    if (!confirm("Uninstall this skill?")) return;
    try {
      const token = await getSessionToken();
      await api.uninstallSkill(id, token);
      setSkills((prev) => prev.filter((s) => s.id !== id));
    } catch (err) {
      setError((err as Error).message);
    }
  };

  const handleSearch = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!searchQuery.trim()) return;
    setSearching(true);
    setError(null);
    try {
      const token = await getSessionToken();
      const raw = await api.searchClawHub(searchQuery.trim(), 20, token);
      // ClawHub API may return {results: [...]} or a bare array
      const results = Array.isArray(raw) ? raw : ((raw as Record<string, unknown>).results as ClawHubSearchResult[] ?? []);
      setSearchResults(results);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setSearching(false);
    }
  };

  const handleInstallFromClawHub = async (slug: string) => {
    setInstalling(slug);
    setError(null);
    try {
      const token = await getSessionToken();
      await api.installFromClawHub(slug, undefined, token);
      await fetchSkills();
      setTab("installed");
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setInstalling(null);
    }
  };

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    setCreating(true);
    setError(null);
    try {
      const token = await getSessionToken();

      if (createMode === "skillmd") {
        // Parse SKILL.md content
        const parsed = await api.parseSkillMD(createContent, token);
        const name = (parsed.frontmatter.name as string) || "unnamed-skill";
        const desc = (parsed.frontmatter.description as string) || "";
        const version = (parsed.frontmatter.version as string) || "1.0.0";

        await api.installSkill(
          {
            name,
            source: "manual",
            description: desc,
            version,
            content_md: parsed.content_md,
            manifest: parsed.frontmatter,
          } as never,
          token,
        );
      } else {
        await api.installSkill(
          {
            name: createName,
            source: "manual",
            description: createDesc,
            content_md: createContent,
          } as never,
          token,
        );
      }

      setCreateName("");
      setCreateDesc("");
      setCreateContent("");
      await fetchSkills();
      setTab("installed");
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setCreating(false);
    }
  };

  const tabClass = (t: Tab) =>
    `px-4 py-2 text-sm font-medium border-b-2 transition-colors ${
      tab === t
        ? "border-primary text-primary"
        : "border-transparent text-muted-foreground hover:text-foreground"
    }`;

  return (
    <div className="p-8">
      <h1 className="text-2xl font-bold">Skills</h1>
      <p className="mt-1 text-muted-foreground">
        Manage installed skills and browse ClawHub marketplace
      </p>

      {error && (
        <div className="mt-4 rounded-md bg-destructive/10 p-3 text-sm text-destructive">
          {error}
        </div>
      )}

      {/* Tabs */}
      <div className="mt-6 flex border-b border-border">
        <button className={tabClass("installed")} onClick={() => setTab("installed")}>
          Installed ({skills.length})
        </button>
        <button className={tabClass("clawhub")} onClick={() => setTab("clawhub")}>
          ClawHub
        </button>
        <button className={tabClass("create")} onClick={() => setTab("create")}>
          Create
        </button>
      </div>

      {/* Installed Tab */}
      {tab === "installed" && (
        <div className="mt-6 space-y-3">
          {loading && <p className="text-muted-foreground">Loading skills...</p>}
          {skills.map((skill) => (
            <div
              key={skill.id}
              className="flex items-center justify-between rounded-lg border border-border p-4 hover:bg-accent/50 transition-colors"
            >
              <Link href={`/skills/${skill.id}`} className="flex-1 min-w-0">
                <div className="font-semibold">{skill.name}</div>
                <div className="mt-1 text-sm text-muted-foreground">
                  v{skill.version} &middot; {skill.source}
                  {skill.verified && (
                    <span className="ml-2 text-green-600">Verified</span>
                  )}
                </div>
                {skill.description && (
                  <p className="mt-1 text-sm text-muted-foreground truncate">
                    {skill.description}
                  </p>
                )}
              </Link>
              <button
                onClick={(e) => {
                  e.preventDefault();
                  handleUninstall(skill.id);
                }}
                className="ml-4 rounded-md border border-destructive/30 px-3 py-1.5 text-sm text-destructive hover:bg-destructive/10"
              >
                Uninstall
              </button>
            </div>
          ))}
          {!loading && skills.length === 0 && (
            <p className="py-8 text-center text-muted-foreground">
              No skills installed. Browse ClawHub or create one manually.
            </p>
          )}
        </div>
      )}

      {/* ClawHub Tab */}
      {tab === "clawhub" && (
        <div className="mt-6">
          <form onSubmit={handleSearch} className="flex gap-2">
            <input
              type="text"
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              placeholder="Search ClawHub skills..."
              className="flex-1 rounded-md border border-border bg-background px-3 py-2 text-sm"
            />
            <button
              type="submit"
              disabled={searching}
              className="rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
            >
              {searching ? "Searching..." : "Search"}
            </button>
          </form>

          <div className="mt-4 space-y-3">
            {searchResults.map((result) => (
              <div
                key={result.slug}
                className="flex items-center justify-between rounded-lg border border-border p-4"
              >
                <div className="flex-1 min-w-0">
                  <div className="font-semibold">
                    {result.displayName || result.slug}
                  </div>
                  <div className="mt-1 font-mono text-xs text-muted-foreground">
                    {result.slug} &middot; v{result.version}
                  </div>
                  {result.summary && (
                    <p className="mt-1 text-sm text-muted-foreground truncate">
                      {result.summary}
                    </p>
                  )}
                </div>
                <button
                  onClick={() => handleInstallFromClawHub(result.slug)}
                  disabled={installing === result.slug}
                  className="ml-4 rounded-md bg-primary px-4 py-1.5 text-sm font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
                >
                  {installing === result.slug ? "Installing..." : "Install"}
                </button>
              </div>
            ))}
            {!searching && searchResults.length === 0 && searchQuery && (
              <p className="py-8 text-center text-muted-foreground">
                No results. Try a different search term.
              </p>
            )}
            {!searchQuery && (
              <p className="py-8 text-center text-muted-foreground">
                Search for skills on ClawHub to get started.
              </p>
            )}
          </div>
        </div>
      )}

      {/* Create Tab */}
      {tab === "create" && (
        <div className="mt-6">
          <div className="mb-4 flex gap-2">
            <button
              onClick={() => setCreateMode("form")}
              className={`rounded-md px-3 py-1 text-sm ${
                createMode === "form"
                  ? "bg-primary text-primary-foreground"
                  : "border border-border hover:bg-accent"
              }`}
            >
              Form
            </button>
            <button
              onClick={() => setCreateMode("skillmd")}
              className={`rounded-md px-3 py-1 text-sm ${
                createMode === "skillmd"
                  ? "bg-primary text-primary-foreground"
                  : "border border-border hover:bg-accent"
              }`}
            >
              Paste SKILL.md
            </button>
          </div>

          <form onSubmit={handleCreate} className="space-y-4">
            {createMode === "form" ? (
              <>
                <div>
                  <label className="block text-sm font-medium text-muted-foreground">Name</label>
                  <input
                    type="text"
                    value={createName}
                    onChange={(e) => setCreateName(e.target.value)}
                    required
                    className="mt-1 w-full rounded-md border border-border bg-background px-3 py-2 text-sm"
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium text-muted-foreground">Description</label>
                  <input
                    type="text"
                    value={createDesc}
                    onChange={(e) => setCreateDesc(e.target.value)}
                    className="mt-1 w-full rounded-md border border-border bg-background px-3 py-2 text-sm"
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium text-muted-foreground">
                    Skill Content (Markdown instructions for the LLM)
                  </label>
                  <textarea
                    value={createContent}
                    onChange={(e) => setCreateContent(e.target.value)}
                    required
                    rows={12}
                    className="mt-1 w-full rounded-md border border-border bg-background px-3 py-2 text-sm font-mono"
                  />
                </div>
              </>
            ) : (
              <div>
                <label className="block text-sm font-medium text-muted-foreground">
                  SKILL.md Content (YAML frontmatter + Markdown)
                </label>
                <textarea
                  value={createContent}
                  onChange={(e) => setCreateContent(e.target.value)}
                  required
                  rows={16}
                  placeholder={`---\nname: my-skill\ndescription: What this skill does\nversion: 1.0.0\n---\n\n# My Skill\n\nInstructions for the LLM...`}
                  className="mt-1 w-full rounded-md border border-border bg-background px-3 py-2 text-sm font-mono"
                />
              </div>
            )}
            <button
              type="submit"
              disabled={creating}
              className="rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
            >
              {creating ? "Creating..." : "Create Skill"}
            </button>
          </form>
        </div>
      )}
    </div>
  );
}
