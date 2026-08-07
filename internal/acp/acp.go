package acp

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	acpsdk "github.com/coder/acp-go-sdk"

	agentpkg "superscience/internal/agent"
	"superscience/internal/mcp"
	"superscience/internal/permissions"
	"superscience/internal/providers"
	"superscience/internal/science"
	"superscience/internal/secrets"
	"superscience/internal/skills"
	"superscience/internal/store"
	"superscience/internal/tools"
)

const maxPromptBytes = 2 << 20

type Options struct {
	Store           *store.Store
	Science         *science.Registry
	Secrets         secrets.Backend
	ProviderFactory func(context.Context, agentpkg.RunRequest) (agentpkg.Provider, error)
	Version         string
}

type sessionState struct {
	root             string
	agent            string
	provider         string
	model            string
	mcpServers       map[string]store.MCPServer
	mcpContext       string
	cancel           context.CancelFunc
	cancelGeneration uint64
}

type Agent struct {
	store           *store.Store
	science         *science.Registry
	secrets         secrets.Backend
	providerFactory func(context.Context, agentpkg.RunRequest) (agentpkg.Provider, error)
	version         string

	mu       sync.Mutex
	conn     *acpsdk.AgentSideConnection
	sessions map[string]*sessionState
}

var _ acpsdk.Agent = (*Agent)(nil)
var _ acpsdk.AgentLoader = (*Agent)(nil)

func New(options Options) *Agent {
	secretStore := options.Secrets
	if secretStore == nil {
		secretStore = secrets.OSBackend{}
	}
	registry := options.Science
	if registry == nil {
		registry = science.NewRegistry(nil)
	}
	version := strings.TrimSpace(options.Version)
	if version == "" {
		version = "dev"
	}
	return &Agent{
		store: options.Store, science: registry, secrets: secretStore, providerFactory: options.ProviderFactory,
		version: version, sessions: map[string]*sessionState{},
	}
}

func (a *Agent) SetConnection(connection *acpsdk.AgentSideConnection) {
	a.mu.Lock()
	a.conn = connection
	a.mu.Unlock()
}

func Serve(ctx context.Context, input io.Reader, output io.Writer, options Options) error {
	if options.Store == nil {
		return errors.New("ACP store is required")
	}
	agent := New(options)
	trackedInput, trackedOutput, outputErrors := drainACPRequests(ctx, input, output)
	connection := acpsdk.NewAgentSideConnection(agent, trackedOutput, trackedInput)
	agent.SetConnection(connection)
	select {
	case <-ctx.Done():
		agent.Shutdown()
		return ctx.Err()
	case err := <-outputErrors:
		agent.Shutdown()
		return fmt.Errorf("ACP protocol output: %w", err)
	case <-connection.Done():
		agent.Shutdown()
		select {
		case err := <-outputErrors:
			return fmt.Errorf("ACP protocol output: %w", err)
		default:
		}
		return nil
	}
}

type rpcEnvelope struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
}

type requestDrain struct {
	mu      sync.Mutex
	pending map[string]int
	wait    sync.WaitGroup
}

func newRequestDrain() *requestDrain {
	return &requestDrain{pending: map[string]int{}}
}

func (d *requestDrain) add(line []byte) {
	var message rpcEnvelope
	if json.Unmarshal(line, &message) != nil || strings.TrimSpace(message.Method) == "" || len(message.ID) == 0 {
		return
	}
	id := string(bytes.TrimSpace(message.ID))
	d.mu.Lock()
	d.pending[id]++
	d.wait.Add(1)
	d.mu.Unlock()
}

func (d *requestDrain) complete(payload []byte) {
	for _, line := range bytes.Split(payload, []byte{'\n'}) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var message rpcEnvelope
		if json.Unmarshal(line, &message) != nil || strings.TrimSpace(message.Method) != "" || len(message.ID) == 0 {
			continue
		}
		id := string(bytes.TrimSpace(message.ID))
		d.mu.Lock()
		if d.pending[id] > 0 {
			d.pending[id]--
			if d.pending[id] == 0 {
				delete(d.pending, id)
			}
			d.wait.Done()
		}
		d.mu.Unlock()
	}
}

type trackedOutput struct {
	writer io.Writer
	drain  *requestDrain
	failed chan error
	once   sync.Once
}

func (w *trackedOutput) Write(payload []byte) (int, error) {
	written, err := w.writer.Write(payload)
	if err != nil {
		w.once.Do(func() { w.failed <- err })
	}
	// A response attempt completes the inbound request even when the peer has
	// already closed its output pipe and the write itself fails.
	w.drain.complete(payload)
	return written, err
}

