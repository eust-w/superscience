package server_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"superscience/internal/agent"
	mcpclient "superscience/internal/mcp"
	pluginruntime "superscience/internal/plugins"
	"superscience/internal/providers"
	"superscience/internal/server"
	"superscience/internal/store"
)

type failingSecretStore struct{}

func (failingSecretStore) Store(string, string) error  { return errors.New("keychain unavailable") }
func (failingSecretStore) Load(string) (string, error) { return "", errors.New("keychain unavailable") }
func (failingSecretStore) Delete(string) error         { return errors.New("keychain unavailable") }

type memorySecretStore struct {
	mu     sync.Mutex
	values map[string]string
}

func newMemorySecretStore() *memorySecretStore {
	return &memorySecretStore{values: map[string]string{}}
}

func (s *memorySecretStore) Store(account, secret string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[account] = secret
	return nil
}

func (s *memorySecretStore) Load(account string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.values[account]
	if !ok {
		return "", errors.New("secret not found")
	}
	return value, nil
}

func (s *memorySecretStore) Delete(account string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.values, account)
	return nil
}

type serverTestProvider struct {
	calls    int
	textOnly bool
}

func (p *serverTestProvider) Stream(_ context.Context, _ providers.ChatRequest, _ func(providers.StreamEvent)) (agent.ProviderResult, error) {
	p.calls++
	if p.textOnly {
		return agent.ProviderResult{Text: "Verified provider test response.", Usage: agent.ProviderUsage{PromptTokens: 2, CompletionTokens: 2, TotalTokens: 4}}, nil
	}
	if p.calls == 1 {
		return agent.ProviderResult{
			ToolCalls: []agent.ProviderToolCall{{ID: "server_test_artifact", Name: "artifact", Arguments: `{"type":"markdown","title":"Protocol test artifact","content":"Verified protocol test output."}`}},
			Usage:     agent.ProviderUsage{PromptTokens: 2, CompletionTokens: 2, TotalTokens: 4},
		}, nil
	}
	return agent.ProviderResult{Text: "Verified provider test response.", Usage: agent.ProviderUsage{PromptTokens: 4, CompletionTokens: 4, TotalTokens: 8}}, nil
}

func serverTestProviderFactory(_ context.Context, request agent.RunRequest) (agent.Provider, error) {
	return &serverTestProvider{textOnly: request.MaxSteps == 1}, nil
}

type resumableServerProvider struct{}

func (resumableServerProvider) Stream(_ context.Context, request providers.ChatRequest, _ func(providers.StreamEvent)) (agent.ProviderResult, error) {
	for _, message := range request.Messages {
		if message.Role == "tool" && message.ToolCallID == "call_server_resume_write" && strings.Contains(message.Content, "server approved evidence") {
			return agent.ProviderResult{Text: "The approved server-side write completed."}, nil
		}
	}
	return agent.ProviderResult{
		Text: "The requested write needs approval.",
		ToolCalls: []providers.ToolCall{{
			ID:        "call_server_resume_write",
			Name:      "write",
			Arguments: `{"path":"notes/server-resume.md","content":"server approved evidence"}`,
		}},
	}, nil
}

type blockingServerProvider struct {
	started  chan struct{}
	canceled chan struct{}
	release  chan struct{}
}

func TestOriginGuardAllowsDesktopAndLocalhostCORSOnly(t *testing.T) {
	handler := server.New(server.Options{}).Handler()
	for _, origin := range []string{"wails://wails", "http://wails.localhost", "http://127.0.0.1:5173", "http://localhost:5173"} {
		t.Run(origin, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:4096/api/health", nil)
			request.Header.Set("Origin", origin)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK || response.Header().Get("Access-Control-Allow-Origin") != origin {
				t.Fatalf("origin %q response=%d allow-origin=%q body=%s", origin, response.Code, response.Header().Get("Access-Control-Allow-Origin"), response.Body.String())
			}
		})
	}
	preflight := httptest.NewRequest(http.MethodOptions, "http://127.0.0.1:4096/api/health", nil)
	preflight.Header.Set("Origin", "wails://wails")
	preflight.Header.Set("Access-Control-Request-Method", http.MethodGet)
	preflightResponse := httptest.NewRecorder()
	handler.ServeHTTP(preflightResponse, preflight)
	if preflightResponse.Code != http.StatusNoContent {
		t.Fatalf("preflight response=%d body=%s", preflightResponse.Code, preflightResponse.Body.String())
	}

	blocked := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:4096/api/health", nil)
	blocked.Header.Set("Origin", "https://attacker.invalid")
	blockedResponse := httptest.NewRecorder()
	handler.ServeHTTP(blockedResponse, blocked)
	if blockedResponse.Code != http.StatusForbidden || blockedResponse.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("blocked origin response=%d headers=%v", blockedResponse.Code, blockedResponse.Header())
	}
}

func TestArtifactsEndpointFiltersBySession(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "superscience.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, artifact := range []store.Artifact{
		{SessionID: "session_a", Title: "A", Content: "A"},
		{SessionID: "session_b", Title: "B", Content: "B"},
	} {
		if _, err := db.CreateArtifact(ctx, artifact); err != nil {
			t.Fatal(err)
		}
	}
	ts := httptest.NewServer(server.New(server.Options{Store: db}).Handler())
	defer ts.Close()

	artifacts := getJSON[[]store.Artifact](t, ts.URL+"/api/artifacts?sessionId=session_a")
	if len(artifacts) != 1 || artifacts[0].SessionID != "session_a" || artifacts[0].Title != "A" {
		t.Fatalf("filtered artifacts = %+v", artifacts)
	}
}

func (p blockingServerProvider) Stream(ctx context.Context, _ providers.ChatRequest, _ func(providers.StreamEvent)) (agent.ProviderResult, error) {
	p.started <- struct{}{}
	select {
	case <-ctx.Done():
		p.canceled <- struct{}{}
		return agent.ProviderResult{}, ctx.Err()
	case <-p.release:
		return agent.ProviderResult{}, errors.New("test provider released without cancellation")
	}
}

func TestAbortEndpointCancelsActiveProviderRequest(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "superscience.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	provider := blockingServerProvider{started: make(chan struct{}, 1), canceled: make(chan struct{}, 1), release: make(chan struct{})}
	app := server.New(server.Options{
		Store:       db,
		ProjectRoot: t.TempDir(),
		ProviderFactory: func(context.Context, agent.RunRequest) (agent.Provider, error) {
			return provider, nil
		},
	})
	ts := httptest.NewServer(app.Handler())
	defer ts.Close()
	defer close(provider.release)
	project := postJSON[store.Project](t, ts.URL+"/api/projects/open", map[string]string{"path": t.TempDir()})
	session := postJSON[store.Session](t, ts.URL+"/api/sessions", map[string]string{"projectId": project.ID, "agent": "research"})
	runRequest, _ := json.Marshal(map[string]any{"goal": "Wait until aborted", "agent": "research", "provider": "protocol-test", "model": "protocol-test-model"})
	done := make(chan struct{})
	go func() {
		resp, requestErr := http.Post(ts.URL+"/api/sessions/"+session.ID+"/run", "application/json", bytes.NewReader(runRequest))
		if requestErr == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
		close(done)
	}()
	select {
	case <-provider.started:
	case <-time.After(2 * time.Second):
		t.Fatal("provider did not start")
	}
	runs, err := db.ListAgentRuns(ctx, session.ID)
	if err != nil || len(runs) != 1 {
		t.Fatalf("active runs = %+v err=%v", runs, err)
	}
	postJSON[map[string]any](t, ts.URL+"/api/runs/"+runs[0].ID+"/abort", map[string]any{})
	select {
	case <-provider.canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("abort endpoint did not cancel the provider request")
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("agent run request did not finish after abort")
	}
	updated, err := db.GetAgentRun(ctx, runs[0].ID)
	if err != nil || updated.Status != "aborted" {
		t.Fatalf("aborted run = %+v err=%v", updated, err)
	}
}

func TestPermissionResponseResumesPersistedAgentCheckpoint(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "superscience.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	root := t.TempDir()
	app := server.New(server.Options{
		Store:       db,
		ProjectRoot: root,
		ProviderFactory: func(context.Context, agent.RunRequest) (agent.Provider, error) {
			return resumableServerProvider{}, nil
		},
	})
	ts := httptest.NewServer(app.Handler())
	defer ts.Close()
	project := postJSON[store.Project](t, ts.URL+"/api/projects/open", map[string]string{"path": root})
	session := postJSON[store.Session](t, ts.URL+"/api/sessions", map[string]string{"projectId": project.ID, "agent": "research"})
	waiting := postJSON[map[string]any](t, ts.URL+"/api/sessions/"+session.ID+"/run", map[string]any{
		"goal":     "Write a server-side evidence note",
		"agent":    "research",
		"provider": "protocol-test",
		"model":    "protocol-test-model",
		"maxSteps": 4,
	})
	if waiting["status"] != "waiting_permission" || waiting["permissionId"] == "" {
		t.Fatalf("waiting run = %+v", waiting)
	}
	runID := waiting["id"].(string)
	permissionID := waiting["permissionId"].(string)
	persistedPermission, err := db.GetPermission(ctx, permissionID)
	if err != nil || persistedPermission.Status != "pending" {
		t.Fatalf("persisted permission = %+v err=%v", persistedPermission, err)
	}

	response := postJSON[map[string]bool](t, ts.URL+"/api/permissions/"+permissionID+"/respond", map[string]string{"decision": "once"})
	if !response["ok"] {
		t.Fatalf("permission response = %+v", response)
	}
	content, err := os.ReadFile(filepath.Join(root, "notes", "server-resume.md"))
	if err != nil || string(content) != "server approved evidence" {
		t.Fatalf("resumed file content=%q err=%v", content, err)
	}
	run, err := db.GetAgentRun(ctx, runID)
	if err != nil || run.Status != "completed" {
		t.Fatalf("resumed run = %+v err=%v", run, err)
	}
	if _, err := db.GetRunCheckpoint(ctx, runID); err == nil {
		t.Fatal("completed server run retained a checkpoint")
	}
}

