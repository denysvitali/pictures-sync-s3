package googlephotos

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const apiBaseURL = "https://photoslibrary.googleapis.com/v1"

// Client is an authenticated HTTP client for the Google Photos API
type Client struct {
	clientID     string
	clientSecret string
	httpClient   *http.Client
	tokenStore   *TokenStore
	mu           sync.RWMutex
}

// NewClient creates a new Google Photos API client
func NewClient(clientID, clientSecret string, tokenStore *TokenStore) *Client {
	return &Client{
		clientID:     clientID,
		clientSecret: clientSecret,
		httpClient:   &http.Client{Timeout: 60 * time.Second},
		tokenStore:   tokenStore,
	}
}

// IsAuthenticated returns true if the client has valid tokens
func (c *Client) IsAuthenticated() bool {
	token, err := c.tokenStore.Load()
	if err != nil {
		return false
	}
	return token != nil && token.RefreshToken != ""
}

// ExchangeCode exchanges an authorization code for tokens
func (c *Client) ExchangeCode(code, redirectURI, codeVerifier string) (*OAuthToken, error) {
	return c.ExchangeCodeContext(context.Background(), code, redirectURI, codeVerifier)
}

// ExchangeCodeContext exchanges an authorization code for tokens.
func (c *Client) ExchangeCodeContext(ctx context.Context, code, redirectURI, codeVerifier string) (*OAuthToken, error) {
	data := url.Values{
		"client_id":     {c.clientID},
		"client_secret": {c.clientSecret},
		"code":          {code},
		"code_verifier": {codeVerifier},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {redirectURI},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, oauthTokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create token exchange request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token exchange request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token exchange failed (%d): %s", resp.StatusCode, string(body))
	}

	var tokenResp TokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	log.Printf("[GooglePhotos] Token exchanged. Granted scopes: %s", tokenResp.Scope)

	token := &OAuthToken{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		TokenType:    tokenResp.TokenType,
		Expiry:       time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
	}

	if err := c.tokenStore.Save(token); err != nil {
		return nil, fmt.Errorf("failed to save token: %w", err)
	}

	return token, nil
}

// refreshToken refreshes the access token using the refresh token
func (c *Client) refreshToken() error {
	return c.refreshTokenContext(context.Background())
}

func (c *Client) refreshTokenContext(ctx context.Context) error {
	token, err := c.tokenStore.Load()
	if err != nil {
		return fmt.Errorf("failed to load token: %w", err)
	}
	if token == nil || token.RefreshToken == "" {
		return fmt.Errorf("no refresh token available")
	}

	data := url.Values{
		"client_id":     {c.clientID},
		"client_secret": {c.clientSecret},
		"refresh_token": {token.RefreshToken},
		"grant_type":    {"refresh_token"},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, oauthTokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create refresh token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("refresh token request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read refresh response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("token refresh failed (%d): %s", resp.StatusCode, string(body))
	}

	var tokenResp TokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return fmt.Errorf("failed to parse refresh response: %w", err)
	}

	// Preserve the refresh token (Google may not return it on refresh)
	newToken := &OAuthToken{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: token.RefreshToken,
		TokenType:    tokenResp.TokenType,
		Expiry:       time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
	}
	if tokenResp.RefreshToken != "" {
		newToken.RefreshToken = tokenResp.RefreshToken
	}

	if err := c.tokenStore.Save(newToken); err != nil {
		return fmt.Errorf("failed to save refreshed token: %w", err)
	}

	return nil
}

// getAccessToken returns a valid access token, refreshing if necessary
func (c *Client) getAccessToken() (string, error) {
	return c.getAccessTokenContext(context.Background())
}

func (c *Client) getAccessTokenContext(ctx context.Context) (string, error) {
	token, err := c.tokenStore.Load()
	if err != nil {
		return "", fmt.Errorf("failed to load token: %w", err)
	}
	if token == nil {
		return "", fmt.Errorf("not authenticated")
	}

	// Refresh if expired or about to expire (5 minute buffer)
	if time.Until(token.Expiry) < 5*time.Minute {
		if err := c.refreshTokenContext(ctx); err != nil {
			return "", err
		}
		token, _ = c.tokenStore.Load()
	}

	return token.AccessToken, nil
}

// doRequest performs an authenticated API request
func (c *Client) doRequest(method, path string, body io.Reader) (*http.Response, error) {
	return c.doRequestContext(context.Background(), method, path, body)
}

func (c *Client) doRequestContext(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	accessToken, err := c.getAccessTokenContext(ctx)
	if err != nil {
		return nil, err
	}

	url := apiBaseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return c.httpClient.Do(req)
}

// doUploadRequest performs an authenticated upload request with binary data.
func (c *Client) doUploadRequest(r io.Reader, size int64, filename string) (*http.Response, error) {
	return c.doUploadRequestContext(context.Background(), r, size, filename)
}

func (c *Client) doUploadRequestContext(ctx context.Context, r io.Reader, size int64, filename string) (*http.Response, error) {
	accessToken, err := c.getAccessTokenContext(ctx)
	if err != nil {
		return nil, err
	}

	url := "https://photoslibrary.googleapis.com/v1/uploads"
	req, err := http.NewRequestWithContext(ctx, "POST", url, r)
	if err != nil {
		return nil, fmt.Errorf("failed to create upload request: %w", err)
	}
	if size >= 0 {
		req.ContentLength = size
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-Goog-Upload-File-Name", filename)
	req.Header.Set("X-Goog-Upload-Protocol", "raw")

	uploadClient := *c.httpClient
	uploadClient.Timeout = googlePhotosTransferTimeout(size)
	return uploadClient.Do(req)
}

// Disconnect removes stored tokens
func (c *Client) Disconnect() error {
	return c.tokenStore.Delete()
}

// GetConnectionStatus returns the current connection status
func (c *Client) GetConnectionStatus() (*ConnectionStatus, error) {
	if !c.IsAuthenticated() {
		return &ConnectionStatus{Connected: false}, nil
	}

	return &ConnectionStatus{
		Connected: true,
	}, nil
}
