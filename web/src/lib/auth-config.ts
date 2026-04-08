import type { NextAuthConfig } from "next-auth";

/**
 * NextAuth configuration for CapyClaw.
 * Supports OIDC providers and a dev credentials mode.
 *
 * Environment variables:
 *   AUTH_SECRET          - NextAuth secret (required)
 *   AUTH_OIDC_ISSUER     - OIDC issuer URL
 *   AUTH_OIDC_CLIENT_ID  - OIDC client ID
 *   AUTH_OIDC_CLIENT_SECRET - OIDC client secret
 */

const providers: NextAuthConfig["providers"] = [];

// OIDC provider (production) — uses the generic Keycloak-compatible provider
if (process.env.AUTH_OIDC_ISSUER) {
  providers.push({
    id: "oidc",
    name: "SSO",
    type: "oidc",
    issuer: process.env.AUTH_OIDC_ISSUER,
    clientId: process.env.AUTH_OIDC_CLIENT_ID,
    clientSecret: process.env.AUTH_OIDC_CLIENT_SECRET,
  });
}

// Dev credentials provider (development only)
if (process.env.NODE_ENV === "development" && !process.env.AUTH_OIDC_ISSUER) {
  // eslint-disable-next-line @typescript-eslint/no-require-imports
  const CredentialsProvider =
    require("next-auth/providers/credentials").default;
  providers.push(
    CredentialsProvider({
      id: "dev-credentials",
      name: "Dev Login",
      credentials: {
        email: { label: "Email", type: "email" },
        password: { label: "Password", type: "password" },
      },
      async authorize(credentials: Record<string, unknown> | undefined) {
        // In dev mode, accept any email/password
        if (credentials?.email) {
          return {
            id: "dev-user-001",
            email: credentials.email as string,
            name: "Dev User",
            tid: "dev-tenant-001",
            role: "admin",
          };
        }
        return null;
      },
    }),
  );
}

export const authConfig: NextAuthConfig = {
  providers,
  session: { strategy: "jwt" },
  pages: {
    signIn: "/login",
  },
  callbacks: {
    jwt({ token, user }) {
      if (user) {
        // Include custom claims from the OIDC/credentials provider
        token.tid = (user as Record<string, unknown>).tid ?? "";
        token.role = (user as Record<string, unknown>).role ?? "operator";
      }
      return token;
    },
    session({ session, token }) {
      if (session.user) {
        (session as unknown as Record<string, unknown>).tid = token.tid;
        (session as unknown as Record<string, unknown>).role = token.role;
        (session as unknown as Record<string, unknown>).accessToken = token.sub;
      }
      return session;
    },
  },
};
