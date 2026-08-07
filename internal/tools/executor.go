package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"superscience/internal/files"
	"superscience/internal/lsp"
	"superscience/internal/mcp"
	"superscience/internal/notebook"
	pluginruntime "superscience/internal/plugins"
	"superscience/internal/science"
	sharepkg "superscience/internal/share"
	"superscience/internal/skills"
	"superscience/internal/store"
)

type PermissionGate interface {
	Ask(sessionID, typ, pattern, message string) string
	IsAllowed(sessionID, typ, pattern string) bool
}

type ExecuteRequest struct {
	RunID              string         `json:"runId"`
	SessionID          string         `json:"sessionId"`
	Tool               string         `json:"tool"`
	Input              map[string]any `json:"input"`
	ProjectRoot        string         `json:"-"`
	PermissionID       string         `json:"-"`
	ExistingToolCallID string         `json:"-"`
}

type ExecuteResult struct {
	ID           string         `json:"id"`
	RunID        string         `json:"runId"`
	SessionID    string         `json:"sessionId"`
	Tool         string         `json:"tool"`
	Status       string         `json:"status"`
	PermissionID string         `json:"permissionId"`
	Output       string         `json:"output"`
	Data         map[string]any `json:"data,omitempty"`
	existingID   string
}

const maxStructuredObservationBytes = 256 * 1024

// Observation returns the complete provider-facing result while preserving
// Output as the concise summary used by API clients.
func (r ExecuteResult) Observation() string {
	if len(r.Data) == 0 {
		return r.Output
	}
	encoded, err := json.Marshal(map[string]any{
		"summary": r.Output,
		"data":    r.Data,
	})
	if err != nil {
		return r.Output
	}
	if len(encoded) <= maxStructuredObservationBytes {
		return string(encoded)
	}
	truncated, err := json.Marshal(map[string]any{
		"summary":   r.Output,
		"truncated": true,
		"bytes":     len(encoded),
	})
	if err != nil {
		return r.Output
	}
	return string(truncated)
}

type Executor struct {
	Store      *store.Store
	Science    *science.Registry
	Gate       PermissionGate
	Secrets    mcp.SecretBackend
	MCPServers map[string]store.MCPServer
}

func (e Executor) Execute(ctx context.Context, request ExecuteRequest) (ExecuteResult, error) {
	if request.Input == nil {
		request.Input = map[string]any{}
	}
	definition, ok := Find(request.Tool)
	if !ok {
		return ExecuteResult{}, fmt.Errorf("unknown tool %q", request.Tool)
	}
	pattern := permissionPattern(request.Tool, request.Input)
	result := ExecuteResult{RunID: request.RunID, SessionID: request.SessionID, Tool: request.Tool, Status: "ok", existingID: request.ExistingToolCallID}
	if definition.RequiresApproval && strings.TrimSpace(request.SessionID) != "" {
		if request.PermissionID != "" {
			permission, err := e.authorizePersistedPermission(ctx, request, pattern)
			if err != nil {
				return ExecuteResult{}, err
			}
			result.PermissionID = permission.ID
			if permission.Status == "reject" {
				result.Status = "permission_denied"
				result.Output = "Permission denied for " + request.Tool + " on " + pattern + "."
				return e.record(ctx, result, request.Input)
			}
		} else if e.Gate != nil && !e.Gate.IsAllowed(request.SessionID, riskType(request.Tool), pattern) {
			result.Status = "pending_permission"
			result.PermissionID = e.Gate.Ask(request.SessionID, riskType(request.Tool), pattern, definition.Name+" requires approval before execution.")
			return e.record(ctx, result, request.Input)
		}
	}

	output, data, err := e.executeAllowed(ctx, request)
	if err != nil {
		result.Status = "error"
		result.Output = err.Error()
		recorded, recordErr := e.record(ctx, result, request.Input)
		if recordErr != nil {
			return recorded, recordErr
		}
		return recorded, nil
	}
	result.Output = output
	result.Data = data
	return e.record(ctx, result, request.Input)
}

func (e Executor) authorizePersistedPermission(ctx context.Context, request ExecuteRequest, pattern string) (store.PermissionRecord, error) {
	if e.Store == nil {
		return store.PermissionRecord{}, errors.New("persisted permission requires a store")
	}
	permission, err := e.Store.GetPermission(ctx, request.PermissionID)
	if err != nil {
		return store.PermissionRecord{}, fmt.Errorf("load permission: %w", err)
	}
	if permission.SessionID != request.SessionID || permission.Type != riskType(request.Tool) || permission.Pattern != pattern {
		return store.PermissionRecord{}, errors.New("permission does not match the requested tool action")
	}
	if permission.Status != "once" && permission.Status != "always" && permission.Status != "reject" {
		return store.PermissionRecord{}, errors.New("permission decision is still pending")
	}
	if permission.Status == "once" {
		if err := e.Store.ConsumePermissionByID(ctx, permission.ID); err != nil {
			return store.PermissionRecord{}, err
		}
	}
	return permission, nil
}

