package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"

	"superscience/internal/agent"
	"superscience/internal/buildinfo"
	"superscience/internal/contracts"
	"superscience/internal/files"
	"superscience/internal/lsp"
	"superscience/internal/mcp"
	"superscience/internal/notebook"
	"superscience/internal/permissions"
	pluginruntime "superscience/internal/plugins"
	"superscience/internal/providers"
	"superscience/internal/science"
	"superscience/internal/secrets"
	sharepkg "superscience/internal/share"
	"superscience/internal/skills"
	"superscience/internal/store"
	"superscience/internal/tools"
)

type Options struct {
	Store           *store.Store
	ProjectRoot     string
	StaticDir       string
	Secrets         secrets.Backend
	ProviderFactory func(context.Context, agent.RunRequest) (agent.Provider, error)
}

type Server struct {
	store       *store.Store
	science     *science.Registry
	permissions *permissions.Manager
	events      *eventBroker
	ptys        *ptyRegistry
	secrets     secrets.Backend
	providers   func(context.Context, agent.RunRequest) (agent.Provider, error)

	mu            sync.RWMutex
	currentRoot   string
	currentProj   string
	currentProjID string
	staticDir     string
	lspSessions   map[string]lsp.Session
	activeRuns    map[string]context.CancelFunc
}

func New(options Options) *Server {
	secretStore := options.Secrets
	if secretStore == nil {
		secretStore = secrets.OSBackend{}
	}
	return &Server{
		store:       options.Store,
		science:     science.NewRegistry(nil),
		permissions: permissions.NewManager(),
		events:      newEventBroker(),
		ptys:        newPTYRegistry(),
		secrets:     secretStore,
		providers:   options.ProviderFactory,
		currentRoot: options.ProjectRoot,
		staticDir:   options.StaticDir,
		lspSessions: map[string]lsp.Session{},
		activeRuns:  map[string]context.CancelFunc{},
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", s.withGuard(s.handleHealth))
	mux.HandleFunc("/api/openapi.json", s.withGuard(s.handleOpenAPI))
	mux.HandleFunc("/api/events", s.withGuard(s.handleEvents))
	mux.HandleFunc("/api/projects", s.withGuard(s.handleProjects))
	mux.HandleFunc("/api/projects/open", s.withGuard(s.handleProjectOpen))
	mux.HandleFunc("/api/sessions", s.withGuard(s.handleSessions))
	mux.HandleFunc("/api/sessions/", s.withGuard(s.handleSessionSubroutes))
	mux.HandleFunc("/api/runs/", s.withGuard(s.handleRunSubroutes))
	mux.HandleFunc("/api/files/tree", s.withGuard(s.handleFileTree))
	mux.HandleFunc("/api/files/content", s.withGuard(s.handleFileContent))
	mux.HandleFunc("/api/files/search", s.withGuard(s.handleFileSearch))
	mux.HandleFunc("/api/settings", s.withGuard(s.handleSettings))
	mux.HandleFunc("/api/settings/credentials", s.withGuard(s.handleCredentials))
	mux.HandleFunc("/api/settings/credentials/test", s.withGuard(s.handleCredentialTest))
	mux.HandleFunc("/api/providers", s.withGuard(s.handleProviders))
	mux.HandleFunc("/api/providers/", s.withGuard(s.handleProviderSubroutes))
	mux.HandleFunc("/api/agents", s.withGuard(s.handleAgents))
	mux.HandleFunc("/api/agents/", s.withGuard(s.handleAgents))
	mux.HandleFunc("/api/tools", s.withGuard(s.handleTools))
	mux.HandleFunc("/api/tools/execute", s.withGuard(s.handleToolExecute))
	mux.HandleFunc("/api/skills/load", s.withGuard(s.handleSkillLoad))
	mux.HandleFunc("/api/skills/install", s.withGuard(s.handleSkillInstall))
	mux.HandleFunc("/api/skills/validate", s.withGuard(s.handleSkillValidate))
	mux.HandleFunc("/api/skills", s.withGuard(s.handleSkills))
	mux.HandleFunc("/api/skills/", s.withGuard(s.handleSkillSubroutes))
	mux.HandleFunc("/api/mcp/oauth/callback", s.withGuard(s.handleMCPOAuthCallback))
	mux.HandleFunc("/api/mcp/servers", s.withGuard(s.handleMCPServers))
	mux.HandleFunc("/api/mcp/servers/", s.withGuard(s.handleMCPServerSubroutes))
	mux.HandleFunc("/api/plugins", s.withGuard(s.handlePlugins))
	mux.HandleFunc("/api/plugins/", s.withGuard(s.handlePluginSubroutes))
	mux.HandleFunc("/api/notebooks/import", s.withGuard(s.handleNotebookImport))
	mux.HandleFunc("/api/notebooks", s.withGuard(s.handleNotebooks))
	mux.HandleFunc("/api/notebooks/", s.withGuard(s.handleNotebookSubroutes))
	mux.HandleFunc("/api/lsp/sessions", s.withGuard(s.handleLSPSessions))
	mux.HandleFunc("/api/lsp/sessions/", s.withGuard(s.handleLSPSessionSubroutes))
	mux.HandleFunc("/api/artifacts", s.withGuard(s.handleArtifacts))
	mux.HandleFunc("/api/artifacts/", s.withGuard(s.handleArtifactSubroutes))
	mux.HandleFunc("/api/share/publish", s.withGuard(s.handleSharePublish))
	mux.HandleFunc("/api/share/", s.withGuard(s.handleShareSubroutes))
	mux.HandleFunc("/api/provenance/", s.withGuard(s.handleProvenanceSubroutes))
	mux.HandleFunc("/api/science/databases", s.withGuard(s.handleScienceDatabases))
	mux.HandleFunc("/api/science/search", s.withGuard(s.handleScienceSearch))
	mux.HandleFunc("/api/permissions", s.withGuard(s.handlePermissions))
	mux.HandleFunc("/api/permissions/", s.withGuard(s.handlePermissionSubroutes))
	mux.HandleFunc("/api/pty", s.withGuard(s.handlePTY))
	mux.HandleFunc("/api/pty/", s.withGuard(s.handlePTYSubroutes))
	mux.HandleFunc("/", s.withGuard(s.handleStatic))
	return mux
}

func (s *Server) withGuard(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		requestID := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if requestID == "" {
			requestID = newRequestID()
		}
		w.Header().Set("X-Request-ID", requestID)
		if !allowedHost(r.Host) {
			writeError(w, http.StatusForbidden, "forbidden host")
			return
		}
		if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" {
			if !allowedOrigin(origin) {
				writeError(w, http.StatusForbidden, "forbidden origin")
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Request-ID")
			w.Header().Set("Vary", "Origin")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next(w, r)
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"version": buildinfo.DisplayVersion(),
		"runtime": runtime.Version(),
		"time":    time.Now().UnixMilli(),
	})
}

func (s *Server) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, contracts.OpenAPI())
}

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeError(w, http.StatusNotFound, "route not found")
		return
	}
	if s.staticDir == "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><title>Superscience</title><body style="font-family:system-ui;margin:40px"><h1>Superscience</h1><p>Frontend is not built. Run <code>pnpm --dir frontend build</code> or use <code>pnpm --dir frontend dev</code>.</p></body>`))
		return
	}
	path := filepath.Join(s.staticDir, filepath.Clean(r.URL.Path))
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		http.ServeFile(w, r, path)
		return
	}
	http.ServeFile(w, r, filepath.Join(s.staticDir, "index.html"))
}

