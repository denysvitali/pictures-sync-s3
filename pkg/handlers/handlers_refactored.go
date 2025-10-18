package handlers

import (
	"net/http"

	"github.com/denysvitali/pictures-sync-s3/pkg/httputil"
	"github.com/denysvitali/pictures-sync-s3/pkg/middleware"
)

// Example of refactored handlers using the new middleware and httputil packages
// These demonstrate the improved patterns without breaking existing functionality

// RefactoredHandleStatus is an example of status handler using new patterns
func (ctx *Context) RefactoredHandleStatus() http.HandlerFunc {
	handler := func(w http.ResponseWriter, r *http.Request) error {
		// Reload state from disk to get latest updates from pictures-sync service
		if err := ctx.StateMgr.Reload(); err != nil {
			// Log is handled by middleware, just return error
			return err
		}

		status := ctx.StateMgr.GetState()
		httputil.JSON(w, http.StatusOK, status)
		return nil
	}

	// Apply middleware chain: recovery -> request logging -> method validation
	wrapped := middleware.Chain(
		middleware.Recovery,
		middleware.RequestLogger,
		middleware.MethodOnly(http.MethodGet),
	)(handler)

	return middleware.Adapt(wrapped)
}

// RefactoredHandleHistory demonstrates clean history retrieval
func (ctx *Context) RefactoredHandleHistory() http.HandlerFunc {
	handler := func(w http.ResponseWriter, r *http.Request) error {
		history := ctx.StateMgr.GetHistory()
		httputil.JSON(w, http.StatusOK, history)
		return nil
	}

	wrapped := middleware.Chain(
		middleware.Recovery,
		middleware.MethodOnly(http.MethodGet),
	)(handler)

	return middleware.Adapt(wrapped)
}

// RefactoredHandleWiFiConnect demonstrates request validation and error handling
func (ctx *Context) RefactoredHandleWiFiConnect() http.HandlerFunc {
	type ConnectRequest struct {
		SSID     string `json:"ssid"`
		Password string `json:"password"`
	}

	handler := func(w http.ResponseWriter, r *http.Request) error {
		// Check WiFi manager availability
		if ctx.WiFiMgr == nil {
			httputil.ServiceUnavailable(w, "WiFi manager not initialized")
			return nil
		}

		// Decode and validate request
		var req ConnectRequest
		if err := httputil.DecodeJSON(r, &req); err != nil {
			httputil.BadRequest(w, "Invalid JSON: "+err.Error())
			return nil
		}

		// Validate required fields
		if req.SSID == "" {
			httputil.BadRequest(w, "SSID is required")
			return nil
		}

		// Execute business logic
		if err := ctx.WiFiMgr.AddNetwork(req.SSID, req.Password); err != nil {
			httputil.Error(w, http.StatusInternalServerError, err.Error())
			return nil
		}

		// Success response
		httputil.SuccessWithMessage(w, "Connected to network", map[string]interface{}{
			"ssid": req.SSID,
		})
		return nil
	}

	wrapped := middleware.Chain(
		middleware.Recovery,
		middleware.MethodOnly(http.MethodPost),
	)(handler)

	return middleware.Adapt(wrapped)
}

// RefactoredHandleDeviceSelect demonstrates comprehensive validation
func (ctx *Context) RefactoredHandleDeviceSelect() http.HandlerFunc {
	type SelectRequest struct {
		DevicePath string `json:"device_path"`
	}

	handler := func(w http.ResponseWriter, r *http.Request) error {
		var req SelectRequest
		if err := httputil.DecodeJSON(r, &req); err != nil {
			httputil.BadRequest(w, "Invalid JSON: "+err.Error())
			return nil
		}

		if req.DevicePath == "" {
			httputil.BadRequest(w, "device_path is required")
			return nil
		}

		// TODO: Trigger sync for the selected device
		// This needs to be integrated with the main pictures-sync service

		httputil.SuccessWithMessage(w,
			"Device selection received. Integration with sync service pending.",
			map[string]interface{}{
				"device_path": req.DevicePath,
			})
		return nil
	}

	wrapped := middleware.Chain(
		middleware.Recovery,
		middleware.MethodOnly(http.MethodPost),
	)(handler)

	return middleware.Adapt(wrapped)
}