func (e Executor) executeAllowed(ctx context.Context, request ExecuteRequest) (string, map[string]any, error) {
	switch request.Tool {
	case "read":
		content, err := files.ReadText(request.ProjectRoot, stringInput(request.Input, "path"))
		return content, nil, err
	case "write":
		path := stringInput(request.Input, "path")
		content := stringInput(request.Input, "content")
		if path == "" {
			return "", nil, errors.New("path is required")
		}
		if err := files.WriteText(request.ProjectRoot, path, content); err != nil {
			return "", nil, err
		}
		return content, map[string]any{"path": path}, nil
	case "edit":
		return e.runEdit(request.ProjectRoot, request.Input)
	case "multiedit":
		return e.runMultiEdit(request.ProjectRoot, request.Input)
	case "apply_patch":
		return e.runApplyPatch(ctx, request.ProjectRoot, request.Input)
	case "ls":
		nodes, err := files.List(request.ProjectRoot, stringInput(request.Input, "path"))
		return fmt.Sprintf("%d nodes", len(nodes)), map[string]any{"nodes": nodes}, err
	case "glob":
		matches, err := runGlob(request.ProjectRoot, stringInput(request.Input, "pattern"))
		return fmt.Sprintf("%d matches", len(matches)), map[string]any{"matches": matches}, err
	case "grep", "codesearch":
		nodes, err := files.Search(request.ProjectRoot, stringInput(request.Input, "query"), intInput(request.Input, "limit", 50))
		return fmt.Sprintf("%d matches", len(nodes)), map[string]any{"matches": nodes}, err
	case "science_search":
		if e.Science == nil {
			return "", nil, errors.New("science registry is unavailable")
		}
		hits, err := e.Science.Search(ctx, science.SearchRequest{
			Database: stringInput(request.Input, "database"),
			Query:    stringInput(request.Input, "query"),
			Limit:    intInput(request.Input, "limit", 10),
			Organism: stringInput(request.Input, "organism"),
		})
		return fmt.Sprintf("%d science hits", len(hits)), map[string]any{"hits": hits}, err
	case "artifact":
		if e.Store == nil {
			return "", nil, errors.New("store is unavailable")
		}
		metadata := "{}"
		if value, ok := request.Input["metadata"]; ok && value != nil {
			metadata = storeJSON(value)
		}
		artifact, err := e.Store.CreateArtifact(ctx, store.Artifact{
			SessionID: request.SessionID,
			Type:      stringInput(request.Input, "type"),
			Title:     stringInput(request.Input, "title"),
			Content:   stringInput(request.Input, "content"),
			Metadata:  metadata,
		})
		if err == nil {
			err = e.linkRunEvidenceToArtifact(ctx, request.RunID, artifact.ID)
		}
		return artifact.ID, map[string]any{"artifact": artifact}, err
	case "provenance":
		return e.runProvenance(ctx, request)
	case "skill":
		name := firstNonEmptyString(stringInput(request.Input, "name"), stringInput(request.Input, "skill"))
		if name == "" {
			return "", nil, errors.New("skill name is required")
		}
		catalog, err := e.skillCatalog(ctx, request.ProjectRoot)
		if err != nil {
			return "", nil, err
		}
		loaded, err := skills.Load(name, catalog)
		if err != nil {
			return "", nil, err
		}
		return loaded.Content, map[string]any{"skill": loaded.SkillRecord, "content": loaded.Content}, nil
	case "lsp":
		path := stringInput(request.Input, "path")
		text := stringInput(request.Input, "text")
		if text == "" && path != "" {
			loaded, err := files.ReadText(request.ProjectRoot, path)
			if err != nil {
				return "", nil, err
			}
			text = loaded
		}
		session, err := lsp.Analyze(ctx, stringInput(request.Input, "language"), filepath.Join(request.ProjectRoot, path), text)
		return fmt.Sprintf("%d diagnostics", len(session.Diagnostics)), map[string]any{"session": session}, err
	case "notebook":
		return e.runNotebook(ctx, request)
	case "rkernel":
		return e.runRKernel(ctx, request)
	case "mcp":
		return e.runMCP(ctx, request.Input)
	case "mcp_resource":
		return e.runMCPResource(ctx, request.Input)
	case "mcp_prompt":
		return e.runMCPPrompt(ctx, request.Input)
	case "plugin":
		return e.runPlugin(ctx, request.Input)
	case "share_publish":
		return e.runSharePublish(ctx, request)
	case "webfetch":
		return runWebFetch(ctx, stringInput(request.Input, "url"), intInput(request.Input, "limitBytes", 1_000_000))
	case "websearch":
		return runWebSearch(ctx, stringInput(request.Input, "query"), intInput(request.Input, "limit", 5))
	case "bash":
		command := strings.TrimSpace(stringInput(request.Input, "command"))
		if command == "" {
			return "", nil, errors.New("command is required")
		}
		cmd := exec.CommandContext(ctx, "/bin/sh", "-lc", command)
		cmd.Dir = request.ProjectRoot
		out, err := cmd.CombinedOutput()
		return string(out), map[string]any{"command": command}, err
	case "todo", "todowrite":
		items, err := normalizeTodoItems(request.Input)
		if err != nil {
			return "", nil, err
		}
		return jsonString(items), map[string]any{"items": items}, nil
	case "todoread":
		items, err := e.readLatestTodos(ctx, request.RunID)
		if err != nil {
			return "", nil, err
		}
		return jsonString(items), map[string]any{"items": items}, nil
	case "plan", "planwrite":
		steps, err := normalizePlanSteps(request.Input)
		if err != nil {
			return "", nil, err
		}
		return jsonString(steps), map[string]any{"steps": steps}, nil
	case "task", "subagent":
		return e.runChildAgentTask(ctx, request)
	case "question":
		question := firstNonEmptyString(stringInput(request.Input, "question"), stringInput(request.Input, "content"))
		if question == "" {
			return "", nil, errors.New("question is required")
		}
		return question, map[string]any{"question": question}, nil
	default:
		return "", nil, fmt.Errorf("unsupported execution path for tool %q", request.Tool)
	}
}

