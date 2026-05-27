package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/httputil"
	"github.com/denysvitali/pictures-sync-s3/pkg/ntpsync"
	"github.com/denysvitali/pictures-sync-s3/pkg/paniclog"
	"github.com/denysvitali/pictures-sync-s3/pkg/systeminfo"
	"github.com/denysvitali/pictures-sync-s3/pkg/tlsconfig"
)

var (
	setSystemTime        = ntpsync.SetSystemTime
	generateCert         = tlsconfig.GeneratePersistentSelfSignedCertificate
	configureCrashOutput = paniclog.ConfigureCrashOutput
	panicLogPath         = paniclog.DefaultPath
	panicCrashPath       = paniclog.DefaultCrashPath
)

type systemTimeRequest struct {
	ClientTime string `json:"client_time"`
}

type tlsCertificateRequest struct {
	Hosts []string `json:"hosts"`
}

func (ctx *Context) HandleSystemTime(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		httputil.JSON(w, http.StatusOK, systemStatusResponse(r, nil))
	case http.MethodPost:
		var req systemTimeRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			httputil.BadRequest(w, "Invalid JSON")
			return
		}

		clientTime, err := parseClientTime(req.ClientTime)
		if err != nil {
			httputil.BadRequest(w, err.Error())
			return
		}
		if !tlsconfig.CurrentTimeCanIssueCertificate(clientTime) {
			httputil.BadRequest(w, fmt.Sprintf("client time %s is too early", clientTime.UTC().Format(time.RFC3339)))
			return
		}

		if err := setSystemTime(clientTime); err != nil {
			httputil.Error(w, http.StatusInternalServerError, fmt.Sprintf("Failed to set system time: %v", err))
			return
		}

		httputil.JSON(w, http.StatusOK, systemStatusResponse(r, map[string]any{
			"synced":      true,
			"client_time": clientTime.UTC().Format(time.RFC3339Nano),
		}))
	default:
		httputil.MethodNotAllowed(w)
	}
}

func (ctx *Context) HandleSystemTLSCertificate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httputil.MethodNotAllowed(w)
		return
	}

	var req tlsCertificateRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		httputil.BadRequest(w, "Invalid JSON")
		return
	}

	if len(req.Hosts) == 0 {
		httputil.BadRequest(w, "invalid host: hosts list must not be empty")
		return
	}
	for _, h := range req.Hosts {
		if !isValidCertHost(h) {
			httputil.BadRequest(w, fmt.Sprintf("invalid host: %q", h))
			return
		}
	}

	hosts := append([]string{}, req.Hosts...)
	hosts = append(hosts, requestHost(r))
	info, err := generateCert(hosts)
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, fmt.Sprintf("Failed to generate TLS certificate: %v", err))
		return
	}

	httputil.JSON(w, http.StatusOK, systemStatusResponse(r, map[string]any{
		"generated":       true,
		"tls_certificate": certificateInfoResponse(info),
	}))
}

// isValidCertHost reports whether host is acceptable for use as a TLS
// certificate Subject Alternative Name. It accepts a valid IP address or a
// DNS hostname conforming to RFC 1123 (letters, digits, hyphens; no leading
// or trailing hyphen; label <= 63 chars; total <= 253 chars).
func isValidCertHost(host string) bool {
	if host == "" {
		return false
	}
	if net.ParseIP(host) != nil {
		return true
	}
	return isValidDNSHostname(host)
}

func isValidDNSHostname(host string) bool {
	// Allow trailing dot in the input but do not count it toward length.
	host = strings.TrimSuffix(host, ".")
	if host == "" || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if !isValidDNSLabel(label) {
			return false
		}
	}
	return true
}

func isValidDNSLabel(label string) bool {
	n := len(label)
	if n == 0 || n > 63 {
		return false
	}
	if label[0] == '-' || label[n-1] == '-' {
		return false
	}
	for i := 0; i < n; i++ {
		c := label[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		case c == '-':
		default:
			return false
		}
	}
	return true
}

func (ctx *Context) HandleSystemPanic(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		records, err := paniclog.ReadAllStored(panicLogPath, panicCrashPath)
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, fmt.Sprintf("Failed to read panic information: %v", err))
			return
		}
		if len(records) == 0 {
			httputil.JSON(w, http.StatusOK, map[string]any{
				"exists": false,
				"panics": []paniclog.Record{},
			})
			return
		}
		httputil.JSON(w, http.StatusOK, map[string]any{
			"exists": true,
			"panic":  records[0],
			"panics": records,
		})
	case http.MethodDelete:
		if err := paniclog.ClearStored(panicLogPath, panicCrashPath); err != nil {
			httputil.Error(w, http.StatusInternalServerError, fmt.Sprintf("Failed to clear panic information: %v", err))
			return
		}
		if err := configureCrashOutput(panicCrashPath); err != nil {
			httputil.Error(w, http.StatusInternalServerError, fmt.Sprintf("Failed to reset panic capture: %v", err))
			return
		}
		httputil.JSON(w, http.StatusOK, map[string]any{
			"success": true,
		})
	default:
		httputil.MethodNotAllowed(w)
	}
}