func TestServerProjectSessionAndMessageStream(t *testing.T) {
	modelServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("provider path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"Verified answer\"}}],\"usage\":{\"prompt_tokens\":2,\"completion_tokens\":2,\"total_tokens\":4}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer modelServer.Close()
	t.Setenv("SUPERSCIENCE_PROVIDER", "openai-compatible")
	t.Setenv("SUPERSCIENCE_BASE_URL", modelServer.URL+"/v1")
	t.Setenv("SUPERSCIENCE_API_KEY", "test-protocol-key")
	t.Setenv("SUPERSCIENCE_MODEL", "test-protocol-model")

	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "superscience.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	app := server.New(server.Options{Store: db})
	ts := httptest.NewServer(app.Handler())
	defer ts.Close()

	health := getJSON[map[string]any](t, ts.URL+"/api/health")
	if health["status"] != "ok" {
		t.Fatalf("health = %+v", health)
	}

	project := postJSON[store.Project](t, ts.URL+"/api/projects/open", map[string]string{"path": t.TempDir()})
	if project.ID == "" {
		t.Fatalf("project = %+v", project)
	}

	session := postJSON[store.Session](t, ts.URL+"/api/sessions", map[string]string{
		"projectId": project.ID,
		"title":     "Kinase screen",
		"agent":     "biology",
	})
	if session.ProjectID != project.ID {
		t.Fatalf("session = %+v", session)
	}

	body, _ := json.Marshal(map[string]string{"content": "Find papers about EGFR"})
	resp, err := http.Post(ts.URL+"/api/sessions/"+session.ID+"/messages/stream", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stream status = %d", resp.StatusCode)
	}
	scanner := bufio.NewScanner(resp.Body)
	var sawAssistant bool
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, `"role":"assistant"`) {
			sawAssistant = true
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if !sawAssistant {
		t.Fatal("stream did not include assistant message")
	}

	messages := getJSON[[]store.Message](t, ts.URL+"/api/sessions/"+session.ID+"/messages")
	if len(messages) < 2 {
		t.Fatalf("messages = %+v", messages)
	}

	permissions := getJSON[[]map[string]any](t, ts.URL+"/api/permissions")
	if len(permissions) != 0 {
		t.Fatalf("plain model response created unrelated permissions: %+v", permissions)
	}
}

func TestMessageStreamUsesPersistedProviderSettingsAndSecret(t *testing.T) {
	modelServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("provider path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer persisted-protocol-key" {
			t.Fatalf("authorization = %q", got)
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request["model"] != "persisted-protocol-model" {
			t.Fatalf("model = %#v", request["model"])
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"Persisted provider answer\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer modelServer.Close()
	t.Setenv("SUPERSCIENCE_PROVIDER", "mock")

	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "superscience.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.UpsertProvider(ctx, store.ProviderProfile{
		ID:          "persisted-protocol",
		Kind:        "openai-compatible",
		DisplayName: "Persisted protocol provider",
		BaseURL:     modelServer.URL + "/v1",
		Enabled:     true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetSettings(ctx, map[string]string{"defaultProvider": "persisted-protocol", "defaultModel": "persisted-protocol-model"}); err != nil {
		t.Fatal(err)
	}
	secretStore := newMemorySecretStore()
	if err := secretStore.Store("persisted-protocol", "persisted-protocol-key"); err != nil {
		t.Fatal(err)
	}
	app := server.New(server.Options{Store: db, Secrets: secretStore})
	ts := httptest.NewServer(app.Handler())
	defer ts.Close()

	project := postJSON[store.Project](t, ts.URL+"/api/projects/open", map[string]string{"path": t.TempDir()})
	session := postJSON[store.Session](t, ts.URL+"/api/sessions", map[string]string{"projectId": project.ID, "agent": "research"})
	body, _ := json.Marshal(map[string]string{"content": "Use persisted configuration"})
	response, err := http.Post(ts.URL+"/api/sessions/"+session.ID+"/messages/stream", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), "Persisted provider answer") {
		t.Fatalf("stream payload = %s", payload)
	}
}

func TestServerReturnsStructuredErrorWithRequestID(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "superscience.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ts := httptest.NewServer(server.New(server.Options{Store: db}).Handler())
	defer ts.Close()
	req, err := http.NewRequest(http.MethodPut, ts.URL+"/api/projects", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	requestID := resp.Header.Get("X-Request-ID")
	if resp.StatusCode != http.StatusMethodNotAllowed || body["code"] != "method_not_allowed" || body["message"] == "" || body["retryable"] != false || body["details"] == nil || body["requestId"] != requestID || requestID == "" {
		t.Fatalf("structured error status=%d headers=%+v body=%+v", resp.StatusCode, resp.Header, body)
	}
}

func TestServerSettingsAndCredentialFlow(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "superscience.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	app := server.New(server.Options{Store: db})
	ts := httptest.NewServer(app.Handler())
	defer ts.Close()

	settings := putJSON[map[string]string](t, ts.URL+"/api/settings", map[string]string{
		"theme":        "dark",
		"defaultModel": "gpt-5.4",
	})
	if settings["theme"] != "dark" {
		t.Fatalf("settings response = %+v", settings)
	}
	loaded := getJSON[map[string]string](t, ts.URL+"/api/settings")
	if loaded["defaultModel"] != "gpt-5.4" {
		t.Fatalf("loaded settings = %+v", loaded)
	}

	credential := postJSON[store.Credential](t, ts.URL+"/api/settings/credentials", map[string]string{
		"provider": "openai",
		"label":    "OpenAI",
		"source":   "environment",
	})
	if credential.Redacted == "" {
		t.Fatalf("credential leaked secret: %+v", credential)
	}
	result := postJSON[map[string]any](t, ts.URL+"/api/settings/credentials/test", map[string]string{})
	if result["ok"] != false || result["status"] != "unconfigured" {
		t.Fatalf("unconfigured credential test = %+v", result)
	}
}

func TestCredentialTestPerformsAuthenticatedProviderRequest(t *testing.T) {
	var calls int
	providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("provider health path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer sk-live-test" {
			t.Fatalf("provider health authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer providerServer.Close()

	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "superscience.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	app := server.New(server.Options{Store: db})
	ts := httptest.NewServer(app.Handler())
	defer ts.Close()

	result := postJSON[map[string]any](t, ts.URL+"/api/settings/credentials/test", map[string]string{
		"provider": "openai-compatible",
		"endpoint": providerServer.URL + "/v1",
		"key":      "sk-live-test",
		"model":    "health-model",
	})
	if calls != 1 {
		t.Fatalf("credential test did not call provider, calls=%d result=%+v", calls, result)
	}
	if result["ok"] != true || result["status"] != "ready" {
		t.Fatalf("credential test = %+v", result)
	}
}

func TestCredentialCreateDoesNotPersistMetadataWhenSecretStorageFails(t *testing.T) {
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "superscience.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	app := server.New(server.Options{Store: db, Secrets: failingSecretStore{}})
	ts := httptest.NewServer(app.Handler())
	defer ts.Close()

	body, _ := json.Marshal(map[string]string{"provider": "openai", "label": "OpenAI", "key": "secret-value"})
	resp, err := http.Post(ts.URL+"/api/settings/credentials", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("credential status = %d", resp.StatusCode)
	}
	credentials, err := db.ListCredentials(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(credentials) != 0 {
		t.Fatalf("credential metadata persisted after secret failure: %+v", credentials)
	}
}

func TestProviderHealthPerformsLiveAuthenticatedProbe(t *testing.T) {
	var calls int
	providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer provider-health-key" {
			t.Fatalf("health authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"healthy\"}}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer providerServer.Close()
	t.Setenv("SUPERSCIENCE_LAB_HEALTH_API_KEY", "provider-health-key")

	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "superscience.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	app := server.New(server.Options{Store: db})
	ts := httptest.NewServer(app.Handler())
	defer ts.Close()

	_ = postJSON[map[string]any](t, ts.URL+"/api/providers", map[string]any{
		"id":          "lab-health",
		"kind":        "openai-compatible",
		"displayName": "Lab Health",
		"baseURL":     providerServer.URL + "/v1",
		"enabled":     true,
	})
	health := getJSON[map[string]any](t, ts.URL+"/api/providers/lab-health/health?model=health-model")
	if calls != 1 {
		t.Fatalf("provider health did not call provider, calls=%d health=%+v", calls, health)
	}
	if health["ok"] != true || health["status"] != "ready" || health["model"] != "health-model" {
		t.Fatalf("provider health = %+v", health)
	}
}

func TestServerCreatesPTYForShellAlias(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "superscience.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	app := server.New(server.Options{Store: db, ProjectRoot: t.TempDir()})
	ts := httptest.NewServer(app.Handler())
	defer ts.Close()

	pty := postJSON[map[string]any](t, ts.URL+"/api/pty", map[string]string{"command": "shell"})
	if pty["id"] == "" || pty["command"] == "shell" {
		t.Fatalf("pty = %+v", pty)
	}
	req, err := http.NewRequest(http.MethodDelete, ts.URL+"/api/pty/"+pty["id"].(string), nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete pty status = %d", resp.StatusCode)
	}
}

func TestServerPermissionGatesDangerousExecutionAPIs(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "superscience.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	projectRoot := t.TempDir()
	app := server.New(server.Options{Store: db, ProjectRoot: projectRoot})
	ts := httptest.NewServer(app.Handler())
	defer ts.Close()

	project := postJSON[store.Project](t, ts.URL+"/api/projects/open", map[string]string{"path": projectRoot})
	session := postJSON[store.Session](t, ts.URL+"/api/sessions", map[string]string{
		"projectId": project.ID,
		"title":     "Permission gate",
		"agent":     "research",
	})

	writeAttempt := putJSON[map[string]any](t, ts.URL+"/api/files/content", map[string]string{
		"sessionId": session.ID,
		"path":      "notes/unsafe.md",
		"content":   "must wait",
	})
	if writeAttempt["status"] != "pending_permission" || writeAttempt["permissionId"] == "" {
		t.Fatalf("write was not gated: %+v", writeAttempt)
	}
	permissions := getJSON[[]map[string]any](t, ts.URL+"/api/permissions")
	if len(permissions) == 0 || permissions[0]["type"] != "write" {
		t.Fatalf("write permission missing: %+v", permissions)
	}
	_ = postJSON[map[string]bool](t, ts.URL+"/api/permissions/"+permissions[0]["id"].(string)+"/respond", map[string]string{"decision": "once"})
	writeResult := putJSON[map[string]any](t, ts.URL+"/api/files/content", map[string]string{
		"sessionId": session.ID,
		"path":      "notes/unsafe.md",
		"content":   "approved",
	})
	if writeResult["status"] != "ok" || writeResult["content"] != "approved" {
		t.Fatalf("write did not execute after approval: %+v", writeResult)
	}

	ptyAttempt := postJSON[map[string]any](t, ts.URL+"/api/pty", map[string]string{
		"sessionId": session.ID,
		"command":   "shell",
	})
	if ptyAttempt["status"] != "pending_permission" || ptyAttempt["permissionId"] == "" {
		t.Fatalf("pty was not gated: %+v", ptyAttempt)
	}
	permissions = getJSON[[]map[string]any](t, ts.URL+"/api/permissions")
	_ = postJSON[map[string]bool](t, ts.URL+"/api/permissions/"+permissions[0]["id"].(string)+"/respond", map[string]string{"decision": "once"})
	pty := postJSON[map[string]any](t, ts.URL+"/api/pty", map[string]string{
		"sessionId": session.ID,
		"command":   "shell",
	})
	if pty["id"] == "" || pty["command"] == "shell" {
		t.Fatalf("pty did not execute after approval: %+v", pty)
	}
	req, err := http.NewRequest(http.MethodDelete, ts.URL+"/api/pty/"+pty["id"].(string), nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	notebook := postJSON[map[string]any](t, ts.URL+"/api/notebooks", map[string]any{
		"title": "Permission notebook",
		"path":  "analysis/permission.ipynb",
		"cells": []map[string]any{{"kind": "code", "language": "python", "source": "print('approved notebook')"}},
	})
	cells := notebook["cells"].([]any)
	cell := cells[0].(map[string]any)
	notebookAttempt := postJSON[map[string]any](t, ts.URL+"/api/notebooks/"+notebook["id"].(string)+"/cells/"+cell["id"].(string)+"/run", map[string]any{
		"sessionId": session.ID,
	})
	if notebookAttempt["status"] != "pending_permission" || notebookAttempt["permissionId"] == "" {
		t.Fatalf("notebook run was not gated: %+v", notebookAttempt)
	}
	permissions = getJSON[[]map[string]any](t, ts.URL+"/api/permissions")
	notebookPermissionID := ""
	for _, permission := range permissions {
		if permission["type"] == "notebook" {
			notebookPermissionID = permission["id"].(string)
			break
		}
	}
	if notebookPermissionID == "" {
		t.Fatalf("notebook permission not found: %+v", permissions)
	}
	_ = postJSON[map[string]bool](t, ts.URL+"/api/permissions/"+notebookPermissionID+"/respond", map[string]string{"decision": "once"})
	notebookResult := postJSON[map[string]any](t, ts.URL+"/api/notebooks/"+notebook["id"].(string)+"/cells/"+cell["id"].(string)+"/run", map[string]any{
		"sessionId": session.ID,
	})
	if notebookResult["status"] != "ok" || !strings.Contains(notebookResult["stdout"].(string), "approved notebook") {
		t.Fatalf("notebook did not execute after approval: %+v", notebookResult)
	}

	artifact := postJSON[map[string]any](t, ts.URL+"/api/artifacts", map[string]any{
		"sessionId": session.ID,
		"type":      "markdown",
		"title":     "Permission artifact",
		"content":   "# Permission\n",
	})
	shareAttempt := postJSON[map[string]any](t, ts.URL+"/api/share/publish", map[string]any{
		"sessionId":   session.ID,
		"title":       "Permission share",
		"artifactIds": []string{artifact["id"].(string)},
	})
	if shareAttempt["status"] != "pending_permission" || shareAttempt["permissionId"] == "" {
		t.Fatalf("share publish was not gated: %+v", shareAttempt)
	}
	permissions = getJSON[[]map[string]any](t, ts.URL+"/api/permissions")
	sharePermissionID := ""
	for _, permission := range permissions {
		if permission["type"] == "share" {
			sharePermissionID = permission["id"].(string)
			break
		}
	}
	if sharePermissionID == "" {
		t.Fatalf("share permission not found: %+v", permissions)
	}
	_ = postJSON[map[string]bool](t, ts.URL+"/api/permissions/"+sharePermissionID+"/respond", map[string]string{"decision": "once"})
	shareResult := postJSON[map[string]any](t, ts.URL+"/api/share/publish", map[string]any{
		"sessionId":   session.ID,
		"title":       "Permission share",
		"artifactIds": []string{artifact["id"].(string)},
	})
	if shareResult["status"] != "published" || shareResult["url"] == "" {
		t.Fatalf("share did not publish after approval: %+v", shareResult)
	}

	scienceAttempt := postJSON[map[string]any](t, ts.URL+"/api/science/search", map[string]any{
		"sessionId": session.ID,
		"database":  "pubmed",
		"query":     "EGFR",
		"limit":     1,
	})
	if scienceAttempt["status"] != "pending_permission" || scienceAttempt["permissionId"] == "" || scienceAttempt["type"] != "network" {
		t.Fatalf("science search was not network gated: %+v", scienceAttempt)
	}
}

func TestSessionRunPersistsRuntimeStepsAndToolCalls(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "superscience.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	app := server.New(server.Options{Store: db, ProjectRoot: t.TempDir(), ProviderFactory: serverTestProviderFactory})
	ts := httptest.NewServer(app.Handler())
	defer ts.Close()

	project := postJSON[store.Project](t, ts.URL+"/api/projects/open", map[string]string{"path": t.TempDir()})
	session := postJSON[store.Session](t, ts.URL+"/api/sessions", map[string]string{"projectId": project.ID})
	run := postJSON[map[string]any](t, ts.URL+"/api/sessions/"+session.ID+"/run", map[string]string{
		"goal":  "Search EGFR and write a short artifact",
		"agent": "biology",
		"model": "mock",
	})
	runID, _ := run["id"].(string)
	if runID == "" || run["status"] == "" {
		t.Fatalf("run response = %+v", run)
	}
	runs := getJSON[[]map[string]any](t, ts.URL+"/api/sessions/"+session.ID+"/runs")
	if len(runs) != 1 || runs[0]["id"] != runID {
		t.Fatalf("runs not persisted: %+v", runs)
	}
	steps := getJSON[[]map[string]any](t, ts.URL+"/api/runs/"+runID+"/steps")
	if len(steps) < 3 {
		t.Fatalf("expected persisted run steps, got %+v", steps)
	}
	calls := getJSON[[]map[string]any](t, ts.URL+"/api/runs/"+runID+"/tool-calls")
	if len(calls) == 0 || calls[0]["tool"] == "" {
		t.Fatalf("expected persisted tool calls, got %+v", calls)
	}
}

func TestSessionRunRoutesThroughConfiguredProviderAdapter(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "superscience.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var sawModelRequest bool
	modelServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected provider path %s", r.URL.Path)
		}
		sawModelRequest = true
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"Adapter routed answer\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2,\"total_tokens\":5}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer modelServer.Close()

	projectRoot := t.TempDir()
	app := server.New(server.Options{Store: db, ProjectRoot: projectRoot})
	ts := httptest.NewServer(app.Handler())
	defer ts.Close()

	project := postJSON[store.Project](t, ts.URL+"/api/projects/open", map[string]string{"path": projectRoot})
	session := postJSON[store.Session](t, ts.URL+"/api/sessions", map[string]string{"projectId": project.ID})
	provider := postJSON[map[string]any](t, ts.URL+"/api/providers", map[string]any{
		"id":          "lab-router",
		"kind":        "openai-compatible",
		"displayName": "Lab Router",
		"baseURL":     modelServer.URL + "/v1",
		"enabled":     true,
	})
	if provider["id"] != "lab-router" {
		t.Fatalf("provider = %+v", provider)
	}

	run := postJSON[map[string]any](t, ts.URL+"/api/sessions/"+session.ID+"/run", map[string]string{
		"goal":     "Route through lab router",
		"provider": "lab-router",
		"model":    "lab-model",
	})
	if !sawModelRequest {
		t.Fatal("configured provider adapter was not called")
	}
	if run["status"] != "completed" || run["provider"] != "lab-router" || run["model"] != "lab-model" {
		t.Fatalf("run provider/model = %+v", run)
	}
	if !strings.Contains(run["message"].(map[string]any)["content"].(string), "Adapter routed answer") {
		t.Fatalf("run did not persist provider answer: %+v", run)
	}
	permissions := getJSON[[]map[string]any](t, ts.URL+"/api/permissions")
	if len(permissions) != 0 {
		t.Fatalf("text-only run created runtime permission: %+v", permissions)
	}
	events := getJSON[[]map[string]any](t, ts.URL+"/api/runs/"+run["id"].(string)+"/events")
	var sawUsage bool
	for _, event := range events {
		if event["type"] == "agent.step" && event["kind"] == "usage" {
			sawUsage = true
		}
	}
	if !sawUsage {
		t.Fatalf("usage event missing: %+v", events)
	}
}

func TestToolExecutePersistsCallsAndUsesPermissionGate(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "superscience.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	projectRoot := t.TempDir()
	app := server.New(server.Options{Store: db, ProjectRoot: projectRoot, ProviderFactory: serverTestProviderFactory})
	ts := httptest.NewServer(app.Handler())
	defer ts.Close()

	project := postJSON[store.Project](t, ts.URL+"/api/projects/open", map[string]string{"path": projectRoot})
	session := postJSON[store.Session](t, ts.URL+"/api/sessions", map[string]string{"projectId": project.ID})
	run := postJSON[map[string]any](t, ts.URL+"/api/sessions/"+session.ID+"/run", map[string]string{"goal": "tool test"})
	runID := run["id"].(string)

	writeAttempt := postJSON[map[string]any](t, ts.URL+"/api/tools/execute", map[string]any{
		"runId":     runID,
		"sessionId": session.ID,
		"tool":      "write",
		"input": map[string]any{
			"path":    "notes/tool.md",
			"content": "from tool",
		},
	})
	if writeAttempt["status"] != "pending_permission" {
		t.Fatalf("write tool was not gated: %+v", writeAttempt)
	}
	permissions := getJSON[[]map[string]any](t, ts.URL+"/api/permissions")
	writePermissionID := ""
	for _, permission := range permissions {
		if permission["type"] == "write" {
			writePermissionID = permission["id"].(string)
			break
		}
	}
	if writePermissionID == "" {
		t.Fatalf("write permission not found: %+v", permissions)
	}
	_ = postJSON[map[string]bool](t, ts.URL+"/api/permissions/"+writePermissionID+"/respond", map[string]string{"decision": "once"})
	writeResult := postJSON[map[string]any](t, ts.URL+"/api/tools/execute", map[string]any{
		"runId":     runID,
		"sessionId": session.ID,
		"tool":      "write",
		"input": map[string]any{
			"path":    "notes/tool.md",
			"content": "from tool",
		},
	})
	if writeResult["status"] != "ok" {
		t.Fatalf("write tool did not execute after approval: %+v", writeResult)
	}
	readResult := postJSON[map[string]any](t, ts.URL+"/api/tools/execute", map[string]any{
		"runId":     runID,
		"sessionId": session.ID,
		"tool":      "read",
		"input":     map[string]any{"path": "notes/tool.md"},
	})
	if readResult["status"] != "ok" || !strings.Contains(readResult["output"].(string), "from tool") {
		t.Fatalf("read tool result = %+v", readResult)
	}
	calls := getJSON[[]map[string]any](t, ts.URL+"/api/runs/"+runID+"/tool-calls")
	var sawRead, sawWrite bool
	for _, call := range calls {
		if call["tool"] == "read" {
			sawRead = true
		}
		if call["tool"] == "write" && call["status"] == "ok" {
			sawWrite = true
		}
	}
	if !sawRead || !sawWrite {
		t.Fatalf("tool calls missing read/write records: %+v", calls)
	}
}

func TestToolExecuteRunsPluginThroughUnifiedExecutor(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "superscience.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	projectRoot := t.TempDir()
	t.Setenv("SUPERSCIENCE_SERVER_PLUGIN_FIXTURE", "1")
	t.Cleanup(func() { _ = pluginruntime.CloseAll() })
	if _, err := db.UpsertPlugin(ctx, store.PluginRecord{
		ID:       "lab-plugin",
		Name:     "Lab Plugin",
		Runtime:  "subprocess",
		Command:  os.Args[0],
		Args:     []string{"-test.run=^TestServerPluginFixtureProcess$"},
		Enabled:  true,
		Status:   "configured",
		Manifest: `{"tools":[{"name":"lab_search","inputSchema":{"type":"object"}}]}`,
	}); err != nil {
		t.Fatal(err)
	}
	app := server.New(server.Options{Store: db, ProjectRoot: projectRoot, ProviderFactory: serverTestProviderFactory})
	ts := httptest.NewServer(app.Handler())
	defer ts.Close()

	project := postJSON[store.Project](t, ts.URL+"/api/projects/open", map[string]string{"path": projectRoot})
	session := postJSON[store.Session](t, ts.URL+"/api/sessions", map[string]string{"projectId": project.ID})
	run := postJSON[map[string]any](t, ts.URL+"/api/sessions/"+session.ID+"/run", map[string]string{"goal": "plugin tool"})
	runID := run["id"].(string)
	attempt := postJSON[map[string]any](t, ts.URL+"/api/tools/execute", map[string]any{
		"runId":     runID,
		"sessionId": session.ID,
		"tool":      "plugin",
		"input":     map[string]any{"pluginId": "lab-plugin", "tool": "lab_search", "input": map[string]any{"query": "EGFR"}},
	})
	if attempt["status"] != "pending_permission" || attempt["permissionId"] == "" {
		t.Fatalf("plugin tool was not gated: %+v", attempt)
	}
	permissions := getJSON[[]map[string]any](t, ts.URL+"/api/permissions")
	var permissionID string
	for _, permission := range permissions {
		if permission["type"] == "plugin" {
			permissionID = permission["id"].(string)
			break
		}
	}
	if permissionID == "" {
		t.Fatalf("plugin permission not found: %+v", permissions)
	}
	_ = postJSON[map[string]bool](t, ts.URL+"/api/permissions/"+permissionID+"/respond", map[string]string{"decision": "once"})
	result := postJSON[map[string]any](t, ts.URL+"/api/tools/execute", map[string]any{
		"runId":     runID,
		"sessionId": session.ID,
		"tool":      "plugin",
		"input":     map[string]any{"pluginId": "lab-plugin", "tool": "lab_search", "input": map[string]any{"query": "EGFR"}},
	})
	if result["status"] != "ok" || !strings.Contains(result["output"].(string), "unified plugin searched EGFR") {
		t.Fatalf("plugin tool result = %+v", result)
	}
	calls := getJSON[[]map[string]any](t, ts.URL+"/api/runs/"+runID+"/tool-calls")
	var sawPlugin bool
	for _, call := range calls {
		if call["tool"] == "plugin" && call["status"] == "ok" {
			sawPlugin = true
			break
		}
	}
	if !sawPlugin {
		t.Fatalf("plugin tool call was not persisted: %+v", calls)
	}
}

func TestToolExecuteRunsMCPResourceAndPromptThroughUnifiedExecutor(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "superscience.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	projectRoot := t.TempDir()
	mcpAPI := newMCPFixtureHTTPServer(t)
	if _, err := db.UpsertMCPServer(ctx, store.MCPServer{
		ID:        "lab-mcp",
		Name:      "Lab MCP",
		Transport: "streamable-http",
		URL:       mcpAPI.URL,
		Enabled:   true,
		Status:    "configured",
	}); err != nil {
		t.Fatal(err)
	}
	app := server.New(server.Options{Store: db, ProjectRoot: projectRoot, ProviderFactory: serverTestProviderFactory})
	ts := httptest.NewServer(app.Handler())
	defer ts.Close()

	project := postJSON[store.Project](t, ts.URL+"/api/projects/open", map[string]string{"path": projectRoot})
	session := postJSON[store.Session](t, ts.URL+"/api/sessions", map[string]string{"projectId": project.ID})
	run := postJSON[map[string]any](t, ts.URL+"/api/sessions/"+session.ID+"/run", map[string]string{"goal": "mcp resource prompt"})
	runID := run["id"].(string)
	resourceInput := map[string]any{"serverId": "lab-mcp", "uri": "file:///dataset.csv"}
	attempt := postJSON[map[string]any](t, ts.URL+"/api/tools/execute", map[string]any{
		"runId":     runID,
		"sessionId": session.ID,
		"tool":      "mcp_resource",
		"input":     resourceInput,
	})
	if attempt["status"] != "pending_permission" || attempt["permissionId"] == "" {
		t.Fatalf("mcp_resource was not gated: %+v", attempt)
	}
	approveFirstPermissionOfType(t, ts.URL, "mcp")
	resource := postJSON[map[string]any](t, ts.URL+"/api/tools/execute", map[string]any{
		"runId":     runID,
		"sessionId": session.ID,
		"tool":      "mcp_resource",
		"input":     resourceInput,
	})
	if resource["status"] != "ok" || !strings.Contains(resource["output"].(string), "EGFR,0.91") {
		t.Fatalf("mcp_resource result = %+v", resource)
	}

	promptInput := map[string]any{"serverId": "lab-mcp", "name": "summarize", "arguments": map[string]any{"topic": "EGFR"}}
	attempt = postJSON[map[string]any](t, ts.URL+"/api/tools/execute", map[string]any{
		"runId":     runID,
		"sessionId": session.ID,
		"tool":      "mcp_prompt",
		"input":     promptInput,
	})
	if attempt["status"] != "pending_permission" || attempt["permissionId"] == "" {
		t.Fatalf("mcp_prompt was not gated: %+v", attempt)
	}
	approveFirstPermissionOfType(t, ts.URL, "mcp")
	prompt := postJSON[map[string]any](t, ts.URL+"/api/tools/execute", map[string]any{
		"runId":     runID,
		"sessionId": session.ID,
		"tool":      "mcp_prompt",
		"input":     promptInput,
	})
	if prompt["status"] != "ok" || !strings.Contains(prompt["output"].(string), "Summarize EGFR evidence") {
		t.Fatalf("mcp_prompt result = %+v", prompt)
	}
	calls := getJSON[[]map[string]any](t, ts.URL+"/api/runs/"+runID+"/tool-calls")
	var sawResource, sawPrompt bool
	for _, call := range calls {
		if call["tool"] == "mcp_resource" && call["status"] == "ok" {
			sawResource = true
		}
		if call["tool"] == "mcp_prompt" && call["status"] == "ok" {
			sawPrompt = true
		}
	}
	if !sawResource || !sawPrompt {
		t.Fatalf("mcp resource/prompt calls were not persisted: %+v", calls)
	}
}

func TestProviderHealthCredentialDeleteAndModelRefresh(t *testing.T) {
	providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-provider-health-test" {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "missing provider credential"}})
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"healthy\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer providerServer.Close()

	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "superscience.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	app := server.New(server.Options{Store: db, ProjectRoot: t.TempDir()})
	ts := httptest.NewServer(app.Handler())
	defer ts.Close()

	_ = postJSON[map[string]any](t, ts.URL+"/api/providers", map[string]any{
		"id":          "health-delete",
		"kind":        "openai-compatible",
		"displayName": "Health Delete",
		"baseURL":     providerServer.URL + "/v1",
		"enabled":     true,
	})
	credential := postJSON[store.Credential](t, ts.URL+"/api/settings/credentials", map[string]string{
		"provider": "health-delete",
		"label":    "Health delete test",
		"key":      "sk-provider-health-test",
	})
	health := getJSON[map[string]any](t, ts.URL+"/api/providers/health-delete/health?model=health-model")
	if health["ok"] != true || health["status"] != "ready" || strings.Contains(toJSON(t, health), "sk-provider-health-test") {
		t.Fatalf("provider health leaked or missed credential: %+v", health)
	}
	refresh := postJSON[map[string]any](t, ts.URL+"/api/providers/models/refresh", map[string]any{})
	if refresh["status"] != "ok" || refresh["models"].(float64) == 0 {
		t.Fatalf("model refresh = %+v", refresh)
	}
	req, err := http.NewRequest(http.MethodDelete, ts.URL+"/api/settings/credentials?id="+credential.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete credential status = %d", resp.StatusCode)
	}
	health = getJSON[map[string]any](t, ts.URL+"/api/providers/health-delete/health?model=health-model")
	if health["ok"] != false || health["status"] != "error" {
		t.Fatalf("provider health still ready after delete: %+v", health)
	}
}

func TestProviderModelRefreshCachesModelsDevData(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "superscience.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	app := server.New(server.Options{Store: db, ProjectRoot: t.TempDir()})
	ts := httptest.NewServer(app.Handler())
	defer ts.Close()

	catalogAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api.json" {
			t.Fatalf("catalog path = %s", r.URL.Path)
		}
		payload := map[string]any{
			"openai": map[string]any{
				"name": "OpenAI",
				"models": map[string]any{
					"gpt-server-catalog": map[string]any{
						"name": "GPT Server Catalog",
						"limit": map[string]any{
							"context": 131072,
						},
						"features": []string{"tool_call"},
					},
				},
			},
		}
		if err := json.NewEncoder(w).Encode(payload); err != nil {
			t.Fatal(err)
		}
	}))
	defer catalogAPI.Close()

	refresh := postJSON[map[string]any](t, ts.URL+"/api/providers/models/refresh", map[string]any{
		"endpoint": catalogAPI.URL + "/api.json",
	})
	if refresh["status"] != "ok" || refresh["source"] != "models.dev" || refresh["models"].(float64) != 1 {
		t.Fatalf("model refresh = %+v", refresh)
	}
	models := getJSON[[]map[string]any](t, ts.URL+"/api/providers/openai/models")
	if len(models) != 1 || models[0]["id"] != "gpt-server-catalog" {
		t.Fatalf("cached models = %+v", models)
	}
	if models[0]["contextWindow"].(float64) != 131072 {
		t.Fatalf("cached model context = %+v", models[0])
	}
}

func TestMCPExecutesApprovedStdioServer(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "superscience.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tmp := t.TempDir()
	t.Setenv("SUPERSCIENCE_SERVER_MCP_FIXTURE", "1")
	t.Cleanup(func() { _ = mcpclient.CloseAll() })
	app := server.New(server.Options{Store: db, ProjectRoot: tmp})
	ts := httptest.NewServer(app.Handler())
	defer ts.Close()

	project := postJSON[store.Project](t, ts.URL+"/api/projects/open", map[string]string{"path": tmp})
	session := postJSON[store.Session](t, ts.URL+"/api/sessions", map[string]string{"projectId": project.ID})
	mcpServer := postJSON[map[string]any](t, ts.URL+"/api/mcp/servers", map[string]any{
		"name":      "echo",
		"transport": "stdio",
		"command":   os.Args[0],
		"args":      []string{"-test.run=^TestServerMCPFixtureProcess$"},
		"enabled":   true,
	})
	first := postJSON[map[string]any](t, ts.URL+"/api/mcp/servers/"+mcpServer["id"].(string)+"/tools/list/execute", map[string]any{
		"sessionId": session.ID,
		"input":     map[string]any{"path": "."},
	})
	if first["status"] != "pending_permission" {
		t.Fatalf("mcp was not gated: %+v", first)
	}
	permissions := getJSON[[]map[string]any](t, ts.URL+"/api/permissions")
	mcpPermissionID := ""
	for _, permission := range permissions {
		if permission["type"] == "mcp" {
			mcpPermissionID = permission["id"].(string)
			break
		}
	}
	if mcpPermissionID == "" {
		t.Fatalf("mcp permission not found: %+v", permissions)
	}
	_ = postJSON[map[string]bool](t, ts.URL+"/api/permissions/"+mcpPermissionID+"/respond", map[string]string{"decision": "once"})
	second := postJSON[map[string]any](t, ts.URL+"/api/mcp/servers/"+mcpServer["id"].(string)+"/tools/list/execute", map[string]any{
		"sessionId": session.ID,
		"input":     map[string]any{"path": "."},
	})
	if second["status"] != "ok" || second["output"] != "listed ." {
		t.Fatalf("mcp stdio output = %+v", second)
	}
	audit, err := db.ListAuditEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !hasAuditEvent(audit, "mcp.tool.execute", mcpServer["id"].(string)) {
		t.Fatalf("mcp tool execution audit missing: %+v", audit)
	}
}

func TestMCPDiscoveryIncludesResourcesAndPrompts(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "superscience.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	app := server.New(server.Options{Store: db, ProjectRoot: t.TempDir()})
	ts := httptest.NewServer(app.Handler())
	defer ts.Close()

	mcpAPI := newMCPFixtureHTTPServer(t)

	mcpServer := postJSON[map[string]any](t, ts.URL+"/api/mcp/servers", map[string]any{
		"name":      "capability-fixture",
		"transport": "http",
		"url":       mcpAPI.URL,
		"enabled":   true,
	})
	discovery := getJSON[map[string]any](t, ts.URL+"/api/mcp/servers/"+mcpServer["id"].(string)+"/discovery")
	if discovery["status"] != "ready" || len(discovery["tools"].([]any)) != 2 || len(discovery["resources"].([]any)) != 1 || len(discovery["prompts"].([]any)) != 1 {
		t.Fatalf("discovery = %+v", discovery)
	}
	resource := discovery["resources"].([]any)[0].(map[string]any)
	if resource["uri"] != "file:///dataset.csv" {
		t.Fatalf("resource = %+v", resource)
	}
}

func TestMCPDiscoveryCachesLiveCapabilitiesAndFallsBackWhenOffline(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "superscience.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	app := server.New(server.Options{Store: db, ProjectRoot: t.TempDir()})
	ts := httptest.NewServer(app.Handler())
	defer ts.Close()

	mcpAPI := newMCPFixtureHTTPServer(t)

	mcpServer := postJSON[map[string]any](t, ts.URL+"/api/mcp/servers", map[string]any{
		"name":      "cached-capability-fixture",
		"transport": "http",
		"url":       mcpAPI.URL,
		"enabled":   true,
	})
	discovery := getJSON[map[string]any](t, ts.URL+"/api/mcp/servers/"+mcpServer["id"].(string)+"/discovery")
	if discovery["status"] != "ready" || discovery["cached"] == true || len(discovery["tools"].([]any)) != 2 {
		t.Fatalf("live discovery = %+v", discovery)
	}
	_ = mcpclient.CloseServer(mcpServer["id"].(string))
	mcpAPI.Close()

	cached := getJSON[map[string]any](t, ts.URL+"/api/mcp/servers/"+mcpServer["id"].(string)+"/discovery")
	if cached["status"] != "degraded" || cached["cached"] != true || len(cached["resources"].([]any)) != 1 || len(cached["prompts"].([]any)) != 1 {
		t.Fatalf("cached discovery = %+v", cached)
	}
	audit, err := db.ListAuditEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var sawRefresh bool
	for _, event := range audit {
		if event.Action == "mcp.discovery.refresh" && event.EntityID == mcpServer["id"] {
			sawRefresh = true
			break
		}
	}
	if !sawRefresh {
		t.Fatalf("discovery refresh audit missing: %+v", audit)
	}
}

func TestMCPToolsEndpointReportsUnavailableWhenOffline(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "superscience.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	app := server.New(server.Options{Store: db, ProjectRoot: t.TempDir()})
	ts := httptest.NewServer(app.Handler())
	defer ts.Close()

	mcpAPI := newMCPFixtureHTTPServer(t)

	mcpServer := postJSON[map[string]any](t, ts.URL+"/api/mcp/servers", map[string]any{
		"name":      "cached-tools-fixture",
		"transport": "http",
		"url":       mcpAPI.URL,
		"enabled":   true,
	})
	live := getJSON[map[string]any](t, ts.URL+"/api/mcp/servers/"+mcpServer["id"].(string)+"/discovery")
	if len(live["tools"].([]any)) != 2 {
		t.Fatalf("live discovery = %+v", live)
	}
	_ = mcpclient.CloseServer(mcpServer["id"].(string))
	mcpAPI.Close()

	response, err := http.Get(ts.URL + "/api/mcp/servers/" + mcpServer["id"].(string) + "/tools")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var apiError map[string]any
	if err := json.NewDecoder(response.Body).Decode(&apiError); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusServiceUnavailable || apiError["code"] != "mcp_unavailable" {
		t.Fatalf("offline tools status=%d body=%+v", response.StatusCode, apiError)
	}
}

func TestMCPOAuthUsesPKCEExchangesCodeAndAuthenticatesTransport(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "superscience.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	secretStore := newMemorySecretStore()

	var oauthBaseURL string
	var tokenExchange map[string]string
	protectedMCP := mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server {
		return newMCPFixtureServer()
	}, nil)
	oauthMux := http.NewServeMux()
	oauthMux.HandleFunc("/.well-known/oauth-protected-resource/mcp", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"resource": oauthBaseURL + "/mcp", "authorization_servers": []string{oauthBaseURL}, "scopes_supported": []string{"mcp:tools"},
		})
	})
	oauthMux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": oauthBaseURL, "authorization_endpoint": oauthBaseURL + "/authorize", "token_endpoint": oauthBaseURL + "/token",
			"response_types_supported": []string{"code"}, "grant_types_supported": []string{"authorization_code", "refresh_token"},
			"code_challenge_methods_supported": []string{"S256"}, "token_endpoint_auth_methods_supported": []string{"none"},
		})
	})
	oauthMux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		tokenExchange = map[string]string{
			"grant_type": r.Form.Get("grant_type"), "code": r.Form.Get("code"), "code_verifier": r.Form.Get("code_verifier"),
			"client_id": r.Form.Get("client_id"), "resource": r.Form.Get("resource"),
		}
		if tokenExchange["code"] != "real-code" || tokenExchange["code_verifier"] == "" {
			writeJSONFixture(w, http.StatusBadRequest, map[string]any{"error": "invalid_grant"})
			return
		}
		writeJSONFixture(w, http.StatusOK, map[string]any{
			"access_token": "oauth-access-token", "token_type": "Bearer", "refresh_token": "oauth-refresh-token", "expires_in": 3600,
		})
	})
	oauthMux.Handle("/mcp", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer oauth-access-token" {
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+oauthBaseURL+`/.well-known/oauth-protected-resource/mcp"`)
			writeJSONFixture(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
			return
		}
		protectedMCP.ServeHTTP(w, r)
	}))
	oauthServer := httptest.NewServer(oauthMux)
	oauthBaseURL = oauthServer.URL
	defer oauthServer.Close()

	app := server.New(server.Options{Store: db, ProjectRoot: t.TempDir(), Secrets: secretStore})
	ts := httptest.NewServer(app.Handler())
	defer ts.Close()
	mcpServer := postJSON[map[string]any](t, ts.URL+"/api/mcp/servers", map[string]any{
		"name": "oauth-fixture", "transport": "streamable-http", "url": oauthBaseURL + "/mcp", "enabled": true,
	})
	start := postJSON[map[string]any](t, ts.URL+"/api/mcp/servers/"+mcpServer["id"].(string)+"/oauth/start", map[string]any{
		"clientId": "superscience-public-client", "scopes": []string{"mcp:tools"},
	})
	authorizationURL, err := url.Parse(start["authorizationUrl"].(string))
	if err != nil {
		t.Fatal(err)
	}
	query := authorizationURL.Query()
	if query.Get("code_challenge_method") != "S256" || query.Get("code_challenge") == "" || query.Get("state") == "" || query.Get("resource") != oauthBaseURL+"/mcp" {
		t.Fatalf("authorization URL = %s", authorizationURL.String())
	}
	callback := getJSON[map[string]any](t, ts.URL+"/api/mcp/oauth/callback?state="+url.QueryEscape(query.Get("state"))+"&code=real-code")
	if callback["ok"] != true || callback["status"] != "authorized" {
		t.Fatalf("callback = %+v", callback)
	}
	if tokenExchange["grant_type"] != "authorization_code" || tokenExchange["client_id"] != "superscience-public-client" || tokenExchange["resource"] != oauthBaseURL+"/mcp" {
		t.Fatalf("token exchange = %+v", tokenExchange)
	}
	discovery := getJSON[map[string]any](t, ts.URL+"/api/mcp/servers/"+mcpServer["id"].(string)+"/discovery")
	if discovery["status"] != "ready" || len(discovery["tools"].([]any)) != 2 {
		t.Fatalf("authenticated discovery = %+v", discovery)
	}
	removed := postJSON[map[string]any](t, ts.URL+"/api/mcp/servers/"+mcpServer["id"].(string)+"/oauth/remove", map[string]any{})
	if removed["removed"] != true {
		t.Fatalf("oauth remove = %+v", removed)
	}
	stored, err := db.GetMCPServer(ctx, mcpServer["id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if stored.OAuthSecretRef != "" || stored.OAuthStatus != "" {
		t.Fatalf("oauth metadata after remove = %+v", stored)
	}
}

