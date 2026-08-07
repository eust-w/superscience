package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"superscience/internal/mcp"
	"superscience/internal/store"
)

func TestUnconfiguredServerDoesNotFabricateCapabilities(t *testing.T) {
	discovery, err := mcp.DiscoverCapabilities(context.Background(), store.MCPServer{
		ID:        "unconfigured",
		Name:      "Unconfigured MCP",
		Transport: "stdio",
		Enabled:   true,
	})
	if err == nil {
		t.Fatal("expected an explicit configuration error")
	}
	if len(discovery.Tools) != 0 || len(discovery.Resources) != 0 || len(discovery.Prompts) != 0 {
		t.Fatalf("fabricated discovery = %+v", discovery)
	}
}

func TestUnconfiguredServerDoesNotFabricateExecution(t *testing.T) {
	result, err := mcp.Execute(context.Background(), store.MCPServer{
		ID:        "unconfigured",
		Name:      "Unconfigured MCP",
		Transport: "stdio",
		Enabled:   true,
	}, "lookup", map[string]any{"id": "EGFR"}, "", nil)
	if err == nil {
		t.Fatal("expected an explicit configuration error")
	}
	if result.Status == "ok" || strings.Contains(result.Output, "EGFR") {
		t.Fatalf("fabricated execution = %+v", result)
	}
}

