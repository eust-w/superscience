package providers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"superscience/internal/providers"
)

func TestCatalogRefreshNormalizesModelsDevProviderData(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api.json" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		payload := map[string]any{
			"openai": map[string]any{
				"name": "OpenAI",
				"models": map[string]any{
					"gpt-catalog-test": map[string]any{
						"name": "GPT Catalog Test",
						"limit": map[string]any{
							"context": 262144,
						},
						"modalities": map[string]any{
							"input":  []string{"text", "image"},
							"output": []string{"text"},
						},
						"features": []string{"tool_call", "reasoning"},
					},
				},
			},
		}
		if err := json.NewEncoder(w).Encode(payload); err != nil {
			t.Fatal(err)
		}
	}))
	defer api.Close()

	catalog := providers.NewCatalog(api.URL+"/api.json", api.Client())
	models, err := catalog.Refresh(context.Background())
	if err != nil {
		t.Fatalf("refresh catalog: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("models = %+v", models)
	}
	model := models[0]
	if model.ID != "gpt-catalog-test" || model.ProviderID != "openai" || model.DisplayName != "GPT Catalog Test" {
		t.Fatalf("model identity = %+v", model)
	}
	if model.ContextWindow != 262144 {
		t.Fatalf("context window = %d", model.ContextWindow)
	}
	if model.Status != "refreshed" {
		t.Fatalf("status = %q", model.Status)
	}
	for _, capability := range []string{"chat", "stream", "tools", "vision", "reasoning"} {
		if !contains(model.Capabilities, capability) {
			t.Fatalf("capability %q missing from %+v", capability, model.Capabilities)
		}
	}

	cached := catalog.Models("openai")
	if len(cached) != 1 || cached[0].ID != "gpt-catalog-test" {
		t.Fatalf("cached models = %+v", cached)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
