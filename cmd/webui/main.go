package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"runtime/debug"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/auth"
	"github.com/denysvitali/pictures-sync-s3/pkg/daemoncontrol"
	"github.com/denysvitali/pictures-sync-s3/pkg/dmesg"
	"github.com/denysvitali/pictures-sync-s3/pkg/events"
	"github.com/denysvitali/pictures-sync-s3/pkg/googlephotos"
	"github.com/denysvitali/pictures-sync-s3/pkg/handlers"
	"github.com/denysvitali/pictures-sync-s3/pkg/metrics"
	"github.com/denysvitali/pictures-sync-s3/pkg/middleware"
	"github.com/denysvitali/pictures-sync-s3/pkg/ntpsync"
	"github.com/denysvitali/pictures-sync-s3/pkg/ota"
	"github.com/denysvitali/pictures-sync-s3/pkg/paniclog"
	"github.com/denysvitali/pictures-sync-s3/pkg/ratelimit"
	"github.com/denysvitali/pictures-sync-s3/pkg/settings"
	"github.com/denysvitali/pictures-sync-s3/pkg/ssrf"
	"github.com/denysvitali/pictures-sync-s3/pkg/state"
	"github.com/denysvitali/pictures-sync-s3/pkg/syncmanager"
	"github.com/denysvitali/pictures-sync-s3/pkg/systeminfo"
	"github.com/denysvitali/pictures-sync-s3/pkg/tlsconfig"
	"github.com/denysvitali/pictures-sync-s3/pkg/websocket"
	"github.com/denysvitali/pictures-sync-s3/pkg/webui"
	"github.com/denysvitali/pictures-sync-s3/pkg/wifimanager"
)

// defaultAllowedOrigins is intentionally empty: same-origin requests don't
// need CORS at all, and a wildcard is incompatible with allowCredentials=true.
// Operators can opt in to additional origins via WEBUI_ALLOWED_ORIGINS.
const defaultAllowedOrigins = ""

// processStartTime is recorded once at init time so health endpoints can
// compute uptime without depending on any external state.
var processStartTime = time.Now()

// buildVersion returns the VCS revision embedded by the Go toolchain, or
// "unknown" when the binary was not built from a VCS checkout.
func buildVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" && s.Value != "" {
				if len(s.Value) > 12 {
					return s.Value[:12]
				}
				return s.Value
			}
		}
	}
	return "unknown"
}