func TestMCPResourceAndPromptAccessRequireApproval(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "superscience.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	projectRoot := t.TempDir()
	app := server.New(server.Options{Store: db, ProjectRoot: projectRoot})
	ts := httptest.NewServer(app.Handler())
	defer ts.Close()

	mcpAPI := newMCPFixtureHTTPServer(t)

	project := postJSON[store.Project](t, ts.URL+"/api/projects/open", map[string]string{"path": projectRoot})
	session := postJSON[store.Session](t, ts.URL+"/api/sessions", map[string]string{"projectId": project.ID})
	mcpServer := postJSON[map[string]any](t, ts.URL+"/api/mcp/servers", map[string]any{
		"name":      "resource-prompt-fixture",
		"transport": "http",
		"url":       mcpAPI.URL,
		"enabled":   true,
	})

	resourceAttempt := postJSON[map[string]any](t, ts.URL+"/api/mcp/servers/"+mcpServer["id"].(string)+"/resources/read", map[string]any{
		"sessionId": session.ID,
		"uri":       "file:///dataset.csv",
	})
	if resourceAttempt["status"] != "pending_permission" || resourceAttempt["permissionId"] == "" {
		t.Fatalf("resource attempt = %+v", resourceAttempt)
	}
	_ = postJSON[map[string]bool](t, ts.URL+"/api/permissions/"+resourceAttempt["permissionId"].(string)+"/respond", map[string]string{"decision": "once"})
	resource := postJSON[map[string]any](t, ts.URL+"/api/mcp/servers/"+mcpServer["id"].(string)+"/resources/read", map[string]any{
		"sessionId": session.ID,
		"uri":       "file:///dataset.csv",
	})
	if resource["status"] != "ok" || !strings.Contains(resource["text"].(string), "EGFR") {
		t.Fatalf("resource = %+v", resource)
	}

	promptAttempt := postJSON[map[string]any](t, ts.URL+"/api/mcp/servers/"+mcpServer["id"].(string)+"/prompts/get", map[string]any{
		"sessionId": session.ID,
		"name":      "summarize",
		"arguments": map[string]any{"topic": "EGFR"},
	})
	if promptAttempt["status"] != "pending_permission" || promptAttempt["permissionId"] == "" {
		t.Fatalf("prompt attempt = %+v", promptAttempt)
	}
	_ = postJSON[map[string]bool](t, ts.URL+"/api/permissions/"+promptAttempt["permissionId"].(string)+"/respond", map[string]string{"decision": "once"})
	prompt := postJSON[map[string]any](t, ts.URL+"/api/mcp/servers/"+mcpServer["id"].(string)+"/prompts/get", map[string]any{
		"sessionId": session.ID,
		"name":      "summarize",
		"arguments": map[string]any{"topic": "EGFR"},
	})
	if prompt["status"] != "ok" || !strings.Contains(prompt["text"].(string), "EGFR evidence") {
		t.Fatalf("prompt = %+v", prompt)
	}
	audit, err := db.ListAuditEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !hasAuditEvent(audit, "mcp.resource.read", mcpServer["id"].(string)) {
		t.Fatalf("mcp resource audit missing: %+v", audit)
	}
	if !hasAuditEvent(audit, "mcp.prompt.get", mcpServer["id"].(string)) {
		t.Fatalf("mcp prompt audit missing: %+v", audit)
	}
}

