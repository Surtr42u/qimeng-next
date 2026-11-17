import { useEffect, useRef, useState } from 'react';
import { Link, NavLink, Outlet, useLocation, useNavigate, useNavigationType, useRouteError } from 'react-router-dom';
import { GlassNavigation, GlassOptics } from '@ui/GlassNavigation';
import { RouteSurface } from '@ui/RouteSurface';
import { SessionGate } from '../data/session';
import { UploadProvider } from '../data/upload';
import { EventsProvider, EventSyncStatus } from '../data/events';
import { Icon } from '../components/Icon';
import type { IconName } from '../components/Icon';
import { usePageRestoration } from '../lib/navigation';

const mainLinks: { id: string; label: string; href: string; icon: IconName }[] = [
  { id: 'home', label: '首页', href: '/', icon: 'home' },
  { id: 'album', label: '相册', href: '/album', icon: 'album' },
  { id: 'mine', label: '我的', href: '/mine', icon: 'mine' },
  { id: 'data', label: '数据', href: '/data', icon: 'chart' },
  { id: 'manage', label: '数据管理', href: '/manage', icon: 'manage' },
];
const navItems = mainLinks.map(item => ({ ...item, icon: <Icon name={item.icon} /> }));
export function App() { return <SessionGate><EventsProvider><UploadProvider><Shell /></UploadProvider></EventsProvider></SessionGate>; }
function Shell() {
  const location = useLocation();
  const direction = useNavigationType();
  const navigate = useNavigate();
  usePageRestoration();
  const [online, setOnline] = useState(navigator.onLine);
  const [query, setQuery] = useState(new URLSearchParams(location.search).get('q') ?? '');
  const search = useRef<HTMLInputElement>(null);
  const historyKeys = useRef<string[]>([]);
  const historyIndex = useRef(0);
  useEffect(() => {
    const known = historyKeys.current.indexOf(location.key);
    if (known >= 0) historyIndex.current = known;
    else if (direction === 'REPLACE' && historyKeys.current.length) historyKeys.current[historyIndex.current] = location.key;
    else { historyKeys.current = [...historyKeys.current.slice(0, historyIndex.current + 1), location.key].slice(-64); historyIndex.current = historyKeys.current.length - 1; }
    setQuery(new URLSearchParams(location.search).get('q') ?? '');
  }, [location.key, location.search, direction]);
  useEffect(() => {
    const update = () => setOnline(navigator.onLine); window.addEventListener('online', update); window.addEventListener('offline', update);
    return () => { window.removeEventListener('online', update); window.removeEventListener('offline', update); };
  }, []);
  const activeId = location.pathname === '/' ? 'home' : location.pathname.startsWith('/album') || location.pathname.startsWith('/asset/') ? 'album' : location.pathname.startsWith('/mine') ? 'mine' : location.pathname.startsWith('/data') ? 'data' : 'manage';
  return <div className="app-shell"><a className="skip-link" href="#main-content">跳到内容</a><GlassOptics />
    <aside className="sidebar"><Link to="/" className="brand"><img src="/icon.svg" width="38" height="38" alt="" /><div><strong>绮梦影库</strong><span>你喜欢的，都在这里</span></div></Link><nav aria-label="桌面主导航">{mainLinks.map(item => <NavLink key={item.id} to={item.href} end={item.href === '/'} className={({ isActive }) => `sidebar-link ${isActive ? 'is-active' : ''}`}><Icon name={item.icon} /><span>{item.label}</span></NavLink>)}</nav><div className="sidebar-bottom"><NavLink to="/maintenance" className="sidebar-link"><Icon name="server" /><span>服务与维护</span></NavLink><NavLink to="/settings" className="sidebar-link"><Icon name="settings" /><span>设置</span></NavLink><button className="sidebar-link" disabled={location.pathname === '/'} onClick={() => historyIndex.current > 0 ? navigate(-1) : navigate('/')}><Icon name="back" /><span>返回</span></button></div></aside>
    <div className="workspace"><header className="topbar"><Link to="/" className="mobile-brand"><img src="/icon.svg" width="30" height="30" alt="绮梦影库" /></Link><form className="global-search" role="search" onSubmit={event => { event.preventDefault(); if (query.trim()) navigate(`/album?q=${encodeURIComponent(query.trim())}`); }}><Icon name="search" size={19} /><input ref={search} value={query} onChange={event => setQuery(event.target.value)} placeholder="搜索文件、作者或标签" aria-label="搜索媒体" maxLength={200} />{query && <button type="button" aria-label="清空搜索" onClick={() => { setQuery(''); search.current?.focus(); }}><Icon name="close" size={16} /></button>}<button type="submit" className="search-submit">搜索</button></form><Link to="/manage/upload" className="button button--small topbar-upload"><Icon name="upload" size={17} /><span>上传</span></Link><Link className="icon-button" to="/settings" aria-label="设置"><Icon name="settings" size={20} /></Link></header>
    {!online && <div className="connection-warning" role="status">网络已断开。当前可能显示缓存内容，管理操作需要恢复连接。</div>}
    <EventSyncStatus />
    <main id="main-content" className="page-content"><RouteSurface transitionKey={location.pathname}><Outlet /></RouteSurface></main>
    </div><div className="mobile-navigation"><GlassNavigation items={navItems} activeId={activeId} onNavigate={href => navigate(href)} /></div>
  </div>;
}
export function RouteError() {
  const error = useRouteError();
  return <div className="connection-screen"><h1>页面暂时无法打开</h1><p>{error instanceof Error ? error.message : '请返回首页重试。'}</p><a className="button" href="/">返回首页</a></div>;
}
