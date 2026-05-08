package ratelimit

import (
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Limiter provides rate limiting functionality
type Limiter struct {
	mu       sync.RWMutex
	limiters map[string]*clientLimiter
	cleanup  *time.Ticker
	done     chan struct{}
}

// clientLimiter tracks rate limiting for a specific client IP
type clientLimiter struct {
	limiter      *rate.Limiter
	lastSeen     time.Time
	failedAuths  int
	lockedUntil  time.Time
	firstFailure time.Time
}

// Config holds rate limiting configuration
type Config struct {
	// RequestsPerSecond is the number of requests allowed per second
	RequestsPerSecond float64
	// Burst is the maximum burst size
	Burst int
	// MaxAuthAttempts is the number of failed auth attempts allowed before lockout
	MaxAuthAttempts int
	// AuthWindow is the time window for counting failed auth attempts
	AuthWindow time.Duration
	// LockoutDuration is how long to lock out an IP after max attempts
	LockoutDuration time.Duration
	// CleanupInterval is how often to clean up old entries
	CleanupInterval time.Duration
	// ClientExpiry is how long to keep a client entry without activity
	ClientExpiry time.Duration
}

// DefaultConfig returns a safe default configuration
func DefaultConfig() Config {
	return Config{
		RequestsPerSecond: 50.0,
		Burst:             100,
		MaxAuthAttempts:   5,
		AuthWindow:        15 * time.Minute,
		LockoutDuration:   15 * time.Minute,
		CleanupInterval:   5 * time.Minute,
		ClientExpiry:      30 * time.Minute,
	}
}

// AuthConfig returns configuration for authentication endpoints
func AuthConfig() Config {
	return Config{
		RequestsPerSecond: 50.0,
		Burst:             100,
		MaxAuthAttempts:   5,
		AuthWindow:        15 * time.Minute,
		LockoutDuration:   15 * time.Minute,
		CleanupInterval:   5 * time.Minute,
		ClientExpiry:      30 * time.Minute,
	}
}

// ExpensiveOpConfig returns configuration for expensive operations (thumbnails, file listing)
func ExpensiveOpConfig() Config {
	return Config{
		RequestsPerSecond: 50.0,
		Burst:             100,
		MaxAuthAttempts:   0,
		AuthWindow:        0,
		LockoutDuration:   0,
		CleanupInterval:   5 * time.Minute,
		ClientExpiry:      30 * time.Minute,
	}
}

// NewLimiter creates a new rate limiter with the given configuration
func NewLimiter(config Config) *Limiter {
	l := &Limiter{
		limiters: make(map[string]*clientLimiter),
		cleanup:  time.NewTicker(config.CleanupInterval),
		done:     make(chan struct{}),
	}

	// Start cleanup goroutine
	go l.cleanupRoutine(config.ClientExpiry)

	return l
}

// cleanupRoutine periodically removes old client entries
func (l *Limiter) cleanupRoutine(expiry time.Duration) {
	for {
		select {
		case <-l.cleanup.C:
			l.cleanup_old_entries(expiry)
		case <-l.done:
			return
		}
	}
}

func (l *Limiter) cleanup_old_entries(expiry time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	for ip, client := range l.limiters {
		// Remove entries that haven't been seen recently and are not locked out
		if now.Sub(client.lastSeen) > expiry && now.After(client.lockedUntil) {
			delete(l.limiters, ip)
		}
	}
}

// Stop stops the cleanup routine
func (l *Limiter) Stop() {
	l.cleanup.Stop()
	close(l.done)
}

// getClientLimiter returns or creates a rate limiter for the given IP
func (l *Limiter) getClientLimiter(ip string, config Config) *clientLimiter {
	l.mu.Lock()
	defer l.mu.Unlock()

	client, exists := l.limiters[ip]
	if !exists {
		client = &clientLimiter{
			limiter:  rate.NewLimiter(rate.Limit(config.RequestsPerSecond), config.Burst),
			lastSeen: time.Now(),
		}
		l.limiters[ip] = client
	} else {
		client.lastSeen = time.Now()
	}

	return client
}

// Allow checks if a request from the given IP should be allowed
func (l *Limiter) Allow(ip string, config Config) bool {
	client := l.getClientLimiter(ip, config)

	// Check if client is locked out
	if time.Now().Before(client.lockedUntil) {
		return false
	}

	return client.limiter.Allow()
}

// RecordAuthFailure records a failed authentication attempt
func (l *Limiter) RecordAuthFailure(ip string, config Config) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	client, exists := l.limiters[ip]
	if !exists {
		client = &clientLimiter{
			limiter:      rate.NewLimiter(rate.Limit(config.RequestsPerSecond), config.Burst),
			lastSeen:     time.Now(),
			firstFailure: time.Now(),
			failedAuths:  1,
		}
		l.limiters[ip] = client
		return false
	}

	now := time.Now()
	client.lastSeen = now

	// Reset counter if outside the auth window
	if now.Sub(client.firstFailure) > config.AuthWindow {
		client.firstFailure = now
		client.failedAuths = 1
		return false
	}

	// Increment failure count
	client.failedAuths++

	// Check if we should lock out this IP
	if client.failedAuths >= config.MaxAuthAttempts {
		client.lockedUntil = now.Add(config.LockoutDuration)
		log.Printf("SECURITY: IP %s locked out due to %d failed auth attempts (lockout until %s)",
			ip, client.failedAuths, client.lockedUntil.Format(time.RFC3339))
		return true // Locked out
	}

	log.Printf("SECURITY: Failed auth attempt %d/%d from IP %s",
		client.failedAuths, config.MaxAuthAttempts, ip)
	return false
}

