package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"superscience/internal/providers"
	"superscience/internal/science"
	"superscience/internal/secrets"
	"superscience/internal/skills"
	"superscience/internal/store"
	"superscience/internal/tools"
)

type Runtime struct {
	Store           *store.Store
	Science         *science.Registry
	Gate            tools.PermissionGate
	Secrets         secrets.Backend
	ProjectRoot     string
	Provider        Provider
	ProviderFactory func(context.Context, RunRequest) (Provider, error)
	SkillCatalog    func(context.Context) ([]store.SkillRecord, error)
	OnRunStarted    func(store.AgentRun)
	MCPServers      map[string]store.MCPServer
}

func storeJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func (r Runtime) updateRunStatus(ctx context.Context, runID, status string) {
	if ctx.Err() != nil {
		ctx = context.WithoutCancel(ctx)
		if status == "error" || status == "review_failed" {
			status = "aborted"
		}
	}
	_ = r.Store.UpdateAgentRunStatus(ctx, runID, status)
}

type RunRequest struct {
	Goal       string
	Context    string
	Agent      string
	ProviderID string
	Model      string
	MaxSteps   int
}

type RunResult struct {
	ID              string         `json:"id"`
	SessionID       string         `json:"sessionId"`
	Agent           string         `json:"agent"`
	Provider        string         `json:"provider"`
	Model           string         `json:"model"`
	Status          string         `json:"status"`
	Message         store.Message  `json:"message"`
	Artifact        store.Artifact `json:"artifact"`
	PermissionID    string         `json:"permissionId,omitempty"`
	PendingActionID string         `json:"pendingActionId,omitempty"`
}

