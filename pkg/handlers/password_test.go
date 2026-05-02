package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/denysvitali/pictures-sync-s3/pkg/auth"
)

func TestHandlePasswordChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gokr-pw.txt")
	if err := os.WriteFile(path, []byte("old-password\n"), 0600); err != nil {
		t.Fatalf("write password file: %v", err)
	}
	passwordMgr, err := auth.NewPasswordManager(path, "dev-password")
	if err != nil {
		t.Fatalf("NewPasswordManager() error = %v", err)
	}
	ctx := &Context{PasswordMgr: passwordMgr}

	body, _ := json.Marshal(map[string]string{
		"current_password": "old-password",
		"new_password":     "new-password",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/password", bytes.NewReader(body))
	w := httptest.NewRecorder()
	ctx.HandlePasswordChange(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("HandlePasswordChange() status = %d, body = %s", w.Code, w.Body.String())
	}
	if got := passwordMgr.CurrentPassword(); got != "new-password" {
		t.Fatalf("CurrentPassword() = %q, want new-password", got)
	}
}

func TestHandlePasswordChangeRejectsWrongCurrentPassword(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gokr-pw.txt")
	if err := os.WriteFile(path, []byte("old-password\n"), 0600); err != nil {
		t.Fatalf("write password file: %v", err)
	}
	passwordMgr, err := auth.NewPasswordManager(path, "dev-password")
	if err != nil {
		t.Fatalf("NewPasswordManager() error = %v", err)
	}
	ctx := &Context{PasswordMgr: passwordMgr}

	body, _ := json.Marshal(map[string]string{
		"current_password": "wrong-password",
		"new_password":     "new-password",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/password", bytes.NewReader(body))
	w := httptest.NewRecorder()
	ctx.HandlePasswordChange(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("HandlePasswordChange() status = %d, want 401", w.Code)
	}
	if got := passwordMgr.CurrentPassword(); got != "old-password" {
		t.Fatalf("CurrentPassword() = %q, want old-password", got)
	}
}
