package providers

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"superscience/internal/store"
)

type Message struct {
	Role        string       `json:"role"`
	Content     string       `json:"content,omitempty"`
	Name        string       `json:"name,omitempty"`
	ToolCallID  string       `json:"toolCallId,omitempty"`
	ToolCalls   []ToolCall   `json:"toolCalls,omitempty"`
	Attachments []Attachment `json:"attachments,omitempty"`
}

type Attachment struct {
	Name     string `json:"name,omitempty"`
	MIMEType string `json:"mimeType"`
	URL      string `json:"url,omitempty"`
	Data     string `json:"data,omitempty"`
}

type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema"`
}

type ChatRequest struct {
	System     string           `json:"system,omitempty"`
	Messages   []Message        `json:"messages"`
	Tools      []ToolDefinition `json:"tools,omitempty"`
	ToolChoice string           `json:"toolChoice,omitempty"`
}

type Usage struct {
	PromptTokens     int `json:"promptTokens"`
	CompletionTokens int `json:"completionTokens"`
	TotalTokens      int `json:"totalTokens"`
}

type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type StreamEvent struct {
	Type             string            `json:"type"`
	Delta            string            `json:"delta,omitempty"`
	ToolCall         *ToolCall         `json:"toolCall,omitempty"`
	Usage            *Usage            `json:"usage,omitempty"`
	FinishReason     string            `json:"finishReason,omitempty"`
	ProviderMetadata map[string]string `json:"providerMetadata,omitempty"`
}

type StreamResult struct {
	Text             string            `json:"text"`
	ToolCalls        []ToolCall        `json:"toolCalls"`
	Usage            Usage             `json:"usage"`
	FinishReason     string            `json:"finishReason"`
	ProviderMetadata map[string]string `json:"providerMetadata"`
}

type Adapter interface {
	Stream(ctx context.Context, request ChatRequest, onEvent func(StreamEvent)) (StreamResult, error)
}

type AdapterConfig struct {
	Profile store.ProviderProfile
	APIKey  string
	Model   string
	Client  *http.Client
}

type ProviderError struct {
	Provider   string            `json:"provider"`
	Status     string            `json:"status"`
	HTTPStatus int               `json:"httpStatus"`
	Code       string            `json:"code"`
	Message    string            `json:"message"`
	Retryable  bool              `json:"retryable"`
	RequestID  string            `json:"requestId"`
	Details    map[string]string `json:"details"`
}

func (e ProviderError) Error() string {
	if e.Message == "" {
		return strings.TrimSpace(e.Provider + " " + e.Status)
	}
	return strings.TrimSpace(e.Provider + " " + e.Status + ": " + e.Message)
}

func NewAdapter(config AdapterConfig) (Adapter, error) {
	kind := strings.ToLower(strings.TrimSpace(config.Profile.Kind))
	id := strings.ToLower(strings.TrimSpace(config.Profile.ID))
	if id == "" {
		return nil, errors.New("provider must be configured before creating an adapter")
	}
	if id == "mock" {
		return nil, errors.New("provider mock is not available in production")
	}
	switch kind {
	case "openai", "openai-compatible":
		return openAICompatibleAdapter{config: withDefaults(config)}, nil
	case "azure-openai":
		return azureOpenAIAdapter{config: withDefaults(config)}, nil
	case "anthropic":
		return anthropicAdapter{config: withDefaults(config)}, nil
	case "google", "gemini":
		return geminiAdapter{config: withDefaults(config)}, nil
	case "bedrock":
		return bedrockAdapter{config: withDefaults(config)}, nil
	}
	if id == "openrouter" || id == "ollama" || id == "mistral" || id == "groq" || id == "xai" || id == "deepseek" || id == "dashscope" {
		return openAICompatibleAdapter{config: withDefaults(config)}, nil
	}
	return nil, fmt.Errorf("unsupported provider adapter %q (%s)", config.Profile.ID, config.Profile.Kind)
}

func BuiltInProfile(id string) (store.ProviderProfile, bool) {
	for _, profile := range BuiltIns() {
		if profile.ID == id {
			return profile, true
		}
	}
	return store.ProviderProfile{}, false
}

