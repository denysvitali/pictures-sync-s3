package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"log"
	"net/http"
	"sync"
)

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

// BasicAuthMiddleware provides HTTP Basic Authentication
func BasicAuthMiddleware(authPassword string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			username, password, ok := r.BasicAuth()

			// Use constant-time comparison to prevent timing attacks
			usernameMatch := subtle.ConstantTimeCompare([]byte(username), []byte("gokrazy")) == 1
			passwordMatch := subtle.ConstantTimeCompare([]byte(password), []byte(authPassword)) == 1

			if !ok || !usernameMatch || !passwordMatch {
				w.Header().Set("WWW-Authenticate", `Basic realm="Photo Backup Station"`)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			// Limit request body size to prevent DoS (10MB max)
			r.Body = http.MaxBytesReader(w, r.Body, 10*1024*1024)

			next.ServeHTTP(w, r)
		})
	}
}