func (s *Server) handleProjects(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		projects, err := s.store.ListProjects(r.Context())
		writeResult(w, projects, err)
	case http.MethodPost:
		var body struct {
			Path string `json:"path"`
		}
		if err := readJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		s.openProject(w, r, body.Path)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleProjectOpen(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var body struct {
		Path string `json:"path"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.openProject(w, r, body.Path)
}

func (s *Server) openProject(w http.ResponseWriter, r *http.Request, projectPath string) {
	if strings.TrimSpace(projectPath) == "" {
		projectPath = "."
	}
	abs, err := filepath.Abs(projectPath)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	project, err := s.store.OpenProject(r.Context(), abs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.mu.Lock()
	s.currentRoot = project.Path
	s.currentProj = project.Name
	s.currentProjID = project.ID
	s.mu.Unlock()
	s.events.publish("project.opened", project)
	writeJSON(w, http.StatusOK, project)
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		projectID := r.URL.Query().Get("projectId")
		if projectID == "" {
			projectID = s.currentProjectID()
		}
		sessions, err := s.store.ListSessions(r.Context(), projectID)
		writeResult(w, sessions, err)
	case http.MethodPost:
		var body struct {
			ProjectID string `json:"projectId"`
			Title     string `json:"title"`
			Agent     string `json:"agent"`
		}
		if err := readJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if body.ProjectID == "" {
			body.ProjectID = s.currentProjectID()
		}
		session, err := s.store.CreateSession(r.Context(), store.CreateSessionInput{ProjectID: body.ProjectID, Title: body.Title, Agent: body.Agent})
		writeResult(w, session, err)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleSessionSubroutes(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/sessions/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	sessionID := parts[0]
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			session, err := s.store.GetSession(r.Context(), sessionID)
			writeResult(w, session, err)
		case http.MethodPatch:
			session, err := s.store.GetSession(r.Context(), sessionID)
			if err != nil {
				writeError(w, http.StatusNotFound, err.Error())
				return
			}
			var body struct {
				Title  *string `json:"title"`
				Agent  *string `json:"agent"`
				Status *string `json:"status"`
			}
			if err := readJSON(r, &body); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			if body.Title != nil {
				session.Title = *body.Title
			}
			if body.Agent != nil {
				session.Agent = *body.Agent
			}
			if body.Status != nil {
				session.Status = *body.Status
			}
			updated, err := s.store.UpdateSession(r.Context(), session)
			writeResult(w, updated, err)
		case http.MethodDelete:
			writeResult(w, map[string]bool{"ok": true}, s.store.DeleteSession(r.Context(), sessionID))
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}
	switch parts[1] {
	case "runs":
		if len(parts) == 2 && r.Method == http.MethodGet {
			runs, err := s.store.ListAgentRuns(r.Context(), sessionID)
			writeResult(w, runs, err)
			return
		}
	case "messages":
		if len(parts) == 2 && r.Method == http.MethodGet {
			messages, err := s.store.ListMessages(r.Context(), sessionID)
			writeResult(w, messages, err)
			return
		}
		if len(parts) == 3 && parts[2] == "stream" && r.Method == http.MethodPost {
			s.handleMessageStream(w, r, sessionID)
			return
		}
	case "run":
		if len(parts) == 2 && r.Method == http.MethodPost {
			s.handleSessionRun(w, r, sessionID)
			return
		}
	case "abort":
		if r.Method == http.MethodPost {
			writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
			return
		}
	}
	writeError(w, http.StatusNotFound, "route not found")
}

func (s *Server) handleMessageStream(w http.ResponseWriter, r *http.Request, sessionID string) {
	var body struct {
		Content  string `json:"content"`
		Provider string `json:"provider"`
		Model    string `json:"model"`
		Agent    string `json:"agent"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(body.Content) == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}
	session, err := s.store.GetSession(r.Context(), sessionID)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	settings, err := s.store.ListSettings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if strings.TrimSpace(body.Provider) == "" {
		body.Provider = settings["defaultProvider"]
	}
	if strings.TrimSpace(body.Model) == "" {
		body.Model = settings["defaultModel"]
	}
	if strings.TrimSpace(body.Agent) == "" {
		body.Agent = session.Agent
	}
	agentDefinition, err := agent.ResolveDefinition(r.Context(), s.store, body.Agent)
	if err != nil {
		writeAgentError(w, err)
		return
	}
	provider, err := s.providerForRun(r.Context(), agent.RunRequest{ProviderID: body.Provider, Model: body.Model, Agent: body.Agent, Goal: body.Content})
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	user, err := s.store.AddMessage(r.Context(), store.AddMessageInput{SessionID: sessionID, Role: "user", Content: body.Content})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, _ := w.(http.Flusher)
	enc := json.NewEncoder(w)
	_ = enc.Encode(map[string]any{"type": "message", "role": user.Role, "message": user})
	if flusher != nil {
		flusher.Flush()
	}
	storedMessages, err := s.store.ListMessages(r.Context(), sessionID)
	if err != nil {
		_ = enc.Encode(map[string]any{"type": "error", "role": "assistant", "content": err.Error()})
		return
	}
	providerMessages := make([]providers.Message, 0, len(storedMessages))
	for _, message := range storedMessages {
		providerMessages = append(providerMessages, providers.Message{Role: message.Role, Content: message.Content})
	}
	assistantResult, err := provider.Stream(r.Context(), providers.ChatRequest{
		System:   agent.SystemPromptForDefinition(agentDefinition),
		Messages: providerMessages,
	}, func(event providers.StreamEvent) {
		if event.Type != "text_delta" {
			return
		}
		_ = enc.Encode(map[string]any{"type": "delta", "role": "assistant", "content": event.Delta})
		if flusher != nil {
			flusher.Flush()
		}
	})
	if err != nil {
		_ = enc.Encode(map[string]any{"type": "error", "role": "assistant", "content": err.Error()})
		return
	}
	if strings.TrimSpace(assistantResult.Text) == "" {
		_ = enc.Encode(map[string]any{"type": "error", "role": "assistant", "content": "provider completed without assistant text"})
		return
	}
	assistant, err := s.store.AddMessage(r.Context(), store.AddMessageInput{SessionID: sessionID, Role: "assistant", Content: assistantResult.Text})
	if err != nil {
		_ = enc.Encode(map[string]any{"type": "error", "role": "assistant", "content": err.Error()})
		return
	}
	_, _ = s.store.AddMessagePart(r.Context(), store.MessagePart{
		MessageID: assistant.ID,
		SessionID: sessionID,
		Type:      "provider_usage",
		Title:     strings.TrimSpace(body.Provider) + "/" + strings.TrimSpace(body.Model),
		Content:   assistantResult.FinishReason,
		Metadata: mustJSON(map[string]any{
			"provider":     strings.TrimSpace(body.Provider),
			"model":        strings.TrimSpace(body.Model),
			"usage":        assistantResult.Usage,
			"finishReason": assistantResult.FinishReason,
		}),
	})
	_ = enc.Encode(map[string]any{"type": "message", "role": assistant.Role, "message": assistant})
}

func (s *Server) handleSessionRun(w http.ResponseWriter, r *http.Request, sessionID string) {
	var body struct {
		Goal     string `json:"goal"`
		Agent    string `json:"agent"`
		Provider string `json:"provider"`
		Model    string `json:"model"`
		MaxSteps int    `json:"maxSteps"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(body.Goal) == "" {
		body.Goal = "Continue the research session."
	}
	if body.Agent == "" {
		body.Agent = "research"
	}
	runContext, cancel := context.WithCancel(r.Context())
	defer cancel()
	runtime := s.agentRuntime()
	startedRunID := ""
	runtime.OnRunStarted = func(run store.AgentRun) {
		startedRunID = run.ID
		s.registerActiveRun(run.ID, cancel)
	}
	defer func() {
		if startedRunID != "" {
			s.unregisterActiveRun(startedRunID)
		}
	}()
	result, err := runtime.Run(runContext, sessionID, agent.RunRequest{
		Goal:       body.Goal,
		Agent:      body.Agent,
		ProviderID: body.Provider,
		Model:      body.Model,
		MaxSteps:   body.MaxSteps,
	})
	writeResult(w, result, err)
}

func (s *Server) agentRuntime() agent.Runtime {
	return agent.Runtime{Store: s.store, Science: s.science, Gate: s, Secrets: s.secrets, ProjectRoot: s.currentRootPath(), ProviderFactory: s.providerForRun, SkillCatalog: s.skillCatalog}
}

func (s *Server) providerForRun(ctx context.Context, request agent.RunRequest) (agent.Provider, error) {
	if s.providers != nil {
		return s.providers(ctx, request)
	}
	providerID := strings.TrimSpace(request.ProviderID)
	if providerID == "" {
		return agent.ProviderFromEnv(), nil
	}
	if providerID == "mock" {
		return nil, errors.New("provider mock is not available in production")
	}
	adapter, err := (providers.Resolver{Store: s.store, Secrets: s.secrets, Client: http.DefaultClient}).Resolve(ctx, providerID, request.Model)
	if err != nil {
		return nil, err
	}
	return agent.AdapterProvider{
		Adapter: adapter,
		System:  "You are Superscience, a local AI workbench for scientific research. Follow the selected agent prompt, normalize provenance, and route risky actions through permission-gated tools.",
	}, nil
}

func (s *Server) handleFileTree(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	root := s.currentRootPath()
	path := r.URL.Query().Get("path")
	nodes, err := files.List(root, path)
	writeResult(w, nodes, err)
}

func (s *Server) handleFileContent(w http.ResponseWriter, r *http.Request) {
	root := s.currentRootPath()
	switch r.Method {
	case http.MethodGet:
		content, err := files.ReadText(root, r.URL.Query().Get("path"))
		writeResult(w, map[string]string{"content": content}, err)
	case http.MethodPut:
		var body struct {
			SessionID string `json:"sessionId"`
			Path      string `json:"path"`
			Content   string `json:"content"`
		}
		if err := readJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if pending, blocked := s.requirePermission(body.SessionID, "write", body.Path, "File writes require approval before execution."); blocked {
			writeJSON(w, http.StatusOK, pending)
			return
		}
		err := files.WriteText(root, body.Path, body.Content)
		writeResult(w, map[string]any{"status": "ok", "content": body.Content}, err)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleFileSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	nodes, err := files.Search(s.currentRootPath(), r.URL.Query().Get("q"), limit)
	writeResult(w, nodes, err)
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		settings, err := s.store.ListSettings(r.Context())
		writeResult(w, settings, err)
	case http.MethodPut, http.MethodPost:
		var body map[string]string
		if err := readJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := s.store.SetSettings(r.Context(), body); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		settings, err := s.store.ListSettings(r.Context())
		writeResult(w, settings, err)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleCredentials(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		credentials, err := s.store.ListCredentials(r.Context())
		writeResult(w, credentials, err)
	case http.MethodPost:
		var body struct {
			Provider string `json:"provider"`
			Label    string `json:"label"`
			Key      string `json:"key"`
			Source   string `json:"source"`
		}
		if err := readJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		redacted := redact(body.Key)
		if redacted == "" {
			redacted = "env/local"
		}
		if body.Key != "" {
			if err := s.secrets.Store(body.Provider, body.Key); err != nil {
				writeError(w, http.StatusInternalServerError, "credential storage failed: "+err.Error())
				return
			}
		}
		credential, err := s.store.UpsertCredential(r.Context(), store.Credential{
			Provider: body.Provider,
			Label:    body.Label,
			Source:   body.Source,
			Redacted: redacted,
		})
		writeResult(w, credential, err)
	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		if id == "" {
			writeError(w, http.StatusBadRequest, "id is required")
			return
		}
		for _, credential := range mustListCredentials(s.store, r) {
			if credential.ID == id {
				if credential.Source != "environment" {
					if err := s.secrets.Delete(credential.Provider); err != nil {
						writeError(w, http.StatusInternalServerError, "credential deletion failed: "+err.Error())
						return
					}
				}
				break
			}
		}
		writeResult(w, map[string]bool{"ok": true}, s.store.DeleteCredential(r.Context(), id))
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleCredentialTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var body struct {
		Provider string `json:"provider"`
		Endpoint string `json:"endpoint"`
		Key      string `json:"key"`
		Model    string `json:"model"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	providerID := strings.ToLower(strings.TrimSpace(body.Provider))
	if providerID == "" || providerID == "mock" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "status": "unconfigured", "message": "A cloud provider is required"})
		return
	}
	profile, ok := providers.BuiltInProfile(providerID)
	if !ok {
		if providerID != "openai-compatible" {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "status": "unsupported", "message": "Provider is not supported"})
			return
		}
		profile = store.ProviderProfile{ID: providerID, Kind: "openai-compatible", DisplayName: "OpenAI-compatible", Enabled: true}
	}
	if endpoint := strings.TrimSpace(body.Endpoint); endpoint != "" {
		parsed, err := url.ParseRequestURI(endpoint)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "status": "invalid_endpoint", "message": "Provider endpoint URL is invalid"})
			return
		}
		profile.BaseURL = endpoint
	}
	apiKey := strings.TrimSpace(body.Key)
	if apiKey == "" {
		apiKey = providers.APIKeyFromEnv(providerID)
	}
	if apiKey == "" {
		if loaded, err := s.secrets.Load(providerID); err == nil {
			apiKey = loaded
		}
	}
	if apiKey == "" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "status": "missing_credentials", "message": "Provider credentials are required"})
		return
	}
	model := strings.TrimSpace(body.Model)
	if model == "" {
		models := providers.Models(providerID)
		if len(models) > 0 {
			model = models[0].ID
		}
	}
	if model == "" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "status": "missing_model", "message": "A provider model is required"})
		return
	}
	adapter, err := providers.NewAdapter(providers.AdapterConfig{Profile: profile, APIKey: apiKey, Model: model, Client: http.DefaultClient})
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "status": "configuration_error", "message": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	started := time.Now()
	result, err := adapter.Stream(ctx, providers.ChatRequest{
		Messages: []providers.Message{{Role: "user", Content: "Reply with OK."}},
	}, func(providers.StreamEvent) {})
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":        false,
			"status":    "error",
			"message":   err.Error(),
			"latencyMs": time.Since(started).Milliseconds(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"status":      "ready",
		"provider":    providerID,
		"model":       model,
		"latencyMs":   time.Since(started).Milliseconds(),
		"totalTokens": result.Usage.TotalTokens,
	})
}

