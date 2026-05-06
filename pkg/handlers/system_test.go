package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/tlsconfig"
)

func TestHandleSystemTimeSyncsClientTime(t *testing.T) {
	originalSetSystemTime := setSystemTime
	defer func() {
		setSystemTime = originalSetSystemTime
	}()

	want := time.Date(2026, time.May, 6, 8, 0, 0, 0, time.UTC)
	var got time.Time
	setSystemTime = func(t time.Time) error {
		got = t
		return nil
	}

	body := bytes.NewBufferString(`{"client_time":"` + want.Format(time.RFC3339Nano) + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/system/time", body)
	rr := httptest.NewRecorder()

	(&Context{}).HandleSystemTime(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if !got.Equal(want) {
		t.Fatalf("expected set time %s, got %s", want, got)
	}

	var response map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response["synced"] != true {
		t.Fatalf("expected synced response, got %v", response)
	}
}

func TestHandleSystemTimeRejectsEarlyClientTime(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/system/time", bytes.NewBufferString(`{"client_time":"1980-01-01T00:00:03Z"}`))
	rr := httptest.NewRecorder()

	(&Context{}).HandleSystemTime(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleSystemTLSCertificateGeneratesCert(t *testing.T) {
	originalGenerateCert := generateCert
	defer func() {
		generateCert = originalGenerateCert
	}()

	var gotHosts []string
	generateCert = func(hosts []string) (*tlsconfig.CertificateInfo, error) {
		gotHosts = append([]string(nil), hosts...)
		return &tlsconfig.CertificateInfo{
			CertFile:          "/perm/ssl/gokrazy-web.pem",
			KeyFile:           "/perm/ssl/gokrazy-web.key.pem",
			Exists:            true,
			ValidNow:          true,
			NeedsRegeneration: false,
			NotBefore:         time.Date(2026, time.May, 6, 7, 55, 0, 0, time.UTC),
			NotAfter:          time.Date(2036, time.May, 4, 8, 0, 0, 0, time.UTC),
			DNSNames:          []string{"photo-backup.local"},
		}, nil
	}

	req := httptest.NewRequest(http.MethodPost, "/api/system/tls-certificate", bytes.NewBufferString(`{"hosts":["photo-backup.local"]}`))
	req.Host = "photo-backup.local:8080"
	rr := httptest.NewRecorder()

	(&Context{}).HandleSystemTLSCertificate(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if !containsHost(gotHosts, "photo-backup.local") {
		t.Fatalf("expected request hosts to include photo-backup.local, got %v", gotHosts)
	}
}

func containsHost(hosts []string, want string) bool {
	for _, host := range hosts {
		if host == want {
			return true
		}
	}
	return false
}
