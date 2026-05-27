package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

// contextKey is an unexported type used for context keys in this package
// to avoid collisions with other packages.
type contextKey int

const requestIDKey contextKey = 0

const requestIDHeader = "X-Request-ID"

// generateRequestID produces a 16-byte (32 hex char) random ID using
// crypto/rand. Falls back to a static sentinel if the system PRNG is
// unavailable (should never happen in practice).
func generateRequestID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "00000000000000000000000000000000"
	}
	return hex.EncodeToString(b)
}

// RequestID is middleware that reads X-Request-ID from the incoming request
// if present, otherwise generates a random hex ID. The ID is stored in the
// request context and echoed back in the X-Request-ID response header.
//
// Wire it at the front of the middleware chain (before logging) so that all
// subsequent handlers can retrieve the ID via RequestIDFromContext.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(requestIDHeader)
		if id == "" {
			id = generateRequestID()
		}

		// Store in context for downstream handlers.
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		r = r.WithContext(ctx)

		// Echo to response header so clients/proxies can correlate.
		w.Header().Set(requestIDHeader, id)

		next.ServeHTTP(w, r)
	})
}

// RequestIDFromContext retrieves the request ID stored by the RequestID
// middleware. Returns an empty string if not set.
func RequestIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(requestIDKey).(string)
	return v
}
