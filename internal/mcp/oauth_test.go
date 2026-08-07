package mcp_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"superscience/internal/mcp"
	"superscience/internal/store"
)

type oauthMemorySecrets struct {
	mu     sync.Mutex
	values map[string]string
}

func (s *oauthMemorySecrets) Store(account, secret string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[account] = secret
	return nil
}

func (s *oauthMemorySecrets) Load(account string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.values[account]
	if !ok {
		return "", errors.New("not found")
	}
	return value, nil
}

func (s *oauthMemorySecrets) Delete(account string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.values, account)
	return nil
}

func TestPrepareAuthenticatedServerRefreshesExpiredCredential(t *testing.T) {
	var sawRefresh bool
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("refresh_token") != "old-refresh" || r.Form.Get("client_id") != "public-client" {
			t.Fatalf("refresh form = %+v", r.Form)
		}
		sawRefresh = true
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "new-access", "refresh_token": "new-refresh", "token_type": "Bearer", "expires_in": 3600,
		})
	}))
	defer tokenServer.Close()

	backend := &oauthMemorySecrets{values: map[string]string{}}
	credential := mcp.OAuthCredential{
		AccessToken: "expired-access", RefreshToken: "old-refresh", TokenType: "Bearer",
		ExpiresAt: time.Now().Add(-time.Minute).UnixMilli(), TokenEndpoint: tokenServer.URL,
		ClientID: "public-client", AuthMethod: "none",
	}
	encoded, _ := json.Marshal(credential)
	backend.values["mcp-oauth:fixture"] = string(encoded)

	prepared, err := mcp.PrepareAuthenticatedServer(context.Background(), store.MCPServer{
		ID: "fixture", OAuthSecretRef: "mcp-oauth:fixture",
	}, backend)
	if err != nil {
		t.Fatal(err)
	}
	if !sawRefresh || prepared.BearerToken != "new-access" {
		t.Fatalf("prepared server = %+v sawRefresh=%v", prepared, sawRefresh)
	}
	stored, err := backend.Load("mcp-oauth:fixture")
	if err != nil {
		t.Fatal(err)
	}
	var refreshed mcp.OAuthCredential
	if err := json.Unmarshal([]byte(stored), &refreshed); err != nil {
		t.Fatal(err)
	}
	if refreshed.AccessToken != "new-access" || refreshed.RefreshToken != "new-refresh" || refreshed.ExpiresAt <= time.Now().UnixMilli() {
		t.Fatalf("refreshed credential = %+v", refreshed)
	}
}
