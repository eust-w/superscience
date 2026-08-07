package acp_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	acpsdk "github.com/coder/acp-go-sdk"

	superacp "superscience/internal/acp"
	agentpkg "superscience/internal/agent"
	"superscience/internal/providers"
	"superscience/internal/science"
	"superscience/internal/store"
)

type protocolProvider struct {
	mu    sync.Mutex
	calls int
}

func (p *protocolProvider) Stream(_ context.Context, _ providers.ChatRequest, onEvent func(providers.StreamEvent)) (agentpkg.ProviderResult, error) {
	p.mu.Lock()
	p.calls++
	call := p.calls
	p.mu.Unlock()
	if call == 1 {
		return agentpkg.ProviderResult{ToolCalls: []agentpkg.ProviderToolCall{{
			ID: "provider_call_1", Name: "write", Arguments: `{"path":"acp-note.md","content":"verified through ACP\n"}`,
		}}}, nil
	}
	if onEvent != nil {
		onEvent(providers.StreamEvent{Type: "text_delta", Delta: "ACP provider complete"})
	}
	return agentpkg.ProviderResult{Text: "ACP provider complete", Usage: agentpkg.ProviderUsage{PromptTokens: 4, CompletionTokens: 3, TotalTokens: 7}}, nil
}

type protocolClient struct {
	mu          sync.Mutex
	updates     []acpsdk.SessionNotification
	permissions int
}

func (c *protocolClient) ReadTextFile(context.Context, acpsdk.ReadTextFileRequest) (acpsdk.ReadTextFileResponse, error) {
	return acpsdk.ReadTextFileResponse{}, acpsdk.NewMethodNotFound(acpsdk.ClientMethodFsReadTextFile)
}
func (c *protocolClient) WriteTextFile(context.Context, acpsdk.WriteTextFileRequest) (acpsdk.WriteTextFileResponse, error) {
	return acpsdk.WriteTextFileResponse{}, acpsdk.NewMethodNotFound(acpsdk.ClientMethodFsWriteTextFile)
}
func (c *protocolClient) RequestPermission(_ context.Context, _ acpsdk.RequestPermissionRequest) (acpsdk.RequestPermissionResponse, error) {
	c.mu.Lock()
	c.permissions++
	c.mu.Unlock()
	return acpsdk.RequestPermissionResponse{Outcome: acpsdk.NewRequestPermissionOutcomeSelected(acpsdk.PermissionOptionId("allow-once"))}, nil
}
func (c *protocolClient) SessionUpdate(_ context.Context, update acpsdk.SessionNotification) error {
	c.mu.Lock()
	c.updates = append(c.updates, update)
	c.mu.Unlock()
	return nil
}
func (c *protocolClient) CreateTerminal(context.Context, acpsdk.CreateTerminalRequest) (acpsdk.CreateTerminalResponse, error) {
	return acpsdk.CreateTerminalResponse{}, acpsdk.NewMethodNotFound(acpsdk.ClientMethodTerminalCreate)
}
func (c *protocolClient) KillTerminal(context.Context, acpsdk.KillTerminalRequest) (acpsdk.KillTerminalResponse, error) {
	return acpsdk.KillTerminalResponse{}, acpsdk.NewMethodNotFound(acpsdk.ClientMethodTerminalKill)
}
func (c *protocolClient) TerminalOutput(context.Context, acpsdk.TerminalOutputRequest) (acpsdk.TerminalOutputResponse, error) {
	return acpsdk.TerminalOutputResponse{}, acpsdk.NewMethodNotFound(acpsdk.ClientMethodTerminalOutput)
}
func (c *protocolClient) ReleaseTerminal(context.Context, acpsdk.ReleaseTerminalRequest) (acpsdk.ReleaseTerminalResponse, error) {
	return acpsdk.ReleaseTerminalResponse{}, acpsdk.NewMethodNotFound(acpsdk.ClientMethodTerminalRelease)
}
func (c *protocolClient) WaitForTerminalExit(context.Context, acpsdk.WaitForTerminalExitRequest) (acpsdk.WaitForTerminalExitResponse, error) {
	return acpsdk.WaitForTerminalExitResponse{}, acpsdk.NewMethodNotFound(acpsdk.ClientMethodTerminalWaitForExit)
}

func (c *protocolClient) snapshot() ([]acpsdk.SessionNotification, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]acpsdk.SessionNotification(nil), c.updates...), c.permissions
}