func (s *Server) handleProviders(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		overrides, err := s.store.ListProviders(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, providers.MergeProfiles(overrides))
	case http.MethodPost:
		var body struct {
			store.ProviderProfile
			Key string `json:"key"`
		}
		if err := readJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		profile := body.ProviderProfile
		if body.Key != "" {
			if err := s.secrets.Store(profile.ID, body.Key); err != nil {
				writeError(w, http.StatusInternalServerError, "credential storage failed: "+err.Error())
				return
			}
			profile.SecretRef = "keychain:" + profile.ID
		}
		saved, err := s.store.UpsertProvider(r.Context(), profile)
		saved.SecretRef = ""
		writeResult(w, saved, err)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleProviderSubroutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/providers/")
	parts := strings.Split(path, "/")
	if len(parts) == 2 && parts[0] == "models" && parts[1] == "refresh" && r.Method == http.MethodPost {
		var body struct {
			Endpoint string `json:"endpoint"`
		}
		if err := readJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		endpoint := strings.TrimSpace(body.Endpoint)
		if endpoint == "" {
			endpoint = strings.TrimSpace(os.Getenv("SUPERSCIENCE_MODELS_DEV_ENDPOINT"))
		}
		if endpoint == "" {
			providerCount, modelCount := providers.ModelStats()
			_ = s.store.SetSettings(r.Context(), map[string]string{
				"models.lastRefresh": fmt.Sprint(time.Now().UnixMilli()),
				"models.source":      "bundled",
			})
			writeJSON(w, http.StatusOK, map[string]any{
				"status":    "ok",
				"source":    "bundled",
				"providers": providerCount,
				"models":    modelCount,
			})
			return
		}
		models, err := providers.RefreshDefaultCatalog(r.Context(), endpoint, nil)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		if err := s.store.ReplaceModelCatalog(r.Context(), modelRecordsFromProviderModels(models)); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		_ = s.store.SetSettings(r.Context(), map[string]string{
			"models.lastRefresh": fmt.Sprint(time.Now().UnixMilli()),
			"models.source":      "models.dev",
			"models.endpoint":    endpoint,
		})
		writeJSON(w, http.StatusOK, map[string]any{
			"status":    "ok",
			"source":    "models.dev",
			"providers": countModelProviders(models),
			"models":    len(models),
		})
		return
	}
	if len(parts) == 2 && parts[1] == "models" && r.Method == http.MethodGet {
		cached, err := s.store.ListModels(r.Context(), parts[0])
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if len(cached) > 0 {
			writeJSON(w, http.StatusOK, providerModelsFromRecords(cached))
			return
		}
		writeJSON(w, http.StatusOK, providers.Models(parts[0]))
		return
	}
	if len(parts) == 2 && parts[1] == "health" && r.Method == http.MethodGet {
		providerID := parts[0]
		model := strings.TrimSpace(r.URL.Query().Get("model"))
		if model == "" {
			models, err := s.store.ListModels(r.Context(), providerID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			if len(models) > 0 {
				model = models[0].ID
			} else if bundled := providers.Models(providerID); len(bundled) > 0 {
				model = bundled[0].ID
			}
		}
		if model == "" {
			writeJSON(w, http.StatusOK, map[string]any{
				"id":      providerID,
				"ok":      false,
				"status":  "missing_model",
				"message": "A provider model is required for health verification",
			})
			return
		}
		provider, err := s.providerForRun(r.Context(), agent.RunRequest{ProviderID: providerID, Model: model})
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"id": providerID, "model": model, "ok": false, "status": "configuration_error", "message": err.Error()})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		started := time.Now()
		result, err := provider.Stream(ctx, providers.ChatRequest{
			Messages: []providers.Message{{Role: "user", Content: "Reply with OK."}},
		}, func(providers.StreamEvent) {})
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"id":        providerID,
				"model":     model,
				"ok":        false,
				"status":    "error",
				"message":   err.Error(),
				"latencyMs": time.Since(started).Milliseconds(),
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id":          providerID,
			"model":       model,
			"ok":          true,
			"configured":  true,
			"status":      "ready",
			"latencyMs":   time.Since(started).Milliseconds(),
			"totalTokens": result.Usage.TotalTokens,
		})
		return
	}
	if len(parts) == 1 && parts[0] != "" && r.Method == http.MethodDelete {
		writeResult(w, map[string]bool{"ok": true}, s.store.DeleteProvider(r.Context(), parts[0]))
		return
	}
	if len(parts) == 1 && parts[0] != "" && r.Method == http.MethodPatch {
		var body store.ProviderProfile
		if err := readJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		body.ID = parts[0]
		saved, err := s.store.UpsertProvider(r.Context(), body)
		saved.SecretRef = ""
		writeResult(w, saved, err)
		return
	}
	writeError(w, http.StatusNotFound, "route not found")
}

func modelRecordsFromProviderModels(models []providers.Model) []store.ModelRecord {
	records := make([]store.ModelRecord, 0, len(models))
	for _, model := range models {
		records = append(records, store.ModelRecord{
			ID:            model.ID,
			ProviderID:    model.ProviderID,
			DisplayName:   model.DisplayName,
			ContextWindow: model.ContextWindow,
			Capabilities:  model.Capabilities,
			Status:        model.Status,
		})
	}
	return records
}

func providerModelsFromRecords(records []store.ModelRecord) []providers.Model {
	models := make([]providers.Model, 0, len(records))
	for _, record := range records {
		models = append(models, providers.Model{
			ID:            record.ID,
			ProviderID:    record.ProviderID,
			DisplayName:   record.DisplayName,
			ContextWindow: record.ContextWindow,
			Capabilities:  record.Capabilities,
			Status:        record.Status,
		})
	}
	return models
}

func countModelProviders(models []providers.Model) int {
	seen := map[string]bool{}
	for _, model := range models {
		seen[model.ProviderID] = true
	}
	return len(seen)
}

