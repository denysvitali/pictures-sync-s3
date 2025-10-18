package httputil

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestJSON(t *testing.T) {
	data := map[string]string{"message": "test"}
	w := httptest.NewRecorder()

	JSON(w, http.StatusOK, data)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %s", contentType)
	}

	var result map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if result["message"] != "test" {
		t.Errorf("Expected message 'test', got '%s'", result["message"])
	}
}

func TestSuccess(t *testing.T) {
	data := map[string]interface{}{"count": 42}
	w := httptest.NewRecorder()

	Success(w, data)

	var response Response
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if !response.Success {
		t.Error("Expected success to be true")
	}

	if count, ok := response.Data["count"].(float64); !ok || count != 42 {
		t.Errorf("Expected count 42, got %v", response.Data["count"])
	}
}

func TestSuccessWithMessage(t *testing.T) {
	data := map[string]interface{}{"id": "123"}
	w := httptest.NewRecorder()

	SuccessWithMessage(w, "Operation completed", data)

	var response Response
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if !response.Success {
		t.Error("Expected success to be true")
	}

	if response.Message != "Operation completed" {
		t.Errorf("Expected message 'Operation completed', got '%s'", response.Message)
	}
}

func TestError(t *testing.T) {
	w := httptest.NewRecorder()

	Error(w, http.StatusBadRequest, "Invalid input")

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}

	var response Response
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if response.Success {
		t.Error("Expected success to be false")
	}

	if response.Error != "Invalid input" {
		t.Errorf("Expected error 'Invalid input', got '%s'", response.Error)
	}
}

func TestErrorWithDetails(t *testing.T) {
	w := httptest.NewRecorder()
	details := map[string]interface{}{"field": "email", "reason": "invalid format"}

	ErrorWithDetails(w, http.StatusBadRequest, "Validation failed", details)

	var response Response
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if response.Success {
		t.Error("Expected success to be false")
	}

	if field, ok := response.Data["field"].(string); !ok || field != "email" {
		t.Errorf("Expected field 'email', got %v", response.Data["field"])
	}
}

func TestHelperFunctions(t *testing.T) {
	tests := []struct {
		name           string
		fn             func(http.ResponseWriter)
		expectedStatus int
	}{
		{"BadRequest", func(w http.ResponseWriter) { BadRequest(w, "bad") }, http.StatusBadRequest},
		{"NotFound", func(w http.ResponseWriter) { NotFound(w, "not found") }, http.StatusNotFound},
		{"Unauthorized", func(w http.ResponseWriter) { Unauthorized(w, "unauthorized") }, http.StatusUnauthorized},
		{"Forbidden", func(w http.ResponseWriter) { Forbidden(w, "forbidden") }, http.StatusForbidden},
		{"MethodNotAllowed", func(w http.ResponseWriter) { MethodNotAllowed(w) }, http.StatusMethodNotAllowed},
		{"ServiceUnavailable", func(w http.ResponseWriter) { ServiceUnavailable(w, "unavailable") }, http.StatusServiceUnavailable},
		{"Conflict", func(w http.ResponseWriter) { Conflict(w, "conflict") }, http.StatusConflict},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			tt.fn(w)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}
		})
	}
}

func TestDecodeJSON(t *testing.T) {
	type TestStruct struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}

	tests := []struct {
		name      string
		body      string
		expectErr bool
	}{
		{"valid JSON", `{"name":"test","value":42}`, false},
		{"invalid JSON", `{invalid}`, true},
		{"unknown fields", `{"name":"test","value":42,"unknown":"field"}`, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(tt.body))
			var result TestStruct

			err := DecodeJSON(req, &result)

			if tt.expectErr && err == nil {
				t.Error("Expected error, got nil")
			}
			if !tt.expectErr && err != nil {
				t.Errorf("Expected no error, got %v", err)
			}

			if !tt.expectErr {
				if result.Name != "test" {
					t.Errorf("Expected name 'test', got '%s'", result.Name)
				}
				if result.Value != 42 {
					t.Errorf("Expected value 42, got %d", result.Value)
				}
			}
		})
	}
}

