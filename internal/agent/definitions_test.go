package agent_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"superscience/internal/agent"
	"superscience/internal/providers"
	"superscience/internal/science"
	"superscience/internal/store"
)

func TestCustomAgentDefinitionMergesWithBuiltInsAndValidatesTools(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "agents.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	created, err := agent.CreateDefinition(ctx, db, agent.Definition{
		ID:           "chemistry-review",
		Name:         "Chemistry Review",
		Domain:       "chemistry",
		Description:  "Reviews chemistry evidence.",
		SystemPrompt: "Require structure-backed evidence and explicit uncertainty.",
		Tools:        []string{"read", "grep"},
		ReviewMode:   "required",
		Enabled:      true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.BuiltIn || created.ID != "chemistry-review" {
		t.Fatalf("created definition = %+v", created)
	}
	definitions, err := agent.ListDefinitions(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	var sawBuiltIn, sawCustom bool
	for _, definition := range definitions {
		sawBuiltIn = sawBuiltIn || definition.ID == "research" && definition.BuiltIn
		sawCustom = sawCustom || definition.ID == created.ID && !definition.BuiltIn
	}
	if !sawBuiltIn || !sawCustom {
		t.Fatalf("merged definitions = %+v", definitions)
	}
	if _, err := agent.CreateDefinition(ctx, db, agent.Definition{ID: "research", Name: "Override", Domain: "general", Description: "No", Tools: []string{"read"}}); err == nil {
		t.Fatal("built-in agent ID was overridden")
	}
	if _, err := agent.CreateDefinition(ctx, db, agent.Definition{ID: "bad-tools", Name: "Bad Tools", Domain: "test", Description: "No", Tools: []string{"not-a-tool"}}); err == nil {
		t.Fatal("unknown tool was accepted")
	}
}

type customAgentCaptureProvider struct {
	requests []providers.ChatRequest
}

func (p *customAgentCaptureProvider) Stream(_ context.Context, request providers.ChatRequest, _ func(providers.StreamEvent)) (agent.ProviderResult, error) {
	p.requests = append(p.requests, request)
	return agent.ProviderResult{Text: "Chemistry review completed from observed evidence."}, nil
}

func TestRuntimeUsesPersistedCustomAgentPromptAndToolAllowlist(t *testing.T) {
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
	definition, err := agent.CreateDefinition(ctx, db, agent.Definition{
		ID:           "chemistry-review",
		Name:         "Chemistry Review",
		Domain:       "chemistry",
		Description:  "Reviews chemistry evidence.",
		SystemPrompt: "Require structure-backed evidence and explicit uncertainty.",
		Tools:        []string{"read", "grep"},
		ReviewMode:   "none",
		Enabled:      true,
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.CreateSession(ctx, store.CreateSessionInput{ProjectID: project.ID, Agent: definition.ID})
	if err != nil {
		t.Fatal(err)
	}
	provider := &customAgentCaptureProvider{}
	runtime := agent.Runtime{Store: db, Science: science.NewRegistry(nil), ProjectRoot: project.Path, Provider: provider}
	result, err := runtime.Run(ctx, session.ID, agent.RunRequest{Goal: "Review the compound evidence", Agent: definition.ID, MaxSteps: 1})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" || len(provider.requests) != 1 {
		t.Fatalf("result=%+v requests=%d", result, len(provider.requests))
	}
	request := provider.requests[0]
	if !strings.Contains(request.System, definition.SystemPrompt) {
		t.Fatalf("custom system prompt was not used: %q", request.System)
	}
	if len(request.Tools) != 2 || request.Tools[0].Name != "read" || request.Tools[1].Name != "grep" {
		t.Fatalf("custom tool allowlist was not used: %+v", request.Tools)
	}
}