func TestSkillLoadReturnsPromptContent(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "superscience.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	app := server.New(server.Options{Store: db, ProjectRoot: t.TempDir()})
	ts := httptest.NewServer(app.Handler())
	defer ts.Close()

	loaded := postJSON[map[string]any](t, ts.URL+"/api/skills/load", map[string]string{"name": "research-protocol"})
	if loaded["name"] != "research-protocol" || !strings.Contains(loaded["content"].(string), "provenance") {
		t.Fatalf("skill load = %+v", loaded)
	}
}

func TestProjectSkillDirectoryIsDiscoveredLoadedAndUsedByRuntime(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "superscience.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	projectRoot := t.TempDir()
	skillDir := filepath.Join(projectRoot, ".superscience", "skills", "project-evidence")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	skillPath := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte("---\nname: project-evidence\ndescription: Project-only evidence skill.\n---\n# Project Evidence\nUse project-only evidence rules before generic reasoning.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	app := server.New(server.Options{Store: db, ProjectRoot: projectRoot, ProviderFactory: serverTestProviderFactory})
	ts := httptest.NewServer(app.Handler())
	defer ts.Close()

	listed := getJSON[[]store.SkillRecord](t, ts.URL+"/api/skills")
	var discovered store.SkillRecord
	for _, skill := range listed {
		if skill.Name == "project-evidence" {
			discovered = skill
			break
		}
	}
	if discovered.Name == "" || discovered.Source != "project" || discovered.Path != skillPath {
		t.Fatalf("project skill was not discovered from .superscience/skills: %+v", listed)
	}

	loaded := postJSON[map[string]any](t, ts.URL+"/api/skills/load", map[string]string{"name": "project-evidence"})
	if loaded["name"] != "project-evidence" || !strings.Contains(loaded["content"].(string), "project-only evidence rules") {
		t.Fatalf("project skill load = %+v", loaded)
	}

	project := postJSON[store.Project](t, ts.URL+"/api/projects/open", map[string]string{"path": projectRoot})
	session := postJSON[store.Session](t, ts.URL+"/api/sessions", map[string]string{"projectId": project.ID})
	run := postJSON[map[string]any](t, ts.URL+"/api/sessions/"+session.ID+"/run", map[string]any{
		"goal":     "/project-evidence inspect EGFR provenance",
		"agent":    "research",
		"provider": "mock",
		"maxSteps": 1,
	})
	runID, _ := run["id"].(string)
	if runID == "" || (run["status"] != "completed" && run["status"] != "pending_permission") {
		t.Fatalf("run = %+v", run)
	}
	steps := getJSON[[]map[string]any](t, ts.URL+"/api/runs/"+runID+"/steps")
	var sawSkillStep bool
	for _, step := range steps {
		if step["kind"] == "skill" && step["title"] == "project-evidence" && strings.Contains(step["content"].(string), "project-only evidence rules") {
			sawSkillStep = true
		}
	}
	if !sawSkillStep {
		t.Fatalf("runtime did not load project skill first: %+v", steps)
	}
	message, _ := run["message"].(map[string]any)
	messageID, _ := message["id"].(string)
	parts, err := db.ListMessageParts(ctx, messageID)
	if err != nil {
		t.Fatal(err)
	}
	foundSkillPart := false
	for _, part := range parts {
		if part.Type == "skill" && part.Title == "project-evidence" && strings.Contains(part.Content, "project-only evidence rules") {
			foundSkillPart = true
		}
	}
	if !foundSkillPart {
		t.Fatalf("project skill message part missing: %+v", parts)
	}
}

func TestUserSkillDirectoryIsDiscoveredLoadedAndUsedByRuntime(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "superscience.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	dataDir := t.TempDir()
	t.Setenv("SUPERSCIENCE_DATA_DIR", dataDir)
	skillDir := filepath.Join(dataDir, "skills", "user-evidence")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	skillPath := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte("---\nname: user-evidence\ndescription: User evidence skill.\n---\n# User Evidence\nUse user-level evidence rules before generic reasoning.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	app := server.New(server.Options{Store: db, ProjectRoot: t.TempDir(), ProviderFactory: serverTestProviderFactory})
	ts := httptest.NewServer(app.Handler())
	defer ts.Close()

	listed := getJSON[[]store.SkillRecord](t, ts.URL+"/api/skills")
	var discovered store.SkillRecord
	for _, skill := range listed {
		if skill.Name == "user-evidence" {
			discovered = skill
			break
		}
	}
	if discovered.Name == "" || discovered.Source != "user" || discovered.Path != skillPath {
		t.Fatalf("user skill was not discovered from data dir: %+v", listed)
	}

	loaded := postJSON[map[string]any](t, ts.URL+"/api/skills/load", map[string]string{"name": "user-evidence"})
	if loaded["name"] != "user-evidence" || !strings.Contains(loaded["content"].(string), "user-level evidence rules") {
		t.Fatalf("user skill load = %+v", loaded)
	}

	project := postJSON[store.Project](t, ts.URL+"/api/projects/open", map[string]string{"path": t.TempDir()})
	session := postJSON[store.Session](t, ts.URL+"/api/sessions", map[string]string{"projectId": project.ID})
	run := postJSON[map[string]any](t, ts.URL+"/api/sessions/"+session.ID+"/run", map[string]any{
		"goal":     "/user-evidence inspect EGFR provenance",
		"agent":    "research",
		"provider": "mock",
		"maxSteps": 1,
	})
	runID, _ := run["id"].(string)
	if runID == "" || (run["status"] != "completed" && run["status"] != "pending_permission") {
		t.Fatalf("run = %+v", run)
	}
	steps := getJSON[[]map[string]any](t, ts.URL+"/api/runs/"+runID+"/steps")
	var sawSkillStep bool
	for _, step := range steps {
		if step["kind"] == "skill" && step["title"] == "user-evidence" && strings.Contains(step["content"].(string), "user-level evidence rules") {
			sawSkillStep = true
		}
	}
	if !sawSkillStep {
		t.Fatalf("runtime did not load user skill first: %+v", steps)
	}
}

func TestSkillInstallFromURLCachesAndLoadsRemoteSkillContent(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "superscience.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	projectRoot := t.TempDir()
	app := server.New(server.Options{Store: db, ProjectRoot: projectRoot})
	ts := httptest.NewServer(app.Handler())
	defer ts.Close()

	remoteSkill := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/markdown")
		_, _ = w.Write([]byte("---\nname: remote-kinase-review\ndescription: Remote kinase review skill.\n---\n# Remote Kinase\nUse remote evidence rules.\n"))
	}))
	defer remoteSkill.Close()

	installed := postJSON[map[string]any](t, ts.URL+"/api/skills/install", map[string]string{"url": remoteSkill.URL + "/SKILL.md"})
	if installed["name"] != "remote-kinase-review" || installed["source"] != "remote" {
		t.Fatalf("installed remote skill = %+v", installed)
	}
	path, _ := installed["path"].(string)
	if path == "" || strings.HasPrefix(path, "http") {
		t.Fatalf("remote skill was not cached to a local file: %+v", installed)
	}
	if !strings.HasPrefix(path, filepath.Join(projectRoot, ".superscience", "skills-cache")) {
		t.Fatalf("remote skill cache escaped project root: %s", path)
	}

	loaded := postJSON[map[string]any](t, ts.URL+"/api/skills/load", map[string]string{"name": "remote-kinase-review"})
	if loaded["name"] != "remote-kinase-review" || !strings.Contains(loaded["content"].(string), "Use remote evidence rules.") {
		t.Fatalf("loaded remote skill = %+v", loaded)
	}
	audit, err := db.ListAuditEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !hasAuditEvent(audit, "skill.install.remote", "remote-kinase-review") {
		t.Fatalf("remote skill install audit missing: %+v", audit)
	}
}