func (ctx *Context) HandleSystemStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httputil.MethodNotAllowed(w)
		return
	}

	now := time.Now()
	q := r.URL.Query()

	// Time window. `since` / `until` (Unix seconds) take precedence over
	// `hours` (back-compat). Falls back to 24h ending now.
	var since, until time.Time
	sinceParam := strings.TrimSpace(q.Get("since"))
	untilParam := strings.TrimSpace(q.Get("until"))

	if untilParam != "" {
		if v, err := strconv.ParseInt(untilParam, 10, 64); err == nil && v > 0 {
			until = time.Unix(v, 0)
		}
	}
	if until.IsZero() {
		until = now
	}

	if sinceParam != "" {
		if v, err := strconv.ParseInt(sinceParam, 10, 64); err == nil && v > 0 {
			since = time.Unix(v, 0)
		}
	}
	if since.IsZero() {
		hours := httputil.QueryParamIntRange(r, "hours", 24, 1, 168)
		since = until.Add(-time.Duration(hours) * time.Hour)
	}

	if !since.Before(until) {
		// Guard against inverted/zero spans — clamp to a 1h window.
		since = until.Add(-time.Hour)
	}

	// Resolution. Accept an integer seconds value or "auto"/""/0 for auto.
	resParam := strings.TrimSpace(strings.ToLower(q.Get("resolution")))
	var resolution int
	switch resParam {
	case "", "auto", "0":
		resolution = systeminfo.AutoResolution(since.Unix(), until.Unix(), 500)
	default:
		if v, err := strconv.Atoi(resParam); err == nil && v > 0 {
			resolution = v
		} else {
			resolution = systeminfo.AutoResolution(since.Unix(), until.Unix(), 500)
		}
	}
	if resolution < 10 {
		resolution = 10
	}

	records, err := systeminfo.ReadStats(since, until)
	if err != nil {
		httputil.InternalError(w, err)
		return
	}

	buckets := systeminfo.Downsample(records, resolution)

	type point struct {
		Timestamp        int64   `json:"timestamp"`
		CPUPercent       float32 `json:"cpu_percent"`
		RSSBytes         uint64  `json:"rss_bytes"`
		TotalMemBytes    uint64  `json:"total_mem_bytes"`
		Load1            float32 `json:"load1"`
		Load5            float32 `json:"load5"`
		Load15           float32 `json:"load15"`
		SwapUsedBytes    uint64  `json:"swap_used_bytes"`
		SwapTotalBytes   uint64  `json:"swap_total_bytes"`
		DiskUsedBytes    uint64  `json:"disk_used_bytes"`
		DiskTotalBytes   uint64  `json:"disk_total_bytes"`
		NetRxBytesPerSec uint64  `json:"net_rx_bytes_per_sec"`
		NetTxBytesPerSec uint64  `json:"net_tx_bytes_per_sec"`
	}

	points := make([]point, len(buckets))
	for i, b := range buckets {
		points[i] = point{
			Timestamp:        b.Timestamp,
			CPUPercent:       b.CPUPercent,
			RSSBytes:         b.RSSBytes,
			TotalMemBytes:    b.TotalMemBytes,
			Load1:            b.Load1,
			Load5:            b.Load5,
			Load15:           b.Load15,
			SwapUsedBytes:    b.SwapUsedBytes,
			SwapTotalBytes:   b.SwapTotalBytes,
			DiskUsedBytes:    b.DiskUsedBytes,
			DiskTotalBytes:   b.DiskTotalBytes,
			NetRxBytesPerSec: b.NetRxBytesPS,
			NetTxBytesPerSec: b.NetTxBytesPS,
		}
	}

	httputil.JSON(w, http.StatusOK, map[string]any{
		"since":      since.Unix(),
		"until":      until.Unix(),
		"interval":   10,
		"resolution": resolution,
		"count":      len(points),
		"points":     points,
	})
}

func systemStatusResponse(r *http.Request, extra map[string]any) map[string]any {
	now := time.Now().UTC()
	info, err := tlsconfig.PersistentCertificateInfo(now)
	response := map[string]any{
		"current_time":    now.Format(time.RFC3339Nano),
		"unix_seconds":    now.Unix(),
		"time_reasonable": tlsconfig.CurrentTimeCanIssueCertificate(now),
		"tls_certificate": certificateInfoResponse(info),
	}
	if err != nil {
		response["tls_certificate_error"] = err.Error()
	}
	if host := requestHost(r); host != "" {
		response["request_host"] = host
	}
	for key, value := range extra {
		response[key] = value
	}
	return response
}

func certificateInfoResponse(info *tlsconfig.CertificateInfo) map[string]any {
	if info == nil {
		return map[string]any{
			"exists":             false,
			"needs_regeneration": true,
		}
	}

	response := map[string]any{
		"cert_file":          info.CertFile,
		"key_file":           info.KeyFile,
		"exists":             info.Exists,
		"valid_now":          info.ValidNow,
		"needs_regeneration": info.NeedsRegeneration,
		"common_name":        info.CommonName,
		"dns_names":          info.DNSNames,
		"ip_addresses":       info.IPAddresses,
		"fingerprint_sha256": info.FingerprintSHA256,
	}
	if !info.NotBefore.IsZero() {
		response["not_before"] = info.NotBefore.UTC().Format(time.RFC3339Nano)
	}
	if !info.NotAfter.IsZero() {
		response["not_after"] = info.NotAfter.UTC().Format(time.RFC3339Nano)
	}
	if info.Error != "" {
		response["error"] = info.Error
	}
	return response
}

func parseClientTime(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, fmt.Errorf("client_time is required")
	}

	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("client_time must be an RFC3339 timestamp")
	}
	return parsed.UTC(), nil
}

func requestHost(r *http.Request) string {
	if r == nil {
		return ""
	}
	host := strings.TrimSpace(r.Host)
	if forwardedHost := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); forwardedHost != "" {
		host = forwardedHost
	}
	if host == "" {
		return ""
	}

	if strings.Contains(host, "://") {
		if parsed, err := url.Parse(host); err == nil {
			host = parsed.Host
		}
	}
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
	}
	host = strings.Trim(host, "[]")
	if host == "" || strings.ContainsAny(host, "/\\ \t\r\n") {
		return ""
	}
	return host
}