func (e Executor) skillCatalog(ctx context.Context, projectRoot string) ([]store.SkillRecord, error) {
	userSkills, err := skills.LoadUser()
	if err != nil {
		return nil, err
	}
	projectSkills, err := skills.LoadProject(projectRoot)
	if err != nil {
		return nil, err
	}
	catalog := append(userSkills, projectSkills...)
	if e.Store != nil {
		persisted, err := e.Store.ListSkills(ctx)
		if err != nil {
			return nil, err
		}
		catalog = append(catalog, persisted...)
	}
	return catalog, nil
}

func normalizeTodoItems(input map[string]any) ([]map[string]string, error) {
	rawItems := sliceInput(input, "items")
	if len(rawItems) == 0 {
		title := firstNonEmptyString(stringInput(input, "title"), stringInput(input, "content"), stringInput(input, "task"))
		if title != "" {
			rawItems = []any{map[string]any{"title": title, "status": stringInput(input, "status"), "priority": stringInput(input, "priority")}}
		}
	}
	if len(rawItems) == 0 {
		return nil, errors.New("todo items are required")
	}
	items := make([]map[string]string, 0, len(rawItems))
	for index, raw := range rawItems {
		item := map[string]string{
			"id":       fmt.Sprintf("todo_%d", index+1),
			"status":   "pending",
			"priority": "normal",
		}
		switch value := raw.(type) {
		case string:
			item["title"] = strings.TrimSpace(value)
		case map[string]any:
			item["title"] = firstNonEmptyString(anyString(value["title"]), anyString(value["content"]), anyString(value["task"]))
			if status := anyString(value["status"]); status != "" {
				item["status"] = status
			}
			if priority := anyString(value["priority"]); priority != "" {
				item["priority"] = priority
			}
			if id := anyString(value["id"]); id != "" {
				item["id"] = id
			}
		default:
			return nil, errors.New("todo items must be strings or objects")
		}
		if strings.TrimSpace(item["title"]) == "" {
			return nil, errors.New("todo item title is required")
		}
		items = append(items, item)
	}
	return items, nil
}

func normalizePlanSteps(input map[string]any) ([]map[string]string, error) {
	rawSteps := sliceInput(input, "steps")
	if len(rawSteps) == 0 {
		title := firstNonEmptyString(stringInput(input, "title"), stringInput(input, "content"), stringInput(input, "step"))
		if title != "" {
			rawSteps = []any{map[string]any{"title": title, "status": stringInput(input, "status"), "notes": stringInput(input, "notes")}}
		}
	}
	if len(rawSteps) == 0 {
		return nil, errors.New("plan steps are required")
	}
	steps := make([]map[string]string, 0, len(rawSteps))
	for index, raw := range rawSteps {
		step := map[string]string{
			"id":     fmt.Sprintf("step_%d", index+1),
			"status": "pending",
		}
		switch value := raw.(type) {
		case string:
			step["title"] = strings.TrimSpace(value)
		case map[string]any:
			step["title"] = firstNonEmptyString(anyString(value["title"]), anyString(value["content"]), anyString(value["step"]))
			if status := anyString(value["status"]); status != "" {
				step["status"] = status
			}
			if notes := anyString(value["notes"]); notes != "" {
				step["notes"] = notes
			}
			if id := anyString(value["id"]); id != "" {
				step["id"] = id
			}
		default:
			return nil, errors.New("plan steps must be strings or objects")
		}
		if strings.TrimSpace(step["title"]) == "" {
			return nil, errors.New("plan step title is required")
		}
		steps = append(steps, step)
	}
	return steps, nil
}

