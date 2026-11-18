import createClient from "openapi-fetch";
import type { paths, components } from "../api/generated/schema";
import { newId } from "../lib/id";
import { ApiError } from "../lib/errors";
import { reportClientProblem } from "../lib/client-problems";
export { ApiError } from "../lib/errors";

export type Schema<K extends keyof components["schemas"]> =
  components["schemas"][K];
export const api = createClient<paths>({
  baseUrl: `${window.location.origin}/api/v1`,
  credentials: "same-origin",
});

api.use({
  onResponse({ response, request, schemaPath }) {
    if (response.status === 401 && !response.url.includes("/sessions"))
      window.dispatchEvent(new Event("qimeng-session-expired"));
    if (response.status >= 500 && !request.signal.aborted && schemaPath !== "/system/client-errors" && !schemaPath.startsWith("/sessions"))
      reportClientProblem("request_failed", response.status);
    return response;
  },
  onError({ request, schemaPath }) {
    if (!request.signal.aborted && schemaPath !== "/system/client-errors" && !schemaPath.startsWith("/sessions")) reportClientProblem("connection_lost");
  },
});

export async function read<T>(
  request: Promise<{ data?: T; error?: unknown; response: Response }>,
): Promise<T> {
  let result: Awaited<typeof request>;
  try {
    result = await request;
  } catch (error) {
    if (error instanceof DOMException && error.name === "AbortError")
      throw error;
    throw new ApiError(
      0,
      "network_unavailable",
      "无法连接影库。请检查网络与服务器；本次操作没有收到成功确认。",
    );
  }
  if (!result.response.ok) {
    const body = result.error as
      { code?: string; message?: string; currentRevision?: number; details?: Record<string, unknown> } | undefined;
    throw new ApiError(
      result.response.status,
      body?.code ?? "http_error",
      body?.message ?? `服务器返回 ${result.response.status}`,
      body?.currentRevision,
      body?.details,
    );
  }
  return result.data as T;
}

export const operationHeaders = (id = newId()) => ({ "Idempotency-Key": id });