func (s *Server) handleRunSubroutes(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/runs/"), "/")
	if len(parts) < 2 || parts[0] == "" {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	runID := parts[0]
	switch parts[1] {
	case "steps":
		if r.Method == http.MethodGet {
			steps, err := s.store.ListAgentSteps(r.Context(), runID)
			writeResult(w, steps, err)
			return
		}
	case "tool-calls":
		if r.Method == http.MethodGet {
			calls, err := s.store.ListToolCalls(r.Context(), runID)
			writeResult(w, calls, err)
			return
		}
	case "events":
		if r.Method == http.MethodGet {
			events, err := s.runEvents(r.Context(), runID)
			writeResult(w, events, err)
			return
		}
	case "abort":
		if r.Method == http.MethodPost {
			s.cancelActiveRun(runID)
			err := s.store.UpdateAgentRunStatus(r.Context(), runID, "aborted")
			writeResult(w, map[string]any{"id": runID, "status": "aborted"}, err)
			return
		}
	case "resume":
		if r.Method == http.MethodPost {
			result, err := s.resumeAgentRun(r.Context(), runID)
			writeResult(w, result, err)
			return
		}
	case "answer":
		if r.Method == http.MethodPost {
			var body struct {
				ActionID string `json:"actionId"`
				Answer   string `json:"answer"`
			}
			if err := readJSON(r, &body); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			result, err := s.answerAgentRun(r.Context(), runID, body.ActionID, body.Answer)
			writeResult(w, result, err)
			return
		}
	case "retry":
		if r.Method == http.MethodPost {
			run, err := s.store.GetAgentRun(r.Context(), runID)
			if err != nil {
				writeResult(w, run, err)
				return
			}
			var body struct {
				Reason string `json:"reason"`
			}
			_ = readJSON(r, &body)
			retry, err := s.store.CreateAgentRun(r.Context(), store.AgentRun{
				SessionID: run.SessionID,
				Agent:     run.Agent,
				Status:    "retry_scheduled",
				Goal:      run.Goal,
				Model:     run.Model,
			})
			if err == nil {
				_, _ = s.store.AddAgentStep(r.Context(), store.AgentStep{
					RunID:   retry.ID,
					Kind:    "retry",
					Status:  "scheduled",
					Title:   "Retry scheduled",
					Content: strings.TrimSpace(body.Reason),
				})
			}
			writeResult(w, retry, err)
			return
		}
	case "compact":
		if r.Method == http.MethodPost {
			result, err := s.compactRun(r.Context(), runID)
			writeResult(w, result, err)
			return
		}
	}
	writeError(w, http.StatusNotFound, "route not found")
}

func (s *Server) runEvents(ctx context.Context, runID string) ([]map[string]any, error) {
	steps, err := s.store.ListAgentSteps(ctx, runID)
	if err != nil {
		return nil, err
	}
	calls, err := s.store.ListToolCalls(ctx, runID)
	if err != nil {
		return nil, err
	}
	events := make([]map[string]any, 0, len(steps)+len(calls))
	for _, step := range steps {
		events = append(events, map[string]any{
			"id":        step.ID,
			"type":      "agent.step",
			"kind":      step.Kind,
			"status":    step.Status,
			"title":     step.Title,
			"content":   step.Content,
			"metadata":  step.Metadata,
			"createdAt": step.CreatedAt,
		})
	}
	for _, call := range calls {
		events = append(events, map[string]any{
			"id":           call.ID,
			"type":         "tool.call",
			"tool":         call.Tool,
			"status":       call.Status,
			"input":        call.Input,
			"output":       call.Output,
			"permissionId": call.PermissionID,
			"createdAt":    call.CreatedAt,
		})
	}
	sort.SliceStable(events, func(i, j int) bool {
		left, _ := events[i]["createdAt"].(int64)
		right, _ := events[j]["createdAt"].(int64)
		return left < right
	})
	return events, nil
}

func (s *Server) compactRun(ctx context.Context, runID string) (map[string]any, error) {
	run, err := s.store.GetAgentRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	events, err := s.runEvents(ctx, runID)
	if err != nil {
		return nil, err
	}
	var builder strings.Builder
	builder.WriteString("# Compacted run\n\n")
	builder.WriteString("Goal: ")
	builder.WriteString(run.Goal)
	builder.WriteString("\n\n")
	for _, event := range events {
		builder.WriteString("- ")
		builder.WriteString(fmt.Sprint(event["type"]))
		builder.WriteString(" ")
		builder.WriteString(fmt.Sprint(event["status"]))
		if title := strings.TrimSpace(fmt.Sprint(event["title"])); title != "" && title != "<nil>" {
			builder.WriteString(": ")
			builder.WriteString(title)
		}
		builder.WriteString("\n")
	}
	message, err := s.store.AddMessage(ctx, store.AddMessageInput{SessionID: run.SessionID, Role: "assistant", Content: builder.String()})
	if err != nil {
		return nil, err
	}
	artifact, err := s.store.CreateArtifact(ctx, store.Artifact{
		SessionID: run.SessionID,
		Type:      "run-summary",
		Title:     "Compacted run " + run.ID,
		Content:   builder.String(),
		Metadata:  `{"renderer":"markdown","source":"compaction"}`,
	})
	if err != nil {
		return nil, err
	}
	_, _ = s.store.AddMessagePart(ctx, store.MessagePart{
		MessageID: message.ID,
		SessionID: run.SessionID,
		Type:      "artifact",
		Title:     artifact.Title,
		Content:   artifact.ID,
		Metadata:  `{"kind":"compaction"}`,
	})
	_, _ = s.store.CreateProvenanceEdge(ctx, store.ProvenanceEdge{
		SourceID:   run.ID,
		SourceType: "agent_run",
		TargetID:   artifact.ID,
		TargetType: "artifact",
		Relation:   "compacted",
	})
	_ = s.store.UpdateAgentRunStatus(ctx, run.ID, "compacted")
	return map[string]any{"status": "compacted", "runId": run.ID, "messageId": message.ID, "artifactId": artifact.ID}, nil
}

func (s *Server) handleAgents(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/agents" {
		s.handleAgentSubroute(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		definitions, err := agent.ListDefinitions(r.Context(), s.store)
		writeResult(w, definitions, err)
	case http.MethodPost:
		var body struct {
			ID           string   `json:"id"`
			Name         string   `json:"name"`
			Domain       string   `json:"domain"`
			Description  string   `json:"description"`
			SystemPrompt string   `json:"systemPrompt"`
			Tools        []string `json:"tools"`
			ReviewMode   string   `json:"reviewMode"`
			Enabled      *bool    `json:"enabled"`
		}
		if err := readJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		enabled := true
		if body.Enabled != nil {
			enabled = *body.Enabled
		}
		created, err := agent.CreateDefinition(r.Context(), s.store, agent.Definition{
			ID: body.ID, Name: body.Name, Domain: body.Domain, Description: body.Description, SystemPrompt: body.SystemPrompt,
			Tools: body.Tools, ReviewMode: body.ReviewMode, Enabled: enabled,
		})
		if err != nil {
			writeAgentError(w, err)
			return
		}
		_, _ = s.store.CreateAuditEvent(r.Context(), store.AuditEvent{Action: "agent.create", EntityID: created.ID, EntityType: "agent"})
		writeJSON(w, http.StatusOK, created)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleAgentSubroute(w http.ResponseWriter, r *http.Request) {
	id, err := url.PathUnescape(strings.TrimPrefix(r.URL.Path, "/api/agents/"))
	if err != nil || strings.TrimSpace(id) == "" || strings.Contains(id, "/") {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		definition, err := agent.GetDefinition(r.Context(), s.store, id)
		if err != nil {
			writeAgentError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, definition)
	case http.MethodPatch:
		var patch agent.DefinitionPatch
		if err := readJSON(r, &patch); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		updated, err := agent.UpdateDefinition(r.Context(), s.store, id, patch)
		if err != nil {
			writeAgentError(w, err)
			return
		}
		_, _ = s.store.CreateAuditEvent(r.Context(), store.AuditEvent{Action: "agent.update", EntityID: updated.ID, EntityType: "agent"})
		writeJSON(w, http.StatusOK, updated)
	case http.MethodDelete:
		if err := agent.DeleteDefinition(r.Context(), s.store, id); err != nil {
			writeAgentError(w, err)
			return
		}
		_, _ = s.store.CreateAuditEvent(r.Context(), store.AuditEvent{Action: "agent.delete", EntityID: id, EntityType: "agent"})
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func writeAgentError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, agent.ErrInvalidDefinition):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, agent.ErrDefinitionConflict), errors.Is(err, agent.ErrReservedAgentID):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, "agent not found")
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

func (s *Server) handleTools(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, tools.Sorted(tools.BuiltIns()))
	case http.MethodPost, http.MethodPatch:
		writeError(w, http.StatusNotImplemented, "runtime tool registration requires a plugin or MCP server; no tool was registered")
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleToolExecute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var body tools.ExecuteRequest
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	body.ProjectRoot = s.currentRootPath()
	executor := tools.Executor{Store: s.store, Science: s.science, Gate: s, Secrets: s.secrets}
	result, err := executor.Execute(r.Context(), body)
	writeResult(w, result, err)
}

func (s *Server) handleSkills(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/skills/load" {
		s.handleSkillLoad(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		catalog, err := s.skillCatalog(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, skills.Merge(skills.Bundled(), catalog))
	case http.MethodPost:
		var body store.SkillRecord
		if err := readJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if body.Source == "" {
			body.Source = "project"
		}
		saved, err := s.store.UpsertSkill(r.Context(), body)
		writeResult(w, saved, err)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleSkillLoad(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	catalog, err := s.skillCatalog(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	loaded, err := skills.Load(body.Name, catalog)
	writeResult(w, loaded, err)
}

func (s *Server) skillCatalog(ctx context.Context) ([]store.SkillRecord, error) {
	userSkills, err := skills.LoadUser()
	if err != nil {
		return nil, err
	}
	projectSkills, err := skills.LoadProject(s.currentRootPath())
	if err != nil {
		return nil, err
	}
	persisted, err := s.store.ListSkills(ctx)
	if err != nil {
		return nil, err
	}
	installedSkills, err := skills.LoadInstalled()
	if err != nil {
		return nil, err
	}
	catalog := append(userSkills, installedSkills...)
	catalog = append(catalog, projectSkills...)
	return append(catalog, persisted...), nil
}

func (s *Server) handleSkillInstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	skill, err := s.skillFromRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	skill.Enabled = true
	saved, err := s.store.UpsertSkill(r.Context(), skill)
	if err == nil && saved.Source == "remote" {
		metadata, _ := json.Marshal(map[string]string{"path": saved.Path})
		_, _ = s.store.CreateAuditEvent(r.Context(), store.AuditEvent{Action: "skill.install.remote", EntityID: saved.Name, EntityType: "skill", Metadata: string(metadata)})
	}
	writeResult(w, saved, err)
}

func (s *Server) handleSkillValidate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	skill, err := s.skillFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"id":          skill.ID,
		"name":        skill.Name,
		"description": skill.Description,
		"path":        skill.Path,
		"source":      skill.Source,
		"entrypoints": []string{"default"},
	})
}

func (s *Server) skillFromRequest(r *http.Request) (store.SkillRecord, error) {
	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Source      string `json:"source"`
		Path        string `json:"path"`
		URL         string `json:"url"`
	}
	if err := readJSON(r, &body); err != nil {
		return store.SkillRecord{}, err
	}
	if body.Path != "" {
		path := body.Path
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			path = filepath.Join(path, "SKILL.md")
		}
		skill, err := skills.ParseFile(path)
		if err != nil {
			return store.SkillRecord{}, err
		}
		if body.Source != "" {
			skill.Source = body.Source
		}
		return skill, nil
	}
	if body.URL != "" {
		req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, body.URL, nil)
		if err != nil {
			return store.SkillRecord{}, err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return store.SkillRecord{}, err
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			return store.SkillRecord{}, fmt.Errorf("skill download failed: %s", resp.Status)
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, 1_000_000))
		if err != nil {
			return store.SkillRecord{}, err
		}
		if len(strings.TrimSpace(string(data))) == 0 {
			return store.SkillRecord{}, errors.New("remote skill is empty")
		}
		sum := sha256.Sum256([]byte(body.URL))
		cacheDir := filepath.Join(s.currentRootPath(), ".superscience", "skills-cache", hex.EncodeToString(sum[:8]))
		if err := os.MkdirAll(cacheDir, 0o755); err != nil {
			return store.SkillRecord{}, err
		}
		cachePath := filepath.Join(cacheDir, "SKILL.md")
		if err := os.WriteFile(cachePath, data, 0o644); err != nil {
			return store.SkillRecord{}, err
		}
		skill, err := skills.ParseFile(cachePath)
		if err != nil {
			return store.SkillRecord{}, err
		}
		skill.Source = "remote"
		if body.Name != "" {
			skill.Name = body.Name
			skill.ID = strings.ToLower(strings.ReplaceAll(skill.Name, " ", "-"))
		}
		if body.Description != "" {
			skill.Description = body.Description
		}
		return skill, nil
	}
	source := body.Source
	if source == "" {
		source = "user"
	}
	return store.SkillRecord{
		Name:        body.Name,
		Description: body.Description,
		Source:      source,
		Path:        body.URL,
		Enabled:     true,
	}, nil
}

