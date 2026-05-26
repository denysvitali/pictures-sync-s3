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
