import { useEffect, useRef, useState } from "react";
import {
  Link,
  NavLink,
  Outlet,
  useLocation,
  useNavigate,
  useNavigationType,
  useRouteError,
  useSearchParams,
} from "react-router-dom";
import { GlassOptics, SegmentedControl } from "@ui/GlassNavigation";
import { RouteSurface } from "@ui/RouteSurface";
import { SelectionLens } from "@ui/SelectionLens";
import { SessionGate } from "../data/session";
import { UploadProvider } from "../data/upload";
import { EventsProvider, EventSyncStatus } from "../data/events";
import { useRefreshLibrary } from "../data/refresh";
import { usePageRestoration, UnsavedProvider } from "../lib/navigation";
import { setTheme } from "../lib/preferences";
import { LegacySearchBox } from "../components/LegacySearchBox";
import {
  BackIcon,
  HomeIcon,
  AlbumsIcon,
  MineIcon,
  DataIcon,
  MoonIcon,
  WrenchIcon,
  SettingsIcon,
  WinMinIcon,
  WinMaxIcon,
  WinCloseIcon,
  RefreshIcon,
  BackTopIcon,
} from "../components/LegacyShellIcons";

// Presentation follows the existing Web shell, reused by user authorization on 2026-11-17.
const navigation = [
  { label: "首页", href: "/", Icon: HomeIcon },
  { label: "相册", href: "/album", Icon: AlbumsIcon },
  { label: "我的", href: "/mine", Icon: MineIcon },
  { label: "数据", href: "/data", Icon: DataIcon },
] as const;

export function App() {
  return (
    <SessionGate>
      <UnsavedProvider>
        <EventsProvider>
          <UploadProvider>
            <Shell />
          </UploadProvider>
        </EventsProvider>
      </UnsavedProvider>
    </SessionGate>
  );
}