func (e Executor) readLatestTodos(ctx context.Context, runID string) ([]map[string]string, error) {
	if e.Store == nil || strings.TrimSpace(runID) == "" {
		return []map[string]string{}, nil
	}
	calls, err := e.Store.ListToolCalls(ctx, runID)
	if err != nil {
		return nil, err
	}
	for i := len(calls) - 1; i >= 0; i-- {
		call := calls[i]
		if call.Status != "ok" || (call.Tool != "todo" && call.Tool != "todowrite") {
			continue
		}
		var items []map[string]string
		if err := json.Unmarshal([]byte(call.Output), &items); err == nil {
			return items, nil
		}
	}
	return []map[string]string{}, nil
}

func jsonString(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return "[]"
	}
	return string(data)
}

func (e Executor) runChildAgentTask(ctx context.Context, request ExecuteRequest) (string, map[string]any, error) {
	if e.Store == nil {
		return "", nil, errors.New("store is unavailable")
	}
	if strings.TrimSpace(request.SessionID) == "" {
		return "", nil, errors.New("sessionId is required")
	}
	goal := firstNonEmptyString(stringInput(request.Input, "goal"), stringInput(request.Input, "content"), stringInput(request.Input, "task"))
	if goal == "" {
		return "", nil, errors.New("task goal is required")
	}
	agentID := firstNonEmptyString(stringInput(request.Input, "agent"), stringInput(request.Input, "subagent"))
	if agentID == "" {
		if request.Tool == "subagent" {
			agentID = "task"
		} else {
			agentID = "task"
		}
	}
	child, err := e.Store.CreateAgentRun(ctx, store.AgentRun{
		SessionID: request.SessionID,
		Agent:     agentID,
		Status:    "queued",
		Goal:      goal,
	})
	if err != nil {
		return "", nil, err
	}
	metadata := map[string]string{
		"parentRunId": request.RunID,
		"tool":        request.Tool,
	}
	_, err = e.Store.AddAgentStep(ctx, store.AgentStep{
		RunID:    child.ID,
		Kind:     request.Tool,
		Status:   "queued",
		Title:    agentID,
		Content:  goal,
		Metadata: storeJSON(metadata),
	})
	if err != nil {
		return "", nil, err
	}
	output := jsonString(map[string]any{
		"id":        child.ID,
		"agent":     child.Agent,
		"status":    child.Status,
		"goal":      child.Goal,
		"parentRun": request.RunID,
	})
	return output, map[string]any{"run": child, "parentRunId": request.RunID}, nil
}

func (e Executor) runEdit(root string, input map[string]any) (string, map[string]any, error) {
	path := stringInput(input, "path")
	oldText := stringInput(input, "old")
	newText := stringInput(input, "new")
	if path == "" || oldText == "" {
		return "", nil, errors.New("path and old are required")
	}
	content, err := files.ReadText(root, path)
	if err != nil {
		return "", nil, err
	}
	replacements := strings.Count(content, oldText)
	if replacements == 0 {
		return "", nil, errors.New("old text was not found")
	}
	limit := 1
	if boolInput(input, "all", false) {
		limit = -1
	}
	updated := strings.Replace(content, oldText, newText, limit)
	if err := files.WriteText(root, path, updated); err != nil {
		return "", nil, err
	}
	if limit == 1 && replacements > 1 {
		replacements = 1
	}
	return fmt.Sprintf("edited %s (%d replacements)", path, replacements), map[string]any{"path": path, "replacements": replacements}, nil
}

func (e Executor) runMultiEdit(root string, input map[string]any) (string, map[string]any, error) {
	raw, ok := input["edits"].([]any)
	if !ok || len(raw) == 0 {
		return "", nil, errors.New("edits are required")
	}
	applied := 0
	for _, item := range raw {
		edit, ok := mapInput(item)
		if !ok {
			return "", nil, errors.New("each edit must be an object")
		}
		if _, _, err := e.runEdit(root, edit); err != nil {
			return "", nil, err
		}
		applied++
	}
	return fmt.Sprintf("applied %d edits", applied), map[string]any{"edits": applied}, nil
}

func (e Executor) runApplyPatch(ctx context.Context, root string, input map[string]any) (string, map[string]any, error) {
	patch := stringInput(input, "patch")
	if strings.TrimSpace(patch) == "" {
		return "", nil, errors.New("patch is required")
	}
	cmd := exec.CommandContext(ctx, "git", "apply", "--whitespace=nowarn", "-")
	cmd.Dir = root
	cmd.Stdin = strings.NewReader(patch)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if len(out) > 0 {
			return "", nil, fmt.Errorf("%s", strings.TrimSpace(string(out)))
		}
		return "", nil, err
	}
	return "patch applied", nil, nil
}