func TestACPProtocolRunsPersistedAgentWithPermissionAndReplay(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	root := t.TempDir()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "acp.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.SetSettings(ctx, map[string]string{"defaultProvider": "protocol-provider", "defaultModel": "protocol-model"}); err != nil {
		t.Fatal(err)
	}
	provider := &protocolProvider{}
	agent := superacp.New(superacp.Options{
		Store: db, Science: science.NewRegistry(nil),
		ProviderFactory: func(context.Context, agentpkg.RunRequest) (agentpkg.Provider, error) { return provider, nil },
		Version:         "test",
	})
	client := &protocolClient{}

	agentReader, clientWriter := io.Pipe()
	clientReader, agentWriter := io.Pipe()
	defer agentReader.Close()
	defer clientWriter.Close()
	defer clientReader.Close()
	defer agentWriter.Close()
	agentConn := acpsdk.NewAgentSideConnection(agent, agentWriter, agentReader)
	agent.SetConnection(agentConn)
	clientConn := acpsdk.NewClientSideConnection(client, clientWriter, clientReader)

	initialized, err := clientConn.Initialize(ctx, acpsdk.InitializeRequest{ProtocolVersion: acpsdk.ProtocolVersionNumber})
	if err != nil {
		t.Fatal(err)
	}
	if initialized.ProtocolVersion != acpsdk.ProtocolVersionNumber || !initialized.AgentCapabilities.LoadSession || initialized.AgentCapabilities.SessionCapabilities.List == nil {
		t.Fatalf("initialize = %+v", initialized)
	}
	created, err := clientConn.NewSession(ctx, acpsdk.NewSessionRequest{Cwd: root, McpServers: []acpsdk.McpServer{}})
	if err != nil {
		t.Fatal(err)
	}
	response, err := clientConn.Prompt(ctx, acpsdk.PromptRequest{SessionId: created.SessionId, Prompt: []acpsdk.ContentBlock{acpsdk.TextBlock("Create a verified ACP note.")}})
	if err != nil {
		t.Fatal(err)
	}
	if response.StopReason != acpsdk.StopReasonEndTurn || response.Usage == nil || response.Usage.TotalTokens != 7 {
		t.Fatalf("prompt response = %+v", response)
	}
	written, err := os.ReadFile(filepath.Join(root, "acp-note.md"))
	if err != nil || string(written) != "verified through ACP\n" {
		t.Fatalf("written = %q err=%v", written, err)
	}
	updates, permissionCount := client.snapshot()
	if permissionCount != 1 {
		t.Fatalf("permission requests = %d", permissionCount)
	}
	var sawPlan, sawTool, sawCompletedTool, sawAnswer bool
	for _, notification := range updates {
		switch {
		case notification.Update.Plan != nil:
			sawPlan = true
		case notification.Update.ToolCall != nil:
			sawTool = true
		case notification.Update.ToolCallUpdate != nil && notification.Update.ToolCallUpdate.Status != nil && *notification.Update.ToolCallUpdate.Status == acpsdk.ToolCallStatusCompleted:
			sawCompletedTool = true
		case notification.Update.AgentMessageChunk != nil && notification.Update.AgentMessageChunk.Content.Text != nil && notification.Update.AgentMessageChunk.Content.Text.Text == "ACP provider complete":
			sawAnswer = true
		}
	}
	if !sawPlan || !sawTool || !sawCompletedTool || !sawAnswer {
		t.Fatalf("updates missing plan=%v tool=%v completed=%v answer=%v: %+v", sawPlan, sawTool, sawCompletedTool, sawAnswer, updates)
	}

	listed, err := clientConn.ListSessions(ctx, acpsdk.ListSessionsRequest{Cwd: acpsdk.Ptr(root)})
	if err != nil || len(listed.Sessions) != 1 || listed.Sessions[0].SessionId != created.SessionId {
		t.Fatalf("sessions = %+v err=%v", listed, err)
	}
	if _, err := clientConn.LoadSession(ctx, acpsdk.LoadSessionRequest{SessionId: created.SessionId, Cwd: root, McpServers: []acpsdk.McpServer{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := clientConn.SetSessionMode(ctx, acpsdk.SetSessionModeRequest{SessionId: created.SessionId, ModeId: acpsdk.SessionModeId("plan")}); err != nil {
		t.Fatal(err)
	}
	if _, err := clientConn.CloseSession(ctx, acpsdk.CloseSessionRequest{SessionId: created.SessionId}); err != nil {
		t.Fatal(err)
	}
	if _, err := clientConn.ResumeSession(ctx, acpsdk.ResumeSessionRequest{SessionId: created.SessionId, Cwd: root}); err != nil {
		t.Fatal(err)
	}
	session, err := db.GetSession(ctx, string(created.SessionId))
	if err != nil || session.Agent != "plan" {
		t.Fatalf("persisted session = %+v err=%v", session, err)
	}
	messages, err := db.ListMessages(ctx, string(created.SessionId))
	if err != nil || len(messages) != 2 || messages[0].Role != "user" || messages[1].Role != "assistant" {
		t.Fatalf("messages = %+v err=%v", messages, err)
	}
}