func (r Runtime) Run(ctx context.Context, sessionID string, request RunRequest) (RunResult, error) {
	if strings.TrimSpace(request.Goal) == "" {
		request.Goal = "Continue the research session."
	}
	if request.Agent == "" {
		request.Agent = "research"
	}
	definition, err := ResolveDefinition(ctx, r.Store, request.Agent)
	if err != nil {
		return RunResult{}, fmt.Errorf("resolve agent %q: %w", request.Agent, err)
	}
	run, err := r.Store.CreateAgentRun(ctx, store.AgentRun{
		SessionID: sessionID,
		Agent:     request.Agent,
		Status:    "running",
		Goal:      request.Goal,
		Model:     request.Model,
	})
	if err != nil {
		return RunResult{}, err
	}
	if _, err := r.Store.AddMessage(ctx, store.AddMessageInput{SessionID: sessionID, Role: "user", Content: request.Goal}); err != nil {
		r.updateRunStatus(ctx, run.ID, "error")
		return RunResult{}, err
	}
	if r.OnRunStarted != nil {
		r.OnRunStarted(run)
	}
	_, _ = r.Store.AddAgentStep(ctx, store.AgentStep{RunID: run.ID, Kind: "planning", Status: "ok", Title: "Plan research loop", Content: request.Goal})
	loadedSkill, effectiveGoal := r.loadSlashSkill(ctx, run.ID, sessionID, request.Goal)
	request.Goal = effectiveGoal
	_, _ = r.Store.AddAgentStep(ctx, store.AgentStep{RunID: run.ID, Kind: "model", Status: "ok", Title: "Select provider", Content: strings.TrimSpace(request.ProviderID + "/" + request.Model)})

	provider, err := r.providerFor(ctx, request)
	if err != nil {
		_, _ = r.Store.AddAgentStep(ctx, store.AgentStep{RunID: run.ID, Kind: "model", Status: "error", Title: "Provider selection failed", Content: err.Error()})
		r.updateRunStatus(ctx, run.ID, "error")
		return RunResult{}, err
	}
	executor := tools.Executor{Store: r.Store, Science: r.Science, Gate: r.Gate, Secrets: r.Secrets, MCPServers: r.MCPServers}
	status := "completed"
	permissionID := ""
	finalText := ""
	messages := []providers.Message{{Role: "user", Content: initialPrompt(request.Goal, request.Context, loadedSkill)}}
	toolSchemas := providerToolDefinitions(definition)
	systemPrompt := SystemPromptForDefinition(definition)
	maxSteps := normalizeMaxSteps(request.MaxSteps)
	for turn := 1; turn <= maxSteps; turn++ {
		response, err := provider.Stream(ctx, providers.ChatRequest{
			System:     systemPrompt,
			Messages:   append([]providers.Message(nil), messages...),
			Tools:      toolSchemas,
			ToolChoice: "auto",
		}, func(providers.StreamEvent) {})
		if err != nil {
			_, _ = r.Store.AddAgentStep(ctx, store.AgentStep{RunID: run.ID, Kind: "model", Status: "error", Title: "Provider error", Content: err.Error()})
			r.updateRunStatus(ctx, run.ID, "error")
			return RunResult{}, err
		}
		if strings.TrimSpace(response.Text) != "" {
			finalText = response.Text
		}
		messages = append(messages, providers.Message{Role: "assistant", Content: response.Text, ToolCalls: response.ToolCalls})
		if turn == 1 {
			_, _ = r.Store.AddAgentStep(ctx, store.AgentStep{RunID: run.ID, Kind: "observation", Status: "ok", Title: "Model response", Content: response.Text})
			r.addUsageStep(ctx, run.ID, "Model usage", response.Usage)
		} else {
			_, _ = r.Store.AddAgentStep(ctx, store.AgentStep{RunID: run.ID, Kind: "model", Status: "ok", Title: "Final response", Content: response.Text, Metadata: storeJSON(map[string]int{"turn": turn})})
			r.addUsageStep(ctx, run.ID, fmt.Sprintf("Model turn %d usage", turn), response.Usage)
		}

		observations := make([]toolObservation, 0, len(response.ToolCalls))
		pendingActionID := ""
		if len(response.ToolCalls) > 0 {
			observations, permissionID, pendingActionID = r.executeProviderToolCalls(ctx, run.ID, sessionID, response.ToolCalls, executor)
			for _, observation := range observations {
				if observation.Status == "pending_permission" || observation.Status == "waiting_input" {
					continue
				}
				messages = appendToolObservationMessage(messages, observation)
			}
		}

		if pendingActionID != "" {
			pendingIndex := len(observations) - 1
			state := runCheckpointState{
				Version:         1,
				Request:         request,
				Messages:        messages,
				PendingCalls:    append([]ProviderToolCall(nil), response.ToolCalls[pendingIndex:]...),
				NextTurn:        turn + 1,
				MaxSteps:        maxSteps,
				FinalText:       finalText,
				PendingActionID: pendingActionID,
			}
			if err := r.saveInputCheckpoint(ctx, run.ID, sessionID, observations[pendingIndex].ToolCallID, state); err != nil {
				r.updateRunStatus(ctx, run.ID, "error")
				return RunResult{}, err
			}
			r.updateRunStatus(ctx, run.ID, "waiting_input")
			return RunResult{ID: run.ID, SessionID: sessionID, Agent: request.Agent, Provider: request.ProviderID, Model: request.Model, Status: "waiting_input", PendingActionID: pendingActionID}, nil
		}

		if permissionID != "" {
			pendingIndex := len(observations) - 1
			if pendingIndex < 0 || pendingIndex >= len(response.ToolCalls) {
				r.updateRunStatus(ctx, run.ID, "error")
				return RunResult{}, errors.New("permission checkpoint could not identify the pending tool call")
			}
			state := runCheckpointState{
				Version:      1,
				Request:      request,
				Messages:     messages,
				PendingCalls: append([]ProviderToolCall(nil), response.ToolCalls[pendingIndex:]...),
				NextTurn:     turn + 1,
				MaxSteps:     maxSteps,
				FinalText:    finalText,
			}
			if err := r.savePermissionCheckpoint(ctx, run.ID, sessionID, permissionID, observations[pendingIndex].ToolCallID, state); err != nil {
				r.updateRunStatus(ctx, run.ID, "error")
				return RunResult{}, err
			}
			r.updateRunStatus(ctx, run.ID, "waiting_permission")
			return RunResult{ID: run.ID, SessionID: sessionID, Agent: request.Agent, Provider: request.ProviderID, Model: request.Model, Status: "waiting_permission", PermissionID: permissionID}, nil
		}
		if len(observations) == 0 {
			break
		}
		if turn == maxSteps {
			status = "max_steps"
			_, _ = r.Store.AddAgentStep(ctx, store.AgentStep{
				RunID:   run.ID,
				Kind:    "max_steps",
				Status:  "hit",
				Title:   "Max steps reached",
				Content: fmt.Sprintf("Stopped after %d model turns with additional tool observations pending review.", maxSteps),
			})
			break
		}
	}
	if strings.TrimSpace(finalText) == "" {
		r.updateRunStatus(ctx, run.ID, "error")
		return RunResult{}, errors.New("provider completed without a final text response")
	}
	if status == "completed" && definition.ReviewMode == "required" {
		finalText, err = r.reviewFinalResponse(ctx, run.ID, provider, request, definition, finalText)
		if err != nil {
			r.updateRunStatus(ctx, run.ID, "review_failed")
			return RunResult{}, err
		}
	}
	message, artifact, err := r.persistRunSummary(ctx, run.ID, sessionID, request, finalText, status)
	if err != nil {
		return RunResult{}, err
	}
	r.updateRunStatus(ctx, run.ID, status)
	return RunResult{ID: run.ID, SessionID: sessionID, Agent: request.Agent, Provider: request.ProviderID, Model: request.Model, Status: status, Message: message, Artifact: artifact, PermissionID: permissionID}, nil
}

type toolObservation struct {
	ProviderCallID  string
	ToolCallID      string
	Tool            string
	Status          string
	Output          string
	PermissionID    string
	PendingActionID string
}

type runCheckpointState struct {
	Version         int                 `json:"version"`
	Request         RunRequest          `json:"request"`
	Messages        []providers.Message `json:"messages"`
	PendingCalls    []ProviderToolCall  `json:"pendingCalls"`
	NextTurn        int                 `json:"nextTurn"`
	MaxSteps        int                 `json:"maxSteps"`
	FinalText       string              `json:"finalText"`
	PendingActionID string              `json:"pendingActionId,omitempty"`
}

