package lsp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Request struct {
	Language string
	Path     string
	Text     string
}

type ServerConfig struct {
	Command string
	Args    []string
	Env     []string
}

type Manager struct {
	mu      sync.RWMutex
	servers map[string]ServerConfig
}

type Session struct {
	ID           string       `json:"id"`
	Language     string       `json:"language"`
	Path         string       `json:"path"`
	Diagnostics  []Diagnostic `json:"diagnostics"`
	Hover        string       `json:"hover"`
	Completions  []string     `json:"completions"`
	Definitions  []Location   `json:"definitions"`
	Capabilities []string     `json:"capabilities"`
	CreatedAt    int64        `json:"createdAt"`
}

type Diagnostic struct {
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
}

type Location struct {
	Path   string `json:"path"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

func NewManager() *Manager {
	return &Manager{servers: map[string]ServerConfig{}}
}

var defaultManager = NewManager()

func RegisterServer(language string, config ServerConfig) {
	defaultManager.RegisterServer(language, config)
}

func (m *Manager) RegisterServer(language string, config ServerConfig) {
	language = normalizeLanguage(language, "")
	m.mu.Lock()
	m.servers[language] = config
	m.mu.Unlock()
}

func Analyze(ctx context.Context, language, path, text string) (Session, error) {
	return defaultManager.Analyze(ctx, Request{Language: language, Path: path, Text: text})
}

func (m *Manager) Analyze(ctx context.Context, request Request) (Session, error) {
	language := normalizeLanguage(request.Language, request.Path)
	request.Language = language
	m.mu.RLock()
	config, ok := m.servers[language]
	m.mu.RUnlock()
	if !ok || strings.TrimSpace(config.Command) == "" {
		config, ok = discoverServer(language)
	}
	if !ok || strings.TrimSpace(config.Command) == "" {
		return Session{}, fmt.Errorf("%s language server is unavailable; install and configure a supported LSP server", language)
	}
	return m.analyzeWithServer(ctx, request, config)
}

func (m *Manager) analyzeWithServer(ctx context.Context, request Request, config ServerConfig) (Session, error) {
	ctx, cancel := context.WithTimeout(ctx, lspTimeout(ctx))
	defer cancel()

	cmd := exec.CommandContext(ctx, config.Command, config.Args...)
	cmd.Env = append(os.Environ(), config.Env...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return Session{}, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return Session{}, err
	}
	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return Session{}, err
	}
	client := newRPCClient(stdin, stdout)
	defer func() {
		_ = client.notify("exit", nil)
		_ = stdin.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()

	rootURI := fileURI(filepath.Dir(request.Path))
	if _, err := client.request(ctx, "initialize", map[string]any{
		"processId": os.Getpid(),
		"rootUri":   rootURI,
		"capabilities": map[string]any{
			"textDocument": map[string]any{},
		},
	}, nil); err != nil {
		return Session{}, withStderr(err, stderr)
	}
	if err := client.notify("initialized", map[string]any{}); err != nil {
		return Session{}, err
	}
	uri := fileURI(request.Path)
	session := baseSession(request.Language, request.Path)
	session.Capabilities = []string{"diagnostics", "hover", "completion", "definition", "external-lsp"}
	if err := client.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri":        uri,
			"languageId": request.Language,
			"version":    1,
			"text":       request.Text,
		},
	}); err != nil {
		return Session{}, err
	}
	client.collectDiagnostics(ctx, 250*time.Millisecond, &session)
	if result, err := client.request(ctx, "textDocument/hover", textPositionParams(uri)); err == nil {
		session.Hover = parseHover(result)
	}
	if result, err := client.request(ctx, "textDocument/completion", textPositionParams(uri)); err == nil {
		session.Completions = parseCompletions(result)
	}
	if result, err := client.request(ctx, "textDocument/definition", textPositionParams(uri)); err == nil {
		session.Definitions = parseDefinitions(result)
	}
	_, _ = client.request(ctx, "shutdown", nil)
	return session, nil
}

type rpcClient struct {
	writer   io.Writer
	messages chan rpcMessage
	mu       sync.Mutex
	nextID   int
}

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func newRPCClient(writer io.Writer, reader io.Reader) *rpcClient {
	client := &rpcClient{writer: writer, messages: make(chan rpcMessage, 32)}
	go client.readLoop(reader)
	return client
}

func (c *rpcClient) request(ctx context.Context, method string, params any, sessions ...*Session) (json.RawMessage, error) {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	if err := writeFrame(c.writer, map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		c.mu.Unlock()
		return nil, err
	}
	c.mu.Unlock()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case message, ok := <-c.messages:
			if !ok {
				return nil, errors.New("lsp server closed")
			}
			if message.Method == "textDocument/publishDiagnostics" && len(sessions) > 0 && sessions[0] != nil {
				applyDiagnostics(message.Params, sessions[0])
				continue
			}
			if strings.TrimSpace(string(message.ID)) != strconv.Itoa(id) {
				continue
			}
			if message.Error != nil {
				return nil, errors.New(message.Error.Message)
			}
			return message.Result, nil
		}
	}
}

func (c *rpcClient) notify(method string, params any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return writeFrame(c.writer, map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (c *rpcClient) collectDiagnostics(ctx context.Context, duration time.Duration, session *Session) {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			return
		case message, ok := <-c.messages:
			if !ok {
				return
			}
			if message.Method == "textDocument/publishDiagnostics" {
				applyDiagnostics(message.Params, session)
			}
		}
	}
}

func (c *rpcClient) readLoop(reader io.Reader) {
	buffered := bufio.NewReader(reader)
	for {
		data, err := readFrame(buffered)
		if err != nil {
			close(c.messages)
			return
		}
		var message rpcMessage
		if err := json.Unmarshal(data, &message); err == nil {
			c.messages <- message
		}
	}
}

func writeFrame(writer io.Writer, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(writer, "Content-Length: %d\r\n\r\n%s", len(data), data)
	return err
}

func readFrame(reader *bufio.Reader) ([]byte, error) {
	contentLength := 0
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if strings.HasPrefix(strings.ToLower(line), "content-length:") {
			_, value, _ := strings.Cut(line, ":")
			contentLength, _ = strconv.Atoi(strings.TrimSpace(value))
		}
	}
	if contentLength <= 0 {
		return nil, io.ErrUnexpectedEOF
	}
	data := make([]byte, contentLength)
	_, err := io.ReadFull(reader, data)
	return data, err
}

func baseSession(language, path string) Session {
	return Session{
		ID:        "lsp_" + strings.ReplaceAll(time.Now().Format("150405.000000000"), ".", ""),
		Language:  language,
		Path:      path,
		CreatedAt: time.Now().UnixMilli(),
	}
}

func discoverServer(language string) (ServerConfig, bool) {
	candidates := map[string][]ServerConfig{
		"go": {
			{Command: "gopls"},
		},
		"python": {
			{Command: "pyright-langserver", Args: []string{"--stdio"}},
			{Command: "basedpyright-langserver", Args: []string{"--stdio"}},
			{Command: "ty", Args: []string{"server"}},
		},
		"typescript": {
			{Command: "typescript-language-server", Args: []string{"--stdio"}},
		},
	}
	for _, config := range candidates[language] {
		path, err := exec.LookPath(config.Command)
		if err != nil {
			continue
		}
		config.Command = path
		return config, true
	}
	return ServerConfig{}, false
}

func normalizeLanguage(language, path string) string {
	language = strings.ToLower(strings.TrimSpace(language))
	if language != "" {
		return language
	}
	return inferLanguage(path)
}

func inferLanguage(path string) string {
	switch {
	case strings.HasSuffix(path, ".go"):
		return "go"
	case strings.HasSuffix(path, ".py"):
		return "python"
	case strings.HasSuffix(path, ".ts"), strings.HasSuffix(path, ".tsx"):
		return "typescript"
	default:
		return "text"
	}
}

func textPositionParams(uri string) map[string]any {
	return map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": 0, "character": 0},
	}
}

func applyDiagnostics(raw json.RawMessage, session *Session) {
	var payload struct {
		Diagnostics []struct {
			Range struct {
				Start struct {
					Line      int `json:"line"`
					Character int `json:"character"`
				} `json:"start"`
			} `json:"range"`
			Severity int    `json:"severity"`
			Message  string `json:"message"`
		} `json:"diagnostics"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return
	}
	session.Diagnostics = session.Diagnostics[:0]
	for _, diagnostic := range payload.Diagnostics {
		session.Diagnostics = append(session.Diagnostics, Diagnostic{
			Severity: severityName(diagnostic.Severity),
			Message:  diagnostic.Message,
			Line:     diagnostic.Range.Start.Line + 1,
			Column:   diagnostic.Range.Start.Character + 1,
		})
	}
}

