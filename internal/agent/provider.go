package agent

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"

	"superscience/internal/providers"
	"superscience/internal/store"
)

type ProviderUsage = providers.Usage
type ProviderToolCall = providers.ToolCall
type ProviderResult = providers.StreamResult

type Provider interface {
	Stream(ctx context.Context, request providers.ChatRequest, onEvent func(providers.StreamEvent)) (ProviderResult, error)
}

type UnavailableProvider struct {
	Reason string
}

func (p UnavailableProvider) Stream(context.Context, providers.ChatRequest, func(providers.StreamEvent)) (ProviderResult, error) {
	reason := strings.TrimSpace(p.Reason)
	if reason == "" {
		reason = "cloud provider is not configured"
	}
	return ProviderResult{}, errors.New(reason)
}

type AdapterProvider struct {
	Adapter providers.Adapter
	System  string
}

func (p AdapterProvider) Stream(ctx context.Context, request providers.ChatRequest, onEvent func(providers.StreamEvent)) (ProviderResult, error) {
	if p.Adapter == nil {
		return ProviderResult{}, errors.New("provider adapter is not configured")
	}
	if strings.TrimSpace(p.System) != "" {
		if strings.TrimSpace(request.System) == "" {
			request.System = p.System
		} else {
			request.System = p.System + "\n\n" + request.System
		}
	}
	return p.Adapter.Stream(ctx, request, onEvent)
}

func ProviderFromEnv() Provider {
	providerID := strings.TrimSpace(os.Getenv("SUPERSCIENCE_PROVIDER"))
	if providerID == "" || providerID == "mock" {
		return UnavailableProvider{Reason: "cloud provider is not configured"}
	}
	profile, ok := providers.BuiltInProfile(providerID)
	if !ok {
		profile = store.ProviderProfile{ID: providerID, Kind: "openai-compatible", BaseURL: os.Getenv("SUPERSCIENCE_BASE_URL"), Enabled: true}
	}
	if base := os.Getenv(envName(providerID, "BASE_URL")); base != "" {
		profile.BaseURL = base
	}
	if profile.BaseURL == "" {
		profile.BaseURL = os.Getenv("SUPERSCIENCE_BASE_URL")
	}
	model := os.Getenv(envName(providerID, "MODEL"))
	if model == "" {
		model = os.Getenv("SUPERSCIENCE_MODEL")
	}
	apiKey := os.Getenv(envName(providerID, "API_KEY"))
	if apiKey == "" {
		apiKey = os.Getenv("SUPERSCIENCE_API_KEY")
	}
	if apiKey == "" && providerID == "openai" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	if model == "" && providerID == "openai" {
		model = os.Getenv("OPENAI_MODEL")
	}
	if profile.BaseURL == "" && providerID == "openai" && os.Getenv("OPENAI_BASE_URL") != "" {
		profile.BaseURL = os.Getenv("OPENAI_BASE_URL")
	}
	if apiKey == "" && profile.ID != "ollama" {
		return UnavailableProvider{Reason: "cloud provider credentials are not configured for " + providerID}
	}
	adapter, err := providers.NewAdapter(providers.AdapterConfig{Profile: profile, APIKey: apiKey, Model: model, Client: http.DefaultClient})
	if err != nil {
		return UnavailableProvider{Reason: "provider configuration failed for " + providerID + ": " + err.Error()}
	}
	return AdapterProvider{Adapter: adapter, System: "You are Superscience, a local AI workbench for scientific research. Be concise, cite provenance when tools provide it, and ask for permission before risky actions."}
}

func envName(providerID string, suffix string) string {
	key := strings.ToUpper(strings.NewReplacer("-", "_", ".", "_").Replace(providerID))
	return "SUPERSCIENCE_" + key + "_" + suffix
}
