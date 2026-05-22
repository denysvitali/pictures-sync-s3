package auth

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// TestPasswordManagerChangePasswordConcurrent proves that concurrent
// ChangePassword callers cannot interleave their verify/write phases. The
// implementation must serialize change attempts so the in-memory password
// and the on-disk file always agree and only one new password ultimately
// wins.
func TestPasswordManagerChangePasswordConcurrent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gokr-pw.txt")
	if err := os.WriteFile(path, []byte("start-password\n"), 0600); err != nil {
		t.Fatalf("write password file: %v", err)
	}

	manager, err := NewPasswordManager(path, "dev-password")
	if err != nil {
		t.Fatalf("NewPasswordManager() error = %v", err)
	}

	// Spawn many goroutines that all try to change from the same starting
	// password to a distinct new password. Exactly one must succeed; all
	// others must observe ErrCurrentPasswordInvalid because the in-memory
	// password is updated atomically with the file write under the lock.
	const workers = 32
	var (
		wg          sync.WaitGroup
		mu          sync.Mutex
		successes   int
		successPass string
	)
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		i := i
		go func() {
			defer wg.Done()
			newPass := "winner-password-" + string(rune('A'+i))
			err := manager.ChangePassword("start-password", newPass)
			if err == nil {
				mu.Lock()
				successes++
				successPass = newPass
				mu.Unlock()
				return
			}
			if err != ErrCurrentPasswordInvalid {
				t.Errorf("unexpected error from ChangePassword: %v", err)
			}
		}()
	}
	wg.Wait()

	if successes != 1 {
		t.Fatalf("expected exactly 1 successful concurrent ChangePassword, got %d", successes)
	}

	// The in-memory password must equal the on-disk password, and both
	// must equal whichever attempt won the race.
	if got := manager.CurrentPassword(); got != successPass {
		t.Fatalf("CurrentPassword() = %q, want %q", got, successPass)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read password file: %v", err)
	}
	if got := strings.TrimRight(string(data), "\n"); got != successPass {
		t.Fatalf("on-disk password = %q, want %q (must match in-memory)", got, successPass)
	}
}