func (r Runtime) Resume(ctx context.Context, runID string) (RunResult, error) {
	checkpoint, err := r.Store.GetRunCheckpoint(ctx, runID)
	if err != nil {
		return RunResult{}, fmt.Errorf("load run checkpoint: %w", err)
	}
	var state runCheckpointState
	if err := json.Unmarshal([]byte(checkpoint.State), &state); err != nil {
		return RunResult{}, fmt.Errorf("decode run checkpoint: %w", err)
	}
	if state.Version != 1 || len(state.PendingCalls) == 0 {
		return RunResult{}, errors.New("run checkpoint is unsupported or has no pending tool call")
	}
	run, err := r.Store.GetAgentRun(ctx, runID)
	if err != nil {
		return RunResult{}, err
	}
	if run.Status != "waiting_permission" && run.Status != "waiting_input" {
		return RunResult{}, fmt.Errorf("run %s is not waiting for a resumable action", runID)
	}
	definition, err := ResolveDefinition(ctx, r.Store, state.Request.Agent)
	if err != nil {
		return RunResult{}, fmt.Errorf("resolve agent %q: %w", state.Request.Agent, err)
	}
	provider, err := r.providerFor(ctx, state.Request)
	if err != nil {
		return RunResult{}, err
	}
	executor := tools.Executor{Store: r.Store, Science: r.Science, Gate: r.Gate, Secrets: r.Secrets, MCPServers: r.MCPServers}
	if checkpoint.Status == "waiting_input" {
		action, err := r.Store.GetPendingAction(ctx, state.PendingActionID)
		if err != nil {
			return RunResult{}, err
		}
		if action.RunID != run.ID || action.SessionID != run.SessionID || action.Status != "answered" {
			return RunResult{}, errors.New("pending question has not been answered for this run")
		}
		call := state.PendingCalls[0]
		updated, err := r.Store.UpdateToolCall(ctx, checkpoint.ToolCallID, "ok", action.Response, "{}", "")
		if err != nil {
			return RunResult{}, err
		}
		r.addToolObservationStep(ctx, run.ID, call, tools.ExecuteResult{ID: updated.ID, RunID: run.ID, SessionID: run.SessionID, Tool: call.Name, Status: "ok", Output: action.Response})
		state.Messages = appendToolObservationMessage(state.Messages, toolObservation{ProviderCallID: call.ID, ToolCallID: updated.ID, Tool: call.Name, Status: "ok", Output: action.Response})
		state.PendingCalls = nil
		state.PendingActionID = ""
	}
	for index, call := range state.PendingCalls {
		executeRequest := tools.ExecuteRequest{
			RunID:       run.ID,
			SessionID:   run.SessionID,
			Tool:        call.Name,
			Input:       parseToolArguments(call.Arguments),
			ProjectRoot: r.ProjectRoot,
		}
		if index == 0 {
			executeRequest.PermissionID = checkpoint.PermissionID
			executeRequest.ExistingToolCallID = checkpoint.ToolCallID
		}
		result, err := executor.Execute(ctx, executeRequest)
		if err != nil {
			r.updateRunStatus(ctx, run.ID, "error")
			return RunResult{}, err
		}
		r.addToolObservationStep(ctx, run.ID, call, result)
		if result.Status == "pending_permission" {
			state.PendingCalls = append([]ProviderToolCall(nil), state.PendingCalls[index:]...)
			if err := r.savePermissionCheckpoint(ctx, run.ID, run.SessionID, result.PermissionID, result.ID, state); err != nil {
				return RunResult{}, err
			}
			return RunResult{ID: run.ID, SessionID: run.SessionID, Agent: state.Request.Agent, Provider: state.Request.ProviderID, Model: state.Request.Model, Status: "waiting_permission", PermissionID: result.PermissionID}, nil
		}
		state.Messages = appendToolObservationMessage(state.Messages, toolObservation{
			ProviderCallID: call.ID,
			ToolCallID:     result.ID,
			Tool:           call.Name,
			Status:         result.Status,
			Output:         result.Observation(),
			PermissionID:   result.PermissionID,
		})
	}
	state.PendingCalls = nil
	status := "completed"
	permissionID := ""
	finalText := state.FinalText
	for turn := state.NextTurn; turn <= state.MaxSteps; turn++ {
		response, err := provider.Stream(ctx, providers.ChatRequest{
			System:     SystemPromptForDefinition(definition),
			Messages:   append([]providers.Message(nil), state.Messages...),
			Tools:      providerToolDefinitions(definition),
			ToolChoice: "auto",
		}, func(providers.StreamEvent) {})
		if err != nil {
			r.updateRunStatus(ctx, run.ID, "error")
			return RunResult{}, err
		}
		if strings.TrimSpace(response.Text) != "" {
			finalText = response.Text
		}
		state.Messages = append(state.Messages, providers.Message{Role: "assistant", Content: response.Text, ToolCalls: response.ToolCalls})
		_, _ = r.Store.AddAgentStep(ctx, store.AgentStep{RunID: run.ID, Kind: "model", Status: "ok", Title: "Resumed model response", Content: response.Text, Metadata: storeJSON(map[string]int{"turn": turn})})
		r.addUsageStep(ctx, run.ID, fmt.Sprintf("Resumed model turn %d usage", turn), response.Usage)

		observations := make([]toolObservation, 0, len(response.ToolCalls))
		pendingActionID := ""
		if len(response.ToolCalls) > 0 {
			observations, permissionID, pendingActionID = r.executeProviderToolCalls(ctx, run.ID, run.SessionID, response.ToolCalls, executor)
			for _, observation := range observations {
				if observation.Status != "pending_permission" && observation.Status != "waiting_input" {
					state.Messages = appendToolObservationMessage(state.Messages, observation)
				}
			}
		}
		if pendingActionID != "" {
			pendingIndex := len(observations) - 1
			state.PendingCalls = append([]ProviderToolCall(nil), response.ToolCalls[pendingIndex:]...)
			state.NextTurn = turn + 1
			state.FinalText = finalText
			state.PendingActionID = pendingActionID
			if err := r.saveInputCheckpoint(ctx, run.ID, run.SessionID, observations[pendingIndex].ToolCallID, state); err != nil {
				return RunResult{}, err
			}
			r.updateRunStatus(ctx, run.ID, "waiting_input")
			return RunResult{ID: run.ID, SessionID: run.SessionID, Agent: state.Request.Agent, Provider: state.Request.ProviderID, Model: state.Request.Model, Status: "waiting_input", PendingActionID: pendingActionID}, nil
		}
		if permissionID != "" {
			pendingIndex := len(observations) - 1
			state.PendingCalls = append([]ProviderToolCall(nil), response.ToolCalls[pendingIndex:]...)
			state.NextTurn = turn + 1
			state.FinalText = finalText
			if err := r.savePermissionCheckpoint(ctx, run.ID, run.SessionID, permissionID, observations[pendingIndex].ToolCallID, state); err != nil {
				return RunResult{}, err
			}
			r.updateRunStatus(ctx, run.ID, "waiting_permission")
			return RunResult{ID: run.ID, SessionID: run.SessionID, Agent: state.Request.Agent, Provider: state.Request.ProviderID, Model: state.Request.Model, Status: "waiting_permission", PermissionID: permissionID}, nil
		}
		if len(observations) == 0 {
			break
		}
		if turn == state.MaxSteps {
			status = "max_steps"
			_, _ = r.Store.AddAgentStep(ctx, store.AgentStep{RunID: run.ID, Kind: "max_steps", Status: "hit", Title: "Max steps reached", Content: fmt.Sprintf("Stopped after %d model turns.", state.MaxSteps)})
			break
		}
	}
	if strings.TrimSpace(finalText) == "" {
		r.updateRunStatus(ctx, run.ID, "error")
		return RunResult{}, errors.New("provider completed without a final text response")
	}
	if status == "completed" && definition.ReviewMode == "required" {
		finalText, err = r.reviewFinalResponse(ctx, run.ID, provider, state.Request, definition, finalText)
		if err != nil {
			r.updateRunStatus(ctx, run.ID, "review_failed")
			return RunResult{}, err
		}
	}
	message, artifact, err := r.persistRunSummary(ctx, run.ID, run.SessionID, state.Request, finalText, status)
	if err != nil {
		return RunResult{}, err
	}
	if err := r.Store.DeleteRunCheckpoint(ctx, run.ID); err != nil {
		return RunResult{}, err
	}
	r.updateRunStatus(ctx, run.ID, status)
	return RunResult{ID: run.ID, SessionID: run.SessionID, Agent: state.Request.Agent, Provider: state.Request.ProviderID, Model: state.Request.Model, Status: status, Message: message, Artifact: artifact}, nil
}

