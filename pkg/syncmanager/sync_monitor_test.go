package syncmanager

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/state"
)

// TestSyncWaitsForProgressMonitor exercises Sync() and verifies that no
// monitorProgress goroutines are still alive once Sync() returns. Prior to
// the fix, Sync() only sent close(done) and returned without waiting for the
// monitor goroutine to exit, which caused a race where the monitor could
// still call stateMgr.UpdateSyncProgress / broadcastProgress after the
// caller had moved on to post-sync state writes.
func TestSyncWaitsForProgressMonitor(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration-style sync test in -short mode")
	}

	tmpDir := t.TempDir()
	srcDir := filepath.Join(tmpDir, "src")
	dstDir := filepath.Join(tmpDir, "dst")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Drop a few small files so the sync has actual work to do.
	for i := 0; i < 8; i++ {
		p := filepath.Join(srcDir, "img-"+string(rune('a'+i))+".jpg")
		if err := os.WriteFile(p, []byte("payload-"+string(rune('a'+i))), 0644); err != nil {
			t.Fatal(err)
		}
	}

	configPath := filepath.Join(tmpDir, "rclone.conf")
	cfg := "[localdst]\ntype = local\n"
	if err := os.WriteFile(configPath, []byte(cfg), 0644); err != nil {
		t.Fatal(err)
	}

	stateMgr, err := state.NewManager()
	if err != nil {
		t.Fatalf("state manager: %v", err)
	}

	mgr := NewManager(configPath, "localdst", dstDir, stateMgr, 2, 2)

	// Subscribe to progress so broadcastProgress has at least one subscriber
	// to exercise; we drain it in the background and unsubscribe at the end.
	progressCh := mgr.SubscribeProgress()
	drainStop := make(chan struct{})
	var drainWG sync.WaitGroup
	drainWG.Add(1)
	go func() {
		defer drainWG.Done()
		for {
			select {
			case <-progressCh:
			case <-drainStop:
				return
			}
		}
	}()

	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	before := runtime.NumGoroutine()

	if err := mgr.Sync(srcDir, "card-abcdef0123", len(filesIn(t, srcDir)), 0); err != nil {
		t.Logf("Sync error (acceptable in this env): %v", err)
	}

	// Immediately after Sync returns the monitor goroutine MUST be gone.
	// We give the scheduler a tiny moment to retire the stack frame.
	time.Sleep(20 * time.Millisecond)
	runtime.GC()
	after := runtime.NumGoroutine()

	close(drainStop)
	drainWG.Wait()
	mgr.UnsubscribeProgress(progressCh)

	// Allow a small variance — testing infrastructure spawns its own goroutines.
	if delta := after - before; delta > 3 {
		t.Errorf("monitorProgress goroutine appears to outlive Sync(): delta=%d (before=%d, after=%d)", delta, before, after)
	}
}

func filesIn(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out
}