func TestDisabledSkillCannotBeLoaded(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "superscience.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	projectRoot := t.TempDir()
	app := server.New(server.Options{Store: db, ProjectRoot: projectRoot})
	ts := httptest.NewServer(app.Handler())
	defer ts.Close()

	skillDir := filepath.Join(projectRoot, "skills", "toggle")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	skillPath := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte("---\nname: toggle-skill\ndescription: Toggle skill.\n---\n# Toggle\nLoad me only when enabled.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	installed := postJSON[store.SkillRecord](t, ts.URL+"/api/skills/install", map[string]string{"path": skillPath})
	installed.Enabled = false
	disabled := patchJSON[store.SkillRecord](t, ts.URL+"/api/skills/"+installed.ID, installed)
	if disabled.Enabled {
		t.Fatalf("skill was not disabled: %+v", disabled)
	}

	body, _ := json.Marshal(map[string]string{"name": "toggle-skill"})
	resp, err := http.Post(ts.URL+"/api/skills/load", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("disabled skill load returned 200")
	}
}

func TestWorkspaceStateAPIsExposeLSPNotebookExportAndShareReplay(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "superscience.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	projectRoot := t.TempDir()
	app := server.New(server.Options{Store: db, ProjectRoot: projectRoot})
	ts := httptest.NewServer(app.Handler())
	defer ts.Close()

	lspStatus, lspError := postJSONResponse(t, ts.URL+"/api/lsp/sessions", map[string]any{
		"language": "unsupported-science-language",
		"path":     "analysis.science",
		"text":     "EGFR",
	})
	if lspStatus != http.StatusServiceUnavailable || !strings.Contains(fmt.Sprint(lspError["message"]), "language server") {
		t.Fatalf("unavailable lsp status=%d error=%+v", lspStatus, lspError)
	}
	lspSessions := getJSON[[]map[string]any](t, ts.URL+"/api/lsp/sessions")
	if len(lspSessions) != 0 {
		t.Fatalf("unavailable lsp session was retained: %+v", lspSessions)
	}

	notebook := postJSON[map[string]any](t, ts.URL+"/api/notebooks", map[string]any{
		"title": "Export notebook",
		"path":  "analysis/export.ipynb",
		"cells": []map[string]any{{"kind": "code", "language": "python", "source": "print('export')"}},
	})
	exported := getJSON[map[string]any](t, ts.URL+"/api/notebooks/"+notebook["id"].(string)+"/export")
	if exported["nbformat"].(float64) != 4 || len(exported["cells"].([]any)) != 1 {
		t.Fatalf("notebook export = %+v", exported)
	}

	artifact := postJSON[map[string]any](t, ts.URL+"/api/artifacts", map[string]any{
		"type":    "markdown",
		"title":   "Replay artifact",
		"content": "# Replay\n",
	})
	share := postJSON[map[string]any](t, ts.URL+"/api/share/publish", map[string]any{
		"title":       "Replay share",
		"artifactIds": []string{artifact["id"].(string)},
	})
	replay := getJSON[map[string]any](t, ts.URL+"/api/share/"+share["id"].(string)+"/replay")
	if replay["share"].(map[string]any)["id"] != share["id"] || len(replay["artifacts"].([]any)) != 1 || len(replay["provenance"].([]any)) == 0 {
		t.Fatalf("share replay = %+v", replay)
	}
}

func TestLSPSessionCanUseExternalServerCommand(t *testing.T) {
	if os.Getenv("SUPERSCIENCE_SERVER_FAKE_LSP") == "1" {
		runServerFakeLSP()
		os.Exit(0)
	}

	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "superscience.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	app := server.New(server.Options{Store: db, ProjectRoot: t.TempDir()})
	ts := httptest.NewServer(app.Handler())
	defer ts.Close()

	session := postJSON[map[string]any](t, ts.URL+"/api/lsp/sessions", map[string]any{
		"language": "go",
		"path":     "/tmp/superscience/main.go",
		"text":     "package main\nfunc main() {}\n",
		"command":  os.Args[0],
		"args":     []string{"-test.run=TestLSPSessionCanUseExternalServerCommand"},
		"env":      []string{"SUPERSCIENCE_SERVER_FAKE_LSP=1"},
	})
	diagnostics, _ := session["diagnostics"].([]any)
	if len(diagnostics) != 1 || diagnostics[0].(map[string]any)["message"] != "server fake diagnostic" {
		t.Fatalf("external lsp diagnostics = %+v", session)
	}
	if session["hover"] != "server fake hover" {
		t.Fatalf("external lsp hover = %+v", session)
	}
}

func runServerFakeLSP() {
	reader := bufio.NewReader(os.Stdin)
	for {
		message, err := readServerLSPMessage(reader)
		if err != nil {
			return
		}
		var request struct {
			ID     any             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		_ = json.Unmarshal(message, &request)
		switch request.Method {
		case "initialize":
			writeServerLSPMessage(map[string]any{
				"jsonrpc": "2.0",
				"id":      request.ID,
				"result": map[string]any{"capabilities": map[string]any{
					"textDocumentSync": 1,
					"hoverProvider":    true,
				}},
			})
		case "textDocument/didOpen":
			var params struct {
				TextDocument struct {
					URI string `json:"uri"`
				} `json:"textDocument"`
			}
			_ = json.Unmarshal(request.Params, &params)
			writeServerLSPMessage(map[string]any{
				"jsonrpc": "2.0",
				"method":  "textDocument/publishDiagnostics",
				"params": map[string]any{
					"uri": params.TextDocument.URI,
					"diagnostics": []map[string]any{{
						"range":    map[string]any{"start": map[string]any{"line": 1, "character": 0}},
						"severity": 2,
						"message":  "server fake diagnostic",
					}},
				},
			})
		case "textDocument/hover":
			writeServerLSPMessage(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": map[string]any{"contents": map[string]any{"value": "server fake hover"}}})
		case "textDocument/completion":
			writeServerLSPMessage(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": map[string]any{"items": []map[string]any{{"label": "serverCompletion"}}}})
		case "textDocument/definition":
			writeServerLSPMessage(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": []map[string]any{{
				"uri":   "file://" + (&url.URL{Path: "/tmp/superscience/main.go"}).EscapedPath(),
				"range": map[string]any{"start": map[string]any{"line": 1, "character": 0}},
			}}})
		case "shutdown":
			writeServerLSPMessage(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": nil})
		case "exit":
			return
		}
	}
}

func readServerLSPMessage(reader *bufio.Reader) ([]byte, error) {
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
	if contentLength == 0 {
		return nil, io.ErrUnexpectedEOF
	}
	data := make([]byte, contentLength)
	_, err := io.ReadFull(reader, data)
	return data, err
}

func writeServerLSPMessage(payload any) {
	data, _ := json.Marshal(payload)
	_, _ = fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n%s", len(data), data)
}

func TestServerContractFreezeAndParityAPIs(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "superscience.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	projectRoot := t.TempDir()
	app := server.New(server.Options{Store: db, ProjectRoot: projectRoot, ProviderFactory: serverTestProviderFactory})
	ts := httptest.NewServer(app.Handler())
	defer ts.Close()

	openapi := getJSON[map[string]any](t, ts.URL+"/api/openapi.json")
	if openapi["openapi"] != "3.1.0" {
		t.Fatalf("openapi version = %+v", openapi["openapi"])
	}
	paths := openapi["paths"].(map[string]any)
	for _, want := range []string{"/api/sessions/{id}/run", "/api/runs/{id}/events", "/api/plugins", "/api/plugins/{id}/tools", "/api/plugins/{id}/tools/{tool}/execute", "/api/notebooks/import", "/api/mcp/servers/{id}/discovery", "/api/mcp/servers/{id}/resources/read", "/api/mcp/servers/{id}/prompts/get"} {
		if _, ok := paths[want]; !ok {
			t.Fatalf("openapi missing %s in %+v", want, paths)
		}
	}

	project := postJSON[store.Project](t, ts.URL+"/api/projects/open", map[string]string{"path": projectRoot})
	session := postJSON[store.Session](t, ts.URL+"/api/sessions", map[string]string{"projectId": project.ID, "agent": "biology"})
	run := postJSON[map[string]any](t, ts.URL+"/api/sessions/"+session.ID+"/run", map[string]string{
		"goal":  "Create a compact EGFR research trace",
		"agent": "biology",
		"model": "mock",
	})
	runID := run["id"].(string)
	events := getJSON[[]map[string]any](t, ts.URL+"/api/runs/"+runID+"/events")
	var sawStep, sawTool bool
	for _, event := range events {
		if event["type"] == "agent.step" {
			sawStep = true
		}
		if event["type"] == "tool.call" {
			sawTool = true
		}
	}
	if !sawStep || !sawTool {
		t.Fatalf("run events missing step/tool: %+v", events)
	}
	retry := postJSON[map[string]any](t, ts.URL+"/api/runs/"+runID+"/retry", map[string]string{"reason": "contract test"})
	if retry["id"] == "" || retry["id"] == runID || retry["status"] == "" {
		t.Fatalf("retry = %+v", retry)
	}
	compacted := postJSON[map[string]any](t, ts.URL+"/api/runs/"+runID+"/compact", map[string]any{})
	if compacted["status"] != "compacted" || compacted["artifactId"] == "" {
		t.Fatalf("compact = %+v", compacted)
	}

	skillDir := filepath.Join(projectRoot, ".superscience", "skills", "kinase")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	skillPath := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte("---\nname: kinase-evidence\ndescription: Review kinase evidence.\n---\n# Kinase\nUse structured evidence.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	validation := postJSON[map[string]any](t, ts.URL+"/api/skills/validate", map[string]string{"path": skillPath})
	if validation["ok"] != true || validation["name"] != "kinase-evidence" {
		t.Fatalf("skill validation = %+v", validation)
	}
	installed := postJSON[map[string]any](t, ts.URL+"/api/skills/install", map[string]string{"path": skillPath})
	if installed["name"] != "kinase-evidence" || installed["enabled"] != true {
		t.Fatalf("skill install = %+v", installed)
	}

	mcpServer := postJSON[map[string]any](t, ts.URL+"/api/mcp/servers", map[string]any{
		"name":      "oauth-mcp",
		"transport": "streamable-http",
		"url":       "http://127.0.0.1:9999/mcp",
		"enabled":   true,
	})
	oauthStatus, oauthError := postJSONResponse(t, ts.URL+"/api/mcp/servers/"+mcpServer["id"].(string)+"/oauth/start", map[string]any{})
	if oauthStatus != http.StatusBadRequest || !strings.Contains(oauthError["message"].(string), "clientId") {
		t.Fatalf("oauth prerequisites status=%d body=%+v", oauthStatus, oauthError)
	}

	plugin := postJSON[map[string]any](t, ts.URL+"/api/plugins", map[string]any{
		"id":          "lab-plugin",
		"name":        "Lab Plugin",
		"version":     "0.1.0",
		"description": "Registers local lab tools.",
		"runtime":     "subprocess",
		"command":     "superscience-lab-plugin",
		"enabled":     true,
		"manifest":    map[string]any{"tools": []string{"lab_search"}, "providers": []string{"lab-router"}},
	})
	if plugin["id"] != "lab-plugin" || plugin["status"] == "" {
		t.Fatalf("plugin = %+v", plugin)
	}
	plugins := getJSON[[]map[string]any](t, ts.URL+"/api/plugins")
	if len(plugins) != 1 || plugins[0]["id"] != "lab-plugin" {
		t.Fatalf("plugins = %+v", plugins)
	}
	reqBody, _ := json.Marshal(map[string]any{"enabled": false, "status": "disabled"})
	req, err := http.NewRequest(http.MethodPatch, ts.URL+"/api/plugins/lab-plugin", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch plugin status = %d", resp.StatusCode)
	}

	t.Setenv("SUPERSCIENCE_SERVER_PLUGIN_FIXTURE", "1")
	t.Cleanup(func() { _ = pluginruntime.CloseAll() })
	executablePlugin := postJSON[map[string]any](t, ts.URL+"/api/plugins", map[string]any{
		"id":       "lab-exec-plugin",
		"name":     "Lab Exec Plugin",
		"runtime":  "subprocess",
		"command":  os.Args[0],
		"args":     []string{"-test.run=^TestServerPluginFixtureProcess$"},
		"enabled":  true,
		"manifest": map[string]any{"tools": []map[string]any{{"name": "lab_search", "description": "Search lab records", "inputSchema": map[string]any{"type": "object"}}}},
	})
	pluginTools := getJSON[[]map[string]any](t, ts.URL+"/api/plugins/"+executablePlugin["id"].(string)+"/tools")
	if len(pluginTools) != 1 || pluginTools[0]["name"] != "lab_search" {
		t.Fatalf("plugin tools = %+v", pluginTools)
	}
	pluginAttempt := postJSON[map[string]any](t, ts.URL+"/api/plugins/"+executablePlugin["id"].(string)+"/tools/lab_search/execute", map[string]any{
		"sessionId": session.ID,
		"input":     map[string]any{"query": "EGFR"},
	})
	if pluginAttempt["status"] != "pending_permission" || pluginAttempt["permissionId"] == "" {
		t.Fatalf("plugin attempt = %+v", pluginAttempt)
	}
	_ = postJSON[map[string]bool](t, ts.URL+"/api/permissions/"+pluginAttempt["permissionId"].(string)+"/respond", map[string]string{"decision": "once"})
	pluginResult := postJSON[map[string]any](t, ts.URL+"/api/plugins/"+executablePlugin["id"].(string)+"/tools/lab_search/execute", map[string]any{
		"sessionId": session.ID,
		"input":     map[string]any{"query": "EGFR"},
	})
	if pluginResult["status"] != "ok" || !strings.Contains(pluginResult["output"].(string), "plugin searched EGFR") {
		t.Fatalf("plugin result = %+v", pluginResult)
	}

	imported := postJSON[map[string]any](t, ts.URL+"/api/notebooks/import", map[string]any{
		"path": "analysis/imported.ipynb",
		"notebook": map[string]any{
			"metadata": map[string]any{"name": "Imported notebook"},
			"cells": []map[string]any{
				{"cell_type": "markdown", "source": []string{"# Imported\n"}},
				{"cell_type": "code", "source": []string{"print('cancel me')\n"}, "metadata": map[string]any{"language": "python"}},
			},
		},
	})
	importedCells := imported["cells"].([]any)
	if len(importedCells) != 2 {
		t.Fatalf("imported notebook = %+v", imported)
	}
	importedCell := importedCells[1].(map[string]any)
	cancelStatus, cancelError := postJSONResponse(t, ts.URL+"/api/notebooks/"+imported["id"].(string)+"/cells/"+importedCell["id"].(string)+"/cancel", map[string]any{})
	if cancelStatus != http.StatusConflict || !strings.Contains(fmt.Sprint(cancelError["message"]), "not running") {
		t.Fatalf("idle kernel cancel status=%d body=%+v", cancelStatus, cancelError)
	}

	lspStatus, lspError := postJSONResponse(t, ts.URL+"/api/lsp/sessions", map[string]any{
		"language": "unsupported-contract-language",
		"path":     "src/example.contract",
		"text":     "EGFR",
	})
	if lspStatus != http.StatusServiceUnavailable || lspError["requestId"] == "" {
		t.Fatalf("lsp unavailable response status=%d body=%+v", lspStatus, lspError)
	}
}

func TestServerEcosystemAPIs(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "superscience.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	projectRoot := t.TempDir()
	app := server.New(server.Options{Store: db, ProjectRoot: projectRoot})
	ts := httptest.NewServer(app.Handler())
	defer ts.Close()

	providers := getJSON[[]map[string]any](t, ts.URL+"/api/providers")
	providerIDs := map[string]bool{}
	for _, provider := range providers {
		providerIDs[provider["id"].(string)] = true
	}
	for _, want := range []string{"openai", "anthropic", "gemini", "openrouter", "ollama", "bedrock", "dashscope"} {
		if !providerIDs[want] {
			t.Fatalf("missing provider %q in %+v", want, providerIDs)
		}
	}

	customProvider := postJSON[map[string]any](t, ts.URL+"/api/providers", map[string]any{
		"id":          "lab-router",
		"kind":        "openai-compatible",
		"displayName": "Lab Router",
		"baseURL":     "http://127.0.0.1:11434/v1",
		"enabled":     true,
	})
	if customProvider["secretRef"] != "" {
		t.Fatalf("provider metadata leaked secret ref: %+v", customProvider)
	}
	models := getJSON[[]map[string]any](t, ts.URL+"/api/providers/openai/models")
	if len(models) == 0 || models[0]["id"] == "" {
		t.Fatalf("models = %+v", models)
	}

	agents := getJSON[[]map[string]any](t, ts.URL+"/api/agents")
	agentIDs := map[string]bool{}
	for _, agent := range agents {
		agentIDs[agent["id"].(string)] = true
	}
	for _, want := range []string{"research", "biology", "physics", "ml", "literature-review", "critic", "plan"} {
		if !agentIDs[want] {
			t.Fatalf("missing agent %q in %+v", want, agentIDs)
		}
	}

	tools := getJSON[[]map[string]any](t, ts.URL+"/api/tools")
	toolIDs := map[string]bool{}
	for _, tool := range tools {
		toolIDs[tool["id"].(string)] = true
	}
	for _, want := range []string{"read", "write", "bash", "science_search", "notebook", "lsp", "mcp", "mcp_resource", "mcp_prompt", "plugin", "artifact"} {
		if !toolIDs[want] {
			t.Fatalf("missing tool %q in %+v", want, toolIDs)
		}
	}

	skills := getJSON[[]map[string]any](t, ts.URL+"/api/skills")
	if len(skills) == 0 {
		t.Fatal("expected bundled skills")
	}
	createdSkill := postJSON[map[string]any](t, ts.URL+"/api/skills", map[string]any{
		"name":        "kinase-review",
		"description": "Review kinase assay evidence",
		"source":      "project",
		"enabled":     true,
	})
	if createdSkill["name"] != "kinase-review" {
		t.Fatalf("created skill = %+v", createdSkill)
	}

	mcpServer := postJSON[map[string]any](t, ts.URL+"/api/mcp/servers", map[string]any{
		"name":      "local-files",
		"transport": "stdio",
		"command":   "superscience-mcp-files",
		"enabled":   true,
	})
	if mcpServer["transport"] != "stdio" || mcpServer["status"] == "" {
		t.Fatalf("mcp server = %+v", mcpServer)
	}
	execution := postJSON[map[string]any](t, ts.URL+"/api/mcp/servers/"+mcpServer["id"].(string)+"/tools/list/execute", map[string]any{
		"input": map[string]any{"path": "."},
	})
	if execution["status"] != "pending_permission" {
		t.Fatalf("expected MCP execution to be permission gated: %+v", execution)
	}

	notebook := postJSON[map[string]any](t, ts.URL+"/api/notebooks", map[string]any{
		"title": "EGFR analysis",
		"path":  "analysis/egfr.ipynb",
		"cells": []map[string]any{{"kind": "code", "language": "python", "source": "print('egfr')"}},
	})
	cells := notebook["cells"].([]any)
	cell := cells[0].(map[string]any)
	output := postJSON[map[string]any](t, ts.URL+"/api/notebooks/"+notebook["id"].(string)+"/cells/"+cell["id"].(string)+"/run", map[string]any{})
	if output["status"] != "ok" || !strings.Contains(output["stdout"].(string), "egfr") {
		t.Fatalf("notebook output = %+v", output)
	}

	lspStatus, lspError := postJSONResponse(t, ts.URL+"/api/lsp/sessions", map[string]any{
		"language": "unsupported-ecosystem-language",
		"path":     "internal/example.unknown",
		"text":     "not analyzed without a server",
	})
	if lspStatus != http.StatusServiceUnavailable || lspError["message"] == "" {
		t.Fatalf("lsp unavailable response status=%d body=%+v", lspStatus, lspError)
	}

	artifact := postJSON[map[string]any](t, ts.URL+"/api/artifacts", map[string]any{
		"sessionId": "session-test",
		"type":      "markdown",
		"title":     "EGFR finding",
		"content":   "# EGFR\n",
	})
	share := postJSON[map[string]any](t, ts.URL+"/api/share/publish", map[string]any{
		"title":       "EGFR share",
		"artifactIds": []string{artifact["id"].(string)},
		"visibility":  "private-link",
	})
	if share["url"] == "" || share["status"] != "published" {
		t.Fatalf("share = %+v", share)
	}
	loadedShare := getJSON[map[string]any](t, ts.URL+"/api/share/"+share["id"].(string))
	if loadedShare["id"] != share["id"] || loadedShare["bundle"] == "" {
		t.Fatalf("loaded share = %+v", loadedShare)
	}
	provenance := getJSON[[]map[string]any](t, ts.URL+"/api/provenance/"+artifact["id"].(string))
	if len(provenance) == 0 {
		t.Fatalf("expected provenance for artifact %s", artifact["id"])
	}
}

func TestServerCustomAgentCRUD(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "superscience.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ts := httptest.NewServer(server.New(server.Options{Store: db, ProjectRoot: t.TempDir()}).Handler())
	defer ts.Close()

	created := postJSON[map[string]any](t, ts.URL+"/api/agents", map[string]any{
		"id": "chemistry-review", "name": "Chemistry Review", "domain": "chemistry",
		"description": "Reviews chemistry evidence.", "systemPrompt": "Require structure-backed evidence.",
		"tools": []string{"read", "grep"}, "reviewMode": "required", "enabled": true,
	})
	if created["id"] != "chemistry-review" || created["builtIn"] != false {
		t.Fatalf("created agent = %+v", created)
	}
	loaded := getJSON[map[string]any](t, ts.URL+"/api/agents/chemistry-review")
	if loaded["systemPrompt"] != "Require structure-backed evidence." {
		t.Fatalf("loaded agent = %+v", loaded)
	}
	updated := patchJSON[map[string]any](t, ts.URL+"/api/agents/chemistry-review", map[string]any{"enabled": false, "reviewMode": "optional"})
	if updated["enabled"] != false || updated["reviewMode"] != "optional" {
		t.Fatalf("updated agent = %+v", updated)
	}

	req, err := http.NewRequest(http.MethodDelete, ts.URL+"/api/agents/chemistry-review", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete agent = %d", resp.StatusCode)
	}
	status, body := postJSONResponse(t, ts.URL+"/api/agents", map[string]any{"id": "research", "name": "Override", "domain": "general", "description": "No", "tools": []string{"read"}})
	if status != http.StatusConflict || body["code"] != "conflict" {
		t.Fatalf("built-in override status=%d body=%+v", status, body)
	}
}

func newMCPFixtureHTTPServer(t *testing.T) *httptest.Server {
	t.Helper()
	fixture := newMCPFixtureServer()
	httpServer := httptest.NewServer(mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server {
		return fixture
	}, nil))
	t.Cleanup(func() {
		_ = mcpclient.CloseAll()
		httpServer.Close()
	})
	return httpServer
}

func newMCPFixtureServer() *mcpsdk.Server {
	fixture := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "superscience-server-test-mcp", Version: "1.0.0"}, nil)
	fixture.AddTool(&mcpsdk.Tool{
		Name: "list",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"path": map[string]any{"type": "string"}},
		},
	}, func(_ context.Context, request *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		var input struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(request.Params.Arguments, &input); err != nil {
			return nil, err
		}
		return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "listed " + input.Path}}}, nil
	})
	fixture.AddTool(&mcpsdk.Tool{
		Name:        "lookup",
		Description: "Lookup a lab record.",
		InputSchema: map[string]any{"type": "object"},
	}, func(context.Context, *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "lookup complete"}}}, nil
	})
	fixture.AddResource(&mcpsdk.Resource{
		URI: "file:///dataset.csv", Name: "Dataset", MIMEType: "text/csv", Description: "Experiment metadata",
	}, func(_ context.Context, request *mcpsdk.ReadResourceRequest) (*mcpsdk.ReadResourceResult, error) {
		return &mcpsdk.ReadResourceResult{Contents: []*mcpsdk.ResourceContents{{
			URI: request.Params.URI, MIMEType: "text/csv", Text: "gene,score\nEGFR,0.91",
		}}}, nil
	})
	fixture.AddPrompt(&mcpsdk.Prompt{
		Name: "summarize", Description: "Summarize selected records", Arguments: []*mcpsdk.PromptArgument{{Name: "topic", Required: true}},
	}, func(_ context.Context, request *mcpsdk.GetPromptRequest) (*mcpsdk.GetPromptResult, error) {
		return &mcpsdk.GetPromptResult{
			Description: "Summarize selected records",
			Messages: []*mcpsdk.PromptMessage{{
				Role: "user", Content: &mcpsdk.TextContent{Text: "Summarize " + request.Params.Arguments["topic"] + " evidence"},
			}},
		}, nil
	})
	return fixture
}

