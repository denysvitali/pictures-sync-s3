package systeminfo

import (
	"testing"
	"time"
)

func clearStateForTest() {
	clearActiveRecordsForTest()
}

func TestStatsCollector_ReadWrite(t *testing.T) {
	clearStateForTest()

	c := NewStatsCollector()
	c.interval = 100 * time.Millisecond
	c.retention = time.Hour

	now := time.Now()
	records := []StatsRecord{
		{Timestamp: now.Add(-20 * time.Second).Unix(), CPUPercent: 10.5, RSSBytes: 1024 * 1024, TotalMemBytes: 1024 * 1024 * 1024},
		{Timestamp: now.Add(-15 * time.Second).Unix(), CPUPercent: 25.0, RSSBytes: 2048 * 1024, TotalMemBytes: 1024 * 1024 * 1024},
		{Timestamp: now.Add(-10 * time.Second).Unix(), CPUPercent: 50.0, RSSBytes: 4096 * 1024, TotalMemBytes: 1024 * 1024 * 1024},
		{Timestamp: now.Add(-5 * time.Second).Unix(), CPUPercent: 75.0, RSSBytes: 8192 * 1024, TotalMemBytes: 1024 * 1024 * 1024},
	}

	for _, r := range records {
		if err := c.appendRecord(r); err != nil {
			t.Fatalf("appendRecord failed: %v", err)
		}
	}

	// Read all
	read, err := ReadStats(now.Add(-30*time.Second), now)
	if err != nil {
		t.Fatalf("ReadStats failed: %v", err)
	}
	if len(read) != len(records) {
		t.Fatalf("expected %d records, got %d", len(records), len(read))
	}

	for i, r := range read {
		if r.Timestamp != records[i].Timestamp {
			t.Errorf("record %d timestamp mismatch: got %d, want %d", i, r.Timestamp, records[i].Timestamp)
		}
		if r.CPUPercent != records[i].CPUPercent {
			t.Errorf("record %d cpu mismatch: got %f, want %f", i, r.CPUPercent, records[i].CPUPercent)
		}
		if r.RSSBytes != records[i].RSSBytes {
			t.Errorf("record %d rss mismatch: got %d, want %d", i, r.RSSBytes, records[i].RSSBytes)
		}
		if r.TotalMemBytes != records[i].TotalMemBytes {
			t.Errorf("record %d total mem mismatch: got %d, want %d", i, r.TotalMemBytes, records[i].TotalMemBytes)
		}
	}

	// Filtered read
	filtered, err := ReadStats(now.Add(-12*time.Second), now.Add(-8*time.Second))
	if err != nil {
		t.Fatalf("filtered ReadStats failed: %v", err)
	}
	if len(filtered) != 1 {
		t.Fatalf("expected 1 filtered record, got %d", len(filtered))
	}
	if filtered[0].CPUPercent != 50.0 {
		t.Errorf("filtered record cpu mismatch: got %f, want 50.0", filtered[0].CPUPercent)
	}
}

func TestStatsCollector_Compact(t *testing.T) {
	clearStateForTest()

	c := NewStatsCollector()
	c.retention = time.Hour

	now := time.Now()
	old := StatsRecord{Timestamp: now.Add(-2 * time.Hour).Unix(), CPUPercent: 1.0, RSSBytes: 100, TotalMemBytes: 1000}
	new := StatsRecord{Timestamp: now.Add(-30 * time.Minute).Unix(), CPUPercent: 2.0, RSSBytes: 200, TotalMemBytes: 1000}

	c.appendRecord(old)
	c.appendRecord(new)

	if err := c.Compact(); err != nil {
		t.Fatalf("Compact failed: %v", err)
	}

	read, err := ReadStats(now.Add(-3*time.Hour), now)
	if err != nil {
		t.Fatalf("ReadStats after compact failed: %v", err)
	}
	if len(read) != 1 {
		t.Fatalf("expected 1 record after compact, got %d", len(read))
	}
	if read[0].Timestamp != new.Timestamp {
		t.Errorf("expected new record to survive compact, got timestamp %d", read[0].Timestamp)
	}
}

func TestParseMeminfoKB(t *testing.T) {
	tests := []struct {
		line string
		want uint64
	}{
		{"MemTotal:       8192345 kB", 8192345},
		{"MemAvailable:    1234567 kB", 1234567},
		{"Invalid", 0},
		{"", 0},
	}
	for _, tt := range tests {
		got := parseMeminfoKB(tt.line)
		if got != tt.want {
			t.Errorf("parseMeminfoKB(%q) = %d, want %d", tt.line, got, tt.want)
		}
	}
}

func TestDefaultStatsCollectorConfig(t *testing.T) {
	c := NewStatsCollector()
	if c.interval != 10*time.Second {
		t.Fatalf("expected default interval 10s, got %s", c.interval)
	}
	if c.retention != 24*time.Hour {
		t.Fatalf("expected default retention 24h, got %s", c.retention)
	}
}

func TestStatsCollector_ReadMissing(t *testing.T) {
	clearStateForTest()

	records, err := ReadStats(time.Now().Add(-time.Hour), time.Now())
	if err != nil {
		t.Fatalf("ReadStats on empty collector failed: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("expected 0 records for empty collector, got %d", len(records))
	}
}