func drainACPRequests(ctx context.Context, input io.Reader, output io.Writer) (io.Reader, io.Writer, <-chan error) {
	drain := newRequestDrain()
	outputErrors := make(chan error, 1)
	reader, writer := io.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		scanner := bufio.NewScanner(input)
		scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
		for scanner.Scan() {
			line := append([]byte(nil), scanner.Bytes()...)
			drain.add(line)
			line = append(line, '\n')
			if _, err := writer.Write(line); err != nil {
				return
			}
		}
		if err := scanner.Err(); err != nil {
			_ = writer.CloseWithError(err)
			return
		}
		drain.wait.Wait()
		_ = writer.Close()
	}()
	go func() {
		select {
		case <-ctx.Done():
			_ = writer.CloseWithError(ctx.Err())
		case <-done:
		}
	}()
	return reader, &trackedOutput{writer: output, drain: drain, failed: outputErrors}, outputErrors
}

func (a *Agent) Shutdown() {
	a.mu.Lock()
	states := make([]*sessionState, 0, len(a.sessions))
	for _, state := range a.sessions {
		states = append(states, state)
	}
	a.sessions = map[string]*sessionState{}
	a.mu.Unlock()
	for _, state := range states {
		if state.cancel != nil {
			state.cancel()
		}
		closeMCPServers(state.mcpServers)
	}
}

func (a *Agent) Initialize(_ context.Context, request acpsdk.InitializeRequest) (acpsdk.InitializeResponse, error) {
	protocolVersion := request.ProtocolVersion
	if protocolVersion != acpsdk.ProtocolVersionNumber {
		protocolVersion = acpsdk.ProtocolVersionNumber
	}
	title := "Superscience"
	return acpsdk.InitializeResponse{
		ProtocolVersion: protocolVersion,
		AgentInfo:       &acpsdk.Implementation{Name: "superscience", Title: &title, Version: a.version},
		AgentCapabilities: acpsdk.AgentCapabilities{
			LoadSession: true,
			PromptCapabilities: acpsdk.PromptCapabilities{
				EmbeddedContext: true,
			},
			McpCapabilities: acpsdk.McpCapabilities{Http: true, Sse: true},
			SessionCapabilities: acpsdk.SessionCapabilities{
				List: &acpsdk.SessionListCapabilities{}, Resume: &acpsdk.SessionResumeCapabilities{}, Close: &acpsdk.SessionCloseCapabilities{},
			},
		},
		AuthMethods: []acpsdk.AuthMethod{},
	}, nil
}

func (a *Agent) Authenticate(context.Context, acpsdk.AuthenticateRequest) (acpsdk.AuthenticateResponse, error) {
	return acpsdk.AuthenticateResponse{}, acpsdk.NewMethodNotFound(acpsdk.AgentMethodAuthenticate)
}

func (a *Agent) Logout(context.Context, acpsdk.LogoutRequest) (acpsdk.LogoutResponse, error) {
	return acpsdk.LogoutResponse{}, acpsdk.NewMethodNotFound(acpsdk.AgentMethodLogout)
}

func (a *Agent) NewSession(ctx context.Context, request acpsdk.NewSessionRequest) (acpsdk.NewSessionResponse, error) {
	if len(request.AdditionalDirectories) > 0 {
		return acpsdk.NewSessionResponse{}, acpsdk.NewInvalidParams(map[string]any{"error": "additionalDirectories are not advertised by Superscience"})
	}
	root, err := canonicalDirectory(request.Cwd)
	if err != nil {
		return acpsdk.NewSessionResponse{}, acpsdk.NewInvalidParams(map[string]any{"error": err.Error()})
	}
	project, err := a.store.OpenProject(ctx, root)
	if err != nil {
		return acpsdk.NewSessionResponse{}, err
	}
	settings, err := a.store.ListSettings(ctx)
	if err != nil {
		return acpsdk.NewSessionResponse{}, err
	}
	record, err := a.store.CreateSession(ctx, store.CreateSessionInput{ProjectID: project.ID, Title: "ACP research session", Agent: "research"})
	if err != nil {
		return acpsdk.NewSessionResponse{}, err
	}
	mcpServers, mcpContext, err := configureMCPServers(ctx, record.ID, request.McpServers)
	if err != nil {
		_ = a.store.DeleteSession(ctx, record.ID)
		return acpsdk.NewSessionResponse{}, err
	}
	state := &sessionState{
		root: root, agent: record.Agent, provider: settings["defaultProvider"], model: settings["defaultModel"],
		mcpServers: mcpServers, mcpContext: mcpContext,
	}
	a.mu.Lock()
	a.sessions[record.ID] = state
	a.mu.Unlock()
	return acpsdk.NewSessionResponse{SessionId: acpsdk.SessionId(record.ID), Modes: sessionModes(record.Agent)}, nil
}

