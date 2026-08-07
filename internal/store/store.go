package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

type Project struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	Pinned    bool   `json:"pinned"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
}

type Session struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	Title     string `json:"title"`
	Agent     string `json:"agent"`
	Status    string `json:"status"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
}

type Message struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionId"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	CreatedAt int64  `json:"createdAt"`
}

type MessagePart struct {
	ID        string `json:"id"`
	MessageID string `json:"messageId"`
	SessionID string `json:"sessionId"`
	Type      string `json:"type"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	Metadata  string `json:"metadata"`
	CreatedAt int64  `json:"createdAt"`
}

type Credential struct {
	ID        string `json:"id"`
	Provider  string `json:"provider"`
	Label     string `json:"label"`
	Source    string `json:"source"`
	Redacted  string `json:"redacted"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
}

type ProviderProfile struct {
	ID           string   `json:"id"`
	Kind         string   `json:"kind"`
	DisplayName  string   `json:"displayName"`
	BaseURL      string   `json:"baseURL"`
	Enabled      bool     `json:"enabled"`
	SecretRef    string   `json:"secretRef"`
	Capabilities []string `json:"capabilities"`
	CreatedAt    int64    `json:"createdAt"`
	UpdatedAt    int64    `json:"updatedAt"`
}

type ModelRecord struct {
	ID            string   `json:"id"`
	ProviderID    string   `json:"providerId"`
	DisplayName   string   `json:"displayName"`
	ContextWindow int      `json:"contextWindow"`
	Capabilities  []string `json:"capabilities"`
	Status        string   `json:"status"`
	UpdatedAt     int64    `json:"updatedAt"`
}

type SkillRecord struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"`
	Path        string `json:"path"`
	Enabled     bool   `json:"enabled"`
	Entry       *bool  `json:"entry,omitempty"`
	CreatedAt   int64  `json:"createdAt"`
	UpdatedAt   int64  `json:"updatedAt"`
}

type AgentRecord struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Domain       string   `json:"domain"`
	Description  string   `json:"description"`
	SystemPrompt string   `json:"systemPrompt"`
	Tools        []string `json:"tools"`
	ReviewMode   string   `json:"reviewMode"`
	Enabled      bool     `json:"enabled"`
	BuiltIn      bool     `json:"builtIn"`
	CreatedAt    int64    `json:"createdAt"`
	UpdatedAt    int64    `json:"updatedAt"`
}

type MCPServer struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	Transport      string            `json:"transport"`
	Command        string            `json:"command"`
	Args           []string          `json:"args"`
	Env            []string          `json:"-"`
	Headers        map[string]string `json:"-"`
	URL            string            `json:"url"`
	Enabled        bool              `json:"enabled"`
	Status         string            `json:"status"`
	OAuthSecretRef string            `json:"oauthSecretRef,omitempty"`
	OAuthStatus    string            `json:"oauthStatus,omitempty"`
	BearerToken    string            `json:"-"`
	CreatedAt      int64             `json:"createdAt"`
	UpdatedAt      int64             `json:"updatedAt"`
}

type MCPOAuthFlow struct {
	StateHash             string `json:"stateHash"`
	ServerID              string `json:"serverId"`
	ClientID              string `json:"clientId"`
	AuthorizationEndpoint string `json:"authorizationEndpoint"`
	TokenEndpoint         string `json:"tokenEndpoint"`
	RedirectURI           string `json:"redirectUri"`
	Resource              string `json:"resource"`
	ScopesJSON            string `json:"scopesJson"`
	AuthMethod            string `json:"authMethod"`
	SecretRef             string `json:"secretRef"`
	ExpiresAt             int64  `json:"expiresAt"`
	CreatedAt             int64  `json:"createdAt"`
}

type MCPDiscoveryCache struct {
	ServerID  string `json:"serverId"`
	Payload   string `json:"payload"`
	Status    string `json:"status"`
	UpdatedAt int64  `json:"updatedAt"`
}

type PluginRecord struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Runtime     string   `json:"runtime"`
	Command     string   `json:"command"`
	Args        []string `json:"args"`
	Enabled     bool     `json:"enabled"`
	Status      string   `json:"status"`
	Manifest    string   `json:"manifest"`
	CreatedAt   int64    `json:"createdAt"`
	UpdatedAt   int64    `json:"updatedAt"`
}

type Artifact struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionId"`
	Type      string `json:"type"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	Metadata  string `json:"metadata"`
	CreatedAt int64  `json:"createdAt"`
}

type Notebook struct {
	ID        string         `json:"id"`
	ProjectID string         `json:"projectId"`
	Path      string         `json:"path"`
	Title     string         `json:"title"`
	Cells     []NotebookCell `json:"cells"`
	CreatedAt int64          `json:"createdAt"`
	UpdatedAt int64          `json:"updatedAt"`
}

type NotebookCell struct {
	ID         string `json:"id"`
	NotebookID string `json:"notebookId"`
	Kind       string `json:"kind"`
	Language   string `json:"language"`
	Source     string `json:"source"`
	Output     string `json:"output"`
	OutputJSON string `json:"outputJson"`
	Status     string `json:"status"`
	CreatedAt  int64  `json:"createdAt"`
	UpdatedAt  int64  `json:"updatedAt"`
}

type ProvenanceEdge struct {
	ID         string `json:"id"`
	SourceID   string `json:"sourceId"`
	SourceType string `json:"sourceType"`
	TargetID   string `json:"targetId"`
	TargetType string `json:"targetType"`
	Relation   string `json:"relation"`
	Metadata   string `json:"metadata"`
	CreatedAt  int64  `json:"createdAt"`
}

type Share struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Visibility string `json:"visibility"`
	Status     string `json:"status"`
	URL        string `json:"url"`
	Bundle     string `json:"bundle"`
	CreatedAt  int64  `json:"createdAt"`
	UpdatedAt  int64  `json:"updatedAt"`
}

type AuditEvent struct {
	ID         string `json:"id"`
	Actor      string `json:"actor"`
	Action     string `json:"action"`
	EntityID   string `json:"entityId"`
	EntityType string `json:"entityType"`
	Metadata   string `json:"metadata"`
	CreatedAt  int64  `json:"createdAt"`
}

type AgentRun struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionId"`
	Agent     string `json:"agent"`
	Status    string `json:"status"`
	Goal      string `json:"goal"`
	Model     string `json:"model"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
}

type AgentStep struct {
	ID        string `json:"id"`
	RunID     string `json:"runId"`
	Kind      string `json:"kind"`
	Status    string `json:"status"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	Metadata  string `json:"metadata"`
	CreatedAt int64  `json:"createdAt"`
}

type ToolCall struct {
	ID           string `json:"id"`
	RunID        string `json:"runId"`
	SessionID    string `json:"sessionId"`
	Tool         string `json:"tool"`
	Status       string `json:"status"`
	Input        string `json:"input"`
	Output       string `json:"output"`
	Data         string `json:"data"`
	PermissionID string `json:"permissionId"`
	CreatedAt    int64  `json:"createdAt"`
	UpdatedAt    int64  `json:"updatedAt"`
}

type PermissionRecord struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionId"`
	Type      string `json:"type"`
	Pattern   string `json:"pattern"`
	Message   string `json:"message"`
	Status    string `json:"status"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
}

type RunCheckpoint struct {
	RunID        string `json:"runId"`
	SessionID    string `json:"sessionId"`
	PermissionID string `json:"permissionId"`
	ToolCallID   string `json:"toolCallId"`
	State        string `json:"state"`
	Status       string `json:"status"`
	CreatedAt    int64  `json:"createdAt"`
	UpdatedAt    int64  `json:"updatedAt"`
}

type PendingAction struct {
	ID        string `json:"id"`
	RunID     string `json:"runId"`
	SessionID string `json:"sessionId"`
	Kind      string `json:"kind"`
	Payload   string `json:"payload"`
	Response  string `json:"response"`
	Status    string `json:"status"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
}

type CreateSessionInput struct {
	ProjectID string
	Title     string
	Agent     string
}

type AddMessageInput struct {
	SessionID string
	Role      string
	Content   string
}

