package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata"

	"qimeng.local/next/server/internal/httpapi"
	"qimeng.local/next/server/internal/service"
)

// setWebCacheHeaders 让“壳”不缓存、带哈希的静态资源长期缓存：
// 重建 Web 后 index.html 立刻换到新的 chunk 名，旧标签页不会拿着死链去请求。
func setWebCacheHeaders(w http.ResponseWriter, requestPath string) {
	if strings.HasPrefix(requestPath, "/assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
}

func main() {
	// Capture the redaction values before run(): it unsets the admin password.
	redactions := startupFailureRedactions(os.Getenv)
	if e := run(); e != nil {
		reason := "configuration, storage or listener failure"
		var op *net.OpError
		if errors.As(e, &op) && op.Op == "listen" {
			reason = "cannot bind TCP listener, check the configured address and whether the port is in use"
		}
		slog.Error("service stopped", "reason", reason)
		// The supervised Android host discards child output in release builds. A bounded,
		// redacted detail beside the startup receipt is the only way the failure banner can
		// distinguish a malformed catalog, another owner of the data directory or an
		// unfinished restore from a generic exit status 1.
		if writeErr := publishStartupFailure(os.Getenv("QIMENG_STARTUP_RECEIPT"), os.Getenv("QIMENG_LAUNCH_ID"), sanitizeStartupFailure(e.Error(), redactions)); writeErr != nil {
			slog.Warn("startup failure detail not published", "detail", "check the private runtime directory")
		}
		os.Exit(1)
	}
}
func run() error {
	if zone := os.Getenv("QIMENG_TIMEZONE"); zone != "" {
		location, e := time.LoadLocation(zone)
		if e != nil {
			return e
		}
		time.Local = location
	}
	var storages map[string]string
	if raw := os.Getenv("QIMENG_STORAGE_ROOTS"); raw != "" {
		if e := json.Unmarshal([]byte(raw), &storages); e != nil {
			return errors.New("QIMENG_STORAGE_ROOTS must be a JSON object of logical names to trusted root paths")
		}
	}
	var backupTargets map[string]service.BackupTarget
	if raw := os.Getenv("QIMENG_BACKUP_TARGETS"); raw != "" {
		if e := json.Unmarshal([]byte(raw), &backupTargets); e != nil {
			return errors.New("QIMENG_BACKUP_TARGETS must contain trusted backup target configuration")
		}
	}
	password := os.Getenv("QIMENG_ADMIN_PASSWORD")
	if e := os.Unsetenv("QIMENG_ADMIN_PASSWORD"); e != nil {
		return e
	}
	sidecarToken := os.Getenv("QIMENG_RESEARCH_SIDECAR_TOKEN")
	if e := os.Unsetenv("QIMENG_RESEARCH_SIDECAR_TOKEN"); e != nil {
		return e
	}
	localSync := service.LocalSyncConfig{Root: os.Getenv("QIMENG_LOCAL_SYNC_ROOT")}
	for key, field := range map[string]*time.Duration{"QIMENG_LOCAL_SYNC_INTERVAL": &localSync.Interval, "QIMENG_LOCAL_SYNC_STABLE_AGE": &localSync.StableAge} {
		if value := os.Getenv(key); value != "" {
			d, e := time.ParseDuration(value)
			if e != nil || d <= 0 {
				return errors.New("invalid local input folder timing")
			}
			*field = d
		}
	}
	scanner := service.LibraryMonitorConfig{WatchEnabled: true, PollInterval: 5 * time.Minute}
	researchEnabled := false
	if value := os.Getenv("QIMENG_RESEARCH_ENABLED"); value != "" {
		enabled, e := strconv.ParseBool(value)
		if e != nil {
			return errors.New("invalid public research setting")
		}
		researchEnabled = enabled
	}
	if value := os.Getenv("QIMENG_SCANNER_WATCH_ENABLED"); value != "" {
		on, e := strconv.ParseBool(value)
		if e != nil {
			return errors.New("invalid scanner watch setting")
		}
		scanner.WatchEnabled = on
	}
	if value := os.Getenv("QIMENG_SCANNER_POLL_INTERVAL"); value != "" {
		duration, e := time.ParseDuration(value)
		if e != nil {
			return errors.New("invalid scanner polling interval")
		}
		if duration > 0 {
			scanner.PollInterval = duration
		}
	}
	discovery, e := readDiscoveryConfig(os.Getenv)
	if e != nil {
		return e
	}
	tlsSetting := os.Getenv("QIMENG_EPHEMERAL_TLS")
	if tlsSetting != "" && tlsSetting != "true" && tlsSetting != "false" {
		return errors.New("QIMENG_EPHEMERAL_TLS must be true or false")
	}
	ephemeralTLS := tlsSetting == "true"
	if ephemeralTLS {
		discovery.Enabled = false
	}
	address := os.Getenv("QIMENG_LISTEN")
	if address == "" {
		address = "127.0.0.1:8421"
	}
	// 本机免密只对回环监听生效；远程、NAS 和反向代理仍必须使用密码或设备凭据。
	localAutoLogin := os.Getenv("QIMENG_LOCAL_WEB_NO_PASSWORD") == "true"
	if localAutoLogin {
		host, _, splitErr := net.SplitHostPort(address)
		ip := net.ParseIP(host)
		if splitErr != nil || ip == nil || !ip.IsLoopback() {
			return errors.New("QIMENG_LOCAL_WEB_NO_PASSWORD requires a numeric loopback listen address")
		}
	}
	// Keep the actual listener through migration so a bind failure cannot leave
	// an older installation with a database only the new binary can open.
	listener, e := openStartupListener(address, os.Getenv("QIMENG_STARTUP_RECEIPT"), os.Getenv("QIMENG_LAUNCH_ID"), ephemeralTLS)
	if e != nil {
		return e
	}
	defer listener.Close()
	s, e := service.New(service.Config{
		DataDir:              os.Getenv("QIMENG_DATA_DIR"),
		AdminToken:           os.Getenv("QIMENG_ADMIN_TOKEN"),
		AdminPassword:        password,
		AIToken:              os.Getenv("QIMENG_AI_TOKEN"),
		StorageRoots:         storages,
		BackupTargets:        backupTargets,
		DefaultBackupTarget:  os.Getenv("QIMENG_DEFAULT_BACKUP_TARGET"),
		SecureCookies:        os.Getenv("QIMENG_SECURE_COOKIES") == "true" || ephemeralTLS,
		LocalAutoLogin:       localAutoLogin,
		ResearchEnabled:      researchEnabled,
		ResearchLensEndpoint: os.Getenv("QIMENG_RESEARCH_LENS_ENDPOINT"),
		ResearchSidecarToken: sidecarToken,
		AIConfigured:         os.Getenv("QIMENG_AI_CONFIGURED") == "true",
		LocalSync:            localSync,
		Scanner:              &scanner,
	})
	password = ""
	if e != nil {
		return e
	}
	defer s.Close()
	api := httpapi.New(s)
	var handler http.Handler = api
	if web := os.Getenv("QIMENG_WEB_DIR"); web != "" {
		files := http.FileServer(http.Dir(web))
		packaged := loadWebEncodings(web)
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/healthz" || r.URL.Path == "/metrics" {
				api.ServeHTTP(w, r)
				return
			}
			if r.Method != "GET" && r.Method != "HEAD" {
				http.Error(w, "method not allowed", 405)
				return
			}
			local := filepath.Clean(filepath.FromSlash(strings.TrimPrefix(r.URL.Path, "/")))
			if local == ".." || strings.HasPrefix(local, ".."+string(os.PathSeparator)) || filepath.IsAbs(local) {
				http.Error(w, "invalid path", 400)
				return
			}
			if entry, ok := packaged[r.URL.Path]; ok {
				setWebCacheHeaders(w, r.URL.Path)
				if entry.serve(w, r, web) {
					return
				}
			}
			if _, e := os.Stat(filepath.Join(web, local)); errors.Is(e, os.ErrNotExist) {
				// 缺失的静态资源必须回 404，不能回退到 index.html：浏览器的动态 import
				// 拿到 HTML 会报 "Failed to fetch dynamically imported module"（重建 Web 后
				// 旧标签页请求已不存在的 chunk 名就是这种情况）。只对导航请求做 SPA 回退。
				if strings.HasPrefix(r.URL.Path, "/assets/") || filepath.Ext(local) != "" {
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Cache-Control", "no-cache")
				http.ServeFile(w, r, filepath.Join(web, "index.html"))
				return
			}
			setWebCacheHeaders(w, r.URL.Path)
			files.ServeHTTP(w, r)
		})
	}
	server := &http.Server{Addr: address, Handler: s.Traffic.Handler(handler), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 32 << 10}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	stopDiscovery, e := startDiscovery(listener.Addr(), s.InstanceID, discovery)
	if e != nil {
		slog.Warn("LAN discovery unavailable", "detail", "check configured interface and multicast support")
	} else {
		defer stopDiscovery()
	}
	if ctx.Err() != nil {
		return nil
	}
	if e := listener.publishReceipt(s.InstanceID); e != nil {
		return e
	}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	select {
	case e := <-done:
		if errors.Is(e, http.ErrServerClosed) {
			return nil
		}
		return e
	case <-ctx.Done():
		// End live event streams first: otherwise Shutdown waits for their
		// long-lived responses until the drain deadline expires.
		s.CloseEvents()
		shutdown, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		return server.Shutdown(shutdown)
	}
}