func (a *Agent) Prompt(ctx context.Context, request acpsdk.PromptRequest) (acpsdk.PromptResponse, error) {
	sessionID := string(request.SessionId)
	state, err := a.ensureSession(ctx, sessionID, "")
	if err != nil {
		return acpsdk.PromptResponse{}, err
	}
	goal, err := promptText(request.Prompt)
	if err != nil {
		return acpsdk.PromptResponse{}, acpsdk.NewInvalidParams(map[string]any{"error": err.Error()})
	}
	if state.provider == "" || state.model == "" {
		return acpsdk.PromptResponse{}, errors.New("Superscience ACP requires a configured provider and model")
	}
	turnCtx, cancel := context.WithCancel(ctx)
	cancelGeneration := a.setSessionCancel(sessionID, cancel)
	defer func() {
		cancel()
		a.clearSessionCancel(sessionID, cancelGeneration)
	}()

	if err := a.notify(turnCtx, sessionID, acpsdk.UpdatePlan(acpsdk.PlanEntry{
		Content: "Run the Superscience research loop", Priority: acpsdk.PlanEntryPriorityHigh, Status: acpsdk.PlanEntryStatusInProgress,
	})); err != nil {
		return acpsdk.PromptResponse{}, err
	}
	runtime := a.runtimeFor(state)
	result, err := a.runOrAnswer(turnCtx, runtime, sessionID, goal, state)
	if err != nil {
		if errors.Is(err, context.Canceled) || turnCtx.Err() != nil {
			return acpsdk.PromptResponse{StopReason: acpsdk.StopReasonCancelled}, nil
		}
		return acpsdk.PromptResponse{}, err
	}
	emitted := map[string]string{}
	for approvals := 0; result.Status == "waiting_permission"; approvals++ {
		if approvals >= 32 {
			return acpsdk.PromptResponse{}, errors.New("ACP run exceeded the permission checkpoint limit")
		}
		if err := a.emitToolCalls(turnCtx, sessionID, result.ID, emitted); err != nil {
			return acpsdk.PromptResponse{}, err
		}
		cancelled, err := a.resolvePermission(turnCtx, sessionID, result.ID, result.PermissionID)
		if err != nil {
			return acpsdk.PromptResponse{}, err
		}
		if cancelled {
			return acpsdk.PromptResponse{StopReason: acpsdk.StopReasonCancelled}, nil
		}
		result, err = runtime.Resume(turnCtx, result.ID)
		if err != nil {
			if errors.Is(err, context.Canceled) || turnCtx.Err() != nil {
				return acpsdk.PromptResponse{StopReason: acpsdk.StopReasonCancelled}, nil
			}
			return acpsdk.PromptResponse{}, err
		}
	}
	if err := a.emitToolCalls(turnCtx, sessionID, result.ID, emitted); err != nil {
		return acpsdk.PromptResponse{}, err
	}
	if result.Status == "waiting_input" {
		question, questionErr := a.pendingQuestion(turnCtx, result.ID)
		if questionErr != nil {
			return acpsdk.PromptResponse{}, questionErr
		}
		if err := a.notify(turnCtx, sessionID, acpsdk.UpdateAgentMessageText(question)); err != nil {
			return acpsdk.PromptResponse{}, err
		}
		return acpsdk.PromptResponse{StopReason: acpsdk.StopReasonEndTurn, UserMessageId: request.MessageId}, nil
	}
	if strings.TrimSpace(result.Message.Content) == "" {
		return acpsdk.PromptResponse{}, errors.New("agent completed without assistant text")
	}
	if err := a.notify(turnCtx, sessionID, acpsdk.UpdateAgentMessageText(result.Message.Content)); err != nil {
		return acpsdk.PromptResponse{}, err
	}
	_ = a.notify(turnCtx, sessionID, acpsdk.UpdatePlan(acpsdk.PlanEntry{
		Content: "Run the Superscience research loop", Priority: acpsdk.PlanEntryPriorityHigh, Status: acpsdk.PlanEntryStatusCompleted,
	}))
	usage := a.runUsage(turnCtx, result.ID)
	return acpsdk.PromptResponse{StopReason: stopReason(result.Status), Usage: usage, UserMessageId: request.MessageId}, nil
}

