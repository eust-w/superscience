package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"superscience/internal/store"
)

type resolverSecrets map[string]string

func (s resolverSecrets) Load(account string) (string, error) { return s[account], nil }

func TestResolverUsesPersistedProfileAndSecretBackend(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer resolver-key" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"resolved\"},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer api.Close()

	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "resolver.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.UpsertProvider(context.Background(), store.ProviderProfile{
		ID: "lab-provider", DisplayName: "Lab Provider", Kind: "openai-compatible", BaseURL: api.URL + "/v1", Enabled: true,
		SecretRef: "keychain:lab-provider",
	})
	if err != nil {
		t.Fatal(err)
	}

	adapter, err := (Resolver{Store: db, Secrets: resolverSecrets{"lab-provider": "resolver-key"}}).Resolve(context.Background(), "lab-provider", "lab-model")
	if err != nil {
		t.Fatal(err)
	}
	result, err := adapter.Stream(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "verify"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "resolved" {
		t.Fatalf("result = %+v", result)
	}
}