func (r Runtime) Answer(ctx context.Context, runID, actionID, answer string) (RunResult, error) {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return RunResult{}, errors.New("answer is required")
	}
	action, err := r.Store.GetPendingAction(ctx, actionID)
	if err != nil {
		return RunResult{}, err
	}
	if action.RunID != runID || action.Status != "waiting_input" || action.Kind != "question" {
		return RunResult{}, errors.New("pending action does not accept an answer for this run")
	}
	if _, err := r.Store.AnswerPendingAction(ctx, actionID, answer); err != nil {
		return RunResult{}, err
	}
	return r.Resume(ctx, runID)
}

func (r Runtime) savePermissionCheckpoint(ctx context.Context, runID, sessionID, permissionID, toolCallID string, state runCheckpointState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	_, err = r.Store.UpsertRunCheckpoint(ctx, store.RunCheckpoint{
		RunID:        runID,
		SessionID:    sessionID,
		PermissionID: permissionID,
		ToolCallID:   toolCallID,
		State:        string(data),
		Status:       "waiting_permission",
	})
	return err
}

func (r Runtime) saveInputCheckpoint(ctx context.Context, runID, sessionID, toolCallID string, state runCheckpointState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	_, err = r.Store.UpsertRunCheckpoint(ctx, store.RunCheckpoint{
		RunID:      runID,
		SessionID:  sessionID,
		ToolCallID: toolCallID,
		State:      string(data),
		Status:     "waiting_input",
	})
	return err
}

func appendToolObservationMessage(messages []providers.Message, observation toolObservation) []providers.Message {
	output := any(observation.Output)
	var structured any
	if json.Unmarshal([]byte(observation.Output), &structured) == nil {
		output = structured
	}
	return append(messages, providers.Message{
		Role:       "tool",
		Name:       observation.Tool,
		ToolCallID: observation.ProviderCallID,
		Content: storeJSON(map[string]any{
			"tool":   observation.Tool,
			"status": observation.Status,
			"output": output,
		}),
	})
}

func (r Runtime) addToolObservationStep(ctx context.Context, runID string, call ProviderToolCall, result tools.ExecuteResult) {
	_, _ = r.Store.AddAgentStep(ctx, store.AgentStep{
		RunID:   runID,
		Kind:    "observation",
		Status:  result.Status,
		Title:   call.Name,
		Content: result.Observation(),
		Metadata: storeJSON(map[string]string{
			"providerToolCallId": call.ID,
			"toolCallId":         result.ID,
			"permissionId":       result.PermissionID,
		}),
	})
}

