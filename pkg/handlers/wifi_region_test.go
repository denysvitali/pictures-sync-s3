package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/denysvitali/pictures-sync-s3/pkg/wifimanager"
	"github.com/denysvitali/pictures-sync-s3/pkg/wifiregion"
)

type regionWiFiManager struct {
	mockWiFiManager
	detection wifimanager.RegionDetection
}

func (m *regionWiFiManager) DetectRegion() (wifimanager.RegionDetection, error) {
	return m.detection, nil
}

func stubApplyRegion(t *testing.T) *[]string {
	t.Helper()
	var applied []string
	old := applyWiFiRegion
	applyWiFiRegion = func(country string) error {
		applied = append(applied, country)
		return nil
	}
	t.Cleanup(func() { applyWiFiRegion = old })
	return &applied
}

func postSettings(t *testing.T, ctx *Context, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/settings", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	ctx.HandleSettings(w, req)
	return w
}

func TestHandleSettingsSetsWiFiCountry(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()
	applied := stubApplyRegion(t)

	w := postSettings(t, ctx, `{"wifi_country": "de"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", w.Code, w.Body.String())
	}
	if got := ctx.AppSettings.GetWiFiCountry(); got != "DE" {
		t.Errorf("WiFiCountry = %q, want DE", got)
	}
	if len(*applied) != 1 || (*applied)[0] != "DE" {
		t.Errorf("applied = %v, want [DE]", *applied)
	}
}

func TestHandleSettingsRejectsInvalidWiFiCountry(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()
	applied := stubApplyRegion(t)

	for _, body := range []string{`{"wifi_country": "DEU"}`, `{"wifi_country": "1x"}`} {
		if w := postSettings(t, ctx, body); w.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", body, w.Code)
		}
	}
	if len(*applied) != 0 || ctx.AppSettings.GetWiFiCountry() != "" {
		t.Errorf("invalid input must not change anything: %v %q", *applied, ctx.AppSettings.GetWiFiCountry())
	}
}

func TestHandleSettingsWiFiCountryAutoClearsSetting(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()
	stubApplyRegion(t)

	if err := ctx.AppSettings.SetWiFiCountry("IT"); err != nil {
		t.Fatal(err)
	}
	if w := postSettings(t, ctx, `{"wifi_country": "auto"}`); w.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", w.Code, w.Body.String())
	}
	if got := ctx.AppSettings.GetWiFiCountry(); got != "" {
		t.Errorf("WiFiCountry = %q, want auto (empty)", got)
	}
}

func TestAutoDetectAppliesOnlyWhenConfident(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()
	applied := stubApplyRegion(t)
	wifiregion.File = t.TempDir() + "/wifi-region"

	mgr := &regionWiFiManager{detection: wifimanager.RegionDetection{Country: "US", Votes: 1, Total: 1}}
	ctx.WiFiMgr = mgr
	ctx.autoDetectWiFiRegion()
	if len(*applied) != 0 {
		t.Fatalf("a single AP must not be applied: %v", *applied)
	}

	mgr.detection = wifimanager.RegionDetection{Country: "CH", Votes: 3, Total: 4}
	ctx.autoDetectWiFiRegion()
	if len(*applied) != 1 || (*applied)[0] != "CH" {
		t.Fatalf("applied = %v, want [CH]", *applied)
	}

	// An explicit setting always wins over detection.
	if err := ctx.AppSettings.SetWiFiCountry("IT"); err != nil {
		t.Fatal(err)
	}
	ctx.autoDetectWiFiRegion()
	if len(*applied) != 1 {
		t.Fatalf("explicit region must not be overridden: %v", *applied)
	}
}

func TestHandleWiFiRegionDetect(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()
	ctx.WiFiMgr = &regionWiFiManager{detection: wifimanager.RegionDetection{Country: "FR", Votes: 2, Total: 2}}

	w := httptest.NewRecorder()
	ctx.HandleWiFiRegion(w, httptest.NewRequest(http.MethodPost, "/api/wifi/region", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", w.Code, w.Body.String())
	}
	var got struct {
		Country   string `json:"country"`
		Confident bool   `json:"confident"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Country != "FR" || !got.Confident {
		t.Errorf("got %+v", got)
	}
}
