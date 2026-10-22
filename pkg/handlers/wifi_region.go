package handlers

import (
	"errors"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/httputil"
	"github.com/denysvitali/pictures-sync-s3/pkg/wifimanager"
	"github.com/denysvitali/pictures-sync-s3/pkg/wifiregion"
)

const (
	regionAutoDetectDelay    = 45 * time.Second
	regionAutoDetectInterval = 6 * time.Hour
)

// regionDetector is implemented by WiFi managers that can infer the
// regulatory region from nearby access points.
type regionDetector interface {
	DetectRegion() (wifimanager.RegionDetection, error)
}

// applyWiFiRegion is replaceable in tests; it needs nl80211 and /perm.
var applyWiFiRegion = applyWiFiRegionToSystem

var regionDetectMu sync.Mutex

// applyWiFiRegionToSystem makes the region effective now and for future boots:
// the kernel regulatory domain, the file read by early-boot services, and the
// region gokrazy/wifi would otherwise reset to the world domain.
func applyWiFiRegionToSystem(country string) error {
	country, err := wifiregion.Normalize(country)
	if err != nil {
		return err
	}
	var errs []error
	if err := wifiregion.Apply(country); err != nil {
		errs = append(errs, err)
	}
	if err := wifiregion.Write(country); err != nil {
		errs = append(errs, err)
	}
	if _, err := wifiregion.SetClientConfigRegion(wifimanager.GokrazyWiFiConfigPath, country, true); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// syncWiFiRegion applies the configured region after a settings change. An
// empty setting means auto, which detects in the background.
func (ctx *Context) syncWiFiRegion() error {
	country := ctx.AppSettings.GetWiFiCountry()
	if country == "" {
		go ctx.autoDetectWiFiRegion()
		return nil
	}
	return applyWiFiRegion(country)
}

// autoDetectWiFiRegion infers the region from nearby access points and applies
// it when the settings are on auto and enough access points agree.
func (ctx *Context) autoDetectWiFiRegion() {
	if ctx.AppSettings == nil || ctx.AppSettings.GetWiFiCountry() != "" {
		return
	}
	detector, ok := ctx.WiFiMgr.(regionDetector)
	if !ok {
		return
	}
	if !regionDetectMu.TryLock() {
		return
	}
	defer regionDetectMu.Unlock()

	detection, err := detector.DetectRegion()
	if err != nil {
		log.Printf("WiFi region auto-detection failed: %v", err)
		return
	}
	if !detection.Confident() {
		log.Printf("WiFi region auto-detection inconclusive: %+v", detection)
		return
	}
	// The setting may have been set explicitly while scanning.
	if ctx.AppSettings.GetWiFiCountry() != "" || detection.Country == wifiregion.Read() {
		return
	}
	log.Printf("WiFi region auto-detected %s from %d of %d access points", detection.Country, detection.Votes, detection.Total)
	if err := applyWiFiRegion(detection.Country); err != nil {
		log.Printf("Failed to apply auto-detected WiFi region %s: %v", detection.Country, err)
	}
}

// RunWiFiRegionAutoDetect keeps the region current while the setting is on
// auto: shortly after boot (once Wi-Fi is up) and then periodically.
func (ctx *Context) RunWiFiRegionAutoDetect(done <-chan struct{}) {
	timer := time.NewTimer(regionAutoDetectDelay)
	defer timer.Stop()
	for {
		select {
		case <-done:
			return
		case <-timer.C:
			ctx.autoDetectWiFiRegion()
			timer.Reset(regionAutoDetectInterval)
		}
	}
}

// HandleWiFiRegion reports the region (GET) or detects it from nearby
// networks without changing anything (POST).
func (ctx *Context) HandleWiFiRegion(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		configured := ctx.AppSettings.GetWiFiCountry()
		httputil.JSON(w, http.StatusOK, map[string]any{
			"configured": configured,
			"auto":       configured == "",
			"effective":  wifiregion.Read(),
		})

	case http.MethodPost:
		detector, ok := ctx.WiFiMgr.(regionDetector)
		if !ok {
			http.Error(w, "WiFi manager not initialized", http.StatusServiceUnavailable)
			return
		}
		regionDetectMu.Lock()
		detection, err := detector.DetectRegion()
		regionDetectMu.Unlock()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		httputil.JSON(w, http.StatusOK, map[string]any{
			"country":   detection.Country,
			"votes":     detection.Votes,
			"total":     detection.Total,
			"confident": detection.Confident(),
		})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

