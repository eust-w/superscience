package providers_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"superscience/internal/providers"
	"superscience/internal/store"
)

func TestNewAdapterRejectsMockAndEmptyProviderProfiles(t *testing.T) {
	for _, profile := range []store.ProviderProfile{
		{},
		{ID: "mock"},
	} {
		adapter, err := providers.NewAdapter(providers.AdapterConfig{Profile: profile})
		if err == nil || adapter != nil {
			t.Fatalf("profile %+v created production adapter %T without error", profile, adapter)
		}
		if !strings.Contains(strings.ToLower(err.Error()), "provider") {
			t.Fatalf("profile %+v returned unclear error %q", profile, err)
		}
	}
}

func TestUnknownProviderBundledCatalogIsAnEmptyArray(t *testing.T) {
	models := providers.BundledModels("custom-provider")
	if models == nil || len(models) != 0 {
		t.Fatalf("unknown provider bundled models = %#v", models)
	}
	encoded, err := json.Marshal(models)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != "[]" {
		t.Fatalf("unknown provider JSON = %s", encoded)
	}
}

func TestProviderHTTPErrorPreservesStructuredRetryMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-request-id", "req_rate_limit_1")
		w.Header().Set("retry-after", "3")
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{
			"message": "rate limit exceeded",
			"code":    "rate_limit_exceeded",
			"type":    "rate_limit_error",
		}})
	}))
	defer server.Close()
	adapter, err := providers.NewAdapter(providers.AdapterConfig{
		Profile: store.ProviderProfile{ID: "openai", Kind: "openai", BaseURL: server.URL + "/v1"},
		APIKey:  "sk-test",
		Model:   "test-model",
		Client:  server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = adapter.Stream(context.Background(), providers.ChatRequest{Messages: []providers.Message{{Role: "user", Content: "health"}}}, func(providers.StreamEvent) {})
	var providerErr providers.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("error type = %T %v", err, err)
	}
	if providerErr.Code != "rate_limit_exceeded" || providerErr.HTTPStatus != http.StatusTooManyRequests || !providerErr.Retryable || providerErr.RequestID != "req_rate_limit_1" || providerErr.Details["retryAfter"] != "3" {
		t.Fatalf("provider error = %+v", providerErr)
	}
}

