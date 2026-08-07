package tools

import "sort"

type Definition struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Category         string   `json:"category"`
	Description      string   `json:"description"`
	Risk             string   `json:"risk"`
	RequiresApproval bool     `json:"requiresApproval"`
	Enabled          bool     `json:"enabled"`
	Inputs           []string `json:"inputs"`
}

type AgentDefinition struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Domain      string   `json:"domain"`
	Description string   `json:"description"`
	Tools       []string `json:"tools"`
	ReviewMode  string   `json:"reviewMode"`
	Enabled     bool     `json:"enabled"`
}

func BuiltIns() []Definition {
	return []Definition{
		{ID: "read", Name: "Read", Category: "files", Description: "Read project-root files with traversal protection.", Risk: "low", Enabled: true, Inputs: []string{"path"}},
		{ID: "write", Name: "Write", Category: "files", Description: "Write files inside the project root after permission approval.", Risk: "high", RequiresApproval: true, Enabled: true, Inputs: []string{"path", "content"}},
		{ID: "edit", Name: "Edit", Category: "files", Description: "Apply targeted text edits to project files.", Risk: "high", RequiresApproval: true, Enabled: true, Inputs: []string{"path", "old", "new"}},
		{ID: "multiedit", Name: "MultiEdit", Category: "files", Description: "Apply multiple ordered file edits.", Risk: "high", RequiresApproval: true, Enabled: true, Inputs: []string{"edits"}},
		{ID: "apply_patch", Name: "Apply Patch", Category: "files", Description: "Apply unified patches to project files.", Risk: "high", RequiresApproval: true, Enabled: true, Inputs: []string{"patch"}},
		{ID: "ls", Name: "List", Category: "files", Description: "List files and directories in the project.", Risk: "low", Enabled: true, Inputs: []string{"path"}},
		{ID: "glob", Name: "Glob", Category: "search", Description: "Find files by glob pattern.", Risk: "low", Enabled: true, Inputs: []string{"pattern"}},
		{ID: "grep", Name: "Grep", Category: "search", Description: "Search project files for text.", Risk: "low", Enabled: true, Inputs: []string{"query"}},
		{ID: "codesearch", Name: "Code Search", Category: "search", Description: "Search code symbols and text.", Risk: "low", Enabled: true, Inputs: []string{"query"}},
		{ID: "bash", Name: "Bash", Category: "execution", Description: "Run shell commands inside the active project root.", Risk: "dangerous", RequiresApproval: true, Enabled: true, Inputs: []string{"command"}},
		{ID: "todo", Name: "Todo", Category: "agent", Description: "Track agent-visible tasks and status.", Risk: "low", Enabled: true, Inputs: []string{"items"}},
		{ID: "todoread", Name: "Todo Read", Category: "agent", Description: "Read the latest structured todo state for the active run.", Risk: "low", Enabled: true, Inputs: []string{}},
		{ID: "todowrite", Name: "Todo Write", Category: "agent", Description: "Persist structured todo items for the active run.", Risk: "low", Enabled: true, Inputs: []string{"items"}},
		{ID: "plan", Name: "Plan", Category: "agent", Description: "Create or update structured research plans.", Risk: "low", Enabled: true, Inputs: []string{"steps"}},
		{ID: "planwrite", Name: "Plan Write", Category: "agent", Description: "Persist structured plan updates into the active run timeline.", Risk: "low", Enabled: true, Inputs: []string{"content"}},
		{ID: "skill", Name: "Skill", Category: "agent", Description: "Load bundled, project, or user SKILL.md instructions into the active agent context.", Risk: "low", Enabled: true, Inputs: []string{"name"}},
		{ID: "task", Name: "Task", Category: "agent", Description: "Create a child task for delegated agent work.", Risk: "medium", Enabled: true, Inputs: []string{"content"}},
		{ID: "subagent", Name: "Subagent", Category: "agent", Description: "Run a scoped child agent task under the active session.", Risk: "medium", Enabled: true, Inputs: []string{"content", "agent"}},
		{ID: "artifact", Name: "Artifact", Category: "research", Description: "Create durable research artifacts.", Risk: "medium", Enabled: true, Inputs: []string{"type", "title", "content", "metadata"}},
		{ID: "provenance", Name: "Provenance", Category: "research", Description: "Create or read provenance graph edges between research entities.", Risk: "low", Enabled: true, Inputs: []string{"sourceId", "targetId", "relation", "entityId"}},
		{ID: "science_search", Name: "Science Search", Category: "science", Description: "Search scientific databases with normalized provenance.", Risk: "network", RequiresApproval: true, Enabled: true, Inputs: []string{"database", "query", "limit", "organism"}},
		{ID: "webfetch", Name: "Web Fetch", Category: "network", Description: "Fetch remote web content with provenance.", Risk: "network", RequiresApproval: true, Enabled: true, Inputs: []string{"url"}},
		{ID: "websearch", Name: "Web Search", Category: "network", Description: "Search the web for research context.", Risk: "network", RequiresApproval: true, Enabled: true, Inputs: []string{"query"}},
		{ID: "notebook", Name: "Notebook", Category: "compute", Description: "Read, edit, and run notebook cells.", Risk: "high", RequiresApproval: true, Enabled: true, Inputs: []string{"notebookId", "cellId"}},
		{ID: "rkernel", Name: "R Kernel", Category: "compute", Description: "Execute R cells through the notebook kernel interface when R is configured.", Risk: "high", RequiresApproval: true, Enabled: true, Inputs: []string{"source"}},
		{ID: "lsp", Name: "LSP", Category: "editor", Description: "Provide diagnostics, hover, completion, and definition hints.", Risk: "low", Enabled: true, Inputs: []string{"language", "path"}},
		{ID: "question", Name: "Question", Category: "agent", Description: "Ask the user for missing research decisions.", Risk: "low", Enabled: true, Inputs: []string{"question"}},
		{ID: "mcp", Name: "MCP", Category: "extension", Description: "Execute external MCP server tools with permission and audit.", Risk: "dangerous", RequiresApproval: true, Enabled: true, Inputs: []string{"serverId", "tool", "input"}},
		{ID: "mcp_resource", Name: "MCP Resource", Category: "extension", Description: "Read MCP server resources through the unified permission and audit path.", Risk: "dangerous", RequiresApproval: true, Enabled: true, Inputs: []string{"serverId", "uri"}},
		{ID: "mcp_prompt", Name: "MCP Prompt", Category: "extension", Description: "Load MCP server prompts through the unified permission and audit path.", Risk: "dangerous", RequiresApproval: true, Enabled: true, Inputs: []string{"serverId", "name", "arguments"}},
		{ID: "plugin", Name: "Plugin", Category: "extension", Description: "Execute subprocess plugin tools through the unified permission and audit path.", Risk: "dangerous", RequiresApproval: true, Enabled: true, Inputs: []string{"pluginId", "tool", "input"}},
		{ID: "share_publish", Name: "Share Publish", Category: "cloud", Description: "Publish artifacts to a local or self-hosted Superscience share.", Risk: "network", RequiresApproval: true, Enabled: true, Inputs: []string{"artifactIds"}},
	}
}

