package validation

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// B2Region represents a Backblaze B2 region with its S3-compatible endpoint.
type B2Region struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Endpoint string `json:"endpoint"`
}

// B2Regions lists the known Backblaze B2 regions with their S3-compatible endpoints.
var B2Regions = []B2Region{
	{ID: "us-west-004", Name: "US West (us-west-004)", Endpoint: "https://s3.us-west-004.backblazeb2.com"},
	{ID: "us-west-002", Name: "US West (us-west-002)", Endpoint: "https://s3.us-west-002.backblazeb2.com"},
	{ID: "us-east-005", Name: "US East (us-east-005)", Endpoint: "https://s3.us-east-005.backblazeb2.com"},
	{ID: "eu-central-003", Name: "EU Central (eu-central-003)", Endpoint: "https://s3.eu-central-003.backblazeb2.com"},
	{ID: "ap-southeast-001", Name: "Asia Pacific (ap-southeast-001)", Endpoint: "https://s3.ap-southeast-001.backblazeb2.com"},
}

var bucketNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9\-]*[a-z0-9]$`)

// B2Config contains Backblaze B2 configuration fields
type B2Config struct {
	Account    string
	Key        string
	Bucket     string
	RemoteName string
	RemotePath string
	Endpoint   string
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
	if err := ValidateB2BucketName(bucket); err != nil {
		return err
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

// ValidateB2BucketName checks a bucket name against Backblaze B2 naming rules:
// - 3-63 characters long
// - Lowercase letters, digits, and hyphens only
// - Must start and end with a letter or digit
// - Must not contain consecutive hyphens
func ValidateB2BucketName(name string) error {
	if len(name) < 3 {
		return errors.New("B2 bucket name must be at least 3 characters")
	}
	if len(name) > 63 {
		return errors.New("B2 bucket name must be at most 63 characters")
	}
	if strings.ContainsAny(name, "\x00\r\n") {
		return errors.New("B2 bucket name contains invalid characters")
	}
	if !bucketNameRe.MatchString(name) {
		return errors.New("B2 bucket name must start and end with a lowercase letter or digit, and contain only lowercase letters, digits, and hyphens")
	}
	if strings.Contains(name, "--") {
		return errors.New("B2 bucket name must not contain consecutive hyphens")
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
	// The native rclone B2 backend normally discovers the correct API endpoint
	// from the application key. S3-compatible bucket endpoints do not belong in
	// this field and cause opaque HTML 400 responses during authorization.

	return []byte(b.String()), nil
}
