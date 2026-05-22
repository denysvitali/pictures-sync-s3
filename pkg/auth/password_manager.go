package auth

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/denysvitali/pictures-sync-s3/pkg/utils"
)

const (
	// #nosec G101 -- this is a well-known gokrazy system file path, not a credential
	DefaultGokrazyPasswordFile = "/perm/gokr-pw.txt"
	// #nosec G101 -- this is a well-known gokrazy system file path, not a credential
	RootGokrazyPasswordFile = "/etc/gokr-pw.txt"
	MinPasswordLength       = 8
	MaxPasswordLength       = 256
)

type PasswordManager struct {
	path     string
	fallback string
	mu       sync.RWMutex
	password string
}

func NewPasswordManager(path, fallback string) (*PasswordManager, error) {
	if strings.TrimSpace(path) == "" {
		path = DefaultGokrazyPasswordFile
	}
	if strings.TrimSpace(fallback) == "" {
		fallback = "dev"
	}

	manager := &PasswordManager{
		path:     path,
		fallback: fallback,
		password: fallback,
	}
	if err := manager.Load(); err != nil {
		return nil, err
	}
	return manager, nil
}

func (m *PasswordManager) CurrentPassword() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.password
}

func (m *PasswordManager) Path() string {
	return m.path
}

func (m *PasswordManager) Load() error {
	data, err := readPasswordFile(m.path)
	if err != nil {
		if os.IsNotExist(err) {
			m.mu.Lock()
			m.password = m.fallback
			m.mu.Unlock()
			return nil
		}
		return fmt.Errorf("read password file: %w", err)
	}

	password := strings.TrimSpace(string(data))
	if password == "" {
		password = m.fallback
	}

	m.mu.Lock()
	m.password = password
	m.mu.Unlock()
	return nil
}

func readPasswordFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err == nil || !os.IsNotExist(err) || path != DefaultGokrazyPasswordFile {
		return data, err
	}
	return os.ReadFile(RootGokrazyPasswordFile)
}

func CurrentGokrazyPassword(fallback string) string {
	if strings.TrimSpace(fallback) == "" {
		fallback = "dev"
	}
	for _, path := range []string{DefaultGokrazyPasswordFile, RootGokrazyPasswordFile} {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if password := strings.TrimSpace(string(data)); password != "" {
			return password
		}
	}
	return fallback
}

func (m *PasswordManager) ChangePassword(currentPassword, newPassword string) error {
	// SECURITY: use length-safe constant-time comparison. Plain
	// subtle.ConstantTimeCompare returns 0 immediately when slice
	// lengths differ, leaking the stored password's length via timing.
	m.mu.RLock()
	matches := constantTimeEqualString(currentPassword, m.password)
	m.mu.RUnlock()
	if !matches {
		return ErrCurrentPasswordInvalid
	}
	if err := ValidateGokrazyPassword(newPassword); err != nil {
		return err
	}
	if err := writePasswordFile(m.path, newPassword); err != nil {
		return err
	}

	m.mu.Lock()
	m.password = newPassword
	m.mu.Unlock()
	return nil
}

var ErrCurrentPasswordInvalid = errors.New("current password is incorrect")

func ValidateGokrazyPassword(password string) error {
	if password == "" {
		return errors.New("new password cannot be empty")
	}
	if strings.TrimSpace(password) != password {
		return errors.New("new password cannot have leading or trailing whitespace")
	}
	if len(password) < MinPasswordLength {
		return fmt.Errorf("new password must be at least %d characters", MinPasswordLength)
	}
	if len(password) > MaxPasswordLength {
		return fmt.Errorf("new password must not exceed %d characters", MaxPasswordLength)
	}
	if strings.ContainsAny(password, "\x00\r\n\t") {
		return errors.New("new password contains invalid control characters")
	}
	for i, r := range password {
		if r < 32 || r > 126 {
			return fmt.Errorf("new password contains invalid character at position %d (only printable ASCII characters allowed)", i)
		}
	}
	return nil
}

func writePasswordFile(path, password string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create password directory: %w", err)
	}

	if err := utils.AtomicWrite(path, []byte(password+"\n"), 0600); err != nil {
		return fmt.Errorf("replace password file: %w", err)
	}
	return nil
}
