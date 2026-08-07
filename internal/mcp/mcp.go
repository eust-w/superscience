package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"superscience/internal/buildinfo"
	"superscience/internal/store"
)

const maxResultBytes = 2_000_000

type Tool struct {
	Name         string         `json:"name"`
	Title        string         `json:"title,omitempty"`
	Description  string         `json:"description"`
	InputSchema  map[string]any `json:"inputSchema"`
	OutputSchema map[string]any `json:"outputSchema,omitempty"`
}

type Resource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
	MimeType    string `json:"mimeType"`
	Size        int64  `json:"size"`
}

type ResourceContent struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType"`
	Text     string `json:"text"`
	Blob     string `json:"blob"`
}

type ResourceReadResult struct {
	ServerID     string            `json:"serverId"`
	URI          string            `json:"uri"`
	Status       string            `json:"status"`
	PermissionID string            `json:"permissionId"`
	Contents     []ResourceContent `json:"contents"`
	Text         string            `json:"text"`
}

type PromptArgument struct {
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

type Prompt struct {
	Name        string           `json:"name"`
	Title       string           `json:"title"`
	Description string           `json:"description"`
	Arguments   []PromptArgument `json:"arguments"`
}

type PromptContent struct {
	Type     string           `json:"type"`
	Text     string           `json:"text"`
	Data     string           `json:"data"`
	MimeType string           `json:"mimeType"`
	Resource *ResourceContent `json:"resource"`
}

type PromptMessage struct {
	Role    string        `json:"role"`
	Content PromptContent `json:"content"`
}

type PromptGetResult struct {
	ServerID     string          `json:"serverId"`
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	Status       string          `json:"status"`
	PermissionID string          `json:"permissionId"`
	Messages     []PromptMessage `json:"messages"`
	Text         string          `json:"text"`
}

type Discovery struct {
	ProtocolVersion string     `json:"protocolVersion,omitempty"`
	ServerName      string     `json:"serverName,omitempty"`
	ServerVersion   string     `json:"serverVersion,omitempty"`
	Instructions    string     `json:"instructions,omitempty"`
	Tools           []Tool     `json:"tools"`
	Resources       []Resource `json:"resources"`
	Prompts         []Prompt   `json:"prompts"`
}

type ExecutionResult struct {
	ID               string          `json:"id"`
	ServerID         string          `json:"serverId"`
	Tool             string          `json:"tool"`
	Status           string          `json:"status"`
	PermissionID     string          `json:"permissionId"`
	Output           string          `json:"output"`
	Input            map[string]any  `json:"input"`
	Content          []PromptContent `json:"content,omitempty"`
	StructuredOutput any             `json:"structuredOutput,omitempty"`
	IsError          bool            `json:"isError"`
}

type PermissionGate interface {
	Ask(sessionID, typ, pattern, message string) string
	IsAllowed(sessionID, typ, pattern string) bool
}

type managedSession struct {
	fingerprint string
	session     *mcpsdk.ClientSession
	cancel      context.CancelFunc
}

type ClientManager struct {
	mu       sync.Mutex
	sessions map[string]*managedSession
	locks    map[string]*sync.Mutex
}

func NewClientManager() *ClientManager {
	return &ClientManager{
		sessions: make(map[string]*managedSession),
		locks:    make(map[string]*sync.Mutex),
	}
}

var defaultManager = NewClientManager()

func Discover(ctx context.Context, server store.MCPServer) ([]Tool, error) {
	discovery, err := defaultManager.DiscoverCapabilities(ctx, server)
	return discovery.Tools, err
}

func DiscoverCapabilities(ctx context.Context, server store.MCPServer) (Discovery, error) {
	return defaultManager.DiscoverCapabilities(ctx, server)
}

func (m *ClientManager) DiscoverCapabilities(ctx context.Context, server store.MCPServer) (Discovery, error) {
	result, err := withSession(ctx, m, server, func(ctx context.Context, session *mcpsdk.ClientSession) (Discovery, error) {
		discovery := Discovery{Tools: []Tool{}, Resources: []Resource{}, Prompts: []Prompt{}}
		initialized := session.InitializeResult()
		if initialized != nil {
			discovery.ProtocolVersion = initialized.ProtocolVersion
			discovery.Instructions = initialized.Instructions
			if initialized.ServerInfo != nil {
				discovery.ServerName = initialized.ServerInfo.Name
				discovery.ServerVersion = initialized.ServerInfo.Version
			}
		}
		capabilities := initializedCapabilities(initialized)
		if capabilities.Tools != nil {
			tools, err := listAllTools(ctx, session)
			if err != nil {
				return Discovery{}, err
			}
			discovery.Tools = tools
		}
		if capabilities.Resources != nil {
			resources, err := listAllResources(ctx, session)
			if err != nil {
				return Discovery{}, err
			}
			discovery.Resources = resources
		}
		if capabilities.Prompts != nil {
			prompts, err := listAllPrompts(ctx, session)
			if err != nil {
				return Discovery{}, err
			}
			discovery.Prompts = prompts
		}
		return discovery, nil
	})
	if err != nil {
		return Discovery{}, fmt.Errorf("discover MCP server %q: %w", server.ID, err)
	}
	return result, nil
}

func Execute(ctx context.Context, server store.MCPServer, toolName string, input map[string]any, sessionID string, gate PermissionGate) (ExecutionResult, error) {
	if err := validateServer(server); err != nil {
		return ExecutionResult{}, err
	}
	toolName = strings.TrimSpace(toolName)
	if toolName == "" {
		return ExecutionResult{}, errors.New("MCP tool name is required")
	}
	if input == nil {
		input = map[string]any{}
	}
	pattern := server.ID + ":" + toolName
	result := ExecutionResult{
		ID:       fmt.Sprintf("mcp-%d", time.Now().UnixNano()),
		ServerID: server.ID,
		Tool:     toolName,
		Input:    input,
	}
	if gate != nil && !gate.IsAllowed(sessionID, "mcp", pattern) {
		result.PermissionID = gate.Ask(sessionID, "mcp", pattern, "External MCP tool execution requires approval.")
		result.Status = "pending_permission"
		return result, nil
	}

	response, err := withSession(ctx, defaultManager, server, func(ctx context.Context, session *mcpsdk.ClientSession) (*mcpsdk.CallToolResult, error) {
		return session.CallTool(ctx, &mcpsdk.CallToolParams{Name: toolName, Arguments: input})
	})
	if err != nil {
		result.Status = "error"
		result.Output = err.Error()
		return result, fmt.Errorf("call MCP tool %q on %q: %w", toolName, server.ID, err)
	}
	result.Content = convertContents(response.Content)
	result.StructuredOutput = response.StructuredContent
	result.IsError = response.IsError
	result.Output, err = toolResultText(result.Content, response.StructuredContent)
	if err != nil {
		return ExecutionResult{}, err
	}
	if response.IsError {
		result.Status = "error"
	} else {
		result.Status = "ok"
	}
	return result, nil
}

func ReadResource(ctx context.Context, server store.MCPServer, uri string, sessionID string, gate PermissionGate) (ResourceReadResult, error) {
	if err := validateServer(server); err != nil {
		return ResourceReadResult{}, err
	}
	uri = strings.TrimSpace(uri)
	if uri == "" {
		return ResourceReadResult{}, errors.New("resource uri is required")
	}
	pattern := server.ID + ":resources/read:" + uri
	result := ResourceReadResult{ServerID: server.ID, URI: uri}
	if gate != nil && !gate.IsAllowed(sessionID, "mcp", pattern) {
		result.PermissionID = gate.Ask(sessionID, "mcp", pattern, "External MCP resource access requires approval.")
		result.Status = "pending_permission"
		return result, nil
	}
	response, err := withSession(ctx, defaultManager, server, func(ctx context.Context, session *mcpsdk.ClientSession) (*mcpsdk.ReadResourceResult, error) {
		return session.ReadResource(ctx, &mcpsdk.ReadResourceParams{URI: uri})
	})
	if err != nil {
		result.Status = "error"
		result.Text = err.Error()
		return result, fmt.Errorf("read MCP resource %q from %q: %w", uri, server.ID, err)
	}
	result.Contents = make([]ResourceContent, 0, len(response.Contents))
	for _, content := range response.Contents {
		if content == nil {
			continue
		}
		result.Contents = append(result.Contents, convertResourceContent(content))
	}
	result.Status = "ok"
	result.Text = resourceContentsText(result.Contents)
	return result, nil
}

func GetPrompt(ctx context.Context, server store.MCPServer, name string, arguments map[string]any, sessionID string, gate PermissionGate) (PromptGetResult, error) {
	if err := validateServer(server); err != nil {
		return PromptGetResult{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return PromptGetResult{}, errors.New("prompt name is required")
	}
	stringArgs, err := promptArguments(arguments)
	if err != nil {
		return PromptGetResult{}, err
	}
	pattern := server.ID + ":prompts/get:" + name
	result := PromptGetResult{ServerID: server.ID, Name: name}
	if gate != nil && !gate.IsAllowed(sessionID, "mcp", pattern) {
		result.PermissionID = gate.Ask(sessionID, "mcp", pattern, "External MCP prompt access requires approval.")
		result.Status = "pending_permission"
		return result, nil
	}
	response, err := withSession(ctx, defaultManager, server, func(ctx context.Context, session *mcpsdk.ClientSession) (*mcpsdk.GetPromptResult, error) {
		return session.GetPrompt(ctx, &mcpsdk.GetPromptParams{Name: name, Arguments: stringArgs})
	})
	if err != nil {
		result.Status = "error"
		result.Text = err.Error()
		return result, fmt.Errorf("get MCP prompt %q from %q: %w", name, server.ID, err)
	}
	result.Description = response.Description
	result.Messages = make([]PromptMessage, 0, len(response.Messages))
	for _, message := range response.Messages {
		if message == nil {
			continue
		}
		result.Messages = append(result.Messages, PromptMessage{
			Role:    string(message.Role),
			Content: convertContent(message.Content),
		})
	}
	result.Status = "ok"
	result.Text = promptMessagesText(result.Messages)
	return result, nil
}

func CloseServer(serverID string) error {
	return defaultManager.CloseServer(serverID)
}

func CloseAll() error {
	return defaultManager.CloseAll()
}

func (m *ClientManager) CloseServer(serverID string) error {
	serverID = strings.TrimSpace(serverID)
	if serverID == "" {
		return nil
	}
	lock := m.serverLock(serverID)
	lock.Lock()
	defer lock.Unlock()
	m.mu.Lock()
	managed := m.sessions[serverID]
	delete(m.sessions, serverID)
	m.mu.Unlock()
	return closeManaged(managed)
}

func (m *ClientManager) CloseAll() error {
	m.mu.Lock()
	sessions := make([]*managedSession, 0, len(m.sessions))
	for _, managed := range m.sessions {
		sessions = append(sessions, managed)
	}
	m.sessions = make(map[string]*managedSession)
	m.mu.Unlock()
	var errs []error
	for _, managed := range sessions {
		if err := closeManaged(managed); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (m *ClientManager) session(ctx context.Context, server store.MCPServer) (*mcpsdk.ClientSession, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateServer(server); err != nil {
		return nil, err
	}
	fingerprint, err := serverFingerprint(server)
	if err != nil {
		return nil, err
	}
	lock := m.serverLock(server.ID)
	lock.Lock()
	defer lock.Unlock()

	m.mu.Lock()
	managed := m.sessions[server.ID]
	m.mu.Unlock()
	if managed != nil && managed.fingerprint == fingerprint {
		return managed.session, nil
	}
	if managed != nil {
		m.mu.Lock()
		delete(m.sessions, server.ID)
		m.mu.Unlock()
		_ = closeManaged(managed)
	}

	managed, err = openSession(ctx, server, fingerprint)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.sessions[server.ID] = managed
	m.mu.Unlock()
	return managed.session, nil
}

func (m *ClientManager) serverLock(serverID string) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	lock := m.locks[serverID]
	if lock == nil {
		lock = &sync.Mutex{}
		m.locks[serverID] = lock
	}
	return lock
}

func (m *ClientManager) invalidate(serverID string, session *mcpsdk.ClientSession) {
	lock := m.serverLock(serverID)
	lock.Lock()
	defer lock.Unlock()
	m.mu.Lock()
	managed := m.sessions[serverID]
	if managed != nil && managed.session == session {
		delete(m.sessions, serverID)
	} else {
		managed = nil
	}
	m.mu.Unlock()
	_ = closeManaged(managed)
}

func openSession(ctx context.Context, server store.MCPServer, fingerprint string) (*managedSession, error) {
	sessionContext, cancel := context.WithCancel(context.Background())
	transport, err := clientTransport(sessionContext, server)
	if err != nil {
		cancel()
		return nil, err
	}
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "superscience", Version: buildinfo.DisplayVersion()}, nil)
	type connectResult struct {
		session *mcpsdk.ClientSession
		err     error
	}
	connected := make(chan connectResult, 1)
	go func() {
		session, connectErr := client.Connect(sessionContext, transport, nil)
		connected <- connectResult{session: session, err: connectErr}
	}()
	select {
	case <-ctx.Done():
		cancel()
		return nil, ctx.Err()
	case result := <-connected:
		if result.err != nil {
			cancel()
			return nil, result.err
		}
		return &managedSession{fingerprint: fingerprint, session: result.session, cancel: cancel}, nil
	}
}

func clientTransport(ctx context.Context, server store.MCPServer) (mcpsdk.Transport, error) {
	switch strings.ToLower(strings.TrimSpace(server.Transport)) {
	case "stdio":
		command := exec.CommandContext(ctx, server.Command, server.Args...)
		if len(server.Env) > 0 {
			command.Env = append(os.Environ(), server.Env...)
		}
		return &mcpsdk.CommandTransport{Command: command}, nil
	case "http", "streamable-http":
		return &mcpsdk.StreamableClientTransport{
			Endpoint:   server.URL,
			HTTPClient: mcpHTTPClient(server.BearerToken, server.Headers),
			MaxRetries: 2,
		}, nil
	case "sse":
		return &mcpsdk.SSEClientTransport{Endpoint: server.URL, HTTPClient: mcpHTTPClient(server.BearerToken, server.Headers)}, nil
	default:
		return nil, fmt.Errorf("unsupported MCP transport %q", server.Transport)
	}
}

func mcpHTTPClient(bearerToken string, headers map[string]string) *http.Client {
	base := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          20,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 20 * time.Second,
	}
	var transport http.RoundTripper = base
	if bearerToken != "" || len(headers) > 0 {
		transport = headerTransport{base: base, token: bearerToken, headers: headers}
	}
	return &http.Client{Transport: transport}
}