func (a *Agent) Cancel(_ context.Context, request acpsdk.CancelNotification) error {
	a.mu.Lock()
	state := a.sessions[string(request.SessionId)]
	var cancel context.CancelFunc
	if state != nil {
		cancel = state.cancel
	}
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

func (a *Agent) CloseSession(ctx context.Context, request acpsdk.CloseSessionRequest) (acpsdk.CloseSessionResponse, error) {
	sessionID := string(request.SessionId)
	if _, err := a.store.GetSession(ctx, sessionID); err != nil {
		return acpsdk.CloseSessionResponse{}, err
	}
	a.mu.Lock()
	state := a.sessions[sessionID]
	delete(a.sessions, sessionID)
	a.mu.Unlock()
	if state != nil {
		if state.cancel != nil {
			state.cancel()
		}
		closeMCPServers(state.mcpServers)
	}
	return acpsdk.CloseSessionResponse{}, nil
}

func (a *Agent) ResumeSession(ctx context.Context, request acpsdk.ResumeSessionRequest) (acpsdk.ResumeSessionResponse, error) {
	if len(request.AdditionalDirectories) > 0 {
		return acpsdk.ResumeSessionResponse{}, acpsdk.NewInvalidParams(map[string]any{"error": "additionalDirectories are not advertised by Superscience"})
	}
	state, err := a.restoreSession(ctx, string(request.SessionId), request.Cwd, request.McpServers)
	if err != nil {
		return acpsdk.ResumeSessionResponse{}, err
	}
	return acpsdk.ResumeSessionResponse{Modes: sessionModes(state.agent)}, nil
}

func (a *Agent) LoadSession(ctx context.Context, request acpsdk.LoadSessionRequest) (acpsdk.LoadSessionResponse, error) {
	if len(request.AdditionalDirectories) > 0 {
		return acpsdk.LoadSessionResponse{}, acpsdk.NewInvalidParams(map[string]any{"error": "additionalDirectories are not advertised by Superscience"})
	}
	state, err := a.restoreSession(ctx, string(request.SessionId), request.Cwd, request.McpServers)
	if err != nil {
		return acpsdk.LoadSessionResponse{}, err
	}
	messages, err := a.store.ListMessages(ctx, string(request.SessionId))
	if err != nil {
		return acpsdk.LoadSessionResponse{}, err
	}
	for _, message := range messages {
		update := acpsdk.UpdateAgentMessageText(message.Content)
		if message.Role == "user" {
			update = acpsdk.UpdateUserMessageText(message.Content)
		}
		if err := a.notify(ctx, string(request.SessionId), update); err != nil {
			return acpsdk.LoadSessionResponse{}, err
		}
	}
	return acpsdk.LoadSessionResponse{Modes: sessionModes(state.agent)}, nil
}

func (a *Agent) ListSessions(ctx context.Context, request acpsdk.ListSessionsRequest) (acpsdk.ListSessionsResponse, error) {
	if request.Cursor != nil && strings.TrimSpace(*request.Cursor) != "" {
		return acpsdk.ListSessionsResponse{}, acpsdk.NewInvalidParams(map[string]any{"error": "cursor is not valid for this complete result set"})
	}
	filter := ""
	if request.Cwd != nil {
		var err error
		filter, err = canonicalDirectory(*request.Cwd)
		if err != nil {
			return acpsdk.ListSessionsResponse{}, acpsdk.NewInvalidParams(map[string]any{"error": err.Error()})
		}
	}
	projects, err := a.store.ListProjects(ctx)
	if err != nil {
		return acpsdk.ListSessionsResponse{}, err
	}
	rows := make([]acpsdk.SessionInfo, 0)
	for _, project := range projects {
		root, err := canonicalDirectory(project.Path)
		if err != nil || (filter != "" && root != filter) {
			continue
		}
		sessions, err := a.store.ListSessions(ctx, project.ID)
		if err != nil {
			return acpsdk.ListSessionsResponse{}, err
		}
		for _, session := range sessions {
			title := session.Title
			updated := time.UnixMilli(session.UpdatedAt).UTC().Format(time.RFC3339Nano)
			rows = append(rows, acpsdk.SessionInfo{SessionId: acpsdk.SessionId(session.ID), Cwd: root, Title: &title, UpdatedAt: &updated})
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return *rows[i].UpdatedAt > *rows[j].UpdatedAt })
	return acpsdk.ListSessionsResponse{Sessions: rows}, nil
}

func (a *Agent) SetSessionMode(ctx context.Context, request acpsdk.SetSessionModeRequest) (acpsdk.SetSessionModeResponse, error) {
	mode := string(request.ModeId)
	if !isAgentMode(mode) {
		return acpsdk.SetSessionModeResponse{}, acpsdk.NewInvalidParams(map[string]any{"error": "unknown Superscience agent mode: " + mode})
	}
	session, err := a.store.GetSession(ctx, string(request.SessionId))
	if err != nil {
		return acpsdk.SetSessionModeResponse{}, err
	}
	session.Agent = mode
	if _, err := a.store.UpdateSession(ctx, session); err != nil {
		return acpsdk.SetSessionModeResponse{}, err
	}
	a.mu.Lock()
	if state := a.sessions[session.ID]; state != nil {
		state.agent = mode
	}
	a.mu.Unlock()
	return acpsdk.SetSessionModeResponse{}, a.notify(ctx, session.ID, acpsdk.SessionUpdate{CurrentModeUpdate: &acpsdk.SessionCurrentModeUpdate{CurrentModeId: request.ModeId}})
}

func (a *Agent) SetSessionConfigOption(context.Context, acpsdk.SetSessionConfigOptionRequest) (acpsdk.SetSessionConfigOptionResponse, error) {
	return acpsdk.SetSessionConfigOptionResponse{}, acpsdk.NewMethodNotFound(acpsdk.AgentMethodSessionSetConfigOption)
}

func (a *Agent) runtimeFor(state *sessionState) agentpkg.Runtime {
	factory := a.providerFactory
	if factory == nil {
		factory = func(ctx context.Context, request agentpkg.RunRequest) (agentpkg.Provider, error) {
			adapter, err := (providers.Resolver{Store: a.store, Secrets: a.secrets}).Resolve(ctx, request.ProviderID, request.Model)
			if err != nil {
				return nil, err
			}
			return agentpkg.AdapterProvider{Adapter: adapter, System: "You are Superscience running through ACP. Follow the selected agent contract and route risky actions through permission-gated tools."}, nil
		}
	}
	root := state.root
	return agentpkg.Runtime{
		Store: a.store, Science: a.science, Gate: persistentGate{store: a.store}, Secrets: a.secrets,
		ProjectRoot: root, ProviderFactory: factory, MCPServers: state.mcpServers,
		SkillCatalog: func(context.Context) ([]store.SkillRecord, error) {
			user, err := skills.LoadUser()
			if err != nil {
				return nil, err
			}
			project, err := skills.LoadProject(root)
			if err != nil {
				return nil, err
			}
			return skills.Merge(user, project), nil
		},
	}
}

func (a *Agent) runOrAnswer(ctx context.Context, runtime agentpkg.Runtime, sessionID, goal string, state *sessionState) (agentpkg.RunResult, error) {
	runs, err := a.store.ListAgentRuns(ctx, sessionID)
	if err != nil {
		return agentpkg.RunResult{}, err
	}
	if len(runs) > 0 && runs[0].Status == "waiting_input" {
		action, err := a.store.GetPendingActionByRun(ctx, runs[0].ID)
		if err != nil {
			return agentpkg.RunResult{}, err
		}
		return runtime.Answer(ctx, runs[0].ID, action.ID, goal)
	}
	return runtime.Run(ctx, sessionID, agentpkg.RunRequest{
		Goal: goal, Context: state.mcpContext, Agent: state.agent, ProviderID: state.provider, Model: state.model,
	})
}

func (a *Agent) resolvePermission(ctx context.Context, sessionID, runID, permissionID string) (bool, error) {
	if strings.TrimSpace(permissionID) == "" {
		return false, errors.New("run is waiting for permission without a permission ID")
	}
	permission, err := a.store.GetPermission(ctx, permissionID)
	if err != nil {
		return false, err
	}
	calls, err := a.store.ListToolCalls(ctx, runID)
	if err != nil {
		return false, err
	}
	var call store.ToolCall
	for _, candidate := range calls {
		if candidate.PermissionID == permissionID {
			call = candidate
			break
		}
	}
	if call.ID == "" {
		return false, errors.New("permission checkpoint has no matching tool call")
	}
	connection := a.connection()
	if connection == nil {
		return false, errors.New("ACP connection is unavailable")
	}
	kind := toolKind(call.Tool)
	status := acpsdk.ToolCallStatusPending
	title := permission.Message
	if strings.TrimSpace(title) == "" {
		title = call.Tool + " requires approval"
	}
	var rawInput any
	_ = json.Unmarshal([]byte(call.Input), &rawInput)
	response, err := connection.RequestPermission(ctx, acpsdk.RequestPermissionRequest{
		SessionId: acpsdk.SessionId(sessionID),
		ToolCall: acpsdk.ToolCallUpdate{
			ToolCallId: acpsdk.ToolCallId(call.ID), Title: &title, Kind: &kind, Status: &status, RawInput: rawInput,
		},
		Options: []acpsdk.PermissionOption{
			{OptionId: "allow-once", Name: "Allow once", Kind: acpsdk.PermissionOptionKindAllowOnce},
			{OptionId: "allow-always", Name: "Always allow this pattern", Kind: acpsdk.PermissionOptionKindAllowAlways},
			{OptionId: "reject-once", Name: "Deny", Kind: acpsdk.PermissionOptionKindRejectOnce},
		},
	})
	if err != nil {
		return false, err
	}
	if response.Outcome.Cancelled != nil {
		_ = a.store.UpdatePermissionStatus(ctx, permissionID, string(permissions.DecisionReject))
		return true, nil
	}
	if response.Outcome.Selected == nil {
		return false, errors.New("ACP client returned an empty permission outcome")
	}
	decision := ""
	switch string(response.Outcome.Selected.OptionId) {
	case "allow-once":
		decision = string(permissions.DecisionAllowOnce)
	case "allow-always":
		decision = string(permissions.DecisionAllowAlways)
	case "reject-once":
		decision = string(permissions.DecisionReject)
	default:
		return false, fmt.Errorf("ACP client selected unknown permission option %q", response.Outcome.Selected.OptionId)
	}
	return false, a.store.UpdatePermissionStatus(ctx, permissionID, decision)
}

func (a *Agent) emitToolCalls(ctx context.Context, sessionID, runID string, emitted map[string]string) error {
	calls, err := a.store.ListToolCalls(ctx, runID)
	if err != nil {
		return err
	}
	for _, call := range calls {
		kind := toolKind(call.Tool)
		status := toolStatus(call.Status)
		var rawInput any
		_ = json.Unmarshal([]byte(call.Input), &rawInput)
		if _, ok := emitted[call.ID]; !ok {
			if err := a.notify(ctx, sessionID, acpsdk.StartToolCall(acpsdk.ToolCallId(call.ID), call.Tool,
				acpsdk.WithStartKind(kind), acpsdk.WithStartStatus(status), acpsdk.WithStartRawInput(rawInput))); err != nil {
				return err
			}
		}
		if emitted[call.ID] == call.Status {
			continue
		}
		options := []acpsdk.ToolCallUpdateOpt{acpsdk.WithUpdateStatus(status), acpsdk.WithUpdateRawOutput(call.Output)}
		if strings.TrimSpace(call.Output) != "" {
			options = append(options, acpsdk.WithUpdateContent([]acpsdk.ToolCallContent{acpsdk.ToolContent(acpsdk.TextBlock(call.Output))}))
		}
		if err := a.notify(ctx, sessionID, acpsdk.UpdateToolCall(acpsdk.ToolCallId(call.ID), options...)); err != nil {
			return err
		}
		emitted[call.ID] = call.Status
	}
	return nil
}

func (a *Agent) pendingQuestion(ctx context.Context, runID string) (string, error) {
	action, err := a.store.GetPendingActionByRun(ctx, runID)
	if err != nil {
		return "", err
	}
	var payload struct {
		Question string `json:"question"`
	}
	if err := json.Unmarshal([]byte(action.Payload), &payload); err != nil {
		return "", err
	}
	if strings.TrimSpace(payload.Question) == "" {
		return "", errors.New("pending question is empty")
	}
	return payload.Question, nil
}

func (a *Agent) runUsage(ctx context.Context, runID string) *acpsdk.Usage {
	steps, err := a.store.ListAgentSteps(ctx, runID)
	if err != nil {
		return nil
	}
	usage := acpsdk.Usage{}
	for _, step := range steps {
		if step.Kind != "usage" {
			continue
		}
		var values struct {
			PromptTokens     int `json:"promptTokens"`
			CompletionTokens int `json:"completionTokens"`
			TotalTokens      int `json:"totalTokens"`
		}
		if json.Unmarshal([]byte(step.Metadata), &values) == nil {
			usage.InputTokens += values.PromptTokens
			usage.OutputTokens += values.CompletionTokens
			usage.TotalTokens += values.TotalTokens
		}
	}
	if usage.TotalTokens == 0 && usage.InputTokens == 0 && usage.OutputTokens == 0 {
		return nil
	}
	return &usage
}

func (a *Agent) restoreSession(ctx context.Context, sessionID, cwd string, configs []acpsdk.McpServer) (*sessionState, error) {
	state, err := a.ensureSession(ctx, sessionID, cwd)
	if err != nil {
		return nil, err
	}
	if configs != nil {
		servers, summary, err := configureMCPServers(ctx, sessionID, configs)
		if err != nil {
			return nil, err
		}
		closeMCPServers(state.mcpServers)
		state.mcpServers = servers
		state.mcpContext = summary
	}
	a.mu.Lock()
	a.sessions[sessionID] = state
	a.mu.Unlock()
	return state, nil
}

func (a *Agent) ensureSession(ctx context.Context, sessionID, cwd string) (*sessionState, error) {
	a.mu.Lock()
	state := a.sessions[sessionID]
	a.mu.Unlock()
	if state != nil {
		if cwd != "" {
			root, err := canonicalDirectory(cwd)
			if err != nil || root != state.root {
				return nil, errors.New("ACP session cwd does not match the persisted project")
			}
		}
		return state, nil
	}
	session, err := a.store.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	project, err := projectForSession(ctx, a.store, session)
	if err != nil {
		return nil, err
	}
	root, err := canonicalDirectory(project.Path)
	if err != nil {
		return nil, err
	}
	if cwd != "" {
		requested, err := canonicalDirectory(cwd)
		if err != nil || requested != root {
			return nil, errors.New("ACP session cwd does not match the persisted project")
		}
	}
	settings, err := a.store.ListSettings(ctx)
	if err != nil {
		return nil, err
	}
	return &sessionState{root: root, agent: session.Agent, provider: settings["defaultProvider"], model: settings["defaultModel"], mcpServers: map[string]store.MCPServer{}}, nil
}

func (a *Agent) notify(ctx context.Context, sessionID string, update acpsdk.SessionUpdate) error {
	connection := a.connection()
	if connection == nil {
		return errors.New("ACP connection is unavailable")
	}
	return connection.SessionUpdate(ctx, acpsdk.SessionNotification{SessionId: acpsdk.SessionId(sessionID), Update: update})
}

func (a *Agent) connection() *acpsdk.AgentSideConnection {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.conn
}

func (a *Agent) setSessionCancel(sessionID string, cancel context.CancelFunc) uint64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	if state := a.sessions[sessionID]; state != nil {
		if state.cancel != nil {
			state.cancel()
		}
		state.cancelGeneration++
		state.cancel = cancel
		return state.cancelGeneration
	}
	return 0
}