// handleHealthz is a minimal liveness probe. It never touches disk, network,
// or settings — it only confirms the process is alive and reports uptime.
// No authentication required.
func handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	uptime := time.Since(processStartTime).Seconds()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"alive":true,"uptime_seconds":%s,"version":%q}`,
		formatSeconds(uptime), buildVersion())
}

// formatSeconds formats a float64 second value with at most 3 decimal places
// and no trailing zeros, suitable for JSON embedding without quotes.
func formatSeconds(s float64) string {
	if s == math.Trunc(s) {
		return fmt.Sprintf("%.0f", s)
	}
	return fmt.Sprintf("%.3f", s)
}

// permPath is the persistent storage directory used by the appliance.
const permPath = "/perm"

// handleReadyz is a readiness probe that checks real infrastructure state.
// Returns 200 when all subsystems are ready, 503 otherwise.
// No authentication required.
//
// The stateMgr parameter is optional (may be nil during tests); when nil,
// sd_card_detected is always reported as false.
func makeReadyzHandler(stateMgr *state.Manager, appSettings *settings.Settings) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		// 1. Check whether /perm is writable by creating a temp file.
		permWritable := false
		if f, err := os.CreateTemp(permPath, ".readyz-probe-*"); err == nil {
			_ = f.Close()
			_ = os.Remove(f.Name())
			permWritable = true
		}

		// 2. Check whether an SD card is currently detected via the state manager.
		sdCardDetected := false
		if stateMgr != nil {
			s := stateMgr.GetState()
			sdCardDetected = s.SDCardMounted
		}

		// 3. Settings loaded — we already hold the pointer if Load() succeeded.
		settingsLoaded := appSettings != nil

		ready := permWritable && settingsLoaded

		w.Header().Set("Content-Type", "application/json")
		if ready {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		fmt.Fprintf(w, `{"ready":%t,"perm_writable":%t,"sd_card_detected":%t,"settings_loaded":%t}`,
			ready, permWritable, sdCardDetected, settingsLoaded)
	}
}

const apiRequestTimeout = 30 * time.Second

// logConfiguredWiFiNetworks logs WiFi networks from both gokrazy and app config files
func logConfiguredWiFiNetworks(wifiMgr *wifimanager.Manager) {
	// Log networks from our app's config (/perm/extra-wifi.json)
	appNetworks := wifiMgr.GetNetworks()
	log.Printf("WiFi networks in /perm/extra-wifi.json: %d", len(appNetworks))
	for i, net := range appNetworks {
		log.Printf("  [%d] SSID: %s (has password: %v)", i+1, net.SSID, net.PSK != "")
	}

	// Log networks from gokrazy's WiFi config (/perm/wifi.json)
	type GokrazyNetwork struct {
		SSID string `json:"ssid"`
		PSK  string `json:"psk"`
	}
	type GokrazyWiFiConfig struct {
		Networks []GokrazyNetwork `json:"networks"`
	}

	gokrazyConfigPath := "/perm/wifi.json"
	if data, err := os.ReadFile(gokrazyConfigPath); err == nil {
		var config GokrazyWiFiConfig
		if err := json.Unmarshal(data, &config); err == nil {
			log.Printf("WiFi networks in %s: %d", gokrazyConfigPath, len(config.Networks))
			for i, net := range config.Networks {
				log.Printf("  [%d] SSID: %s (has password: %v)", i+1, net.SSID, net.PSK != "")
			}
		} else {
			log.Printf("WiFi networks in %s: failed to parse (%v)", gokrazyConfigPath, err)
		}
	} else if !os.IsNotExist(err) {
		log.Printf("WiFi networks in %s: failed to read (%v)", gokrazyConfigPath, err)
	} else {
		log.Printf("WiFi networks in %s: 0 (file does not exist)", gokrazyConfigPath)
	}
}

func parseAllowedOrigins(raw string) []string {
	origins := make(map[string]struct{})
	for _, part := range strings.Split(raw, ",") {
		normalized := strings.ToLower(strings.TrimSpace(part))
		if normalized == "" {
			continue
		}

		if strings.HasPrefix(normalized, "http://") || strings.HasPrefix(normalized, "https://") {
			u, err := url.Parse(normalized)
			if err != nil {
				continue
			}
			normalized = u.Host
		}

		normalized = strings.TrimSuffix(normalized, "/")
		if normalized != "" {
			origins[normalized] = struct{}{}
		}
	}

	items := make([]string, 0, len(origins))
	for origin := range origins {
		items = append(items, origin)
	}
	sort.Strings(items)
	return items
}

func logAllowedOrigins(origins []string) {
	if len(origins) == 0 {
		log.Println("No web UI CORS/WS override origins configured")
		return
	}
	log.Printf("Allowing browser UI origins: %v", strings.Join(origins, ", "))
}

func configuredAllowedOrigins() []string {
	return parseAllowedOrigins(defaultAllowedOrigins + "," + os.Getenv("WEBUI_ALLOWED_ORIGINS"))
}

func repairClockAndPersistentCertificateBeforeTLS() {
	if ntpsync.IsClockSane() {
		log.Println("System clock already valid; skipping userland NTP sync")
	} else if err := ntpsync.EnsureTimeSync(1); err != nil {
		log.Printf("Warning: NTP time sync before TLS setup failed: %v", err)
	} else {
		log.Println("System time synchronized before TLS setup")
	}

	if _, err := os.Stat("/perm"); err != nil {
		log.Printf("Skipping persistent TLS certificate repair because /perm is not available: %v", err)
		return
	}

	info, regenerated, err := tlsconfig.EnsurePersistentSelfSignedCertificate(nil)
	if err != nil {
		log.Printf("Warning: Failed to repair persistent TLS certificate: %v", err)
		return
	}
	if regenerated {
		log.Printf("Persistent TLS certificate generated at %s", info.CertFile)
	}
}

func registerAPIRoutes(
	mux *http.ServeMux,
	ctx *handlers.Context,
	passwordProvider auth.PasswordProvider,
	stateMgr *state.Manager,
	eventMgr *events.Manager,
	dmesgMgr *dmesg.Manager,
	otaMgr *ota.Manager,
) {
	routes := []struct {
		path    string
		handler http.HandlerFunc
	}{
		{"/api/ws-token", handlers.WSTokenHandler(passwordProvider)},
		{"/api/version", ctx.HandleVersion},
		{"/api/status", ctx.HandleStatus},
		{"/api/history", ctx.HandleHistory},
		{"/api/config", ctx.HandleConfig},
		{"/api/config/b2", ctx.HandleConfigB2},
		{"/api/config/b2/regions", ctx.HandleConfigB2Regions},
		{"/api/config/test", ctx.HandleConfigTest},
		{"/api/auth/password", ctx.HandlePasswordChange},
		{"/api/breakglass/authorized-keys", ctx.HandleBreakglassAuthorizedKeys},
		{"/api/settings", ctx.HandleSettings},
		{"/api/devices", ctx.HandleDevices},
		{"/api/devices/select", ctx.HandleDeviceSelect},
		{"/api/devices/format", ctx.HandleDeviceFormat},
		{"/api/devices/redetect", ctx.HandleDeviceRedetect},
		{"/api/sync/start", ctx.HandleSyncStart},
		{"/api/sync/cancel", ctx.HandleSyncCancel},
		{"/api/wifi/scan", ctx.HandleWiFiScan},
		{"/api/wifi/networks", ctx.HandleWiFiNetworks},
		{"/api/wifi/connect", ctx.HandleWiFiConnect},
		{"/api/wifi/disconnect", ctx.HandleWiFiDisconnect},
		{"/api/wifi/reorder", ctx.HandleWiFiReorder},
		{"/api/wifi/status", ctx.HandleWiFiStatus},
		{"/api/files/cards", ctx.HandleFileCards},
		{"/api/files", ctx.HandleFiles},
		{"/api/files/paginated", ctx.HandleFilesPaginated},
		{"/api/files/link", ctx.HandleFileLink},
		{"/api/files/view", ctx.HandleFileView},
		{"/api/files/thumbnail", ctx.HandleFileThumbnail},
		{"/api/thumbnail", ctx.HandleThumbnail},
		{"/api/sdcard/files", ctx.HandleSDCardFiles},
		{"/api/sdcard/preview", ctx.HandleSDCardPreview},
		{"/api/sdcard/file", ctx.HandleSDCardFile},
		{"/api/network/dns", ctx.HandleNetworkDNS},
		{"/api/network/interfaces", ctx.HandleNetworkInterfaces},
		{"/api/network/dns-lookup", ctx.HandleDNSLookup},
		{"/api/network/ping", ctx.HandlePing},
		{"/api/network/diagnostics", ctx.HandleNetworkDiagnostics},
		{"/api/ota/status", ctx.HandleOTAStatus},
		{"/api/ota/install", ctx.HandleOTAInstall},
		{"/api/system/time", ctx.HandleSystemTime},
		{"/api/system/tls-certificate", ctx.HandleSystemTLSCertificate},
		{"/api/system/services/restart", ctx.HandleSystemServicesRestart},
		{"/api/system/panic", ctx.HandleSystemPanic},
		{"/api/system/stats", ctx.HandleSystemStats},
		{"/api/googlephotos/status", ctx.HandleGooglePhotosStatus},
		{"/api/googlephotos/auth/start", ctx.HandleGooglePhotosAuthStart},
		{"/api/googlephotos/auth/callback", ctx.HandleGooglePhotosAuthCallback},
		{"/api/googlephotos/auth/disconnect", ctx.HandleGooglePhotosAuthDisconnect},
		{"/api/googlephotos/sync", ctx.HandleGooglePhotosSync},
		{"/api/googlephotos/sync/cancel", ctx.HandleGooglePhotosSyncCancel},
		{"/api/googlephotos/sync/progress", ctx.HandleGooglePhotosSyncProgress},
		{"/api/googlephotos/sync/history/export", ctx.HandleGooglePhotosSyncHistoryExport},
		{"/api/googlephotos/albums", ctx.HandleGooglePhotosAlbums},
		{"/api/googlephotos/albums/", ctx.HandleGooglePhotosAlbums},
		{"/api/googlephotos/cards", ctx.HandleGooglePhotosCards},
		{"/api/googlephotos/cards/", ctx.HandleGooglePhotosCards},
		{"/ws", websocket.HandleWebSocketWithDmesg(stateMgr, eventMgr, dmesgMgr, otaMgr)},
	}
	for _, route := range routes {
		mux.HandleFunc(route.path, route.handler)
	}
}

func registerWebRoutes(mux *http.ServeMux) {
	routes := []struct {
		path    string
		handler http.HandlerFunc
	}{
		{"/static/", webui.HandleStatic},
		{"/", webui.HandleSPA},
		{"/legacy/status", webui.HandleIndex},
		{"/legacy/wifi", webui.HandleWiFi},
		{"/legacy/history", webui.HandleHistory},
		{"/legacy/gallery", webui.HandleGallery},
		{"/legacy/config", webui.HandleConfig},
	}
	for _, route := range routes {
		mux.HandleFunc(route.path, route.handler)
	}
}

func buildHandler(
	appMux http.Handler,
	passwordProvider auth.PasswordProvider,
	allowedOrigins []string,
	stateMgr *state.Manager,
	appSettings *settings.Settings,
) http.Handler {
	authLimiter := ratelimit.NewLimiter(ratelimit.AuthConfig())

	infraMux := http.NewServeMux()
	infraMux.HandleFunc("/healthz", handleHealthz)
	infraMux.Handle("/readyz", makeReadyzHandler(stateMgr, appSettings))
	infraMux.Handle("/metrics", metrics.Handler())

	authProtected := auth.BasicAuthMiddlewareWithProvider(passwordProvider, authLimiter)(appMux)
	router := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz", "/readyz", "/metrics":
			infraMux.ServeHTTP(w, r)
		default:
			authProtected.ServeHTTP(w, r)
		}
	})

	handler := auth.SecurityHeadersMiddleware(
		auth.CORSMiddleware(allowedOrigins, true)(router),
	)
	handler = metrics.HTTPMiddleware(processStartTime, handler)
	handler = middleware.RequestID(handler)
	handler = requestTimeoutMiddleware(apiRequestTimeout)(handler)
	handler = panicPersistenceMiddleware(paniclog.DefaultPath)(handler)
	return handler
}

func main() {
	// Enable caller reporting in logs (file:line)
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	if err := paniclog.ConfigureCrashOutput(paniclog.DefaultCrashPath); err != nil {
		log.Printf("Warning: Failed to configure persistent crash output: %v", err)
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			if err := paniclog.Capture(paniclog.DefaultPath, "webui-main", recovered); err != nil {
				log.Printf("Failed to persist panic information: %v", err)
			}
			log.Printf("Recovered from panic: %v (exiting for clean restart)", recovered)
			os.Exit(1)
		}
	}()

	log.Println("Photo Backup Station WebUI - Starting...")

	passwordMgr, err := auth.NewPasswordManager(auth.DefaultGokrazyPasswordFile, "dev")
	if err != nil {
		log.Printf("Warning: Failed to initialize password manager: %v", err)
		log.Println("Using default development password")
		passwordMgr, _ = auth.NewPasswordManager("", "dev")
	}

	// Get port from environment or default to 8080
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Create a context that will be cancelled on shutdown signals
	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
	defer shutdownCancel()

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(sigChan)
	go recoverAndPersistPanic("webui-signal-handler", func() {
		sig := <-sigChan
		log.Printf("Received signal %v, initiating graceful shutdown...", sig)
		shutdownCancel() // Cancel context to stop cleanup goroutines
	})

	// Start WebSocket token cleanup goroutine with context for proper shutdown
	go recoverAndPersistPanic("webui-ws-token-cleanup", func() {
		websocket.CleanupExpiredWSTokens(shutdownCtx)
	})

	// Start system stats collector
	statsCollector := systeminfo.NewStatsCollector()
	statsCollector.Start()
	go func() {
		<-shutdownCtx.Done()
		statsCollector.Stop()
	}()

	// Start dmesg tailer for live kernel log streaming
	dmesgMgr := dmesg.NewManager()
	dmesgMgr.Start()
	go func() {
		<-shutdownCtx.Done()
		dmesgMgr.Stop()
	}()

	// Initialize event manager. In the webui process this only acts as a
	// local pub/sub cache: every event it emits originates from the daemon's
	// subscribe stream (see startDaemonSubscription below), and nothing in
	// this process publishes directly.
	eventMgr := events.NewManager()

	// Initialize state manager. As with eventMgr above, this is a cache
	// populated by the daemon stream; the webui process never persists state
	// itself.
	stateMgr, err := state.NewManager()
	if err != nil {
		log.Fatalf("Failed to create state manager: %v", err)
	}

	// Spawn the long-lived subscription consumer that keeps stateMgr/eventMgr
	// in sync with the daemon over the control socket. Reconnects with
	// exponential backoff on failure.
	go recoverAndPersistPanic("webui-daemon-subscription", func() {
		startDaemonSubscription(shutdownCtx, stateMgr, eventMgr)
	})

	// Load settings
	appSettings, err := settings.Load()
	if err != nil {
		log.Fatalf("Failed to load settings: %v", err)
	}

	// Initialize sync manager
	syncMgr := syncmanager.NewManager(
		state.GetRcloneConfigPath(),
		appSettings.GetRemoteName(),
		appSettings.GetRemotePath(),
		stateMgr,
		appSettings.GetTransfers(),
		appSettings.GetCheckers(),
	)
	// Update Google Photos settings
	syncMgr.SetGooglePhotos(appSettings.GetGooglePhotosEnabled(), appSettings.GetGooglePhotosRemoteName())

	// Initialize WiFi manager
	wifiMgr, err := wifimanager.NewManager()
	if err != nil {
		log.Printf("Warning: Failed to initialize WiFi manager: %v", err)
	} else {
		wifiMgr.SetPrefer5GHzNetworks(appSettings.GetPrefer5GHzWiFi())
		// Log configured WiFi networks
		logConfiguredWiFiNetworks(wifiMgr)
	}

	log.Println("Security headers middleware enabled")

	// Initialize SSRF validator with rate limiting
	// Allow 10 network diagnostic requests per minute per client IP
	ssrfValidator := ssrf.NewValidator(10, time.Minute)
	log.Println("SSRF protection enabled: 10 network diagnostic requests per minute per client")

	// Initialize OTA manager
	otaMgr := ota.NewManager()

	// Create handler context
	ctx := &handlers.Context{
		StateMgr:               stateMgr,
		SyncMgr:                syncMgr,
		Daemon:                 handlers.DaemonControlClient{},
		WiFiMgr:                wifiMgr,
		AppSettings:            appSettings,
		SSRFValidator:          ssrfValidator,
		OTAMgr:                 otaMgr,
		PasswordMgr:            passwordMgr,
		GooglePhotosStateStore: googlephotos.NewStateStore(),
	}

	allowedOrigins := configuredAllowedOrigins()
	if len(allowedOrigins) > 0 {
		logAllowedOrigins(allowedOrigins)
		websocket.SetAllowedOrigins(allowedOrigins)
	}

	// Setup HTTP handlers. Mutating endpoints rely on Basic Auth + same-origin
	// (and an explicit ws-token for WebSocket); CSRF token middleware was
	// removed because it produced spurious failures and added no defense
	// beyond what credentialed same-origin already provides.
	appMux := http.NewServeMux()
	registerAPIRoutes(appMux, ctx, passwordMgr, stateMgr, eventMgr, dmesgMgr, otaMgr)
	registerWebRoutes(appMux)

	handler := buildHandler(appMux, passwordMgr, allowedOrigins, stateMgr, appSettings)

	// Start server (HTTPS if certificates are available, HTTP for development)
	addr := ":" + port

	repairClockAndPersistentCertificateBeforeTLS()

	// Try to load TLS configuration
	tlsConfig, useTLS, err := tlsconfig.LoadOrDefault()
	if err != nil {
		log.Printf("Warning: TLS configuration error: %v", err)
		log.Println("Falling back to HTTP mode")
		useTLS = false
	}

	if useTLS && tlsConfig != nil {
		// Create server with custom TLS configuration
		// This properly handles self-signed certificates for internal/development use
		server := &http.Server{
			Addr:      addr,
			Handler:   handler,
			TLSConfig: tlsConfig,
			// Filter routine TLS handshake noise from clients rejecting the
			// self-signed certificate; real config errors still surface.
			ErrorLog: tlsconfig.NewServerErrorLog(),
			// Timeouts for production readiness
			ReadTimeout:       30 * time.Second,
			WriteTimeout:      30 * time.Second,
			IdleTimeout:       120 * time.Second,
			ReadHeaderTimeout: 10 * time.Second,
		}

		log.Printf("WebUI HTTPS server listening on %s", addr)
		log.Println("TLS configuration: Self-signed certificates accepted for internal use")
		log.Println("Note: This configuration is secure for internal networks (Tailscale, local LAN)")

		// Get cert and key paths from resolved config
		cfg := tlsconfig.ResolveConfig()
		if err := serveWithShutdown(shutdownCtx, server, func() error {
			return server.ListenAndServeTLS(cfg.CertFile, cfg.KeyFile)
		}); err != nil {
			log.Fatalf("Failed to start HTTPS server: %v", err)
		}
		return
	}

	// Fallback to HTTP for development
	log.Printf("SSL certificates not found, starting HTTP server on %s", addr)
	log.Println("Note: Using HTTP for development. Production should use HTTPS.")

	server := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
	}

	if err := serveWithShutdown(shutdownCtx, server, server.ListenAndServe); err != nil {
		log.Fatalf("Failed to start HTTP server: %v", err)
	}
}

// startDaemonSubscription maintains a streaming subscription to the
// pictures-sync daemon over the control socket and pushes received state
// snapshots / events into the local stateMgr and eventMgr caches. The
// WebSocket layer subscribes to those caches, so this becomes the single
// source for real-time UI updates — no more 2-second disk polling.
//
// On any disconnection (daemon restart, socket gone, decode error) the loop
// reconnects with exponential backoff starting at 500ms and capped at 10s.
// Backoff is reset to the base value after a successful subscribe.
func startDaemonSubscription(ctx context.Context, stateMgr *state.Manager, eventMgr *events.Manager) {
	const (
		baseBackoff = 500 * time.Millisecond
		maxBackoff  = 10 * time.Second
	)

	backoff := baseBackoff
	for {
		if ctx.Err() != nil {
			return
		}

		envelopes, err := daemoncontrol.Subscribe(ctx)
		if err != nil {
			log.Printf("daemon subscribe failed (will retry in %s): %v", backoff, err)
			if !sleepCtx(ctx, backoff) {
				return
			}
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}

		log.Println("daemon subscribe: connected, streaming state and events")
		backoff = baseBackoff

		for env := range envelopes {
			switch env.Kind {
			case daemoncontrol.EnvelopeKindState:
				if env.State != nil {
					stateMgr.Replace(*env.State)
				}
			case daemoncontrol.EnvelopeKindEvent:
				if env.Event != nil {
					eventMgr.Republish(*env.Event)
				}
			}
		}

		if ctx.Err() != nil {
			return
		}
		log.Println("daemon subscribe: stream closed, reconnecting")
		if !sleepCtx(ctx, backoff) {
			return
		}
	}
}

// sleepCtx sleeps for d but returns early if ctx is cancelled. Returns false
// when ctx is cancelled so the caller knows to exit.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func recoverAndPersistPanic(source string, fn func()) {
	defer func() {
		if recovered := recover(); recovered != nil {
			if err := paniclog.Capture(paniclog.DefaultPath, source, recovered); err != nil {
				log.Printf("Failed to persist panic information: %v", err)
			}
			panic(recovered)
		}
	}()
	fn()
}

func serveWithShutdown(ctx context.Context, server *http.Server, serve func() error) error {
	errCh := make(chan error, 1)
	go func() {
		errCh <- serve()
	}()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return err
		}
		err := <-errCh
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func requestTimeoutMiddleware(timeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/ws" {
				next.ServeHTTP(w, r)
				return
			}

			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func panicPersistenceMiddleware(path string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if recovered := recover(); recovered != nil {
					if err := paniclog.Capture(path, "webui-http", recovered); err != nil {
						log.Printf("Failed to persist HTTP panic information: %v", err)
					}
					log.Printf("Recovered HTTP panic: %v", recovered)
					http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
