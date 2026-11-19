import { createContext, useContext, useEffect, useRef, useState } from "react";
import type { ReactNode } from "react";
import { onlineManager, useMutation, useQuery } from "@tanstack/react-query";
import type { QueryKey, UseQueryResult } from "@tanstack/react-query";
import { api, ApiError, read } from "./client";
import type { Schema } from "./client";
import { queryClient } from "./query";
import { ErrorNotice, Loading } from "../components/Feedback";
import { LegacyLogin } from "../components/LegacyLogin";
import { ClientDiagnostics } from "./client-diagnostics";

const SessionContext = createContext<{
  instance: Schema<"Instance">;
  session: Schema<"Session">;
} | null>(null);
export function useSession() {
  const value = useContext(SessionContext);
  if (!value) throw new Error("Authenticated session is required");
  return value;
}
export function useRemoteKey() {
  const { instance, session } = useSession();
  return ["remote", instance.id, session.id] as const;
}

const instanceKey = ["instance"] as const;
const startupTimeoutMs = 10_000;

async function probe<T>(querySignal: AbortSignal, get: (signal: AbortSignal) => Promise<T>) {
  querySignal.throwIfAborted();
  const request = new AbortController();
  const cancel = () => request.abort(querySignal.reason);
  querySignal.addEventListener("abort", cancel, { once: true });
  const timer = window.setTimeout(() => {
    request.abort(new ApiError(0, "startup_timeout", "等待影库响应超时。请确认服务可用后重试。"));
  }, startupTimeoutMs);
  try {
    return await get(request.signal);
  } catch (error) {
    // The SDK may report a fetch or body-read failure when the deadline aborts it.
    request.signal.throwIfAborted();
    throw error;
  } finally {
    window.clearTimeout(timer);
    querySignal.removeEventListener("abort", cancel);
  }
}

async function currentSession(signal: AbortSignal) {
  try {
    return await read(api.GET("/sessions/current", { signal }));
  } catch (error) {
    // A confirmed sign-out must replace any previously successful session in this tab.
    if (error instanceof ApiError && error.status === 401) return null;
    throw error;
  }
}

async function restartProbe(queryKey: QueryKey) {
  const active = { queryKey, exact: true, type: "active" } as const;
  // With no prior data, refetch alone would join the old initial request.
  await queryClient.cancelQueries(active);
  await queryClient.refetchQueries(active);
}

function ConnectionCheck({ query, queryKey, text }: {
  query: UseQueryResult<unknown>;
  queryKey: QueryKey;
  text: string;
}) {
  useEffect(() => {
    // A document opened offline may never have seen the offline event.
    // Its query manager already considers itself online when the native event arrives.
    const reconnect = () => { void restartProbe(queryKey); };
    window.addEventListener("online", reconnect);
    return () => window.removeEventListener("online", reconnect);
  }, [queryKey]);
  if (query.fetchStatus === "paused") {
    const reason = onlineManager.isOnline() ? "连接检查已暂停。" : "浏览器报告离线，连接检查已暂停。";
    const error = new Error(query.failureReason ? `${reason} ${query.failureReason.message}` : reason);
    return <ErrorNotice error={error} retry={() => void restartProbe(queryKey)} />;
  }
  if (query.isPending || query.isFetching) return <Loading text={text} />;
  return <ErrorNotice error={query.error} retry={() => void query.refetch()} />;
}

export function SessionGate({ children }: { children: ReactNode }) {
  const instance = useQuery({
    queryKey: instanceKey,
    queryFn: ({ signal }) => probe(signal, request => read(api.GET("/instance", { signal: request }))),
    networkMode: "offlineFirst",
    refetchOnReconnect: query => query.state.data !== undefined,
    refetchInterval: 60_000,
  });
  const sessionKey = ["session", instance.data?.id] as const;
  const session = useQuery({
    queryKey: sessionKey,
    queryFn: ({ signal }) => probe(signal, currentSession),
    networkMode: "offlineFirst",
    refetchOnReconnect: query => query.state.data !== undefined,
    enabled: !!instance.data,
    gcTime: 0,
    retry: false,
    staleTime: 0,
  });
  const previousScope = useRef<readonly string[] | undefined>(undefined);
  useEffect(() => {
    const expired = () => {
      void session.refetch();
    };
    window.addEventListener("qimeng-session-expired", expired);
    return () => window.removeEventListener("qimeng-session-expired", expired);
  }, [session.refetch]);
  useEffect(() => {
    const old = previousScope.current;
    const current = instance.data && session.data ? ["remote", instance.data.id, session.data.id] : undefined;
    if (old && (!current || old[1] !== current[1] || old[2] !== current[2])) {
      void queryClient.cancelQueries({ queryKey: old });
      queryClient.removeQueries({ queryKey: old });
    }
    previousScope.current = current;
  }, [instance.data?.id, session.data?.id]);
  if (!instance.data)
    return (
      <div className="connection-screen">
        <img src="/icons/icon-192.png" width="64" height="64" alt="" />
        <h1>绮梦影库</h1>
        <ConnectionCheck query={instance} queryKey={instanceKey} text="正在连接影库…" />
      </div>
    );
  if (instance.data.apiVersion !== "0.1.0")
    return (
      <div className="connection-screen">
        <h1>需要更新客户端</h1>
        <p>当前服务器协议 {instance.data.apiVersion} 与此客户端不兼容。</p>
      </div>
    );
  if (session.data === null)
    return <LoginForm verify={session.refetch} connectionError={session.error} />;
  if (!session.data) {
    return (
      <div className="connection-screen">
        <ConnectionCheck query={session} queryKey={sessionKey} text="正在确认设备会话…" />
      </div>
    );
  }
  return (
    <SessionContext.Provider
      key={JSON.stringify([instance.data.id, session.data.id])}
      value={{ instance: instance.data, session: session.data }}
    >
      <ClientDiagnostics />
      {instance.isError && (
        <div className="connection-warning" role="status">
          连接检查失败，当前内容可能已过期。
        </div>
      )}
      {children}
    </SessionContext.Provider>
  );
}

function LoginForm({ verify, connectionError }: { verify: () => Promise<unknown>; connectionError: Error | null }) {
  const [password, setPassword] = useState("");
  const login = useMutation({
    gcTime: 0,
    mutationFn: async () => {
      // The browser only needs the HttpOnly cookie. Do not put a returned token in mutation data.
      await read(
        api.POST("/sessions", { body: { password, label: "Web 浏览器" } }),
      );
    },
    onSuccess: async () => {
      setPassword("");
      await verify();
    },
  });
  return (
    <LegacyLogin
      password={password}
      setPassword={setPassword}
      busy={login.isPending}
      error={login.error}
      connectionNotice={connectionError && <ErrorNotice error={connectionError} retry={() => void verify()} />}
      submit={() => {
        if (!login.isPending && password) login.mutate();
      }}
    />
  );
}
export function useLogout() {
  return useMutation({
    mutationFn: () => read(api.DELETE("/sessions/current")),
    onSuccess: () => {
      queryClient.clear();
      window.location.assign("/");
    },
  });
}
