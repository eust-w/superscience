package store_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"superscience/internal/store"
)

func TestStorePersistsProjectsSessionsAndMessages(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "superscience.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	project, err := db.OpenProject(ctx, "/tmp/kinase-screen")
	if err != nil {
		t.Fatalf("open project: %v", err)
	}
	if project.Name != "kinase-screen" {
		t.Fatalf("project name = %q", project.Name)
	}

	session, err := db.CreateSession(ctx, store.CreateSessionInput{
		ProjectID: project.ID,
		Title:     "Literature review",
		Agent:     "biology",
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if session.ProjectID != project.ID || session.Agent != "biology" {
		t.Fatalf("unexpected session: %+v", session)
	}

	message, err := db.AddMessage(ctx, store.AddMessageInput{
		SessionID: session.ID,
		Role:      "user",
		Content:   "Find EGFR inhibitor papers",
	})
	if err != nil {
		t.Fatalf("add message: %v", err)
	}
	if message.Role != "user" {
		t.Fatalf("message role = %q", message.Role)
	}

	messages, err := db.ListMessages(ctx, session.ID)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(messages) != 1 || messages[0].Content != "Find EGFR inhibitor papers" {
		t.Fatalf("messages = %+v", messages)
	}
	part, err := db.AddMessagePart(ctx, store.MessagePart{
		MessageID: message.ID,
		SessionID: session.ID,
		Type:      "tool",
		Title:     "skill",
		Content:   "loaded skill research-protocol",
		Metadata:  `{"name":"research-protocol"}`,
	})
	if err != nil {
		t.Fatalf("add message part: %v", err)
	}
	parts, err := db.ListMessageParts(ctx, message.ID)
	if err != nil {
		t.Fatalf("list message parts: %v", err)
	}
	if len(parts) != 1 || parts[0].ID != part.ID || parts[0].Title != "skill" {
		t.Fatalf("message parts = %+v", parts)
	}

	projects, err := db.ListProjects(ctx)
	if err != nil {
		t.Fatalf("list projects: %v", err)
	}
	if len(projects) != 1 || projects[0].ID != project.ID {
		t.Fatalf("projects = %+v", projects)
	}
}

func TestStorePersistsSettingsAndUpsertsCredentialByProvider(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "superscience.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	if err := db.SetSettings(ctx, map[string]string{
		"theme":       "dark",
		"defaultMode": "analysis",
	}); err != nil {
		t.Fatalf("set settings: %v", err)
	}
	settings, err := db.ListSettings(ctx)
	if err != nil {
		t.Fatalf("list settings: %v", err)
	}
	if settings["theme"] != "dark" || settings["defaultMode"] != "analysis" {
		t.Fatalf("settings = %+v", settings)
	}

	first, err := db.UpsertCredential(ctx, store.Credential{
		Provider: "openai",
		Label:    "OpenAI",
		Source:   "local",
		Redacted: "sk...1111",
	})
	if err != nil {
		t.Fatalf("insert credential: %v", err)
	}
	second, err := db.UpsertCredential(ctx, store.Credential{
		Provider: "openai",
		Label:    "OpenAI updated",
		Source:   "local",
		Redacted: "sk...2222",
	})
	if err != nil {
		t.Fatalf("update credential: %v", err)
	}
	if first.ID != second.ID || second.Redacted != "sk...2222" {
		t.Fatalf("credential was not upserted by provider: first=%+v second=%+v", first, second)
	}
	credentials, err := db.ListCredentials(ctx)
	if err != nil {
		t.Fatalf("list credentials: %v", err)
	}
	if len(credentials) != 1 {
		t.Fatalf("credentials = %+v", credentials)
	}
}