func (e Executor) runNotebook(ctx context.Context, request ExecuteRequest) (string, map[string]any, error) {
	if e.Store == nil {
		return "", nil, errors.New("store is unavailable")
	}
	notebookID := stringInput(request.Input, "notebookId")
	cellID := stringInput(request.Input, "cellId")
	cell, err := e.Store.GetNotebookCell(ctx, notebookID, cellID)
	if err != nil {
		return "", nil, err
	}
	result, err := notebook.RunCell(ctx, cell)
	if err != nil {
		return "", nil, err
	}
	cell.Output = result.Stdout + result.Stderr
	outputs, _ := json.Marshal(result.Outputs)
	cell.OutputJSON = string(outputs)
	cell.Status = result.Status
	_, _ = e.Store.UpdateNotebookCellOutput(ctx, cell)
	return cell.Output, map[string]any{"result": result}, nil
}

func (e Executor) runRKernel(ctx context.Context, request ExecuteRequest) (string, map[string]any, error) {
	if e.Store == nil {
		return "", nil, errors.New("store is unavailable")
	}
	source := stringInput(request.Input, "source")
	if strings.TrimSpace(source) == "" {
		return "", nil, errors.New("R source is required")
	}
	projectID := ""
	if request.SessionID != "" {
		if session, err := e.Store.GetSession(ctx, request.SessionID); err == nil {
			projectID = session.ProjectID
		}
	}
	created, err := e.Store.CreateNotebook(ctx, store.Notebook{
		ProjectID: projectID,
		Title:     "R kernel task",
		Cells: []store.NotebookCell{{
			Kind:     "code",
			Language: "r",
			Source:   source,
			Status:   "idle",
		}},
	})
	if err != nil {
		return "", nil, err
	}
	cell := created.Cells[0]
	result, err := notebook.RunCell(ctx, cell)
	if err != nil {
		return "", map[string]any{"notebookId": created.ID, "cellId": cell.ID}, err
	}
	cell.Output = result.Stdout + result.Stderr
	outputs, _ := json.Marshal(result.Outputs)
	cell.OutputJSON = string(outputs)
	cell.Status = result.Status
	_, _ = e.Store.UpdateNotebookCellOutput(ctx, cell)
	return cell.Output, map[string]any{"notebook": created, "result": result}, nil
}

func (e Executor) runMCP(ctx context.Context, input map[string]any) (string, map[string]any, error) {
	serverID := stringInput(input, "serverId")
	toolName := stringInput(input, "tool")
	server, err := e.mcpServer(ctx, serverID)
	if err != nil {
		return "", nil, err
	}
	server, err = mcp.PrepareAuthenticatedServer(ctx, server, e.Secrets)
	if err != nil {
		return "", nil, err
	}
	result, err := mcp.Execute(ctx, server, toolName, mapInputOrEmpty(input["input"]), "", nil)
	if err != nil {
		return "", nil, err
	}
	if result.Status == "error" {
		return result.Output, map[string]any{"result": result}, errors.New(result.Output)
	}
	return result.Output, map[string]any{"result": result}, nil
}

func (e Executor) runMCPResource(ctx context.Context, input map[string]any) (string, map[string]any, error) {
	serverID := stringInput(input, "serverId")
	uri := stringInput(input, "uri")
	if serverID == "" || uri == "" {
		return "", nil, errors.New("serverId and uri are required")
	}
	server, err := e.mcpServer(ctx, serverID)
	if err != nil {
		return "", nil, err
	}
	server, err = mcp.PrepareAuthenticatedServer(ctx, server, e.Secrets)
	if err != nil {
		return "", nil, err
	}
	result, err := mcp.ReadResource(ctx, server, uri, "", nil)
	if err != nil {
		return "", nil, err
	}
	if result.Status == "error" {
		return result.Text, map[string]any{"result": result}, errors.New(result.Text)
	}
	return result.Text, map[string]any{"result": result}, nil
}

func (e Executor) runMCPPrompt(ctx context.Context, input map[string]any) (string, map[string]any, error) {
	serverID := stringInput(input, "serverId")
	name := stringInput(input, "name")
	if serverID == "" || name == "" {
		return "", nil, errors.New("serverId and name are required")
	}
	server, err := e.mcpServer(ctx, serverID)
	if err != nil {
		return "", nil, err
	}
	server, err = mcp.PrepareAuthenticatedServer(ctx, server, e.Secrets)
	if err != nil {
		return "", nil, err
	}
	result, err := mcp.GetPrompt(ctx, server, name, mapInputOrEmpty(input["arguments"]), "", nil)
	if err != nil {
		return "", nil, err
	}
	if result.Status == "error" {
		return result.Text, map[string]any{"result": result}, errors.New(result.Text)
	}
	return result.Text, map[string]any{"result": result}, nil
}

