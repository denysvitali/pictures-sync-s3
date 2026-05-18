package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"syscall"
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

func TestHandleSystemServicesRestartDefaultsToAppServices(t *testing.T) {
	restore := overrideServiceRestartForTest(t)
	defer restore()

	findServicePIDs = func(exePath string) ([]int, error) {
		if exePath != "/user/pictures-sync" {
			t.Fatalf("findServicePIDs path = %q", exePath)
		}
		return []int{42}, nil
	}
	currentPID = func() int { return 99 }

	var signaled []int
	signalService = func(pid int, sig syscall.Signal) error {
		if sig != syscall.SIGTERM {
			t.Fatalf("signal = %v, want SIGTERM", sig)
		}
		signaled = append(signaled, pid)
		return nil
	}
	scheduleServiceSignal = func(fn func()) {
		fn()
	}

	req := httptest.NewRequest(http.MethodPost, "/api/system/services/restart", bytes.NewBufferString(`{}`))
	rr := httptest.NewRecorder()

	(&Context{}).HandleSystemServicesRestart(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body: %s", rr.Code, http.StatusAccepted, rr.Body.String())
	}
	if !reflect.DeepEqual(signaled, []int{42, 99}) {
		t.Fatalf("signaled pids = %#v, want []int{42, 99}", signaled)
	}

	var response struct {
		Success bool                   `json:"success"`
		Results []serviceRestartResult `json:"results"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if !response.Success {
		t.Fatal("success = false, want true")
	}
	if len(response.Results) != 2 {
		t.Fatalf("results len = %d, want 2", len(response.Results))
	}
	if response.Results[0].Service != servicePicturesSync || response.Results[0].Status != "signaled" {
		t.Fatalf("unexpected first result: %+v", response.Results[0])
	}
	if response.Results[1].Service != serviceWebUI || response.Results[1].Status != "scheduled" {
		t.Fatalf("unexpected second result: %+v", response.Results[1])
	}
}

func TestHandleSystemServicesRestartRejectsUnknownService(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/system/services/restart", bytes.NewBufferString(`{"services":["ssh"]}`))
	rr := httptest.NewRecorder()

	(&Context{}).HandleSystemServicesRestart(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body: %s", rr.Code, http.StatusBadRequest, rr.Body.String())
	}
}

func overrideServiceRestartForTest(t *testing.T) func() {
	t.Helper()

	oldFind := findServicePIDs
	oldSignal := signalService
	oldCurrentPID := currentPID
	oldSchedule := scheduleServiceSignal

	return func() {
		findServicePIDs = oldFind
		signalService = oldSignal
		currentPID = oldCurrentPID
		scheduleServiceSignal = oldSchedule
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
