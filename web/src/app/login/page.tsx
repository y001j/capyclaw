"use client";

import { useState } from "react";
import { signIn, getProviders } from "next-auth/react";
import { useEffect } from "react";

type AuthProvider = {
  id: string;
  name: string;
  type: string;
};

export default function LoginPage() {
  const [providers, setProviders] = useState<Record<string, AuthProvider>>({});
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    getProviders().then((p) => {
      if (p) setProviders(p);
    });
  }, []);

  const handleOIDCSignIn = async (providerId: string) => {
    setLoading(true);
    setError(null);
    await signIn(providerId, { callbackUrl: "/" });
  };

  const handleCredentialsSignIn = async (
    e: React.FormEvent<HTMLFormElement>,
  ) => {
    e.preventDefault();
    setError(null);
    setLoading(true);

    const form = new FormData(e.currentTarget);
    const email = form.get("email") as string;
    const password = form.get("password") as string;

    const result = await signIn("dev-credentials", {
      email,
      password,
      redirect: false,
    });

    if (result?.error) {
      setError("Invalid credentials");
      setLoading(false);
    } else {
      window.location.href = "/";
    }
  };

  const hasOIDC = Object.values(providers).some((p) => p.id === "oidc");
  const hasDevCredentials = Object.values(providers).some(
    (p) => p.id === "dev-credentials",
  );

  return (
    <div className="flex min-h-screen items-center justify-center bg-background">
      <div className="w-full max-w-sm rounded-lg border border-border bg-card p-8">
        <h1 className="text-center text-2xl font-bold">CapyClaw</h1>
        <p className="mt-2 text-center text-sm text-muted-foreground">
          Sign in to your account
        </p>

        <div className="mt-8 space-y-4">
          {/* OIDC SSO Button */}
          {hasOIDC && (
            <button
              onClick={() => handleOIDCSignIn("oidc")}
              disabled={loading}
              className="w-full rounded-md bg-primary py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
            >
              {loading ? "Signing in..." : "Sign in with SSO"}
            </button>
          )}

          {/* Dev credentials form */}
          {hasDevCredentials && (
            <>
              {hasOIDC && (
                <div className="relative">
                  <div className="absolute inset-0 flex items-center">
                    <span className="w-full border-t border-border" />
                  </div>
                  <div className="relative flex justify-center text-xs">
                    <span className="bg-card px-2 text-muted-foreground">
                      or dev login
                    </span>
                  </div>
                </div>
              )}
              <form onSubmit={handleCredentialsSignIn} className="space-y-4">
                <div>
                  <label className="block text-sm font-medium">Email</label>
                  <input
                    name="email"
                    type="email"
                    required
                    className="mt-1 w-full rounded-md border border-input bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
                    placeholder="admin@capyclaw.io"
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium">Password</label>
                  <input
                    name="password"
                    type="password"
                    required
                    className="mt-1 w-full rounded-md border border-input bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
                  />
                </div>
                <button
                  type="submit"
                  disabled={loading}
                  className="w-full rounded-md bg-primary py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
                >
                  {loading ? "Signing in..." : "Sign In"}
                </button>
              </form>
            </>
          )}

          {error && (
            <div className="text-center text-sm text-destructive">{error}</div>
          )}
        </div>

        <p className="mt-6 text-center text-xs text-muted-foreground">
          Authentication powered by next-auth (OIDC)
        </p>
      </div>
    </div>
  );
}
