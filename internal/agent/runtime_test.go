package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"superscience/internal/agent"
	"superscience/internal/providers"
	"superscience/internal/science"
	"superscience/internal/store"
)

type runtimeProvider struct{}

func (runtimeProvider) Stream(ctx context.Context, request providers.ChatRequest, onEvent func(providers.StreamEvent)) (agent.ProviderResult, error) {
	onEvent(providers.StreamEvent{Type: "text_delta", Delta: "adapter "})
	onEvent(providers.StreamEvent{Type: "text_delta", Delta: "answer"})
	return agent.ProviderResult{
		Text: "adapter answer",
		Usage: agent.ProviderUsage{
			PromptTokens:     11,
			CompletionTokens: 7,
			TotalTokens:      18,
		},
	}, nil
}

type cancelAwareProvider struct {
	started  chan struct{}
	canceled chan struct{}
}

func TestRuntimePersistsUserGoalBeforeAssistantResult(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	project, err := db.OpenProject(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.CreateSession(ctx, store.CreateSessionInput{ProjectID: project.ID, Agent: "research"})
	if err != nil {
		t.Fatal(err)
	}
	runtime := agent.Runtime{Store: db, Science: science.NewRegistry(nil), ProjectRoot: project.Path, Provider: runtimeProvider{}}
	if _, err := runtime.Run(ctx, session.ID, agent.RunRequest{Goal: "Persist this user objective", Agent: "research"}); err != nil {
		t.Fatal(err)
	}
	messages, err := db.ListMessages(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 || messages[0].Role != "user" || messages[0].Content != "Persist this user objective" || messages[1].Role != "assistant" {
		t.Fatalf("messages = %+v", messages)
	}
}

func (p cancelAwareProvider) Stream(ctx context.Context, _ providers.ChatRequest, _ func(providers.StreamEvent)) (agent.ProviderResult, error) {
	close(p.started)
	<-ctx.Done()
	close(p.canceled)
	return agent.ProviderResult{}, ctx.Err()
}

func TestRuntimePersistsAbortedStatusWhenProviderContextIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	project, err := db.OpenProject(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.CreateSession(context.Background(), store.CreateSessionInput{ProjectID: project.ID, Agent: "research"})
	if err != nil {
		t.Fatal(err)
	}
	provider := cancelAwareProvider{started: make(chan struct{}), canceled: make(chan struct{})}
	runtime := agent.Runtime{Store: db, Science: science.NewRegistry(nil), ProjectRoot: project.Path, Provider: provider}
	errCh := make(chan error, 1)
	go func() {
		_, runErr := runtime.Run(ctx, session.ID, agent.RunRequest{Goal: "Wait for cancellation", Agent: "research"})
		errCh <- runErr
	}()
	<-provider.started
	cancel()
	if err := <-errCh; !errors.Is(err, context.Canceled) {
		t.Fatalf("run cancellation error = %v", err)
	}
	<-provider.canceled
	runs, err := db.ListAgentRuns(context.Background(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].Status != "aborted" {
		t.Fatalf("canceled run status = %+v", runs)
	}
}

type promptCaptureProvider struct {
	calls   int
	prompts []string
}

func requestText(request providers.ChatRequest) string {
	parts := make([]string, 0, len(request.Messages))
	for _, message := range request.Messages {
		if message.Content != "" {
			parts = append(parts, message.Content)
		}
	}
	return strings.Join(parts, "\n")
}

func requestHasToolResult(request providers.ChatRequest, callID string, contains string) bool {
	for _, message := range request.Messages {
		if message.Role == "tool" && message.ToolCallID == callID && strings.Contains(message.Content, contains) {
			return true
		}
	}
	return false
}

type nativeToolLoopProvider struct {
	requests []providers.ChatRequest
}

func (p *nativeToolLoopProvider) Stream(ctx context.Context, request providers.ChatRequest, onEvent func(providers.StreamEvent)) (agent.ProviderResult, error) {
	p.requests = append(p.requests, request)
	if len(p.requests) == 1 {
		return agent.ProviderResult{
			ToolCalls: []agent.ProviderToolCall{{
				ID:        "call_artifact_1",
				Name:      "artifact",
				Arguments: `{"type":"markdown","title":"Evidence map","content":"EGFR evidence"}`,
			}},
			Usage: agent.ProviderUsage{PromptTokens: 5, CompletionTokens: 2, TotalTokens: 7},
		}, nil
	}
	return agent.ProviderResult{
		Text:  "Artifact created from a structured tool observation.",
		Usage: agent.ProviderUsage{PromptTokens: 9, CompletionTokens: 6, TotalTokens: 15},
	}, nil
}

func TestRuntimeUsesNativeToolSchemasAndStructuredObservationMessages(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	project, err := db.OpenProject(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.CreateSession(ctx, store.CreateSessionInput{ProjectID: project.ID, Agent: "research"})
	if err != nil {
		t.Fatal(err)
	}
	provider := &nativeToolLoopProvider{}
	runtime := agent.Runtime{Store: db, Science: science.NewRegistry(nil), ProjectRoot: project.Path, Provider: provider}

	result, err := runtime.Run(ctx, session.ID, agent.RunRequest{Goal: "Create an EGFR evidence artifact", Agent: "research", MaxSteps: 3})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" || !strings.Contains(result.Message.Content, "structured tool observation") {
		t.Fatalf("result = %+v", result)
	}
	if len(provider.requests) != 2 {
		t.Fatalf("provider requests = %+v", provider.requests)
	}
	var hasArtifactTool, hasArtifactTitle, hasScienceLimit, hasScienceOrganism bool
	for _, tool := range provider.requests[0].Tools {
		properties, _ := tool.InputSchema["properties"].(map[string]any)
		if tool.Name == "artifact" && tool.InputSchema["type"] == "object" {
			hasArtifactTool = true
			_, hasArtifactTitle = properties["title"]
		}
		if tool.Name == "science_search" {
			limit, _ := properties["limit"].(map[string]any)
			organism, _ := properties["organism"].(map[string]any)
			hasScienceLimit = limit["type"] == "integer"
			hasScienceOrganism = organism["type"] == "string"
		}
	}
	if !hasArtifactTool || !hasArtifactTitle || !hasScienceLimit || !hasScienceOrganism {
		t.Fatalf("complete artifact/science tool schemas missing: %+v", provider.requests[0].Tools)
	}
	var assistantCall, toolResult bool
	for _, message := range provider.requests[1].Messages {
		if strings.Contains(message.Content, "Tool observations:") {
			t.Fatalf("runtime used concatenated observation prompt: %+v", provider.requests[1].Messages)
		}
		if message.Role == "assistant" && len(message.ToolCalls) == 1 && message.ToolCalls[0].ID == "call_artifact_1" {
			assistantCall = true
		}
		if message.Role == "tool" && message.ToolCallID == "call_artifact_1" && strings.Contains(message.Content, "artifact_") {
			toolResult = true
		}
	}
	if !assistantCall || !toolResult {
		t.Fatalf("structured tool turn missing: %+v", provider.requests[1].Messages)
	}
}

type scienceObservationProvider struct {
	requests []providers.ChatRequest
}

func (p *scienceObservationProvider) Stream(ctx context.Context, request providers.ChatRequest, onEvent func(providers.StreamEvent)) (agent.ProviderResult, error) {
	p.requests = append(p.requests, request)
	if len(p.requests) == 1 {
		return agent.ProviderResult{
			ToolCalls: []agent.ProviderToolCall{{
				ID:        "call_science_1",
				Name:      "science_search",
				Arguments: `{"database":"pubmed","query":"EGFR","limit":3}`,
			}},
		}, nil
	}
	if !requestHasToolResult(request, "call_science_1", `"id":"123"`) ||
		!requestHasToolResult(request, "call_science_1", "EGFR inhibitor clinical response") ||
		!requestHasToolResult(request, "call_science_1", "https://pubmed.ncbi.nlm.nih.gov/123/") {
		return agent.ProviderResult{}, errors.New("structured PubMed hit was not included in the tool observation")
	}
	return agent.ProviderResult{Text: "PMID 123 supports the evidence summary."}, nil
}

func TestRuntimePreservesStructuredScienceResultsForProviderAndReplay(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/esearch.fcgi":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"esearchresult": map[string]any{"idlist": []string{"123"}},
			})
		case "/esummary.fcgi":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result": map[string]any{
					"uids": []string{"123"},
					"123":  map[string]any{"title": "EGFR inhibitor clinical response", "source": "PubMed", "pubdate": "2026"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	project, err := db.OpenProject(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.CreateSession(ctx, store.CreateSessionInput{ProjectID: project.ID, Agent: "research"})
	if err != nil {
		t.Fatal(err)
	}
	provider := &scienceObservationProvider{}
	gate := &runtimeGate{allowed: map[string]bool{"network:pubmed:EGFR": true}}
	runtime := agent.Runtime{
		Store:       db,
		Science:     science.NewRegistryWithEndpoints(server.Client(), science.Endpoints{PubMed: server.URL}),
		ProjectRoot: project.Path,
		Provider:    provider,
		Gate:        gate,
	}

	result, err := runtime.Run(ctx, session.ID, agent.RunRequest{Goal: "Review EGFR evidence", Agent: "research", MaxSteps: 3})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" || result.Message.Content != "PMID 123 supports the evidence summary." {
		t.Fatalf("result = %+v", result)
	}
	calls, err := db.ListToolCalls(ctx, result.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || !strings.Contains(calls[0].Data, `"id":"123"`) || !strings.Contains(calls[0].Data, "https://pubmed.ncbi.nlm.nih.gov/123/") {
		t.Fatalf("persisted tool output omitted structured PubMed evidence: %+v", calls)
	}
}

func TestProviderFromEnvFailsWithoutConfiguredCloudProvider(t *testing.T) {
	for _, name := range []string{
		"SUPERSCIENCE_PROVIDER",
		"SUPERSCIENCE_API_KEY",
		"OPENAI_API_KEY",
		"ANTHROPIC_API_KEY",
	} {
		t.Setenv(name, "")
	}
	provider := agent.ProviderFromEnv()
	result, err := provider.Stream(context.Background(), providers.ChatRequest{
		Messages: []providers.Message{{Role: "user", Content: "real research request"}},
	}, func(providers.StreamEvent) {})
	if err == nil {
		t.Fatalf("unconfigured provider returned synthetic result: %+v", result)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "provider") || !strings.Contains(strings.ToLower(err.Error()), "configured") {
		t.Fatalf("unconfigured provider error is not actionable: %v", err)
	}
}

func (p *promptCaptureProvider) Stream(ctx context.Context, request providers.ChatRequest, onEvent func(providers.StreamEvent)) (agent.ProviderResult, error) {
	prompt := requestText(request)
	p.calls++
	p.prompts = append(p.prompts, prompt)
	return agent.ProviderResult{
		Text:  "captured prompt",
		Usage: agent.ProviderUsage{PromptTokens: len(strings.Fields(prompt)), CompletionTokens: 2, TotalTokens: len(strings.Fields(prompt)) + 2},
	}, nil
}

func TestRuntimeUsesProviderFactoryAndPersistsUsageStep(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	project, err := db.OpenProject(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.CreateSession(ctx, store.CreateSessionInput{ProjectID: project.ID, Agent: "biology"})
	if err != nil {
		t.Fatal(err)
	}

	var selectedProvider string
	var selectedModel string
	runtime := agent.Runtime{
		Store:   db,
		Science: science.NewRegistry(nil),
		ProviderFactory: func(ctx context.Context, request agent.RunRequest) (agent.Provider, error) {
			selectedProvider = request.ProviderID
			selectedModel = request.Model
			return runtimeProvider{}, nil
		},
		ProjectRoot: project.Path,
	}
	result, err := runtime.Run(ctx, session.ID, agent.RunRequest{
		Goal:       "Summarize EGFR evidence",
		Agent:      "biology",
		ProviderID: "anthropic",
		Model:      "claude-sonnet-4.5",
	})
	if err != nil {
		t.Fatal(err)
	}
	if selectedProvider != "anthropic" || selectedModel != "claude-sonnet-4.5" {
		t.Fatalf("provider selection = %s/%s", selectedProvider, selectedModel)
	}
	if result.Status != "completed" || result.Provider != "anthropic" || !strings.Contains(result.Message.Content, "adapter answer") {
		t.Fatalf("result = %+v", result)
	}
	steps, err := db.ListAgentSteps(ctx, result.ID)
	if err != nil {
		t.Fatal(err)
	}
	var sawUsage bool
	for _, step := range steps {
		if step.Kind == "usage" && strings.Contains(step.Metadata, `"totalTokens":18`) {
			sawUsage = true
		}
	}
	if !sawUsage {
		t.Fatalf("usage step missing: %+v", steps)
	}
}

func TestRuntimeDoesNotAutoRunScienceSearchWhenModelReturnsFinalText(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	project, err := db.OpenProject(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.CreateSession(ctx, store.CreateSessionInput{ProjectID: project.ID, Agent: "research"})
	if err != nil {
		t.Fatal(err)
	}
	provider := &promptCaptureProvider{}
	runtime := agent.Runtime{Store: db, Science: science.NewRegistry(nil), ProjectRoot: project.Path, Provider: provider}

	result, err := runtime.Run(ctx, session.ID, agent.RunRequest{Goal: "Answer directly without tools", Agent: "research"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" {
		t.Fatalf("run status = %+v", result)
	}
	if provider.calls != 1 {
		t.Fatalf("expected one model call, got %d prompts=%+v", provider.calls, provider.prompts)
	}
	calls, err := db.ListToolCalls(ctx, result.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, call := range calls {
		if call.Tool == "science_search" {
			t.Fatalf("model final text should not trigger fallback science_search: %+v", calls)
		}
	}
}

func TestRuntimeLoadsSlashSkillBeforeModelCallAndInjectsPrompt(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	project, err := db.OpenProject(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.CreateSession(ctx, store.CreateSessionInput{ProjectID: project.ID, Agent: "research"})
	if err != nil {
		t.Fatal(err)
	}
	provider := &promptCaptureProvider{}
	runtime := agent.Runtime{Store: db, Science: science.NewRegistry(nil), ProjectRoot: project.Path, Provider: provider}

	result, err := runtime.Run(ctx, session.ID, agent.RunRequest{Goal: "/research-protocol map kinase resistance evidence", Agent: "research", MaxSteps: 1})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" {
		t.Fatalf("run status = %+v", result)
	}
	if provider.calls != 1 {
		t.Fatalf("expected one model call, got %d", provider.calls)
	}
	if len(provider.prompts) != 1 || !strings.Contains(provider.prompts[0], "Loaded skill: research-protocol") || !strings.Contains(provider.prompts[0], "Structure literature search") {
		t.Fatalf("skill content was not injected into prompt: %+v", provider.prompts)
	}
	if strings.Contains(provider.prompts[0], "/research-protocol") {
		t.Fatalf("slash invocation leaked into user goal prompt: %s", provider.prompts[0])
	}
	steps, err := db.ListAgentSteps(ctx, result.ID)
	if err != nil {
		t.Fatal(err)
	}
	firstAction := ""
	var sawSkillStep bool
	for _, step := range steps {
		if firstAction == "" && step.Kind != "planning" {
			firstAction = step.Kind
		}
		if step.Kind == "skill" && step.Title == "research-protocol" && strings.Contains(step.Content, "Structure literature search") {
			sawSkillStep = true
		}
	}
	if firstAction != "skill" {
		t.Fatalf("skill should be first runtime action after planning, got %q steps=%+v", firstAction, steps)
	}
	if !sawSkillStep {
		t.Fatalf("skill step missing: %+v", steps)
	}
	calls, err := db.ListToolCalls(ctx, result.ID)
	if err != nil {
		t.Fatal(err)
	}
	var sawSkillToolCall bool
	for _, call := range calls {
		if call.Tool == "skill" && call.Status == "ok" && strings.Contains(call.Output, "research-protocol") {
			sawSkillToolCall = true
		}
	}
	if !sawSkillToolCall {
		t.Fatalf("skill tool call missing: %+v", calls)
	}
	parts, err := db.ListMessageParts(ctx, result.Message.ID)
	if err != nil {
		t.Fatal(err)
	}
	var sawSkillPart bool
	for _, part := range parts {
		if part.Type == "skill" && part.Title == "research-protocol" && strings.Contains(part.Content, "Structure literature search") {
			sawSkillPart = true
		}
	}
	if !sawSkillPart {
		t.Fatalf("skill message part missing: %+v", parts)
	}
}

func TestRuntimeLoadsProjectSlashSkillFromProjectRoot(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	projectRoot := t.TempDir()
	skillDir := filepath.Join(projectRoot, ".superscience", "skills", "project-evidence")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: project-evidence\ndescription: Project evidence skill.\n---\n# Project Evidence\nUse project-root evidence before the model call.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	project, err := db.OpenProject(ctx, projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.CreateSession(ctx, store.CreateSessionInput{ProjectID: project.ID, Agent: "research"})
	if err != nil {
		t.Fatal(err)
	}
	provider := &promptCaptureProvider{}
	runtime := agent.Runtime{Store: db, Science: science.NewRegistry(nil), ProjectRoot: project.Path, Provider: provider}

	result, err := runtime.Run(ctx, session.ID, agent.RunRequest{Goal: "/project-evidence inspect kinase claims", Agent: "research", MaxSteps: 1})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" {
		t.Fatalf("run status = %+v", result)
	}
	if len(provider.prompts) != 1 || !strings.Contains(provider.prompts[0], "Loaded skill: project-evidence") || !strings.Contains(provider.prompts[0], "project-root evidence") {
		t.Fatalf("project skill content was not injected into prompt: %+v", provider.prompts)
	}
	steps, err := db.ListAgentSteps(ctx, result.ID)
	if err != nil {
		t.Fatal(err)
	}
	var sawProjectSkill bool
	for _, step := range steps {
		if step.Kind == "skill" && step.Title == "project-evidence" && strings.Contains(step.Content, "project-root evidence") {
			sawProjectSkill = true
		}
	}
	if !sawProjectSkill {
		t.Fatalf("project skill step missing: %+v", steps)
	}
}

func TestRuntimeLoadsUserSlashSkillFromDataDir(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "runtime.db"))
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
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: user-evidence\ndescription: User evidence skill.\n---\n# User Evidence\nUse user-level evidence before the model call.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	project, err := db.OpenProject(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.CreateSession(ctx, store.CreateSessionInput{ProjectID: project.ID, Agent: "research"})
	if err != nil {
		t.Fatal(err)
	}
	provider := &promptCaptureProvider{}
	runtime := agent.Runtime{Store: db, Science: science.NewRegistry(nil), ProjectRoot: project.Path, Provider: provider}

	result, err := runtime.Run(ctx, session.ID, agent.RunRequest{Goal: "/user-evidence inspect kinase claims", Agent: "research", MaxSteps: 1})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" {
		t.Fatalf("run status = %+v", result)
	}
	if len(provider.prompts) != 1 || !strings.Contains(provider.prompts[0], "Loaded skill: user-evidence") || !strings.Contains(provider.prompts[0], "user-level evidence") {
		t.Fatalf("user skill content was not injected into prompt: %+v", provider.prompts)
	}
}

func TestRuntimeTreatsUnknownSlashSkillAsPlainGoal(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	project, err := db.OpenProject(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.CreateSession(ctx, store.CreateSessionInput{ProjectID: project.ID, Agent: "research"})
	if err != nil {
		t.Fatal(err)
	}
	provider := &promptCaptureProvider{}
	runtime := agent.Runtime{Store: db, Science: science.NewRegistry(nil), ProjectRoot: project.Path, Provider: provider}

	result, err := runtime.Run(ctx, session.ID, agent.RunRequest{Goal: "/missing-skill keep this literal", Agent: "research", MaxSteps: 1})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" {
		t.Fatalf("run status = %+v", result)
	}
	if len(provider.prompts) != 1 || !strings.Contains(provider.prompts[0], "/missing-skill keep this literal") {
		t.Fatalf("unknown slash skill should remain literal: %+v", provider.prompts)
	}
	steps, err := db.ListAgentSteps(ctx, result.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range steps {
		if step.Kind == "skill" {
			t.Fatalf("unknown slash skill should not create skill step: %+v", steps)
		}
	}
}

type toolCallingProvider struct {
	call providers.ToolCall
}

func (p toolCallingProvider) Stream(ctx context.Context, request providers.ChatRequest, onEvent func(providers.StreamEvent)) (agent.ProviderResult, error) {
	if requestHasToolResult(request, p.call.ID, "artifact_") {
		return agent.ProviderResult{
			Text:  "Finalized after the requested tool call.",
			Usage: agent.ProviderUsage{PromptTokens: 9, CompletionTokens: 5, TotalTokens: 14},
		}, nil
	}
	return agent.ProviderResult{
		Text:      "I need to call a tool before finalizing.",
		ToolCalls: []providers.ToolCall{p.call},
		Usage:     agent.ProviderUsage{PromptTokens: 8, CompletionTokens: 6, TotalTokens: 14},
	}, nil
}

func TestRuntimeExecutesProviderToolCallsAndPersistsObservation(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	project, err := db.OpenProject(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.CreateSession(ctx, store.CreateSessionInput{ProjectID: project.ID, Agent: "research"})
	if err != nil {
		t.Fatal(err)
	}
	runtime := agent.Runtime{
		Store:       db,
		Science:     science.NewRegistry(nil),
		ProjectRoot: project.Path,
		Provider: toolCallingProvider{call: providers.ToolCall{
			ID:        "call_artifact",
			Name:      "artifact",
			Arguments: `{"type":"markdown","title":"EGFR note","content":"# EGFR\nTool-created artifact."}`,
		}},
	}
	result, err := runtime.Run(ctx, session.ID, agent.RunRequest{Goal: "Create an EGFR artifact", Agent: "research"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" {
		t.Fatalf("run status = %+v", result)
	}
	artifacts, err := db.ListArtifacts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var sawToolArtifact bool
	for _, artifact := range artifacts {
		if artifact.Title == "EGFR note" && strings.Contains(artifact.Content, "Tool-created artifact") {
			sawToolArtifact = true
		}
	}
	if !sawToolArtifact {
		t.Fatalf("tool artifact missing: %+v", artifacts)
	}
	calls, err := db.ListToolCalls(ctx, result.ID)
	if err != nil {
		t.Fatal(err)
	}
	var sawExecutedArtifact bool
	for _, call := range calls {
		if call.Tool == "artifact" && call.Status == "ok" && strings.Contains(call.Output, "artifact_") {
			sawExecutedArtifact = true
		}
	}
	if !sawExecutedArtifact {
		t.Fatalf("executed artifact tool call missing: %+v", calls)
	}
	steps, err := db.ListAgentSteps(ctx, result.ID)
	if err != nil {
		t.Fatal(err)
	}
	var sawObservation bool
	for _, step := range steps {
		if step.Kind == "observation" && step.Title == "artifact" && strings.Contains(step.Content, "artifact_") {
			sawObservation = true
		}
	}
	if !sawObservation {
		t.Fatalf("tool observation step missing: %+v", steps)
	}
	parts, err := db.ListMessageParts(ctx, result.Message.ID)
	if err != nil {
		t.Fatal(err)
	}
	var sawToolPart bool
	for _, part := range parts {
		if part.Type == "tool" && part.Title == "artifact" && strings.Contains(part.Content, "artifact_") {
			sawToolPart = true
		}
	}
	if !sawToolPart {
		t.Fatalf("tool message part missing: %+v", parts)
	}
}

type observationAwareProvider struct {
	calls   int
	prompts []string
}

func (p *observationAwareProvider) Stream(ctx context.Context, request providers.ChatRequest, onEvent func(providers.StreamEvent)) (agent.ProviderResult, error) {
	prompt := requestText(request)
	p.calls++
	p.prompts = append(p.prompts, prompt)
	if p.calls == 1 {
		return agent.ProviderResult{
			Text: "Need a durable artifact first.",
			ToolCalls: []providers.ToolCall{{
				ID:        "call_artifact",
				Name:      "artifact",
				Arguments: `{"type":"markdown","title":"Observation artifact","content":"# Observation\nCreated from first model turn."}`,
			}},
			Usage: agent.ProviderUsage{PromptTokens: 5, CompletionTokens: 4, TotalTokens: 9},
		}, nil
	}
	if !requestHasToolResult(request, "call_artifact", "artifact_") {
		return agent.ProviderResult{Text: "missing observations"}, nil
	}
	return agent.ProviderResult{
		Text:  "Final answer after reading the artifact observation.",
		Usage: agent.ProviderUsage{PromptTokens: 12, CompletionTokens: 8, TotalTokens: 20},
	}, nil
}

func TestRuntimeCallsModelAgainAfterSuccessfulToolObservations(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	project, err := db.OpenProject(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.CreateSession(ctx, store.CreateSessionInput{ProjectID: project.ID, Agent: "research"})
	if err != nil {
		t.Fatal(err)
	}
	provider := &observationAwareProvider{}
	runtime := agent.Runtime{Store: db, Science: science.NewRegistry(nil), ProjectRoot: project.Path, Provider: provider}

	result, err := runtime.Run(ctx, session.ID, agent.RunRequest{Goal: "Create and summarize an artifact", Agent: "research"})
	if err != nil {
		t.Fatal(err)
	}
	if provider.calls != 2 {
		t.Fatalf("expected two model calls, got %d prompts=%+v", provider.calls, provider.prompts)
	}
	if result.Status != "completed" || result.Message.Content != "Final answer after reading the artifact observation." {
		t.Fatalf("result = %+v", result)
	}
	steps, err := db.ListAgentSteps(ctx, result.ID)
	if err != nil {
		t.Fatal(err)
	}
	var sawFinalModel bool
	for _, step := range steps {
		if step.Kind == "model" && step.Title == "Final response" && strings.Contains(step.Content, "Final answer") {
			sawFinalModel = true
		}
	}
	if !sawFinalModel {
		t.Fatalf("final model step missing: %+v", steps)
	}
}

type multiRoundToolProvider struct {
	calls   int
	prompts []string
}

func (p *multiRoundToolProvider) Stream(ctx context.Context, request providers.ChatRequest, onEvent func(providers.StreamEvent)) (agent.ProviderResult, error) {
	prompt := requestText(request)
	p.calls++
	p.prompts = append(p.prompts, prompt)
	switch p.calls {
	case 1:
		return agent.ProviderResult{
			Text: "Need the first artifact.",
			ToolCalls: []providers.ToolCall{{
				ID:        "call_first_artifact",
				Name:      "artifact",
				Arguments: `{"type":"markdown","title":"First loop artifact","content":"# First\nCreated in turn one."}`,
			}},
			Usage: agent.ProviderUsage{PromptTokens: 4, CompletionTokens: 3, TotalTokens: 7},
		}, nil
	case 2:
		if !requestHasToolResult(request, "call_first_artifact", "artifact_") {
			return agent.ProviderResult{Text: "missing first observation"}, nil
		}
		return agent.ProviderResult{
			Text: "Need the second artifact.",
			ToolCalls: []providers.ToolCall{{
				ID:        "call_second_artifact",
				Name:      "artifact",
				Arguments: `{"type":"markdown","title":"Second loop artifact","content":"# Second\nCreated in turn two."}`,
			}},
			Usage: agent.ProviderUsage{PromptTokens: 6, CompletionTokens: 5, TotalTokens: 11},
		}, nil
	default:
		if !requestHasToolResult(request, "call_second_artifact", "artifact_") {
			return agent.ProviderResult{Text: "missing second observation"}, nil
		}
		return agent.ProviderResult{
			Text:  "Final answer after two tool rounds.",
			Usage: agent.ProviderUsage{PromptTokens: 9, CompletionTokens: 7, TotalTokens: 16},
		}, nil
	}
}

func TestRuntimeContinuesToolLoopUntilModelReturnsFinalAnswer(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	project, err := db.OpenProject(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.CreateSession(ctx, store.CreateSessionInput{ProjectID: project.ID, Agent: "research"})
	if err != nil {
		t.Fatal(err)
	}
	provider := &multiRoundToolProvider{}
	runtime := agent.Runtime{Store: db, Science: science.NewRegistry(nil), ProjectRoot: project.Path, Provider: provider}

	result, err := runtime.Run(ctx, session.ID, agent.RunRequest{Goal: "Create two artifacts before final answer", Agent: "research"})
	if err != nil {
		t.Fatal(err)
	}
	if provider.calls != 3 {
		t.Fatalf("expected three model calls, got %d prompts=%+v", provider.calls, provider.prompts)
	}
	if result.Status != "completed" || result.Message.Content != "Final answer after two tool rounds." {
		t.Fatalf("result = %+v", result)
	}
	calls, err := db.ListToolCalls(ctx, result.ID)
	if err != nil {
		t.Fatal(err)
	}
	var artifactCalls int
	for _, call := range calls {
		if call.Tool == "artifact" && call.Status == "ok" {
			artifactCalls++
		}
	}
	if artifactCalls != 2 {
		t.Fatalf("expected two artifact tool calls, got %d calls=%+v", artifactCalls, calls)
	}
	steps, err := db.ListAgentSteps(ctx, result.ID)
	if err != nil {
		t.Fatal(err)
	}
	var sawSecondObservation bool
	for _, step := range steps {
		if step.Kind == "observation" && step.Title == "artifact" && strings.Contains(step.Metadata, "call_second_artifact") {
			sawSecondObservation = true
		}
	}
	if !sawSecondObservation {
		t.Fatalf("second tool observation step missing: %+v", steps)
	}
}

type repeatingToolProvider struct {
	calls int
}

func (p *repeatingToolProvider) Stream(ctx context.Context, request providers.ChatRequest, onEvent func(providers.StreamEvent)) (agent.ProviderResult, error) {
	p.calls++
	return agent.ProviderResult{
		Text: "Still asking for another artifact.",
		ToolCalls: []providers.ToolCall{{
			ID:        "call_repeating_artifact",
			Name:      "artifact",
			Arguments: `{"type":"markdown","title":"Repeating artifact","content":"# Repeat\nThe model kept asking for tools."}`,
		}},
		Usage: agent.ProviderUsage{PromptTokens: 3, CompletionTokens: 4, TotalTokens: 7},
	}, nil
}

func TestRuntimeStopsAtMaxStepsWhenModelKeepsRequestingTools(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	project, err := db.OpenProject(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.CreateSession(ctx, store.CreateSessionInput{ProjectID: project.ID, Agent: "research"})
	if err != nil {
		t.Fatal(err)
	}
	provider := &repeatingToolProvider{}
	runtime := agent.Runtime{Store: db, Science: science.NewRegistry(nil), ProjectRoot: project.Path, Provider: provider}

	result, err := runtime.Run(ctx, session.ID, agent.RunRequest{Goal: "Never-ending tool loop", Agent: "research", MaxSteps: 2})
	if err != nil {
		t.Fatal(err)
	}
	if provider.calls != 2 {
		t.Fatalf("expected provider to stop after two model calls, got %d", provider.calls)
	}
	if result.Status != "max_steps" {
		t.Fatalf("expected max_steps status, got %+v", result)
	}
	if result.Message.Content != "Still asking for another artifact." || strings.Contains(result.Message.Content, "Stopped after reaching max step limit") {
		t.Fatalf("max-step result must preserve provider text without a generated fallback: %+v", result.Message)
	}
	steps, err := db.ListAgentSteps(ctx, result.ID)
	if err != nil {
		t.Fatal(err)
	}
	var sawMaxStep bool
	for _, step := range steps {
		if step.Kind == "max_steps" && step.Status == "hit" && strings.Contains(step.Content, "2") {
			sawMaxStep = true
		}
	}
	if !sawMaxStep {
		t.Fatalf("max_steps step missing: %+v", steps)
	}
}

func TestRuntimeAddsReviewerGateForRequiredReviewAgents(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	project, err := db.OpenProject(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.CreateSession(ctx, store.CreateSessionInput{ProjectID: project.ID, Agent: "literature-review"})
	if err != nil {
		t.Fatal(err)
	}
	provider := &reviewerProvider{reviewResponse: `{"decision":"approve","reason":"Claims are bounded by the cited evidence."}`}
	runtime := agent.Runtime{Store: db, Science: science.NewRegistry(nil), ProjectRoot: project.Path, Provider: provider}

	result, err := runtime.Run(ctx, session.ID, agent.RunRequest{Goal: "Review EGFR literature", Agent: "literature-review", MaxSteps: 1})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" {
		t.Fatalf("run status = %+v", result)
	}
	if len(provider.requests) != 2 {
		t.Fatalf("reviewer must perform a separate provider call, got %d requests", len(provider.requests))
	}
	reviewRequest := provider.requests[1]
	if !strings.Contains(reviewRequest.System, "independent scientific reviewer") || len(reviewRequest.Tools) != 0 || reviewRequest.ToolChoice != "none" {
		t.Fatalf("review request is not isolated from the primary agent: %+v", reviewRequest)
	}
	if !strings.Contains(requestText(reviewRequest), "Primary draft") {
		t.Fatalf("review request does not contain the primary draft: %+v", reviewRequest.Messages)
	}
	steps, err := db.ListAgentSteps(ctx, result.ID)
	if err != nil {
		t.Fatal(err)
	}
	var sawReviewerGate bool
	for _, step := range steps {
		if step.Kind == "reviewer_gate" && step.Status == "approved" && strings.Contains(step.Content, "cited evidence") {
			sawReviewerGate = true
		}
	}
	if !sawReviewerGate {
		t.Fatalf("reviewer gate step missing: %+v", steps)
	}
}

type reviewerProvider struct {
	requests       []providers.ChatRequest
	reviewResponse string
}

func (p *reviewerProvider) Stream(_ context.Context, request providers.ChatRequest, _ func(providers.StreamEvent)) (agent.ProviderResult, error) {
	p.requests = append(p.requests, request)
	if len(p.requests) == 1 {
		return agent.ProviderResult{Text: "Primary draft with provenance.", Usage: agent.ProviderUsage{PromptTokens: 4, CompletionTokens: 3, TotalTokens: 7}}, nil
	}
	return agent.ProviderResult{Text: p.reviewResponse, Usage: agent.ProviderUsage{PromptTokens: 5, CompletionTokens: 4, TotalTokens: 9}}, nil
}

func TestRuntimeRejectsInvalidReviewerResponse(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	project, err := db.OpenProject(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.CreateSession(ctx, store.CreateSessionInput{ProjectID: project.ID, Agent: "literature-review"})
	if err != nil {
		t.Fatal(err)
	}
	provider := &reviewerProvider{reviewResponse: "Looks good to me."}
	runtime := agent.Runtime{Store: db, Science: science.NewRegistry(nil), ProjectRoot: project.Path, Provider: provider}

	_, err = runtime.Run(ctx, session.ID, agent.RunRequest{Goal: "Review EGFR literature", Agent: "literature-review", MaxSteps: 1})
	if err == nil || !strings.Contains(err.Error(), "reviewer response") {
		t.Fatalf("invalid reviewer response should fail explicitly, got %v", err)
	}
	runs, err := db.ListAgentRuns(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].Status != "review_failed" {
		t.Fatalf("review failure status was not persisted: %+v", runs)
	}
}

type revisionProvider struct {
	requests []providers.ChatRequest
}

func (p *revisionProvider) Stream(_ context.Context, request providers.ChatRequest, _ func(providers.StreamEvent)) (agent.ProviderResult, error) {
	p.requests = append(p.requests, request)
	switch len(p.requests) {
	case 1:
		return agent.ProviderResult{Text: "Initial draft."}, nil
	case 2:
		return agent.ProviderResult{Text: `{"decision":"revise","reason":"State the uncertainty explicitly."}`}, nil
	case 3:
		return agent.ProviderResult{Text: "Revised draft with explicit uncertainty."}, nil
	default:
		return agent.ProviderResult{Text: `{"decision":"approve","reason":"Uncertainty is now explicit."}`}, nil
	}
}

func TestRuntimeRevisesDraftAndRunsReviewerAgain(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	project, err := db.OpenProject(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.CreateSession(ctx, store.CreateSessionInput{ProjectID: project.ID, Agent: "literature-review"})
	if err != nil {
		t.Fatal(err)
	}
	provider := &revisionProvider{}
	runtime := agent.Runtime{Store: db, Science: science.NewRegistry(nil), ProjectRoot: project.Path, Provider: provider}

	result, err := runtime.Run(ctx, session.ID, agent.RunRequest{Goal: "Review EGFR literature", Agent: "literature-review", MaxSteps: 1})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" || result.Message.Content != "Revised draft with explicit uncertainty." {
		t.Fatalf("revision was not persisted as the final answer: %+v", result)
	}
	if len(provider.requests) != 4 {
		t.Fatalf("expected primary, reviewer, revision, and second reviewer calls; got %d", len(provider.requests))
	}
	if !strings.Contains(requestText(provider.requests[2]), "State the uncertainty explicitly") {
		t.Fatalf("revision request omitted reviewer reason: %+v", provider.requests[2].Messages)
	}
}

type evidenceAwareReviewerProvider struct {
	requests []providers.ChatRequest
}

func (p *evidenceAwareReviewerProvider) Stream(_ context.Context, request providers.ChatRequest, _ func(providers.StreamEvent)) (agent.ProviderResult, error) {
	p.requests = append(p.requests, request)
	switch len(p.requests) {
	case 1:
		return agent.ProviderResult{ToolCalls: []agent.ProviderToolCall{{
			ID:        "call_evidence_artifact",
			Name:      "artifact",
			Arguments: `{"type":"markdown","title":"Evidence","content":"PMID 123 | EGFR result | https://pubmed.ncbi.nlm.nih.gov/123/"}`,
		}}}, nil
	case 2:
		return agent.ProviderResult{Text: "The evidence artifact was created."}, nil
	case 3:
		if !strings.Contains(requestText(request), "PMID 123") || !strings.Contains(requestText(request), "call_evidence_artifact") {
			return agent.ProviderResult{}, errors.New("reviewer did not receive persisted tool evidence")
		}
		return agent.ProviderResult{Text: `{"decision":"revise","reason":"Include the exact PMID and URL in the final response."}`}, nil
	case 4:
		if !strings.Contains(requestText(request), "PMID 123") || !strings.Contains(requestText(request), "https://pubmed.ncbi.nlm.nih.gov/123/") {
			return agent.ProviderResult{}, errors.New("revision did not receive persisted tool evidence")
		}
		return agent.ProviderResult{Text: "PMID 123: EGFR result. https://pubmed.ncbi.nlm.nih.gov/123/"}, nil
	default:
		return agent.ProviderResult{Text: `{"decision":"approve","reason":"The final response now reproduces the persisted evidence."}`}, nil
	}
}

func TestReviewerAndRevisionReceivePersistedToolEvidence(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	project, err := db.OpenProject(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.CreateSession(ctx, store.CreateSessionInput{ProjectID: project.ID, Agent: "literature-review"})
	if err != nil {
		t.Fatal(err)
	}
	provider := &evidenceAwareReviewerProvider{}
	runtime := agent.Runtime{Store: db, Science: science.NewRegistry(nil), ProjectRoot: project.Path, Provider: provider}

	result, err := runtime.Run(ctx, session.ID, agent.RunRequest{Goal: "Create a cited EGFR review", Agent: "literature-review", MaxSteps: 3})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" || !strings.Contains(result.Message.Content, "PMID 123") {
		t.Fatalf("result = %+v", result)
	}
	if len(provider.requests) != 5 {
		t.Fatalf("provider requests = %d", len(provider.requests))
	}
}

type runtimeGate struct {
	allowed map[string]bool
	asked   []string
}

func (g *runtimeGate) Ask(sessionID, typ, pattern, message string) string {
	g.asked = append(g.asked, typ+":"+pattern)
	return "permission_" + strings.ReplaceAll(typ+"_"+pattern, "/", "_")
}

func (g *runtimeGate) IsAllowed(sessionID, typ, pattern string) bool {
	if g.allowed == nil {
		return false
	}
	return g.allowed[typ+":"+pattern]
}

type persistentRuntimeGate struct {
	store *store.Store
}

func (g persistentRuntimeGate) Ask(sessionID, typ, pattern, message string) string {
	permission, err := g.store.CreatePermission(context.Background(), store.PermissionRecord{
		SessionID: sessionID,
		Type:      typ,
		Pattern:   pattern,
		Message:   message,
		Status:    "pending",
	})
	if err != nil {
		panic(err)
	}
	return permission.ID
}

func (persistentRuntimeGate) IsAllowed(string, string, string) bool { return false }

type resumableWriteProvider struct {
	requests []providers.ChatRequest
}

func (p *resumableWriteProvider) Stream(_ context.Context, request providers.ChatRequest, _ func(providers.StreamEvent)) (agent.ProviderResult, error) {
	p.requests = append(p.requests, request)
	if requestHasToolResult(request, "call_write_resume", "approved evidence") {
		return agent.ProviderResult{Text: "The approved research note was written."}, nil
	}
	return agent.ProviderResult{
		Text: "Writing the requested evidence note requires approval.",
		ToolCalls: []providers.ToolCall{{
			ID:        "call_write_resume",
			Name:      "write",
			Arguments: `{"path":"notes/egfr.md","content":"approved evidence"}`,
		}},
	}, nil
}

type questionProvider struct {
	requests []providers.ChatRequest
}

func (p *questionProvider) Stream(_ context.Context, request providers.ChatRequest, _ func(providers.StreamEvent)) (agent.ProviderResult, error) {
	p.requests = append(p.requests, request)
	if requestHasToolResult(request, "call_question_organism", "human") {
		return agent.ProviderResult{Text: "The analysis will use the human reference genome."}, nil
	}
	return agent.ProviderResult{
		Text: "I need the target organism before continuing.",
		ToolCalls: []providers.ToolCall{{
			ID:        "call_question_organism",
			Name:      "question",
			Arguments: `{"question":"Which organism should be used?"}`,
		}},
	}, nil
}

func TestRuntimeWaitsForAndResumesWithUserAnswer(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	project, err := db.OpenProject(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.CreateSession(ctx, store.CreateSessionInput{ProjectID: project.ID, Agent: "research"})
	if err != nil {
		t.Fatal(err)
	}
	provider := &questionProvider{}
	runtime := agent.Runtime{Store: db, Science: science.NewRegistry(nil), ProjectRoot: project.Path, Provider: provider}

	waiting, err := runtime.Run(ctx, session.ID, agent.RunRequest{Goal: "Analyze a reference genome", Agent: "research", MaxSteps: 4})
	if err != nil {
		t.Fatal(err)
	}
	if waiting.Status != "waiting_input" || waiting.PendingActionID == "" {
		t.Fatalf("question run = %+v", waiting)
	}
	action, err := db.GetPendingAction(ctx, waiting.PendingActionID)
	if err != nil || action.Status != "waiting_input" || !strings.Contains(action.Payload, "Which organism") {
		t.Fatalf("pending action = %+v err=%v", action, err)
	}

	completed, err := runtime.Answer(ctx, waiting.ID, waiting.PendingActionID, "human")
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != "completed" || completed.Message.Content != "The analysis will use the human reference genome." {
		t.Fatalf("answered run = %+v", completed)
	}
	answered, err := db.GetPendingAction(ctx, waiting.PendingActionID)
	if err != nil || answered.Status != "answered" || answered.Response != "human" {
		t.Fatalf("answered action = %+v err=%v", answered, err)
	}
	if len(provider.requests) != 2 {
		t.Fatalf("provider requests = %d", len(provider.requests))
	}
}

func TestRuntimeResumesApprovedToolCallFromPersistedCheckpoint(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	root := t.TempDir()
	project, err := db.OpenProject(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.CreateSession(ctx, store.CreateSessionInput{ProjectID: project.ID, Agent: "research"})
	if err != nil {
		t.Fatal(err)
	}
	provider := &resumableWriteProvider{}
	runtime := agent.Runtime{
		Store:       db,
		Science:     science.NewRegistry(nil),
		Gate:        persistentRuntimeGate{store: db},
		ProjectRoot: root,
		Provider:    provider,
	}

	waiting, err := runtime.Run(ctx, session.ID, agent.RunRequest{Goal: "Write an evidence note", Agent: "research", MaxSteps: 4})
	if err != nil {
		t.Fatal(err)
	}
	if waiting.Status != "waiting_permission" || waiting.PermissionID == "" {
		t.Fatalf("waiting run = %+v", waiting)
	}
	checkpoint, err := db.GetRunCheckpoint(ctx, waiting.ID)
	if err != nil {
		t.Fatalf("checkpoint missing: %v", err)
	}
	if checkpoint.PermissionID != waiting.PermissionID || checkpoint.ToolCallID == "" {
		t.Fatalf("checkpoint = %+v", checkpoint)
	}
	if err := db.UpdatePermissionStatus(ctx, waiting.PermissionID, "once"); err != nil {
		t.Fatal(err)
	}

	completed, err := runtime.Resume(ctx, waiting.ID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != "completed" || completed.Message.Content != "The approved research note was written." {
		t.Fatalf("completed run = %+v", completed)
	}
	content, err := os.ReadFile(filepath.Join(root, "notes", "egfr.md"))
	if err != nil || string(content) != "approved evidence" {
		t.Fatalf("resumed write content=%q err=%v", content, err)
	}
	calls, err := db.ListToolCalls(ctx, waiting.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].ID != checkpoint.ToolCallID || calls[0].Status != "ok" {
		t.Fatalf("tool call was not resumed in place: %+v", calls)
	}
	if _, err := db.GetRunCheckpoint(ctx, waiting.ID); err == nil {
		t.Fatal("completed run retained its checkpoint")
	}
}

func TestRuntimePausesWhenProviderToolCallNeedsPermission(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	root := t.TempDir()
	project, err := db.OpenProject(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.CreateSession(ctx, store.CreateSessionInput{ProjectID: project.ID, Agent: "research"})
	if err != nil {
		t.Fatal(err)
	}
	gate := &runtimeGate{allowed: map[string]bool{}}
	runtime := agent.Runtime{
		Store:       db,
		Science:     science.NewRegistry(nil),
		Gate:        gate,
		ProjectRoot: project.Path,
		Provider: toolCallingProvider{call: providers.ToolCall{
			ID:        "call_write",
			Name:      "write",
			Arguments: `{"path":"notes/egfr.md","content":"approved later"}`,
		}},
	}
	result, err := runtime.Run(ctx, session.ID, agent.RunRequest{Goal: "Write a note", Agent: "research"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "waiting_permission" || result.PermissionID == "" {
		t.Fatalf("run should wait for write permission: %+v", result)
	}
	if len(gate.asked) != 1 || gate.asked[0] != "write:notes/egfr.md" {
		t.Fatalf("permission requests = %+v", gate.asked)
	}
	calls, err := db.ListToolCalls(ctx, result.ID)
	if err != nil {
		t.Fatal(err)
	}
	var sawPendingWrite bool
	for _, call := range calls {
		if call.Tool == "write" && call.Status == "pending_permission" && call.PermissionID == result.PermissionID {
			sawPendingWrite = true
		}
		if call.Tool == "science_search" {
			t.Fatalf("fallback science_search should not run after provider requested write: %+v", calls)
		}
	}
	if !sawPendingWrite {
		t.Fatalf("pending write call missing: %+v", calls)
	}
}
