package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/denysvitali/pictures-sync-s3/pkg/auth"
	"github.com/denysvitali/pictures-sync-s3/pkg/mock"
	"github.com/denysvitali/pictures-sync-s3/pkg/webui"
	"github.com/denysvitali/pictures-sync-s3/pkg/websocket"
)

func main() {
	// Enable caller reporting in logs (file:line)
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	log.Println("Photo Backup Station Mock WebUI - Starting...")
	log.Println("🧪 MOCK MODE: Using simulated backend data for UI testing")

	// Use development password for mock mode
	authPassword := "dev"

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
	go func() {
		sig := <-sigChan
		log.Printf("Received signal %v, initiating graceful shutdown...", sig)
		shutdownCancel() // Cancel context to stop cleanup goroutines
	}()

	// Start WebSocket token cleanup goroutine with context for proper shutdown
	go websocket.CleanupExpiredWSTokens(shutdownCtx)

	// Start WebSocket rate limiter cleanup goroutine
	go websocket.StartRateLimiterCleanup(shutdownCtx)

	// Initialize mock backend with realistic test data
	mockBackend := mock.NewMockBackend()

	// Create a new mux for organizing handlers
	mux := http.NewServeMux()

	// Register mock API handlers
	mockBackend.RegisterHandlers(mux)

	// Page routes (same as production)
	// SPA route (primary interface)
	mux.HandleFunc("/", webui.HandleSPA)

	// API endpoints for page partials (htmx)
	mux.HandleFunc("/api/pages/", webui.HandlePagePartial)

	// Legacy page routes
	mux.HandleFunc("/legacy/status", webui.HandleIndex)
	mux.HandleFunc("/legacy/wifi", webui.HandleWiFi)
	mux.HandleFunc("/legacy/history", webui.HandleHistory)
	mux.HandleFunc("/legacy/gallery", webui.HandleGallery)
	mux.HandleFunc("/legacy/config", webui.HandleConfig)

	// Static assets
	mux.HandleFunc("/static/css/theme.css", webui.HandleThemeCSS)
	mux.HandleFunc("/static/bootstrap/css/bootstrap.min.css", webui.HandleBootstrapCSS)
	mux.HandleFunc("/static/bootstrap/js/bootstrap.bundle.min.js", webui.HandleBootstrapJS)
	mux.HandleFunc("/static/js/htmx.min.js", webui.HandleHtmxJS)
	mux.HandleFunc("/static/js/utils.js", webui.HandleUtilsJS)
	mux.HandleFunc("/static/js/components.js", webui.HandleComponentsJS)
	mux.HandleFunc("/static/js/router.js", webui.HandleRouterJS)

	// Mock WebSocket handler that simulates real-time updates
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		log.Println("⚠️  WebSocket connection attempted - not fully implemented in mock mode")
		http.Error(w, "WebSocket not implemented in mock mode", http.StatusNotImplemented)
	})

	// Wrap with middleware chain: security headers -> basic auth
	handler := auth.SecurityHeadersMiddleware(
		auth.BasicAuthMiddleware(authPassword, nil)(mux),
	)

	// Start HTTP server (mock mode only uses HTTP, no SSL certificates needed)
	addr := ":" + port
	log.Printf("🚀 Mock WebUI HTTP server listening on %s", addr)
	log.Printf("🔗 Open in browser: http://localhost:%s", port)
	log.Printf("🔐 Username: gokrazy, Password: %s", authPassword)
	log.Println("📋 Features available:")
	log.Println("   • Status page with simulated sync progress")
	log.Println("   • WiFi management with realistic network data")
	log.Println("   • History with sample sync records")
	log.Println("   • Configuration with rclone config simulation")
	log.Println("   • Gallery with mock file structure")
	log.Println("   • All Bootstrap UI components and interactions")
	log.Println()
	log.Println("🎯 Perfect for:")
	log.Println("   • UI development and testing")
	log.Println("   • E2E testing with Cypress")
	log.Println("   • Demo and presentation purposes")
	log.Println("   • Frontend development without hardware")

	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatalf("Failed to start HTTP server: %v", err)
	}
}