// RefactoredHandleFileView demonstrates query parameter handling
func (ctx *Context) RefactoredHandleFileView() http.HandlerFunc {
	handler := func(w http.ResponseWriter, r *http.Request) error {
		// Use helper to get required query parameter
		filePath, err := httputil.QueryParam(r, "path")
		if err != nil {
			httputil.BadRequest(w, err.Error())
			return nil
		}

		// Validate file type and set content type
		contentType, err := getImageContentType(filePath)
		if err != nil {
			httputil.BadRequest(w, err.Error())
			return nil
		}

		// Set headers
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "public, max-age=3600")

		// Stream the file from remote
		if err := ctx.SyncMgr.GetFile(filePath, w); err != nil {
			return err // Let middleware handle internal error
		}

		return nil
	}

	wrapped := middleware.Chain(
		middleware.Recovery,
		middleware.MethodOnly(http.MethodGet),
		middleware.RequireQueryParam("path"),
	)(handler)

	return middleware.Adapt(wrapped)
}

// RefactoredHandleFilesPaginated demonstrates pagination with validation
func (ctx *Context) RefactoredHandleFilesPaginated() http.HandlerFunc {
	handler := func(w http.ResponseWriter, r *http.Request) error {
		// Get path from query (optional)
		path := httputil.QueryParamDefault(r, "path", "")

		// Get pagination params with range validation
		page := httputil.QueryParamIntRange(r, "page", 1, 1, 10000)
		pageSize := httputil.QueryParamIntRange(r, "page_size", 100, 1, 1000)

		// Execute query
		result, err := ctx.SyncMgr.ListFilesPaginated(path, page, pageSize)
		if err != nil {
			return err // Let middleware handle
		}

		httputil.JSON(w, http.StatusOK, result)
		return nil
	}

	wrapped := middleware.Chain(
		middleware.Recovery,
		middleware.MethodOnly(http.MethodGet),
	)(handler)

	return middleware.Adapt(wrapped)
}

// RefactoredHandleWiFiScan demonstrates handling optional request body
func (ctx *Context) RefactoredHandleWiFiScan() http.HandlerFunc {
	type ScanRequest struct {
		SortBy string `json:"sort_by,omitempty"` // "signal", "name", "security"
	}

	handler := func(w http.ResponseWriter, r *http.Request) error {
		if ctx.WiFiMgr == nil {
			httputil.ServiceUnavailable(w, "WiFi manager not initialized")
			return nil
		}

		// Parse optional request body for sorting options
		var req ScanRequest
		if r.Body != nil {
			// Ignore errors for backward compatibility with empty body
			_ = httputil.DecodeJSON(r, &req)
		}

		// Scan networks
		networks, err := ctx.WiFiMgr.ScanNetworks()
		if err != nil {
			return err
		}

		// Apply sorting
		sortedNetworks := sortWiFiNetworks(networks, req.SortBy)

		httputil.Success(w, map[string]interface{}{
			"networks": sortedNetworks,
		})
		return nil
	}

	wrapped := middleware.Chain(
		middleware.Recovery,
		middleware.MethodOnly(http.MethodPost),
	)(handler)

	return middleware.Adapt(wrapped)
}

// Helper function for content type detection (used in refactored handlers)
func getImageContentType(filePath string) (string, error) {
	ext := getFileExtension(filePath)
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg", nil
	case ".png":
		return "image/png", nil
	case ".gif":
		return "image/gif", nil
	case ".webp":
		return "image/webp", nil
	default:
		return "", httputil.CheckRequired(map[string]interface{}{}, "valid_image_extension")
	}
}

// Helper to get lowercase file extension
func getFileExtension(path string) string {
	for i := len(path) - 1; i >= 0 && i > len(path)-6; i-- {
		if path[i] == '.' {
			result := path[i:]
			// Convert to lowercase manually to avoid import
			lower := make([]byte, len(result))
			for j := 0; j < len(result); j++ {
				if result[j] >= 'A' && result[j] <= 'Z' {
					lower[j] = result[j] + 32
				} else {
					lower[j] = result[j]
				}
			}
			return string(lower)
		}
	}
	return ""
}
