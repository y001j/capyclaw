import { useEffect, useRef } from "react";
import { gatewayWS } from "@/lib/ws";

/**
 * Subscribes to a WebSocket event method and cleans up on unmount.
 */
export function useWebSocket(method: string, handler: (msg: unknown) => void) {
  const handlerRef = useRef(handler);
  handlerRef.current = handler;

  useEffect(() => {
    const off = gatewayWS.on(method, (msg) => handlerRef.current(msg));
    return off;
  }, [method]);
}
