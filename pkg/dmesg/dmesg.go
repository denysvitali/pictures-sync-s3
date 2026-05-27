// Package dmesg provides live kernel log tailing from /dev/kmsg
// with fallback to dmesg -w. It supports multiple concurrent subscribers
// via a pub-sub pattern matching the project's event/state managers.
package dmesg

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Line represents a single kernel log message.
type Line struct {
	Timestamp time.Time `json:"timestamp"`
	Text      string    `json:"text"`
	Level     int       `json:"level"`
}

// Manager manages live kernel log distribution.
type Manager struct {
	mu        sync.RWMutex
	listeners []*subscriber
	cancel    context.CancelFunc
	wg        sync.WaitGroup
}

type subscriber struct {
	mu     sync.Mutex
	ch     chan Line
	closed bool
}

func (s *subscriber) send(line Line) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	select {
	case s.ch <- line:
	default:
		// Drop if channel is full to prevent blocking
	}
}

func (s *subscriber) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	close(s.ch)
}

// NewManager creates a new dmesg manager. Call Start to begin tailing.
func NewManager() *Manager {
	return &Manager{
		listeners: make([]*subscriber, 0),
	}
}

// Subscribe returns a channel that receives kernel log lines.
func (m *Manager) Subscribe() chan Line {
	m.mu.Lock()
	defer m.mu.Unlock()

	ch := make(chan Line, 500) // Buffer for burst absorption
	m.listeners = append(m.listeners, &subscriber{ch: ch})
	return ch
}

// Unsubscribe removes a channel and closes it.
func (m *Manager) Unsubscribe(ch chan Line) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, listener := range m.listeners {
		if listener.ch == ch {
			m.listeners = append(m.listeners[:i], m.listeners[i+1:]...)
			listener.close()
			break
		}
	}
}

// Start begins tailing kernel logs. It is safe to call multiple times;
// subsequent calls restart the tailer.
func (m *Manager) Start() {
	m.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.run(ctx)
	}()
}

// Stop halts the tailer and waits for the reader goroutine to exit.
func (m *Manager) Stop() {
	if m.cancel != nil {
		m.cancel()
		m.wg.Wait()
		m.cancel = nil
	}
}

func (m *Manager) run(ctx context.Context) {
	// Prefer /dev/kmsg for true live kernel messages.
	// Fall back to dmesg -w if unavailable.
	if f, err := os.Open("/dev/kmsg"); err == nil {
		defer f.Close()
		m.readKMsg(ctx, f)
		return
	}

	m.readDmesg(ctx)
}

func (m *Manager) readKMsg(ctx context.Context, r io.Reader) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line := scanner.Text()
		parsed := parseKMsgLine(line)
		m.broadcast(parsed)
	}
}

func (m *Manager) readDmesg(ctx context.Context) {
	cmd := exec.CommandContext(ctx, "dmesg", "-w")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return
	}
	if err := cmd.Start(); err != nil {
		return
	}
	defer cmd.Wait()

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line := scanner.Text()
		parsed := parseDmesgLine(line)
		m.broadcast(parsed)
	}
}

func (m *Manager) broadcast(line Line) {
	m.mu.RLock()
	listenersCopy := make([]*subscriber, len(m.listeners))
	copy(listenersCopy, m.listeners)
	m.mu.RUnlock()

	for _, listener := range listenersCopy {
		listener.send(line)
	}
}

// parseKMsgLine parses a raw /dev/kmsg line.
// Format: priority,seqnum,timestamp[,flags];message
func parseKMsgLine(raw string) Line {
	result := Line{
		Timestamp: time.Now(),
		Text:      raw,
		Level:     -1,
	}

	semi := strings.Index(raw, ";")
	if semi < 0 {
		return result
	}

	prefix := raw[:semi]
	msg := raw[semi+1:]

	// prefix: priority,seqnum,timestamp[,flags]
	parts := strings.Split(prefix, ",")
	if len(parts) >= 3 {
		if lvl, err := strconv.Atoi(parts[0]); err == nil {
			result.Level = lvl & 0x7 // kernel level is lower 3 bits
		}
		// parts[1] = seqnum, parts[2] = timestamp (microseconds since boot)
		// We keep time.Now() as the wall-clock timestamp.
	}

	// Remove trailing newline if any
	result.Text = strings.TrimSuffix(msg, "\n")
	return result
}

// parseDmesgLine parses output from dmesg -w.
// Lines look like: [    0.000000] message  or  [Mon May 27 12:34:56 2024] message
func parseDmesgLine(raw string) Line {
	result := Line{
		Timestamp: time.Now(),
		Text:      raw,
		Level:     -1,
	}

	// Try to extract level from dmesg prefix like <6>
	if strings.HasPrefix(raw, "<") {
		if end := strings.Index(raw, ">"); end > 0 {
			if lvl, err := strconv.Atoi(raw[1:end]); err == nil {
				result.Level = lvl
				result.Text = strings.TrimSpace(raw[end+1:])
			}
		}
	}

	return result
}

// LevelName returns a human-readable name for a kernel log level.
func LevelName(level int) string {
	switch level {
	case 0:
		return "EMERG"
	case 1:
		return "ALERT"
	case 2:
		return "CRIT"
	case 3:
		return "ERR"
	case 4:
		return "WARN"
	case 5:
		return "NOTICE"
	case 6:
		return "INFO"
	case 7:
		return "DEBUG"
	default:
		return ""
	}
}

// FormatLine returns a dmesg-style formatted string for display.
func FormatLine(line Line) string {
	if line.Level >= 0 {
		return fmt.Sprintf("[%s] %s", LevelName(line.Level), line.Text)
	}
	return line.Text
}