type headerTransport struct {
	base    http.RoundTripper
	token   string
	headers map[string]string
}

func (t headerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.Header = request.Header.Clone()
	for name, value := range t.headers {
		if textproto.CanonicalMIMEHeaderKey(name) == "" || strings.ContainsAny(value, "\r\n") {
			return nil, fmt.Errorf("invalid MCP HTTP header %q", name)
		}
		clone.Header.Set(name, value)
	}
	if t.token != "" {
		clone.Header.Set("Authorization", "Bearer "+t.token)
	}
	return t.base.RoundTrip(clone)
}

func withSession[T any](ctx context.Context, manager *ClientManager, server store.MCPServer, call func(context.Context, *mcpsdk.ClientSession) (T, error)) (T, error) {
	var zero T
	for attempt := 0; attempt < 2; attempt++ {
		session, err := manager.session(ctx, server)
		if err != nil {
			return zero, err
		}
		result, err := call(ctx, session)
		if err == nil {
			return result, nil
		}
		if !errors.Is(err, mcpsdk.ErrConnectionClosed) || ctx.Err() != nil {
			return zero, err
		}
		manager.invalidate(server.ID, session)
	}
	return zero, mcpsdk.ErrConnectionClosed
}

func validateServer(server store.MCPServer) error {
	if strings.TrimSpace(server.ID) == "" {
		return errors.New("MCP server id is required")
	}
	if !server.Enabled {
		return fmt.Errorf("MCP server %q is disabled", server.ID)
	}
	switch strings.ToLower(strings.TrimSpace(server.Transport)) {
	case "stdio":
		if strings.TrimSpace(server.Command) == "" {
			return fmt.Errorf("MCP server %q requires a command", server.ID)
		}
	case "http", "streamable-http", "sse":
		endpoint := strings.TrimSpace(server.URL)
		if endpoint == "" {
			return fmt.Errorf("MCP server %q requires a URL", server.ID)
		}
		parsed, err := url.Parse(endpoint)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return fmt.Errorf("MCP server %q has an invalid URL", server.ID)
		}
	default:
		return fmt.Errorf("unsupported MCP transport %q", server.Transport)
	}
	return nil
}

