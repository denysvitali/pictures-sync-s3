package validation

import (
	"testing"
	"time"
)

func TestParseGooglePhotosRcloneConfig(t *testing.T) {
	tests := []struct {
		name       string
		config     string
		remote     string
		wantID     string
		wantSecret string
	}{
		{
			name: "extracts creds from named section",
			config: `[b2]
type = b2
account = abc

[gphotos]
type = googlephotos
client_id = my-id.apps.googleusercontent.com
client_secret = GOCSPX-secret
token = {"access_token":"x","refresh_token":"y"}
`,
			remote:     "gphotos",
			wantID:     "my-id.apps.googleusercontent.com",
			wantSecret: "GOCSPX-secret",
		},
		{
			name: "ignores keys from other sections",
			config: `[other]
client_id = wrong-id
client_secret = wrong-secret

[gphotos]
type = googlephotos
client_id = right-id
`,
			remote:     "gphotos",
			wantID:     "right-id",
			wantSecret: "",
		},
		{
			name: "handles no spaces around equals and comments",
			config: `[gphotos]
; a comment
client_id=tight-id
# another comment
client_secret =  spaced-secret
`,
			remote:     "gphotos",
			wantID:     "tight-id",
			wantSecret: "spaced-secret",
		},
		{
			name:       "missing section returns empty",
			config:     "[b2]\ntype = b2\n",
			remote:     "gphotos",
			wantID:     "",
			wantSecret: "",
		},
		{
			name:       "empty remote name returns empty",
			config:     "[gphotos]\nclient_id = id\n",
			remote:     "",
			wantID:     "",
			wantSecret: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, secret := ParseGooglePhotosRcloneConfig([]byte(tt.config), tt.remote)
			if id != tt.wantID {
				t.Errorf("client_id = %q, want %q", id, tt.wantID)
			}
			if secret != tt.wantSecret {
				t.Errorf("client_secret = %q, want %q", secret, tt.wantSecret)
			}
		})
	}
}

// TestGooglePhotosRcloneConfigRoundTrip verifies the parser reads back exactly
// what the builder writes, so the native client and the rclone remote share an
// identical client_id (the whole point of unifying them).
func TestGooglePhotosRcloneConfigRoundTrip(t *testing.T) {
	const (
		remote = "gphotos"
		id     = "round-trip-id.apps.googleusercontent.com"
		secret = "GOCSPX-round-trip"
	)
	token := &GooglePhotosToken{
		AccessToken:  "access",
		TokenType:    "Bearer",
		RefreshToken: "refresh",
		Expiry:       time.Now().Add(time.Hour),
	}

	data, err := BuildGooglePhotosRcloneConfig(remote, id, secret, token)
	if err != nil {
		t.Fatalf("BuildGooglePhotosRcloneConfig: %v", err)
	}

	gotID, gotSecret := ParseGooglePhotosRcloneConfig(data, remote)
	if gotID != id {
		t.Errorf("round-trip client_id = %q, want %q", gotID, id)
	}
	if gotSecret != secret {
		t.Errorf("round-trip client_secret = %q, want %q", gotSecret, secret)
	}
}
