package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/auth"
	"github.com/denysvitali/pictures-sync-s3/pkg/events"
	"github.com/denysvitali/pictures-sync-s3/pkg/handlers"
	"github.com/denysvitali/pictures-sync-s3/pkg/ntpsync"
	"github.com/denysvitali/pictures-sync-s3/pkg/ota"
	"github.com/denysvitali/pictures-sync-s3/pkg/ratelimit"
	"github.com/denysvitali/pictures-sync-s3/pkg/settings"
	"github.com/denysvitali/pictures-sync-s3/pkg/ssrf"
	"github.com/denysvitali/pictures-sync-s3/pkg/state"
	"github.com/denysvitali/pictures-sync-s3/pkg/syncmanager"
	"github.com/denysvitali/pictures-sync-s3/pkg/tlsconfig"
	"github.com/denysvitali/pictures-sync-s3/pkg/websocket"
	"github.com/denysvitali/pictures-sync-s3/pkg/webui"
	"github.com/denysvitali/pictures-sync-s3/pkg/wifimanager"
)

// defaultAllowedOrigins is intentionally empty: same-origin requests don't
// need CORS at all, and a wildcard is incompatible with allowCredentials=true.
// Operators can opt in to additional origins via WEBUI_ALLOWED_ORIGINS.
const defaultAllowedOrigins = ""

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

func main() {
	// Enable caller reporting in logs (file:line)
	log.SetFlags(log.LstdFlags | log.Lshortfile)

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
	go func() {
		sig := <-sigChan
		log.Printf("Received signal %v, initiating graceful shutdown...", sig)
		shutdownCancel() // Cancel context to stop cleanup goroutines
	}()

	// Start WebSocket token cleanup goroutine with context for proper shutdown
	go websocket.CleanupExpiredWSTokens(shutdownCtx)

	// Start WebSocket rate limiter cleanup goroutine
	go websocket.StartRateLimiterCleanup(shutdownCtx)

	// Initialize event manager
	eventMgr := events.NewManager()

	// Initialize state manager
	stateMgr, err := state.NewManager()
	if err != nil {
		log.Fatalf("Failed to create state manager: %v", err)
	}

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
		StateMgr:      stateMgr,
		SyncMgr:       syncMgr,
		Daemon:        handlers.DaemonControlClient{},
		WiFiMgr:       wifiMgr,
		AppSettings:   appSettings,
		SSRFValidator: ssrfValidator,
		OTAMgr:        otaMgr,
		PasswordMgr:   passwordMgr,
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
	wsTokenLimiter := ratelimit.NewLimiter(handlers.WSTokenRateLimitConfig())
	http.HandleFunc("/api/ws-token", handlers.WSTokenHandler(passwordMgr, wsTokenLimiter))
	http.HandleFunc("/api/version", ctx.HandleVersion)
	http.HandleFunc("/api/status", ctx.HandleStatus)
	http.HandleFunc("/api/history", ctx.HandleHistory)
	http.HandleFunc("/api/config", ctx.HandleConfig)
	http.HandleFunc("/api/config/b2", ctx.HandleConfigB2)
	http.HandleFunc("/api/config/b2/regions", ctx.HandleConfigB2Regions)
	http.HandleFunc("/api/config/test", ctx.HandleConfigTest)
	http.HandleFunc("/api/auth/password", ctx.HandlePasswordChange)
	http.HandleFunc("/api/breakglass/authorized-keys", ctx.HandleBreakglassAuthorizedKeys)
	http.HandleFunc("/api/settings", ctx.HandleSettings)
	http.HandleFunc("/api/devices", ctx.HandleDevices)
	http.HandleFunc("/api/devices/select", ctx.HandleDeviceSelect)
	http.HandleFunc("/api/devices/format", ctx.HandleDeviceFormat)
	http.HandleFunc("/api/devices/redetect", ctx.HandleDeviceRedetect)
	http.HandleFunc("/api/sync/start", ctx.HandleSyncStart)
	http.HandleFunc("/api/sync/cancel", ctx.HandleSyncCancel)
	http.HandleFunc("/api/wifi/scan", ctx.HandleWiFiScan)
	http.HandleFunc("/api/wifi/networks", ctx.HandleWiFiNetworks)
	http.HandleFunc("/api/wifi/connect", ctx.HandleWiFiConnect)
	http.HandleFunc("/api/wifi/disconnect", ctx.HandleWiFiDisconnect)
	http.HandleFunc("/api/wifi/reorder", ctx.HandleWiFiReorder)
	http.HandleFunc("/api/wifi/status", ctx.HandleWiFiStatus)
	http.HandleFunc("/api/files/cards", ctx.HandleFileCards)
	http.HandleFunc("/api/files", ctx.HandleFiles)
	http.HandleFunc("/api/files/paginated", ctx.HandleFilesPaginated)
	http.HandleFunc("/api/files/link", ctx.HandleFileLink)
	http.HandleFunc("/api/files/view", ctx.HandleFileView)
	http.HandleFunc("/api/thumbnail", ctx.HandleThumbnail)
	http.HandleFunc("/api/sdcard/files", ctx.HandleSDCardFiles)
	http.HandleFunc("/api/sdcard/preview", ctx.HandleSDCardPreview)
	http.HandleFunc("/api/sdcard/file", ctx.HandleSDCardFile)
	http.HandleFunc("/api/network/dns", ctx.HandleNetworkDNS)
	http.HandleFunc("/api/network/interfaces", ctx.HandleNetworkInterfaces)
	http.HandleFunc("/api/network/dns-lookup", ctx.HandleDNSLookup)
	http.HandleFunc("/api/network/ping", ctx.HandlePing)
	http.HandleFunc("/api/network/diagnostics", ctx.HandleNetworkDiagnostics)
	http.HandleFunc("/api/ota/status", ctx.HandleOTAStatus)
	http.HandleFunc("/api/ota/install", ctx.HandleOTAInstall)
	http.HandleFunc("/api/system/time", ctx.HandleSystemTime)
	http.HandleFunc("/api/system/tls-certificate", ctx.HandleSystemTLSCertificate)
	http.HandleFunc("/api/system/services/restart", ctx.HandleSystemServicesRestart)
	http.HandleFunc("/ws", websocket.HandleWebSocket(stateMgr, eventMgr, otaMgr))

	// SPA route and static assets for React frontend
	http.HandleFunc("/static/", webui.HandleStatic)
	http.HandleFunc("/", webui.HandleSPA)

	// Legacy routes keep working and load the new SPA shell.
	http.HandleFunc("/legacy/status", webui.HandleIndex)
	http.HandleFunc("/legacy/wifi", webui.HandleWiFi)
	http.HandleFunc("/legacy/history", webui.HandleHistory)
	http.HandleFunc("/legacy/gallery", webui.HandleGallery)
	http.HandleFunc("/legacy/config", webui.HandleConfig)

	// Build a per-IP rate limiter for authentication so brute-force attempts
	// against Basic Auth get throttled and locked out (50 attempts / 15 min).
	authLimiter := ratelimit.NewLimiter(ratelimit.AuthConfig())

	// Wrap default mux with middleware chain: security headers -> CORS -> basic auth.
	// Security headers are applied first so they're present on all responses (including auth failures).
	handler := auth.SecurityHeadersMiddleware(
		auth.CORSMiddleware(allowedOrigins, true)(
			auth.BasicAuthMiddlewareWithProvider(passwordMgr, authLimiter)(http.DefaultServeMux),
		),
	)

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
