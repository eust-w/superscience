package providers

import (
	"sort"

	"superscience/internal/store"
)

type Model struct {
	ID            string   `json:"id"`
	ProviderID    string   `json:"providerId"`
	DisplayName   string   `json:"displayName"`
	ContextWindow int      `json:"contextWindow"`
	Capabilities  []string `json:"capabilities"`
	Status        string   `json:"status"`
}

func BuiltIns() []store.ProviderProfile {
	return []store.ProviderProfile{
		{ID: "openai", Kind: "openai", DisplayName: "OpenAI", BaseURL: "https://api.openai.com/v1", Enabled: true, Capabilities: []string{"chat", "stream", "tools", "vision", "embeddings"}},
		{ID: "openai-compatible", Kind: "openai-compatible", DisplayName: "OpenAI-compatible", Enabled: true, Capabilities: []string{"chat", "stream", "tools"}},
		{ID: "anthropic", Kind: "anthropic", DisplayName: "Anthropic", BaseURL: "https://api.anthropic.com", Enabled: true, Capabilities: []string{"chat", "stream", "tools", "vision"}},
		{ID: "gemini", Kind: "google", DisplayName: "Google Gemini", BaseURL: "https://generativelanguage.googleapis.com", Enabled: true, Capabilities: []string{"chat", "stream", "tools", "vision"}},
		{ID: "openrouter", Kind: "openai-compatible", DisplayName: "OpenRouter", BaseURL: "https://openrouter.ai/api/v1", Enabled: true, Capabilities: []string{"chat", "stream", "tools", "routing"}},
		{ID: "ollama", Kind: "openai-compatible", DisplayName: "Ollama / local", BaseURL: "http://127.0.0.1:11434/v1", Enabled: false, Capabilities: []string{"chat", "stream", "local"}},
		{ID: "azure-openai", Kind: "azure-openai", DisplayName: "Azure OpenAI", Enabled: true, Capabilities: []string{"chat", "stream", "tools", "enterprise"}},
		{ID: "bedrock", Kind: "bedrock", DisplayName: "AWS Bedrock", Enabled: true, Capabilities: []string{"chat", "stream", "enterprise"}},
		{ID: "mistral", Kind: "openai-compatible", DisplayName: "Mistral", BaseURL: "https://api.mistral.ai/v1", Enabled: true, Capabilities: []string{"chat", "stream", "tools"}},
		{ID: "groq", Kind: "openai-compatible", DisplayName: "Groq", BaseURL: "https://api.groq.com/openai/v1", Enabled: true, Capabilities: []string{"chat", "stream", "low-latency"}},
		{ID: "xai", Kind: "openai-compatible", DisplayName: "xAI", BaseURL: "https://api.x.ai/v1", Enabled: true, Capabilities: []string{"chat", "stream", "tools"}},
		{ID: "deepseek", Kind: "openai-compatible", DisplayName: "DeepSeek", BaseURL: "https://api.deepseek.com/v1", Enabled: true, Capabilities: []string{"chat", "stream", "tools"}},
		{ID: "dashscope", Kind: "openai-compatible", DisplayName: "DashScope / Qwen", BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Enabled: true, Capabilities: []string{"chat", "stream", "tools"}},
	}
}

func MergeProfiles(overrides []store.ProviderProfile) []store.ProviderProfile {
	merged := map[string]store.ProviderProfile{}
	for _, provider := range BuiltIns() {
		merged[provider.ID] = provider
	}
	for _, provider := range overrides {
		base := merged[provider.ID]
		if provider.Kind == "" {
			provider.Kind = base.Kind
		}
		if provider.DisplayName == "" {
			provider.DisplayName = base.DisplayName
		}
		if len(provider.Capabilities) == 0 {
			provider.Capabilities = base.Capabilities
		}
		merged[provider.ID] = provider
	}
	result := make([]store.ProviderProfile, 0, len(merged))
	for _, provider := range merged {
		provider.SecretRef = ""
		result = append(result, provider)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].DisplayName < result[j].DisplayName })
	return result
}

func BundledModels(providerID string) []Model {
	catalog := map[string][]Model{
		"openai": {
			{ID: "gpt-5.4", ProviderID: "openai", DisplayName: "GPT 5.4", ContextWindow: 128000, Capabilities: []string{"chat", "stream", "tools", "vision"}, Status: "available"},
			{ID: "gpt-4o-mini", ProviderID: "openai", DisplayName: "GPT-4o mini", ContextWindow: 128000, Capabilities: []string{"chat", "stream", "tools"}, Status: "available"},
		},
		"anthropic": {
			{ID: "claude-sonnet-4.5", ProviderID: "anthropic", DisplayName: "Claude Sonnet 4.5", ContextWindow: 200000, Capabilities: []string{"chat", "stream", "tools", "vision"}, Status: "available"},
		},
		"gemini": {
			{ID: "gemini-2.5-pro", ProviderID: "gemini", DisplayName: "Gemini 2.5 Pro", ContextWindow: 1000000, Capabilities: []string{"chat", "stream", "tools", "vision"}, Status: "available"},
		},
		"openrouter": {
			{ID: "openrouter/auto", ProviderID: "openrouter", DisplayName: "OpenRouter Auto", ContextWindow: 128000, Capabilities: []string{"chat", "stream", "tools", "routing"}, Status: "available"},
		},
		"ollama": {
			{ID: "llama3.1", ProviderID: "ollama", DisplayName: "Llama 3.1 local", ContextWindow: 128000, Capabilities: []string{"chat", "stream", "local"}, Status: "local"},
		},
		"bedrock": {
			{ID: "bedrock/anthropic.claude", ProviderID: "bedrock", DisplayName: "Bedrock Claude", ContextWindow: 200000, Capabilities: []string{"chat", "stream", "enterprise"}, Status: "configured"},
		},
		"dashscope": {
			{ID: "qwen-max", ProviderID: "dashscope", DisplayName: "Qwen Max", ContextWindow: 128000, Capabilities: []string{"chat", "stream", "tools"}, Status: "available"},
		},
	}
	if models, ok := catalog[providerID]; ok {
		return models
	}
	return []Model{}
}

func ModelStats() (providersCount int, modelsCount int) {
	for _, provider := range BuiltIns() {
		providersCount++
		modelsCount += len(Models(provider.ID))
	}
	return providersCount, modelsCount
}