func TestServerMCPFixtureProcess(t *testing.T) {
	if os.Getenv("SUPERSCIENCE_SERVER_MCP_FIXTURE") != "1" {
		return
	}
	if err := newMCPFixtureServer().Run(context.Background(), &mcpsdk.StdioTransport{}); err != nil {
		t.Fatal(err)
	}
}

func TestServerPluginFixtureProcess(t *testing.T) {
	if os.Getenv("SUPERSCIENCE_SERVER_PLUGIN_FIXTURE") != "1" {
		return
	}
	reader := bufio.NewScanner(os.Stdin)
	writer := bufio.NewWriter(os.Stdout)
	for reader.Scan() {
		var request struct {
			ID     any            `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if json.Unmarshal(reader.Bytes(), &request) != nil {
			continue
		}
		response := map[string]any{"jsonrpc": "2.0", "id": request.ID}
		switch request.Method {
		case "initialize":
			response["result"] = map[string]any{
				"protocolVersion": "superscience-plugin/1",
				"pluginInfo":      map[string]any{"name": "server-fixture", "version": "1.0.0"},
				"capabilities":    map[string]any{"tools": true},
			}
		case "tools/list":
			response["result"] = map[string]any{"tools": []map[string]any{{"name": "lab_search", "inputSchema": map[string]any{"type": "object"}}}}
		case "tools/call":
			arguments, _ := request.Params["arguments"].(map[string]any)
			response["result"] = map[string]any{"content": []map[string]any{{
				"type": "text", "text": fmt.Sprintf("unified plugin searched %v", arguments["query"]),
			}}}
		case "shutdown":
			response["result"] = map[string]any{"ok": true}
		default:
			response["error"] = map[string]any{"code": -32601, "message": "unknown method"}
		}
		data, _ := json.Marshal(response)
		_, _ = writer.Write(append(data, '\n'))
		_ = writer.Flush()
		if request.Method == "shutdown" {
			return
		}
	}
}

func getJSON[T any](t *testing.T, url string) T {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d", url, resp.StatusCode)
	}
	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func writeJSONFixture(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func approveFirstPermissionOfType(t *testing.T, baseURL, typ string) {
	t.Helper()
	permissions := getJSON[[]map[string]any](t, baseURL+"/api/permissions")
	for _, permission := range permissions {
		if permission["type"] == typ {
			_ = postJSON[map[string]bool](t, baseURL+"/api/permissions/"+permission["id"].(string)+"/respond", map[string]string{"decision": "once"})
			return
		}
	}
	t.Fatalf("%s permission not found: %+v", typ, permissions)
}

func hasAuditEvent(events []store.AuditEvent, action, entityID string) bool {
	for _, event := range events {
		if event.Action == action && event.EntityID == entityID {
			return true
		}
	}
	return false
}

func postJSON[T any](t *testing.T, url string, payload any) T {
	t.Helper()
	body, _ := json.Marshal(payload)
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST %s = %d", url, resp.StatusCode)
	}
	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func postJSONResponse(t *testing.T, url string, payload any) (int, map[string]any) {
	t.Helper()
	body, _ := json.Marshal(payload)
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, out
}

func putJSON[T any](t *testing.T, url string, payload any) T {
	t.Helper()
	body, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT %s = %d", url, resp.StatusCode)
	}
	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func patchJSON[T any](t *testing.T, url string, payload any) T {
	t.Helper()
	body, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPatch, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH %s = %d", url, resp.StatusCode)
	}
	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func toJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
