package validation

import (
	"errors"
	"fmt"
	"strings"
)

// B2Config contains Backblaze B2 configuration fields
type B2Config struct {
	Account       string
	Key           string
	Bucket        string
	RemoteName    string
	RemotePath    string
	Endpoint      string
}

// ValidateB2Config validates B2 configuration fields
func ValidateB2Config(cfg *B2Config) error {
	if cfg == nil {
		return errors.New("B2 config is nil")
	}

	account := strings.TrimSpace(cfg.Account)
	if account == "" {
		return errors.New("B2 account ID is required")
	}
	if len(account) > 64 {
		return errors.New("B2 account ID is too long (max 64 characters)")
	}
	if strings.ContainsAny(account, "\x00\r\n") {
		return errors.New("B2 account ID contains invalid characters")
	}

	key := strings.TrimSpace(cfg.Key)
	if key == "" {
		return errors.New("B2 application key is required")
	}
	if len(key) > 256 {
		return errors.New("B2 application key is too long (max 256 characters)")
	}
	if strings.ContainsAny(key, "\x00\r\n") {
		return errors.New("B2 application key contains invalid characters")
	}

	bucket := strings.TrimSpace(cfg.Bucket)
	if bucket == "" {
		return errors.New("B2 bucket name is required")
	}
	if len(bucket) > 256 {
		return errors.New("B2 bucket name is too long (max 256 characters)")
	}
	if strings.ContainsAny(bucket, "\x00\r\n") {
		return errors.New("B2 bucket name contains invalid characters")
	}

	remoteName := strings.TrimSpace(cfg.RemoteName)
	if remoteName == "" {
		remoteName = "b2"
	}
	if err := isValidSectionName(remoteName); !err {
		return fmt.Errorf("invalid remote name: %s", remoteName)
	}

	remotePath := strings.TrimSpace(cfg.RemotePath)
	if remotePath == "" {
		remotePath = "/photos"
	}
	if strings.Contains(remotePath, "..") {
		return errors.New("remote path contains path traversal sequences")
	}

	if cfg.Endpoint != "" {
		endpoint := strings.TrimSpace(cfg.Endpoint)
		if !strings.HasPrefix(endpoint, "https://") && !strings.HasPrefix(endpoint, "http://") {
			return errors.New("B2 endpoint must be a valid HTTP(S) URL")
		}
	}

	return nil
}

// BuildB2RcloneConfig generates rclone.conf INI content for Backblaze B2
func BuildB2RcloneConfig(cfg *B2Config) ([]byte, error) {
	if err := ValidateB2Config(cfg); err != nil {
		return nil, err
	}

	remoteName := strings.TrimSpace(cfg.RemoteName)
	if remoteName == "" {
		remoteName = "b2"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "[%s]\n", remoteName)
	fmt.Fprintf(&b, "type = b2\n")
	fmt.Fprintf(&b, "account = %s\n", strings.TrimSpace(cfg.Account))
	fmt.Fprintf(&b, "key = %s\n", strings.TrimSpace(cfg.Key))

	if cfg.Endpoint != "" {
		fmt.Fprintf(&b, "endpoint = %s\n", strings.TrimSpace(cfg.Endpoint))
	}

	return []byte(b.String()), nil
}
