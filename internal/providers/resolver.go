package providers

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"

	"superscience/internal/store"
)

type SecretLoader interface {
	Load(account string) (string, error)
}

// Resolver builds a provider adapter from the same persisted profile, environment,
// and secret backend used by web, desktop, CLI, and protocol entrypoints.
type Resolver struct {
	Store   *store.Store
	Secrets SecretLoader
	Client  *http.Client
}

func (r Resolver) Resolve(ctx context.Context, providerID, model string) (Adapter, error) {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		return nil, errors.New("provider is required")
	}
	if providerID == "mock" {
		return nil, errors.New("provider mock is not available in production")
	}
	profile, ok := BuiltInProfile(providerID)
	if r.Store != nil {
		overrides, err := r.Store.ListProviders(ctx)
		if err != nil {
			return nil, err
		}
		for _, override := range overrides {
			if override.ID != providerID {
				continue
			}
			if override.Kind != "" {
				profile.Kind = override.Kind
			}
			if override.DisplayName != "" {
				profile.DisplayName = override.DisplayName
			}
			if override.BaseURL != "" {
				profile.BaseURL = override.BaseURL
			}
			profile.ID = providerID
			profile.Enabled = override.Enabled
			profile.SecretRef = override.SecretRef
			if len(override.Capabilities) > 0 {
				profile.Capabilities = override.Capabilities
			}
			ok = true
			break
		}
	}
	if !ok {
		profile = store.ProviderProfile{ID: providerID, Kind: "openai-compatible", DisplayName: providerID, Enabled: true}
	}
	if !profile.Enabled {
		return nil, errors.New("provider is disabled: " + providerID)
	}
	apiKey := APIKeyFromEnv(providerID)
	if apiKey == "" && r.Secrets != nil {
		account := providerID
		if strings.TrimSpace(profile.SecretRef) != "" {
			account = strings.TrimSpace(profile.SecretRef)
			if strings.HasPrefix(account, "keychain:") {
				account = strings.TrimSpace(strings.TrimPrefix(account, "keychain:"))
			}
			if account == "" {
				account = providerID
			}
		}
		if loaded, err := r.Secrets.Load(account); err == nil {
			apiKey = strings.TrimSpace(loaded)
		}
	}
	client := r.Client
	if client == nil {
		client = http.DefaultClient
	}
	return NewAdapter(AdapterConfig{Profile: profile, APIKey: apiKey, Model: strings.TrimSpace(model), Client: client})
}

func APIKeyFromEnv(providerID string) string {
	key := strings.ToUpper(strings.NewReplacer("-", "_", ".", "_").Replace(providerID))
	for _, name := range []string{
		"SUPERSCIENCE_" + key + "_API_KEY",
		"SUPERSCIENCE_API_KEY",
		key + "_API_KEY",
	} {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	if providerID == "openai" {
		return strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	}
	if providerID == "anthropic" {
		return strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY"))
	}
	if providerID == "gemini" {
		return strings.TrimSpace(os.Getenv("GEMINI_API_KEY"))
	}
	return ""
}