func (s *Server) handleSkillSubroutes(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/skills/")
	if id == "" {
		writeError(w, http.StatusNotFound, "skill not found")
		return
	}
	switch r.Method {
	case http.MethodPatch:
		var body store.SkillRecord
		if err := readJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		body.ID = id
		saved, err := s.store.UpsertSkill(r.Context(), body)
		writeResult(w, saved, err)
	case http.MethodDelete:
		writeResult(w, map[string]bool{"ok": true}, s.store.DeleteSkill(r.Context(), id))
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

type mcpDiscoveryResponse struct {
	mcp.Discovery
	Status    string `json:"status"`
	Cached    bool   `json:"cached"`
	Error     string `json:"error,omitempty"`
	UpdatedAt int64  `json:"updatedAt,omitempty"`
}

func (s *Server) handleMCPServers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		servers, err := s.store.ListMCPServers(r.Context())
		writeResult(w, servers, err)
	case http.MethodPost:
		var body store.MCPServer
		if err := readJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		body.OAuthSecretRef = ""
		body.OAuthStatus = ""
		saved, err := s.store.UpsertMCPServer(r.Context(), body)
		if err == nil {
			_ = mcp.CloseServer(saved.ID)
		}
		writeResult(w, saved, err)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleMCPServerSubroutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/mcp/servers/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusNotFound, "mcp server not found")
		return
	}
	serverID := parts[0]
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodPatch:
			existing, err := s.store.GetMCPServer(r.Context(), serverID)
			if err != nil {
				writeResult(w, nil, err)
				return
			}
			var body store.MCPServer
			if err := readJSON(r, &body); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			body.ID = serverID
			body.OAuthSecretRef = existing.OAuthSecretRef
			body.OAuthStatus = existing.OAuthStatus
			body.CreatedAt = existing.CreatedAt
			saved, err := s.store.UpsertMCPServer(r.Context(), body)
			if err == nil {
				_ = mcp.CloseServer(saved.ID)
			}
			writeResult(w, saved, err)
		case http.MethodDelete:
			_ = mcp.CloseServer(serverID)
			writeResult(w, map[string]bool{"ok": true}, s.store.DeleteMCPServer(r.Context(), serverID))
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}
	server, err := s.store.GetMCPServer(r.Context(), serverID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if len(parts) == 3 && parts[1] == "oauth" && parts[2] == "start" && r.Method == http.MethodPost {
		s.handleMCPOAuthStart(w, r, server)
		return
	}
	if len(parts) == 3 && parts[1] == "oauth" && parts[2] == "remove" && r.Method == http.MethodPost {
		s.handleMCPOAuthRemove(w, r, server)
		return
	}
	if len(parts) == 3 && parts[1] == "oauth" && parts[2] == "status" && r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, map[string]any{"serverId": server.ID, "status": server.OAuthStatus, "configured": server.OAuthSecretRef != ""})
		return
	}
	server, err = s.authenticatedMCPServer(r.Context(), server)
	if err != nil {
		writeResult(w, nil, err)
		return
	}
	if len(parts) == 2 && parts[1] == "tools" && r.Method == http.MethodGet {
		discovery, err := mcp.DiscoverCapabilities(r.Context(), server)
		if err == nil {
			data, _ := json.Marshal(discovery)
			_, _ = s.store.UpsertMCPDiscovery(r.Context(), store.MCPDiscoveryCache{
				ServerID: server.ID,
				Payload:  string(data),
				Status:   "live",
			})
			writeJSON(w, http.StatusOK, discovery.Tools)
			return
		}
		writeStructuredError(w, http.StatusServiceUnavailable, "mcp_unavailable", err.Error(), true, map[string]any{"serverId": server.ID})
		return
	}
	if len(parts) == 2 && parts[1] == "discovery" && r.Method == http.MethodGet {
		discovery, err := mcp.DiscoverCapabilities(r.Context(), server)
		if err == nil {
			data, _ := json.Marshal(discovery)
			cache, cacheErr := s.store.UpsertMCPDiscovery(r.Context(), store.MCPDiscoveryCache{
				ServerID: server.ID,
				Payload:  string(data),
				Status:   "live",
			})
			metadata, _ := json.Marshal(map[string]int{
				"tools":     len(discovery.Tools),
				"resources": len(discovery.Resources),
				"prompts":   len(discovery.Prompts),
			})
			_, _ = s.store.CreateAuditEvent(r.Context(), store.AuditEvent{Action: "mcp.discovery.refresh", EntityID: server.ID, EntityType: "mcp_server", Metadata: string(metadata)})
			if cacheErr != nil {
				writeResult(w, discovery, cacheErr)
				return
			}
			writeJSON(w, http.StatusOK, mcpDiscoveryResponse{Discovery: discovery, Status: "ready", UpdatedAt: cache.UpdatedAt})
			return
		}
		cache, cacheErr := s.store.GetMCPDiscovery(r.Context(), server.ID)
		if cacheErr == nil {
			var cached mcp.Discovery
			if decodeErr := json.Unmarshal([]byte(cache.Payload), &cached); decodeErr != nil {
				writeResult(w, discovery, decodeErr)
				return
			}
			writeJSON(w, http.StatusOK, mcpDiscoveryResponse{Discovery: cached, Status: "degraded", Cached: true, Error: err.Error(), UpdatedAt: cache.UpdatedAt})
			return
		}
		writeResult(w, discovery, err)
		return
	}
	if len(parts) == 4 && parts[1] == "tools" && parts[3] == "execute" && r.Method == http.MethodPost {
		var body struct {
			SessionID string         `json:"sessionId"`
			Input     map[string]any `json:"input"`
		}
		if err := readJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if body.Input == nil {
			body.Input = map[string]any{}
		}
		result, err := mcp.Execute(r.Context(), server, parts[2], body.Input, body.SessionID, s)
		if err == nil && result.Status != "pending_permission" {
			metadata, _ := json.Marshal(map[string]string{"tool": parts[2], "status": result.Status})
			_, _ = s.store.CreateAuditEvent(r.Context(), store.AuditEvent{Action: "mcp.tool.execute", EntityID: server.ID, EntityType: "mcp_server", Metadata: string(metadata)})
		}
		writeResult(w, result, err)
		return
	}
	if len(parts) == 3 && parts[1] == "resources" && parts[2] == "read" && r.Method == http.MethodPost {
		var body struct {
			SessionID string `json:"sessionId"`
			URI       string `json:"uri"`
		}
		if err := readJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		result, err := mcp.ReadResource(r.Context(), server, body.URI, body.SessionID, s)
		if err == nil && result.Status != "pending_permission" {
			metadata, _ := json.Marshal(map[string]string{"uri": body.URI, "status": result.Status})
			_, _ = s.store.CreateAuditEvent(r.Context(), store.AuditEvent{Action: "mcp.resource.read", EntityID: server.ID, EntityType: "mcp_server", Metadata: string(metadata)})
		}
		writeResult(w, result, err)
		return
	}
	if len(parts) == 3 && parts[1] == "prompts" && parts[2] == "get" && r.Method == http.MethodPost {
		var body struct {
			SessionID string         `json:"sessionId"`
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := readJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		result, err := mcp.GetPrompt(r.Context(), server, body.Name, body.Arguments, body.SessionID, s)
		if err == nil && result.Status != "pending_permission" {
			metadata, _ := json.Marshal(map[string]string{"prompt": body.Name, "status": result.Status})
			_, _ = s.store.CreateAuditEvent(r.Context(), store.AuditEvent{Action: "mcp.prompt.get", EntityID: server.ID, EntityType: "mcp_server", Metadata: string(metadata)})
		}
		writeResult(w, result, err)
		return
	}
	writeError(w, http.StatusNotFound, "route not found")
}

type pluginPayload struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Version     string          `json:"version"`
	Description string          `json:"description"`
	Runtime     string          `json:"runtime"`
	Command     string          `json:"command"`
	Args        []string        `json:"args"`
	Enabled     *bool           `json:"enabled"`
	Status      string          `json:"status"`
	Manifest    json.RawMessage `json:"manifest"`
}

func (s *Server) handlePlugins(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		plugins, err := s.store.ListPlugins(r.Context())
		writeResult(w, plugins, err)
	case http.MethodPost:
		payload, err := readPluginPayload(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		plugin := pluginFromPayload(payload, store.PluginRecord{Enabled: true})
		saved, err := s.store.UpsertPlugin(r.Context(), plugin)
		if err == nil {
			_ = pluginruntime.Close(saved.ID)
			_, _ = s.store.CreateAuditEvent(r.Context(), store.AuditEvent{Action: "plugin.upsert", EntityID: saved.ID, EntityType: "plugin"})
		}
		writeResult(w, saved, err)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handlePluginSubroutes(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/plugins/"), "/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusNotFound, "plugin not found")
		return
	}
	id := parts[0]
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			plugin, err := s.store.GetPlugin(r.Context(), id)
			writeResult(w, plugin, err)
		case http.MethodPatch:
			existing, err := s.store.GetPlugin(r.Context(), id)
			if err != nil {
				writeResult(w, existing, err)
				return
			}
			payload, err := readPluginPayload(r)
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			payload.ID = id
			saved, err := s.store.UpsertPlugin(r.Context(), pluginFromPayload(payload, existing))
			if err == nil {
				_ = pluginruntime.Close(saved.ID)
				_, _ = s.store.CreateAuditEvent(r.Context(), store.AuditEvent{Action: "plugin.patch", EntityID: saved.ID, EntityType: "plugin"})
			}
			writeResult(w, saved, err)
		case http.MethodDelete:
			_ = pluginruntime.Close(id)
			err := s.store.DeletePlugin(r.Context(), id)
			if err == nil {
				_, _ = s.store.CreateAuditEvent(r.Context(), store.AuditEvent{Action: "plugin.delete", EntityID: id, EntityType: "plugin"})
			}
			writeResult(w, map[string]bool{"ok": true}, err)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}
	plugin, err := s.store.GetPlugin(r.Context(), id)
	if err != nil {
		writeResult(w, plugin, err)
		return
	}
	if len(parts) == 2 && parts[1] == "tools" && r.Method == http.MethodGet {
		discovery, err := pluginruntime.Discover(r.Context(), plugin)
		writeResult(w, discovery.Tools, err)
		return
	}
	if len(parts) == 2 && parts[1] == "discovery" && r.Method == http.MethodGet {
		discovery, err := pluginruntime.Discover(r.Context(), plugin)
		writeResult(w, discovery, err)
		return
	}
	if len(parts) == 4 && parts[1] == "tools" && parts[3] == "execute" && r.Method == http.MethodPost {
		var body struct {
			SessionID string         `json:"sessionId"`
			Input     map[string]any `json:"input"`
		}
		if err := readJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if body.Input == nil {
			body.Input = map[string]any{}
		}
		result, err := pluginruntime.Execute(r.Context(), plugin, parts[2], body.Input, body.SessionID, s)
		if err == nil && result.Status != "pending_permission" {
			metadata, _ := json.Marshal(map[string]any{"tool": parts[2], "status": result.Status})
			_, _ = s.store.CreateAuditEvent(r.Context(), store.AuditEvent{Action: "plugin.tool.execute", EntityID: plugin.ID, EntityType: "plugin", Metadata: string(metadata)})
		}
		writeResult(w, result, err)
		return
	}
	writeError(w, http.StatusNotFound, "route not found")
}

