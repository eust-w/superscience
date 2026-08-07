package plugins

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/jsonschema-go/jsonschema"

	"superscience/internal/store"
)

const (
	pluginProtocolVersion = "superscience-plugin/1"
	maxPluginMessageBytes = 2_000_000
	maxPluginStderrBytes  = 64_000
)

var ErrProcessClosed = errors.New("plugin process closed")

type Tool struct {
	Name         string         `json:"name"`
	Description  string         `json:"description"`
	InputSchema  map[string]any `json:"inputSchema"`
	OutputSchema map[string]any `json:"outputSchema,omitempty"`
}

type ProviderRegistration struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}

type HookRegistration struct {
	Name   string   `json:"name"`
	Events []string `json:"events"`
}

type Discovery struct {
	ProtocolVersion string                 `json:"protocolVersion"`
	PluginName      string                 `json:"pluginName"`
	PluginVersion   string                 `json:"pluginVersion"`
	Capabilities    map[string]any         `json:"capabilities"`
	Tools           []Tool                 `json:"tools"`
	Providers       []ProviderRegistration `json:"providers"`
	Hooks           []HookRegistration     `json:"hooks"`
}

type ExecuteResult struct {
	PluginID         string         `json:"pluginId"`
	Tool             string         `json:"tool"`
	Status           string         `json:"status"`
	PermissionID     string         `json:"permissionId"`
	Output           string         `json:"output"`
	Input            map[string]any `json:"input"`
	StructuredOutput any            `json:"structuredOutput,omitempty"`
	IsError          bool           `json:"isError"`
}

type PermissionGate interface {
	Ask(sessionID, typ, pattern, message string) string
	IsAllowed(sessionID, typ, pattern string) bool
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *rpcError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("plugin JSON-RPC error %d: %s", e.Code, e.Message)
}

type callResult struct {
	result json.RawMessage
	err    error
}

type processSession struct {
	fingerprint string
	cmd         *exec.Cmd
	stdin       io.WriteCloser
	cancel      context.CancelFunc
	discovery   Discovery

	nextID atomic.Uint64

	mu        sync.Mutex
	pending   map[string]chan callResult
	closedErr error
	writeMu   sync.Mutex
	stderrMu  sync.Mutex
	stderr    []byte
	done      chan struct{}
	closeOnce sync.Once
}

type Manager struct {
	mu       sync.Mutex
	sessions map[string]*processSession
	locks    map[string]*sync.Mutex
}

func NewManager() *Manager {
	return &Manager{sessions: map[string]*processSession{}, locks: map[string]*sync.Mutex{}}
}

var defaultManager = NewManager()

func ManifestTools(plugin store.PluginRecord) ([]Tool, error) {
	var manifest struct {
		Tools []json.RawMessage `json:"tools"`
	}
	if strings.TrimSpace(plugin.Manifest) == "" {
		return []Tool{}, nil
	}
	if err := json.Unmarshal([]byte(plugin.Manifest), &manifest); err != nil {
		return nil, err
	}
	tools := make([]Tool, 0, len(manifest.Tools))
	seen := map[string]bool{}
	for _, raw := range manifest.Tools {
		var name string
		if err := json.Unmarshal(raw, &name); err == nil {
			name = strings.TrimSpace(name)
			if name != "" && !seen[name] {
				tools = append(tools, Tool{Name: name, InputSchema: map[string]any{"type": "object"}})
				seen[name] = true
			}
			continue
		}
		var tool Tool
		if err := json.Unmarshal(raw, &tool); err != nil {
			return nil, err
		}
		tool.Name = strings.TrimSpace(tool.Name)
		if tool.Name == "" || seen[tool.Name] {
			continue
		}
		if tool.InputSchema == nil {
			return nil, fmt.Errorf("plugin tool %q is missing inputSchema", tool.Name)
		}
		if err := validateSchema(tool.InputSchema); err != nil {
			return nil, fmt.Errorf("plugin tool %q schema: %w", tool.Name, err)
		}
		tools = append(tools, tool)
		seen[tool.Name] = true
	}
	return tools, nil
}

func Discover(ctx context.Context, plugin store.PluginRecord) (Discovery, error) {
	session, err := defaultManager.session(ctx, plugin)
	if err != nil {
		return Discovery{}, fmt.Errorf("discover plugin %q: %w", plugin.ID, err)
	}
	return cloneDiscovery(session.discovery), nil
}