func withDefaults(config AdapterConfig) AdapterConfig {
	if config.Client == nil {
		config.Client = http.DefaultClient
	}
	if config.Model == "" {
		models := Models(config.Profile.ID)
		if len(models) > 0 {
			config.Model = models[0].ID
		}
	}
	if config.Profile.BaseURL == "" {
		if profile, ok := BuiltInProfile(config.Profile.ID); ok {
			config.Profile.BaseURL = profile.BaseURL
			if config.Profile.Kind == "" {
				config.Profile.Kind = profile.Kind
			}
		}
	}
	return config
}

type openAICompatibleAdapter struct {
	config AdapterConfig
}

func (a openAICompatibleAdapter) Stream(ctx context.Context, request ChatRequest, onEvent func(StreamEvent)) (StreamResult, error) {
	payload, err := openAIChatPayload(request, a.config.Model)
	if err != nil {
		return StreamResult{}, err
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openAIChatURL(a.config.Profile.BaseURL), bytes.NewReader(body))
	if err != nil {
		return StreamResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if a.config.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+a.config.APIKey)
	}
	resp, err := a.config.Client.Do(req)
	if err != nil {
		return StreamResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return StreamResult{}, normalizeHTTPError(a.config.Profile.ID, resp)
	}
	result, err := parseOpenAIStream(resp, onEvent)
	return completeProviderResult(result, resp, a.config.Profile.ID, a.config.Model), err
}

func openAIChatPayload(request ChatRequest, model string) (map[string]any, error) {
	messages := make([]map[string]any, 0, len(request.Messages)+1)
	if request.System != "" {
		messages = append(messages, map[string]any{"role": "system", "content": request.System})
	}
	for _, message := range request.Messages {
		item := map[string]any{"role": message.Role}
		content, err := openAIMessageContent(message)
		if err != nil {
			return nil, err
		}
		if message.Content != "" || len(message.ToolCalls) == 0 || len(message.Attachments) > 0 {
			item["content"] = content
		} else {
			item["content"] = nil
		}
		if message.ToolCallID != "" {
			item["tool_call_id"] = message.ToolCallID
		}
		if len(message.ToolCalls) > 0 {
			calls := make([]map[string]any, 0, len(message.ToolCalls))
			for _, call := range message.ToolCalls {
				calls = append(calls, map[string]any{
					"id":   call.ID,
					"type": "function",
					"function": map[string]any{
						"name":      call.Name,
						"arguments": call.Arguments,
					},
				})
			}
			item["tool_calls"] = calls
		}
		messages = append(messages, item)
	}
	payload := map[string]any{"model": model, "stream": true, "stream_options": map[string]any{"include_usage": true}, "messages": messages}
	if len(request.Tools) > 0 {
		tools := make([]map[string]any, 0, len(request.Tools))
		for _, tool := range request.Tools {
			parameters := tool.InputSchema
			if parameters == nil {
				parameters = map[string]any{"type": "object", "properties": map[string]any{}}
			}
			tools = append(tools, map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        tool.Name,
					"description": tool.Description,
					"parameters":  parameters,
				},
			})
		}
		payload["tools"] = tools
		choice := strings.TrimSpace(request.ToolChoice)
		if choice == "" {
			choice = "auto"
		}
		payload["tool_choice"] = choice
	}
	return payload, nil
}

func openAIMessageContent(message Message) (any, error) {
	if len(message.Attachments) == 0 {
		return message.Content, nil
	}
	if message.Role == "tool" {
		return nil, errors.New("OpenAI-compatible tool result messages do not support attachments")
	}
	blocks := make([]map[string]any, 0, len(message.Attachments)+1)
	if message.Content != "" {
		blocks = append(blocks, map[string]any{"type": "text", "text": message.Content})
	}
	for _, attachment := range message.Attachments {
		mimeType := strings.TrimSpace(attachment.MIMEType)
		if !strings.HasPrefix(mimeType, "image/") {
			return nil, fmt.Errorf("OpenAI-compatible chat attachments require an image MIME type, got %q", mimeType)
		}
		source := strings.TrimSpace(attachment.URL)
		if source == "" && strings.TrimSpace(attachment.Data) != "" {
			source = "data:" + mimeType + ";base64," + strings.TrimSpace(attachment.Data)
		}
		if source == "" {
			return nil, errors.New("attachment URL or base64 data is required")
		}
		blocks = append(blocks, map[string]any{"type": "image_url", "image_url": map[string]any{"url": source}})
	}
	return blocks, nil
}

