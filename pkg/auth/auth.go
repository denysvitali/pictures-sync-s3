package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/denysvitali/pictures-sync-s3/pkg/ratelimit"
)

type PasswordProvider interface {
	CurrentPassword() string
}

type staticPasswordProvider string

func (p staticPasswordProvider) CurrentPassword() string {
	return string(p)
}

var (
	csrfToken string
	csrfMutex sync.RWMutex
)

// GenerateCSRFToken creates a new CSRF token
func GenerateCSRFToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		log.Fatal("Failed to generate CSRF token:", err)
	}
	return base64.StdEncoding.EncodeToString(b)
}

// InitCSRFToken initializes the CSRF token
func InitCSRFToken() {
	csrfMutex.Lock()
	defer csrfMutex.Unlock()
	csrfToken = GenerateCSRFToken()
}

// GetCSRFToken returns the current CSRF token
func GetCSRFToken() string {
	csrfMutex.RLock()
	defer csrfMutex.RUnlock()
	return csrfToken
}

// ValidateCSRFToken checks if the provided token matches the current token
func ValidateCSRFToken(token string) bool {
	if token == "" {
		return false
	}
	csrfMutex.RLock()
	defer csrfMutex.RUnlock()
	return subtle.ConstantTimeCompare([]byte(token), []byte(csrfToken)) == 1
}

// CSRFProtection is middleware that validates CSRF tokens for state-changing requests
func CSRFProtection(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Only check CSRF for state-changing methods
		if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodDelete {
			token := r.Header.Get("X-CSRF-Token")
			if !ValidateCSRFToken(token) {
				http.Error(w, "Invalid CSRF token", http.StatusForbidden)
				log.Printf("CSRF validation failed for %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
				return
			}
		}
		next(w, r)
	}
}

// SecurityHeadersMiddleware adds comprehensive HTTP security headers
func SecurityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if this is a WebSocket upgrade request
		isWebSocket := strings.ToLower(r.Header.Get("Upgrade")) == "websocket"

		// X-Frame-Options: Prevent clickjacking attacks
		// DENY prevents any domain from framing the content
		w.Header().Set("X-Frame-Options", "DENY")

		// X-Content-Type-Options: Prevent MIME sniffing
		// nosniff ensures browsers respect the Content-Type header
		w.Header().Set("X-Content-Type-Options", "nosniff")

		// X-XSS-Protection: Enable browser's XSS filtering (legacy support)
		// mode=block tells the browser to block the page if XSS is detected
		w.Header().Set("X-XSS-Protection", "1; mode=block")

		// Strict-Transport-Security: Enforce HTTPS for 1 year
		// This tells browsers to always use HTTPS for this domain
		// preload allows inclusion in browser HSTS preload lists
		w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains; preload")

		// Referrer-Policy: Control referrer information leakage
		// strict-origin-when-cross-origin sends full URL for same-origin,
		// only origin for cross-origin, and nothing for downgrade (HTTPS->HTTP)
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

		// Permissions-Policy: Disable unnecessary browser features
		// This restricts access to potentially dangerous APIs
		w.Header().Set("Permissions-Policy", "geolocation=(), microphone=(), camera=(), payment=()")

		// Content-Security-Policy: Defense-in-depth against XSS and injection attacks
		// Skip CSP for WebSocket upgrades as they have different security model
		if !isWebSocket {
			csp := []string{
				// default-src: Fallback for any resource type not explicitly listed
				"default-src 'self'",

				// script-src: Allow inline scripts (required for embedded app) with unsafe-inline
				// and 'self' for any potential external scripts loaded from same origin
				"script-src 'self' 'unsafe-inline'",

				// style-src: Allow inline styles (required for embedded app)
				// unsafe-inline is needed for the extensive inline CSS in templates.go
				"style-src 'self' 'unsafe-inline'",

				// img-src: Allow images from same origin and data URIs (for thumbnails/previews)
				"img-src 'self' data: blob:",

				// font-src: Allow fonts from same origin and data URIs
				"font-src 'self' data:",

				// connect-src: Allow API calls to same origin and WebSocket connections
				// wss: and ws: are needed for WebSocket functionality
				"connect-src 'self' ws: wss:",

				// media-src: Allow media files from same origin and blob URIs
				"media-src 'self' blob:",

				// object-src: Block plugins like Flash
				"object-src 'none'",

				// base-uri: Restrict base tag to prevent base tag injection
				"base-uri 'self'",

				// form-action: Restrict form submissions to same origin
				"form-action 'self'",

				// frame-ancestors: Prevent embedding in iframes (redundant with X-Frame-Options but more powerful)
				"frame-ancestors 'none'",

				// upgrade-insecure-requests: Automatically upgrade HTTP to HTTPS
				"upgrade-insecure-requests",
			}

			w.Header().Set("Content-Security-Policy", strings.Join(csp, "; "))
		}

		next.ServeHTTP(w, r)
	})
}

