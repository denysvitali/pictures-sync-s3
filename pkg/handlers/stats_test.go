package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/systeminfo"
)

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
