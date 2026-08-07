package tools_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	mcpclient "superscience/internal/mcp"
	pluginruntime "superscience/internal/plugins"
	"superscience/internal/science"
	"superscience/internal/store"
	"superscience/internal/tools"
)

func TestExecutorRunsRegisteredFileAndNetworkTools(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	executor := tools.Executor{}

	write, err := executor.Execute(ctx, tools.ExecuteRequest{
		Tool:        "write",
		ProjectRoot: root,
		Input:       map[string]any{"path": "notes/a.txt", "content": "hello\n"},
	})
	if err != nil || write.Status != "ok" {
		t.Fatalf("write = %+v err=%v", write, err)
	}

	edit, err := executor.Execute(ctx, tools.ExecuteRequest{
		Tool:        "edit",
		ProjectRoot: root,
		Input:       map[string]any{"path": "notes/a.txt", "old": "hello", "new": "hi"},
	})
	if err != nil || edit.Status != "ok" {
		t.Fatalf("edit = %+v err=%v", edit, err)
	}

	multiedit, err := executor.Execute(ctx, tools.ExecuteRequest{
		Tool:        "multiedit",
		ProjectRoot: root,
		Input: map[string]any{"edits": []any{
			map[string]any{"path": "notes/a.txt", "old": "hi", "new": "hey"},
			map[string]any{"path": "notes/a.txt", "old": "hey", "new": "patched"},
		}},
	})
	if err != nil || multiedit.Status != "ok" {
		t.Fatalf("multiedit = %+v err=%v", multiedit, err)
	}

	patch := "diff --git a/notes/a.txt b/notes/a.txt\n--- a/notes/a.txt\n+++ b/notes/a.txt\n@@ -1 +1 @@\n-patched\n+patched via diff\n"
	apply, err := executor.Execute(ctx, tools.ExecuteRequest{
		Tool:        "apply_patch",
		ProjectRoot: root,
		Input:       map[string]any{"patch": patch},
	})
	if err != nil || apply.Status != "ok" {
		t.Fatalf("apply_patch = %+v err=%v", apply, err)
	}

	read, err := executor.Execute(ctx, tools.ExecuteRequest{
		Tool:        "read",
		ProjectRoot: root,
		Input:       map[string]any{"path": "notes/a.txt"},
	})
	if err != nil || !strings.Contains(read.Output, "patched via diff") {
		t.Fatalf("read = %+v err=%v", read, err)
	}

	glob, err := executor.Execute(ctx, tools.ExecuteRequest{
		Tool:        "glob",
		ProjectRoot: root,
		Input:       map[string]any{"pattern": "notes/*.txt"},
	})
	if err != nil || glob.Status != "ok" || !strings.Contains(glob.Output, "1 matches") {
		t.Fatalf("glob = %+v err=%v", glob, err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("research payload"))
	}))
	defer server.Close()

	fetch, err := executor.Execute(ctx, tools.ExecuteRequest{
		Tool:        "webfetch",
		ProjectRoot: root,
		Input:       map[string]any{"url": server.URL},
	})
	if err != nil || fetch.Status != "ok" || !strings.Contains(fetch.Output, "research payload") {
		t.Fatalf("webfetch = %+v err=%v", fetch, err)
	}
}

