package netwatchdog

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// persistLog appends timestamped lines to a file, rotating to file.1 when the
// active file exceeds maxBytes. Errors are silently dropped — the watchdog must
// keep running even if /perm is unavailable (e.g. local dev).
type persistLog struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	now      func() time.Time
}

func newPersistLog(path string, maxBytes int64) *persistLog {
	return &persistLog{path: path, maxBytes: maxBytes, now: time.Now}
}

func (p *persistLog) Writef(format string, args ...any) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(p.path), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(p.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	line := fmt.Sprintf("%s %s\n",
		p.now().UTC().Format(time.RFC3339),
		fmt.Sprintf(format, args...))
	_, _ = f.WriteString(line)
	info, statErr := f.Stat()
	_ = f.Close()
	if statErr == nil && p.maxBytes > 0 && info.Size() > p.maxBytes {
		_ = os.Rename(p.path, p.path+".1")
	}
}