func TestQueryParam(t *testing.T) {
	tests := []struct {
		name        string
		queryString string
		param       string
		expectValue string
		expectErr   bool
	}{
		{"param present", "?id=123", "id", "123", false},
		{"param missing", "", "id", "", true},
		{"multiple params", "?id=123&name=test", "name", "test", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/test"+tt.queryString, nil)
			value, err := QueryParam(req, tt.param)

			if tt.expectErr && err == nil {
				t.Error("Expected error, got nil")
			}
			if !tt.expectErr && err != nil {
				t.Errorf("Expected no error, got %v", err)
			}
			if !tt.expectErr && value != tt.expectValue {
				t.Errorf("Expected value '%s', got '%s'", tt.expectValue, value)
			}
		})
	}
}

func TestQueryParamDefault(t *testing.T) {
	tests := []struct {
		name         string
		queryString  string
		param        string
		defaultValue string
		expected     string
	}{
		{"param present", "?sort=name", "sort", "date", "name"},
		{"param missing", "", "sort", "date", "date"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/test"+tt.queryString, nil)
			value := QueryParamDefault(req, tt.param, tt.defaultValue)

			if value != tt.expected {
				t.Errorf("Expected '%s', got '%s'", tt.expected, value)
			}
		})
	}
}

func TestQueryParamInt(t *testing.T) {
	tests := []struct {
		name         string
		queryString  string
		param        string
		defaultValue int
		expected     int
	}{
		{"valid integer", "?page=5", "page", 1, 5},
		{"invalid integer", "?page=abc", "page", 1, 1},
		{"missing param", "", "page", 1, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/test"+tt.queryString, nil)
			value := QueryParamInt(req, tt.param, tt.defaultValue)

			if value != tt.expected {
				t.Errorf("Expected %d, got %d", tt.expected, value)
			}
		})
	}
}

func TestQueryParamIntRange(t *testing.T) {
	tests := []struct {
		name         string
		queryString  string
		param        string
		defaultValue int
		min          int
		max          int
		expected     int
	}{
		{"within range", "?limit=50", "limit", 10, 1, 100, 50},
		{"below min", "?limit=0", "limit", 10, 1, 100, 1},
		{"above max", "?limit=150", "limit", 10, 1, 100, 100},
		{"missing param", "", "limit", 10, 1, 100, 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/test"+tt.queryString, nil)
			value := QueryParamIntRange(req, tt.param, tt.defaultValue, tt.min, tt.max)

			if value != tt.expected {
				t.Errorf("Expected %d, got %d", tt.expected, value)
			}
		})
	}
}

func TestMethodGuard(t *testing.T) {
	tests := []struct {
		name           string
		method         string
		allowed        []string
		expectPass     bool
		expectedStatus int
	}{
		{"GET allowed", http.MethodGet, []string{http.MethodGet}, true, 0},
		{"POST not allowed", http.MethodPost, []string{http.MethodGet}, false, http.StatusMethodNotAllowed},
		{"Multiple allowed", http.MethodPost, []string{http.MethodGet, http.MethodPost}, true, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/test", nil)
			w := httptest.NewRecorder()

			result := MethodGuard(w, req, tt.allowed...)

			if result != tt.expectPass {
				t.Errorf("Expected pass=%v, got %v", tt.expectPass, result)
			}

			if !tt.expectPass && w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}
		})
	}
}

func TestCheckRequired(t *testing.T) {
	tests := []struct {
		name      string
		data      map[string]interface{}
		required  []string
		expectErr bool
	}{
		{
			name:      "all fields present",
			data:      map[string]interface{}{"name": "test", "value": 42},
			required:  []string{"name", "value"},
			expectErr: false,
		},
		{
			name:      "missing field",
			data:      map[string]interface{}{"name": "test"},
			required:  []string{"name", "value"},
			expectErr: true,
		},
		{
			name:      "empty string field",
			data:      map[string]interface{}{"name": ""},
			required:  []string{"name"},
			expectErr: true,
		},
		{
			name:      "no required fields",
			data:      map[string]interface{}{"name": "test"},
			required:  []string{},
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckRequired(tt.data, tt.required...)

			if tt.expectErr && err == nil {
				t.Error("Expected error, got nil")
			}
			if !tt.expectErr && err != nil {
				t.Errorf("Expected no error, got %v", err)
			}
		})
	}
}
