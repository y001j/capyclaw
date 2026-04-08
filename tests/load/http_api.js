/**
 * CapyClaw HTTP API Load Test (k6)
 *
 * Tests throughput of REST API endpoints under load.
 *
 * Usage:
 *   k6 run tests/load/http_api.js --env TOKEN=<jwt-token>
 */

import http from "k6/http";
import { check, sleep } from "k6";
import { Rate, Trend } from "k6/metrics";

const baseUrl = __ENV.BASE_URL || "http://localhost:18789";
const token = __ENV.TOKEN || "test-token";

const errorRate = new Rate("http_error_rate");
const apiLatency = new Trend("api_latency_ms");

export const options = {
  stages: [
    { duration: "30s", target: 50 },
    { duration: "1m", target: 200 },
    { duration: "1m", target: 500 },
    { duration: "30s", target: 0 },
  ],
  thresholds: {
    http_error_rate: ["rate<0.05"], // <5% error rate
    api_latency_ms: ["p(95)<2000"], // p95 < 2s
    http_req_duration: ["p(99)<5000"], // p99 < 5s
  },
};

const headers = {
  Authorization: `Bearer ${token}`,
  "Content-Type": "application/json",
};

export default function () {
  // Health check
  const healthRes = http.get(`${baseUrl}/healthz`);
  check(healthRes, {
    "healthz 200": (r) => r.status === 200,
  });

  // List agents
  const start = Date.now();
  const agentsRes = http.get(`${baseUrl}/api/v1/agents`, { headers });
  apiLatency.add(Date.now() - start);
  errorRate.add(agentsRes.status >= 400);
  check(agentsRes, {
    "list agents 200": (r) => r.status === 200,
  });

  // Non-streaming chat completion
  const chatStart = Date.now();
  const chatRes = http.post(
    `${baseUrl}/v1/chat/completions`,
    JSON.stringify({
      model: "claude-sonnet-4-20250514",
      stream: false,
      messages: [{ role: "user", content: "Hello" }],
    }),
    { headers, timeout: "30s" },
  );
  apiLatency.add(Date.now() - chatStart);
  errorRate.add(chatRes.status >= 400);
  check(chatRes, {
    "chat completion 200": (r) => r.status === 200,
  });

  sleep(1);
}
