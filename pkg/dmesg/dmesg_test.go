package dmesg

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------- parseKMsgLine ----------

func TestParseKMsgLine(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		wantLevel int
		wantText  string
	}{
		{
			name:      "valid info line",
			raw:       "6,123,456789,-;hello world",
			wantLevel: 6,
			wantText:  "hello world",
		},
		{
			name:      "level masks lower 3 bits",
			raw:       "14,1,0,-;masked", // 14 & 7 = 6
			wantLevel: 6,
			wantText:  "masked",
		},
		{
			name:      "trailing newline trimmed",
			raw:       "3,1,2,-;error message\n",
			wantLevel: 3,
			wantText:  "error message",
		},
		{
			name:      "no semicolon returns raw",
			raw:       "no-semicolon-here",
			wantLevel: -1,
			wantText:  "no-semicolon-here",
		},
		{
			name:      "non-numeric level",
			raw:       "abc,1,2,-;weird",
			wantLevel: -1,
			wantText:  "weird",
		},
		{
			name:      "too few prefix parts",
			raw:       "6;short",
			wantLevel: -1,
			wantText:  "short",
		},
		{
			name:      "empty message body",
			raw:       "4,1,1,-;",
			wantLevel: 4,
			wantText:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseKMsgLine(tt.raw)
			if got.Level != tt.wantLevel {
				t.Errorf("level: got %d want %d", got.Level, tt.wantLevel)
			}
			if got.Text != tt.wantText {
				t.Errorf("text: got %q want %q", got.Text, tt.wantText)
			}
			if got.Timestamp.IsZero() {
				t.Error("timestamp should be set")
			}
		})
	}
}

// ---------- parseDmesgLine ----------

func TestParseDmesgLine(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		wantLevel int
		wantText  string
	}{
		{
			name:      "with level prefix",
			raw:       "<6>some message",
			wantLevel: 6,
			wantText:  "some message",
		},
		{
			name:      "without level prefix",
			raw:       "[    0.000000] boot message",
			wantLevel: -1,
			wantText:  "[    0.000000] boot message",
		},
		{
			name:      "malformed level",
			raw:       "<abc>oops",
			wantLevel: -1,
			wantText:  "<abc>oops",
		},
		{
			name:      "level with no closing",
			raw:       "<7 unclosed",
			wantLevel: -1,
			wantText:  "<7 unclosed",
		},
		{
			name:      "empty string",
			raw:       "",
			wantLevel: -1,
			wantText:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseDmesgLine(tt.raw)
			if got.Level != tt.wantLevel {
				t.Errorf("level: got %d want %d", got.Level, tt.wantLevel)
			}
			if got.Text != tt.wantText {
				t.Errorf("text: got %q want %q", got.Text, tt.wantText)
			}
		})
	}
}

// ---------- LevelName ----------

func TestLevelName(t *testing.T) {
	cases := map[int]string{
		0:  "EMERG",
		1:  "ALERT",
		2:  "CRIT",
		3:  "ERR",
		4:  "WARN",
		5:  "NOTICE",
		6:  "INFO",
		7:  "DEBUG",
		-1: "",
		99: "",
	}
	for level, want := range cases {
		if got := LevelName(level); got != want {
			t.Errorf("LevelName(%d) = %q want %q", level, got, want)
		}
	}
}

// ---------- FormatLine ----------

func TestFormatLine(t *testing.T) {
	tests := []struct {
		name string
		line Line
		want string
	}{
		{"with level", Line{Level: 6, Text: "hello"}, "[INFO] hello"},
		{"level zero", Line{Level: 0, Text: "panic"}, "[EMERG] panic"},
		{"no level", Line{Level: -1, Text: "plain"}, "plain"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatLine(tt.line); got != tt.want {
				t.Errorf("got %q want %q", got, tt.want)
			}
		})
	}
}

// ---------- NewManager / Subscribe / Unsubscribe ----------

func TestNewManager(t *testing.T) {
	m := NewManager()
	if m == nil {
		t.Fatal("NewManager returned nil")
	}
	if m.listeners == nil {
		t.Error("listeners not initialized")
	}
	if len(m.listeners) != 0 {
		t.Errorf("expected 0 listeners, got %d", len(m.listeners))
	}
}

func TestSubscribe(t *testing.T) {
	m := NewManager()
	ch := m.Subscribe()
	if ch == nil {
		t.Fatal("Subscribe returned nil")
	}
	if cap(ch) != 500 {
		t.Errorf("expected buffer 500, got %d", cap(ch))
	}
	if len(m.listeners) != 1 {
		t.Errorf("expected 1 listener, got %d", len(m.listeners))
	}
}

