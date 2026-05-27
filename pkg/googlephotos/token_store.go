package googlephotos

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/denysvitali/pictures-sync-s3/pkg/utils"
)

// defaultTokenPath resolves the token file path at call time so that PERM_DIR
// overrides set by tests (after package init) are respected.
func defaultTokenPath() string {
	if base := os.Getenv("PERM_DIR"); base != "" {
		return filepath.Join(base, "pictures-sync", "google-photos-token.json")
	}
	return "/perm/pictures-sync/google-photos-token.json"
}

// TokenStore manages OAuth token persistence
type TokenStore struct {
	mu       sync.RWMutex
	filePath string
	token    *OAuthToken
}

// NewTokenStore creates a new token store
func NewTokenStore(filePath string) *TokenStore {
	if filePath == "" {
		filePath = defaultTokenPath()
	}
	return &TokenStore{
		filePath: filePath,
	}
}

// Load reads the token from disk
func (ts *TokenStore) Load() (*OAuthToken, error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	data, err := os.ReadFile(ts.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read token file: %w", err)
	}

	var token OAuthToken
	if err := json.Unmarshal(data, &token); err != nil {
		return nil, fmt.Errorf("failed to parse token file: %w", err)
	}

	ts.token = &token
	return &token, nil
}

// Save persists the token to disk atomically with restricted permissions
func (ts *TokenStore) Save(token *OAuthToken) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	ts.token = token

	// Ensure parent directory exists
	if err := utils.EnsureDir(filepath.Dir(ts.filePath), 0750); err != nil {
		return err
	}

	data, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal token: %w", err)
	}

	// Write with 0600 permissions (only owner can read)
	return utils.AtomicWrite(ts.filePath, data, 0600)
}

// Delete removes the stored token
func (ts *TokenStore) Delete() error {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	ts.token = nil
	if err := os.Remove(ts.filePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete token file: %w", err)
	}
	return nil
}

// Get returns the current token without reloading from disk
func (ts *TokenStore) Get() *OAuthToken {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return ts.token
}

// HasToken returns true if a token is stored
func (ts *TokenStore) HasToken() bool {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	if ts.token != nil && ts.token.RefreshToken != "" {
		return true
	}
	// Also check disk
	_, err := os.Stat(ts.filePath)
	return err == nil
}
