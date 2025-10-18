package middleware

import (
	"encoding/json"
	"log"
	"net/http"
	"runtime/debug"
)

// HandlerFunc is a custom handler type that returns an error
type HandlerFunc func(w http.ResponseWriter, r *http.Request) error

// ErrorResponse represents a standard error response
type ErrorResponse struct {
	Error   string                 `json:"error"`
	Details map[string]interface{} `json:"details,omitempty"`
}

// SuccessResponse represents a standard success response
type SuccessResponse struct {
	Status  string                 `json:"status"`
	Message string                 `json:"message,omitempty"`
	Data    map[string]interface{} `json:"data,omitempty"`
}

// WriteJSON writes a JSON response with proper headers
func WriteJSON(w http.ResponseWriter, statusCode int, data interface{}) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	return json.NewEncoder(w).Encode(data)
}

// WriteError writes a standard error response
func WriteError(w http.ResponseWriter, statusCode int, message string, details map[string]interface{}) {
	if err := WriteJSON(w, statusCode, ErrorResponse{
		Error:   message,
		Details: details,
	}); err != nil {
		log.Printf("Failed to write error response: %v", err)
	}
}

// WriteSuccess writes a standard success response
func WriteSuccess(w http.ResponseWriter, message string, data map[string]interface{}) {
	if err := WriteJSON(w, http.StatusOK, SuccessResponse{
		Status:  "ok",
		Message: message,
		Data:    data,
	}); err != nil {
		log.Printf("Failed to write success response: %v", err)
	}
}

// DecodeJSON decodes JSON request body with size limit validation
func DecodeJSON(r *http.Request, v interface{}, maxBytes int64) error {
	if maxBytes > 0 {
		r.Body = http.MaxBytesReader(nil, r.Body, maxBytes)
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields() // Strict parsing
	return decoder.Decode(v)
}

// MethodOnly creates a middleware that only allows specific HTTP methods
func MethodOnly(methods ...string) func(HandlerFunc) HandlerFunc {
	allowedMethods := make(map[string]bool)
	for _, method := range methods {
		allowedMethods[method] = true
	}

	return func(next HandlerFunc) HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) error {
			if !allowedMethods[r.Method] {
				WriteError(w, http.StatusMethodNotAllowed, "Method not allowed", map[string]interface{}{
					"allowed_methods": methods,
					"received":        r.Method,
				})
				return nil // Already handled
			}
			return next(w, r)
		}
	}
}

// Recovery middleware recovers from panics and logs them
func Recovery(next HandlerFunc) HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		defer func() {
			if err := recover(); err != nil {
				log.Printf("PANIC recovered: %v\n%s", err, debug.Stack())
				WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
			}
		}()
		return next(w, r)
	}
}

// RequestLogger logs HTTP requests with method, path, and client IP
func RequestLogger(next HandlerFunc) HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		clientIP := GetClientIP(r)
		log.Printf("[HTTP] %s %s from %s", r.Method, r.URL.Path, clientIP)
		return next(w, r)
	}
}

// Chain combines multiple middleware functions into one
func Chain(middlewares ...func(HandlerFunc) HandlerFunc) func(HandlerFunc) HandlerFunc {
	return func(final HandlerFunc) HandlerFunc {
		// Apply middlewares in reverse order so they execute in the correct order
		for i := len(middlewares) - 1; i >= 0; i-- {
			final = middlewares[i](final)
		}
		return final
	}
}

// Adapt converts our custom HandlerFunc to http.HandlerFunc
func Adapt(h HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := h(w, r); err != nil {
			log.Printf("Handler error for %s %s: %v", r.Method, r.URL.Path, err)
			WriteError(w, http.StatusInternalServerError, err.Error(), nil)
		}
	}
}

// RequireQueryParam ensures a query parameter is present
func RequireQueryParam(param string) func(HandlerFunc) HandlerFunc {
	return func(next HandlerFunc) HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) error {
			if r.URL.Query().Get(param) == "" {
				WriteError(w, http.StatusBadRequest,
					param+" parameter required",
					map[string]interface{}{"parameter": param})
				return nil
			}
			return next(w, r)
		}
	}
}

// GetClientIP extracts the real client IP from request headers
func GetClientIP(r *http.Request) string {
	// Check X-Forwarded-For header (proxy/load balancer)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take the first IP in the comma-separated list
		for i := 0; i < len(xff); i++ {
			if xff[i] == ',' {
				return xff[:i]
			}
		}
		return xff
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
	for i := len(r.RemoteAddr) - 1; i >= 0; i-- {
		if r.RemoteAddr[i] == ':' {
			return r.RemoteAddr[:i]
		}
	}
	return r.RemoteAddr
}
