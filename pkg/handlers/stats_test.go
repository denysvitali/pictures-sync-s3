package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/systeminfo"
)

func systeminfoAppendForTest(c *systeminfo.StatsCollector, r systeminfo.StatsRecord) error {
	return c.AppendRecordForTest(r)
}

func itoa(v int64) string {
	return strconv.FormatInt(v, 10)
}

func TestHandleSystemStats_HoursFiltering(t *testing.T) {
	// Set up a fresh collector
	c := systeminfo.NewStatsCollector()
	c.Start()
	defer c.Stop()

	ctx := &Context{}

	// Request with default hours (24)
	req := httptest.NewRequest(http.MethodGet, "/api/system/stats", nil)
	rec := httptest.NewRecorder()
	ctx.HandleSystemStats(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var resp1 map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp1); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Request with 1 hour
	req2 := httptest.NewRequest(http.MethodGet, "/api/system/stats?hours=1", nil)
	rec2 := httptest.NewRecorder()
	ctx.HandleSystemStats(rec2, req2)

	var resp2 map[string]any
	if err := json.Unmarshal(rec2.Body.Bytes(), &resp2); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// The since timestamps should differ
	since1 := int64(resp1["since"].(float64))
	since2 := int64(resp2["since"].(float64))

	// since2 (1h ago) should be later (closer to now) than since1 (24h ago default)
	if since2 <= since1 {
		t.Errorf("expected since2 (%d) > since1 (%d) for 1h vs 24h", since2, since1)
	}

	t.Logf("24h since: %v, 1h since: %v", time.Unix(since1, 0), time.Unix(since2, 0))
}

