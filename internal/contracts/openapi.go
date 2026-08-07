package contracts

import (
	"encoding/json"
	"io"
)

func OpenAPI() map[string]any {
	paths := map[string]any{}
	for _, path := range []string{
		"/api/health",
		"/api/projects",
		"/api/projects/open",
		"/api/sessions",
		"/api/sessions/{id}/messages",
		"/api/sessions/{id}/messages/stream",
		"/api/sessions/{id}/run",
		"/api/sessions/{id}/runs",
		"/api/runs/{id}/events",
		"/api/runs/{id}/steps",
		"/api/runs/{id}/tool-calls",
		"/api/runs/{id}/abort",
		"/api/runs/{id}/retry",
		"/api/runs/{id}/compact",
		"/api/files/tree",
		"/api/files/content",
		"/api/files/search",
		"/api/settings",
		"/api/settings/credentials",
		"/api/providers",
		"/api/providers/{id}/models",
		"/api/providers/{id}/health",
		"/api/providers/models/refresh",
		"/api/agents",
		"/api/agents/{id}",
		"/api/tools",
		"/api/tools/execute",
		"/api/skills",
		"/api/skills/install",
		"/api/skills/validate",
		"/api/mcp/servers",
		"/api/mcp/servers/{id}/discovery",
		"/api/mcp/servers/{id}/oauth/start",
		"/api/mcp/servers/{id}/resources/read",
		"/api/mcp/servers/{id}/prompts/get",
		"/api/mcp/oauth/callback",
		"/api/plugins",
		"/api/plugins/{id}/tools",
		"/api/plugins/{id}/tools/{tool}/execute",
		"/api/notebooks",
		"/api/notebooks/import",
		"/api/notebooks/{id}/export",
		"/api/notebooks/{id}/cells/{cellId}/run",
		"/api/notebooks/{id}/cells/{cellId}/cancel",
		"/api/lsp/sessions",
		"/api/lsp/sessions/{id}/status",
		"/api/artifacts",
		"/api/share/publish",
		"/api/share/{id}/replay",
		"/api/provenance/{entityId}",
		"/api/science/databases",
		"/api/science/search",
		"/api/permissions",
		"/api/pty",
	} {
		paths[path] = map[string]any{
			"description": "Superscience clean-room OpenScience parity endpoint",
		}
	}
	return map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":   "Superscience API",
			"version": "0.1.0",
		},
		"paths": paths,
	}
}

func WriteOpenAPI(w io.Writer) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(OpenAPI())
}