func Execute(ctx context.Context, plugin store.PluginRecord, toolName string, input map[string]any, sessionID string, gate PermissionGate) (ExecuteResult, error) {
	if err := validatePlugin(plugin); err != nil {
		return ExecuteResult{}, err
	}
	toolName = strings.TrimSpace(toolName)
	if toolName == "" {
		return ExecuteResult{}, errors.New("tool name is required")
	}
	if input == nil {
		input = map[string]any{}
	}
	result := ExecuteResult{PluginID: plugin.ID, Tool: toolName, Input: input}
	pattern := plugin.ID + ":" + toolName
	if gate != nil && strings.TrimSpace(sessionID) != "" && !gate.IsAllowed(sessionID, "plugin", pattern) {
		result.Status = "pending_permission"
		result.PermissionID = gate.Ask(sessionID, "plugin", pattern, "External plugin tool execution requires approval.")
		return result, nil
	}

	for attempt := 0; attempt < 2; attempt++ {
		process, err := defaultManager.session(ctx, plugin)
		if err != nil {
			return ExecuteResult{}, err
		}
		tool, ok := findTool(process.discovery.Tools, toolName)
		if !ok {
			return ExecuteResult{}, fmt.Errorf("plugin %q does not register tool %q", plugin.ID, toolName)
		}
		if err := validateToolInput(tool, input); err != nil {
			return ExecuteResult{}, fmt.Errorf("validate plugin tool %q input: %w", toolName, err)
		}
		raw, callErr := process.call(ctx, "tools/call", map[string]any{"name": toolName, "arguments": input})
		if callErr != nil {
			if errors.Is(callErr, ErrProcessClosed) && attempt == 0 && ctx.Err() == nil {
				defaultManager.invalidate(plugin.ID, process)
				continue
			}
			result.Status = "error"
			result.Output = callErr.Error()
			return result, callErr
		}
		output, structured, isError, parseErr := parseToolResult(raw)
		if parseErr != nil {
			return ExecuteResult{}, parseErr
		}
		result.Output = output
		result.StructuredOutput = structured
		result.IsError = isError
		if isError {
			result.Status = "error"
		} else {
			result.Status = "ok"
		}
		return result, nil
	}
	return ExecuteResult{}, ErrProcessClosed
}

func Close(pluginID string) error {
	return defaultManager.Close(pluginID)
}

func CloseAll() error {
	return defaultManager.CloseAll()
}

func (m *Manager) session(ctx context.Context, plugin store.PluginRecord) (*processSession, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validatePlugin(plugin); err != nil {
		return nil, err
	}
	fingerprint, err := pluginFingerprint(plugin)
	if err != nil {
		return nil, err
	}
	lock := m.pluginLock(plugin.ID)
	lock.Lock()
	defer lock.Unlock()

	m.mu.Lock()
	existing := m.sessions[plugin.ID]
	m.mu.Unlock()
	if existing != nil && existing.fingerprint == fingerprint && !existing.isClosed() {
		return existing, nil
	}
	if existing != nil {
		m.mu.Lock()
		delete(m.sessions, plugin.ID)
		m.mu.Unlock()
		_ = existing.close()
	}

	process, err := startProcess(ctx, plugin, fingerprint)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.sessions[plugin.ID] = process
	m.mu.Unlock()
	return process, nil
}

func (m *Manager) pluginLock(pluginID string) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	lock := m.locks[pluginID]
	if lock == nil {
		lock = &sync.Mutex{}
		m.locks[pluginID] = lock
	}
	return lock
}

func (m *Manager) invalidate(pluginID string, process *processSession) {
	lock := m.pluginLock(pluginID)
	lock.Lock()
	defer lock.Unlock()
	m.mu.Lock()
	current := m.sessions[pluginID]
	if current == process {
		delete(m.sessions, pluginID)
	} else {
		process = nil
	}
	m.mu.Unlock()
	if process != nil {
		_ = process.close()
	}
}

func (m *Manager) Close(pluginID string) error {
	pluginID = strings.TrimSpace(pluginID)
	if pluginID == "" {
		return nil
	}
	lock := m.pluginLock(pluginID)
	lock.Lock()
	defer lock.Unlock()
	m.mu.Lock()
	process := m.sessions[pluginID]
	delete(m.sessions, pluginID)
	m.mu.Unlock()
	if process == nil {
		return nil
	}
	return process.close()
}