func severityName(value int) string {
	switch value {
	case 1:
		return "error"
	case 2:
		return "warning"
	case 3:
		return "info"
	case 4:
		return "hint"
	default:
		return "info"
	}
}

func parseHover(raw json.RawMessage) string {
	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	root := mapFromAny(payload)
	contents := root["contents"]
	if text := stringFromAny(contents); text != "" && !strings.HasPrefix(text, "map[") {
		return text
	}
	return firstField(mapFromAny(contents), "value", "text")
}

func parseCompletions(raw json.RawMessage) []string {
	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}
	records := recordsFromAny(payload, "items")
	result := make([]string, 0, len(records))
	for _, record := range records {
		if label := firstField(record, "label", "insertText"); label != "" {
			result = append(result, label)
		}
	}
	return result
}

func parseDefinitions(raw json.RawMessage) []Location {
	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}
	records := recordsFromAny(payload)
	if len(records) == 0 {
		if record := mapFromAny(payload); len(record) > 0 {
			records = []map[string]any{record}
		}
	}
	result := make([]Location, 0, len(records))
	for _, record := range records {
		start := mapFromAny(mapFromAny(record["range"])["start"])
		result = append(result, Location{
			Path:   pathFromURI(firstField(record, "uri", "targetUri")),
			Line:   intFromAny(start["line"]) + 1,
			Column: intFromAny(start["character"]) + 1,
		})
	}
	return result
}