func (e Executor) mcpServer(ctx context.Context, id string) (store.MCPServer, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return store.MCPServer{}, errors.New("serverId is required")
	}
	if server, ok := e.MCPServers[id]; ok {
		return server, nil
	}
	if e.Store == nil {
		return store.MCPServer{}, errors.New("store is unavailable")
	}
	return e.Store.GetMCPServer(ctx, id)
}

func (e Executor) runPlugin(ctx context.Context, input map[string]any) (string, map[string]any, error) {
	if e.Store == nil {
		return "", nil, errors.New("store is unavailable")
	}
	pluginID := stringInput(input, "pluginId")
	toolName := stringInput(input, "tool")
	if pluginID == "" || toolName == "" {
		return "", nil, errors.New("pluginId and tool are required")
	}
	plugin, err := e.Store.GetPlugin(ctx, pluginID)
	if err != nil {
		return "", nil, err
	}
	result, err := pluginruntime.Execute(ctx, plugin, toolName, mapInputOrEmpty(input["input"]), "", nil)
	if err != nil {
		return "", nil, err
	}
	if result.Status == "error" {
		return result.Output, map[string]any{"result": result}, errors.New(result.Output)
	}
	return result.Output, map[string]any{"result": result}, nil
}

func (e Executor) runSharePublish(ctx context.Context, request ExecuteRequest) (string, map[string]any, error) {
	if e.Store == nil {
		return "", nil, errors.New("store is unavailable")
	}
	artifactIDs := stringSliceInput(request.Input, "artifactIds")
	artifacts := make([]store.Artifact, 0, len(artifactIDs))
	for _, id := range artifactIDs {
		artifact, err := e.Store.GetArtifact(ctx, id)
		if err != nil {
			return "", nil, err
		}
		artifacts = append(artifacts, artifact)
	}
	bundle, err := sharepkg.BuildBundle(sharepkg.PublishRequest{
		Title:       stringInput(request.Input, "title"),
		Visibility:  stringInput(request.Input, "visibility"),
		ArtifactIDs: artifactIDs,
		SessionID:   request.SessionID,
	}, artifacts)
	if err != nil {
		return "", nil, err
	}
	record := store.Share{
		ID:         fmt.Sprintf("share_%d", time.Now().UnixNano()),
		Title:      stringInput(request.Input, "title"),
		Visibility: stringInput(request.Input, "visibility"),
		Status:     "published",
		Bundle:     bundle,
	}
	if record.Title == "" {
		record.Title = "Superscience share"
	}
	if record.Visibility == "" {
		record.Visibility = "private-link"
	}
	record.URL = "/api/share/" + record.ID
	record, err = e.Store.CreateShare(ctx, record)
	if err != nil {
		return "", nil, err
	}
	for _, artifact := range artifacts {
		_, _ = e.Store.CreateProvenanceEdge(ctx, store.ProvenanceEdge{
			SourceID:   artifact.ID,
			SourceType: "artifact",
			TargetID:   record.ID,
			TargetType: "share",
			Relation:   "published",
		})
	}
	_, _ = e.Store.CreateAuditEvent(ctx, store.AuditEvent{Action: "share.publish", EntityID: record.ID, EntityType: "share"})
	return record.URL, map[string]any{"share": record}, nil
}

func (e Executor) runProvenance(ctx context.Context, request ExecuteRequest) (string, map[string]any, error) {
	if e.Store == nil {
		return "", nil, errors.New("store is unavailable")
	}
	entityID := firstNonEmptyString(stringInput(request.Input, "entityId"), stringInput(request.Input, "id"))
	sourceID := stringInput(request.Input, "sourceId")
	targetID := stringInput(request.Input, "targetId")
	if entityID != "" && sourceID == "" && targetID == "" {
		edges, err := e.Store.ListProvenance(ctx, entityID)
		return jsonString(edges), map[string]any{"edges": edges}, err
	}
	if sourceID == "" || targetID == "" {
		return "", nil, errors.New("sourceId and targetId are required")
	}
	edge, err := e.Store.CreateProvenanceEdge(ctx, store.ProvenanceEdge{
		SourceID:   sourceID,
		SourceType: firstNonEmptyString(stringInput(request.Input, "sourceType"), "entity"),
		TargetID:   targetID,
		TargetType: firstNonEmptyString(stringInput(request.Input, "targetType"), "entity"),
		Relation:   firstNonEmptyString(stringInput(request.Input, "relation"), "derived_from"),
		Metadata:   metadataJSON(request.Input["metadata"]),
	})
	return jsonString(edge), map[string]any{"edge": edge}, err
}

