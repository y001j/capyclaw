import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  output: "standalone",
  // Harden headers: prevent UI from accepting gatewayUrl from query strings
  async headers() {
    return [
      {
        source: "/(.*)",
        headers: [
          { key: "X-Frame-Options", value: "DENY" },
          { key: "X-Content-Type-Options", value: "nosniff" },
          { key: "Referrer-Policy", value: "strict-origin-when-cross-origin" },
          {
            key: "Content-Security-Policy",
            value: [
              "default-src 'self'",
              "script-src 'self' 'unsafe-eval' 'unsafe-inline'",
              "style-src 'self' 'unsafe-inline'",
              "img-src 'self' data: blob:",
              "connect-src 'self' ws: wss: http://localhost:18789 ws://localhost:18789",
            ].join("; "),
          },
        ],
      },
    ];
  },
};

export default nextConfig;