func initialPrompt(goal, runtimeContext string, loadedSkill *skills.Loaded) string {
	var builder strings.Builder
	builder.WriteString("Run a scientific research loop with concise provenance notes for: ")
	builder.WriteString(goal)
	if strings.TrimSpace(runtimeContext) != "" {
		builder.WriteString("\n\nRuntime context:\n")
		builder.WriteString(strings.TrimSpace(runtimeContext))
	}
	if loadedSkill != nil {
		builder.WriteString("\n\nLoaded skill: ")
		builder.WriteString(loadedSkill.Name)
		builder.WriteString("\n")
		builder.WriteString(loadedSkill.Content)
		builder.WriteString("\n\nFollow the loaded skill instructions before any generic reasoning.")
	}
	return builder.String()
}

// SystemPrompt exposes the same agent contract to direct conversational turns.
func SystemPrompt(agentID string) string {
	for _, definition := range BuiltInDefinitions() {
		if definition.ID == agentID {
			return SystemPromptForDefinition(definition)
		}
	}
	return SystemPromptForDefinition(Definition{Name: "Research", Description: "General scientific research agent."})
}

func providerToolDefinitions(agentDefinition Definition) []providers.ToolDefinition {
	allowed := map[string]bool{}
	for _, toolID := range agentDefinition.Tools {
		allowed[toolID] = true
	}
	definitions := make([]providers.ToolDefinition, 0, len(allowed))
	for _, definition := range tools.BuiltIns() {
		if !allowed[definition.ID] || !definition.Enabled {
			continue
		}
		properties := map[string]any{}
		for _, input := range definition.Inputs {
			properties[input] = providerToolProperty(input)
		}
		definitions = append(definitions, providers.ToolDefinition{
			Name:        definition.ID,
			Description: definition.Description,
			InputSchema: map[string]any{"type": "object", "properties": properties, "additionalProperties": false},
		})
	}
	return definitions
}

func providerToolProperty(name string) map[string]any {
	switch name {
	case "items", "steps", "edits", "artifactIds":
		return map[string]any{"type": "array"}
	case "input", "metadata", "arguments", "filters":
		return map[string]any{"type": "object"}
	case "limit", "limitBytes", "maxSteps":
		return map[string]any{"type": "integer"}
	default:
		return map[string]any{"type": "string"}
	}
}

func normalizeMaxSteps(maxSteps int) int {
	if maxSteps <= 0 {
		return 6
	}
	if maxSteps > 20 {
		return 20
	}
	return maxSteps
}

type reviewerDecision struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

func (r Runtime) reviewFinalResponse(ctx context.Context, runID string, provider Provider, request RunRequest, definition Definition, draft string) (string, error) {
	evidence := r.reviewEvidenceContext(ctx, runID)
	for cycle := 1; cycle <= 2; cycle++ {
		response, err := provider.Stream(ctx, providers.ChatRequest{
			System:     reviewerSystemPrompt(request.Agent),
			ToolChoice: "none",
			Messages: []providers.Message{{
				Role: "user",
				Content: "Research goal:\n" + request.Goal + "\n\nPersisted execution evidence:\n" + evidence + "\n\nPrimary draft:\n" + draft +
					"\n\nReturn exactly one JSON object with decision set to approve or revise and a non-empty reason.",
			}},
		}, func(providers.StreamEvent) {})
		if err != nil {
			r.addReviewerFailure(ctx, runID, cycle, "reviewer provider call failed: "+err.Error())
			return "", fmt.Errorf("reviewer provider call failed: %w", err)
		}
		r.addUsageStep(ctx, runID, fmt.Sprintf("Reviewer cycle %d usage", cycle), response.Usage)
		decision, err := parseReviewerDecision(response.Text)
		if err != nil {
			r.addReviewerFailure(ctx, runID, cycle, err.Error())
			return "", err
		}
		_, _ = r.Store.AddAgentStep(ctx, store.AgentStep{
			RunID:    runID,
			Kind:     "reviewer_gate",
			Status:   map[string]string{"approve": "approved", "revise": "revision_required"}[decision.Decision],
			Title:    fmt.Sprintf("Reviewer cycle %d", cycle),
			Content:  decision.Reason,
			Metadata: storeJSON(map[string]any{"agent": request.Agent, "mode": definition.ReviewMode, "decision": decision.Decision, "cycle": cycle}),
		})
		if decision.Decision == "approve" {
			return draft, nil
		}
		if cycle == 2 {
			return "", errors.New("reviewer requested revisions after the maximum review cycles")
		}
		revision, err := provider.Stream(ctx, providers.ChatRequest{
			System:     SystemPromptForDefinition(definition) + " Revise the draft to address the independent reviewer. Do not invent evidence or claim unobserved tool results.",
			ToolChoice: "none",
			Messages: []providers.Message{
				{Role: "user", Content: "Research goal:\n" + request.Goal},
				{Role: "user", Content: "Persisted execution evidence:\n" + evidence},
				{Role: "assistant", Content: draft},
				{Role: "user", Content: "Reviewer required this revision:\n" + decision.Reason + "\n\nReturn only the complete revised answer."},
			},
		}, func(providers.StreamEvent) {})
		if err != nil {
			r.addReviewerFailure(ctx, runID, cycle, "revision provider call failed: "+err.Error())
			return "", fmt.Errorf("revision provider call failed: %w", err)
		}
		if strings.TrimSpace(revision.Text) == "" || len(revision.ToolCalls) != 0 {
			err := errors.New("revision provider response must contain text and no tool calls")
			r.addReviewerFailure(ctx, runID, cycle, err.Error())
			return "", err
		}
		r.addUsageStep(ctx, runID, fmt.Sprintf("Revision cycle %d usage", cycle), revision.Usage)
		_, _ = r.Store.AddAgentStep(ctx, store.AgentStep{
			RunID:    runID,
			Kind:     "revision",
			Status:   "ok",
			Title:    fmt.Sprintf("Revision cycle %d", cycle),
			Content:  revision.Text,
			Metadata: storeJSON(map[string]int{"cycle": cycle}),
		})
		draft = revision.Text
	}
	return "", errors.New("reviewer did not produce a final decision")
}