func metadataJSON(value any) string {
	if value == nil {
		return "{}"
	}
	if text, ok := value.(string); ok {
		if strings.TrimSpace(text) == "" {
			return "{}"
		}
		return text
	}
	return storeJSON(value)
}

func runGlob(root, pattern string) ([]string, error) {
	if strings.TrimSpace(pattern) == "" {
		return nil, errors.New("pattern is required")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	rootReal, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		rootReal = rootAbs
	}
	pattern = strings.TrimPrefix(pattern, string(filepath.Separator))
	matches, err := filepath.Glob(filepath.Join(rootReal, pattern))
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(matches))
	for _, match := range matches {
		rel, err := filepath.Rel(rootReal, match)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			continue
		}
		if _, err := os.Stat(match); err == nil {
			result = append(result, rel)
		}
	}
	sort.Strings(result)
	return result, nil
}

func runWebFetch(ctx context.Context, rawURL string, limitBytes int) (string, map[string]any, error) {
	if strings.TrimSpace(rawURL) == "" {
		return "", nil, errors.New("url is required")
	}
	if limitBytes <= 0 || limitBytes > 5_000_000 {
		limitBytes = 1_000_000
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, int64(limitBytes)))
	if err != nil {
		return "", nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", nil, fmt.Errorf("fetch failed: %s", resp.Status)
	}
	return string(data), map[string]any{"url": rawURL, "status": resp.StatusCode}, nil
}

func runWebSearch(ctx context.Context, query string, limit int) (string, map[string]any, error) {
	if strings.TrimSpace(query) == "" {
		return "", nil, errors.New("query is required")
	}
	if limit <= 0 || limit > 10 {
		limit = 5
	}
	searchURL := "https://api.duckduckgo.com/?" + url.Values{
		"q":           []string{query},
		"format":      []string{"json"},
		"no_redirect": []string{"1"},
		"no_html":     []string{"1"},
	}.Encode()
	var payload struct {
		Abstract    string `json:"Abstract"`
		AbstractURL string `json:"AbstractURL"`
		Heading     string `json:"Heading"`
		Related     []struct {
			Text     string `json:"Text"`
			FirstURL string `json:"FirstURL"`
		} `json:"RelatedTopics"`
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return "", nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", nil, errors.New(resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", nil, err
	}
	results := make([]map[string]string, 0, limit)
	if payload.Abstract != "" || payload.AbstractURL != "" {
		results = append(results, map[string]string{"title": payload.Heading, "summary": payload.Abstract, "url": payload.AbstractURL})
	}
	for _, related := range payload.Related {
		if len(results) >= limit {
			break
		}
		if related.Text != "" || related.FirstURL != "" {
			results = append(results, map[string]string{"title": related.Text, "summary": related.Text, "url": related.FirstURL})
		}
	}
	data, _ := json.Marshal(results)
	return string(data), map[string]any{"results": results}, nil
}

func (e Executor) record(ctx context.Context, result ExecuteResult, input map[string]any) (ExecuteResult, error) {
	if e.Store == nil || result.RunID == "" {
		return result, nil
	}
	data := storeJSON(result.Data)
	if result.existingID != "" {
		call, err := e.Store.UpdateToolCall(ctx, result.existingID, result.Status, result.Output, data, result.PermissionID)
		if err != nil {
			return result, err
		}
		result.ID = call.ID
		if err := e.recordResultProvenance(ctx, result); err != nil {
			return result, err
		}
		return result, nil
	}
	call, err := e.Store.CreateToolCall(ctx, store.ToolCall{
		RunID:        result.RunID,
		SessionID:    result.SessionID,
		Tool:         result.Tool,
		Status:       result.Status,
		Input:        storeJSON(input),
		Output:       result.Output,
		Data:         data,
		PermissionID: result.PermissionID,
	})
	if err != nil {
		return result, err
	}
	result.ID = call.ID
	if err := e.recordResultProvenance(ctx, result); err != nil {
		return result, err
	}
	return result, nil
}