func TestStoreMigratesSyntheticRuntimeSettingsToRealOnlyModes(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "superscience.db")
	db, err := store.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetSettings(ctx, map[string]string{
		"defaultProvider": "mock",
		"scienceNetwork":  "live-with-fallback",
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = store.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	settings, err := db.ListSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if settings["defaultProvider"] != "" {
		t.Fatalf("defaultProvider = %q", settings["defaultProvider"])
	}
	if settings["scienceNetwork"] != "live-only" {
		t.Fatalf("scienceNetwork = %q", settings["scienceNetwork"])
	}
}

func TestStoreReplacesModelCatalogCache(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "superscience.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	if err := db.ReplaceModelCatalog(ctx, []store.ModelRecord{
		{
			ID:            "gpt-catalog-test",
			ProviderID:    "openai",
			DisplayName:   "GPT Catalog Test",
			ContextWindow: 262144,
			Capabilities:  []string{"chat", "stream", "tools"},
			Status:        "refreshed",
		},
		{
			ID:            "claude-catalog-test",
			ProviderID:    "anthropic",
			DisplayName:   "Claude Catalog Test",
			ContextWindow: 200000,
			Capabilities:  []string{"chat", "stream", "tools", "vision"},
			Status:        "refreshed",
		},
	}); err != nil {
		t.Fatalf("replace model catalog: %v", err)
	}
	if err := db.ReplaceModelCatalog(ctx, []store.ModelRecord{
		{
			ID:            "gpt-catalog-new",
			ProviderID:    "openai",
			DisplayName:   "GPT Catalog New",
			ContextWindow: 128000,
			Capabilities:  []string{"chat"},
			Status:        "refreshed",
		},
	}); err != nil {
		t.Fatalf("replace model catalog second time: %v", err)
	}

	openAIModels, err := db.ListModels(ctx, "openai")
	if err != nil {
		t.Fatalf("list openai models: %v", err)
	}
	if len(openAIModels) != 1 || openAIModels[0].ID != "gpt-catalog-new" {
		t.Fatalf("openai models = %+v", openAIModels)
	}
	anthropicModels, err := db.ListModels(ctx, "anthropic")
	if err != nil {
		t.Fatalf("list anthropic models: %v", err)
	}
	if len(anthropicModels) != 0 {
		t.Fatalf("stale anthropic models = %+v", anthropicModels)
	}
}

func TestStorePersistsMCPDiscoveryCache(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "superscience.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	first, err := db.UpsertMCPDiscovery(ctx, store.MCPDiscoveryCache{
		ServerID: "lab-mcp",
		Payload:  `{"tools":[{"name":"lookup"}],"resources":[{"uri":"file:///dataset.csv"}],"prompts":[{"name":"summarize"}]}`,
		Status:   "live",
	})
	if err != nil {
		t.Fatalf("upsert discovery cache: %v", err)
	}
	if first.ServerID != "lab-mcp" || first.Status != "live" || first.UpdatedAt == 0 {
		t.Fatalf("first cache = %+v", first)
	}
	second, err := db.UpsertMCPDiscovery(ctx, store.MCPDiscoveryCache{
		ServerID: "lab-mcp",
		Payload:  `{"tools":[{"name":"lookup2"}],"resources":[],"prompts":[]}`,
		Status:   "refreshed",
	})
	if err != nil {
		t.Fatalf("upsert second discovery cache: %v", err)
	}
	cached, err := db.GetMCPDiscovery(ctx, "lab-mcp")
	if err != nil {
		t.Fatalf("get discovery cache: %v", err)
	}
	if cached.ServerID != "lab-mcp" || cached.Payload != second.Payload || cached.Status != "refreshed" {
		t.Fatalf("cached = %+v second=%+v", cached, second)
	}
}

func TestStorePersistsMCPCommandArguments(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "superscience.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	saved, err := db.UpsertMCPServer(ctx, store.MCPServer{
		ID:        "lab-mcp",
		Name:      "Lab MCP",
		Transport: "stdio",
		Command:   "/usr/bin/env",
		Args:      []string{"python3", "-m", "lab_mcp", "--safe-mode"},
		Enabled:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Args) != 4 {
		t.Fatalf("saved args = %#v", saved.Args)
	}

	loaded, err := db.GetMCPServer(ctx, saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(loaded.Args, "|") != "python3|-m|lab_mcp|--safe-mode" {
		t.Fatalf("loaded args = %#v", loaded.Args)
	}
}