func fileURI(path string) string {
	if strings.TrimSpace(path) == "" {
		path = "."
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	return (&url.URL{Scheme: "file", Path: abs}).String()
}

func pathFromURI(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "file" {
		return raw
	}
	return parsed.Path
}

func lspTimeout(ctx context.Context) time.Duration {
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining > 0 {
			return remaining
		}
	}
	return 5 * time.Second
}

func withStderr(err error, stderr *bytes.Buffer) error {
	if stderr == nil || strings.TrimSpace(stderr.String()) == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
}

func mapFromAny(value any) map[string]any {
	switch typed := value.(type) {
	case map[string]any:
		return typed
	default:
		return nil
	}
}

func recordsFromAny(value any, keys ...string) []map[string]any {
	switch typed := value.(type) {
	case []any:
		records := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if record := mapFromAny(item); len(record) > 0 {
				records = append(records, record)
			}
		}
		return records
	case map[string]any:
		for _, key := range keys {
			if records := recordsFromAny(typed[key]); len(records) > 0 {
				return records
			}
		}
		return nil
	default:
		return nil
	}
}

func stringFromAny(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := stringFromAny(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func firstField(record map[string]any, keys ...string) string {
	for _, key := range keys {
		if text := stringFromAny(record[key]); text != "" {
			return text
		}
	}
	return ""
}

func intFromAny(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case float64:
		return int(typed)
	case json.Number:
		number, _ := typed.Int64()
		return int(number)
	default:
		return 0
	}
}