func (a *Agent) clearSessionCancel(sessionID string, generation uint64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if state := a.sessions[sessionID]; state != nil && state.cancel != nil && state.cancelGeneration == generation {
		state.cancel = nil
	}
}

type persistentGate struct{ store *store.Store }

func (g persistentGate) Ask(sessionID, typ, pattern, message string) string {
	record, err := g.store.CreatePermission(context.Background(), store.PermissionRecord{
		SessionID: sessionID, Type: typ, Pattern: pattern, Message: message, Status: "pending",
	})
	if err != nil {
		return ""
	}
	return record.ID
}

func (g persistentGate) IsAllowed(sessionID, typ, pattern string) bool {
	decision, allowed, err := g.store.ConsumePermissionDecision(context.Background(), sessionID, typ, pattern)
	return err == nil && allowed && (decision == string(permissions.DecisionAllowOnce) || decision == string(permissions.DecisionAllowAlways))
}

func configureMCPServers(ctx context.Context, sessionID string, configs []acpsdk.McpServer) (map[string]store.MCPServer, string, error) {
	if len(configs) > 32 {
		return nil, "", errors.New("ACP session exceeds the 32 MCP server limit")
	}
	servers := make(map[string]store.MCPServer, len(configs))
	summaries := make([]string, 0, len(configs))
	for index, config := range configs {
		server, err := convertMCPServer(sessionID, index, config)
		if err != nil {
			closeMCPServers(servers)
			return nil, "", err
		}
		discovery, err := mcp.DiscoverCapabilities(ctx, server)
		if err != nil {
			closeMCPServers(servers)
			return nil, "", fmt.Errorf("connect ACP MCP server %q: %w", server.Name, err)
		}
		servers[server.ID] = server
		toolNames := make([]string, 0, len(discovery.Tools))
		for _, tool := range discovery.Tools {
			toolNames = append(toolNames, tool.Name)
		}
		summaries = append(summaries, fmt.Sprintf("MCP server %s (%s), tools: %s", server.ID, server.Name, strings.Join(toolNames, ", ")))
	}
	return servers, strings.Join(summaries, "\n"), nil
}

