import { createContext, useContext, useEffect, useRef, useState } from "react";
import type { ReactNode } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { api, ApiError, read } from "./client";
import type { Schema } from "./client";
import { queryClient } from "./query";
import { ErrorNotice, Loading } from "../components/Feedback";
import { LegacyLogin } from "../components/LegacyLogin";

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

export function SessionGate({ children }: { children: ReactNode }) {
  const instance = useQuery({
    queryKey: ["instance"],
    queryFn: ({ signal }) => read(api.GET("/instance", { signal })),
    refetchInterval: 60_000,
  });
  const session = useQuery({
    queryKey: ["session", instance.data?.id],
    queryFn: ({ signal }) => read(api.GET("/sessions/current", { signal })),
    enabled: !!instance.data,
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
    const authenticated = !(session.error instanceof ApiError && session.error.status === 401);
    const current = instance.data && session.data && authenticated ? ["remote", instance.data.id, session.data.id] : undefined;
    if (old && (!current || old[1] !== current[1] || old[2] !== current[2])) {
      void queryClient.cancelQueries({ queryKey: old });
      queryClient.removeQueries({ queryKey: old });
    }
    previousScope.current = current;
  }, [instance.data?.id, session.data?.id, session.error]);
  if (!instance.data)
    return (
      <div className="connection-screen">
        <img src="/icon.svg" width="64" height="64" alt="" />
        <h1>绮梦影库</h1>
        {instance.isPending ? (
          <Loading text="正在连接影库…" />
        ) : (
          <ErrorNotice
            error={instance.error}
            retry={() => void instance.refetch()}
          />
        )}
      </div>
    );
  if (instance.data.apiVersion !== "0.1.0")
    return (
      <div className="connection-screen">
        <h1>需要更新客户端</h1>
        <p>当前服务器协议 {instance.data.apiVersion} 与此客户端不兼容。</p>
      </div>
    );
  if (session.isPending)
    return (
      <div className="connection-screen">
        <Loading text="正在确认设备会话…" />
      </div>
    );
  if (session.error instanceof ApiError && session.error.status === 401)
    return <LoginForm onSuccess={() => void session.refetch()} />;
  if (!session.data) {
    return (
      <div className="connection-screen">
        <ErrorNotice
          error={session.error}
          retry={() => void session.refetch()}
        />
      </div>
    );
  }
  return (
    <SessionContext.Provider
      key={JSON.stringify([instance.data.id, session.data.id])}
      value={{ instance: instance.data, session: session.data }}
    >
      {instance.isError && (
        <div className="connection-warning" role="status">
          连接检查失败，当前内容可能已过期。
        </div>
      )}
      {children}
    </SessionContext.Provider>
  );
}

function LoginForm({ onSuccess }: { onSuccess: () => void }) {
  const [password, setPassword] = useState("");
  const login = useMutation({
    gcTime: 0,
    mutationFn: async () => {
      // The browser only needs the HttpOnly cookie. Do not put a returned token in mutation data.
      await read(
        api.POST("/sessions", { body: { password, label: "Web 浏览器" } }),
      );
    },
    onSuccess: () => {
      setPassword("");
      onSuccess();
    },
  });
  return (
    <LegacyLogin
      password={password}
      setPassword={setPassword}
      busy={login.isPending}
      error={login.error}
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
