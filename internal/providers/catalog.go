package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const DefaultCatalogURL = "https://models.dev/api.json"

type Catalog struct {
	endpoint string
	client   *http.Client

	mu          sync.RWMutex
	byProvider  map[string][]Model
	lastRefresh time.Time
}

var (
	defaultCatalogMu sync.RWMutex
	defaultCatalog   = NewCatalog(DefaultCatalogURL, nil)
)

func NewCatalog(endpoint string, client *http.Client) *Catalog {
	if strings.TrimSpace(endpoint) == "" {
		endpoint = DefaultCatalogURL
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &Catalog{
		endpoint:   endpoint,
		client:     client,
		byProvider: map[string][]Model{},
	}
}

func RefreshDefaultCatalog(ctx context.Context, endpoint string, client *http.Client) ([]Model, error) {
	catalog := NewCatalog(endpoint, client)
	models, err := catalog.Refresh(ctx)
	if err != nil {
		return nil, err
	}
	defaultCatalogMu.Lock()
	defaultCatalog = catalog
	defaultCatalogMu.Unlock()
	return models, nil
}

func Models(providerID string) []Model {
	defaultCatalogMu.RLock()
	catalog := defaultCatalog
	defaultCatalogMu.RUnlock()
	return catalog.Models(providerID)
}

func (c *Catalog) Refresh(ctx context.Context) ([]Model, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Superscience/0.1 (+https://superscience.local)")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("model catalog fetch failed: %s", resp.Status)
	}
	var payload any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	models := normalizeCatalog(payload)
	if len(models) == 0 {
		return nil, fmt.Errorf("model catalog did not contain provider models")
	}
	grouped := groupModels(models)
	c.mu.Lock()
	c.byProvider = grouped
	c.lastRefresh = time.Now()
	c.mu.Unlock()
	return models, nil
}

func (c *Catalog) Models(providerID string) []Model {
	c.mu.RLock()
	models := append([]Model(nil), c.byProvider[providerID]...)
	c.mu.RUnlock()
	if len(models) > 0 {
		return models
	}
	return BundledModels(providerID)
}

func normalizeCatalog(payload any) []Model {
	providers := object(payload)
	if nested := object(providers["providers"]); len(nested) > 0 {
		providers = nested
	}
	result := make([]Model, 0)
	for providerID, rawProvider := range providers {
		provider := object(rawProvider)
		id := firstString(provider, "id", "provider", "provider_id")
		if id == "" {
			id = providerID
		}
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		for modelKey, rawModel := range modelEntries(provider["models"]) {
			model := object(rawModel)
			modelID := firstString(model, "id", "model", "model_id")
			if modelID == "" {
				modelID = modelKey
			}
			modelID = strings.TrimSpace(modelID)
			if modelID == "" {
				continue
			}
			displayName := firstString(model, "name", "displayName", "display_name", "title")
			if displayName == "" {
				displayName = modelID
			}
			result = append(result, Model{
				ID:            modelID,
				ProviderID:    id,
				DisplayName:   displayName,
				ContextWindow: contextWindow(model),
				Capabilities:  modelCapabilities(model),
				Status:        "refreshed",
			})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].ProviderID != result[j].ProviderID {
			return result[i].ProviderID < result[j].ProviderID
		}
		if result[i].DisplayName != result[j].DisplayName {
			return result[i].DisplayName < result[j].DisplayName
		}
		return result[i].ID < result[j].ID
	})
	return result
}

func modelEntries(raw any) map[string]any {
	switch typed := raw.(type) {
	case map[string]any:
		return typed
	case []any:
		result := map[string]any{}
		for i, item := range typed {
			model := object(item)
			id := firstString(model, "id", "model", "model_id")
			if id == "" {
				id = strconv.Itoa(i)
			}
			result[id] = item
		}
		return result
	default:
		return map[string]any{}
	}
}

func groupModels(models []Model) map[string][]Model {
	grouped := map[string][]Model{}
	for _, model := range models {
		grouped[model.ProviderID] = append(grouped[model.ProviderID], model)
	}
	return grouped
}

func object(value any) map[string]any {
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	return map[string]any{}
}

func firstString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			switch typed := value.(type) {
			case string:
				if trimmed := strings.TrimSpace(typed); trimmed != "" {
					return trimmed
				}
			case fmt.Stringer:
				if trimmed := strings.TrimSpace(typed.String()); trimmed != "" {
					return trimmed
				}
			}
		}
	}
	return ""
}

func contextWindow(model map[string]any) int {
	for _, nestedKey := range []string{"limit", "limits"} {
		nested := object(model[nestedKey])
		if value := intValue(nested["context"]); value > 0 {
			return value
		}
	}
	for _, key := range []string{"context", "context_window", "contextWindow", "context_length", "contextLength"} {
		if value := intValue(model[key]); value > 0 {
			return value
		}
	}
	return 0
}

func intValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return int(parsed)
	case string:
		parsed, _ := strconv.Atoi(strings.ReplaceAll(typed, ",", ""))
		return parsed
	default:
		return 0
	}
}

func modelCapabilities(model map[string]any) []string {
	capabilities := map[string]bool{
		"chat":   true,
		"stream": true,
	}
	addIf(capabilities, boolValue(model["tool_call"]) || boolValue(model["tools"]) || hasToken(model, "tools", "tool", "function", "tool_call") || hasToken(model, "features", "tool", "function", "tool_call"), "tools")
	addIf(capabilities, boolValue(model["reasoning"]) || hasToken(model, "reasoning_options", "reasoning") || hasToken(model, "features", "reasoning"), "reasoning")
	addIf(capabilities, boolValue(model["structured_output"]) || boolValue(model["json"]) || hasToken(model, "features", "structured"), "structured-output")
	addIf(capabilities, boolValue(model["open_weights"]) || hasToken(model, "features", "open_weights", "local"), "open-weights")

	modalities := object(model["modalities"])
	inputs := stringValues(modalities["input"])
	for _, input := range inputs {
		switch strings.ToLower(input) {
		case "image", "vision":
			capabilities["vision"] = true
		case "audio":
			capabilities["audio"] = true
		case "video":
			capabilities["video"] = true
		case "pdf":
			capabilities["pdf"] = true
		}
	}
	idAndName := strings.ToLower(firstString(model, "id", "name"))
	if strings.Contains(idAndName, "embed") {
		capabilities["embeddings"] = true
	}
	result := make([]string, 0, len(capabilities))
	for capability := range capabilities {
		result = append(result, capability)
	}
	sort.Strings(result)
	return result
}

func addIf(values map[string]bool, condition bool, value string) {
	if condition {
		values[value] = true
	}
}

func boolValue(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return typed == "true" || typed == "yes" || typed == "1"
	case float64:
		return typed != 0
	default:
		return false
	}
}

func hasToken(model map[string]any, key string, needles ...string) bool {
	for _, value := range stringValues(model[key]) {
		value = strings.ToLower(value)
		for _, needle := range needles {
			if strings.Contains(value, strings.ToLower(needle)) {
				return true
			}
		}
	}
	return false
}

func stringValues(value any) []string {
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				result = append(result, text)
			}
		}
		return result
	case string:
		return []string{typed}
	default:
		return nil
	}
}