// extractIP extracts the real IP address from the request
// Handles X-Forwarded-For and X-Real-IP headers for reverse proxies
func extractIP(r *http.Request) string {
	// Check X-Forwarded-For header
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		// Take the first IP in the list
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			ip := strings.TrimSpace(parts[0])
			if net.ParseIP(ip) != nil {
				return ip
			}
		}
	}

	// Check X-Real-IP header
	xri := r.Header.Get("X-Real-IP")
	if xri != "" {
		if net.ParseIP(xri) != nil {
			return xri
		}
	}

	// Fall back to RemoteAddr
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

// ExpensiveOperationMiddleware rate limits expensive operations like thumbnails and file listings
func ExpensiveOperationMiddleware(limiter *ratelimit.Limiter) func(http.HandlerFunc) http.HandlerFunc {
	opConfig := ratelimit.ExpensiveOpConfig()

	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			ip := extractIP(r)

			// Check if IP is rate limited
			if !limiter.Allow(ip, opConfig) {
				http.Error(w, "Rate limit exceeded for expensive operation", http.StatusTooManyRequests)
				log.Printf("SECURITY: Rate limit exceeded for expensive operation from IP %s on %s %s",
					ip, r.Method, r.URL.Path)
				return
			}

			next(w, r)
		}
	}
}

// BasicAuthMiddleware provides HTTP Basic Authentication with rate limiting
func BasicAuthMiddleware(authPassword string, limiter *ratelimit.Limiter) func(http.Handler) http.Handler {
	return BasicAuthMiddlewareWithProvider(staticPasswordProvider(authPassword), limiter)
}