func TestExecutorResumesPersistedApprovedToolCallInPlace(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "superscience.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	root := t.TempDir()
	permission, err := db.CreatePermission(ctx, store.PermissionRecord{
		ID:        "permission_write_1",
		SessionID: "session_1",
		Type:      "write",
		Pattern:   "notes/egfr.md",
		Status:    "once",
	})
	if err != nil {
		t.Fatal(err)
	}
	call, err := db.CreateToolCall(ctx, store.ToolCall{
		RunID:        "run_1",
		SessionID:    "session_1",
		Tool:         "write",
		Status:       "pending_permission",
		Input:        `{"path":"notes/egfr.md","content":"approved evidence"}`,
		PermissionID: permission.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := (tools.Executor{Store: db}).Execute(ctx, tools.ExecuteRequest{
		RunID:              call.RunID,
		SessionID:          call.SessionID,
		Tool:               call.Tool,
		ProjectRoot:        root,
		Input:              map[string]any{"path": "notes/egfr.md", "content": "approved evidence"},
		PermissionID:       permission.ID,
		ExistingToolCallID: call.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ID != call.ID || result.Status != "ok" {
		t.Fatalf("resumed result = %+v", result)
	}
	content, err := os.ReadFile(filepath.Join(root, "notes", "egfr.md"))
	if err != nil || string(content) != "approved evidence" {
		t.Fatalf("approved write content=%q err=%v", content, err)
	}
	calls, err := db.ListToolCalls(ctx, call.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].ID != call.ID || calls[0].Status != "ok" {
		t.Fatalf("resumed call was not updated in place: %+v", calls)
	}
}

func TestExecutorDoesNotRunRejectedPersistedToolCall(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "superscience.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	root := t.TempDir()
	permission, err := db.CreatePermission(ctx, store.PermissionRecord{
		ID:        "permission_write_rejected",
		SessionID: "session_1",
		Type:      "write",
		Pattern:   "notes/rejected.md",
		Status:    "reject",
	})
	if err != nil {
		t.Fatal(err)
	}
	call, err := db.CreateToolCall(ctx, store.ToolCall{
		RunID:        "run_1",
		SessionID:    "session_1",
		Tool:         "write",
		Status:       "pending_permission",
		Input:        `{"path":"notes/rejected.md","content":"must not exist"}`,
		PermissionID: permission.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := (tools.Executor{Store: db}).Execute(ctx, tools.ExecuteRequest{
		RunID:              call.RunID,
		SessionID:          call.SessionID,
		Tool:               call.Tool,
		ProjectRoot:        root,
		Input:              map[string]any{"path": "notes/rejected.md", "content": "must not exist"},
		PermissionID:       permission.ID,
		ExistingToolCallID: call.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "permission_denied" {
		t.Fatalf("rejected result = %+v", result)
	}
	if _, err := os.Stat(filepath.Join(root, "notes", "rejected.md")); !os.IsNotExist(err) {
		t.Fatalf("rejected write executed unexpectedly: %v", err)
	}
}

func TestExecutorLoadsSkillToolContent(t *testing.T) {
	ctx := context.Background()
	executor := tools.Executor{}
	definition, ok := tools.Find("skill")
	if !ok || definition.Category != "agent" {
		t.Fatalf("skill tool definition missing: %+v ok=%v", definition, ok)
	}

	result, err := executor.Execute(ctx, tools.ExecuteRequest{
		Tool:  "skill",
		Input: map[string]any{"name": "research-protocol"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "ok" || !strings.Contains(result.Output, "Structure literature search") {
		t.Fatalf("skill result = %+v", result)
	}
	if result.Data["skill"] == nil || result.Data["content"] == nil {
		t.Fatalf("skill data missing: %+v", result.Data)
	}
}

func TestExecutorLoadsProjectSkillToolContent(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: project-evidence\ndescription: Project evidence skill.\n---\n# Project Evidence\nUse project-level evidence inside tool calls.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := (tools.Executor{Store: db}).Execute(ctx, tools.ExecuteRequest{
		RunID:       "run_project_skill_tool",
		SessionID:   "session_project_skill_tool",
		Tool:        "skill",
		ProjectRoot: projectRoot,
		Input:       map[string]any{"name": "project-evidence"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ID == "" || result.Status != "ok" || !strings.Contains(result.Output, "project-level evidence") {
		t.Fatalf("project skill tool result = %+v", result)
	}
	calls, err := db.ListToolCalls(ctx, "run_project_skill_tool")
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].Tool != "skill" || !strings.Contains(calls[0].Output, "project-level evidence") {
		t.Fatalf("project skill tool call not persisted: %+v", calls)
	}
}

func TestExecutorLoadsUserSkillToolContent(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	t.Setenv("SUPERSCIENCE_DATA_DIR", dataDir)
	skillDir := filepath.Join(dataDir, "skills", "user-evidence")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: user-evidence\ndescription: User evidence skill.\n---\n# User Evidence\nUse user-level evidence inside tool calls.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := (tools.Executor{}).Execute(ctx, tools.ExecuteRequest{
		Tool:  "skill",
		Input: map[string]any{"name": "user-evidence"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "ok" || !strings.Contains(result.Output, "user-level evidence") {
		t.Fatalf("user skill tool result = %+v", result)
	}
}

func TestExecutorPersistsAndReadsStructuredTodos(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "superscience.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, id := range []string{"todoread", "todowrite"} {
		definition, ok := tools.Find(id)
		if !ok || definition.Category != "agent" {
			t.Fatalf("%s definition = %+v ok=%v", id, definition, ok)
		}
	}
	executor := tools.Executor{Store: db}

	written, err := executor.Execute(ctx, tools.ExecuteRequest{
		RunID:     "run_todo",
		SessionID: "session_todo",
		Tool:      "todowrite",
		Input: map[string]any{"items": []any{
			map[string]any{"title": "Read EGFR paper", "status": "in_progress", "priority": "high"},
			map[string]any{"content": "Extract provenance", "status": "pending"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if written.Status != "ok" || !strings.Contains(written.Output, "Read EGFR paper") || !strings.Contains(written.Output, "Extract provenance") {
		t.Fatalf("todowrite = %+v", written)
	}
	if written.Data["items"] == nil {
		t.Fatalf("todowrite data missing: %+v", written.Data)
	}

	read, err := executor.Execute(ctx, tools.ExecuteRequest{
		RunID:     "run_todo",
		SessionID: "session_todo",
		Tool:      "todoread",
	})
	if err != nil {
		t.Fatal(err)
	}
	if read.Status != "ok" || !strings.Contains(read.Output, "Read EGFR paper") || !strings.Contains(read.Output, "in_progress") {
		t.Fatalf("todoread = %+v", read)
	}
	calls, err := db.ListToolCalls(ctx, "run_todo")
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[0].Tool != "todowrite" || calls[1].Tool != "todoread" {
		t.Fatalf("todo calls = %+v", calls)
	}
}

func TestExecutorWritesStructuredPlanSteps(t *testing.T) {
	ctx := context.Background()
	executor := tools.Executor{}

	result, err := executor.Execute(ctx, tools.ExecuteRequest{
		Tool: "planwrite",
		Input: map[string]any{"steps": []any{
			map[string]any{"title": "Search PubMed", "status": "done"},
			map[string]any{"title": "Render artifact", "status": "pending", "notes": "Use provenance"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "ok" || !strings.Contains(result.Output, "Search PubMed") || !strings.Contains(result.Output, "Render artifact") {
		t.Fatalf("planwrite = %+v", result)
	}
	if result.Data["steps"] == nil {
		t.Fatalf("planwrite data missing: %+v", result.Data)
	}
}

func TestBuiltInResearchAgentExposesStructuredTodoTools(t *testing.T) {
	var research tools.AgentDefinition
	for _, agent := range tools.BuiltInAgents() {
		if agent.ID == "research" {
			research = agent
			break
		}
	}
	if research.ID == "" {
		t.Fatal("research agent missing")
	}
	for _, want := range []string{"todoread", "todowrite", "provenance"} {
		found := false
		for _, tool := range research.Tools {
			if tool == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("research agent tools missing %s: %+v", want, research.Tools)
		}
	}
}

func TestExecutorCreatesChildTaskRun(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "superscience.db"))
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
	parent, err := db.CreateAgentRun(ctx, store.AgentRun{SessionID: session.ID, Agent: "research", Goal: "Parent run"})
	if err != nil {
		t.Fatal(err)
	}

	result, err := (tools.Executor{Store: db}).Execute(ctx, tools.ExecuteRequest{
		RunID:     parent.ID,
		SessionID: session.ID,
		Tool:      "task",
		Input: map[string]any{
			"goal":  "Review EGFR resistance evidence",
			"agent": "literature-review",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "ok" || !strings.Contains(result.Output, "Review EGFR resistance evidence") || !strings.Contains(result.Output, "literature-review") {
		t.Fatalf("task result = %+v", result)
	}
	child, ok := result.Data["run"].(store.AgentRun)
	if !ok || child.ID == "" || child.Agent != "literature-review" || child.Status != "queued" {
		t.Fatalf("child run data = %+v", result.Data)
	}
	runs, err := db.ListAgentRuns(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	var sawChild bool
	for _, run := range runs {
		if run.ID == child.ID && run.Goal == "Review EGFR resistance evidence" {
			sawChild = true
		}
	}
	if !sawChild {
		t.Fatalf("child run was not persisted: %+v", runs)
	}
	steps, err := db.ListAgentSteps(ctx, child.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 1 || steps[0].Kind != "task" || !strings.Contains(steps[0].Content, "Review EGFR") {
		t.Fatalf("child steps = %+v", steps)
	}
	calls, err := db.ListToolCalls(ctx, parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].Tool != "task" || !strings.Contains(calls[0].Output, child.ID) {
		t.Fatalf("parent tool calls = %+v", calls)
	}
}

func TestExecutorCreatesSubagentRun(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "superscience.db"))
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
	parent, err := db.CreateAgentRun(ctx, store.AgentRun{SessionID: session.ID, Agent: "research", Goal: "Parent run"})
	if err != nil {
		t.Fatal(err)
	}

	result, err := (tools.Executor{Store: db}).Execute(ctx, tools.ExecuteRequest{
		RunID:     parent.ID,
		SessionID: session.ID,
		Tool:      "subagent",
		Input: map[string]any{
			"content": "Critique citation coverage",
			"agent":   "critic",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	child, ok := result.Data["run"].(store.AgentRun)
	if !ok || child.Agent != "critic" || child.Status != "queued" {
		t.Fatalf("subagent data = %+v result=%+v", result.Data, result)
	}
	steps, err := db.ListAgentSteps(ctx, child.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 1 || steps[0].Kind != "subagent" || steps[0].Title != "critic" {
		t.Fatalf("subagent steps = %+v", steps)
	}
}

func TestExecutorPersistsArtifactMetadata(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "tools.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	executor := tools.Executor{Store: db}

	result, err := executor.Execute(ctx, tools.ExecuteRequest{
		RunID:     "run_artifact_metadata",
		SessionID: "session_artifact_metadata",
		Tool:      "artifact",
		Input: map[string]any{
			"type":    "markdown",
			"title":   "Evidence",
			"content": "# Evidence",
			"metadata": map[string]any{
				"database": "pubmed",
				"limit":    5,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := db.GetArtifact(ctx, result.Output)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(artifact.Metadata, `"database":"pubmed"`) || !strings.Contains(artifact.Metadata, `"limit":5`) {
		t.Fatalf("artifact metadata = %s", artifact.Metadata)
	}
}

func TestExecutorLinksScienceEvidenceToArtifactProvenance(t *testing.T) {
	pubmed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/esearch.fcgi":
			_ = json.NewEncoder(w).Encode(map[string]any{"esearchresult": map[string]any{"idlist": []string{"123"}}})
		case "/esummary.fcgi":
			_ = json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{
				"uids": []string{"123"},
				"123":  map[string]any{"title": "EGFR evidence", "source": "PubMed", "pubdate": "2026"},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer pubmed.Close()

	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "tools.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	executor := tools.Executor{
		Store:   db,
		Science: science.NewRegistryWithEndpoints(pubmed.Client(), science.Endpoints{PubMed: pubmed.URL}),
	}
	search, err := executor.Execute(ctx, tools.ExecuteRequest{
		RunID: "run_science_artifact",
		Tool:  "science_search",
		Input: map[string]any{"database": "pubmed", "query": "EGFR", "limit": 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := executor.Execute(ctx, tools.ExecuteRequest{
		RunID: "run_science_artifact",
		Tool:  "artifact",
		Input: map[string]any{"type": "markdown", "title": "Evidence", "content": "PMID 123"},
	})
	if err != nil {
		t.Fatal(err)
	}
	searchEdges, err := db.ListProvenance(ctx, search.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(searchEdges) != 2 {
		t.Fatalf("science tool provenance = %+v", searchEdges)
	}
	var sawHit, sawArtifact bool
	for _, edge := range searchEdges {
		if edge.SourceType == "science_hit" && edge.SourceID == "pubmed:123" && edge.TargetID == search.ID && edge.Relation == "observed_in" {
			sawHit = true
		}
		if edge.SourceID == search.ID && edge.TargetID == artifact.Output && edge.Relation == "derived_into" {
			sawArtifact = true
		}
	}
	if !sawHit || !sawArtifact {
		t.Fatalf("science-to-artifact provenance incomplete: %+v", searchEdges)
	}
}

func TestExecutorCreatesAndReadsProvenanceEdges(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "superscience.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	definition, ok := tools.Find("provenance")
	if !ok || definition.Category != "research" {
		t.Fatalf("provenance definition = %+v ok=%v", definition, ok)
	}

	created, err := (tools.Executor{Store: db}).Execute(ctx, tools.ExecuteRequest{
		RunID:     "run_provenance",
		SessionID: "session_provenance",
		Tool:      "provenance",
		Input: map[string]any{
			"sourceId":   "artifact_egfr",
			"sourceType": "artifact",
			"targetId":   "share_packet",
			"targetType": "share",
			"relation":   "published",
			"metadata":   map[string]any{"note": "EGFR evidence packet"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.Status != "ok" || !strings.Contains(created.Output, "artifact_egfr") || !strings.Contains(created.Output, "share_packet") {
		t.Fatalf("created provenance = %+v", created)
	}
	edge, ok := created.Data["edge"].(store.ProvenanceEdge)
	if !ok || edge.ID == "" || edge.Relation != "published" || !strings.Contains(edge.Metadata, "EGFR evidence") {
		t.Fatalf("created provenance data = %+v", created.Data)
	}

	read, err := (tools.Executor{Store: db}).Execute(ctx, tools.ExecuteRequest{
		RunID:     "run_provenance",
		SessionID: "session_provenance",
		Tool:      "provenance",
		Input:     map[string]any{"entityId": "artifact_egfr"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if read.Status != "ok" || !strings.Contains(read.Output, "published") || !strings.Contains(read.Output, edge.ID) {
		t.Fatalf("read provenance = %+v", read)
	}
	edges, ok := read.Data["edges"].([]store.ProvenanceEdge)
	if !ok || len(edges) != 1 || edges[0].ID != edge.ID {
		t.Fatalf("read provenance data = %+v", read.Data)
	}
	calls, err := db.ListToolCalls(ctx, "run_provenance")
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[0].Tool != "provenance" || calls[1].Tool != "provenance" {
		t.Fatalf("provenance calls = %+v", calls)
	}
}

func TestExecutorRunsPluginToolFromUnifiedToolPath(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "superscience.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	t.Setenv("SUPERSCIENCE_TOOLS_PLUGIN_HELPER", "1")
	t.Cleanup(func() { _ = pluginruntime.CloseAll() })
	if _, err := db.UpsertPlugin(ctx, store.PluginRecord{
		ID:       "lab-plugin",
		Name:     "Lab Plugin",
		Runtime:  "subprocess",
		Command:  os.Args[0],
		Args:     []string{"-test.run=^TestToolsPluginFixtureProcess$"},
		Enabled:  true,
		Status:   "configured",
		Manifest: `{"tools":[{"name":"lab_search","inputSchema":{"type":"object"}}]}`,
	}); err != nil {
		t.Fatal(err)
	}

	definition, ok := tools.Find("plugin")
	if !ok || !definition.RequiresApproval || definition.Category != "extension" {
		t.Fatalf("plugin tool definition = %+v ok=%v", definition, ok)
	}
	result, err := (tools.Executor{Store: db}).Execute(ctx, tools.ExecuteRequest{
		Tool: "plugin",
		Input: map[string]any{
			"pluginId": "lab-plugin",
			"tool":     "lab_search",
			"input":    map[string]any{"query": "EGFR"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "ok" || !strings.Contains(result.Output, "unified plugin pong") {
		t.Fatalf("result = %+v", result)
	}
	if result.Data["result"] == nil {
		t.Fatalf("plugin result data missing: %+v", result.Data)
	}
}

func TestToolsPluginFixtureProcess(t *testing.T) {
	if os.Getenv("SUPERSCIENCE_TOOLS_PLUGIN_HELPER") != "1" {
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
				"pluginInfo":      map[string]any{"name": "tools-fixture", "version": "1.0.0"},
				"capabilities":    map[string]any{"tools": true},
			}
		case "tools/list":
			response["result"] = map[string]any{"tools": []map[string]any{{"name": "lab_search", "inputSchema": map[string]any{"type": "object"}}}}
		case "tools/call":
			response["result"] = map[string]any{"content": []map[string]any{{"type": "text", "text": "unified plugin pong"}}}
		case "shutdown":
			response["result"] = map[string]any{"ok": true}
		default:
			response["error"] = map[string]any{"code": -32601, "message": fmt.Sprintf("unknown method %s", request.Method)}
		}
		data, _ := json.Marshal(response)
		_, _ = writer.Write(append(data, '\n'))
		_ = writer.Flush()
		if request.Method == "shutdown" {
			return
		}
	}
}

func TestExecutorRunsMCPResourceAndPromptFromUnifiedToolPath(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "superscience.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	fixture := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "tools-test-mcp", Version: "1.0.0"}, nil)
	fixture.AddResource(&mcpsdk.Resource{URI: "file:///dataset.csv", Name: "Dataset", MIMEType: "text/csv"}, func(_ context.Context, request *mcpsdk.ReadResourceRequest) (*mcpsdk.ReadResourceResult, error) {
		return &mcpsdk.ReadResourceResult{Contents: []*mcpsdk.ResourceContents{{
			URI: request.Params.URI, MIMEType: "text/csv", Text: "gene,score\nEGFR,0.91",
		}}}, nil
	})
	fixture.AddPrompt(&mcpsdk.Prompt{Name: "summarize", Arguments: []*mcpsdk.PromptArgument{{Name: "topic", Required: true}}}, func(_ context.Context, request *mcpsdk.GetPromptRequest) (*mcpsdk.GetPromptResult, error) {
		return &mcpsdk.GetPromptResult{
			Description: "Summarize selected records",
			Messages: []*mcpsdk.PromptMessage{{
				Role: "user", Content: &mcpsdk.TextContent{Text: "Summarize " + request.Params.Arguments["topic"] + " evidence"},
			}},
		}, nil
	})
	fixture.AddTool(&mcpsdk.Tool{Name: "reject", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "lab rejected request"}}, IsError: true}, nil
	})
	mcpAPI := httptest.NewServer(mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return fixture }, nil))
	defer func() {
		_ = mcpclient.CloseAll()
		mcpAPI.Close()
	}()
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

	for _, id := range []string{"mcp_resource", "mcp_prompt"} {
		definition, ok := tools.Find(id)
		if !ok || !definition.RequiresApproval || definition.Category != "extension" {
			t.Fatalf("%s definition = %+v ok=%v", id, definition, ok)
		}
	}

	resource, err := (tools.Executor{Store: db}).Execute(ctx, tools.ExecuteRequest{
		Tool:  "mcp_resource",
		Input: map[string]any{"serverId": "lab-mcp", "uri": "file:///dataset.csv"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resource.Status != "ok" || !strings.Contains(resource.Output, "EGFR,0.91") {
		t.Fatalf("resource result = %+v", resource)
	}
	if resource.Data["result"] == nil {
		t.Fatalf("resource data missing: %+v", resource.Data)
	}

	prompt, err := (tools.Executor{Store: db}).Execute(ctx, tools.ExecuteRequest{
		Tool: "mcp_prompt",
		Input: map[string]any{
			"serverId":  "lab-mcp",
			"name":      "summarize",
			"arguments": map[string]any{"topic": "EGFR"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if prompt.Status != "ok" || !strings.Contains(prompt.Output, "Summarize EGFR evidence") {
		t.Fatalf("prompt result = %+v", prompt)
	}
	if prompt.Data["result"] == nil {
		t.Fatalf("prompt data missing: %+v", prompt.Data)
	}

	rejected, err := (tools.Executor{Store: db}).Execute(ctx, tools.ExecuteRequest{
		Tool: "mcp",
		Input: map[string]any{
			"serverId": "lab-mcp",
			"tool":     "reject",
			"input":    map[string]any{},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rejected.Status != "error" || !strings.Contains(rejected.Output, "lab rejected request") {
		t.Fatalf("rejected MCP result = %+v", rejected)
	}
}