// ResetAuthFailures resets the failed authentication counter for an IP
func (l *Limiter) ResetAuthFailures(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if client, exists := l.limiters[ip]; exists {
		client.failedAuths = 0
		client.firstFailure = time.Time{}
		client.lockedUntil = time.Time{}
	}
}

// GetAuthFailureCount returns the current failed auth count for an IP
func (l *Limiter) GetAuthFailureCount(ip string) int {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if client, exists := l.limiters[ip]; exists {
		return client.failedAuths
	}
	return 0
}

// IsLockedOut checks if an IP is currently locked out
func (l *Limiter) IsLockedOut(ip string) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if client, exists := l.limiters[ip]; exists {
		return time.Now().Before(client.lockedUntil)
	}
	return false
}

// extractIP extracts the real IP address from the request
// Handles X-Forwarded-For and X-Real-IP headers for reverse proxies
func extractIP(r *http.Request) string {
	// Check X-Forwarded-For header
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		// Take the first IP in the list
		if ip := net.ParseIP(xff); ip != nil {
			return ip.String()
		}
		// If it's a list, take the first one
		if ips := splitAndTrim(xff, ","); len(ips) > 0 {
			if ip := net.ParseIP(ips[0]); ip != nil {
				return ip.String()
			}
		}
	}

	// Check X-Real-IP header
	xri := r.Header.Get("X-Real-IP")
	if xri != "" {
		if ip := net.ParseIP(xri); ip != nil {
			return ip.String()
		}
	}

	// Fall back to RemoteAddr
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

// splitAndTrim splits a string and trims whitespace from each part
func splitAndTrim(s, sep string) []string {
	parts := []string{}
	for _, part := range splitString(s, sep) {
		trimmed := trimSpace(part)
		if trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return parts
}

func splitString(s, sep string) []string {
	if s == "" {
		return []string{}
	}
	result := []string{}
	current := ""
	for _, c := range s {
		if string(c) == sep {
			result = append(result, current)
			current = ""
		} else {
			current += string(c)
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}

func trimSpace(s string) string {
	start := 0
	end := len(s)

	// Trim leading whitespace
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}

	// Trim trailing whitespace
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}

	return s[start:end]
}

// Middleware creates an HTTP middleware that enforces rate limiting
func (l *Limiter) Middleware(config Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := extractIP(r)

			// Check if IP is locked out
			if l.IsLockedOut(ip) {
				log.Printf("SECURITY: Blocked request from locked-out IP %s to %s %s",
					ip, r.Method, r.URL.Path)
				http.Error(w, "Too many requests - temporarily locked out", http.StatusTooManyRequests)
				return
			}

			// Check rate limit
			if !l.Allow(ip, config) {
				log.Printf("SECURITY: Rate limit exceeded for IP %s on %s %s",
					ip, r.Method, r.URL.Path)
				http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// HandlerFunc wraps an http.HandlerFunc with rate limiting
func (l *Limiter) HandlerFunc(config Config, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := extractIP(r)

		// Check if IP is locked out
		if l.IsLockedOut(ip) {
			log.Printf("SECURITY: Blocked request from locked-out IP %s to %s %s",
				ip, r.Method, r.URL.Path)
			http.Error(w, "Too many requests - temporarily locked out", http.StatusTooManyRequests)
			return
		}

		// Check rate limit
		if !l.Allow(ip, config) {
			log.Printf("SECURITY: Rate limit exceeded for IP %s on %s %s",
				ip, r.Method, r.URL.Path)
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		handler(w, r)
	}
}
