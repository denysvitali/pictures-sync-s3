package googlephotos

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// APIError is returned for non-2xx responses from the Google Photos API.
// Status is the HTTP status code; Body is the (possibly truncated) raw response;
// Message is a best-effort extraction of the Google `error.message` field.
type APIError struct {
	Op      string // operation that failed, e.g. "batchRemoveMediaItems"
	Status  int
	Message string
	Body    string
}

func (e *APIError) Error() string {
	msg := e.Message
	if msg == "" {
		msg = strings.TrimSpace(e.Body)
	}
	if msg == "" {
		return fmt.Sprintf("%s failed (%d)", e.Op, e.Status)
	}
	return fmt.Sprintf("%s failed (%d): %s", e.Op, e.Status, msg)
}

// IsPermissionDenied reports whether err is a 403 PERMISSION_DENIED from the API.
// batchRemoveMediaItems returns this when an item was not added to the album by the app,
// or when the album was not created by the app.
func IsPermissionDenied(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.Status == 403
}

// newAPIError parses a Google API error envelope ({"error":{"code":..,"message":..,"status":..}})
// out of body and wraps it as an APIError. Falls back to raw body if not parseable.
func newAPIError(op string, status int, body []byte) *APIError {
	e := &APIError{Op: op, Status: status, Body: string(body)}
	var env struct {
		Error struct {
			Message string `json:"message"`
			Status  string `json:"status"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &env) == nil {
		if env.Error.Message != "" {
			e.Message = env.Error.Message
			if env.Error.Status != "" {
				e.Message = env.Error.Status + ": " + env.Error.Message
			}
		}
	}
	return e
}