func (m *Manager) CloseAll() error {
	m.mu.Lock()
	processes := make([]*processSession, 0, len(m.sessions))
	for _, process := range m.sessions {
		processes = append(processes, process)
	}
	m.sessions = map[string]*processSession{}
	m.mu.Unlock()
	var errs []error
	for _, process := range processes {
		if err := process.close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func startProcess(ctx context.Context, plugin store.PluginRecord, fingerprint string) (*processSession, error) {
	processContext, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(processContext, plugin.Command, plugin.Args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	process := &processSession{
		fingerprint: fingerprint, cmd: cmd, stdin: stdin, cancel: cancel, pending: map[string]chan callResult{}, done: make(chan struct{}),
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}
	go process.readResponses(stdout)
	go process.readStderr(stderr)
	go func() {
		err := cmd.Wait()
		if err != nil {
			stderrText := process.stderrText()
			if stderrText != "" {
				err = fmt.Errorf("%w: %v: %s", ErrProcessClosed, err, stderrText)
			} else {
				err = fmt.Errorf("%w: %v", ErrProcessClosed, err)
			}
		} else {
			err = ErrProcessClosed
		}
		process.fail(err)
	}()

	if err := process.initialize(ctx, plugin); err != nil {
		_ = process.close()
		return nil, err
	}
	return process, nil
}

func (p *processSession) initialize(ctx context.Context, plugin store.PluginRecord) error {
	raw, err := p.call(ctx, "initialize", map[string]any{
		"protocolVersion": pluginProtocolVersion,
		"clientInfo":      map[string]any{"name": "superscience", "version": "dev"},
	})
	if err != nil {
		return err
	}
	var initialized struct {
		ProtocolVersion string         `json:"protocolVersion"`
		PluginInfo      map[string]any `json:"pluginInfo"`
		Capabilities    map[string]any `json:"capabilities"`
	}
	if err := json.Unmarshal(raw, &initialized); err != nil {
		return fmt.Errorf("decode plugin initialize result: %w", err)
	}
	if initialized.ProtocolVersion != pluginProtocolVersion {
		return fmt.Errorf("plugin protocol %q is unsupported", initialized.ProtocolVersion)
	}
	if !capabilityEnabled(initialized.Capabilities["tools"]) {
		return errors.New("plugin does not declare tools capability")
	}
	p.discovery = Discovery{
		ProtocolVersion: initialized.ProtocolVersion,
		PluginName:      stringValue(initialized.PluginInfo["name"]),
		PluginVersion:   stringValue(initialized.PluginInfo["version"]),
		Capabilities:    initialized.Capabilities,
		Tools:           []Tool{},
		Providers:       []ProviderRegistration{},
		Hooks:           []HookRegistration{},
	}
	if p.discovery.PluginName == "" {
		return errors.New("plugin initialize result is missing pluginInfo.name")
	}
	toolsRaw, err := p.call(ctx, "tools/list", map[string]any{})
	if err != nil {
		return err
	}
	var toolsResult struct {
		Tools []Tool `json:"tools"`
	}
	if err := json.Unmarshal(toolsRaw, &toolsResult); err != nil {
		return fmt.Errorf("decode plugin tools: %w", err)
	}
	if err := validateRuntimeTools(plugin, toolsResult.Tools); err != nil {
		return err
	}
	p.discovery.Tools = toolsResult.Tools

	if capabilityEnabled(initialized.Capabilities["providers"]) {
		providersRaw, err := p.call(ctx, "providers/list", map[string]any{})
		if err != nil {
			return err
		}
		var providersResult struct {
			Providers []ProviderRegistration `json:"providers"`
		}
		if err := json.Unmarshal(providersRaw, &providersResult); err != nil {
			return err
		}
		p.discovery.Providers = providersResult.Providers
	}
	if capabilityEnabled(initialized.Capabilities["hooks"]) {
		hooksRaw, err := p.call(ctx, "hooks/list", map[string]any{})
		if err != nil {
			return err
		}
		var hooksResult struct {
			Hooks []HookRegistration `json:"hooks"`
		}
		if err := json.Unmarshal(hooksRaw, &hooksResult); err != nil {
			return err
		}
		p.discovery.Hooks = hooksResult.Hooks
	}
	return nil
}

func (p *processSession) call(ctx context.Context, method string, params map[string]any) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	id := p.nextID.Add(1)
	idKey := fmt.Sprintf("%d", id)
	response := make(chan callResult, 1)
	p.mu.Lock()
	if p.closedErr != nil {
		err := p.closedErr
		p.mu.Unlock()
		return nil, err
	}
	p.pending[idKey] = response
	p.mu.Unlock()

	payload, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if err != nil {
		p.removePending(idKey)
		return nil, err
	}
	p.writeMu.Lock()
	_, err = p.stdin.Write(append(payload, '\n'))
	p.writeMu.Unlock()
	if err != nil {
		p.removePending(idKey)
		return nil, fmt.Errorf("%w: %v", ErrProcessClosed, err)
	}

	select {
	case result := <-response:
		return result.result, result.err
	case <-ctx.Done():
		p.removePending(idKey)
		p.notifyCancel(id)
		return nil, ctx.Err()
	}
}

func (p *processSession) notifyCancel(id uint64) {
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "method": "$/cancelRequest", "params": map[string]any{"id": id},
	})
	if err != nil {
		return
	}
	p.writeMu.Lock()
	_, _ = p.stdin.Write(append(payload, '\n'))
	p.writeMu.Unlock()
}

