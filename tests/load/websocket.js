/**
 * CapyClaw WebSocket Load Test (k6)
 *
 * Simulates concurrent WebSocket connections sending chat messages.
 *
 * Usage:
 *   k6 run tests/load/websocket.js \
 *     --env TOKEN=<jwt-token> \
 *     --env AGENT_ID=<agent-uuid>
 */

import ws from "k6/ws";
import { check, sleep } from "k6";
import { Counter, Trend } from "k6/metrics";

const wsUrl = __ENV.WS_URL || "ws://localhost:18789";
const token = __ENV.TOKEN || "test-token";
const agentId = __ENV.AGENT_ID || "test-agent";

// Custom metrics
const messagesReceived = new Counter("ws_messages_received");
const messageLatency = new Trend("ws_message_latency_ms");

export const options = {
  stages: [
    { duration: "30s", target: 100 },
    { duration: "1m", target: 500 },
    { duration: "1m", target: 1000 },
    { duration: "30s", target: 0 },
  ],
  thresholds: {
    ws_message_latency_ms: ["p(95)<5000"],
    ws_messages_received: ["count>100"],
  },
};

export default function () {
  const url = `${wsUrl}/ws?token=${token}`;
  const startTime = Date.now();

  const res = ws.connect(url, {}, function (socket) {
    socket.on("open", () => {
      // Send a chat message
      socket.send(
        JSON.stringify({
          jsonrpc: "2.0",
          id: `${__VU}-${__ITER}`,
          method: "chat.send",
          params: {
            agent_id: agentId,
            content: `Load test message from VU ${__VU}`,
          },
        }),
      );
    });

    socket.on("message", (data) => {
      messagesReceived.add(1);

      try {
        const msg = JSON.parse(data);
        if (msg.event === "agent.message.done" || msg.result) {
          const latency = Date.now() - startTime;
          messageLatency.add(latency);
          socket.close();
        }
      } catch (e) {
        // ignore parse errors
      }
    });

    // Timeout after 30 seconds
    socket.setTimeout(() => {
      socket.close();
    }, 30000);
  });

  check(res, {
    "WebSocket connected": (r) => r && r.status === 101,
  });

  sleep(1);
}