func BuiltInAgents() []AgentDefinition {
	common := []string{"read", "grep", "science_search", "artifact", "provenance", "todo", "todoread", "todowrite", "plan", "skill", "question"}
	return []AgentDefinition{
		{ID: "research", Name: "Research", Domain: "general", Description: "General scientific research agent with literature, file, and artifact tools.", Tools: common, ReviewMode: "optional", Enabled: true},
		{ID: "biology", Name: "Biology", Domain: "biology", Description: "Biology agent for proteins, compounds, genes, pathways, and literature.", Tools: append(copyStrings(common), "notebook", "lsp"), ReviewMode: "recommended", Enabled: true},
		{ID: "physics", Name: "Physics", Domain: "physics", Description: "Physics agent for papers, equations, plots, and reproducible notes.", Tools: append(copyStrings(common), "notebook"), ReviewMode: "optional", Enabled: true},
		{ID: "ml", Name: "ML", Domain: "machine-learning", Description: "Machine learning agent for papers, code search, notebooks, and experiments.", Tools: append(copyStrings(common), "bash", "notebook", "codesearch"), ReviewMode: "recommended", Enabled: true},
		{ID: "literature-review", Name: "Literature Review", Domain: "literature", Description: "Synthesizes literature with provenance and citation-ready artifacts.", Tools: []string{"science_search", "websearch", "artifact", "todo", "plan"}, ReviewMode: "required", Enabled: true},
		{ID: "critic", Name: "Critic", Domain: "review", Description: "Reviews findings, claims, citations, and risk before publication.", Tools: []string{"read", "science_search", "artifact"}, ReviewMode: "required", Enabled: true},
		{ID: "plan", Name: "Plan", Domain: "planning", Description: "Produces implementation or research plans without executing risky tools.", Tools: []string{"read", "grep", "plan", "todo", "question"}, ReviewMode: "none", Enabled: true},
	}
}

func Find(id string) (Definition, bool) {
	for _, tool := range BuiltIns() {
		if tool.ID == id {
			return tool, true
		}
	}
	return Definition{}, false
}

func Sorted(defs []Definition) []Definition {
	result := append([]Definition(nil), defs...)
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func copyStrings(values []string) []string {
	return append([]string(nil), values...)
}