func convertMCPServer(sessionID string, index int, config acpsdk.McpServer) (store.MCPServer, error) {
	server := store.MCPServer{Enabled: true, Status: "connected"}
	switch {
	case config.Stdio != nil:
		server.Name = config.Stdio.Name
		server.Transport = "stdio"
		server.Command = strings.TrimSpace(config.Stdio.Command)
		server.Args = append([]string(nil), config.Stdio.Args...)
		if !filepath.IsAbs(server.Command) {
			return store.MCPServer{}, fmt.Errorf("ACP stdio MCP command must be absolute: %s", server.Command)
		}
		for _, variable := range config.Stdio.Env {
			if !validEnvironmentName(variable.Name) || strings.ContainsRune(variable.Value, '\x00') {
				return store.MCPServer{}, fmt.Errorf("ACP MCP server %q has an invalid environment variable", server.Name)
			}
			server.Env = append(server.Env, variable.Name+"="+variable.Value)
		}
	case config.Http != nil:
		server.Name = config.Http.Name
		server.Transport = "streamable-http"
		server.URL = config.Http.Url
		server.Headers = headerMap(config.Http.Headers)
	case config.Sse != nil:
		server.Name = config.Sse.Name
		server.Transport = "sse"
		server.URL = config.Sse.Url
		server.Headers = headerMap(config.Sse.Headers)
	case config.Acp != nil:
		return store.MCPServer{}, errors.New("MCP-over-ACP is not advertised by Superscience")
	default:
		return store.MCPServer{}, errors.New("ACP MCP server has no transport configuration")
	}
	if strings.TrimSpace(server.Name) == "" {
		server.Name = fmt.Sprintf("server-%d", index+1)
	}
	if server.Transport != "stdio" {
		parsed, err := url.Parse(server.URL)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return store.MCPServer{}, fmt.Errorf("ACP MCP server %q has an invalid URL", server.Name)
		}
		for name, value := range server.Headers {
			if textproto.CanonicalMIMEHeaderKey(name) == "" || strings.ContainsAny(value, "\r\n") {
				return store.MCPServer{}, fmt.Errorf("ACP MCP server %q has an invalid HTTP header", server.Name)
			}
		}
	}
	server.ID = fmt.Sprintf("acp-%s-%d-%s", shortID(sessionID), index+1, slug(server.Name))
	return server, nil
}