func serverFingerprint(server store.MCPServer) (string, error) {
	tokenDigest := secretDigest(server.BearerToken)
	envJSON, err := json.Marshal(server.Env)
	if err != nil {
		return "", err
	}
	headerJSON, err := json.Marshal(server.Headers)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(struct {
		Transport  string   `json:"transport"`
		Command    string   `json:"command"`
		Args       []string `json:"args"`
		URL        string   `json:"url"`
		TokenHash  string   `json:"tokenHash"`
		EnvHash    string   `json:"envHash"`
		HeaderHash string   `json:"headerHash"`
	}{server.Transport, server.Command, server.Args, server.URL, tokenDigest, secretDigest(string(envJSON)), secretDigest(string(headerJSON))})
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func secretDigest(value string) string {
	if value == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func closeManaged(managed *managedSession) error {
	if managed == nil {
		return nil
	}
	managed.cancel()
	return managed.session.Close()
}

func initializedCapabilities(result *mcpsdk.InitializeResult) *mcpsdk.ServerCapabilities {
	if result == nil || result.Capabilities == nil {
		return &mcpsdk.ServerCapabilities{}
	}
	return result.Capabilities
}

func listAllTools(ctx context.Context, session *mcpsdk.ClientSession) ([]Tool, error) {
	tools := []Tool{}
	cursor := ""
	for {
		result, err := session.ListTools(ctx, &mcpsdk.ListToolsParams{Cursor: cursor})
		if err != nil {
			return nil, err
		}
		for _, item := range result.Tools {
			if item == nil {
				continue
			}
			tools = append(tools, Tool{
				Name:         item.Name,
				Title:        item.Title,
				Description:  item.Description,
				InputSchema:  schemaMap(item.InputSchema),
				OutputSchema: schemaMap(item.OutputSchema),
			})
		}
		if result.NextCursor == "" {
			return tools, nil
		}
		cursor = result.NextCursor
	}
}

func listAllResources(ctx context.Context, session *mcpsdk.ClientSession) ([]Resource, error) {
	resources := []Resource{}
	cursor := ""
	for {
		result, err := session.ListResources(ctx, &mcpsdk.ListResourcesParams{Cursor: cursor})
		if err != nil {
			return nil, err
		}
		for _, item := range result.Resources {
			if item == nil {
				continue
			}
			resources = append(resources, Resource{
				URI:         item.URI,
				Name:        item.Name,
				Title:       item.Title,
				Description: item.Description,
				MimeType:    item.MIMEType,
				Size:        item.Size,
			})
		}
		if result.NextCursor == "" {
			return resources, nil
		}
		cursor = result.NextCursor
	}
}

func listAllPrompts(ctx context.Context, session *mcpsdk.ClientSession) ([]Prompt, error) {
	prompts := []Prompt{}
	cursor := ""
	for {
		result, err := session.ListPrompts(ctx, &mcpsdk.ListPromptsParams{Cursor: cursor})
		if err != nil {
			return nil, err
		}
		for _, item := range result.Prompts {
			if item == nil {
				continue
			}
			prompt := Prompt{Name: item.Name, Title: item.Title, Description: item.Description, Arguments: []PromptArgument{}}
			for _, argument := range item.Arguments {
				if argument == nil {
					continue
				}
				prompt.Arguments = append(prompt.Arguments, PromptArgument{
					Name: argument.Name, Title: argument.Title, Description: argument.Description, Required: argument.Required,
				})
			}
			prompts = append(prompts, prompt)
		}
		if result.NextCursor == "" {
			return prompts, nil
		}
		cursor = result.NextCursor
	}
}

func schemaMap(schema any) map[string]any {
	if schema == nil {
		return nil
	}
	if typed, ok := schema.(map[string]any); ok {
		return typed
	}
	data, err := json.Marshal(schema)
	if err != nil {
		return nil
	}
	var result map[string]any
	if json.Unmarshal(data, &result) != nil {
		return nil
	}
	return result
}

func convertResourceContent(content *mcpsdk.ResourceContents) ResourceContent {
	converted := ResourceContent{URI: content.URI, MimeType: content.MIMEType, Text: content.Text}
	if len(content.Blob) > 0 {
		converted.Blob = base64.StdEncoding.EncodeToString(content.Blob)
	}
	return converted
}

func convertContents(contents []mcpsdk.Content) []PromptContent {
	converted := make([]PromptContent, 0, len(contents))
	for _, content := range contents {
		converted = append(converted, convertContent(content))
	}
	return converted
}

func convertContent(content mcpsdk.Content) PromptContent {
	switch item := content.(type) {
	case *mcpsdk.TextContent:
		return PromptContent{Type: "text", Text: item.Text}
	case *mcpsdk.ImageContent:
		return PromptContent{Type: "image", Data: base64.StdEncoding.EncodeToString(item.Data), MimeType: item.MIMEType}
	case *mcpsdk.AudioContent:
		return PromptContent{Type: "audio", Data: base64.StdEncoding.EncodeToString(item.Data), MimeType: item.MIMEType}
	case *mcpsdk.EmbeddedResource:
		if item.Resource == nil {
			return PromptContent{Type: "resource"}
		}
		resource := convertResourceContent(item.Resource)
		return PromptContent{Type: "resource", Resource: &resource}
	case *mcpsdk.ResourceLink:
		resource := ResourceContent{URI: item.URI, MimeType: item.MIMEType}
		return PromptContent{Type: "resource_link", Text: item.Title, Resource: &resource}
	default:
		data, _ := json.Marshal(content)
		return PromptContent{Type: "unknown", Data: string(data)}
	}
}

func toolResultText(contents []PromptContent, structured any) (string, error) {
	parts := make([]string, 0, len(contents))
	for _, content := range contents {
		if content.Text != "" {
			parts = append(parts, content.Text)
		}
		if content.Resource != nil && content.Resource.Text != "" {
			parts = append(parts, content.Resource.Text)
		}
	}
	output := strings.Join(parts, "\n")
	if output == "" && structured != nil {
		data, err := json.Marshal(structured)
		if err != nil {
			return "", err
		}
		output = string(data)
	}
	if output == "" && len(contents) > 0 {
		data, err := json.Marshal(contents)
		if err != nil {
			return "", err
		}
		output = string(data)
	}
	if len(output) > maxResultBytes {
		return "", fmt.Errorf("MCP result exceeds %d bytes", maxResultBytes)
	}
	return output, nil
}

func resourceContentsText(contents []ResourceContent) string {
	parts := make([]string, 0, len(contents))
	for _, content := range contents {
		if content.Text != "" {
			parts = append(parts, content.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func promptMessagesText(messages []PromptMessage) string {
	parts := make([]string, 0, len(messages))
	for _, message := range messages {
		if message.Content.Text != "" {
			parts = append(parts, message.Content.Text)
		}
		if message.Content.Resource != nil && message.Content.Resource.Text != "" {
			parts = append(parts, message.Content.Resource.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func promptArguments(arguments map[string]any) (map[string]string, error) {
	converted := make(map[string]string, len(arguments))
	for key, value := range arguments {
		text, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("MCP prompt argument %q must be a string", key)
		}
		converted[key] = text
	}
	return converted, nil
}