func TestOpenAICompatibleAdapterNormalizesStreamingTextToolCallsAndUsage(t *testing.T) {
	var sawAuthorization bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") == "Bearer sk-test" {
			sawAuthorization = true
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"Hel\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"lo\",\"tool_calls\":[{\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"science_search\",\"arguments\":\"{\\\"database\\\":\\\"pubmed\\\"}\"}}]}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[],\"usage\":{\"prompt_tokens\":4,\"completion_tokens\":2,\"total_tokens\":6}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	adapter, err := providers.NewAdapter(providers.AdapterConfig{
		Profile: store.ProviderProfile{ID: "openrouter", Kind: "openai-compatible", BaseURL: server.URL + "/v1"},
		APIKey:  "sk-test",
		Model:   "openrouter/auto",
		Client:  server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	var events []providers.StreamEvent
	result, err := adapter.Stream(context.Background(), providers.ChatRequest{
		Messages: []providers.Message{{Role: "user", Content: "search EGFR"}},
	}, func(event providers.StreamEvent) {
		events = append(events, event)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !sawAuthorization {
		t.Fatal("adapter did not send bearer authorization")
	}
	if result.Text != "Hello" {
		t.Fatalf("text = %q", result.Text)
	}
	if result.Usage.TotalTokens != 6 {
		t.Fatalf("usage = %+v", result.Usage)
	}
	if len(result.ToolCalls) != 1 || result.ToolCalls[0].Name != "science_search" || !strings.Contains(result.ToolCalls[0].Arguments, "pubmed") {
		t.Fatalf("tool calls = %+v", result.ToolCalls)
	}
	if countEvents(events, "text_delta") != 2 || countEvents(events, "tool_call") != 1 || countEvents(events, "usage") != 1 {
		t.Fatalf("events = %+v", events)
	}
}

func TestOpenAIAdapterNormalizesAttachmentsFinishReasonAndProviderMetadata(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("x-request-id", "req_openai_1")
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_1\",\"model\":\"vision-model\",\"choices\":[{\"delta\":{\"content\":\"observed\"},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()
	adapter, err := providers.NewAdapter(providers.AdapterConfig{
		Profile: store.ProviderProfile{ID: "openai", Kind: "openai", BaseURL: server.URL + "/v1"},
		APIKey:  "sk-test",
		Model:   "vision-model",
		Client:  server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := adapter.Stream(context.Background(), providers.ChatRequest{
		Messages: []providers.Message{{
			Role:    "user",
			Content: "Inspect this microscopy image.",
			Attachments: []providers.Attachment{{
				Name:     "cell.png",
				MIMEType: "image/png",
				Data:     "aW1hZ2U=",
			}},
		}},
	}, func(providers.StreamEvent) {})
	if err != nil {
		t.Fatal(err)
	}
	if result.FinishReason != "stop" || result.ProviderMetadata["requestId"] != "req_openai_1" || result.ProviderMetadata["model"] != "vision-model" {
		t.Fatalf("normalized result = %+v", result)
	}
	streamOptions, _ := requestBody["stream_options"].(map[string]any)
	if streamOptions["include_usage"] != true {
		t.Fatalf("stream usage was not requested: %+v", requestBody)
	}
	messages := requestBody["messages"].([]any)
	content, ok := messages[0].(map[string]any)["content"].([]any)
	if !ok || len(content) != 2 {
		t.Fatalf("multimodal content = %+v", messages[0])
	}
	imageBlock := content[1].(map[string]any)
	imageURL := imageBlock["image_url"].(map[string]any)["url"]
	if imageBlock["type"] != "image_url" || imageURL != "data:image/png;base64,aW1hZ2U=" {
		t.Fatalf("image block = %+v", imageBlock)
	}
}

func TestAzureOpenAIAdapterUsesAzureV1EndpointAndAPIKeyHeader(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/openai/v1/chat/completions" {
			t.Fatalf("azure path = %s", r.URL.Path)
		}
		if r.Header.Get("api-key") != "azure-secret" || r.Header.Get("Authorization") != "" {
			t.Fatalf("azure auth headers = %+v", r.Header)
		}
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"azure ready\"},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()
	adapter, err := providers.NewAdapter(providers.AdapterConfig{
		Profile: store.ProviderProfile{ID: "azure-openai", Kind: "azure-openai", BaseURL: server.URL},
		APIKey:  "azure-secret",
		Model:   "gpt-deployment",
		Client:  server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := adapter.Stream(context.Background(), providers.ChatRequest{Messages: []providers.Message{{Role: "user", Content: "health"}}}, func(providers.StreamEvent) {})
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "azure ready" || result.FinishReason != "stop" || requestBody["model"] != "gpt-deployment" {
		t.Fatalf("azure result=%+v request=%+v", result, requestBody)
	}
}

func TestOpenAICompatibleAdapterSendsNativeToolsAndStructuredToolResults(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"done\"},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	adapter, err := providers.NewAdapter(providers.AdapterConfig{
		Profile: store.ProviderProfile{ID: "openai-compatible", Kind: "openai-compatible", BaseURL: server.URL + "/v1"},
		APIKey:  "sk-test",
		Model:   "tool-model",
		Client:  server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = adapter.Stream(context.Background(), providers.ChatRequest{
		Messages: []providers.Message{
			{Role: "user", Content: "Find EGFR evidence"},
			{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "call_1", Name: "science_search", Arguments: `{"database":"pubmed","query":"EGFR"}`}}},
			{Role: "tool", ToolCallID: "call_1", Content: `{"hits":1}`},
		},
		Tools: []providers.ToolDefinition{{
			Name:        "science_search",
			Description: "Search scientific databases",
			InputSchema: map[string]any{"type": "object", "required": []string{"database", "query"}},
		}},
		ToolChoice: "auto",
	}, func(providers.StreamEvent) {})
	if err != nil {
		t.Fatal(err)
	}

	tools, ok := requestBody["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("native tools missing from request: %+v", requestBody)
	}
	function := tools[0].(map[string]any)["function"].(map[string]any)
	if function["name"] != "science_search" || function["parameters"] == nil {
		t.Fatalf("tool schema = %+v", function)
	}
	messages := requestBody["messages"].([]any)
	assistant := messages[1].(map[string]any)
	toolMessage := messages[2].(map[string]any)
	if assistant["tool_calls"] == nil {
		t.Fatalf("assistant tool call missing: %+v", assistant)
	}
	if toolMessage["role"] != "tool" || toolMessage["tool_call_id"] != "call_1" || toolMessage["content"] != `{"hits":1}` {
		t.Fatalf("tool result message = %+v", toolMessage)
	}
	if requestBody["tool_choice"] != "auto" {
		t.Fatalf("tool choice = %+v", requestBody["tool_choice"])
	}
}

func TestOpenAICompatibleAdapterMergesStreamingToolCallArgumentChunks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"science_search\",\"arguments\":\"{\\\"database\\\":\"}}]}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"\\\"pubmed\\\",\\\"query\\\":\\\"EGFR\\\"}\"}}]}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	adapter, err := providers.NewAdapter(providers.AdapterConfig{
		Profile: store.ProviderProfile{ID: "deepseek", Kind: "openai-compatible", BaseURL: server.URL + "/v1"},
		APIKey:  "sk-test",
		Model:   "deepseek-chat",
		Client:  server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	var events []providers.StreamEvent
	result, err := adapter.Stream(context.Background(), providers.ChatRequest{
		Messages: []providers.Message{{Role: "user", Content: "search EGFR"}},
	}, func(event providers.StreamEvent) {
		events = append(events, event)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ToolCalls) != 1 {
		t.Fatalf("tool calls = %+v", result.ToolCalls)
	}
	call := result.ToolCalls[0]
	if call.ID != "call_1" || call.Name != "science_search" || call.Arguments != `{"database":"pubmed","query":"EGFR"}` {
		t.Fatalf("merged call = %+v", call)
	}
	if countEvents(events, "tool_call") != 1 {
		t.Fatalf("tool call should emit once after merge: %+v", events)
	}
}

func TestAnthropicAdapterNormalizesStreamingTextToolUseAndUsage(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "sk-ant-test" {
			t.Fatalf("missing anthropic key header: %+v", r.Header)
		}
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("request-id", "req_anthropic_1")
		_, _ = w.Write([]byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"Alpha\"}}\n\n"))
		_, _ = w.Write([]byte("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_1\",\"name\":\"science_search\",\"input\":{\"database\":\"pubmed\"}}}\n\n"))
		_, _ = w.Write([]byte("event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"},\"usage\":{\"output_tokens\":5}}\n\n"))
		_, _ = w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer server.Close()

	adapter, err := providers.NewAdapter(providers.AdapterConfig{
		Profile: store.ProviderProfile{ID: "anthropic", Kind: "anthropic", BaseURL: server.URL},
		APIKey:  "sk-ant-test",
		Model:   "claude-sonnet-4.5",
		Client:  server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	var events []providers.StreamEvent
	result, err := adapter.Stream(context.Background(), providers.ChatRequest{
		System:   "You are a scientist.",
		Messages: []providers.Message{{Role: "user", Content: "search EGFR"}},
		Tools: []providers.ToolDefinition{{
			Name:        "science_search",
			Description: "Search scientific databases",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string"}}},
		}},
		ToolChoice: "auto",
	}, func(event providers.StreamEvent) {
		events = append(events, event)
	})
	if err != nil {
		t.Fatal(err)
	}
	if requestBody["system"] != "You are a scientist." || requestBody["model"] != "claude-sonnet-4.5" || requestBody["tools"] == nil {
		t.Fatalf("request body = %+v", requestBody)
	}
	if result.Text != "Alpha" || result.Usage.CompletionTokens != 5 {
		t.Fatalf("result = %+v", result)
	}
	if result.FinishReason != "tool_use" || result.ProviderMetadata["requestId"] != "req_anthropic_1" {
		t.Fatalf("anthropic metadata = %+v", result)
	}
	if len(result.ToolCalls) != 1 || result.ToolCalls[0].ID != "toolu_1" || !strings.Contains(result.ToolCalls[0].Arguments, "pubmed") {
		t.Fatalf("tool calls = %+v", result.ToolCalls)
	}
	if countEvents(events, "text_delta") != 1 || countEvents(events, "tool_call") != 1 || countEvents(events, "usage") != 1 {
		t.Fatalf("events = %+v", events)
	}
}

func TestGeminiAdapterNormalizesStreamingTextFunctionCallsAndUsage(t *testing.T) {
	var requestBody map[string]any
	var sawAPIKey bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1beta/models/gemini-test:streamGenerateContent" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.URL.Query().Get("alt") != "sse" {
			t.Fatalf("missing sse query: %s", r.URL.RawQuery)
		}
		if r.URL.Query().Get("key") == "gemini-key" {
			sawAPIKey = true
		}
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("x-request-id", "req_gemini_1")
		_, _ = w.Write([]byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"Gene\"}]}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"candidates\":[{\"finishReason\":\"STOP\",\"content\":{\"parts\":[{\"text\":\" map\"},{\"functionCall\":{\"name\":\"science_search\",\"args\":{\"database\":\"pubmed\",\"query\":\"EGFR\"}}}]}}],\"usageMetadata\":{\"promptTokenCount\":6,\"candidatesTokenCount\":3,\"totalTokenCount\":9}}\n\n"))
	}))
	defer server.Close()

	adapter, err := providers.NewAdapter(providers.AdapterConfig{
		Profile: store.ProviderProfile{ID: "gemini", Kind: "google", BaseURL: server.URL},
		APIKey:  "gemini-key",
		Model:   "gemini-test",
		Client:  server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	var events []providers.StreamEvent
	result, err := adapter.Stream(context.Background(), providers.ChatRequest{
		System:   "You are a scientist.",
		Messages: []providers.Message{{Role: "user", Content: "search EGFR"}},
		Tools: []providers.ToolDefinition{{
			Name:        "science_search",
			Description: "Search scientific databases",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string"}}},
		}},
	}, func(event providers.StreamEvent) {
		events = append(events, event)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !sawAPIKey {
		t.Fatal("gemini adapter did not send API key query")
	}
	if requestBody["system_instruction"] == nil || requestBody["contents"] == nil || requestBody["tools"] == nil {
		t.Fatalf("request body missing Gemini fields: %+v", requestBody)
	}
	if result.Text != "Gene map" || result.Usage.TotalTokens != 9 {
		t.Fatalf("result = %+v", result)
	}
	if result.FinishReason != "stop" || result.ProviderMetadata["requestId"] != "req_gemini_1" {
		t.Fatalf("gemini metadata = %+v", result)
	}
	if len(result.ToolCalls) != 1 || result.ToolCalls[0].Name != "science_search" || !strings.Contains(result.ToolCalls[0].Arguments, "EGFR") {
		t.Fatalf("tool calls = %+v", result.ToolCalls)
	}
	if countEvents(events, "text_delta") != 2 || countEvents(events, "tool_call") != 1 || countEvents(events, "usage") != 1 {
		t.Fatalf("events = %+v", events)
	}
}

func TestBedrockAdapterUsesConverseRequestAndNormalizesResponse(t *testing.T) {
	var requestBody map[string]any
	var sawSigV4 bool
	var sawSessionToken bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.RequestURI, "/model/anthropic.claude-3-sonnet-20240229-v1:0/converse") {
			t.Fatalf("unexpected bedrock request uri %s", r.RequestURI)
		}
		if strings.Contains(r.Header.Get("Authorization"), "Credential=AKID/") && strings.Contains(r.Header.Get("Authorization"), "/us-west-2/bedrock/aws4_request") {
			sawSigV4 = true
		}
		if r.Header.Get("X-Amz-Security-Token") == "SESSION" {
			sawSessionToken = true
		}
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("x-amzn-requestid", "req_bedrock_1")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"output": map[string]any{
				"message": map[string]any{
					"role": "assistant",
					"content": []map[string]any{
						{"text": "Evidence map"},
						{"toolUse": map[string]any{
							"toolUseId": "tooluse_1",
							"name":      "science_search",
							"input":     map[string]any{"database": "pubmed", "query": "EGFR"},
						}},
					},
				},
			},
			"usage":      map[string]any{"inputTokens": 7, "outputTokens": 3, "totalTokens": 10},
			"stopReason": "tool_use",
		})
	}))
	defer server.Close()

	adapter, err := providers.NewAdapter(providers.AdapterConfig{
		Profile: store.ProviderProfile{ID: "bedrock", Kind: "bedrock", BaseURL: server.URL},
		APIKey:  "AKID:SECRET:us-west-2:SESSION",
		Model:   "anthropic.claude-3-sonnet-20240229-v1:0",
		Client:  server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	var events []providers.StreamEvent
	result, err := adapter.Stream(context.Background(), providers.ChatRequest{
		System:   "You are a scientist.",
		Messages: []providers.Message{{Role: "user", Content: "map EGFR evidence"}},
		Tools: []providers.ToolDefinition{{
			Name:        "science_search",
			Description: "Search scientific databases",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string"}}},
		}},
	}, func(event providers.StreamEvent) {
		events = append(events, event)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !sawSigV4 || !sawSessionToken {
		t.Fatalf("bedrock auth headers missing: sigv4=%v session=%v", sawSigV4, sawSessionToken)
	}
	if requestBody["system"] == nil || requestBody["messages"] == nil || requestBody["inferenceConfig"] == nil || requestBody["toolConfig"] == nil {
		t.Fatalf("bedrock request body missing converse fields: %+v", requestBody)
	}
	if result.Text != "Evidence map" || result.Usage.TotalTokens != 10 {
		t.Fatalf("result = %+v", result)
	}
	if result.FinishReason != "tool_use" || result.ProviderMetadata["requestId"] != "req_bedrock_1" {
		t.Fatalf("bedrock metadata = %+v", result)
	}
	if len(result.ToolCalls) != 1 || result.ToolCalls[0].ID != "tooluse_1" || !strings.Contains(result.ToolCalls[0].Arguments, "EGFR") {
		t.Fatalf("tool calls = %+v", result.ToolCalls)
	}
	if countEvents(events, "text_delta") != 1 || countEvents(events, "tool_call") != 1 || countEvents(events, "usage") != 1 {
		t.Fatalf("events = %+v", events)
	}
}

func countEvents(events []providers.StreamEvent, typ string) int {
	var count int
	for _, event := range events {
		if event.Type == typ {
			count++
		}
	}
	return count
}