func (e Executor) recordResultProvenance(ctx context.Context, result ExecuteResult) error {
	if e.Store == nil || result.Tool != "science_search" || result.Status != "ok" || result.ID == "" {
		return nil
	}
	hits, ok := result.Data["hits"].([]science.Hit)
	if !ok {
		return nil
	}
	for _, hit := range hits {
		if strings.TrimSpace(hit.ID) == "" {
			continue
		}
		source := strings.ToLower(strings.TrimSpace(hit.Source))
		if source == "" {
			source = "science"
		}
		_, err := e.Store.CreateProvenanceEdge(ctx, store.ProvenanceEdge{
			SourceID:   source + ":" + hit.ID,
			SourceType: "science_hit",
			TargetID:   result.ID,
			TargetType: "tool_call",
			Relation:   "observed_in",
			Metadata: storeJSON(map[string]any{
				"title":  hit.Title,
				"url":    hit.URL,
				"source": hit.Source,
			}),
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (e Executor) linkRunEvidenceToArtifact(ctx context.Context, runID, artifactID string) error {
	if e.Store == nil || strings.TrimSpace(runID) == "" || strings.TrimSpace(artifactID) == "" {
		return nil
	}
	calls, err := e.Store.ListToolCalls(ctx, runID)
	if err != nil {
		return err
	}
	for _, call := range calls {
		if call.Status != "ok" {
			continue
		}
		_, err := e.Store.CreateProvenanceEdge(ctx, store.ProvenanceEdge{
			SourceID:   call.ID,
			SourceType: "tool_call",
			TargetID:   artifactID,
			TargetType: "artifact",
			Relation:   "derived_into",
			Metadata:   storeJSON(map[string]string{"tool": call.Tool}),
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func permissionPattern(tool string, input map[string]any) string {
	switch tool {
	case "write", "edit", "multiedit", "apply_patch", "read":
		if path := stringInput(input, "path"); path != "" {
			return path
		}
		if patch := stringInput(input, "patch"); patch != "" {
			return "patch:" + fmt.Sprint(len(patch))
		}
	case "bash":
		if command := stringInput(input, "command"); command != "" {
			return command
		}
	case "science_search":
		return stringInput(input, "database") + ":" + stringInput(input, "query")
	case "webfetch":
		return stringInput(input, "url")
	case "websearch":
		return stringInput(input, "query")
	case "notebook":
		return stringInput(input, "notebookId") + ":" + stringInput(input, "cellId")
	case "rkernel":
		return "r:" + fmt.Sprint(len(stringInput(input, "source")))
	case "mcp":
		return stringInput(input, "serverId") + ":" + stringInput(input, "tool")
	case "mcp_resource":
		return stringInput(input, "serverId") + ":resource:" + stringInput(input, "uri")
	case "mcp_prompt":
		return stringInput(input, "serverId") + ":prompt:" + stringInput(input, "name")
	case "plugin":
		return stringInput(input, "pluginId") + ":" + stringInput(input, "tool")
	case "share_publish":
		return strings.Join(stringSliceInput(input, "artifactIds"), ",")
	}
	return tool
}

func riskType(tool string) string {
	switch tool {
	case "write", "edit", "multiedit", "apply_patch":
		return "write"
	case "bash":
		return "shell"
	case "science_search", "webfetch", "websearch":
		return "network"
	case "share_publish":
		return "share"
	case "mcp", "mcp_resource", "mcp_prompt":
		return "mcp"
	case "plugin":
		return "plugin"
	case "notebook", "rkernel":
		return "notebook"
	default:
		return "tool"
	}
}

func stringInput(input map[string]any, key string) string {
	if value, ok := input[key].(string); ok {
		return value
	}
	return ""
}

func anyString(value any) string {
	if value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func boolInput(input map[string]any, key string, fallback bool) bool {
	if value, ok := input[key].(bool); ok {
		return value
	}
	return fallback
}

func intInput(input map[string]any, key string, fallback int) int {
	switch value := input[key].(type) {
	case int:
		return value
	case float64:
		return int(value)
	default:
		return fallback
	}
}

func sliceInput(input map[string]any, key string) []any {
	raw, ok := input[key]
	if !ok {
		return nil
	}
	switch value := raw.(type) {
	case []any:
		return value
	case []map[string]any:
		result := make([]any, 0, len(value))
		for _, item := range value {
			result = append(result, item)
		}
		return result
	case []string:
		result := make([]any, 0, len(value))
		for _, item := range value {
			result = append(result, item)
		}
		return result
	default:
		return nil
	}
}

func stringSliceInput(input map[string]any, key string) []string {
	raw, ok := input[key]
	if !ok {
		return nil
	}
	switch values := raw.(type) {
	case []string:
		return values
	case []any:
		result := make([]string, 0, len(values))
		for _, value := range values {
			if text, ok := value.(string); ok {
				result = append(result, text)
			}
		}
		return result
	default:
		return nil
	}
}

func mapInput(value any) (map[string]any, bool) {
	if value == nil {
		return nil, false
	}
	switch typed := value.(type) {
	case map[string]any:
		return typed, true
	case map[string]string:
		result := make(map[string]any, len(typed))
		for key, value := range typed {
			result[key] = value
		}
		return result, true
	default:
		data, err := json.Marshal(value)
		if err != nil {
			return nil, false
		}
		var result map[string]any
		if err := json.Unmarshal(data, &result); err != nil {
			return nil, false
		}
		return result, true
	}
}

func mapInputOrEmpty(value any) map[string]any {
	result, ok := mapInput(value)
	if !ok {
		return map[string]any{}
	}
	return result
}

func storeJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(data)
}