func openAIChatURL(base string) string {
	base = strings.TrimRight(base, "/")
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	if strings.HasSuffix(base, "/v1") {
		return base + "/chat/completions"
	}
	return base + "/v1/chat/completions"
}

type azureOpenAIAdapter struct {
	config AdapterConfig
}

func (a azureOpenAIAdapter) Stream(ctx context.Context, request ChatRequest, onEvent func(StreamEvent)) (StreamResult, error) {
	payload, err := openAIChatPayload(request, a.config.Model)
	if err != nil {
		return StreamResult{}, err
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, azureOpenAIChatURL(a.config.Profile.BaseURL), bytes.NewReader(body))
	if err != nil {
		return StreamResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if a.config.APIKey != "" {
		req.Header.Set("api-key", a.config.APIKey)
	}
	resp, err := a.config.Client.Do(req)
	if err != nil {
		return StreamResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return StreamResult{}, normalizeHTTPError(a.config.Profile.ID, resp)
	}
	result, err := parseOpenAIStream(resp, onEvent)
	return completeProviderResult(result, resp, a.config.Profile.ID, a.config.Model), err
}

func azureOpenAIChatURL(base string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if strings.HasSuffix(base, "/openai/v1") {
		return base + "/chat/completions"
	}
	return base + "/openai/v1/chat/completions"
}

func parseOpenAIStream(resp *http.Response, onEvent func(StreamEvent)) (StreamResult, error) {
	if onEvent == nil {
		onEvent = func(StreamEvent) {}
	}
	var result StreamResult
	pendingToolCalls := map[int]*ToolCall{}
	pendingOrder := []int{}
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		data, ok := sseData(scanner.Text())
		if !ok {
			continue
		}
		if data == "[DONE]" {
			break
		}
		var event struct {
			ID      string `json:"id"`
			Model   string `json:"model"`
			Choices []struct {
				FinishReason string `json:"finish_reason"`
				Delta        struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    *int   `json:"index"`
						ID       string `json:"id"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
			Usage struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
				TotalTokens      int `json:"total_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}
		if result.ProviderMetadata == nil {
			result.ProviderMetadata = map[string]string{}
		}
		if event.ID != "" {
			result.ProviderMetadata["responseId"] = event.ID
		}
		if event.Model != "" {
			result.ProviderMetadata["model"] = event.Model
		}
		for _, choice := range event.Choices {
			if choice.FinishReason != "" {
				result.FinishReason = choice.FinishReason
				onEvent(StreamEvent{Type: "finish", FinishReason: choice.FinishReason})
			}
			if choice.Delta.Content != "" {
				result.Text += choice.Delta.Content
				onEvent(StreamEvent{Type: "text_delta", Delta: choice.Delta.Content})
			}
			for _, raw := range choice.Delta.ToolCalls {
				if raw.Index != nil {
					call, ok := pendingToolCalls[*raw.Index]
					if !ok {
						call = &ToolCall{}
						pendingToolCalls[*raw.Index] = call
						pendingOrder = append(pendingOrder, *raw.Index)
					}
					if raw.ID != "" {
						call.ID = raw.ID
					}
					if raw.Function.Name != "" {
						call.Name = raw.Function.Name
					}
					call.Arguments += raw.Function.Arguments
					continue
				}
				call := ToolCall{ID: raw.ID, Name: raw.Function.Name, Arguments: raw.Function.Arguments}
				result.ToolCalls = append(result.ToolCalls, call)
				onEvent(StreamEvent{Type: "tool_call", ToolCall: &call})
			}
		}
		if event.Usage.TotalTokens != 0 || event.Usage.PromptTokens != 0 || event.Usage.CompletionTokens != 0 {
			result.Usage = Usage{PromptTokens: event.Usage.PromptTokens, CompletionTokens: event.Usage.CompletionTokens, TotalTokens: event.Usage.TotalTokens}
			onEvent(StreamEvent{Type: "usage", Usage: &result.Usage})
		}
	}
	for _, index := range pendingOrder {
		call := *pendingToolCalls[index]
		result.ToolCalls = append(result.ToolCalls, call)
		onEvent(StreamEvent{Type: "tool_call", ToolCall: &call})
	}
	return result, scanner.Err()
}

func completeProviderResult(result StreamResult, response *http.Response, providerID, model string) StreamResult {
	if result.ProviderMetadata == nil {
		result.ProviderMetadata = map[string]string{}
	}
	result.ProviderMetadata["provider"] = providerID
	if result.ProviderMetadata["model"] == "" {
		result.ProviderMetadata["model"] = model
	}
	for _, header := range []string{"x-request-id", "request-id", "x-amzn-requestid"} {
		if value := strings.TrimSpace(response.Header.Get(header)); value != "" {
			result.ProviderMetadata["requestId"] = value
			break
		}
	}
	return result
}

type anthropicAdapter struct {
	config AdapterConfig
}

func (a anthropicAdapter) Stream(ctx context.Context, request ChatRequest, onEvent func(StreamEvent)) (StreamResult, error) {
	payload := map[string]any{"model": a.config.Model, "stream": true, "max_tokens": 4096, "messages": anthropicMessages(request.Messages)}
	if request.System != "" {
		payload["system"] = request.System
	}
	if len(request.Tools) > 0 {
		tools := make([]map[string]any, 0, len(request.Tools))
		for _, tool := range request.Tools {
			schema := tool.InputSchema
			if schema == nil {
				schema = map[string]any{"type": "object", "properties": map[string]any{}}
			}
			tools = append(tools, map[string]any{"name": tool.Name, "description": tool.Description, "input_schema": schema})
		}
		payload["tools"] = tools
		choice := "auto"
		if request.ToolChoice == "required" {
			choice = "any"
		}
		payload["tool_choice"] = map[string]any{"type": choice}
	}
	body, _ := json.Marshal(payload)
	base := strings.TrimRight(a.config.Profile.BaseURL, "/")
	if base == "" {
		base = "https://api.anthropic.com"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return StreamResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	if a.config.APIKey != "" {
		req.Header.Set("x-api-key", a.config.APIKey)
	}
	resp, err := a.config.Client.Do(req)
	if err != nil {
		return StreamResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return StreamResult{}, normalizeHTTPError(a.config.Profile.ID, resp)
	}
	result, err := parseAnthropicStream(resp, onEvent)
	return completeProviderResult(result, resp, a.config.Profile.ID, a.config.Model), err
}

func anthropicMessages(messages []Message) []map[string]any {
	result := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		role := message.Role
		if role == "system" || role == "tool" {
			role = "user"
		}
		if role != "assistant" {
			role = "user"
		}
		if message.Role == "tool" {
			result = append(result, map[string]any{
				"role": role,
				"content": []map[string]any{{
					"type":        "tool_result",
					"tool_use_id": message.ToolCallID,
					"content":     message.Content,
				}},
			})
			continue
		}
		if len(message.ToolCalls) == 0 {
			result = append(result, map[string]any{"role": role, "content": message.Content})
			continue
		}
		content := make([]map[string]any, 0, len(message.ToolCalls)+1)
		if message.Content != "" {
			content = append(content, map[string]any{"type": "text", "text": message.Content})
		}
		for _, call := range message.ToolCalls {
			content = append(content, map[string]any{
				"type":  "tool_use",
				"id":    call.ID,
				"name":  call.Name,
				"input": decodeJSONMap(call.Arguments),
			})
		}
		result = append(result, map[string]any{"role": "assistant", "content": content})
	}
	return result
}

func parseAnthropicStream(resp *http.Response, onEvent func(StreamEvent)) (StreamResult, error) {
	if onEvent == nil {
		onEvent = func(StreamEvent) {}
	}
	var result StreamResult
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		data, ok := sseData(scanner.Text())
		if !ok || data == "[DONE]" {
			continue
		}
		var event struct {
			Type  string `json:"type"`
			Delta struct {
				Type       string `json:"type"`
				Text       string `json:"text"`
				StopReason string `json:"stop_reason"`
			} `json:"delta"`
			ContentBlock struct {
				Type  string         `json:"type"`
				ID    string         `json:"id"`
				Name  string         `json:"name"`
				Input map[string]any `json:"input"`
			} `json:"content_block"`
			Usage struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}
		if event.Delta.Text != "" {
			result.Text += event.Delta.Text
			onEvent(StreamEvent{Type: "text_delta", Delta: event.Delta.Text})
		}
		if event.Delta.StopReason != "" {
			result.FinishReason = normalizeFinishReason(event.Delta.StopReason)
			onEvent(StreamEvent{Type: "finish", FinishReason: result.FinishReason})
		}
		if event.ContentBlock.Type == "tool_use" {
			args, _ := json.Marshal(event.ContentBlock.Input)
			call := ToolCall{ID: event.ContentBlock.ID, Name: event.ContentBlock.Name, Arguments: string(args)}
			result.ToolCalls = append(result.ToolCalls, call)
			onEvent(StreamEvent{Type: "tool_call", ToolCall: &call})
		}
		if event.Usage.InputTokens != 0 || event.Usage.OutputTokens != 0 {
			result.Usage.PromptTokens += event.Usage.InputTokens
			result.Usage.CompletionTokens += event.Usage.OutputTokens
			result.Usage.TotalTokens = result.Usage.PromptTokens + result.Usage.CompletionTokens
			onEvent(StreamEvent{Type: "usage", Usage: &result.Usage})
		}
	}
	return result, scanner.Err()
}

type geminiAdapter struct {
	config AdapterConfig
}

func (a geminiAdapter) Stream(ctx context.Context, request ChatRequest, onEvent func(StreamEvent)) (StreamResult, error) {
	payload := map[string]any{
		"contents": geminiContents(request.Messages),
	}
	if strings.TrimSpace(request.System) != "" {
		payload["system_instruction"] = map[string]any{
			"parts": []map[string]string{{"text": request.System}},
		}
	}
	if len(request.Tools) > 0 {
		declarations := make([]map[string]any, 0, len(request.Tools))
		for _, tool := range request.Tools {
			schema := tool.InputSchema
			if schema == nil {
				schema = map[string]any{"type": "object", "properties": map[string]any{}}
			}
			declarations = append(declarations, map[string]any{
				"name":        tool.Name,
				"description": tool.Description,
				"parameters":  schema,
			})
		}
		payload["tools"] = []map[string]any{{"functionDeclarations": declarations}}
		payload["toolConfig"] = map[string]any{"functionCallingConfig": map[string]any{"mode": "AUTO"}}
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, geminiStreamURL(a.config.Profile.BaseURL, a.config.Model, a.config.APIKey), bytes.NewReader(body))
	if err != nil {
		return StreamResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.config.Client.Do(req)
	if err != nil {
		return StreamResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return StreamResult{}, normalizeHTTPError(a.config.Profile.ID, resp)
	}
	result, err := parseGeminiStream(resp, onEvent)
	return completeProviderResult(result, resp, a.config.Profile.ID, a.config.Model), err
}

func geminiContents(messages []Message) []map[string]any {
	contents := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		if message.Role == "tool" {
			contents = append(contents, map[string]any{
				"role": "user",
				"parts": []map[string]any{{
					"functionResponse": map[string]any{
						"name":     message.Name,
						"response": decodeJSONMap(message.Content),
					},
				}},
			})
			continue
		}
		role := message.Role
		if role == "" || role == "assistant" {
			role = "model"
		}
		if role == "system" {
			role = "user"
		}
		parts := make([]map[string]any, 0, len(message.ToolCalls)+1)
		if message.Content != "" {
			parts = append(parts, map[string]any{"text": message.Content})
		}
		for _, call := range message.ToolCalls {
			parts = append(parts, map[string]any{"functionCall": map[string]any{"name": call.Name, "args": decodeJSONMap(call.Arguments)}})
		}
		contents = append(contents, map[string]any{"role": role, "parts": parts})
	}
	return contents
}

func geminiStreamURL(base string, model string, apiKey string) string {
	base = strings.TrimRight(base, "/")
	if base == "" {
		base = "https://generativelanguage.googleapis.com"
	}
	if model == "" {
		model = "gemini-2.5-pro"
	}
	values := url.Values{"alt": []string{"sse"}}
	if apiKey != "" {
		values.Set("key", apiKey)
	}
	return base + "/v1beta/models/" + url.PathEscape(model) + ":streamGenerateContent?" + values.Encode()
}

func parseGeminiStream(resp *http.Response, onEvent func(StreamEvent)) (StreamResult, error) {
	if onEvent == nil {
		onEvent = func(StreamEvent) {}
	}
	var result StreamResult
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		data, ok := sseData(scanner.Text())
		if !ok {
			continue
		}
		var event struct {
			Candidates []struct {
				FinishReason string `json:"finishReason"`
				Content      struct {
					Parts []struct {
						Text         string `json:"text"`
						FunctionCall struct {
							Name string         `json:"name"`
							Args map[string]any `json:"args"`
						} `json:"functionCall"`
					} `json:"parts"`
				} `json:"content"`
			} `json:"candidates"`
			UsageMetadata struct {
				PromptTokenCount     int `json:"promptTokenCount"`
				CandidatesTokenCount int `json:"candidatesTokenCount"`
				TotalTokenCount      int `json:"totalTokenCount"`
			} `json:"usageMetadata"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}
		for _, candidate := range event.Candidates {
			if candidate.FinishReason != "" {
				result.FinishReason = normalizeFinishReason(candidate.FinishReason)
				onEvent(StreamEvent{Type: "finish", FinishReason: result.FinishReason})
			}
			for _, part := range candidate.Content.Parts {
				if part.Text != "" {
					result.Text += part.Text
					onEvent(StreamEvent{Type: "text_delta", Delta: part.Text})
				}
				if part.FunctionCall.Name != "" {
					args, _ := json.Marshal(part.FunctionCall.Args)
					call := ToolCall{ID: "gemini_" + part.FunctionCall.Name, Name: part.FunctionCall.Name, Arguments: string(args)}
					result.ToolCalls = append(result.ToolCalls, call)
					onEvent(StreamEvent{Type: "tool_call", ToolCall: &call})
				}
			}
		}
		if event.UsageMetadata.TotalTokenCount != 0 || event.UsageMetadata.PromptTokenCount != 0 || event.UsageMetadata.CandidatesTokenCount != 0 {
			result.Usage = Usage{
				PromptTokens:     event.UsageMetadata.PromptTokenCount,
				CompletionTokens: event.UsageMetadata.CandidatesTokenCount,
				TotalTokens:      event.UsageMetadata.TotalTokenCount,
			}
			onEvent(StreamEvent{Type: "usage", Usage: &result.Usage})
		}
	}
	return result, scanner.Err()
}

type bedrockAdapter struct {
	config AdapterConfig
}

func (a bedrockAdapter) Stream(ctx context.Context, request ChatRequest, onEvent func(StreamEvent)) (StreamResult, error) {
	payload := map[string]any{
		"messages":        bedrockMessages(request.Messages),
		"inferenceConfig": map[string]any{"maxTokens": 4096},
	}
	if strings.TrimSpace(request.System) != "" {
		payload["system"] = []map[string]string{{"text": request.System}}
	}
	if len(request.Tools) > 0 {
		toolSpecs := make([]map[string]any, 0, len(request.Tools))
		for _, tool := range request.Tools {
			schema := tool.InputSchema
			if schema == nil {
				schema = map[string]any{"type": "object", "properties": map[string]any{}}
			}
			toolSpecs = append(toolSpecs, map[string]any{
				"toolSpec": map[string]any{
					"name":        tool.Name,
					"description": tool.Description,
					"inputSchema": map[string]any{"json": schema},
				},
			})
		}
		payload["toolConfig"] = map[string]any{"tools": toolSpecs, "toolChoice": map[string]any{"auto": map[string]any{}}}
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, bedrockConverseURL(a.config.Profile.BaseURL, a.config.Model), bytes.NewReader(body))
	if err != nil {
		return StreamResult{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	if err := signBedrockRequest(req, body, a.config.APIKey); err != nil {
		return StreamResult{}, err
	}
	resp, err := a.config.Client.Do(req)
	if err != nil {
		return StreamResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return StreamResult{}, normalizeHTTPError(a.config.Profile.ID, resp)
	}
	result, err := parseBedrockConverse(resp, onEvent)
	return completeProviderResult(result, resp, a.config.Profile.ID, a.config.Model), err
}

func bedrockMessages(messages []Message) []map[string]any {
	result := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		if message.Role == "tool" {
			result = append(result, map[string]any{
				"role": "user",
				"content": []map[string]any{{
					"toolResult": map[string]any{
						"toolUseId": message.ToolCallID,
						"content":   []map[string]any{{"json": decodeJSONMap(message.Content)}},
					},
				}},
			})
			continue
		}
		role := message.Role
		if role == "" || role == "system" {
			role = "user"
		}
		if role != "assistant" {
			role = "user"
		}
		content := make([]map[string]any, 0, len(message.ToolCalls)+1)
		if message.Content != "" {
			content = append(content, map[string]any{"text": message.Content})
		}
		for _, call := range message.ToolCalls {
			content = append(content, map[string]any{
				"toolUse": map[string]any{
					"toolUseId": call.ID,
					"name":      call.Name,
					"input":     decodeJSONMap(call.Arguments),
				},
			})
		}
		result = append(result, map[string]any{"role": role, "content": content})
	}
	return result
}

func bedrockConverseURL(base string, model string) string {
	base = strings.TrimRight(base, "/")
	if base == "" {
		base = "https://bedrock-runtime.us-east-1.amazonaws.com"
	}
	if model == "" {
		model = "anthropic.claude-3-sonnet-20240229-v1:0"
	}
	return base + "/model/" + url.PathEscape(model) + "/converse"
}

func parseBedrockConverse(resp *http.Response, onEvent func(StreamEvent)) (StreamResult, error) {
	if onEvent == nil {
		onEvent = func(StreamEvent) {}
	}
	var payload struct {
		StopReason string `json:"stopReason"`
		Output     struct {
			Message struct {
				Content []struct {
					Text    string `json:"text"`
					ToolUse struct {
						ToolUseID string         `json:"toolUseId"`
						Name      string         `json:"name"`
						Input     map[string]any `json:"input"`
					} `json:"toolUse"`
				} `json:"content"`
			} `json:"message"`
		} `json:"output"`
		Usage struct {
			InputTokens  int `json:"inputTokens"`
			OutputTokens int `json:"outputTokens"`
			TotalTokens  int `json:"totalTokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return StreamResult{}, err
	}
	result := StreamResult{FinishReason: normalizeFinishReason(payload.StopReason)}
	for _, block := range payload.Output.Message.Content {
		if block.Text != "" {
			result.Text += block.Text
			onEvent(StreamEvent{Type: "text_delta", Delta: block.Text})
		}
		if block.ToolUse.Name != "" {
			args, _ := json.Marshal(block.ToolUse.Input)
			call := ToolCall{ID: block.ToolUse.ToolUseID, Name: block.ToolUse.Name, Arguments: string(args)}
			result.ToolCalls = append(result.ToolCalls, call)
			onEvent(StreamEvent{Type: "tool_call", ToolCall: &call})
		}
	}
	if payload.Usage.InputTokens != 0 || payload.Usage.OutputTokens != 0 || payload.Usage.TotalTokens != 0 {
		result.Usage = Usage{
			PromptTokens:     payload.Usage.InputTokens,
			CompletionTokens: payload.Usage.OutputTokens,
			TotalTokens:      payload.Usage.TotalTokens,
		}
		onEvent(StreamEvent{Type: "usage", Usage: &result.Usage})
	}
	if result.FinishReason != "" {
		onEvent(StreamEvent{Type: "finish", FinishReason: result.FinishReason})
	}
	return result, nil
}

func normalizeFinishReason(reason string) string {
	reason = strings.ToLower(strings.TrimSpace(reason))
	switch reason {
	case "end_turn", "stop_sequence", "stop":
		return "stop"
	case "max_tokens", "max_token", "length":
		return "length"
	case "tool_use", "tool_calls", "function_call":
		return "tool_use"
	default:
		return reason
	}
}

type awsCredentials struct {
	AccessKey    string
	SecretKey    string
	Region       string
	SessionToken string
}

func parseAWSCredentials(raw string, endpoint string) (awsCredentials, error) {
	parts := strings.Split(raw, ":")
	if len(parts) < 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return awsCredentials{}, errors.New("bedrock API key must be accessKey:secretKey[:region[:sessionToken]]")
	}
	credentials := awsCredentials{
		AccessKey: strings.TrimSpace(parts[0]),
		SecretKey: strings.TrimSpace(parts[1]),
		Region:    "us-east-1",
	}
	if region := bedrockRegionFromEndpoint(endpoint); region != "" {
		credentials.Region = region
	}
	if len(parts) >= 3 && strings.TrimSpace(parts[2]) != "" {
		credentials.Region = strings.TrimSpace(parts[2])
	}
	if len(parts) >= 4 {
		credentials.SessionToken = strings.TrimSpace(strings.Join(parts[3:], ":"))
	}
	return credentials, nil
}

