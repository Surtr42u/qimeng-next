import { createContext, useContext, useEffect, useRef, useState } from 'react';
import type { FormEvent, ReactNode } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { api, ApiError, read } from './client';
import type { Schema } from './client';
import { queryClient } from './query';
import { ErrorNotice, Loading } from '../components/Feedback';

const SessionContext = createContext<{ instance: Schema<'Instance'>; session: Schema<'Session'> } | null>(null);
export function useSession() {
  const value = useContext(SessionContext);
  if (!value) throw new Error('Authenticated session is required');
  return value;
}
export function useRemoteKey() { const { instance, session } = useSession(); return ['remote', instance.id, session.id] as const; }

export function SessionGate({ children }: { children: ReactNode }) {
  const instance = useQuery({ queryKey: ['instance'], queryFn: ({ signal }) => read(api.GET('/instance', { signal })), refetchInterval: 60_000 });
  const session = useQuery({ queryKey: ['session', instance.data?.id], queryFn: ({ signal }) => read(api.GET('/sessions/current', { signal })), enabled: !!instance.data, retry: false, staleTime: 0 });
  const previousId = useRef<string | undefined>(undefined);
  useEffect(() => {
    const expired = () => { void session.refetch(); };
    window.addEventListener('qimeng-session-expired', expired);
    return () => window.removeEventListener('qimeng-session-expired', expired);
  }, [session.refetch]);
  useEffect(() => {
    if (previousId.current && instance.data?.id !== previousId.current) {
      void queryClient.cancelQueries({ queryKey: ['remote'] });
      queryClient.removeQueries({ queryKey: ['remote'] });
    }
    previousId.current = instance.data?.id;
  }, [instance.data?.id]);
  if (!instance.data) return <div className="connection-screen"><img src="/icon.svg" width="64" height="64" alt="" /><h1>绮梦影库</h1>{instance.isPending ? <Loading text="正在连接影库…" /> : <ErrorNotice error={instance.error} retry={() => void instance.refetch()} />}</div>;
  if (instance.data.apiVersion !== '0.1.0') return <div className="connection-screen"><h1>需要更新客户端</h1><p>当前服务器协议 {instance.data.apiVersion} 与此客户端不兼容。</p></div>;
  if (session.isPending) return <div className="connection-screen"><Loading text="正在确认设备会话…" /></div>;
  if (session.error instanceof ApiError && session.error.status === 401) return <LoginForm instanceName={instance.data.name} onSuccess={() => void session.refetch()} />;
  if (!session.data) {
    return <div className="connection-screen"><ErrorNotice error={session.error} retry={() => void session.refetch()} /></div>;
  }
  return <SessionContext.Provider value={{ instance: instance.data, session: session.data }}>{instance.isError && <div className="connection-warning" role="status">连接检查失败，当前内容可能已过期。</div>}{children}</SessionContext.Provider>;
}

function LoginForm({ instanceName, onSuccess }: { instanceName: string; onSuccess: () => void }) {
  const [token, setToken] = useState('');
  const login = useMutation({ mutationFn: () => read(api.POST('/sessions', { body: { token, label: 'Web 浏览器' } })), onSuccess: () => { setToken(''); onSuccess(); } });
  const submit = (event: FormEvent) => { event.preventDefault(); if (!login.isPending && token) login.mutate(); };
  return <div className="connection-screen"><form className="login-card qm-glass" onSubmit={submit}><img src="/icon.svg" width="64" height="64" alt="" /><span className="eyebrow">你的私人媒体空间</span><h1>连接绮梦影库</h1><p>{instanceName}</p><label>访问令牌<input type="password" autoComplete="current-password" value={token} onChange={event => setToken(event.target.value)} required autoFocus /></label><ErrorNotice error={login.error} /><button className="button button--primary" disabled={login.isPending || !token}>{login.isPending ? '正在验证…' : '连接'}</button><p className="muted">使用服务器配置的访问令牌连接此设备。</p></form></div>;
}

export function useLogout() {
  return useMutation({ mutationFn: () => read(api.DELETE('/sessions/current')), onSuccess: () => { queryClient.clear(); window.location.assign('/'); } });
}