func closeMCPServers(servers map[string]store.MCPServer) {
	for id := range servers {
		_ = mcp.CloseServer(id)
	}
}

func headerMap(headers []acpsdk.HttpHeader) map[string]string {
	result := make(map[string]string, len(headers))
	for _, header := range headers {
		result[header.Name] = header.Value
	}
	return result
}

func promptText(blocks []acpsdk.ContentBlock) (string, error) {
	var builder strings.Builder
	for _, block := range blocks {
		switch {
		case block.Text != nil:
			builder.WriteString(block.Text.Text)
		case block.ResourceLink != nil:
			fmt.Fprintf(&builder, "\n\nResource link: %s <%s>", block.ResourceLink.Name, block.ResourceLink.Uri)
		case block.Resource != nil && block.Resource.Resource.TextResourceContents != nil:
			resource := block.Resource.Resource.TextResourceContents
			fmt.Fprintf(&builder, "\n\nEmbedded resource %s:\n%s", resource.Uri, resource.Text)
		case block.Resource != nil:
			return "", errors.New("binary embedded resources are not supported")
		case block.Image != nil, block.Audio != nil:
			return "", errors.New("image and audio prompts are not advertised by Superscience ACP")
		default:
			return "", errors.New("unsupported ACP content block")
		}
		if builder.Len() > maxPromptBytes {
			return "", fmt.Errorf("ACP prompt exceeds the %d MiB limit", maxPromptBytes>>20)
		}
	}
	text := strings.TrimSpace(builder.String())
	if text == "" {
		return "", errors.New("ACP prompt contains no text or resource context")
	}
	return text, nil
}