func Open(ctx context.Context, path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	store := &Store{db: db}
	if err := store.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
PRAGMA journal_mode = WAL;
CREATE TABLE IF NOT EXISTS projects (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  path TEXT NOT NULL UNIQUE,
  pinned INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS sessions (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  title TEXT NOT NULL,
  agent TEXT NOT NULL,
  status TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS messages (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL,
  role TEXT NOT NULL,
  content TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  FOREIGN KEY(session_id) REFERENCES sessions(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS message_parts (
  id TEXT PRIMARY KEY,
  message_id TEXT NOT NULL,
  session_id TEXT NOT NULL,
  type TEXT NOT NULL,
  title TEXT NOT NULL,
  content TEXT NOT NULL,
  metadata TEXT NOT NULL DEFAULT '{}',
  created_at INTEGER NOT NULL,
  FOREIGN KEY(message_id) REFERENCES messages(id) ON DELETE CASCADE,
  FOREIGN KEY(session_id) REFERENCES sessions(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS permissions (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL,
  type TEXT NOT NULL,
  pattern TEXT NOT NULL,
  message TEXT NOT NULL,
  status TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS artifacts (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL,
  type TEXT NOT NULL,
  title TEXT NOT NULL,
  content TEXT NOT NULL,
  metadata TEXT NOT NULL DEFAULT '{}',
  created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS credentials (
  id TEXT PRIMARY KEY,
  provider TEXT NOT NULL,
  label TEXT NOT NULL,
  source TEXT NOT NULL,
  redacted TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS providers (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL,
  display_name TEXT NOT NULL,
  base_url TEXT NOT NULL DEFAULT '',
  enabled INTEGER NOT NULL DEFAULT 0,
  secret_ref TEXT NOT NULL DEFAULT '',
  capabilities TEXT NOT NULL DEFAULT '[]',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS models (
  id TEXT NOT NULL,
  provider_id TEXT NOT NULL,
  display_name TEXT NOT NULL,
  context_window INTEGER NOT NULL DEFAULT 0,
  capabilities TEXT NOT NULL DEFAULT '[]',
  status TEXT NOT NULL DEFAULT 'available',
  updated_at INTEGER NOT NULL,
  PRIMARY KEY(id, provider_id)
);
CREATE TABLE IF NOT EXISTS agents (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  domain TEXT NOT NULL,
  description TEXT NOT NULL,
  system_prompt TEXT NOT NULL DEFAULT '',
  tools TEXT NOT NULL DEFAULT '[]',
  review_mode TEXT NOT NULL DEFAULT 'optional',
  enabled INTEGER NOT NULL DEFAULT 1,
  built_in INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS tools (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  category TEXT NOT NULL,
  description TEXT NOT NULL,
  risk TEXT NOT NULL,
  requires_approval INTEGER NOT NULL DEFAULT 0,
  enabled INTEGER NOT NULL DEFAULT 1,
  updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS skills (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  description TEXT NOT NULL,
  source TEXT NOT NULL,
  path TEXT NOT NULL DEFAULT '',
  enabled INTEGER NOT NULL DEFAULT 1,
  entry INTEGER NOT NULL DEFAULT 1,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS mcp_servers (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  transport TEXT NOT NULL,
  command TEXT NOT NULL DEFAULT '',
  args_json TEXT NOT NULL DEFAULT '[]',
  url TEXT NOT NULL DEFAULT '',
  enabled INTEGER NOT NULL DEFAULT 1,
  status TEXT NOT NULL,
  oauth_secret_ref TEXT NOT NULL DEFAULT '',
  oauth_status TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS mcp_discovery_cache (
  server_id TEXT PRIMARY KEY,
  payload TEXT NOT NULL,
  status TEXT NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS mcp_oauth_flows (
  state_hash TEXT PRIMARY KEY,
  server_id TEXT NOT NULL,
  client_id TEXT NOT NULL,
  authorization_endpoint TEXT NOT NULL,
  token_endpoint TEXT NOT NULL,
  redirect_uri TEXT NOT NULL,
  resource TEXT NOT NULL,
  scopes_json TEXT NOT NULL DEFAULT '[]',
  auth_method TEXT NOT NULL DEFAULT 'none',
  secret_ref TEXT NOT NULL,
  expires_at INTEGER NOT NULL,
  created_at INTEGER NOT NULL,
  FOREIGN KEY (server_id) REFERENCES mcp_servers(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS plugins (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  version TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  runtime TEXT NOT NULL DEFAULT 'subprocess',
  command TEXT NOT NULL DEFAULT '',
  args_json TEXT NOT NULL DEFAULT '[]',
  enabled INTEGER NOT NULL DEFAULT 1,
  status TEXT NOT NULL DEFAULT 'configured',
  manifest TEXT NOT NULL DEFAULT '{}',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS notebooks (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL DEFAULT '',
  path TEXT NOT NULL,
  title TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS notebook_cells (
  id TEXT PRIMARY KEY,
  notebook_id TEXT NOT NULL,
  kind TEXT NOT NULL,
  language TEXT NOT NULL,
  source TEXT NOT NULL,
  output TEXT NOT NULL DEFAULT '',
  output_json TEXT NOT NULL DEFAULT '[]',
  status TEXT NOT NULL DEFAULT 'idle',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  FOREIGN KEY(notebook_id) REFERENCES notebooks(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS provenance_edges (
  id TEXT PRIMARY KEY,
  source_id TEXT NOT NULL,
  source_type TEXT NOT NULL,
  target_id TEXT NOT NULL,
  target_type TEXT NOT NULL,
  relation TEXT NOT NULL,
  metadata TEXT NOT NULL DEFAULT '{}',
  created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS shares (
  id TEXT PRIMARY KEY,
  title TEXT NOT NULL,
  visibility TEXT NOT NULL,
  status TEXT NOT NULL,
  url TEXT NOT NULL,
  bundle TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS audit_events (
  id TEXT PRIMARY KEY,
  actor TEXT NOT NULL,
  action TEXT NOT NULL,
  entity_id TEXT NOT NULL,
  entity_type TEXT NOT NULL,
  metadata TEXT NOT NULL DEFAULT '{}',
  created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS secret_refs (
  id TEXT PRIMARY KEY,
  provider TEXT NOT NULL,
  label TEXT NOT NULL,
  backend TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS agent_runs (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL,
  agent TEXT NOT NULL,
  status TEXT NOT NULL,
  goal TEXT NOT NULL,
  model TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS agent_steps (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL,
  kind TEXT NOT NULL,
  status TEXT NOT NULL,
  title TEXT NOT NULL,
  content TEXT NOT NULL,
  metadata TEXT NOT NULL DEFAULT '{}',
  created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS tool_calls (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL,
  session_id TEXT NOT NULL,
  tool TEXT NOT NULL,
  status TEXT NOT NULL,
  input TEXT NOT NULL DEFAULT '{}',
  output TEXT NOT NULL DEFAULT '',
  data TEXT NOT NULL DEFAULT '{}',
  permission_id TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS run_checkpoints (
  run_id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL,
  permission_id TEXT NOT NULL DEFAULT '',
  tool_call_id TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL,
  status TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS pending_actions (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL,
  session_id TEXT NOT NULL,
  kind TEXT NOT NULL,
  payload TEXT NOT NULL DEFAULT '{}',
  response TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS cloud_users (
  id TEXT PRIMARY KEY,
  actor TEXT NOT NULL UNIQUE,
  display_name TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS cloud_orgs (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS cloud_org_members (
  org_id TEXT NOT NULL,
  user_id TEXT NOT NULL,
  role TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (org_id, user_id),
  FOREIGN KEY (org_id) REFERENCES cloud_orgs(id) ON DELETE CASCADE,
  FOREIGN KEY (user_id) REFERENCES cloud_users(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS cloud_auth_sessions (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL,
  device_id TEXT NOT NULL,
  token_hash TEXT NOT NULL UNIQUE,
  expires_at INTEGER NOT NULL,
  revoked_at INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  FOREIGN KEY (user_id) REFERENCES cloud_users(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS cloud_devices (
  id TEXT PRIMARY KEY,
  org_id TEXT NOT NULL,
  user_id TEXT NOT NULL,
  name TEXT NOT NULL,
  mode TEXT NOT NULL,
  last_seen_at INTEGER NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  FOREIGN KEY (org_id) REFERENCES cloud_orgs(id) ON DELETE CASCADE,
  FOREIGN KEY (user_id) REFERENCES cloud_users(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS cloud_resource_owners (
  entity_type TEXT NOT NULL,
  entity_id TEXT NOT NULL,
  org_id TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (entity_type, entity_id),
  FOREIGN KEY (org_id) REFERENCES cloud_orgs(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS cloud_usage_ledger (
  id TEXT PRIMARY KEY,
  org_id TEXT NOT NULL,
  provider TEXT NOT NULL DEFAULT '',
  model TEXT NOT NULL DEFAULT '',
  input_tokens INTEGER NOT NULL DEFAULT 0,
  output_tokens INTEGER NOT NULL DEFAULT 0,
  cost_micros INTEGER NOT NULL DEFAULT 0,
  credit_delta_micros INTEGER NOT NULL DEFAULT 0,
  metadata TEXT NOT NULL DEFAULT '{}',
  created_at INTEGER NOT NULL,
  FOREIGN KEY (org_id) REFERENCES cloud_orgs(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS cloud_share_links (
  id TEXT PRIMARY KEY,
  share_id TEXT NOT NULL,
  org_id TEXT NOT NULL,
  token_hash TEXT NOT NULL UNIQUE,
  expires_at INTEGER NOT NULL DEFAULT 0,
  revoked_at INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  FOREIGN KEY (share_id) REFERENCES shares(id) ON DELETE CASCADE,
  FOREIGN KEY (org_id) REFERENCES cloud_orgs(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_cloud_auth_sessions_token ON cloud_auth_sessions(token_hash);
CREATE INDEX IF NOT EXISTS idx_cloud_members_user ON cloud_org_members(user_id);
CREATE INDEX IF NOT EXISTS idx_cloud_owners_org ON cloud_resource_owners(org_id, entity_type);
CREATE INDEX IF NOT EXISTS idx_cloud_usage_org ON cloud_usage_ledger(org_id, created_at);
CREATE INDEX IF NOT EXISTS idx_cloud_share_links_token ON cloud_share_links(token_hash);
UPDATE settings SET value = '' WHERE key = 'defaultProvider' AND value = 'mock';
UPDATE settings SET value = 'live-only' WHERE key = 'scienceNetwork' AND value IN ('live-with-fallback', 'offline-fallback');
`)
	if err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "notebook_cells", "output_json", "TEXT NOT NULL DEFAULT '[]'"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "mcp_servers", "args_json", "TEXT NOT NULL DEFAULT '[]'"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "mcp_servers", "oauth_secret_ref", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "mcp_servers", "oauth_status", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "plugins", "args_json", "TEXT NOT NULL DEFAULT '[]'"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "agents", "system_prompt", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "skills", "entry", "INTEGER NOT NULL DEFAULT 1"); err != nil {
		return err
	}
	return s.ensureColumn(ctx, "tool_calls", "data", "TEXT NOT NULL DEFAULT '{}'")
}

func (s *Store) ensureColumn(ctx context.Context, table, column, definition string) error {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, primaryKey int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			_ = rows.Close()
			return err
		}
		if name == column {
			found = true
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if found {
		return nil
	}
	_, err = s.db.ExecContext(ctx, `ALTER TABLE `+table+` ADD COLUMN `+column+` `+definition)
	return err
}

func (s *Store) OpenProject(ctx context.Context, projectPath string) (Project, error) {
	now := unixMillis()
	clean := filepath.Clean(projectPath)
	name := filepath.Base(clean)
	if name == "." || name == string(filepath.Separator) {
		name = clean
	}

	var existing Project
	err := s.db.QueryRowContext(ctx, `SELECT id, name, path, pinned, created_at, updated_at FROM projects WHERE path = ?`, clean).
		Scan(&existing.ID, &existing.Name, &existing.Path, &existing.Pinned, &existing.CreatedAt, &existing.UpdatedAt)
	if err == nil {
		existing.UpdatedAt = now
		_, err = s.db.ExecContext(ctx, `UPDATE projects SET updated_at = ? WHERE id = ?`, now, existing.ID)
		return existing, err
	}
	if err != sql.ErrNoRows {
		return Project{}, err
	}

	project := Project{ID: newID("project"), Name: name, Path: clean, CreatedAt: now, UpdatedAt: now}
	_, err = s.db.ExecContext(ctx, `INSERT INTO projects (id, name, path, pinned, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		project.ID, project.Name, project.Path, boolInt(project.Pinned), project.CreatedAt, project.UpdatedAt)
	return project, err
}

func (s *Store) ListProjects(ctx context.Context) ([]Project, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, path, pinned, created_at, updated_at FROM projects ORDER BY pinned DESC, updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Project, 0)
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.Name, &p.Path, &p.Pinned, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

func (s *Store) CreateSession(ctx context.Context, input CreateSessionInput) (Session, error) {
	now := unixMillis()
	title := strings.TrimSpace(input.Title)
	if title == "" {
		title = "New research session"
	}
	agent := strings.TrimSpace(input.Agent)
	if agent == "" {
		agent = "research"
	}
	session := Session{
		ID:        newID("session"),
		ProjectID: input.ProjectID,
		Title:     title,
		Agent:     agent,
		Status:    "idle",
		CreatedAt: now,
		UpdatedAt: now,
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions (id, project_id, title, agent, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		session.ID, session.ProjectID, session.Title, session.Agent, session.Status, session.CreatedAt, session.UpdatedAt)
	return session, err
}

func (s *Store) ListSessions(ctx context.Context, projectID string) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, project_id, title, agent, status, created_at, updated_at FROM sessions WHERE project_id = ? ORDER BY updated_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Session, 0)
	for rows.Next() {
		var session Session
		if err := rows.Scan(&session.ID, &session.ProjectID, &session.Title, &session.Agent, &session.Status, &session.CreatedAt, &session.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, session)
	}
	return result, rows.Err()
}

func (s *Store) GetSession(ctx context.Context, id string) (Session, error) {
	var session Session
	err := s.db.QueryRowContext(ctx, `SELECT id, project_id, title, agent, status, created_at, updated_at FROM sessions WHERE id = ?`, id).
		Scan(&session.ID, &session.ProjectID, &session.Title, &session.Agent, &session.Status, &session.CreatedAt, &session.UpdatedAt)
	return session, err
}

func (s *Store) UpdateSession(ctx context.Context, session Session) (Session, error) {
	session.UpdatedAt = unixMillis()
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET title = ?, agent = ?, status = ?, updated_at = ? WHERE id = ?`,
		session.Title, session.Agent, session.Status, session.UpdatedAt, session.ID)
	return session, err
}

func (s *Store) DeleteSession(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	return err
}

func (s *Store) AddMessage(ctx context.Context, input AddMessageInput) (Message, error) {
	now := unixMillis()
	message := Message{ID: newID("message"), SessionID: input.SessionID, Role: input.Role, Content: input.Content, CreatedAt: now}
	_, err := s.db.ExecContext(ctx, `INSERT INTO messages (id, session_id, role, content, created_at) VALUES (?, ?, ?, ?, ?)`,
		message.ID, message.SessionID, message.Role, message.Content, message.CreatedAt)
	if err != nil {
		return Message{}, err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE sessions SET updated_at = ? WHERE id = ?`, now, input.SessionID)
	return message, err
}

func (s *Store) AddMessagePart(ctx context.Context, part MessagePart) (MessagePart, error) {
	if part.ID == "" {
		part.ID = newID("part")
	}
	if part.CreatedAt == 0 {
		part.CreatedAt = unixMillis()
	}
	if part.Metadata == "" {
		part.Metadata = "{}"
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO message_parts (id, message_id, session_id, type, title, content, metadata, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		part.ID, part.MessageID, part.SessionID, part.Type, part.Title, part.Content, part.Metadata, part.CreatedAt)
	return part, err
}

func (s *Store) ListMessageParts(ctx context.Context, messageID string) ([]MessagePart, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, message_id, session_id, type, title, content, metadata, created_at FROM message_parts WHERE message_id = ? ORDER BY created_at ASC`, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]MessagePart, 0)
	for rows.Next() {
		var part MessagePart
		if err := rows.Scan(&part.ID, &part.MessageID, &part.SessionID, &part.Type, &part.Title, &part.Content, &part.Metadata, &part.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, part)
	}
	return result, rows.Err()
}

func (s *Store) ListMessages(ctx context.Context, sessionID string) ([]Message, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, session_id, role, content, created_at FROM messages WHERE session_id = ? ORDER BY created_at ASC`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Message, 0)
	for rows.Next() {
		var msg Message
		if err := rows.Scan(&msg.ID, &msg.SessionID, &msg.Role, &msg.Content, &msg.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, msg)
	}
	return result, rows.Err()
}

func (s *Store) UpsertCredential(ctx context.Context, credential Credential) (Credential, error) {
	now := unixMillis()
	credential.Provider = strings.TrimSpace(credential.Provider)
	if credential.Provider == "" {
		credential.Provider = "local"
	}
	var existing Credential
	if credential.ID == "" {
		err := s.db.QueryRowContext(ctx, `SELECT id, created_at FROM credentials WHERE provider = ? ORDER BY updated_at DESC LIMIT 1`, credential.Provider).
			Scan(&existing.ID, &existing.CreatedAt)
		if err == nil {
			credential.ID = existing.ID
			credential.CreatedAt = existing.CreatedAt
		} else if err != sql.ErrNoRows {
			return Credential{}, err
		}
	}
	if credential.ID == "" {
		credential.ID = newID("credential")
		credential.CreatedAt = now
	}
	if credential.CreatedAt == 0 {
		credential.CreatedAt = now
	}
	credential.UpdatedAt = now
	if credential.Source == "" {
		credential.Source = "local"
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO credentials (id, provider, label, source, redacted, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET provider=excluded.provider, label=excluded.label, source=excluded.source, redacted=excluded.redacted, updated_at=excluded.updated_at`,
		credential.ID, credential.Provider, credential.Label, credential.Source, credential.Redacted, credential.CreatedAt, credential.UpdatedAt)
	return credential, err
}

func (s *Store) ListCredentials(ctx context.Context) ([]Credential, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, provider, label, source, redacted, created_at, updated_at FROM credentials ORDER BY provider ASC, updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Credential, 0)
	for rows.Next() {
		var credential Credential
		if err := rows.Scan(&credential.ID, &credential.Provider, &credential.Label, &credential.Source, &credential.Redacted, &credential.CreatedAt, &credential.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, credential)
	}
	return result, rows.Err()
}

func (s *Store) DeleteCredential(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM credentials WHERE id = ?`, id)
	return err
}

func (s *Store) ListSettings(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM settings ORDER BY key ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]string)
	for rows.Next() {
		var key string
		var value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		result[key] = value
	}
	return result, rows.Err()
}

func (s *Store) SetSettings(ctx context.Context, settings map[string]string) error {
	if len(settings) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := unixMillis()
	for key, value := range settings {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`, key, value, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) UpsertProvider(ctx context.Context, provider ProviderProfile) (ProviderProfile, error) {
	now := unixMillis()
	provider.ID = slugOrID(provider.ID, provider.DisplayName, "provider")
	if provider.Kind == "" {
		provider.Kind = "openai-compatible"
	}
	if provider.DisplayName == "" {
		provider.DisplayName = provider.ID
	}
	if provider.CreatedAt == 0 {
		provider.CreatedAt = now
	}
	provider.UpdatedAt = now
	caps := mustJSON(provider.Capabilities)
	_, err := s.db.ExecContext(ctx, `INSERT INTO providers (id, kind, display_name, base_url, enabled, secret_ref, capabilities, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET kind=excluded.kind, display_name=excluded.display_name, base_url=excluded.base_url, enabled=excluded.enabled, secret_ref=excluded.secret_ref, capabilities=excluded.capabilities, updated_at=excluded.updated_at`,
		provider.ID, provider.Kind, provider.DisplayName, provider.BaseURL, boolInt(provider.Enabled), provider.SecretRef, caps, provider.CreatedAt, provider.UpdatedAt)
	return provider, err
}

func (s *Store) ListProviders(ctx context.Context) ([]ProviderProfile, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, kind, display_name, base_url, enabled, secret_ref, capabilities, created_at, updated_at FROM providers ORDER BY display_name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]ProviderProfile, 0)
	for rows.Next() {
		var provider ProviderProfile
		var caps string
		if err := rows.Scan(&provider.ID, &provider.Kind, &provider.DisplayName, &provider.BaseURL, &provider.Enabled, &provider.SecretRef, &caps, &provider.CreatedAt, &provider.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(caps), &provider.Capabilities)
		result = append(result, provider)
	}
	return result, rows.Err()
}

func (s *Store) DeleteProvider(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM providers WHERE id = ?`, id)
	return err
}

func (s *Store) CreateAgent(ctx context.Context, record AgentRecord) (AgentRecord, error) {
	now := unixMillis()
	record.ID = strings.TrimSpace(record.ID)
	if record.CreatedAt == 0 {
		record.CreatedAt = now
	}
	record.UpdatedAt = now
	_, err := s.db.ExecContext(ctx, `INSERT INTO agents
(id, name, domain, description, system_prompt, tools, review_mode, enabled, built_in, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, record.ID, record.Name, record.Domain, record.Description, record.SystemPrompt,
		mustJSON(record.Tools), record.ReviewMode, boolInt(record.Enabled), boolInt(record.BuiltIn), record.CreatedAt, record.UpdatedAt)
	return record, err
}

func (s *Store) UpdateAgent(ctx context.Context, record AgentRecord) (AgentRecord, error) {
	record.ID = strings.TrimSpace(record.ID)
	record.UpdatedAt = unixMillis()
	result, err := s.db.ExecContext(ctx, `UPDATE agents SET
name = ?, domain = ?, description = ?, system_prompt = ?, tools = ?, review_mode = ?, enabled = ?, updated_at = ?
WHERE id = ? AND built_in = 0`, record.Name, record.Domain, record.Description, record.SystemPrompt, mustJSON(record.Tools),
		record.ReviewMode, boolInt(record.Enabled), record.UpdatedAt, record.ID)
	if err != nil {
		return AgentRecord{}, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return AgentRecord{}, err
	}
	if rows == 0 {
		return AgentRecord{}, sql.ErrNoRows
	}
	return s.GetAgent(ctx, record.ID)
}

func (s *Store) GetAgent(ctx context.Context, id string) (AgentRecord, error) {
	var record AgentRecord
	var toolsJSON string
	err := s.db.QueryRowContext(ctx, `SELECT id, name, domain, description, system_prompt, tools, review_mode, enabled, built_in, created_at, updated_at
FROM agents WHERE id = ?`, strings.TrimSpace(id)).Scan(&record.ID, &record.Name, &record.Domain, &record.Description, &record.SystemPrompt,
		&toolsJSON, &record.ReviewMode, &record.Enabled, &record.BuiltIn, &record.CreatedAt, &record.UpdatedAt)
	if err != nil {
		return AgentRecord{}, err
	}
	if err := json.Unmarshal([]byte(toolsJSON), &record.Tools); err != nil {
		return AgentRecord{}, err
	}
	return record, nil
}

func (s *Store) ListAgents(ctx context.Context) ([]AgentRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, domain, description, system_prompt, tools, review_mode, enabled, built_in, created_at, updated_at
FROM agents ORDER BY name ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := make([]AgentRecord, 0)
	for rows.Next() {
		var record AgentRecord
		var toolsJSON string
		if err := rows.Scan(&record.ID, &record.Name, &record.Domain, &record.Description, &record.SystemPrompt, &toolsJSON,
			&record.ReviewMode, &record.Enabled, &record.BuiltIn, &record.CreatedAt, &record.UpdatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(toolsJSON), &record.Tools); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (s *Store) DeleteAgent(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM agents WHERE id = ? AND built_in = 0`, strings.TrimSpace(id))
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) ReplaceModelCatalog(ctx context.Context, models []ModelRecord) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM models`); err != nil {
		return err
	}
	now := unixMillis()
	for _, model := range models {
		model.ID = strings.TrimSpace(model.ID)
		model.ProviderID = strings.TrimSpace(model.ProviderID)
		if model.ID == "" || model.ProviderID == "" {
			continue
		}
		if model.DisplayName == "" {
			model.DisplayName = model.ID
		}
		if model.Status == "" {
			model.Status = "refreshed"
		}
		if model.UpdatedAt == 0 {
			model.UpdatedAt = now
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO models (id, provider_id, display_name, context_window, capabilities, status, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id, provider_id) DO UPDATE SET display_name=excluded.display_name, context_window=excluded.context_window, capabilities=excluded.capabilities, status=excluded.status, updated_at=excluded.updated_at`,
			model.ID, model.ProviderID, model.DisplayName, model.ContextWindow, mustJSON(model.Capabilities), model.Status, model.UpdatedAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ListModels(ctx context.Context, providerID string) ([]ModelRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, provider_id, display_name, context_window, capabilities, status, updated_at
FROM models WHERE provider_id = ? ORDER BY display_name ASC, id ASC`, providerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]ModelRecord, 0)
	for rows.Next() {
		var model ModelRecord
		var caps string
		if err := rows.Scan(&model.ID, &model.ProviderID, &model.DisplayName, &model.ContextWindow, &caps, &model.Status, &model.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(caps), &model.Capabilities)
		result = append(result, model)
	}
	return result, rows.Err()
}

func (s *Store) UpsertSkill(ctx context.Context, skill SkillRecord) (SkillRecord, error) {
	now := unixMillis()
	skill.ID = slugOrID(skill.ID, skill.Name, "skill")
	if skill.Name == "" {
		skill.Name = skill.ID
	}
	if skill.Source == "" {
		skill.Source = "project"
	}
	if skill.CreatedAt == 0 {
		skill.CreatedAt = now
	}
	skill.UpdatedAt = now
	entry := true
	if skill.Entry != nil {
		entry = *skill.Entry
	}
	skill.Entry = boolPointer(entry)
	_, err := s.db.ExecContext(ctx, `INSERT INTO skills (id, name, description, source, path, enabled, entry, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET name=excluded.name, description=excluded.description, source=excluded.source, path=excluded.path, enabled=excluded.enabled, entry=excluded.entry, updated_at=excluded.updated_at
ON CONFLICT(name) DO UPDATE SET description=excluded.description, source=excluded.source, path=excluded.path, enabled=excluded.enabled, entry=excluded.entry, updated_at=excluded.updated_at`,
		skill.ID, skill.Name, skill.Description, skill.Source, skill.Path, boolInt(skill.Enabled), boolInt(entry), skill.CreatedAt, skill.UpdatedAt)
	return skill, err
}

func (s *Store) ListSkills(ctx context.Context) ([]SkillRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, description, source, path, enabled, entry, created_at, updated_at FROM skills ORDER BY source ASC, name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]SkillRecord, 0)
	for rows.Next() {
		var skill SkillRecord
		var entry bool
		if err := rows.Scan(&skill.ID, &skill.Name, &skill.Description, &skill.Source, &skill.Path, &skill.Enabled, &entry, &skill.CreatedAt, &skill.UpdatedAt); err != nil {
			return nil, err
		}
		skill.Entry = boolPointer(entry)
		result = append(result, skill)
	}
	return result, rows.Err()
}

func boolPointer(value bool) *bool {
	return &value
}

func (s *Store) DeleteSkill(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM skills WHERE id = ?`, id)
	return err
}

func (s *Store) UpsertMCPServer(ctx context.Context, server MCPServer) (MCPServer, error) {
	now := unixMillis()
	server.ID = slugOrID(server.ID, server.Name, "mcp")
	if server.Name == "" {
		server.Name = server.ID
	}
	if server.Transport == "" {
		server.Transport = "stdio"
	}
	if server.Status == "" {
		if server.Enabled {
			server.Status = "configured"
		} else {
			server.Status = "disabled"
		}
	}
	if server.CreatedAt == 0 {
		server.CreatedAt = now
	}
	if server.Args == nil {
		server.Args = []string{}
	}
	argsJSON, err := json.Marshal(server.Args)
	if err != nil {
		return MCPServer{}, err
	}
	server.UpdatedAt = now
	_, err = s.db.ExecContext(ctx, `INSERT INTO mcp_servers (id, name, transport, command, args_json, url, enabled, status, oauth_secret_ref, oauth_status, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET name=excluded.name, transport=excluded.transport, command=excluded.command, args_json=excluded.args_json, url=excluded.url, enabled=excluded.enabled, status=excluded.status, oauth_secret_ref=excluded.oauth_secret_ref, oauth_status=excluded.oauth_status, updated_at=excluded.updated_at`,
		server.ID, server.Name, server.Transport, server.Command, string(argsJSON), server.URL, boolInt(server.Enabled), server.Status, server.OAuthSecretRef, server.OAuthStatus, server.CreatedAt, server.UpdatedAt)
	return server, err
}

func (s *Store) ListMCPServers(ctx context.Context) ([]MCPServer, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, transport, command, args_json, url, enabled, status, oauth_secret_ref, oauth_status, created_at, updated_at FROM mcp_servers ORDER BY name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]MCPServer, 0)
	for rows.Next() {
		var server MCPServer
		var argsJSON string
		if err := rows.Scan(&server.ID, &server.Name, &server.Transport, &server.Command, &argsJSON, &server.URL, &server.Enabled, &server.Status, &server.OAuthSecretRef, &server.OAuthStatus, &server.CreatedAt, &server.UpdatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(argsJSON), &server.Args); err != nil {
			return nil, err
		}
		result = append(result, server)
	}
	return result, rows.Err()
}

func (s *Store) GetMCPServer(ctx context.Context, id string) (MCPServer, error) {
	var server MCPServer
	var argsJSON string
	err := s.db.QueryRowContext(ctx, `SELECT id, name, transport, command, args_json, url, enabled, status, oauth_secret_ref, oauth_status, created_at, updated_at FROM mcp_servers WHERE id = ?`, id).
		Scan(&server.ID, &server.Name, &server.Transport, &server.Command, &argsJSON, &server.URL, &server.Enabled, &server.Status, &server.OAuthSecretRef, &server.OAuthStatus, &server.CreatedAt, &server.UpdatedAt)
	if err == nil {
		err = json.Unmarshal([]byte(argsJSON), &server.Args)
	}
	return server, err
}

func (s *Store) DeleteMCPServer(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM mcp_servers WHERE id = ?`, id)
	return err
}

func (s *Store) UpsertMCPDiscovery(ctx context.Context, cache MCPDiscoveryCache) (MCPDiscoveryCache, error) {
	cache.ServerID = strings.TrimSpace(cache.ServerID)
	if cache.Payload == "" {
		cache.Payload = "{}"
	}
	if cache.Status == "" {
		cache.Status = "refreshed"
	}
	cache.UpdatedAt = unixMillis()
	_, err := s.db.ExecContext(ctx, `INSERT INTO mcp_discovery_cache (server_id, payload, status, updated_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(server_id) DO UPDATE SET payload=excluded.payload, status=excluded.status, updated_at=excluded.updated_at`,
		cache.ServerID, cache.Payload, cache.Status, cache.UpdatedAt)
	return cache, err
}

func (s *Store) GetMCPDiscovery(ctx context.Context, serverID string) (MCPDiscoveryCache, error) {
	var cache MCPDiscoveryCache
	err := s.db.QueryRowContext(ctx, `SELECT server_id, payload, status, updated_at FROM mcp_discovery_cache WHERE server_id = ?`, serverID).
		Scan(&cache.ServerID, &cache.Payload, &cache.Status, &cache.UpdatedAt)
	return cache, err
}

func (s *Store) CreateMCPOAuthFlow(ctx context.Context, flow MCPOAuthFlow) (MCPOAuthFlow, error) {
	if strings.TrimSpace(flow.StateHash) == "" || strings.TrimSpace(flow.ServerID) == "" {
		return MCPOAuthFlow{}, errors.New("oauth flow state hash and server id are required")
	}
	if flow.ScopesJSON == "" {
		flow.ScopesJSON = "[]"
	}
	if flow.AuthMethod == "" {
		flow.AuthMethod = "none"
	}
	if flow.CreatedAt == 0 {
		flow.CreatedAt = unixMillis()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO mcp_oauth_flows (state_hash, server_id, client_id, authorization_endpoint, token_endpoint, redirect_uri, resource, scopes_json, auth_method, secret_ref, expires_at, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(state_hash) DO UPDATE SET server_id=excluded.server_id, client_id=excluded.client_id, authorization_endpoint=excluded.authorization_endpoint, token_endpoint=excluded.token_endpoint, redirect_uri=excluded.redirect_uri, resource=excluded.resource, scopes_json=excluded.scopes_json, auth_method=excluded.auth_method, secret_ref=excluded.secret_ref, expires_at=excluded.expires_at, created_at=excluded.created_at`,
		flow.StateHash, flow.ServerID, flow.ClientID, flow.AuthorizationEndpoint, flow.TokenEndpoint, flow.RedirectURI, flow.Resource, flow.ScopesJSON, flow.AuthMethod, flow.SecretRef, flow.ExpiresAt, flow.CreatedAt)
	return flow, err
}

func (s *Store) GetMCPOAuthFlow(ctx context.Context, stateHash string) (MCPOAuthFlow, error) {
	var flow MCPOAuthFlow
	err := s.db.QueryRowContext(ctx, `SELECT state_hash, server_id, client_id, authorization_endpoint, token_endpoint, redirect_uri, resource, scopes_json, auth_method, secret_ref, expires_at, created_at FROM mcp_oauth_flows WHERE state_hash = ? AND expires_at > ?`, stateHash, unixMillis()).
		Scan(&flow.StateHash, &flow.ServerID, &flow.ClientID, &flow.AuthorizationEndpoint, &flow.TokenEndpoint, &flow.RedirectURI, &flow.Resource, &flow.ScopesJSON, &flow.AuthMethod, &flow.SecretRef, &flow.ExpiresAt, &flow.CreatedAt)
	return flow, err
}

func (s *Store) DeleteMCPOAuthFlow(ctx context.Context, stateHash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM mcp_oauth_flows WHERE state_hash = ?`, stateHash)
	return err
}

func (s *Store) UpsertPlugin(ctx context.Context, plugin PluginRecord) (PluginRecord, error) {
	now := unixMillis()
	plugin.ID = slugOrID(plugin.ID, plugin.Name, "plugin")
	if plugin.Name == "" {
		plugin.Name = plugin.ID
	}
	if plugin.Runtime == "" {
		plugin.Runtime = "subprocess"
	}
	if plugin.Status == "" {
		if plugin.Enabled {
			plugin.Status = "configured"
		} else {
			plugin.Status = "disabled"
		}
	}
	if plugin.Manifest == "" {
		plugin.Manifest = "{}"
	}
	if plugin.CreatedAt == 0 {
		plugin.CreatedAt = now
	}
	if plugin.Args == nil {
		plugin.Args = []string{}
	}
	argsJSON, err := json.Marshal(plugin.Args)
	if err != nil {
		return PluginRecord{}, err
	}
	plugin.UpdatedAt = now
	_, err = s.db.ExecContext(ctx, `INSERT INTO plugins (id, name, version, description, runtime, command, args_json, enabled, status, manifest, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET name=excluded.name, version=excluded.version, description=excluded.description, runtime=excluded.runtime, command=excluded.command, args_json=excluded.args_json, enabled=excluded.enabled, status=excluded.status, manifest=excluded.manifest, updated_at=excluded.updated_at`,
		plugin.ID, plugin.Name, plugin.Version, plugin.Description, plugin.Runtime, plugin.Command, string(argsJSON), boolInt(plugin.Enabled), plugin.Status, plugin.Manifest, plugin.CreatedAt, plugin.UpdatedAt)
	return plugin, err
}

func (s *Store) ListPlugins(ctx context.Context) ([]PluginRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, version, description, runtime, command, args_json, enabled, status, manifest, created_at, updated_at FROM plugins ORDER BY name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]PluginRecord, 0)
	for rows.Next() {
		var plugin PluginRecord
		var argsJSON string
		if err := rows.Scan(&plugin.ID, &plugin.Name, &plugin.Version, &plugin.Description, &plugin.Runtime, &plugin.Command, &argsJSON, &plugin.Enabled, &plugin.Status, &plugin.Manifest, &plugin.CreatedAt, &plugin.UpdatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(argsJSON), &plugin.Args); err != nil {
			return nil, err
		}
		result = append(result, plugin)
	}
	return result, rows.Err()
}

func (s *Store) GetPlugin(ctx context.Context, id string) (PluginRecord, error) {
	var plugin PluginRecord
	var argsJSON string
	err := s.db.QueryRowContext(ctx, `SELECT id, name, version, description, runtime, command, args_json, enabled, status, manifest, created_at, updated_at FROM plugins WHERE id = ?`, id).
		Scan(&plugin.ID, &plugin.Name, &plugin.Version, &plugin.Description, &plugin.Runtime, &plugin.Command, &argsJSON, &plugin.Enabled, &plugin.Status, &plugin.Manifest, &plugin.CreatedAt, &plugin.UpdatedAt)
	if err == nil {
		err = json.Unmarshal([]byte(argsJSON), &plugin.Args)
	}
	return plugin, err
}

func (s *Store) DeletePlugin(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM plugins WHERE id = ?`, id)
	return err
}

func (s *Store) CreateNotebook(ctx context.Context, notebook Notebook) (Notebook, error) {
	now := unixMillis()
	if notebook.ID == "" {
		notebook.ID = newID("notebook")
	}
	if notebook.Title == "" {
		notebook.Title = "Untitled notebook"
	}
	if notebook.Path == "" {
		notebook.Path = notebook.ID + ".ipynb"
	}
	notebook.CreatedAt = now
	notebook.UpdatedAt = now
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Notebook{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO notebooks (id, project_id, path, title, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		notebook.ID, notebook.ProjectID, notebook.Path, notebook.Title, notebook.CreatedAt, notebook.UpdatedAt); err != nil {
		return Notebook{}, err
	}
	for i := range notebook.Cells {
		cell := &notebook.Cells[i]
		if cell.ID == "" {
			cell.ID = newID("cell")
		}
		cell.NotebookID = notebook.ID
		if cell.Kind == "" {
			cell.Kind = "code"
		}
		if cell.Language == "" {
			cell.Language = "python"
		}
		if cell.Status == "" {
			cell.Status = "idle"
		}
		cell.CreatedAt = now
		cell.UpdatedAt = now
		if cell.OutputJSON == "" {
			cell.OutputJSON = "[]"
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO notebook_cells (id, notebook_id, kind, language, source, output, output_json, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			cell.ID, cell.NotebookID, cell.Kind, cell.Language, cell.Source, cell.Output, cell.OutputJSON, cell.Status, cell.CreatedAt, cell.UpdatedAt); err != nil {
			return Notebook{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Notebook{}, err
	}
	return s.GetNotebook(ctx, notebook.ID)
}

func (s *Store) ListNotebooks(ctx context.Context) ([]Notebook, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, project_id, path, title, created_at, updated_at FROM notebooks ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	result := make([]Notebook, 0)
	for rows.Next() {
		var notebook Notebook
		if err := rows.Scan(&notebook.ID, &notebook.ProjectID, &notebook.Path, &notebook.Title, &notebook.CreatedAt, &notebook.UpdatedAt); err != nil {
			_ = rows.Close()
			return nil, err
		}
		result = append(result, notebook)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for index := range result {
		cells, err := s.ListNotebookCells(ctx, result[index].ID)
		if err != nil {
			return nil, err
		}
		result[index].Cells = cells
	}
	return result, nil
}

func (s *Store) GetNotebook(ctx context.Context, id string) (Notebook, error) {
	var notebook Notebook
	err := s.db.QueryRowContext(ctx, `SELECT id, project_id, path, title, created_at, updated_at FROM notebooks WHERE id = ?`, id).
		Scan(&notebook.ID, &notebook.ProjectID, &notebook.Path, &notebook.Title, &notebook.CreatedAt, &notebook.UpdatedAt)
	if err != nil {
		return Notebook{}, err
	}
	cells, err := s.ListNotebookCells(ctx, id)
	if err != nil {
		return Notebook{}, err
	}
	notebook.Cells = cells
	return notebook, nil
}

func (s *Store) ListNotebookCells(ctx context.Context, notebookID string) ([]NotebookCell, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, notebook_id, kind, language, source, output, output_json, status, created_at, updated_at FROM notebook_cells WHERE notebook_id = ? ORDER BY created_at ASC`, notebookID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]NotebookCell, 0)
	for rows.Next() {
		var cell NotebookCell
		if err := rows.Scan(&cell.ID, &cell.NotebookID, &cell.Kind, &cell.Language, &cell.Source, &cell.Output, &cell.OutputJSON, &cell.Status, &cell.CreatedAt, &cell.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, cell)
	}
	return result, rows.Err()
}

func (s *Store) GetNotebookCell(ctx context.Context, notebookID, cellID string) (NotebookCell, error) {
	var cell NotebookCell
	err := s.db.QueryRowContext(ctx, `SELECT id, notebook_id, kind, language, source, output, output_json, status, created_at, updated_at FROM notebook_cells WHERE notebook_id = ? AND id = ?`, notebookID, cellID).
		Scan(&cell.ID, &cell.NotebookID, &cell.Kind, &cell.Language, &cell.Source, &cell.Output, &cell.OutputJSON, &cell.Status, &cell.CreatedAt, &cell.UpdatedAt)
	return cell, err
}

func (s *Store) UpdateNotebookCellOutput(ctx context.Context, cell NotebookCell) (NotebookCell, error) {
	cell.UpdatedAt = unixMillis()
	if cell.OutputJSON == "" {
		cell.OutputJSON = "[]"
	}
	_, err := s.db.ExecContext(ctx, `UPDATE notebook_cells SET output = ?, output_json = ?, status = ?, updated_at = ? WHERE id = ? AND notebook_id = ?`,
		cell.Output, cell.OutputJSON, cell.Status, cell.UpdatedAt, cell.ID, cell.NotebookID)
	return cell, err
}

func (s *Store) UpdateNotebookCellStatus(ctx context.Context, notebookID, cellID, status string) (NotebookCell, error) {
	cell, err := s.GetNotebookCell(ctx, notebookID, cellID)
	if err != nil {
		return NotebookCell{}, err
	}
	cell.Status = status
	cell.UpdatedAt = unixMillis()
	_, err = s.db.ExecContext(ctx, `UPDATE notebook_cells SET status = ?, updated_at = ? WHERE id = ? AND notebook_id = ?`,
		cell.Status, cell.UpdatedAt, cell.ID, cell.NotebookID)
	return cell, err
}

func (s *Store) CreateArtifact(ctx context.Context, artifact Artifact) (Artifact, error) {
	now := unixMillis()
	if artifact.ID == "" {
		artifact.ID = newID("artifact")
	}
	if artifact.Type == "" {
		artifact.Type = "markdown"
	}
	if artifact.Title == "" {
		artifact.Title = artifact.ID
	}
	if artifact.Metadata == "" {
		artifact.Metadata = "{}"
	}
	artifact.CreatedAt = now
	_, err := s.db.ExecContext(ctx, `INSERT INTO artifacts (id, session_id, type, title, content, metadata, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		artifact.ID, artifact.SessionID, artifact.Type, artifact.Title, artifact.Content, artifact.Metadata, artifact.CreatedAt)
	return artifact, err
}

func (s *Store) ListArtifacts(ctx context.Context) ([]Artifact, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, session_id, type, title, content, metadata, created_at FROM artifacts ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Artifact, 0)
	for rows.Next() {
		var artifact Artifact
		if err := rows.Scan(&artifact.ID, &artifact.SessionID, &artifact.Type, &artifact.Title, &artifact.Content, &artifact.Metadata, &artifact.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, artifact)
	}
	return result, rows.Err()
}

func (s *Store) ListArtifactsBySession(ctx context.Context, sessionID string) ([]Artifact, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, session_id, type, title, content, metadata, created_at FROM artifacts WHERE session_id = ? ORDER BY created_at DESC`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Artifact, 0)
	for rows.Next() {
		var artifact Artifact
		if err := rows.Scan(&artifact.ID, &artifact.SessionID, &artifact.Type, &artifact.Title, &artifact.Content, &artifact.Metadata, &artifact.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, artifact)
	}
	return result, rows.Err()
}

func (s *Store) GetArtifact(ctx context.Context, id string) (Artifact, error) {
	var artifact Artifact
	err := s.db.QueryRowContext(ctx, `SELECT id, session_id, type, title, content, metadata, created_at FROM artifacts WHERE id = ?`, id).
		Scan(&artifact.ID, &artifact.SessionID, &artifact.Type, &artifact.Title, &artifact.Content, &artifact.Metadata, &artifact.CreatedAt)
	return artifact, err
}

func (s *Store) DeleteArtifact(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM artifacts WHERE id = ?`, id)
	return err
}

func (s *Store) CreateProvenanceEdge(ctx context.Context, edge ProvenanceEdge) (ProvenanceEdge, error) {
	if edge.ID == "" {
		edge.ID = newID("prov")
	}
	if edge.Relation == "" {
		edge.Relation = "derived_from"
	}
	if edge.Metadata == "" {
		edge.Metadata = "{}"
	}
	edge.CreatedAt = unixMillis()
	_, err := s.db.ExecContext(ctx, `INSERT INTO provenance_edges (id, source_id, source_type, target_id, target_type, relation, metadata, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		edge.ID, edge.SourceID, edge.SourceType, edge.TargetID, edge.TargetType, edge.Relation, edge.Metadata, edge.CreatedAt)
	return edge, err
}

func (s *Store) ListProvenance(ctx context.Context, entityID string) ([]ProvenanceEdge, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, source_id, source_type, target_id, target_type, relation, metadata, created_at FROM provenance_edges WHERE source_id = ? OR target_id = ? ORDER BY created_at DESC`, entityID, entityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]ProvenanceEdge, 0)
	for rows.Next() {
		var edge ProvenanceEdge
		if err := rows.Scan(&edge.ID, &edge.SourceID, &edge.SourceType, &edge.TargetID, &edge.TargetType, &edge.Relation, &edge.Metadata, &edge.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, edge)
	}
	return result, rows.Err()
}

func (s *Store) CreateShare(ctx context.Context, share Share) (Share, error) {
	now := unixMillis()
	if share.ID == "" {
		share.ID = newID("share")
	}
	if share.Title == "" {
		share.Title = "Superscience share"
	}
	if share.Visibility == "" {
		share.Visibility = "private-link"
	}
	if share.Status == "" {
		share.Status = "published"
	}
	share.CreatedAt = now
	share.UpdatedAt = now
	_, err := s.db.ExecContext(ctx, `INSERT INTO shares (id, title, visibility, status, url, bundle, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		share.ID, share.Title, share.Visibility, share.Status, share.URL, share.Bundle, share.CreatedAt, share.UpdatedAt)
	return share, err
}

// ImportShare atomically restores portable artifacts, their share record, and provenance edges.
func (s *Store) ImportShare(ctx context.Context, share Share, artifacts []Artifact) (Share, []Artifact, error) {
	if len(artifacts) == 0 {
		return Share{}, nil, errors.New("share import requires at least one artifact")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Share{}, nil, err
	}
	defer tx.Rollback()
	now := unixMillis()
	created := make([]Artifact, 0, len(artifacts))
	for _, artifact := range artifacts {
		if artifact.ID == "" {
			artifact.ID = newID("artifact")
		}
		if artifact.Type == "" {
			artifact.Type = "markdown"
		}
		if artifact.Title == "" {
			artifact.Title = artifact.ID
		}
		if artifact.Metadata == "" {
			artifact.Metadata = "{}"
		}
		artifact.CreatedAt = now
		if _, err := tx.ExecContext(ctx, `INSERT INTO artifacts (id, session_id, type, title, content, metadata, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			artifact.ID, artifact.SessionID, artifact.Type, artifact.Title, artifact.Content, artifact.Metadata, artifact.CreatedAt); err != nil {
			return Share{}, nil, err
		}
		created = append(created, artifact)
	}
	if share.ID == "" {
		share.ID = newID("share")
	}
	if share.Title == "" {
		share.Title = "Superscience share"
	}
	if share.Visibility == "" {
		share.Visibility = "private-link"
	}
	if share.Status == "" {
		share.Status = "published"
	}
	share.CreatedAt = now
	share.UpdatedAt = now
	if _, err := tx.ExecContext(ctx, `INSERT INTO shares (id, title, visibility, status, url, bundle, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		share.ID, share.Title, share.Visibility, share.Status, share.URL, share.Bundle, share.CreatedAt, share.UpdatedAt); err != nil {
		return Share{}, nil, err
	}
	for _, artifact := range created {
		if _, err := tx.ExecContext(ctx, `INSERT INTO provenance_edges (id, source_id, source_type, target_id, target_type, relation, metadata, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			newID("prov"), artifact.ID, "artifact", share.ID, "share", "imported", "{}", now); err != nil {
			return Share{}, nil, err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_events (id, actor, action, entity_id, entity_type, metadata, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		newID("audit"), "local", "bundle.import", share.ID, "share", "{}", now); err != nil {
		return Share{}, nil, err
	}
	if err := tx.Commit(); err != nil {
		return Share{}, nil, err
	}
	return share, created, nil
}

func (s *Store) GetShare(ctx context.Context, id string) (Share, error) {
	var share Share
	err := s.db.QueryRowContext(ctx, `SELECT id, title, visibility, status, url, bundle, created_at, updated_at FROM shares WHERE id = ?`, id).
		Scan(&share.ID, &share.Title, &share.Visibility, &share.Status, &share.URL, &share.Bundle, &share.CreatedAt, &share.UpdatedAt)
	return share, err
}

func (s *Store) ListShares(ctx context.Context) ([]Share, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, title, visibility, status, url, bundle, created_at, updated_at FROM shares ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Share, 0)
	for rows.Next() {
		var share Share
		if err := rows.Scan(&share.ID, &share.Title, &share.Visibility, &share.Status, &share.URL, &share.Bundle, &share.CreatedAt, &share.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, share)
	}
	return result, rows.Err()
}

func (s *Store) CreateAuditEvent(ctx context.Context, event AuditEvent) (AuditEvent, error) {
	if event.ID == "" {
		event.ID = newID("audit")
	}
	if event.Actor == "" {
		event.Actor = "local"
	}
	if event.Metadata == "" {
		event.Metadata = "{}"
	}
	event.CreatedAt = unixMillis()
	_, err := s.db.ExecContext(ctx, `INSERT INTO audit_events (id, actor, action, entity_id, entity_type, metadata, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		event.ID, event.Actor, event.Action, event.EntityID, event.EntityType, event.Metadata, event.CreatedAt)
	return event, err
}

func (s *Store) ListAuditEvents(ctx context.Context) ([]AuditEvent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, actor, action, entity_id, entity_type, metadata, created_at FROM audit_events ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]AuditEvent, 0)
	for rows.Next() {
		var event AuditEvent
		if err := rows.Scan(&event.ID, &event.Actor, &event.Action, &event.EntityID, &event.EntityType, &event.Metadata, &event.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, event)
	}
	return result, rows.Err()
}

func (s *Store) CreateAgentRun(ctx context.Context, run AgentRun) (AgentRun, error) {
	now := unixMillis()
	if run.ID == "" {
		run.ID = newID("run")
	}
	if run.Agent == "" {
		run.Agent = "research"
	}
	if run.Status == "" {
		run.Status = "running"
	}
	run.CreatedAt = now
	run.UpdatedAt = now
	_, err := s.db.ExecContext(ctx, `INSERT INTO agent_runs (id, session_id, agent, status, goal, model, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		run.ID, run.SessionID, run.Agent, run.Status, run.Goal, run.Model, run.CreatedAt, run.UpdatedAt)
	return run, err
}

func (s *Store) UpdateAgentRunStatus(ctx context.Context, id, status string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE agent_runs SET status = ?, updated_at = ? WHERE id = ?`, status, unixMillis(), id)
	return err
}

func (s *Store) GetAgentRun(ctx context.Context, id string) (AgentRun, error) {
	var run AgentRun
	err := s.db.QueryRowContext(ctx, `SELECT id, session_id, agent, status, goal, model, created_at, updated_at FROM agent_runs WHERE id = ?`, id).
		Scan(&run.ID, &run.SessionID, &run.Agent, &run.Status, &run.Goal, &run.Model, &run.CreatedAt, &run.UpdatedAt)
	return run, err
}

func (s *Store) ListAgentRuns(ctx context.Context, sessionID string) ([]AgentRun, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, session_id, agent, status, goal, model, created_at, updated_at FROM agent_runs WHERE session_id = ? ORDER BY created_at DESC`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]AgentRun, 0)
	for rows.Next() {
		var run AgentRun
		if err := rows.Scan(&run.ID, &run.SessionID, &run.Agent, &run.Status, &run.Goal, &run.Model, &run.CreatedAt, &run.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, run)
	}
	return result, rows.Err()
}

func (s *Store) AddAgentStep(ctx context.Context, step AgentStep) (AgentStep, error) {
	if step.ID == "" {
		step.ID = newID("step")
	}
	if step.Status == "" {
		step.Status = "ok"
	}
	if step.Metadata == "" {
		step.Metadata = "{}"
	}
	step.CreatedAt = unixMillis()
	_, err := s.db.ExecContext(ctx, `INSERT INTO agent_steps (id, run_id, kind, status, title, content, metadata, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		step.ID, step.RunID, step.Kind, step.Status, step.Title, step.Content, step.Metadata, step.CreatedAt)
	return step, err
}

func (s *Store) ListAgentSteps(ctx context.Context, runID string) ([]AgentStep, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, run_id, kind, status, title, content, metadata, created_at FROM agent_steps WHERE run_id = ? ORDER BY created_at ASC`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]AgentStep, 0)
	for rows.Next() {
		var step AgentStep
		if err := rows.Scan(&step.ID, &step.RunID, &step.Kind, &step.Status, &step.Title, &step.Content, &step.Metadata, &step.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, step)
	}
	return result, rows.Err()
}

func (s *Store) CreateToolCall(ctx context.Context, call ToolCall) (ToolCall, error) {
	now := unixMillis()
	if call.ID == "" {
		call.ID = newID("tool")
	}
	if call.Status == "" {
		call.Status = "ok"
	}
	if call.Input == "" {
		call.Input = "{}"
	}
	if call.Data == "" {
		call.Data = "{}"
	}
	call.CreatedAt = now
	call.UpdatedAt = now
	_, err := s.db.ExecContext(ctx, `INSERT INTO tool_calls (id, run_id, session_id, tool, status, input, output, data, permission_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		call.ID, call.RunID, call.SessionID, call.Tool, call.Status, call.Input, call.Output, call.Data, call.PermissionID, call.CreatedAt, call.UpdatedAt)
	return call, err
}

func (s *Store) ListToolCalls(ctx context.Context, runID string) ([]ToolCall, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, run_id, session_id, tool, status, input, output, data, permission_id, created_at, updated_at FROM tool_calls WHERE run_id = ? ORDER BY created_at ASC`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]ToolCall, 0)
	for rows.Next() {
		var call ToolCall
		if err := rows.Scan(&call.ID, &call.RunID, &call.SessionID, &call.Tool, &call.Status, &call.Input, &call.Output, &call.Data, &call.PermissionID, &call.CreatedAt, &call.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, call)
	}
	return result, rows.Err()
}

func (s *Store) UpdateToolCall(ctx context.Context, id, status, output, data, permissionID string) (ToolCall, error) {
	now := unixMillis()
	if data == "" {
		data = "{}"
	}
	_, err := s.db.ExecContext(ctx, `UPDATE tool_calls SET status = ?, output = ?, data = ?, permission_id = ?, updated_at = ? WHERE id = ?`, status, output, data, permissionID, now, id)
	if err != nil {
		return ToolCall{}, err
	}
	var call ToolCall
	err = s.db.QueryRowContext(ctx, `SELECT id, run_id, session_id, tool, status, input, output, data, permission_id, created_at, updated_at FROM tool_calls WHERE id = ?`, id).
		Scan(&call.ID, &call.RunID, &call.SessionID, &call.Tool, &call.Status, &call.Input, &call.Output, &call.Data, &call.PermissionID, &call.CreatedAt, &call.UpdatedAt)
	return call, err
}

func (s *Store) CreatePermission(ctx context.Context, permission PermissionRecord) (PermissionRecord, error) {
	now := unixMillis()
	if permission.ID == "" {
		permission.ID = newID("permission")
	}
	if permission.Status == "" {
		permission.Status = "pending"
	}
	permission.CreatedAt = now
	permission.UpdatedAt = now
	_, err := s.db.ExecContext(ctx, `INSERT INTO permissions (id, session_id, type, pattern, message, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		permission.ID, permission.SessionID, permission.Type, permission.Pattern, permission.Message, permission.Status, permission.CreatedAt, permission.UpdatedAt)
	return permission, err
}

func (s *Store) GetPermission(ctx context.Context, id string) (PermissionRecord, error) {
	var permission PermissionRecord
	err := s.db.QueryRowContext(ctx, `SELECT id, session_id, type, pattern, message, status, created_at, updated_at FROM permissions WHERE id = ?`, id).
		Scan(&permission.ID, &permission.SessionID, &permission.Type, &permission.Pattern, &permission.Message, &permission.Status, &permission.CreatedAt, &permission.UpdatedAt)
	return permission, err
}

func (s *Store) UpdatePermissionStatus(ctx context.Context, id, status string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE permissions SET status = ?, updated_at = ? WHERE id = ?`, status, unixMillis(), id)
	return err
}

func (s *Store) ListPendingPermissions(ctx context.Context) ([]PermissionRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, session_id, type, pattern, message, status, created_at, updated_at FROM permissions WHERE status = 'pending' ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]PermissionRecord, 0)
	for rows.Next() {
		var permission PermissionRecord
		if err := rows.Scan(&permission.ID, &permission.SessionID, &permission.Type, &permission.Pattern, &permission.Message, &permission.Status, &permission.CreatedAt, &permission.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, permission)
	}
	return result, rows.Err()
}

func (s *Store) ConsumePermissionDecision(ctx context.Context, sessionID, typ, pattern string) (string, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", false, err
	}
	defer tx.Rollback()
	var id, status string
	err = tx.QueryRowContext(ctx, `SELECT id, status FROM permissions
WHERE session_id = ? AND type = ? AND pattern = ? AND status IN ('once', 'always')
ORDER BY updated_at DESC LIMIT 1`, sessionID, typ, pattern).Scan(&id, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if status == "once" {
		if _, err := tx.ExecContext(ctx, `UPDATE permissions SET status = 'consumed', updated_at = ? WHERE id = ? AND status = 'once'`, unixMillis(), id); err != nil {
			return "", false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return "", false, err
	}
	return status, true, nil
}

func (s *Store) ConsumePermissionByID(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE permissions SET status = 'consumed', updated_at = ? WHERE id = ? AND status = 'once'`, unixMillis(), id)
	return err
}

func (s *Store) UpsertRunCheckpoint(ctx context.Context, checkpoint RunCheckpoint) (RunCheckpoint, error) {
	now := unixMillis()
	if checkpoint.Status == "" {
		checkpoint.Status = "waiting_permission"
	}
	if checkpoint.State == "" {
		checkpoint.State = "{}"
	}
	checkpoint.CreatedAt = now
	checkpoint.UpdatedAt = now
	_, err := s.db.ExecContext(ctx, `INSERT INTO run_checkpoints (run_id, session_id, permission_id, tool_call_id, state, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(run_id) DO UPDATE SET session_id = excluded.session_id, permission_id = excluded.permission_id, tool_call_id = excluded.tool_call_id, state = excluded.state, status = excluded.status, updated_at = excluded.updated_at`,
		checkpoint.RunID, checkpoint.SessionID, checkpoint.PermissionID, checkpoint.ToolCallID, checkpoint.State, checkpoint.Status, checkpoint.CreatedAt, checkpoint.UpdatedAt)
	return checkpoint, err
}

func (s *Store) GetRunCheckpoint(ctx context.Context, runID string) (RunCheckpoint, error) {
	var checkpoint RunCheckpoint
	err := s.db.QueryRowContext(ctx, `SELECT run_id, session_id, permission_id, tool_call_id, state, status, created_at, updated_at FROM run_checkpoints WHERE run_id = ?`, runID).
		Scan(&checkpoint.RunID, &checkpoint.SessionID, &checkpoint.PermissionID, &checkpoint.ToolCallID, &checkpoint.State, &checkpoint.Status, &checkpoint.CreatedAt, &checkpoint.UpdatedAt)
	return checkpoint, err
}

func (s *Store) GetRunCheckpointByPermission(ctx context.Context, permissionID string) (RunCheckpoint, error) {
	var checkpoint RunCheckpoint
	err := s.db.QueryRowContext(ctx, `SELECT run_id, session_id, permission_id, tool_call_id, state, status, created_at, updated_at FROM run_checkpoints WHERE permission_id = ?`, permissionID).
		Scan(&checkpoint.RunID, &checkpoint.SessionID, &checkpoint.PermissionID, &checkpoint.ToolCallID, &checkpoint.State, &checkpoint.Status, &checkpoint.CreatedAt, &checkpoint.UpdatedAt)
	return checkpoint, err
}

func (s *Store) DeleteRunCheckpoint(ctx context.Context, runID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM run_checkpoints WHERE run_id = ?`, runID)
	return err
}

func (s *Store) CreatePendingAction(ctx context.Context, action PendingAction) (PendingAction, error) {
	now := unixMillis()
	if action.ID == "" {
		action.ID = newID("action")
	}
	if action.Kind == "" {
		action.Kind = "question"
	}
	if action.Status == "" {
		action.Status = "waiting_input"
	}
	if action.Payload == "" {
		action.Payload = "{}"
	}
	action.CreatedAt = now
	action.UpdatedAt = now
	_, err := s.db.ExecContext(ctx, `INSERT INTO pending_actions (id, run_id, session_id, kind, payload, response, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		action.ID, action.RunID, action.SessionID, action.Kind, action.Payload, action.Response, action.Status, action.CreatedAt, action.UpdatedAt)
	return action, err
}

func (s *Store) GetPendingAction(ctx context.Context, id string) (PendingAction, error) {
	var action PendingAction
	err := s.db.QueryRowContext(ctx, `SELECT id, run_id, session_id, kind, payload, response, status, created_at, updated_at FROM pending_actions WHERE id = ?`, id).
		Scan(&action.ID, &action.RunID, &action.SessionID, &action.Kind, &action.Payload, &action.Response, &action.Status, &action.CreatedAt, &action.UpdatedAt)
	return action, err
}

func (s *Store) GetPendingActionByRun(ctx context.Context, runID string) (PendingAction, error) {
	var action PendingAction
	err := s.db.QueryRowContext(ctx, `SELECT id, run_id, session_id, kind, payload, response, status, created_at, updated_at FROM pending_actions WHERE run_id = ? ORDER BY created_at DESC LIMIT 1`, runID).
		Scan(&action.ID, &action.RunID, &action.SessionID, &action.Kind, &action.Payload, &action.Response, &action.Status, &action.CreatedAt, &action.UpdatedAt)
	return action, err
}

func (s *Store) AnswerPendingAction(ctx context.Context, id, response string) (PendingAction, error) {
	_, err := s.db.ExecContext(ctx, `UPDATE pending_actions SET response = ?, status = 'answered', updated_at = ? WHERE id = ? AND status = 'waiting_input'`, response, unixMillis(), id)
	if err != nil {
		return PendingAction{}, err
	}
	return s.GetPendingAction(ctx, id)
}

func mustJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return "null"
	}
	return string(data)
}

func slugOrID(id, name, prefix string) string {
	id = strings.TrimSpace(id)
	if id != "" {
		return id
	}
	slug := strings.ToLower(strings.TrimSpace(name))
	slug = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '-' || r == '_':
			return r
		case r == ' ' || r == '/' || r == '.':
			return '-'
		default:
			return -1
		}
	}, slug)
	slug = strings.Trim(slug, "-_")
	if slug == "" {
		return newID(prefix)
	}
	return slug
}

func newID(prefix string) string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return prefix + "_" + hex.EncodeToString([]byte(time.Now().Format("150405.000000000")))
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}

func unixMillis() int64 {
	return time.Now().UnixMilli()
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
