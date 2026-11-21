/**
 * Live invalidation transport for the browser client.
 *
 * The stream is a change signal, not state: a frame only says which cached
 * data may be stale. Everything is re-read through the normal API client.
 * `fetch` streaming is used instead of `EventSource` because the reconnect
 * policy, the abort handling and the retry hint then live in one place (the
 * cookie session is sent either way).
 */

export const EVENTS_PATH = "/api/v1/events";

const RECONNECT_DELAY_MS = 3_000;
const MIN_RECONNECT_DELAY_MS = 1_000;
const MAX_RECONNECT_DELAY_MS = 30_000;

export interface ServerEventMessage {
  event: string;
  data: string;
  id?: string;
  retry?: number;
}

interface Frame {
  event?: string;
  data: string[];
  id?: string;
  retry?: number;
}

/**
 * Incremental WHATWG frame parser. A network chunk boundary is unrelated to a
 * frame boundary, so the parser keeps the trailing partial line between calls.
 */
export function createServerEventParser(): (chunk: string) => ServerEventMessage[] {
  let pendingLine = "";
  let frame: Frame | null = null;

  return (chunk: string): ServerEventMessage[] => {
    const messages: ServerEventMessage[] = [];
    const lines = (pendingLine + chunk).split("\n");
    pendingLine = lines.pop() ?? "";

    for (const rawLine of lines) {
      const line = rawLine.endsWith("\r") ? rawLine.slice(0, -1) : rawLine;

      if (line === "") {
        // An empty line ends the frame. A frame carrying only retry still
        // reports the new delay; a comment-only heartbeat frame is dropped.
        if (frame && (frame.event !== undefined || frame.data.length > 0 || frame.retry !== undefined)) {
          const message: ServerEventMessage = {
            event: frame.event ?? "message",
            data: frame.data.join("\n"),
          };
          if (frame.id !== undefined) message.id = frame.id;
          if (frame.retry !== undefined) message.retry = frame.retry;
          messages.push(message);
        }
        frame = null;
        continue;
      }

      // A line starting with a colon is a comment (keep-alive).
      if (line.startsWith(":")) continue;

      const colon = line.indexOf(":");
      const field = colon === -1 ? line : line.slice(0, colon);
      let value = colon === -1 ? "" : line.slice(colon + 1);
      if (value.startsWith(" ")) value = value.slice(1);

      frame ??= { data: [] };
      switch (field) {
        case "event":
          frame.event = value;
          break;
        case "data":
          frame.data.push(value);
          break;
        case "id":
          frame.id = value;
          break;
        case "retry": {
          const retry = Number(value);
          if (Number.isFinite(retry) && retry > 0) frame.retry = retry;
          break;
        }
        default:
          // Unknown fields are ignored for forward compatibility.
          break;
      }
    }
    return messages;
  };
}

/** Data families a frame can invalidate. */
export type ServerRefreshTarget = "library" | "favorite" | "like";

/**
 * The single event-to-refresh mapping. Every current query nests under the
 * remote root key, so all targets invalidate that root; the split is kept so a
 * future finer-grained key can change one table entry only.
 */
const REFRESH_TARGETS: Readonly<Record<string, readonly ServerRefreshTarget[]>> = {
  "library.changed": ["library"],
  "upload.done": ["library"],
  "favorite.changed": ["favorite"],
  "like.changed": ["like"],
};

export function refreshTargets(event: string): readonly ServerRefreshTarget[] {
  return REFRESH_TARGETS[event] ?? [];
}

export interface ServerEventOptions {
  url: string;
  onMessage: (message: ServerEventMessage) => void;
  /** Fired on every successful connection, including reconnects. */
  onOpen?: () => void;
  /** A rejected session stops reconnecting until the session gate restarts it. */
  onAuthFailure?: () => void;
}

/**
 * Opens the stream and reconnects with backoff. Returns the cancel function;
 * the caller owns the lifetime and must call it on unmount.
 */
export function subscribeServerEvents(options: ServerEventOptions): () => void {
  let cancelled = false;
  let controller: AbortController | null = null;
  let timer: ReturnType<typeof setTimeout> | null = null;
  let reconnectDelay = RECONNECT_DELAY_MS;
  let lastRetry: number | null = null;

  const cancel = (): void => {
    cancelled = true;
    controller?.abort();
    if (timer !== null) clearTimeout(timer);
  };

  const connect = async (): Promise<void> => {
    controller = new AbortController();
    try {
      const response = await fetch(options.url, {
        headers: { Accept: "text/event-stream" },
        credentials: "same-origin",
        signal: controller.signal,
      });

      if (response.status === 401 || response.status === 403) {
        // The session is gone. Reconnecting cannot fix it, so the session gate
        // owns the next attempt after a fresh login.
        controller?.abort();
        options.onAuthFailure?.();
        return;
      }
      if (!response.ok) throw new Error(`event stream status ${response.status}`);
      if (!response.body) throw new Error("event stream has no body");

      options.onOpen?.();
      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      const parse = createServerEventParser();

      for (;;) {
        const { done, value } = await reader.read();
        if (done) break;
        for (const message of parse(decoder.decode(value, { stream: true }))) {
          if (message.retry !== undefined && message.retry !== lastRetry) {
            lastRetry = message.retry;
            reconnectDelay = Math.min(
              Math.max(message.retry, MIN_RECONNECT_DELAY_MS),
              MAX_RECONNECT_DELAY_MS,
            );
          }
          options.onMessage(message);
        }
      }
    } catch {
      // A network error or a non-stream response: release any partial body and
      // retry after backoff.
      controller?.abort();
      if (cancelled) return;
    }

    if (cancelled) return;
    timer = setTimeout(() => void connect(), reconnectDelay);
  };

  void connect();
  return cancel;
}
