package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/denysvitali/pictures-sync-s3/pkg/httputil"
	"github.com/denysvitali/pictures-sync-s3/pkg/utils"
	"golang.org/x/crypto/ssh"
)

const (
	defaultBreakglassAuthorizedKeysPath = "/perm/breakglass/authorized_keys"
	maxAuthorizedKeysSize               = 64 * 1024
)

var breakglassAuthorizedKeysPath = defaultBreakglassAuthorizedKeysPath

// HandleBreakglassAuthorizedKeys manages the persistent authorized_keys file
// used by github.com/gokrazy/breakglass.
func (ctx *Context) HandleBreakglassAuthorizedKeys(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		keys, err := readBreakglassAuthorizedKeys()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		httputil.JSON(w, http.StatusOK, map[string]any{
			"authorized_keys": keys,
			"path":            breakglassAuthorizedKeysPath,
			"count":           countAuthorizedKeys(keys),
		})

	case http.MethodPost:
		var req struct {
			AuthorizedKeys string `json:"authorized_keys"`
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, maxAuthorizedKeysSize+1))
		if err != nil {
			http.Error(w, "Failed to read body", http.StatusBadRequest)
			return
		}
		if len(body) > maxAuthorizedKeysSize {
			http.Error(w, "authorized_keys file is too large", http.StatusRequestEntityTooLarge)
			return
		}
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		keys, err := normalizeAuthorizedKeys(req.AuthorizedKeys)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "error",
				"error":  err.Error(),
			})
			return
		}

		if err := writeBreakglassAuthorizedKeys(keys); err != nil {
			http.Error(w, fmt.Sprintf("Failed to write authorized_keys: %v", err), http.StatusInternalServerError)
			return
		}

		httputil.JSON(w, http.StatusOK, map[string]any{
			"status": "ok",
			"path":   breakglassAuthorizedKeysPath,
			"count":  countAuthorizedKeys(keys),
		})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func readBreakglassAuthorizedKeys() (string, error) {
	data, err := os.ReadFile(breakglassAuthorizedKeysPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("failed to read authorized_keys: %w", err)
	}
	return string(data), nil
}

func writeBreakglassAuthorizedKeys(keys string) error {
	if err := os.MkdirAll(filepath.Dir(breakglassAuthorizedKeysPath), 0700); err != nil {
		return fmt.Errorf("failed to create breakglass directory: %w", err)
	}
	return utils.AtomicWrite(breakglassAuthorizedKeysPath, []byte(keys), 0600)
}

func normalizeAuthorizedKeys(raw string) (string, error) {
	var normalized []string
	for lineNumber, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if _, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line)); err != nil {
			return "", fmt.Errorf("line %d is not a valid SSH authorized key", lineNumber+1)
		}
		normalized = append(normalized, line)
	}

	if len(normalized) == 0 {
		return "", nil
	}

	return strings.Join(normalized, "\n") + "\n", nil
}

func countAuthorizedKeys(keys string) int {
	count := 0
	for _, line := range strings.Split(keys, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			count++
		}
	}
	return count
}
