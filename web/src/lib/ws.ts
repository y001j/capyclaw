/**
 * WebSocket client for the Riverbank gateway.
 * Handles reconnection, JSON-RPC framing, and message routing.
 *
 * SECURITY: The gateway URL is sourced from environment variables only —
 * never from URL query parameters (prevents the ClawJacked attack vector).
 */

const GATEWAY_WS_URL =
  process.env.NEXT_PUBLIC_GATEWAY_WS_URL ?? "wss://localhost:18789";

type MessageHandler = (msg: unknown) => void;

interface PendingRequest {
  resolve: (result: unknown) => void;
  reject: (error: Error) => void;
  timer: ReturnType<typeof setTimeout>;
}

export class GatewayWebSocket {
  private ws: WebSocket | null = null;
  private handlers = new Map<string, MessageHandler[]>();
  private pending = new Map<string, PendingRequest>();
  private reconnectDelay = 1000;
  private maxReconnectDelay = 30000;
  private token: string | null = null;

  connect(token?: string): void {
    if (token) {
      this.token = token;
    }

    // URL is never derived from window.location or query parameters.
    const url = this.token
      ? `${GATEWAY_WS_URL}/ws?token=${this.token}`
      : `${GATEWAY_WS_URL}/ws`;

    this.ws = new WebSocket(url);

    this.ws.onopen = () => {
      this.reconnectDelay = 1000;
      console.info("[CapyClaw] WebSocket connected");
    };

    this.ws.onmessage = (event) => {
      try {
        const frame = JSON.parse(event.data as string) as {
          id?: string;
          method?: string;
          result?: unknown;
          error?: { message: string };
        };

        // Handle RPC response
        if (frame.id && this.pending.has(frame.id)) {
          const req = this.pending.get(frame.id)!;
          this.pending.delete(frame.id);
          clearTimeout(req.timer);
          if (frame.error) {
            req.reject(new Error(frame.error.message));
          } else {
            req.resolve(frame.result);
          }
          return;
        }

        // Handle notification/event
        const method = frame.method;
        if (method && this.handlers.has(method)) {
          this.handlers.get(method)!.forEach((h) => h(frame));
        }
      } catch {
        console.error("[CapyClaw] Failed to parse WebSocket message");
      }
    };

    this.ws.onclose = () => {
      console.warn(
        `[CapyClaw] WebSocket closed, reconnecting in ${this.reconnectDelay}ms`,
      );
      setTimeout(() => this.connect(), this.reconnectDelay);
      this.reconnectDelay = Math.min(
        this.reconnectDelay * 2,
        this.maxReconnectDelay,
      );
    };
  }

  on(method: string, handler: MessageHandler): () => void {
    if (!this.handlers.has(method)) this.handlers.set(method, []);
    this.handlers.get(method)!.push(handler);
    return () => {
      const arr = this.handlers.get(method) ?? [];
      this.handlers.set(
        method,
        arr.filter((h) => h !== handler),
      );
    };
  }

  send(method: string, params?: unknown): void {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
      console.warn("[CapyClaw] WebSocket not ready, dropping message");
      return;
    }
    const frame = { jsonrpc: "2.0", id: crypto.randomUUID(), method, params };
    this.ws.send(JSON.stringify(frame));
  }

  /**
   * Send a JSON-RPC request and wait for the response.
   * Times out after 30 seconds by default.
   */
  sendRPC<T = unknown>(
    method: string,
    params?: unknown,
    timeoutMs = 30000,
  ): Promise<T> {
    return new Promise<T>((resolve, reject) => {
      if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
        reject(new Error("WebSocket not connected"));
        return;
      }

      const id = crypto.randomUUID();
      const timer = setTimeout(() => {
        this.pending.delete(id);
        reject(new Error(`RPC timeout: ${method}`));
      }, timeoutMs);

      this.pending.set(id, {
        resolve: resolve as (result: unknown) => void,
        reject,
        timer,
      });

      const frame = { jsonrpc: "2.0", id, method, params };
      this.ws.send(JSON.stringify(frame));
    });
  }

  disconnect(): void {
    // Reject all pending requests
    for (const [id, req] of this.pending) {
      clearTimeout(req.timer);
      req.reject(new Error("WebSocket disconnected"));
      this.pending.delete(id);
    }
    this.ws?.close(1000, "user disconnect");
    this.ws = null;
  }

  get isConnected(): boolean {
    return this.ws?.readyState === WebSocket.OPEN;
  }
}

export const gatewayWS = new GatewayWebSocket();