// BasicAuthMiddlewareWithProvider provides HTTP Basic Authentication with rate limiting.
func BasicAuthMiddlewareWithProvider(passwordProvider PasswordProvider, limiter *ratelimit.Limiter) func(http.Handler) http.Handler {
	authConfig := ratelimit.AuthConfig()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := extractIP(r)

			// Only apply rate limiting if limiter is provided
			if limiter != nil {
				// Check if IP is locked out due to too many failed attempts
				if limiter.IsLockedOut(ip) {
					w.Header().Set("WWW-Authenticate", `Basic realm="Photo Backup Station"`)
					http.Error(w, "Too many failed authentication attempts - temporarily locked out", http.StatusTooManyRequests)
					log.Printf("SECURITY: Blocked authentication attempt from locked-out IP %s for %s %s",
						ip, r.Method, r.URL.Path)
					return
				}

				// Check general rate limit for this IP
				if !limiter.Allow(ip, authConfig) {
					w.Header().Set("WWW-Authenticate", `Basic realm="Photo Backup Station"`)
					http.Error(w, "Too many requests - rate limit exceeded", http.StatusTooManyRequests)
					log.Printf("SECURITY: Rate limit exceeded for IP %s on %s %s",
						ip, r.Method, r.URL.Path)
					return
				}
			}

			username, password, ok := r.BasicAuth()
			authPassword := ""
			if passwordProvider != nil {
				authPassword = passwordProvider.CurrentPassword()
			}

			// Use constant-time comparison to prevent timing attacks
			usernameMatch := subtle.ConstantTimeCompare([]byte(username), []byte("gokrazy")) == 1
			passwordMatch := subtle.ConstantTimeCompare([]byte(password), []byte(authPassword)) == 1

			if !ok || !usernameMatch || !passwordMatch {
				w.Header().Set("WWW-Authenticate", `Basic realm="Photo Backup Station"`)

				// Record failed authentication attempt if rate limiter is enabled
				if limiter != nil {
					isLockedOut := limiter.RecordAuthFailure(ip, authConfig)
					if isLockedOut {
						http.Error(w, "Too many failed authentication attempts - account locked", http.StatusTooManyRequests)
					} else {
						http.Error(w, "Unauthorized", http.StatusUnauthorized)
					}
					log.Printf("SECURITY: Failed authentication attempt from IP %s for %s %s (attempts: %d)",
						ip, r.Method, r.URL.Path, limiter.GetAuthFailureCount(ip))
				} else {
					http.Error(w, "Unauthorized", http.StatusUnauthorized)
					log.Printf("SECURITY: Failed authentication attempt from IP %s for %s %s",
						ip, r.Method, r.URL.Path)
				}
				return
			}

			// Successful authentication - reset failure counter for this IP if rate limiter is enabled
			if limiter != nil {
				limiter.ResetAuthFailures(ip)
			}

			// Limit request body size to prevent DoS (10MB max)
			r.Body = http.MaxBytesReader(w, r.Body, 10*1024*1024)

			next.ServeHTTP(w, r)
		})
	}
}

// CORSMiddleware configures CORS for browser-hosted UIs.
// It allows credentials and only permits explicitly configured origins.
// When no allowed origins are configured, CORS is not applied.
func CORSMiddleware(allowedOrigins []string, allowCredentials bool) func(http.Handler) http.Handler {
	origins := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		normalized := strings.TrimSpace(strings.ToLower(origin))
		if normalized == "" {
			continue
		}

		// Accept plain hosts while also accepting full URLs.
		if strings.HasPrefix(normalized, "http://") || strings.HasPrefix(normalized, "https://") {
			u, err := url.Parse(normalized)
			if err != nil {
				continue
			}
			normalized = u.Host
		}

		if normalized == "*" {
			// SECURITY: never reflect a wildcard when credentials are allowed.
			// CORS spec forbids `Access-Control-Allow-Origin: *` together with
			// `Access-Control-Allow-Credentials: true`. Treat wildcard with
			// credentials as a configuration error and fall back to deny-all.
			if allowCredentials {
				log.Printf("auth: ignoring CORS wildcard '*' because allowCredentials=true; configure explicit origins instead")
				continue
			}
			origins = make(map[string]struct{}, 1)
			origins["*"] = struct{}{}
			break
		}

		origins[normalized] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if len(origins) == 0 {
				next.ServeHTTP(w, r)
				return
			}

			origin := r.Header.Get("Origin")
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}

			u, err := url.Parse(origin)
			if err != nil {
				// Malformed Origin header: reject regardless of method.
				http.Error(w, "CORS origin not allowed", http.StatusForbidden)
				return
			}

			normalizedOriginHost := strings.ToLower(u.Host)

			// Always allow the same host as the API endpoint.
			if normalizedOriginHost == strings.ToLower(r.Host) {
				next.ServeHTTP(w, r)
				return
			}

			_, allowAll := origins["*"]
			_, allowed := origins[normalizedOriginHost]
			if !allowAll && !allowed {
				// Disallowed origin: always return 403 (do not fall through).
				http.Error(w, "CORS origin not allowed", http.StatusForbidden)
				return
			}

			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			if allowCredentials {
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-CSRF-Token")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			// Continue with actual request.
			next.ServeHTTP(w, r)
		})
	}
}