func TestStreamableHTTPMaintainsOneInitializedSession(t *testing.T) {
	state := newFixtureState()
	fixture := fixtureServer(state)
	httpServer := httptest.NewServer(mcpsdk.NewStreamableHTTPHandler(func(_ *http.Request) *mcpsdk.Server {
		return fixture
	}, nil))
	defer func() {
		_ = mcp.CloseAll()
		httpServer.Close()
	}()

	server := store.MCPServer{
		ID:        "http-fixture",
		Name:      "HTTP Fixture",
		Transport: "streamable-http",
		URL:       httpServer.URL,
		Enabled:   true,
	}
	discovery, err := mcp.DiscoverCapabilities(context.Background(), server)
	if err != nil {
		t.Fatal(err)
	}
	if discovery.ProtocolVersion == "" || discovery.ServerName != "superscience-test-mcp" {
		t.Fatalf("metadata = %+v", discovery)
	}
	if len(discovery.Tools) != 3 || discovery.Tools[0].InputSchema["type"] != "object" {
		t.Fatalf("tools = %+v", discovery.Tools)
	}
	if len(discovery.Resources) != 1 || discovery.Resources[0].URI != "file:///dataset.csv" {
		t.Fatalf("resources = %+v", discovery.Resources)
	}
	if len(discovery.Prompts) != 1 || discovery.Prompts[0].Name != "summarize" {
		t.Fatalf("prompts = %+v", discovery.Prompts)
	}

	first, err := mcp.Execute(context.Background(), server, "increment", map[string]any{}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := mcp.Execute(context.Background(), server, "increment", map[string]any{}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.Output != "count=1" || second.Output != "count=2" {
		t.Fatalf("persistent counts = %q, %q", first.Output, second.Output)
	}
	if state.sessionCount() != 1 {
		t.Fatalf("session count = %d", state.sessionCount())
	}
}

func TestMCPToolResourcePromptAndToolErrorUseProtocolResults(t *testing.T) {
	fixture := fixtureServer(newFixtureState())
	httpServer := httptest.NewServer(mcpsdk.NewStreamableHTTPHandler(func(_ *http.Request) *mcpsdk.Server {
		return fixture
	}, nil))
	defer func() {
		_ = mcp.CloseAll()
		httpServer.Close()
	}()
	server := store.MCPServer{ID: "protocol-fixture", Transport: "http", URL: httpServer.URL, Enabled: true}

	tool, err := mcp.Execute(context.Background(), server, "lookup", map[string]any{"id": "EGFR"}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if tool.Status != "ok" || tool.Output != "lookup EGFR" || len(tool.Content) != 1 {
		t.Fatalf("tool result = %+v", tool)
	}

	resource, err := mcp.ReadResource(context.Background(), server, "file:///dataset.csv", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if resource.Status != "ok" || resource.Text != "gene,score\nEGFR,0.91" || len(resource.Contents) != 1 {
		t.Fatalf("resource result = %+v", resource)
	}

	prompt, err := mcp.GetPrompt(context.Background(), server, "summarize", map[string]any{"topic": "EGFR"}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if prompt.Status != "ok" || prompt.Text != "Summarize EGFR evidence" || len(prompt.Messages) != 1 {
		t.Fatalf("prompt result = %+v", prompt)
	}

	failed, err := mcp.Execute(context.Background(), server, "fail", map[string]any{}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != "error" || !failed.IsError || failed.Output != "fixture rejected the request" {
		t.Fatalf("tool error = %+v", failed)
	}
}

func TestStdioUsesStructuredArgumentsAndKeepsProcessAlive(t *testing.T) {
	t.Setenv("SUPERSCIENCE_MCP_STDIO_HELPER", "1")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	server := store.MCPServer{
		ID:        "stdio-fixture",
		Transport: "stdio",
		Command:   executable,
		Args:      []string{"-test.run=^TestMCPStdioHelper$"},
		Enabled:   true,
	}
	defer mcp.CloseAll()

	tools, err := mcp.Discover(context.Background(), server)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 3 {
		t.Fatalf("tools = %+v", tools)
	}
	first, err := mcp.Execute(context.Background(), server, "increment", map[string]any{}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := mcp.Execute(context.Background(), server, "increment", map[string]any{}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.Output != "count=1" || second.Output != "count=2" {
		t.Fatalf("stdio counts = %q, %q", first.Output, second.Output)
	}
}

func TestStdioAppliesEphemeralEnvironmentWithoutShellExpansion(t *testing.T) {
	t.Setenv("SUPERSCIENCE_MCP_STDIO_HELPER", "1")
	t.Setenv("SUPERSCIENCE_MCP_EXPECT_CHILD_ENV", "acp-session-secret")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	server := store.MCPServer{
		ID:        "stdio-env-fixture",
		Transport: "stdio",
		Command:   executable,
		Args:      []string{"-test.run=^TestMCPStdioHelper$"},
		Env:       []string{"ACP_MCP_TOKEN=acp-session-secret"},
		Enabled:   true,
	}
	defer mcp.CloseAll()
	if _, err := mcp.Discover(context.Background(), server); err != nil {
		t.Fatal(err)
	}
}

func TestStreamableHTTPAppliesEphemeralHeaders(t *testing.T) {
	fixture := fixtureServer(newFixtureState())
	var observed string
	httpServer := httptest.NewServer(mcpsdk.NewStreamableHTTPHandler(func(request *http.Request) *mcpsdk.Server {
		observed = request.Header.Get("X-ACP-Session")
		return fixture
	}, nil))
	defer func() {
		_ = mcp.CloseAll()
		httpServer.Close()
	}()
	server := store.MCPServer{
		ID: "http-header-fixture", Transport: "streamable-http", URL: httpServer.URL, Enabled: true,
		Headers: map[string]string{"X-ACP-Session": "session-token"},
	}
	if _, err := mcp.Discover(context.Background(), server); err != nil {
		t.Fatal(err)
	}
	if observed != "session-token" {
		t.Fatalf("observed header = %q", observed)
	}
}

func TestStdioDoesNotSplitCommandString(t *testing.T) {
	t.Setenv("SUPERSCIENCE_MCP_STDIO_HELPER", "1")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err = mcp.Discover(ctx, store.MCPServer{
		ID:        "unsafe-command",
		Transport: "stdio",
		Command:   executable + " -test.run=^TestMCPStdioHelper$",
		Enabled:   true,
	})
	if err == nil {
		t.Fatal("expected executable lookup to fail instead of splitting a shell command")
	}
}

func TestMCPStdioHelper(t *testing.T) {
	if os.Getenv("SUPERSCIENCE_MCP_STDIO_HELPER") != "1" {
		return
	}
	if expected := os.Getenv("SUPERSCIENCE_MCP_EXPECT_CHILD_ENV"); expected != "" && os.Getenv("ACP_MCP_TOKEN") != expected {
		t.Fatalf("child environment was not forwarded")
	}
	if err := fixtureServer(newFixtureState()).Run(context.Background(), &mcpsdk.StdioTransport{}); err != nil {
		t.Fatal(err)
	}
}

type fixtureState struct {
	mu     sync.Mutex
	counts map[string]int
}

func newFixtureState() *fixtureState {
	return &fixtureState{counts: map[string]int{}}
}

func (s *fixtureState) next(sessionID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counts[sessionID]++
	return s.counts[sessionID]
}

func (s *fixtureState) sessionCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.counts)
}

func fixtureServer(state *fixtureState) *mcpsdk.Server {
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "superscience-test-mcp", Version: "1.0.0"}, nil)
	objectSchema := map[string]any{"type": "object", "properties": map[string]any{}}
	server.AddTool(&mcpsdk.Tool{
		Name:        "increment",
		Description: "Increment a session-local counter.",
		InputSchema: objectSchema,
	}, func(_ context.Context, request *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		count := state.next(request.Session.ID())
		return &mcpsdk.CallToolResult{
			Content:           []mcpsdk.Content{&mcpsdk.TextContent{Text: fmt.Sprintf("count=%d", count)}},
			StructuredContent: map[string]any{"count": count},
		}, nil
	})
	server.AddTool(&mcpsdk.Tool{
		Name: "lookup",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"id": map[string]any{"type": "string"}},
			"required":   []string{"id"},
		},
	}, func(_ context.Context, request *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		var input struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(request.Params.Arguments, &input); err != nil {
			return nil, err
		}
		return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "lookup " + input.ID}}}, nil
	})
	server.AddTool(&mcpsdk.Tool{Name: "fail", InputSchema: objectSchema}, func(context.Context, *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "fixture rejected the request"}},
			IsError: true,
		}, nil
	})
	server.AddResource(&mcpsdk.Resource{
		URI:         "file:///dataset.csv",
		Name:        "Dataset",
		Description: "Experiment metadata",
		MIMEType:    "text/csv",
	}, func(_ context.Context, request *mcpsdk.ReadResourceRequest) (*mcpsdk.ReadResourceResult, error) {
		return &mcpsdk.ReadResourceResult{Contents: []*mcpsdk.ResourceContents{{
			URI: request.Params.URI, MIMEType: "text/csv", Text: "gene,score\nEGFR,0.91",
		}}}, nil
	})
	server.AddPrompt(&mcpsdk.Prompt{
		Name:        "summarize",
		Description: "Summarize selected records",
		Arguments:   []*mcpsdk.PromptArgument{{Name: "topic", Required: true}},
	}, func(_ context.Context, request *mcpsdk.GetPromptRequest) (*mcpsdk.GetPromptResult, error) {
		return &mcpsdk.GetPromptResult{
			Description: "Summarize selected records",
			Messages: []*mcpsdk.PromptMessage{{
				Role: "user", Content: &mcpsdk.TextContent{Text: "Summarize " + request.Params.Arguments["topic"] + " evidence"},
			}},
		}, nil
	})
	return server
}