func bedrockRegionFromEndpoint(endpoint string) string {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return ""
	}
	host := parsed.Hostname()
	if !strings.HasPrefix(host, "bedrock-runtime.") {
		return ""
	}
	rest := strings.TrimPrefix(host, "bedrock-runtime.")
	region, _, found := strings.Cut(rest, ".")
	if !found {
		return ""
	}
	return region
}

func signBedrockRequest(req *http.Request, body []byte, apiKey string) error {
	credentials, err := parseAWSCredentials(apiKey, req.URL.String())
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")
	payloadHash := sha256Hex(body)
	req.Header.Set("Host", req.URL.Host)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)
	req.Header.Set("X-Amz-Date", amzDate)
	if credentials.SessionToken != "" {
		req.Header.Set("X-Amz-Security-Token", credentials.SessionToken)
	}

	signedHeaders, canonicalHeaders := canonicalAWSHeaders(req.Header)
	canonicalRequest := strings.Join([]string{
		req.Method,
		req.URL.EscapedPath(),
		req.URL.RawQuery,
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")
	scope := dateStamp + "/" + credentials.Region + "/bedrock/aws4_request"
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")
	signingKey := awsSigningKey(credentials.SecretKey, dateStamp, credentials.Region, "bedrock")
	signature := hex.EncodeToString(hmacSHA256(signingKey, stringToSign))
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+credentials.AccessKey+"/"+scope+", SignedHeaders="+signedHeaders+", Signature="+signature)
	return nil
}