func readPluginPayload(r *http.Request) (pluginPayload, error) {
	var payload pluginPayload
	err := readJSON(r, &payload)
	return payload, err
}

func pluginFromPayload(payload pluginPayload, base store.PluginRecord) store.PluginRecord {
	plugin := base
	if payload.ID != "" {
		plugin.ID = payload.ID
	}
	if payload.Name != "" {
		plugin.Name = payload.Name
	}
	if payload.Version != "" {
		plugin.Version = payload.Version
	}
	if payload.Description != "" {
		plugin.Description = payload.Description
	}
	if payload.Runtime != "" {
		plugin.Runtime = payload.Runtime
	}
	if payload.Command != "" {
		plugin.Command = payload.Command
	}
	if payload.Args != nil {
		plugin.Args = append([]string(nil), payload.Args...)
	}
	if payload.Enabled != nil {
		plugin.Enabled = *payload.Enabled
	}
	if payload.Status != "" {
		plugin.Status = payload.Status
	}
	if len(payload.Manifest) > 0 {
		plugin.Manifest = string(payload.Manifest)
	}
	return plugin
}

func (s *Server) handleNotebooks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		notebooks, err := s.store.ListNotebooks(r.Context())
		writeResult(w, notebooks, err)
	case http.MethodPost:
		var body store.Notebook
		if err := readJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if body.ProjectID == "" {
			body.ProjectID = s.currentProjectID()
		}
		created, err := s.store.CreateNotebook(r.Context(), body)
		writeResult(w, created, err)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleNotebookImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var body struct {
		ProjectID string         `json:"projectId"`
		Path      string         `json:"path"`
		Title     string         `json:"title"`
		Notebook  map[string]any `json:"notebook"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.ProjectID == "" {
		body.ProjectID = s.currentProjectID()
	}
	notebookRecord := store.Notebook{
		ProjectID: body.ProjectID,
		Path:      body.Path,
		Title:     body.Title,
	}
	if notebookRecord.Title == "" {
		if metadata, ok := body.Notebook["metadata"].(map[string]any); ok {
			notebookRecord.Title = strings.TrimSpace(fmt.Sprint(metadata["name"]))
		}
	}
	if notebookRecord.Title == "" {
		notebookRecord.Title = "Imported notebook"
	}
	if notebookRecord.Path == "" {
		notebookRecord.Path = "imported.ipynb"
	}
	for _, rawCell := range anySlice(body.Notebook["cells"]) {
		cellMap, ok := rawCell.(map[string]any)
		if !ok {
			continue
		}
		kind := strings.TrimSpace(fmt.Sprint(cellMap["cell_type"]))
		if kind == "" {
			kind = strings.TrimSpace(fmt.Sprint(cellMap["kind"]))
		}
		if kind == "" {
			kind = "code"
		}
		language := "python"
		if metadata, ok := cellMap["metadata"].(map[string]any); ok {
			if value := strings.TrimSpace(fmt.Sprint(metadata["language"])); value != "" && value != "<nil>" {
				language = value
			}
		}
		outputJSON := "[]"
		if outputs, ok := cellMap["outputs"]; ok {
			if encoded, err := json.Marshal(outputs); err == nil {
				outputJSON = string(encoded)
			}
		}
		notebookRecord.Cells = append(notebookRecord.Cells, store.NotebookCell{
			Kind:       kind,
			Language:   language,
			Source:     notebookSource(cellMap["source"]),
			OutputJSON: outputJSON,
			Status:     "idle",
		})
	}
	if len(notebookRecord.Cells) == 0 {
		notebookRecord.Cells = append(notebookRecord.Cells, store.NotebookCell{Kind: "code", Language: "python", Status: "idle"})
	}
	created, err := s.store.CreateNotebook(r.Context(), notebookRecord)
	writeResult(w, created, err)
}

func (s *Server) handleNotebookSubroutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/notebooks/")
	parts := strings.Split(path, "/")
	if len(parts) == 1 && parts[0] != "" && r.Method == http.MethodGet {
		notebook, err := s.store.GetNotebook(r.Context(), parts[0])
		writeResult(w, notebook, err)
		return
	}
	if len(parts) == 2 && parts[1] == "export" && r.Method == http.MethodGet {
		notebook, err := s.store.GetNotebook(r.Context(), parts[0])
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, notebookExport(notebook))
		return
	}
	if len(parts) == 3 && parts[1] == "kernel" && parts[2] == "status" && r.Method == http.MethodGet {
		notebookRecord, err := s.store.GetNotebook(r.Context(), parts[0])
		if err != nil {
			writeResult(w, notebook.KernelStatus{}, err)
			return
		}
		writeJSON(w, http.StatusOK, notebook.Status(parts[0], notebookLanguage(notebookRecord)))
		return
	}
	if len(parts) == 3 && parts[1] == "kernel" && parts[2] == "restart" && r.Method == http.MethodPost {
		notebookRecord, err := s.store.GetNotebook(r.Context(), parts[0])
		if err != nil {
			writeResult(w, notebook.KernelStatus{}, err)
			return
		}
		language := notebookLanguage(notebookRecord)
		if err := notebook.Restart(parts[0], language); err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, notebook.Status(parts[0], language))
		return
	}
	if len(parts) == 4 && parts[1] == "cells" && parts[3] == "cancel" && r.Method == http.MethodPost {
		cell, err := s.store.GetNotebookCell(r.Context(), parts[0], parts[2])
		if err != nil {
			writeResult(w, cell, err)
			return
		}
		if err := notebook.Interrupt(parts[0], cell.Language); err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		cell, err = s.store.UpdateNotebookCellStatus(r.Context(), parts[0], parts[2], "cancelled")
		if err != nil {
			writeResult(w, cell, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "cancelled", "cellId": cell.ID, "notebookId": cell.NotebookID})
		return
	}
	if len(parts) == 4 && parts[1] == "cells" && parts[3] == "run" && r.Method == http.MethodPost {
		var body struct {
			SessionID string `json:"sessionId"`
		}
		if err := readJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if pending, blocked := s.requirePermission(body.SessionID, "notebook", parts[0]+":"+parts[2], "Notebook execution requires approval before running code."); blocked {
			writeJSON(w, http.StatusOK, pending)
			return
		}
		cell, err := s.store.GetNotebookCell(r.Context(), parts[0], parts[2])
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		result, err := notebook.RunCell(r.Context(), cell)
		if err == nil {
			cell.Output = result.Stdout + result.Stderr
			outputs, _ := json.Marshal(result.Outputs)
			cell.OutputJSON = string(outputs)
			cell.Status = result.Status
			_, _ = s.store.UpdateNotebookCellOutput(r.Context(), cell)
			artifact, artifactErr := s.store.CreateArtifact(r.Context(), store.Artifact{
				SessionID: body.SessionID,
				Type:      "notebook-output",
				Title:     "Notebook cell output",
				Content:   mustJSON(result),
				Metadata:  mustJSON(map[string]string{"renderer": "notebook", "kernel": result.Kernel, "cellId": cell.ID}),
			})
			if artifactErr == nil {
				_, _ = s.store.CreateProvenanceEdge(r.Context(), store.ProvenanceEdge{
					SourceID:   cell.ID,
					SourceType: "notebook_cell",
					TargetID:   artifact.ID,
					TargetType: "artifact",
					Relation:   "produced",
				})
			}
		}
		writeResult(w, result, err)
		return
	}
	writeError(w, http.StatusNotFound, "route not found")
}

func (s *Server) handleLSPSessions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.mu.RLock()
		sessions := make([]lsp.Session, 0, len(s.lspSessions))
		for _, session := range s.lspSessions {
			sessions = append(sessions, session)
		}
		s.mu.RUnlock()
		writeJSON(w, http.StatusOK, sessions)
	case http.MethodPost:
		var body struct {
			Language string   `json:"language"`
			Path     string   `json:"path"`
			Text     string   `json:"text"`
			Command  string   `json:"command"`
			Args     []string `json:"args"`
			Env      []string `json:"env"`
		}
		if err := readJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		var session lsp.Session
		if strings.TrimSpace(body.Command) != "" {
			manager := lsp.NewManager()
			manager.RegisterServer(body.Language, lsp.ServerConfig{Command: body.Command, Args: body.Args, Env: body.Env})
			analyzed, err := manager.Analyze(r.Context(), lsp.Request{Language: body.Language, Path: body.Path, Text: body.Text})
			if err != nil {
				writeError(w, http.StatusBadGateway, err.Error())
				return
			}
			session = analyzed
		} else {
			analyzed, err := lsp.Analyze(r.Context(), body.Language, body.Path, body.Text)
			if err != nil {
				writeError(w, http.StatusServiceUnavailable, err.Error())
				return
			}
			session = analyzed
		}
		s.mu.Lock()
		s.lspSessions[session.ID] = session
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, session)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleLSPSessionSubroutes(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/lsp/sessions/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] != "status" || r.Method != http.MethodGet {
		writeError(w, http.StatusNotFound, "route not found")
		return
	}
	s.mu.RLock()
	session, ok := s.lspSessions[parts[0]]
	s.mu.RUnlock()
	if !ok {
		writeError(w, http.StatusNotFound, "lsp session not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":           session.ID,
		"language":     session.Language,
		"path":         session.Path,
		"status":       "ready",
		"diagnostics":  len(session.Diagnostics),
		"capabilities": session.Capabilities,
		"createdAt":    session.CreatedAt,
	})
}

func (s *Server) handleArtifacts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		sessionID := strings.TrimSpace(r.URL.Query().Get("sessionId"))
		var artifacts []store.Artifact
		var err error
		if sessionID == "" {
			artifacts, err = s.store.ListArtifacts(r.Context())
		} else {
			artifacts, err = s.store.ListArtifactsBySession(r.Context(), sessionID)
		}
		writeResult(w, artifacts, err)
	case http.MethodPost:
		var body store.Artifact
		if err := readJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		artifact, err := s.store.CreateArtifact(r.Context(), body)
		if err == nil {
			_, _ = s.store.CreateProvenanceEdge(r.Context(), store.ProvenanceEdge{
				SourceID:   "local",
				SourceType: "user",
				TargetID:   artifact.ID,
				TargetType: "artifact",
				Relation:   "created",
			})
		}
		writeResult(w, artifact, err)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleArtifactSubroutes(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/artifacts/")
	switch r.Method {
	case http.MethodGet:
		artifact, err := s.store.GetArtifact(r.Context(), id)
		writeResult(w, artifact, err)
	case http.MethodDelete:
		writeResult(w, map[string]bool{"ok": true}, s.store.DeleteArtifact(r.Context(), id))
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleSharePublish(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var request sharepkg.PublishRequest
	if err := readJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if pending, blocked := s.requirePermission(request.SessionID, "share", strings.Join(request.ArtifactIDs, ","), "Share publishing requires approval before exposing artifacts outside the workspace."); blocked {
		writeJSON(w, http.StatusOK, pending)
		return
	}
	artifacts := make([]store.Artifact, 0, len(request.ArtifactIDs))
	for _, id := range request.ArtifactIDs {
		artifact, err := s.store.GetArtifact(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusBadRequest, "artifact not found: "+id)
			return
		}
		artifacts = append(artifacts, artifact)
	}
	bundle, err := sharepkg.BuildBundle(request, artifacts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	record := store.Share{
		Title:      request.Title,
		Visibility: request.Visibility,
		Status:     "published",
		Bundle:     bundle,
	}
	if record.Title == "" {
		record.Title = "Superscience share"
	}
	if record.Visibility == "" {
		record.Visibility = "private-link"
	}
	record.ID = fmt.Sprintf("share_%d", time.Now().UnixNano())
	record.URL = "/api/share/" + record.ID
	record, err = s.store.CreateShare(r.Context(), record)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, artifact := range artifacts {
		_, _ = s.store.CreateProvenanceEdge(r.Context(), store.ProvenanceEdge{
			SourceID:   artifact.ID,
			SourceType: "artifact",
			TargetID:   record.ID,
			TargetType: "share",
			Relation:   "published",
		})
	}
	_, _ = s.store.CreateAuditEvent(r.Context(), store.AuditEvent{Action: "share.publish", EntityID: record.ID, EntityType: "share"})
	writeJSON(w, http.StatusOK, record)
}

func (s *Server) handleShareSubroutes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/share/"), "/")
	id := parts[0]
	share, err := s.store.GetShare(r.Context(), id)
	if err != nil {
		writeResult(w, share, err)
		return
	}
	if len(parts) == 2 && parts[1] == "replay" {
		var bundle sharepkg.Bundle
		_ = json.Unmarshal([]byte(share.Bundle), &bundle)
		provenance, provErr := s.store.ListProvenance(r.Context(), share.ID)
		if provErr != nil {
			writeError(w, http.StatusInternalServerError, provErr.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"share":      share,
			"artifacts":  bundle.Artifacts,
			"provenance": provenance,
		})
		return
	}
	writeJSON(w, http.StatusOK, share)
}

func (s *Server) handleProvenanceSubroutes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/provenance/")
	edges, err := s.store.ListProvenance(r.Context(), id)
	writeResult(w, edges, err)
}

func (s *Server) handleScienceDatabases(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, s.science.Databases())
}

func (s *Server) handleScienceSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var body struct {
		SessionID string `json:"sessionId"`
		Database  string `json:"database"`
		Query     string `json:"query"`
		Limit     int    `json:"limit"`
		Organism  string `json:"organism,omitempty"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	pattern := strings.TrimSpace(body.Database) + ":" + strings.TrimSpace(body.Query)
	if pending, blocked := s.requirePermission(body.SessionID, "network", pattern, "Scientific database search requires approval before sending the query to an external service."); blocked {
		writeJSON(w, http.StatusOK, pending)
		return
	}
	hits, err := s.science.Search(r.Context(), science.SearchRequest{Database: body.Database, Query: body.Query, Limit: body.Limit, Organism: body.Organism})
	writeResult(w, hits, err)
}