func canonicalDirectory(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", errors.New("ACP cwd must be absolute")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("ACP cwd is not a directory")
	}
	return filepath.Clean(resolved), nil
}

func projectForSession(ctx context.Context, db *store.Store, session store.Session) (store.Project, error) {
	projects, err := db.ListProjects(ctx)
	if err != nil {
		return store.Project{}, err
	}
	for _, project := range projects {
		if project.ID == session.ProjectID {
			return project, nil
		}
	}
	return store.Project{}, sql.ErrNoRows
}

func sessionModes(current string) *acpsdk.SessionModeState {
	modes := make([]acpsdk.SessionMode, 0)
	for _, definition := range tools.BuiltInAgents() {
		if !definition.Enabled {
			continue
		}
		description := definition.Description
		modes = append(modes, acpsdk.SessionMode{Id: acpsdk.SessionModeId(definition.ID), Name: definition.Name, Description: &description})
	}
	return &acpsdk.SessionModeState{CurrentModeId: acpsdk.SessionModeId(current), AvailableModes: modes}
}

func isAgentMode(id string) bool {
	for _, definition := range tools.BuiltInAgents() {
		if definition.Enabled && definition.ID == id {
			return true
		}
	}
	return false
}

func toolKind(tool string) acpsdk.ToolKind {
	switch tool {
	case "read", "ls", "glob":
		return acpsdk.ToolKindRead
	case "write", "edit", "multiedit", "apply_patch":
		return acpsdk.ToolKindEdit
	case "grep", "codesearch", "science_search", "websearch":
		return acpsdk.ToolKindSearch
	case "webfetch", "mcp", "mcp_resource", "mcp_prompt":
		return acpsdk.ToolKindFetch
	case "bash", "notebook", "rkernel", "lsp":
		return acpsdk.ToolKindExecute
	case "plan", "planwrite", "todo", "question":
		return acpsdk.ToolKindThink
	default:
		return acpsdk.ToolKindOther
	}
}

func toolStatus(status string) acpsdk.ToolCallStatus {
	switch status {
	case "ok", "completed":
		return acpsdk.ToolCallStatusCompleted
	case "error", "permission_denied", "failed":
		return acpsdk.ToolCallStatusFailed
	case "running", "in_progress":
		return acpsdk.ToolCallStatusInProgress
	default:
		return acpsdk.ToolCallStatusPending
	}
}

func stopReason(status string) acpsdk.StopReason {
	switch status {
	case "aborted", "cancelled":
		return acpsdk.StopReasonCancelled
	case "max_steps":
		return acpsdk.StopReasonMaxTurnRequests
	case "review_failed", "refused":
		return acpsdk.StopReasonRefusal
	default:
		return acpsdk.StopReasonEndTurn
	}
}

func validEnvironmentName(name string) bool {
	if name == "" {
		return false
	}
	for index, character := range name {
		if index == 0 && character != '_' && !unicode.IsLetter(character) {
			return false
		}
		if index > 0 && character != '_' && !unicode.IsLetter(character) && !unicode.IsDigit(character) {
			return false
		}
	}
	return true
}

func slug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || character == '-' || character == '_' {
			builder.WriteRune(character)
		} else if builder.Len() > 0 {
			builder.WriteByte('-')
		}
	}
	result := strings.Trim(builder.String(), "-")
	if result == "" {
		return "server"
	}
	return result
}

func shortID(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 12 {
		return value[len(value)-12:]
	}
	return value
}