func (p *processSession) readResponses(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), maxPluginMessageBytes)
	for scanner.Scan() {
		var response rpcResponse
		if err := json.Unmarshal(scanner.Bytes(), &response); err != nil || len(response.ID) == 0 {
			continue
		}
		key := strings.TrimSpace(string(response.ID))
		p.mu.Lock()
		pending := p.pending[key]
		delete(p.pending, key)
		p.mu.Unlock()
		if pending == nil {
			continue
		}
		if response.Error != nil {
			pending <- callResult{err: response.Error}
		} else {
			pending <- callResult{result: response.Result}
		}
	}
	if err := scanner.Err(); err != nil {
		p.fail(fmt.Errorf("%w: read stdout: %v", ErrProcessClosed, err))
	}
}

func (p *processSession) readStderr(reader io.Reader) {
	buffer := make([]byte, 8*1024)
	for {
		read, err := reader.Read(buffer)
		if read > 0 {
			p.stderrMu.Lock()
			p.stderr = append(p.stderr, buffer[:read]...)
			if len(p.stderr) > maxPluginStderrBytes {
				p.stderr = append([]byte(nil), p.stderr[len(p.stderr)-maxPluginStderrBytes:]...)
			}
			p.stderrMu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

func (p *processSession) stderrText() string {
	p.stderrMu.Lock()
	defer p.stderrMu.Unlock()
	return strings.TrimSpace(string(p.stderr))
}

func (p *processSession) removePending(id string) {
	p.mu.Lock()
	delete(p.pending, id)
	p.mu.Unlock()
}

func (p *processSession) fail(err error) {
	if err == nil {
		err = ErrProcessClosed
	}
	p.closeOnce.Do(func() {
		p.mu.Lock()
		p.closedErr = err
		pending := p.pending
		p.pending = map[string]chan callResult{}
		p.mu.Unlock()
		for _, waiter := range pending {
			waiter <- callResult{err: err}
		}
		close(p.done)
	})
}

func (p *processSession) isClosed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closedErr != nil
}

func (p *processSession) close() error {
	if p.isClosed() {
		p.cancel()
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
	_, _ = p.call(ctx, "shutdown", map[string]any{})
	cancel()
	_ = p.stdin.Close()
	select {
	case <-p.done:
	case <-time.After(750 * time.Millisecond):
		p.cancel()
		select {
		case <-p.done:
		case <-time.After(250 * time.Millisecond):
			if p.cmd.Process != nil {
				_ = p.cmd.Process.Kill()
			}
		}
	}
	p.cancel()
	return nil
}

func validatePlugin(plugin store.PluginRecord) error {
	if strings.TrimSpace(plugin.ID) == "" {
		return errors.New("plugin id is required")
	}
	if !plugin.Enabled || plugin.Status == "disabled" {
		return fmt.Errorf("plugin %q is disabled", plugin.ID)
	}
	if plugin.Runtime != "" && plugin.Runtime != "subprocess" {
		return fmt.Errorf("unsupported plugin runtime %q", plugin.Runtime)
	}
	if strings.TrimSpace(plugin.Command) == "" {
		return fmt.Errorf("plugin %q command is required", plugin.ID)
	}
	return nil
}

func pluginFingerprint(plugin store.PluginRecord) (string, error) {
	data, err := json.Marshal(struct {
		Command  string   `json:"command"`
		Args     []string `json:"args"`
		Manifest string   `json:"manifest"`
	}{plugin.Command, plugin.Args, plugin.Manifest})
	return string(data), err
}

func validateRuntimeTools(plugin store.PluginRecord, runtimeTools []Tool) error {
	manifestTools, err := ManifestTools(plugin)
	if err != nil {
		return err
	}
	manifestNames := map[string]bool{}
	for _, tool := range manifestTools {
		manifestNames[tool.Name] = true
	}
	seen := map[string]bool{}
	for _, tool := range runtimeTools {
		tool.Name = strings.TrimSpace(tool.Name)
		if tool.Name == "" || seen[tool.Name] {
			return errors.New("plugin runtime returned an empty or duplicate tool name")
		}
		if len(manifestNames) > 0 && !manifestNames[tool.Name] {
			return fmt.Errorf("plugin runtime registered undeclared tool %q", tool.Name)
		}
		if tool.InputSchema == nil {
			return fmt.Errorf("plugin runtime tool %q is missing inputSchema", tool.Name)
		}
		if err := validateSchema(tool.InputSchema); err != nil {
			return fmt.Errorf("plugin runtime tool %q schema: %w", tool.Name, err)
		}
		seen[tool.Name] = true
	}
	for name := range manifestNames {
		if !seen[name] {
			return fmt.Errorf("plugin manifest tool %q was not registered by the runtime", name)
		}
	}
	return nil
}

func validateSchema(schemaMap map[string]any) error {
	data, err := json.Marshal(schemaMap)
	if err != nil {
		return err
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(data, &schema); err != nil {
		return err
	}
	_, err = schema.Resolve(nil)
	return err
}

func validateToolInput(tool Tool, input map[string]any) error {
	data, err := json.Marshal(tool.InputSchema)
	if err != nil {
		return err
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(data, &schema); err != nil {
		return err
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		return err
	}
	return resolved.Validate(input)
}

func findTool(tools []Tool, name string) (Tool, bool) {
	for _, tool := range tools {
		if tool.Name == name {
			return tool, true
		}
	}
	return Tool{}, false
}

func capabilityEnabled(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case map[string]any:
		return true
	default:
		return false
	}
}

func stringValue(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func cloneDiscovery(discovery Discovery) Discovery {
	data, _ := json.Marshal(discovery)
	var clone Discovery
	_ = json.Unmarshal(data, &clone)
	return clone
}

func parseToolResult(result json.RawMessage) (string, any, bool, error) {
	if len(result) == 0 {
		return "", nil, false, errors.New("plugin returned an empty tool result")
	}
	var text string
	if err := json.Unmarshal(result, &text); err == nil {
		if len(text) > maxPluginMessageBytes {
			return "", nil, false, errors.New("plugin tool output exceeds size limit")
		}
		return text, nil, false, nil
	}
	var payload struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		StructuredContent any  `json:"structuredContent"`
		Result            any  `json:"result"`
		IsError           bool `json:"isError"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		return "", nil, false, err
	}
	parts := make([]string, 0, len(payload.Content))
	for _, item := range payload.Content {
		if item.Type == "text" && item.Text != "" {
			parts = append(parts, item.Text)
		}
	}
	output := strings.Join(parts, "\n")
	structured := payload.StructuredContent
	if structured == nil {
		structured = payload.Result
	}
	if output == "" && structured != nil {
		data, err := json.Marshal(structured)
		if err != nil {
			return "", nil, false, err
		}
		output = string(data)
	}
	if output == "" {
		return "", structured, payload.IsError, errors.New("plugin returned no tool content")
	}
	if len(output) > maxPluginMessageBytes {
		return "", nil, false, errors.New("plugin tool output exceeds size limit")
	}
	return output, structured, payload.IsError, nil
}