func (s *Server) handlePermissions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	permissions, err := s.store.ListPendingPermissions(r.Context())
	writeResult(w, permissions, err)
}

func (s *Server) handlePermissionSubroutes(w http.ResponseWriter, r *http.Request) {
	if !strings.HasSuffix(r.URL.Path, "/respond") || r.Method != http.MethodPost {
		writeError(w, http.StatusNotFound, "route not found")
		return
	}
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/permissions/"), "/respond")
	var body struct {
		Decision permissions.Decision `json:"decision"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Decision != permissions.DecisionAllowOnce && body.Decision != permissions.DecisionAllowAlways && body.Decision != permissions.DecisionReject {
		writeError(w, http.StatusBadRequest, "unknown permission decision")
		return
	}
	if _, err := s.store.GetPermission(r.Context(), id); err != nil {
		writeResult(w, map[string]bool{"ok": false}, err)
		return
	}
	managerErr := s.permissions.Respond(id, body.Decision)
	if managerErr != nil && !strings.Contains(managerErr.Error(), "not found") {
		writeResult(w, map[string]bool{"ok": false}, managerErr)
		return
	}
	if err := s.store.UpdatePermissionStatus(r.Context(), id, string(body.Decision)); err != nil {
		writeResult(w, map[string]bool{"ok": false}, err)
		return
	}
	checkpoint, err := s.store.GetRunCheckpointByPermission(r.Context(), id)
	if err == nil {
		result, resumeErr := s.resumeAgentRun(r.Context(), checkpoint.RunID)
		if resumeErr != nil {
			writeResult(w, map[string]bool{"ok": false}, resumeErr)
			return
		}
		s.events.publish("agent.run.resumed", result)
	} else if !errors.Is(err, sql.ErrNoRows) {
		writeResult(w, map[string]bool{"ok": false}, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	ch, cancel := s.events.subscribe()
	defer cancel()
	fmt.Fprintf(w, "event: ready\ndata: {\"ok\":true}\n\n")
	flusher, _ := w.(http.Flusher)
	if flusher != nil {
		flusher.Flush()
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case event := <-ch:
			payload, _ := json.Marshal(event.Data)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, payload)
			if flusher != nil {
				flusher.Flush()
			}
		}
	}
}

func (s *Server) handlePTY(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.ptys.list())
	case http.MethodPost:
		var body struct {
			SessionID string `json:"sessionId"`
			Command   string `json:"command"`
			CWD       string `json:"cwd"`
		}
		_ = readJSON(r, &body)
		if body.CWD == "" {
			body.CWD = s.currentRootPath()
		}
		pattern := body.Command
		if pattern == "" || pattern == "shell" {
			pattern = "shell"
		}
		if pending, blocked := s.requirePermission(body.SessionID, "shell", pattern, "Terminal shell sessions require approval before execution."); blocked {
			writeJSON(w, http.StatusOK, pending)
			return
		}
		info, err := s.ptys.create(body.Command, body.CWD)
		writeResult(w, info, err)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handlePTYSubroutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/pty/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusNotFound, "pty not found")
		return
	}
	id := parts[0]
	if len(parts) == 1 && r.Method == http.MethodDelete {
		s.ptys.remove(id)
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	if len(parts) == 2 && parts[1] == "ws" && r.Method == http.MethodGet {
		s.ptys.connect(w, r, id)
		return
	}
	writeError(w, http.StatusNotFound, "route not found")
}

func (s *Server) currentRootPath() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.currentRoot != "" {
		return s.currentRoot
	}
	wd, _ := os.Getwd()
	return wd
}

func (s *Server) currentProjectID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentProjID
}

func (s *Server) Ask(sessionID, typ, pattern, message string) string {
	request := s.permissions.Ask(permissions.Request{
		SessionID: sessionID,
		Type:      typ,
		Pattern:   pattern,
		Message:   message,
	})
	if s.store != nil {
		if _, err := s.store.CreatePermission(context.Background(), store.PermissionRecord{
			ID:        request.ID,
			SessionID: request.SessionID,
			Type:      request.Type,
			Pattern:   request.Pattern,
			Message:   request.Message,
			Status:    request.Status,
		}); err != nil {
			s.events.publish("permission.persistence_failed", map[string]string{"id": request.ID, "error": err.Error()})
		}
	}
	s.events.publish("permission.created", request)
	return request.ID
}

func (s *Server) registerActiveRun(runID string, cancel context.CancelFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activeRuns[runID] = cancel
}

func (s *Server) unregisterActiveRun(runID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.activeRuns, runID)
}

func (s *Server) cancelActiveRun(runID string) bool {
	s.mu.RLock()
	cancel := s.activeRuns[runID]
	s.mu.RUnlock()
	if cancel == nil {
		return false
	}
	cancel()
	return true
}

func (s *Server) resumeAgentRun(ctx context.Context, runID string) (agent.RunResult, error) {
	runContext, cancel := context.WithCancel(ctx)
	s.registerActiveRun(runID, cancel)
	defer func() {
		cancel()
		s.unregisterActiveRun(runID)
	}()
	return s.agentRuntime().Resume(runContext, runID)
}

func (s *Server) answerAgentRun(ctx context.Context, runID, actionID, answer string) (agent.RunResult, error) {
	runContext, cancel := context.WithCancel(ctx)
	s.registerActiveRun(runID, cancel)
	defer func() {
		cancel()
		s.unregisterActiveRun(runID)
	}()
	return s.agentRuntime().Answer(runContext, runID, actionID, answer)
}

func (s *Server) IsAllowed(sessionID, typ, pattern string) bool {
	if s.store == nil {
		return false
	}
	decision, allowed, err := s.store.ConsumePermissionDecision(context.Background(), sessionID, typ, pattern)
	if err != nil {
		s.events.publish("permission.lookup_failed", map[string]string{"sessionId": sessionID, "type": typ, "pattern": pattern, "error": err.Error()})
		return false
	}
	return allowed && (decision == string(permissions.DecisionAllowOnce) || decision == string(permissions.DecisionAllowAlways))
}

func (s *Server) requirePermission(sessionID, typ, pattern, message string) (map[string]any, bool) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, false
	}
	if s.IsAllowed(sessionID, typ, pattern) {
		return nil, false
	}
	id := s.Ask(sessionID, typ, pattern, message)
	return map[string]any{
		"status":       "pending_permission",
		"permissionId": id,
		"type":         typ,
		"pattern":      pattern,
	}, true
}

func notebookExport(notebook store.Notebook) map[string]any {
	cells := make([]map[string]any, 0, len(notebook.Cells))
	for _, cell := range notebook.Cells {
		cellType := "code"
		if cell.Kind == "markdown" {
			cellType = "markdown"
		}
		exported := map[string]any{
			"cell_type": cellType,
			"metadata":  map[string]any{"id": cell.ID, "language": cell.Language, "status": cell.Status},
			"source":    cell.Source,
		}
		if cellType == "code" {
			outputs := []map[string]any{}
			if strings.TrimSpace(cell.OutputJSON) != "" {
				_ = json.Unmarshal([]byte(cell.OutputJSON), &outputs)
			}
			if len(outputs) == 0 && cell.Output != "" {
				outputs = append(outputs, map[string]any{
					"output_type": "stream",
					"name":        "stdout",
					"text":        cell.Output,
				})
			}
			exported["execution_count"] = nil
			exported["outputs"] = outputs
		}
		cells = append(cells, exported)
	}
	kernelName, kernelLanguage := notebookKernelMetadata(notebook)
	return map[string]any{
		"nbformat":       4,
		"nbformat_minor": 5,
		"metadata": map[string]any{
			"name": notebook.Title,
			"path": notebook.Path,
			"kernelspec": map[string]string{
				"name":     kernelName,
				"language": kernelLanguage,
			},
			"language_info": map[string]string{"name": kernelLanguage},
		},
		"cells": cells,
	}
}

func notebookKernelMetadata(notebook store.Notebook) (string, string) {
	for _, cell := range notebook.Cells {
		if cell.Kind != "code" {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(cell.Language)) {
		case "r":
			return "ir", "R"
		case "julia":
			return "julia", "julia"
		default:
			return "python3", "python"
		}
	}
	return "python3", "python"
}

func notebookLanguage(notebook store.Notebook) string {
	for _, cell := range notebook.Cells {
		if cell.Kind == "code" && strings.TrimSpace(cell.Language) != "" {
			return cell.Language
		}
	}
	return "python"
}

func anySlice(value any) []any {
	switch typed := value.(type) {
	case []any:
		return typed
	case nil:
		return nil
	default:
		return []any{typed}
	}
}

func notebookSource(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []any:
		var builder strings.Builder
		for _, part := range typed {
			builder.WriteString(fmt.Sprint(part))
		}
		return builder.String()
	case []string:
		return strings.Join(typed, "")
	case nil:
		return ""
	default:
		return fmt.Sprint(typed)
	}
}

func readJSON(r *http.Request, out any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(out)
}

func writeResult(w http.ResponseWriter, value any, err error) {
	if err != nil {
		var providerErr providers.ProviderError
		if errors.As(err, &providerErr) {
			status := providerErr.HTTPStatus
			if status == 0 {
				status = http.StatusBadGateway
			}
			code := providerErr.Code
			if code == "" {
				code = "provider_error"
			}
			details := map[string]any{"provider": providerErr.Provider, "providerStatus": providerErr.Status}
			for key, value := range providerErr.Details {
				details[key] = value
			}
			writeStructuredError(w, status, code, providerErr.Message, providerErr.Retryable, details)
			return
		}
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
		}
		if errors.Is(err, files.ErrOutsideRoot) {
			status = http.StatusForbidden
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeStructuredError(w, status, errorCodeForStatus(status), message, status == http.StatusRequestTimeout || status == http.StatusTooManyRequests || status >= 500, map[string]any{})
}

func writeStructuredError(w http.ResponseWriter, status int, code, message string, retryable bool, details map[string]any) {
	writeJSON(w, status, map[string]any{
		"code":      code,
		"message":   message,
		"retryable": retryable,
		"details":   details,
		"requestId": w.Header().Get("X-Request-ID"),
	})
}

func errorCodeForStatus(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "bad_request"
	case http.StatusUnauthorized:
		return "unauthorized"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusMethodNotAllowed:
		return "method_not_allowed"
	case http.StatusConflict:
		return "conflict"
	case http.StatusTooManyRequests:
		return "rate_limited"
	default:
		if status >= 500 {
			return "internal_error"
		}
		return "request_failed"
	}
}

func newRequestID() string {
	data := make([]byte, 12)
	if _, err := rand.Read(data); err == nil {
		return "req_" + hex.EncodeToString(data)
	}
	return fmt.Sprintf("req_%d", time.Now().UnixNano())
}

func allowedHost(hostport string) bool {
	if hostport == "" {
		return true
	}
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		host = hostport
	}
	host = strings.Trim(host, "[]")
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func allowedOrigin(origin string) bool {
	parsed, err := url.Parse(origin)
	if err != nil || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	scheme := strings.ToLower(parsed.Scheme)
	if scheme == "wails" && host == "wails" {
		return true
	}
	if scheme == "http" && host == "wails.localhost" && parsed.Port() == "" {
		return true
	}
	if scheme != "http" && scheme != "https" {
		return false
	}
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func redact(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "..." + key[len(key)-4:]
}

func mustJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func mapBool(condition bool, yes string, no string) string {
	if condition {
		return yes
	}
	return no
}

func mustListCredentials(db *store.Store, r *http.Request) []store.Credential {
	credentials, err := db.ListCredentials(r.Context())
	if err != nil {
		return nil
	}
	return credentials
}

type event struct {
	Type string
	Data any
}

type eventBroker struct {
	mu          sync.Mutex
	subscribers map[chan event]struct{}
}

func newEventBroker() *eventBroker {
	return &eventBroker{subscribers: map[chan event]struct{}{}}
}

func (b *eventBroker) subscribe() (<-chan event, func()) {
	ch := make(chan event, 16)
	b.mu.Lock()
	b.subscribers[ch] = struct{}{}
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		delete(b.subscribers, ch)
		close(ch)
		b.mu.Unlock()
	}
}

func (b *eventBroker) publish(typ string, data any) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subscribers {
		select {
		case ch <- event{Type: typ, Data: data}:
		default:
		}
	}
}

type ptyInfo struct {
	ID      string `json:"id"`
	Command string `json:"command"`
	CWD     string `json:"cwd"`
	Created int64  `json:"created"`
}

type ptyRegistry struct {
	mu    sync.Mutex
	items map[string]*ptySession
}

func newPTYRegistry() *ptyRegistry {
	return &ptyRegistry{items: map[string]*ptySession{}}
}

type ptySession struct {
	info ptyInfo
	cmd  *exec.Cmd
	file *os.File
}

func (p *ptyRegistry) create(command, cwd string) (ptyInfo, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	info := ptyInfo{ID: fmt.Sprintf("pty_%d", time.Now().UnixNano()), Command: command, CWD: cwd, Created: time.Now().UnixMilli()}
	if info.Command == "" || info.Command == "shell" {
		info.Command = os.Getenv("SHELL")
		if info.Command == "" {
			info.Command = "/bin/sh"
		}
	}
	cmd := exec.Command(info.Command)
	cmd.Dir = cwd
	file, err := pty.Start(cmd)
	if err != nil {
		return ptyInfo{}, err
	}
	p.items[info.ID] = &ptySession{info: info, cmd: cmd, file: file}
	return info, nil
}

func (p *ptyRegistry) list() []ptyInfo {
	p.mu.Lock()
	defer p.mu.Unlock()
	result := make([]ptyInfo, 0, len(p.items))
	for _, item := range p.items {
		result = append(result, item.info)
	}
	return result
}

func (p *ptyRegistry) remove(id string) {
	p.mu.Lock()
	session := p.items[id]
	delete(p.items, id)
	p.mu.Unlock()
	if session != nil {
		_ = session.file.Close()
		_ = session.cmd.Process.Kill()
	}
}

func (p *ptyRegistry) connect(w http.ResponseWriter, r *http.Request, id string) {
	p.mu.Lock()
	session, ok := p.items[id]
	p.mu.Unlock()
	if !ok {
		writeError(w, http.StatusNotFound, "pty not found")
		return
	}
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 4096)
		for {
			n, err := session.file.Read(buf)
			if err != nil {
				return
			}
			if n > 0 {
				if err := conn.WriteMessage(websocket.TextMessage, buf[:n]); err != nil {
					return
				}
			}
		}
	}()
	for {
		select {
		case <-done:
			return
		default:
		}
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var clientMessage struct {
			Type string `json:"type"`
			Data string `json:"data"`
			Cols uint16 `json:"cols"`
			Rows uint16 `json:"rows"`
		}
		if json.Unmarshal(msg, &clientMessage) == nil && clientMessage.Type != "" {
			switch clientMessage.Type {
			case "input":
				if clientMessage.Data == "" {
					continue
				}
				if _, err := session.file.Write([]byte(clientMessage.Data)); err != nil {
					return
				}
			case "resize":
				if clientMessage.Cols < 20 || clientMessage.Rows < 5 {
					continue
				}
				if err := pty.Setsize(session.file, &pty.Winsize{Cols: clientMessage.Cols, Rows: clientMessage.Rows}); err != nil {
					return
				}
			}
			continue
		}
		if len(msg) == 0 || msg[len(msg)-1] != '\n' {
			msg = append(msg, '\n')
		}
		if _, err := session.file.Write(msg); err != nil {
			return
		}
	}
}
