package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/httputil"
	"github.com/denysvitali/pictures-sync-s3/pkg/ntpsync"
	"github.com/denysvitali/pictures-sync-s3/pkg/paniclog"
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

func (ctx *Context) HandleSystemPanic(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		record, err := paniclog.ReadStored(panicLogPath, panicCrashPath)
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, fmt.Sprintf("Failed to read panic information: %v", err))
			return
		}
		if record == nil {
			httputil.JSON(w, http.StatusOK, map[string]any{
				"exists": false,
			})
			return
		}
		httputil.JSON(w, http.StatusOK, map[string]any{
			"exists": true,
			"panic":  record,
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
