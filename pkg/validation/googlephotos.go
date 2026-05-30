package validation

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// GooglePhotosToken is the rclone-compatible token format for the googlephotos backend
type GooglePhotosToken struct {
	AccessToken  string    `json:"access_token"`
	TokenType    string    `json:"token_type"`
	RefreshToken string    `json:"refresh_token"`
	Expiry       time.Time `json:"expiry"`
}

// BuildGooglePhotosRcloneConfig generates rclone.conf INI content for Google Photos.
// The token is serialised in the same JSON format rclone uses internally.
func BuildGooglePhotosRcloneConfig(remoteName, clientID, clientSecret string, token *GooglePhotosToken) ([]byte, error) {
	if strings.TrimSpace(remoteName) == "" {
		remoteName = "gphotos"
	}
	if err := isValidSectionName(remoteName); !err {
		return nil, fmt.Errorf("invalid googlephotos remote name: %s", remoteName)
	}
	if strings.TrimSpace(clientID) == "" {
		return nil, fmt.Errorf("google photos client id is required")
	}
	if strings.TrimSpace(clientSecret) == "" {
		return nil, fmt.Errorf("google photos client secret is required")
	}
	if token == nil || token.RefreshToken == "" {
		return nil, fmt.Errorf("google photos token with refresh token is required")
	}

	tokenJSON, err := json.Marshal(token)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal token: %w", err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "[%s]\n", remoteName)
	fmt.Fprintf(&b, "type = googlephotos\n")
	fmt.Fprintf(&b, "client_id = %s\n", strings.TrimSpace(clientID))
	fmt.Fprintf(&b, "client_secret = %s\n", strings.TrimSpace(clientSecret))
	fmt.Fprintf(&b, "token = %s\n", string(tokenJSON))

	return []byte(b.String()), nil
}

// ParseGooglePhotosRcloneConfig extracts the client_id and client_secret from
// the named googlephotos remote in rclone.conf INI content. It returns empty
// strings when the section or keys are absent.
//
// This is the read counterpart to BuildGooglePhotosRcloneConfig: it lets the
// native Google Photos API client operate under the exact same OAuth client_id
// that rclone uploads with, so app-side album operations can see every item
// rclone wrote (the Library API only exposes media created by the requesting
// OAuth client).
func ParseGooglePhotosRcloneConfig(data []byte, remoteName string) (clientID, clientSecret string) {
	remoteName = strings.TrimSpace(remoteName)
	if remoteName == "" {
		return "", ""
	}

	inSection := false
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			name := strings.TrimSpace(line[1 : len(line)-1])
			inSection = name == remoteName
			continue
		}
		if !inSection {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "client_id":
			clientID = strings.TrimSpace(value)
		case "client_secret":
			clientSecret = strings.TrimSpace(value)
		}
	}
	return clientID, clientSecret
}