func TestStorePersistsPluginCommandArguments(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "superscience.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	saved, err := db.UpsertPlugin(ctx, store.PluginRecord{
		ID: "lab-plugin", Name: "Lab Plugin", Runtime: "subprocess", Command: "/usr/bin/env",
		Args: []string{"python3", "-m", "superscience_lab_plugin"}, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := db.GetPlugin(ctx, saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(loaded.Args, "|") != "python3|-m|superscience_lab_plugin" {
		t.Fatalf("plugin args = %#v", loaded.Args)
	}
}

func TestStorePersistsPermissionDecisionAndRunCheckpoint(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "superscience.db")
	db, err := store.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	permission, err := db.CreatePermission(ctx, store.PermissionRecord{
		ID:        "permission_resume_1",
		SessionID: "session_1",
		Type:      "write",
		Pattern:   "notes/egfr.md",
		Message:   "Approve write",
		Status:    "pending",
	})
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := db.UpsertRunCheckpoint(ctx, store.RunCheckpoint{
		RunID:        "run_1",
		SessionID:    "session_1",
		PermissionID: permission.ID,
		ToolCallID:   "tool_1",
		State:        `{"nextTurn":2}`,
		Status:       "waiting_permission",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.UpdatePermissionStatus(ctx, permission.ID, "once"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := store.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	loadedPermission, err := reopened.GetPermission(ctx, permission.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loadedPermission.Status != "once" || loadedPermission.Pattern != "notes/egfr.md" {
		t.Fatalf("permission = %+v", loadedPermission)
	}
	loadedCheckpoint, err := reopened.GetRunCheckpoint(ctx, checkpoint.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if loadedCheckpoint.PermissionID != permission.ID || loadedCheckpoint.ToolCallID != "tool_1" || loadedCheckpoint.State != `{"nextTurn":2}` {
		t.Fatalf("checkpoint = %+v", loadedCheckpoint)
	}
	if err := reopened.DeleteRunCheckpoint(ctx, checkpoint.RunID); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.GetRunCheckpoint(ctx, checkpoint.RunID); err == nil {
		t.Fatal("deleted checkpoint remained readable")
	}
}

func TestStoreUpdatesPendingToolCallInPlace(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "superscience.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	call, err := db.CreateToolCall(ctx, store.ToolCall{
		RunID:        "run_1",
		SessionID:    "session_1",
		Tool:         "write",
		Status:       "pending_permission",
		Input:        `{"path":"notes/egfr.md"}`,
		PermissionID: "permission_1",
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := db.UpdateToolCall(ctx, call.ID, "ok", "approved output", `{"approved":true}`, call.PermissionID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != call.ID || updated.Status != "ok" || updated.Output != "approved output" {
		t.Fatalf("updated call = %+v", updated)
	}
	calls, err := db.ListToolCalls(ctx, call.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].ID != call.ID || calls[0].Status != "ok" {
		t.Fatalf("tool calls = %+v", calls)
	}
}

func TestStoreListsArtifactsBySession(t *testing.T) {
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
	artifacts, err := db.ListArtifactsBySession(ctx, "session_a")
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 1 || artifacts[0].SessionID != "session_a" || artifacts[0].Title != "A" {
		t.Fatalf("session artifacts = %+v", artifacts)
	}
}

func TestStorePersistsAndAnswersPendingAction(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "superscience.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	action, err := db.CreatePendingAction(ctx, store.PendingAction{
		RunID:     "run_1",
		SessionID: "session_1",
		Kind:      "question",
		Payload:   `{"question":"Which organism?"}`,
		Status:    "waiting_input",
	})
	if err != nil {
		t.Fatal(err)
	}
	if action.ID == "" {
		t.Fatal("pending action id is empty")
	}
	answered, err := db.AnswerPendingAction(ctx, action.ID, "human")
	if err != nil {
		t.Fatal(err)
	}
	if answered.Status != "answered" || answered.Response != "human" {
		t.Fatalf("answered action = %+v", answered)
	}
	loaded, err := db.GetPendingAction(ctx, action.ID)
	if err != nil || loaded.RunID != "run_1" || loaded.Response != "human" {
		t.Fatalf("loaded action = %+v err=%v", loaded, err)
	}
}

func TestStorePersistsNotebookRichOutputJSON(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "superscience.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	notebook, err := db.CreateNotebook(ctx, store.Notebook{
		Path:  "analysis.ipynb",
		Title: "Rich output",
		Cells: []store.NotebookCell{{Kind: "code", Language: "python", Source: "display('EGFR')"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	cell := notebook.Cells[0]
	cell.Output = "EGFR"
	cell.OutputJSON = `[{"output_type":"display_data","data":{"text/html":"<b>EGFR</b>"},"metadata":{}}]`
	cell.Status = "ok"
	if _, err := db.UpdateNotebookCellOutput(ctx, cell); err != nil {
		t.Fatal(err)
	}
	loaded, err := db.GetNotebookCell(ctx, notebook.ID, cell.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.OutputJSON != cell.OutputJSON || loaded.Output != "EGFR" || loaded.Status != "ok" {
		t.Fatalf("loaded rich output = %+v", loaded)
	}
}

func TestListNotebooksDoesNotReenterTheSingleSQLiteConnection(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "superscience.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.CreateNotebook(ctx, store.Notebook{
		Path:  "deadlock-regression.ipynb",
		Title: "Deadlock regression",
		Cells: []store.NotebookCell{{Kind: "code", Language: "python", Source: "print(1)"}},
	}); err != nil {
		t.Fatal(err)
	}
	queryContext, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	notebooks, err := db.ListNotebooks(queryContext)
	if err != nil {
		t.Fatalf("list notebooks: %v", err)
	}
	if len(notebooks) != 1 || len(notebooks[0].Cells) != 1 {
		t.Fatalf("notebooks = %+v", notebooks)
	}
}

func TestPersistedPermissionDecisionSurvivesRestartAndConsumesAllowOnce(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "superscience.db")
	db, err := store.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	permission, err := db.CreatePermission(ctx, store.PermissionRecord{SessionID: "session_1", Type: "notebook", Pattern: "notebook_1:cell_1", Status: "pending"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.UpdatePermissionStatus(ctx, permission.ID, "once"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := store.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	decision, allowed, err := reopened.ConsumePermissionDecision(ctx, "session_1", "notebook", "notebook_1:cell_1")
	if err != nil || !allowed || decision != "once" {
		t.Fatalf("first consume = decision %q allowed %v err %v", decision, allowed, err)
	}
	decision, allowed, err = reopened.ConsumePermissionDecision(ctx, "session_1", "notebook", "notebook_1:cell_1")
	if err != nil || allowed || decision != "" {
		t.Fatalf("second consume = decision %q allowed %v err %v", decision, allowed, err)
	}
}

func TestStorePersistsCustomAgentLifecycle(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "superscience.db")
	db, err := store.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	created, err := db.CreateAgent(ctx, store.AgentRecord{
		ID:           "chemistry-review",
		Name:         "Chemistry Review",
		Domain:       "chemistry",
		Description:  "Reviews chemistry evidence.",
		SystemPrompt: "Require structure-backed evidence and explicit uncertainty.",
		Tools:        []string{"read", "grep", "science_search"},
		ReviewMode:   "required",
		Enabled:      true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != "chemistry-review" || created.BuiltIn || created.CreatedAt == 0 || created.UpdatedAt == 0 {
		t.Fatalf("created agent = %+v", created)
	}
	created.Enabled = false
	created.SystemPrompt = "Updated chemistry review instructions."
	updated, err := db.UpdateAgent(ctx, created)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Enabled || updated.SystemPrompt != created.SystemPrompt || updated.UpdatedAt < created.UpdatedAt {
		t.Fatalf("updated agent = %+v", updated)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := store.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	loaded, err := reopened.GetAgent(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SystemPrompt != created.SystemPrompt || loaded.Enabled || strings.Join(loaded.Tools, ",") != "read,grep,science_search" {
		t.Fatalf("reopened agent = %+v", loaded)
	}
	agents, err := reopened.ListAgents(ctx)
	if err != nil || len(agents) != 1 || agents[0].ID != created.ID {
		t.Fatalf("agents = %+v err=%v", agents, err)
	}
	if err := reopened.DeleteAgent(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.GetAgent(ctx, created.ID); err == nil {
		t.Fatal("deleted agent remained readable")
	}
}
