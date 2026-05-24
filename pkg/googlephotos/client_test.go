package googlephotos

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestExchangeCodeSendsCodeVerifier(t *testing.T) {
	tokenStore := NewTokenStore(t.TempDir() + "/token.json")
	client := NewClient("client-id", "client-secret", tokenStore)

	var form url.Values
	client.httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.Method != http.MethodPost {
				t.Errorf("method = %s, want %s", req.Method, http.MethodPost)
			}
			if got := req.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
				t.Errorf("content type = %q, want application/x-www-form-urlencoded", got)
			}

			body, err := io.ReadAll(req.Body)
			if err != nil {
				t.Fatalf("failed to read request body: %v", err)
			}
			form, err = url.ParseQuery(string(body))
			if err != nil {
				t.Fatalf("failed to parse request body: %v", err)
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"access_token":"access","refresh_token":"refresh","token_type":"Bearer","expires_in":3600}`)),
			}, nil
		}),
	}

	_, err := client.ExchangeCode("auth-code", "http://device.local/api/googlephotos/auth/callback", "pkce-verifier")
	if err != nil {
		t.Fatalf("ExchangeCode returned error: %v", err)
	}

	want := map[string]string{
		"client_id":     "client-id",
		"client_secret": "client-secret",
		"code":          "auth-code",
		"code_verifier": "pkce-verifier",
		"grant_type":    "authorization_code",
		"redirect_uri":  "http://device.local/api/googlephotos/auth/callback",
	}
	for key, value := range want {
		if got := form.Get(key); got != value {
			t.Errorf("%s = %q, want %q", key, got, value)
		}
	}
}
