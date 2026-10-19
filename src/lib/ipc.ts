// WebSocket transport shared by the Tauri window and a plain browser tab.
//
// The envelope is described in backend/internal/protocol: one request id per
// call, responses correlated by id, unsolicited events pushed by name. There is
// no retry/queueing layer here - a failed request surfaces to the UI, because
// silently replaying a delete would be dangerous.

import { invoke } from "@tauri-apps/api/core";
import type { BackendInfo } from "./types";

export class RpcError extends Error {
  code: string;
  detail?: string;

  constructor(code: string, message: string, detail?: string) {
    super(message);
    this.name = "RpcError";
    this.code = code;
    this.detail = detail;
  }
}

interface Pending {
  resolve: (value: unknown) => void;
  reject: (error: RpcError) => void;
  timer: number;
}

interface Envelope {
  id?: string;
  type: string;
  ok?: boolean;
  result?: unknown;
  error?: { code: string; message: string; detail?: string };
  event?: string;
  payload?: unknown;
}

export type ConnectionState = "idle" | "connecting" | "open" | "closed" | "error";

export function isTauri(): boolean {
  return typeof window !== "undefined" && "__TAURI_INTERNALS__" in window;
}

async function resolveEndpoint(): Promise<{ url: string; info: BackendInfo | null }> {
  if (isTauri()) {
    const info = await invoke<BackendInfo>("backend_info");
    const url = `ws://127.0.0.1:${info.port}/ws?token=${encodeURIComponent(info.token)}`;
    return { url, info };
  }
  // Browser development: run `pnpm backend:run` and open the Vite URL.
  const devUrl =
    (import.meta.env.VITE_BACKEND_WS as string | undefined) ??
    "ws://127.0.0.1:47121/ws?token=dev";
  return { url: devUrl, info: null };
}

class BackendClient {
  private socket: WebSocket | null = null;
  private pending = new Map<string, Pending>();
  private handlers = new Map<string, Set<(payload: unknown) => void>>();
  private seq = 0;
  private connectPromise: Promise<void> | null = null;

  state: ConnectionState = "idle";
  info: BackendInfo | null = null;
  onStateChange: ((state: ConnectionState) => void) | null = null;

  /** Connects, retrying briefly while the sidecar finishes booting. */
  async connect(): Promise<void> {
    if (this.state === "open") return;
    if (this.connectPromise) return this.connectPromise;

    this.connectPromise = (async () => {
      this.setState("connecting");
      let lastError: unknown = null;
      for (let attempt = 0; attempt < 12; attempt += 1) {
        try {
          const { url, info } = await resolveEndpoint();
          this.info = info;
          await this.open(url);
          this.setState("open");
          return;
        } catch (error) {
          lastError = error;
          // Keep the UI informed but do not spam toasts while retrying.
          this.setState("connecting");
          await new Promise((resolve) => setTimeout(resolve, 400 + attempt * 150));
        }
      }
      this.setState("error");
      throw lastError instanceof Error
        ? lastError
        : new Error("无法连接到后端服务");
    })().finally(() => {
      this.connectPromise = null;
    });

    return this.connectPromise;
  }

  private open(url: string): Promise<void> {
    return new Promise((resolve, reject) => {
      let settled = false;
      const socket = new WebSocket(url);

      const timer = window.setTimeout(() => {
        if (settled) return;
        settled = true;
        socket.close();
        reject(new Error("连接后端超时"));
      }, 8000);

      socket.onopen = () => {
        if (settled) return;
        settled = true;
        window.clearTimeout(timer);
        this.socket = socket;
        socket.onmessage = (event) => this.receive(event.data);
        socket.onclose = () => this.handleClose();
        socket.onerror = () => {
          /* onclose always follows */
        };
        resolve();
      };

      socket.onerror = () => {
        if (settled) return;
        settled = true;
        window.clearTimeout(timer);
        reject(new Error("WebSocket 连接失败"));
      };
    });
  }

  private handleClose(): void {
    this.socket = null;
    for (const [, pending] of this.pending) {
      window.clearTimeout(pending.timer);
      pending.reject(new RpcError("DISCONNECTED", "与后端的连接已断开"));
    }
    this.pending.clear();
    if (this.state !== "closed") this.setState("closed");
  }

  private setState(state: ConnectionState): void {
    this.state = state;
    this.onStateChange?.(state);
  }

  private receive(raw: string): void {
    let message: Envelope;
    try {
      message = JSON.parse(raw) as Envelope;
    } catch {
      return;
    }

    if (message.type === "event" && message.event) {
      const listeners = this.handlers.get(message.event);
      if (listeners) {
        for (const listener of listeners) listener(message.payload);
      }
      return;
    }

    if (message.type !== "response" || !message.id) return;
    const pending = this.pending.get(message.id);
    if (!pending) return;
    this.pending.delete(message.id);
    window.clearTimeout(pending.timer);

    if (message.ok) {
      pending.resolve(message.result);
    } else {
      const error = message.error ?? { code: "INTERNAL", message: "未知错误" };
      pending.reject(new RpcError(error.code, error.message, error.detail));
    }
  }

  /** Calls a backend method. `timeoutMs` defaults to two minutes. */
  request<T>(method: string, params?: unknown, timeoutMs = 120_000): Promise<T> {
    if (!this.socket || this.socket.readyState !== WebSocket.OPEN) {
      return Promise.reject(new RpcError("DISCONNECTED", "尚未连接到后端服务"));
    }
    const id = String(++this.seq);
    return new Promise<T>((resolve, reject) => {
      const timer = window.setTimeout(() => {
        this.pending.delete(id);
        reject(new RpcError("TIMEOUT", "请求超时，后端未响应"));
      }, timeoutMs);

      this.pending.set(id, {
        resolve: resolve as (value: unknown) => void,
        reject,
        timer,
      });

      try {
        this.socket!.send(
          JSON.stringify({ id, type: "request", method, params: params ?? {} }),
        );
      } catch (error) {
        window.clearTimeout(timer);
        this.pending.delete(id);
        reject(new RpcError("DISCONNECTED", String(error)));
      }
    });
  }

  /** Subscribes to a backend event. Returns an unsubscribe function. */
  on(event: string, handler: (payload: unknown) => void): () => void {
    let set = this.handlers.get(event);
    if (!set) {
      set = new Set();
      this.handlers.set(event, set);
    }
    set.add(handler);
    return () => set?.delete(handler);
  }
}

export const rpc = new BackendClient();

export function errorText(error: unknown): string {
  if (error instanceof RpcError) {
    return error.detail ? `${error.message}（${error.detail}）` : error.message;
  }
  if (error instanceof Error) return error.message;
  return String(error);
}

export function errorCode(error: unknown): string {
  return error instanceof RpcError ? error.code : "INTERNAL";
}
