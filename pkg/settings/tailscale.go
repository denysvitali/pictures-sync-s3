package settings

import (
	"bytes"
	"os"
	"path/filepath"

	"github.com/denysvitali/pictures-sync-s3/pkg/utils"
)

const (
	TailscaleAuthKeyFile = "/perm/tailscale/auth_key"
)

// SaveTailscaleAuthKey persists the Tailscale auth key in /perm.
func SaveTailscaleAuthKey(authKey string) error {
	return SaveTailscaleAuthKeyTo(TailscaleAuthKeyFile, authKey)
}

// SaveTailscaleAuthKeyTo persists the Tailscale auth key to a specific path.
func SaveTailscaleAuthKeyTo(path, authKey string) error {
	if err := ValidateTailscaleAuthKey(authKey); err != nil {
		return err
	}
	if err := utils.EnsureDir(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return utils.AtomicWrite(path, []byte(authKey+"\n"), 0600)
}

// HasTailscaleAuthKey reports whether a non-empty auth key has been stored.
func HasTailscaleAuthKey() (bool, error) {
	return HasTailscaleAuthKeyAt(TailscaleAuthKeyFile)
}

// HasTailscaleAuthKeyAt reports whether a non-empty auth key exists at path.
func HasTailscaleAuthKeyAt(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return len(bytes.TrimSpace(data)) > 0, nil
}