function Shell() {
  const location = useLocation();
  const direction = useNavigationType();
  const navigate = useNavigate();
  const [params, setParams] = useSearchParams();
  const refreshLibrary = useRefreshLibrary();
  usePageRestoration();
  const content = useRef<HTMLElement>(null);
  const rail = useRef<HTMLElement>(null);
  const knownKeys = useRef<string[]>([]);
  const currentIndex = useRef(0);
  const [online, setOnline] = useState(navigator.onLine);
  const [showBackTop, setShowBackTop] = useState(false);
  const [spinning, setSpinning] = useState(false);
  const [themeError, setThemeError] = useState(false);
  const home = location.pathname === "/";
  const tab =
    params.get("tab") === "rank" ? "hot" : (params.get("tab") ?? "recommend");
  const period = params.get("period") ?? "day";
  const detail = /^\/asset\/[^/]+$/.test(location.pathname);

  useEffect(() => {
    const known = knownKeys.current.indexOf(location.key);
    if (known >= 0) currentIndex.current = known;
    else if (direction === "REPLACE" && knownKeys.current.length)
      knownKeys.current[currentIndex.current] = location.key;
    else {
      knownKeys.current = [
        ...knownKeys.current.slice(0, currentIndex.current + 1),
        location.key,
      ].slice(-64);
      currentIndex.current = knownKeys.current.length - 1;
    }
  }, [location.key, location.search, direction]);
  useEffect(() => {
    const update = () => setOnline(navigator.onLine);
    window.addEventListener("online", update);
    window.addEventListener("offline", update);
    return () => {
      window.removeEventListener("online", update);
      window.removeEventListener("offline", update);
    };
  }, []);
  const changeTab = (value: string) =>
    setParams(
      (current) => {
        const next = new URLSearchParams(current);
        value === "recommend" ? next.delete("tab") : next.set("tab", value);
        if (value !== "hot") next.delete("period");
        return next;
      },
      { replace: true },
    );
  const refresh = () => {
    if (home && tab !== "hot")
      setParams(
        (current) => {
          const next = new URLSearchParams(current);
          next.set(
            "seed",
            String(crypto.getRandomValues(new Uint32Array(1))[0]),
          );
          return next;
        },
        { replace: true },
      );
    else void refreshLibrary(home && tab === "hot");
    setSpinning(true);
  };
  return (
    <div className="layout">
      <a className="skip-link" href="#main-content">
        跳到内容
      </a>
      <GlassOptics />
      <aside className="sidebar">
        <button
          className="sidebar--back"
          title={home ? "已在首页" : "返回"}
          aria-label="返回"
          type="button"
          disabled={home}
          onClick={() =>
            currentIndex.current > 0 ? navigate(-1) : navigate("/")
          }
        >
          <BackIcon />
        </button>
        <nav
          className="sidebar--pages qm-selection-host"
          aria-label="主导航"
          ref={rail}
        >
          <SelectionLens
            host={rail}
            selected='[aria-current="page"]'
            selectionKey={location.pathname}
          />
          {navigation.map((item) => (
            <NavLink
              key={item.href}
              to={item.href}
              end
              className={({ isActive }) =>
                `nav-item${isActive ? " active" : ""}`
              }
            >
              <span className="nav-icon">
                <item.Icon />
              </span>
              <p>{item.label}</p>
            </NavLink>
          ))}
        </nav>
        <div className="sidebar--settings">
          <button
            className="settings-item"
            type="button"
            title="明暗主题"
            aria-label="明暗主题"
            onClick={() =>
              setThemeError(
                !setTheme(
                  document.documentElement.dataset.theme === "dark"
                    ? "light"
                    : "dark",
                ),
              )
            }
          >
            <MoonIcon />
          </button>
          <Link
            className="settings-item"
            title="维护"
            aria-label="维护"
            to="/maintenance"
          >
            <WrenchIcon />
          </Link>
          <Link
            className="settings-item"
            title="设置"
            aria-label="设置"
            to="/settings"
          >
            <SettingsIcon />
          </Link>
        </div>
      </aside>
      <div className="main">
        <header className="header" data-home={home ? "" : undefined}>
          <div className="header--left">
            {home && (
              <SegmentedControl
                label="首页内容"
                value={tab}
                options={[
                  { value: "recommend", label: "推荐" },
                  { value: "cos", label: "COS" },
                  { value: "hot", label: "排行榜" },
                ]}
                onChange={changeTab}
              />
            )}
          </div>
          <LegacySearchBox />
          <div className="header--right">
            <div className="win-controls" aria-hidden="true">
              <span className="win-btn">
                <WinMinIcon />
              </span>
              <span className="win-btn">
                <WinMaxIcon />
              </span>
              <span className="win-btn">
                <WinCloseIcon />
              </span>
            </div>
          </div>
        </header>
        {home && tab === "hot" && (
          <div className="rank-panel">
            <div className="rank-tabs">
              {[
                { value: "day", label: "日榜" },
                { value: "month", label: "月榜" },
                { value: "week", label: "周榜" },
                { value: "year", label: "年榜" },
              ].map((item) => (
                <button
                  type="button"
                  className={`rank-tab${period === item.value ? " active" : ""}`}
                  aria-pressed={period === item.value}
                  key={item.value}
                  onClick={() =>
                    setParams(
                      (current) => {
                        const next = new URLSearchParams(current);
                        next.set("period", item.value);
                        return next;
                      },
                      { replace: true },
                    )
                  }
                >
                  {item.label}
                </button>
              ))}
            </div>
          </div>
        )}
        {!online && (
          <div className="connection-warning" role="status">
            网络已断开。当前可能显示缓存内容，管理操作需要恢复连接。
          </div>
        )}
        {themeError && (
          <div className="connection-warning" role="status">
            主题已切换，但浏览器无法保存偏好。
          </div>
        )}
        <EventSyncStatus />
        <main
          id="main-content"
          className="content"
          data-scroll-root
          ref={content}
          onScroll={(event) =>
            setShowBackTop(event.currentTarget.scrollTop > 400)
          }
        >
          <RouteSurface transitionKey={location.pathname}>
            <Outlet />
          </RouteSurface>
        </main>
      </div>
      {!detail && (
        <button
          className={`refresh-fab${spinning ? " spinning" : ""}`}
          title="刷新"
          aria-label="刷新"
          type="button"
          onClick={refresh}
          onAnimationEnd={() => setSpinning(false)}
        >
          <RefreshIcon />
        </button>
      )}
      <button
        className={`backtop-fab${showBackTop && !detail ? " shown" : ""}`}
        title="回到顶部"
        type="button"
        tabIndex={showBackTop && !detail ? 0 : -1}
        onClick={() =>
          content.current?.scrollTo({
            top: 0,
            behavior: matchMedia("(prefers-reduced-motion: reduce)").matches
              ? "instant"
              : "smooth",
          })
        }
      >
        <BackTopIcon />
        <p>顶部</p>
      </button>
    </div>
  );
}

export function RouteError() {
  const error = useRouteError();
  return (
    <div className="connection-screen">
      <h1>页面暂时无法打开</h1>
      <p>{error instanceof Error ? error.message : "请返回首页重试。"}</p>
      <a className="button" href="/">
        返回首页
      </a>
    </div>
  );
}
