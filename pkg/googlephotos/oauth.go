package googlephotos

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"log"
	"net/url"
	"sync"
	"time"
)

const (
	oauthAuthURL  = "https://accounts.google.com/o/oauth2/v2/auth"
	oauthTokenURL = "https://oauth2.googleapis.com/token"
	oauthScope    = "https://www.googleapis.com/auth/photoslibrary.readonly https://www.googleapis.com/auth/photoslibrary.appendonly"
)

// StateStore manages PKCE state tokens for the OAuth flow
type StateStore struct {
	mu     sync.Mutex
	states map[string]*AuthState
}

// NewStateStore creates a new state store with automatic cleanup
func NewStateStore() *StateStore {
	s := &StateStore{
		states: make(map[string]*AuthState),
	}
	go s.cleanupLoop()
	return s
}

// GeneratePKCE generates a new PKCE code verifier and challenge
func GeneratePKCE() (verifier, challenge string, err error) {
	// Generate random bytes for code verifier (32 bytes = 256 bits)
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("failed to generate random bytes: %w", err)
	}
	verifier = base64.RawURLEncoding.EncodeToString(b)

	// Compute S256 challenge
	h := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(h[:])

	return verifier, challenge, nil
}

// GenerateState generates a random state parameter
func GenerateState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// StartAuth initiates the OAuth flow and returns the authorization URL
func (s *StateStore) StartAuth(clientID, redirectURI string) (*AuthState, string, error) {
	verifier, challenge, err := GeneratePKCE()
	if err != nil {
		return nil, "", err
	}

	state, err := GenerateState()
	if err != nil {
		return nil, "", err
	}

	authState := &AuthState{
		CodeVerifier: verifier,
		State:        state,
		RedirectURI:  redirectURI,
		ExpiresAt:    time.Now().Add(10 * time.Minute),
	}

	s.mu.Lock()
	s.states[state] = authState
	s.mu.Unlock()

	authURL := fmt.Sprintf(
		"%s?client_id=%s&redirect_uri=%s&response_type=code&scope=%s&state=%s&code_challenge=%s&code_challenge_method=S256&access_type=offline&prompt=consent",
		oauthAuthURL,
		url.QueryEscape(clientID),
		url.QueryEscape(redirectURI),
		url.QueryEscape(oauthScope),
		url.QueryEscape(state),
		url.QueryEscape(challenge),
	)

	log.Printf("[GooglePhotos] OAuth auth URL generated with scopes: %s", oauthScope)

	return authState, authURL, nil
}

// ValidateState checks if a state token is valid and returns the associated auth state
func (s *StateStore) ValidateState(state string) (*AuthState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	authState, ok := s.states[state]
	if !ok {
		return nil, false
	}

	if time.Now().After(authState.ExpiresAt) {
		delete(s.states, state)
		return nil, false
	}

	// Remove the state so it can't be reused
	delete(s.states, state)
	return authState, true
}

// cleanupLoop periodically removes expired states
func (s *StateStore) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		s.mu.Lock()
		now := time.Now()
		for state, authState := range s.states {
			if now.After(authState.ExpiresAt) {
				delete(s.states, state)
			}
		}
		s.mu.Unlock()
	}
}