func canonicalAWSHeaders(headers http.Header) (string, string) {
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, strings.ToLower(name))
	}
	sort.Strings(names)
	var builder strings.Builder
	for _, name := range names {
		builder.WriteString(name)
		builder.WriteString(":")
		builder.WriteString(strings.Join(headers.Values(name), ","))
		builder.WriteString("\n")
	}
	return strings.Join(names, ";"), builder.String()
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(data))
	return mac.Sum(nil)
}

func awsSigningKey(secret string, date string, region string, service string) []byte {
	dateKey := hmacSHA256([]byte("AWS4"+secret), date)
	regionKey := hmacSHA256(dateKey, region)
	serviceKey := hmacSHA256(regionKey, service)
	return hmacSHA256(serviceKey, "aws4_request")
}

func decodeJSONMap(raw string) map[string]any {
	if strings.TrimSpace(raw) == "" {
		return map[string]any{}
	}
	var value map[string]any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return map[string]any{"value": raw}
	}
	return value
}

func sseData(line string) (string, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "data:") {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(line, "data:")), true
}

func normalizeHTTPError(provider string, resp *http.Response) error {
	var body struct {
		Error any `json:"error"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	message := ""
	code := ""
	errorType := ""
	switch errValue := body.Error.(type) {
	case string:
		message = errValue
	case map[string]any:
		message = fmt.Sprint(errValue["message"])
		code = strings.TrimSpace(fmt.Sprint(errValue["code"]))
		errorType = strings.TrimSpace(fmt.Sprint(errValue["type"]))
	}
	if message == "<nil>" {
		message = ""
	}
	if message == "" {
		message = resp.Status
	}
	requestID := ""
	for _, header := range []string{"x-request-id", "request-id", "x-amzn-requestid"} {
		if value := strings.TrimSpace(resp.Header.Get(header)); value != "" {
			requestID = value
			break
		}
	}
	details := map[string]string{}
	if errorType != "" && errorType != "<nil>" {
		details["type"] = errorType
	}
	if retryAfter := strings.TrimSpace(resp.Header.Get("retry-after")); retryAfter != "" {
		details["retryAfter"] = retryAfter
	}
	if code == "<nil>" {
		code = ""
	}
	return ProviderError{
		Provider:   provider,
		Status:     resp.Status,
		HTTPStatus: resp.StatusCode,
		Code:       code,
		Message:    message,
		Retryable:  resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode == http.StatusConflict || resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500,
		RequestID:  requestID,
		Details:    details,
	}
}

var _ Adapter = openAICompatibleAdapter{}
var _ Adapter = anthropicAdapter{}
var _ Adapter = geminiAdapter{}
var _ Adapter = bedrockAdapter{}
var _ error = ProviderError{}

func IsProviderError(err error) bool {
	var providerErr ProviderError
	return errors.As(err, &providerErr)
}