func (r Runtime) reviewEvidenceContext(ctx context.Context, runID string) string {
	type evidenceStep struct {
		Kind     string `json:"kind"`
		Status   string `json:"status"`
		Title    string `json:"title"`
		Content  string `json:"content"`
		Metadata string `json:"metadata"`
	}
	type evidenceCall struct {
		ID     string `json:"id"`
		Tool   string `json:"tool"`
		Status string `json:"status"`
		Input  string `json:"input"`
		Output string `json:"output"`
		Data   string `json:"data"`
	}
	packet := struct {
		Steps     []evidenceStep `json:"steps"`
		ToolCalls []evidenceCall `json:"toolCalls"`
	}{
		Steps:     []evidenceStep{},
		ToolCalls: []evidenceCall{},
	}
	if steps, err := r.Store.ListAgentSteps(ctx, runID); err == nil {
		for _, step := range steps {
			if step.Kind != "tool_request" && (step.Kind != "observation" || step.Title == "Model response") {
				continue
			}
			packet.Steps = append(packet.Steps, evidenceStep{
				Kind:     step.Kind,
				Status:   step.Status,
				Title:    step.Title,
				Content:  boundedEvidenceText(step.Content),
				Metadata: boundedEvidenceText(step.Metadata),
			})
		}
	}
	if calls, err := r.Store.ListToolCalls(ctx, runID); err == nil {
		if len(calls) > 12 {
			calls = calls[len(calls)-12:]
		}
		for _, call := range calls {
			packet.ToolCalls = append(packet.ToolCalls, evidenceCall{
				ID:     call.ID,
				Tool:   call.Tool,
				Status: call.Status,
				Input:  boundedEvidenceText(call.Input),
				Output: boundedEvidenceText(call.Output),
				Data:   boundedEvidenceText(call.Data),
			})
		}
	}
	encoded, err := json.Marshal(packet)
	if err != nil {
		return `{"steps":[],"toolCalls":[]}`
	}
	return string(encoded)
}

func boundedEvidenceText(value string) string {
	const maxRunes = 16 * 1024
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes]) + "\n[truncated]"
}

func reviewerSystemPrompt(agentID string) string {
	return "You are an independent scientific reviewer for the Superscience " + agentID +
		" agent. Check whether claims are supported by the draft's stated evidence, uncertainty is explicit, and conclusions answer the research goal. " +
		"Do not use tools. Return strict JSON only: {\"decision\":\"approve|revise\",\"reason\":\"...\"}."
}