func TestHandleSystemStats_SinceUntilParams(t *testing.T) {
	c := systeminfo.NewStatsCollector()
	c.Start()
	defer c.Stop()

	ctx := &Context{}

	now := time.Now().Unix()
	since := now - 3600
	until := now

	url := "/api/system/stats?since=" + itoa(since) + "&until=" + itoa(until)
	req := httptest.NewRequest(http.MethodGet, url, nil)
	rec := httptest.NewRecorder()
	ctx.HandleSystemStats(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if int64(resp["since"].(float64)) != since {
		t.Errorf("since: got %v want %v", resp["since"], since)
	}
	if int64(resp["until"].(float64)) != until {
		t.Errorf("until: got %v want %v", resp["until"], until)
	}
	if _, ok := resp["resolution"]; !ok {
		t.Errorf("expected resolution in response")
	}
	if _, ok := resp["count"]; !ok {
		t.Errorf("expected count in response")
	}
}

func TestHandleSystemStats_ResolutionExplicit(t *testing.T) {
	c := systeminfo.NewStatsCollector()
	c.Start()
	defer c.Stop()

	ctx := &Context{}

	req := httptest.NewRequest(http.MethodGet, "/api/system/stats?hours=1&resolution=300", nil)
	rec := httptest.NewRecorder()
	ctx.HandleSystemStats(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res := int(resp["resolution"].(float64)); res != 300 {
		t.Errorf("resolution: got %d want 300", res)
	}
}

func TestHandleSystemStats_ResolutionAuto(t *testing.T) {
	c := systeminfo.NewStatsCollector()
	c.Start()
	defer c.Stop()

	ctx := &Context{}

	// 24h window: auto should pick a bucket > 10s to keep count <= 500.
	req := httptest.NewRequest(http.MethodGet, "/api/system/stats?hours=24&resolution=auto", nil)
	rec := httptest.NewRecorder()
	ctx.HandleSystemStats(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	res := int(resp["resolution"].(float64))
	if res <= 10 {
		t.Errorf("expected auto resolution > 10s for 24h window, got %d", res)
	}
	// 24h * 3600 / res must be <= ~500
	if 24*3600/res > 500 {
		t.Errorf("auto resolution %d does not keep points <= 500", res)
	}
}

func TestHandleSystemStats_PointShape(t *testing.T) {
	c := systeminfo.NewStatsCollector()
	defer c.Stop()

	// Seed a few records directly to exercise the point serializer.
	now := time.Now().Unix()
	records := []systeminfo.StatsRecord{
		{
			Timestamp:        now - 30,
			CPUPercent:       42.5,
			RSSBytes:         100,
			TotalMemBytes:    1000,
			Load1:            0.5, Load5: 0.4, Load15: 0.3,
			SwapUsedBytes:  10, SwapTotalBytes: 100,
			DiskUsedBytes:  500, DiskTotalBytes: 1000,
			NetRxBytesPerSec: 11, NetTxBytesPerSec: 22,
		},
		{
			Timestamp:        now - 20,
			CPUPercent:       50,
			RSSBytes:         200,
			TotalMemBytes:    1000,
			Load1:            0.6, Load5: 0.5, Load15: 0.4,
			SwapUsedBytes:  20, SwapTotalBytes: 100,
			DiskUsedBytes:  600, DiskTotalBytes: 1000,
			NetRxBytesPerSec: 13, NetTxBytesPerSec: 24,
		},
	}
	for _, r := range records {
		if err := systeminfoAppendForTest(c, r); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	ctx := &Context{}
	req := httptest.NewRequest(http.MethodGet, "/api/system/stats?hours=1&resolution=10", nil)
	rec := httptest.NewRecorder()
	ctx.HandleSystemStats(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	pts, ok := resp["points"].([]any)
	if !ok || len(pts) == 0 {
		t.Fatalf("expected non-empty points, got %v", resp["points"])
	}
	first, _ := pts[0].(map[string]any)
	expectedKeys := []string{
		"timestamp", "cpu_percent", "rss_bytes", "total_mem_bytes",
		"load1", "load5", "load15",
		"swap_used_bytes", "swap_total_bytes",
		"disk_used_bytes", "disk_total_bytes",
		"net_rx_bytes_per_sec", "net_tx_bytes_per_sec",
	}
	for _, k := range expectedKeys {
		if _, ok := first[k]; !ok {
			t.Errorf("point missing key %q (have keys: %v)", k, first)
		}
	}
}

func TestHandleSystemStats_DownsampleAggregates(t *testing.T) {
	c := systeminfo.NewStatsCollector()
	defer c.Stop()

	// Insert 6 records inside a single 60s bucket. CPU avg should be ~50,
	// totals should be the last value.
	base := (time.Now().Unix() / 60) * 60
	for i := int64(0); i < 6; i++ {
		err := systeminfoAppendForTest(c, systeminfo.StatsRecord{
			Timestamp:      base + i*10,
			CPUPercent:     float32(i * 20), // 0,20,40,60,80,100 → avg 50
			RSSBytes:       uint64(i * 100),
			TotalMemBytes:  1000,
			DiskUsedBytes:  uint64(500 + i),
			DiskTotalBytes: 1000,
		})
		if err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	ctx := &Context{}
	since := base - 10
	until := base + 120
	url := "/api/system/stats?since=" + itoa(since) + "&until=" + itoa(until) + "&resolution=60"
	req := httptest.NewRequest(http.MethodGet, url, nil)
	rec := httptest.NewRecorder()
	ctx.HandleSystemStats(rec, req)

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	pts, _ := resp["points"].([]any)
	if len(pts) != 1 {
		t.Fatalf("expected 1 aggregated bucket, got %d", len(pts))
	}
	p := pts[0].(map[string]any)
	if cpu := p["cpu_percent"].(float64); cpu < 49 || cpu > 51 {
		t.Errorf("cpu avg: got %v want ~50", cpu)
	}
	if disk := uint64(p["disk_used_bytes"].(float64)); disk != 505 {
		t.Errorf("disk last-value: got %d want 505", disk)
	}
	if ts := int64(p["timestamp"].(float64)); ts != base {
		t.Errorf("bucket start: got %d want %d", ts, base)
	}
}