func TestUnsubscribe(t *testing.T) {
	m := NewManager()
	ch1 := m.Subscribe()
	ch2 := m.Subscribe()
	ch3 := m.Subscribe()

	if len(m.listeners) != 3 {
		t.Fatalf("expected 3 listeners, got %d", len(m.listeners))
	}

	m.Unsubscribe(ch2)
	if len(m.listeners) != 2 {
		t.Errorf("expected 2 listeners after Unsubscribe, got %d", len(m.listeners))
	}

	// Confirm ch2 closed
	select {
	case _, ok := <-ch2:
		if ok {
			t.Error("expected closed channel, got value")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("ch2 not closed after Unsubscribe")
	}

	// Remaining channels still open
	m.Unsubscribe(ch1)
	m.Unsubscribe(ch3)
	if len(m.listeners) != 0 {
		t.Errorf("expected 0 listeners, got %d", len(m.listeners))
	}
}

func TestUnsubscribeUnknownChannel(t *testing.T) {
	m := NewManager()
	m.Subscribe()
	other := make(chan Line, 1)
	// Should be a no-op, not panic
	m.Unsubscribe(other)
	if len(m.listeners) != 1 {
		t.Errorf("listeners changed unexpectedly: %d", len(m.listeners))
	}
}

// ---------- broadcast / pub-sub ----------

func TestBroadcastToMultipleSubscribers(t *testing.T) {
	m := NewManager()
	ch1 := m.Subscribe()
	ch2 := m.Subscribe()
	ch3 := m.Subscribe()

	line := Line{Text: "hello", Level: 6, Timestamp: time.Now()}
	m.broadcast(line)

	for i, ch := range []chan Line{ch1, ch2, ch3} {
		select {
		case got := <-ch:
			if got.Text != "hello" {
				t.Errorf("subscriber %d: text mismatch: %q", i, got.Text)
			}
		case <-time.After(time.Second):
			t.Errorf("subscriber %d: timed out waiting for broadcast", i)
		}
	}
}

func TestBroadcastSlowConsumerDrops(t *testing.T) {
	m := NewManager()
	ch := m.Subscribe()

	// Fill the buffer past its capacity (500) without reading
	for i := 0; i < 600; i++ {
		m.broadcast(Line{Text: "x"})
	}

	if got := len(ch); got != 500 {
		t.Errorf("expected buffer to be full at 500, got %d", got)
	}
	// We must NOT have blocked. If we reached here without timeout, that's success.
}

func TestBroadcastAfterUnsubscribeIsSafe(t *testing.T) {
	m := NewManager()
	ch := m.Subscribe()
	m.Unsubscribe(ch)
	// Should not panic on closed subscriber (it is no longer in the list)
	m.broadcast(Line{Text: "after unsub"})
}

func TestSubscriberSendAfterCloseIsSafe(t *testing.T) {
	s := &subscriber{ch: make(chan Line, 1)}
	s.close()
	// double close should be a no-op
	s.close()
	// send after close should not panic
	s.send(Line{Text: "x"})
}

func TestConcurrentSubscribeUnsubscribe(t *testing.T) {
	m := NewManager()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ch := m.Subscribe()
			time.Sleep(time.Millisecond)
			m.Unsubscribe(ch)
		}()
	}
	// Broadcasting concurrently should not race
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			m.broadcast(Line{Text: "boom"})
		}
	}()
	wg.Wait()
}

// ---------- readKMsg ----------

func TestReadKMsg(t *testing.T) {
	m := NewManager()
	ch := m.Subscribe()

	input := strings.NewReader(
		"6,1,0,-;line one\n" +
			"4,2,0,-;line two\n" +
			"3,3,0,-;line three\n",
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		m.readKMsg(ctx, input)
		close(done)
	}()

	got := []Line{}
	timeout := time.After(2 * time.Second)
	for len(got) < 3 {
		select {
		case l := <-ch:
			got = append(got, l)
		case <-timeout:
			t.Fatalf("timeout waiting for lines, got %d", len(got))
		}
	}
	cancel()
	<-done

	if got[0].Text != "line one" || got[0].Level != 6 {
		t.Errorf("line 0: %+v", got[0])
	}
	if got[1].Text != "line two" || got[1].Level != 4 {
		t.Errorf("line 1: %+v", got[1])
	}
	if got[2].Text != "line three" || got[2].Level != 3 {
		t.Errorf("line 2: %+v", got[2])
	}
}

func TestReadKMsgContextCancellation(t *testing.T) {
	m := NewManager()
	m.Subscribe()

	// Use a pipe so the scanner blocks waiting on input
	pr, pw := io.Pipe()
	defer pw.Close()

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		m.readKMsg(ctx, pr)
		close(done)
	}()

	// Write one line then cancel
	if _, err := pw.Write([]byte("6,1,0,-;hello\n")); err != nil {
		t.Fatal(err)
	}

	// Give scanner a beat to process
	time.Sleep(50 * time.Millisecond)
	cancel()
	// Close the writer so the scanner unblocks (cancellation alone won't unblock the read)
	pw.Close()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("readKMsg did not exit after context cancellation")
	}
}

// ---------- Start / Stop ----------

func TestStartStop(t *testing.T) {
	m := NewManager()
	// Multiple Start/Stop cycles must not panic or deadlock.
	m.Start()
	m.Stop()
	m.Start()
	m.Stop()
	// Stop without prior Start is safe.
	m.Stop()
}

func TestStartRestarts(t *testing.T) {
	m := NewManager()
	m.Start()
	// Second Start should stop the previous one and start fresh.
	m.Start()
	m.Stop()
}
