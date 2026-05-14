package httputil

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
)

// Response is a generic JSON response structure
type Response struct {
	Success bool                   `json:"success"`
	Error   string                 `json:"error,omitempty"`
	Data    map[string]interface{} `json:"data,omitempty"`
	Message string                 `json:"message,omitempty"`
}

// JSON writes a JSON response to the ResponseWriter
func JSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("Failed to encode JSON response: %v", err)
	}
}

// Success writes a successful JSON response
func Success(w http.ResponseWriter, data map[string]interface{}) {
	JSON(w, http.StatusOK, Response{
		Success: true,
		Data:    data,
	})
}

// SuccessWithMessage writes a successful JSON response with a message
func SuccessWithMessage(w http.ResponseWriter, message string, data map[string]interface{}) {
	JSON(w, http.StatusOK, Response{
		Success: true,
		Message: message,
		Data:    data,
	})
}

// Error writes an error JSON response
func Error(w http.ResponseWriter, statusCode int, message string) {
	JSON(w, statusCode, Response{
		Success: false,
		Error:   message,
	})
}

// ErrorWithDetails writes an error JSON response with additional details
func ErrorWithDetails(w http.ResponseWriter, statusCode int, message string, details map[string]interface{}) {
	JSON(w, statusCode, Response{
		Success: false,
		Error:   message,
		Data:    details,
	})
}

// InternalError writes a 500 Internal Server Error response. The full error
// is logged server-side; the response body contains only a generic message to
// avoid leaking internal paths or backend details to clients.
func InternalError(w http.ResponseWriter, err error) {
	log.Printf("Internal error: %v", err)
	Error(w, http.StatusInternalServerError, "Internal Server Error")
}

// BadRequest writes a 400 Bad Request response
func BadRequest(w http.ResponseWriter, message string) {
	Error(w, http.StatusBadRequest, message)
}

// NotFound writes a 404 Not Found response
func NotFound(w http.ResponseWriter, message string) {
	Error(w, http.StatusNotFound, message)
}

// Unauthorized writes a 401 Unauthorized response
func Unauthorized(w http.ResponseWriter, message string) {
	Error(w, http.StatusUnauthorized, message)
}

// Forbidden writes a 403 Forbidden response
func Forbidden(w http.ResponseWriter, message string) {
	Error(w, http.StatusForbidden, message)
}

// MethodNotAllowed writes a 405 Method Not Allowed response
func MethodNotAllowed(w http.ResponseWriter) {
	Error(w, http.StatusMethodNotAllowed, "Method not allowed")
}

// ServiceUnavailable writes a 503 Service Unavailable response
func ServiceUnavailable(w http.ResponseWriter, message string) {
	Error(w, http.StatusServiceUnavailable, message)
}

// Conflict writes a 409 Conflict response
func Conflict(w http.ResponseWriter, message string) {
	Error(w, http.StatusConflict, message)
}

// DecodeJSON decodes JSON request body with size limit
func DecodeJSON(r *http.Request, v interface{}) error {
	// Limit request body to 10MB by default
	r.Body = http.MaxBytesReader(nil, r.Body, 10*1024*1024)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(v)
}

// DecodeJSONWithLimit decodes JSON request body with custom size limit
func DecodeJSONWithLimit(r *http.Request, v interface{}, maxBytes int64) error {
	r.Body = http.MaxBytesReader(nil, r.Body, maxBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(v)
}

// QueryParam retrieves a query parameter, returns error if missing
func QueryParam(r *http.Request, param string) (string, error) {
	value := r.URL.Query().Get(param)
	if value == "" {
		return "", fmt.Errorf("%s parameter required", param)
	}
	return value, nil
}

// QueryParamDefault retrieves a query parameter with a default value
func QueryParamDefault(r *http.Request, param, defaultValue string) string {
	value := r.URL.Query().Get(param)
	if value == "" {
		return defaultValue
	}
	return value
}

// QueryParamInt retrieves an integer query parameter
func QueryParamInt(r *http.Request, param string, defaultValue int) int {
	value := r.URL.Query().Get(param)
	if value == "" {
		return defaultValue
	}
	intVal, err := strconv.Atoi(value)
	if err != nil {
		return defaultValue
	}
	return intVal
}

// QueryParamIntRange retrieves an integer query parameter and clamps it to a range
func QueryParamIntRange(r *http.Request, param string, defaultValue, min, max int) int {
	value := QueryParamInt(r, param, defaultValue)
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

// MethodGuard checks if request method matches allowed methods
func MethodGuard(w http.ResponseWriter, r *http.Request, allowedMethods ...string) bool {
	for _, method := range allowedMethods {
		if r.Method == method {
			return true
		}
	}
	MethodNotAllowed(w)
	return false
}

// CheckRequired checks if required fields are present in a map
func CheckRequired(data map[string]interface{}, fields ...string) error {
	for _, field := range fields {
		val, ok := data[field]
		if !ok {
			return fmt.Errorf("missing required field: %s", field)
		}
		// Check if string field is empty
		if str, ok := val.(string); ok && str == "" {
			return fmt.Errorf("field %s cannot be empty", field)
		}
	}
	return nil
}