func parseReviewerDecision(content string) (reviewerDecision, error) {
	var decision reviewerDecision
	decoder := json.NewDecoder(strings.NewReader(strings.TrimSpace(content)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decision); err != nil {
		return reviewerDecision{}, fmt.Errorf("invalid reviewer response: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return reviewerDecision{}, errors.New("invalid reviewer response: trailing JSON value")
	}
	decision.Decision = strings.ToLower(strings.TrimSpace(decision.Decision))
	decision.Reason = strings.TrimSpace(decision.Reason)
	if decision.Decision != "approve" && decision.Decision != "revise" {
		return reviewerDecision{}, errors.New("invalid reviewer response: decision must be approve or revise")
	}
	if decision.Reason == "" {
		return reviewerDecision{}, errors.New("invalid reviewer response: reason is required")
	}
	return decision, nil
}

func (r Runtime) addReviewerFailure(ctx context.Context, runID string, cycle int, content string) {
	_, _ = r.Store.AddAgentStep(ctx, store.AgentStep{
		RunID:    runID,
		Kind:     "reviewer_gate",
		Status:   "error",
		Title:    fmt.Sprintf("Reviewer cycle %d failed", cycle),
		Content:  content,
		Metadata: storeJSON(map[string]int{"cycle": cycle}),
	})
}

func (r Runtime) loadSlashSkill(ctx context.Context, runID string, sessionID string, goal string) (*skills.Loaded, string) {
	name, remainder, ok := parseSlashSkillInvocation(goal)
	if !ok {
		return nil, goal
	}
	catalog, err := r.loadSkillCatalog(ctx)
	if err != nil {
		return nil, goal
	}
	loaded, err := skills.Load(name, catalog)
	if err != nil {
		return nil, goal
	}
	_, _ = r.Store.AddAgentStep(ctx, store.AgentStep{
		RunID:    runID,
		Kind:     "skill",
		Status:   "ok",
		Title:    loaded.Name,
		Content:  loaded.Content,
		Metadata: storeJSON(map[string]string{"source": loaded.Source, "id": loaded.ID}),
	})
	_, _ = r.Store.CreateToolCall(ctx, store.ToolCall{
		RunID:     runID,
		SessionID: sessionID,
		Tool:      "skill",
		Status:    "ok",
		Input:     storeJSON(map[string]string{"name": name}),
		Output:    "loaded skill " + loaded.Name,
	})
	if strings.TrimSpace(remainder) == "" {
		remainder = "Use the loaded skill to continue the research session."
	}
	return &loaded, remainder
}

func (r Runtime) loadSkillCatalog(ctx context.Context) ([]store.SkillRecord, error) {
	if r.SkillCatalog != nil {
		return r.SkillCatalog(ctx)
	}
	userSkills, err := skills.LoadUser()
	if err != nil {
		return nil, err
	}
	projectSkills, err := skills.LoadProject(r.ProjectRoot)
	if err != nil {
		return nil, err
	}
	installedSkills, err := skills.LoadInstalled()
	if err != nil {
		return nil, err
	}
	catalog := append(userSkills, installedSkills...)
	catalog = append(catalog, projectSkills...)
	if r.Store == nil {
		return catalog, nil
	}
	persisted, err := r.Store.ListSkills(ctx)
	if err != nil {
		return nil, err
	}
	return append(catalog, persisted...), nil
}

func parseSlashSkillInvocation(goal string) (string, string, bool) {
	trimmed := strings.TrimSpace(goal)
	if !strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, "//") {
		return "", goal, false
	}
	first, rest, _ := strings.Cut(trimmed, " ")
	name := strings.TrimPrefix(first, "/")
	if name == "" || strings.ContainsAny(name, `/\`) {
		return "", goal, false
	}
	return name, strings.TrimSpace(rest), true
}

func (r Runtime) executeProviderToolCalls(ctx context.Context, runID string, sessionID string, calls []ProviderToolCall, executor tools.Executor) ([]toolObservation, string, string) {
	observations := make([]toolObservation, 0, len(calls))
	for _, call := range calls {
		input := parseToolArguments(call.Arguments)
		_, _ = r.Store.AddAgentStep(ctx, store.AgentStep{
			RunID:    runID,
			Kind:     "tool_request",
			Status:   "ok",
			Title:    call.Name,
			Content:  call.Arguments,
			Metadata: storeJSON(map[string]string{"providerToolCallId": call.ID}),
		})
		if call.Name == "question" {
			question, _ := input["question"].(string)
			question = strings.TrimSpace(question)
			if len(calls) != 1 || question == "" {
				message := "question tool must be the only tool call and include a non-empty question"
				toolCall, _ := r.Store.CreateToolCall(ctx, store.ToolCall{RunID: runID, SessionID: sessionID, Tool: call.Name, Status: "error", Input: storeJSON(input), Output: message})
				observations = append(observations, toolObservation{ProviderCallID: call.ID, ToolCallID: toolCall.ID, Tool: call.Name, Status: "error", Output: message})
				continue
			}
			toolCall, err := r.Store.CreateToolCall(ctx, store.ToolCall{RunID: runID, SessionID: sessionID, Tool: call.Name, Status: "waiting_input", Input: storeJSON(input)})
			if err != nil {
				observations = append(observations, toolObservation{ProviderCallID: call.ID, Tool: call.Name, Status: "error", Output: err.Error()})
				continue
			}
			action, err := r.Store.CreatePendingAction(ctx, store.PendingAction{
				RunID:     runID,
				SessionID: sessionID,
				Kind:      "question",
				Payload:   storeJSON(map[string]string{"question": question, "providerToolCallId": call.ID}),
				Status:    "waiting_input",
			})
			if err != nil {
				_, _ = r.Store.UpdateToolCall(ctx, toolCall.ID, "error", err.Error(), "{}", "")
				observations = append(observations, toolObservation{ProviderCallID: call.ID, ToolCallID: toolCall.ID, Tool: call.Name, Status: "error", Output: err.Error()})
				continue
			}
			_, _ = r.Store.AddAgentStep(ctx, store.AgentStep{RunID: runID, Kind: "question", Status: "waiting_input", Title: "User input required", Content: question, Metadata: storeJSON(map[string]string{"pendingActionId": action.ID, "providerToolCallId": call.ID})})
			observation := toolObservation{ProviderCallID: call.ID, ToolCallID: toolCall.ID, Tool: call.Name, Status: "waiting_input", Output: question, PendingActionID: action.ID}
			return append(observations, observation), "", action.ID
		}
		result, err := executor.Execute(ctx, tools.ExecuteRequest{
			RunID:       runID,
			SessionID:   sessionID,
			Tool:        call.Name,
			Input:       input,
			ProjectRoot: r.ProjectRoot,
		})
		if err != nil {
			_, _ = r.Store.AddAgentStep(ctx, store.AgentStep{
				RunID:    runID,
				Kind:     "observation",
				Status:   "error",
				Title:    call.Name,
				Content:  err.Error(),
				Metadata: storeJSON(map[string]string{"providerToolCallId": call.ID}),
			})
			observations = append(observations, toolObservation{ProviderCallID: call.ID, Tool: call.Name, Status: "error", Output: err.Error()})
			continue
		}
		_, _ = r.Store.AddAgentStep(ctx, store.AgentStep{
			RunID:   runID,
			Kind:    "observation",
			Status:  result.Status,
			Title:   call.Name,
			Content: result.Observation(),
			Metadata: storeJSON(map[string]string{
				"providerToolCallId": call.ID,
				"toolCallId":         result.ID,
			}),
		})
		observations = append(observations, toolObservation{ProviderCallID: call.ID, ToolCallID: result.ID, Tool: call.Name, Status: result.Status, Output: result.Observation(), PermissionID: result.PermissionID})
		if result.Status == "pending_permission" {
			return observations, result.PermissionID, ""
		}
	}
	return observations, "", ""
}

func (r Runtime) addUsageStep(ctx context.Context, runID string, title string, usage ProviderUsage) {
	_, _ = r.Store.AddAgentStep(ctx, store.AgentStep{
		RunID:    runID,
		Kind:     "usage",
		Status:   "ok",
		Title:    title,
		Content:  "Provider usage normalized",
		Metadata: storeJSON(map[string]int{"promptTokens": usage.PromptTokens, "completionTokens": usage.CompletionTokens, "totalTokens": usage.TotalTokens}),
	})
}

func followUpPrompt(goal string, modelText string, observations []toolObservation) string {
	var builder strings.Builder
	builder.WriteString("Continue the scientific research loop and produce the final answer.\n\n")
	builder.WriteString("Original goal:\n")
	builder.WriteString(goal)
	builder.WriteString("\n\nPrevious model response:\n")
	builder.WriteString(modelText)
	builder.WriteString("\n\nTool observations:\n")
	for _, observation := range observations {
		builder.WriteString("- ")
		builder.WriteString(observation.Tool)
		builder.WriteString(": ")
		builder.WriteString(observation.Status)
		if observation.PermissionID != "" {
			builder.WriteString(" permission=")
			builder.WriteString(observation.PermissionID)
		}
		if observation.Output != "" {
			builder.WriteString(" output=")
			builder.WriteString(observation.Output)
		}
		builder.WriteString("\n")
	}
	builder.WriteString("\nUse the observations above, cite tool provenance when relevant, and do not request more tools unless absolutely necessary.")
	return builder.String()
}

func parseToolArguments(arguments string) map[string]any {
	if strings.TrimSpace(arguments) == "" {
		return map[string]any{}
	}
	var input map[string]any
	if err := json.Unmarshal([]byte(arguments), &input); err != nil {
		return map[string]any{"_raw": arguments}
	}
	return input
}

func (r Runtime) providerFor(ctx context.Context, request RunRequest) (Provider, error) {
	if r.ProviderFactory != nil {
		return r.ProviderFactory(ctx, request)
	}
	if r.Provider != nil {
		return r.Provider, nil
	}
	return nil, fmt.Errorf("cloud provider is not configured for agent run")
}

func (r Runtime) persistRunSummary(ctx context.Context, runID string, sessionID string, request RunRequest, content string, status string) (store.Message, store.Artifact, error) {
	message, err := r.Store.AddMessage(ctx, store.AddMessageInput{SessionID: sessionID, Role: "assistant", Content: content})
	if err != nil {
		return store.Message{}, store.Artifact{}, err
	}
	r.attachRunMessageParts(ctx, runID, message)
	artifact, err := r.Store.CreateArtifact(ctx, store.Artifact{
		SessionID: sessionID,
		Type:      "agent-run",
		Title:     "Agent run: " + request.Agent,
		Content:   content,
		Metadata:  storeJSON(map[string]string{"status": status}),
	})
	if err != nil {
		return message, store.Artifact{}, err
	}
	_, _ = r.Store.CreateProvenanceEdge(ctx, store.ProvenanceEdge{
		SourceID:   message.ID,
		SourceType: "message",
		TargetID:   artifact.ID,
		TargetType: "artifact",
		Relation:   "created",
	})
	return message, artifact, nil
}

func (r Runtime) attachRunMessageParts(ctx context.Context, runID string, message store.Message) {
	steps, err := r.Store.ListAgentSteps(ctx, runID)
	if err == nil {
		for _, step := range steps {
			if step.Kind != "skill" {
				continue
			}
			_, _ = r.Store.AddMessagePart(ctx, store.MessagePart{
				MessageID: message.ID,
				SessionID: message.SessionID,
				Type:      "skill",
				Title:     step.Title,
				Content:   step.Content,
				Metadata:  step.Metadata,
			})
		}
	}
	calls, err := r.Store.ListToolCalls(ctx, runID)
	if err != nil {
		return
	}
	for _, call := range calls {
		_, _ = r.Store.AddMessagePart(ctx, store.MessagePart{
			MessageID: message.ID,
			SessionID: message.SessionID,
			Type:      "tool",
			Title:     call.Tool,
			Content:   storedToolObservation(call),
			Metadata: storeJSON(map[string]string{
				"toolCallId":   call.ID,
				"status":       call.Status,
				"permissionId": call.PermissionID,
			}),
		})
	}
}

func storedToolObservation(call store.ToolCall) string {
	var data any
	if strings.TrimSpace(call.Data) == "" || strings.TrimSpace(call.Data) == "{}" || strings.TrimSpace(call.Data) == "null" || json.Unmarshal([]byte(call.Data), &data) != nil {
		return call.Output
	}
	return storeJSON(map[string]any{"summary": call.Output, "data": data})
}
