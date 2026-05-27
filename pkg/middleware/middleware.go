package middleware

import (
	"encoding/json"
	"io"
	"log"
	"net"
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

// DecodeJSON decodes JSON request body with size limit validation. It drains
// and closes the request body before returning so HTTP/1.1 keep-alive
// connections can be reused even when callers forget to close r.Body.
func DecodeJSON(r *http.Request, v interface{}, maxBytes int64) error {
	if r.Body == nil {
		return io.EOF
	}
	if maxBytes > 0 {
		r.Body = http.MaxBytesReader(nil, r.Body, maxBytes)
	}
	defer func() {
		// Drain any remaining bytes so the underlying connection can be
		// reused, then close. Errors are ignored: the caller already has
		// the decode result and there is no meaningful recovery here.
		_, _ = io.Copy(io.Discard, r.Body)
		_ = r.Body.Close()
	}()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields() // Strict parsing
	return decoder.Decode(v)
}

// recoveryResponseWriter wraps http.ResponseWriter to track whether the
// response has been written to, so panic recovery can avoid corrupting an
// in-flight response by writing a second header/body.
type recoveryResponseWriter struct {
	http.ResponseWriter
	wroteHeader bool
}

func (rw *recoveryResponseWriter) WriteHeader(code int) {
	rw.wroteHeader = true
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *recoveryResponseWriter) Write(b []byte) (int, error) {
	rw.wroteHeader = true
	return rw.ResponseWriter.Write(b)
}

// Recovery middleware recovers from panics and logs them. It re-raises
// http.ErrAbortHandler per net/http convention, and only writes a 500
// response when the wrapped handler has not yet written to the response
// (otherwise it would corrupt the in-flight body with a superfluous header).
func Recovery(next HandlerFunc) HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		rw := &recoveryResponseWriter{ResponseWriter: w}
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			// http.ErrAbortHandler signals an intentional abort and must be
			// re-panicked so the http.Server can terminate the connection.
			if rec == http.ErrAbortHandler {
				panic(rec)
			}
			log.Printf("PANIC recovered: %v\n%s", rec, debug.Stack())
			if rw.wroteHeader {
				// Headers/body already partially sent — writing again would
				// emit a superfluous WriteHeader and append garbage JSON to
				// the response body. Nothing safe to do but log.
				return
			}
			WriteError(rw.ResponseWriter, http.StatusInternalServerError, "Internal server error", nil)
		}()
		return next(rw, r)
	}
}

// RequestLogger logs HTTP requests with method, path, client IP, and request ID.
// It reads the request ID set by the RequestID middleware (if wired) so that
// log lines can be correlated with the X-Request-ID response header.
func RequestLogger(next HandlerFunc) HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		clientIP := GetClientIP(r)
		reqID := RequestIDFromContext(r.Context())
		if reqID != "" {
			log.Printf("[HTTP] %s %s from %s req_id=%s", r.Method, r.URL.Path, clientIP, reqID)
		} else {
			log.Printf("[HTTP] %s %s from %s", r.Method, r.URL.Path, clientIP)
		}
		return next(w, r)
	}
}

// GetClientIP extracts the real client IP from request headers. X-Forwarded-For
// and X-Real-IP are only honored when RemoteAddr is a loopback or RFC1918
// private address.
func GetClientIP(r *http.Request) string {
	if isTrustedProxySource(r.RemoteAddr) {
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
	}

	// Fall back to RemoteAddr (strip port). Preserve bracketed IPv6 form
	// for backwards compatibility with existing callers/tests.
	for i := len(r.RemoteAddr) - 1; i >= 0; i-- {
		if r.RemoteAddr[i] == ':' {
			return r.RemoteAddr[:i]
		}
	}
	return r.RemoteAddr
}

// isTrustedProxySource reports whether the given RemoteAddr is a loopback,
// RFC1918 private, or link-local address. Only such sources are honored for
// X-Forwarded-For / X-Real-IP headers.
func isTrustedProxySource(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		// No port (or malformed) — try the raw value as an IP.
		host = remoteAddr
	}
	// Strip IPv6 brackets if present (e.g. "[::1]").
	if len(host) >= 2 && host[0] == '[' && host[len(host)-1] == ']' {
		host = host[1 : len(host)-1]
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
		return true
	}
	return false
}
