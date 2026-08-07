package cloudpostgres

const schema = `
CREATE TABLE IF NOT EXISTS cloud_schema_migrations (
  version BIGINT PRIMARY KEY,
  applied_at BIGINT NOT NULL
);

CREATE TABLE IF NOT EXISTS projects (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  path TEXT NOT NULL UNIQUE,
  pinned BOOLEAN NOT NULL DEFAULT FALSE,
  created_at BIGINT NOT NULL,
  updated_at BIGINT NOT NULL
);

CREATE TABLE IF NOT EXISTS artifacts (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL DEFAULT '',
  type TEXT NOT NULL,
  title TEXT NOT NULL,
  content TEXT NOT NULL DEFAULT '',
  metadata TEXT NOT NULL DEFAULT '{}',
  created_at BIGINT NOT NULL
);

CREATE TABLE IF NOT EXISTS provenance_edges (
  id TEXT PRIMARY KEY,
  source_id TEXT NOT NULL,
  source_type TEXT NOT NULL,
  target_id TEXT NOT NULL,
  target_type TEXT NOT NULL,
  relation TEXT NOT NULL,
  metadata TEXT NOT NULL DEFAULT '{}',
  created_at BIGINT NOT NULL
);

CREATE TABLE IF NOT EXISTS shares (
  id TEXT PRIMARY KEY,
  title TEXT NOT NULL,
  visibility TEXT NOT NULL,
  status TEXT NOT NULL,
  url TEXT NOT NULL DEFAULT '',
  bundle TEXT NOT NULL,
  created_at BIGINT NOT NULL,
  updated_at BIGINT NOT NULL
);

CREATE TABLE IF NOT EXISTS audit_events (
  id TEXT PRIMARY KEY,
  actor TEXT NOT NULL,
  action TEXT NOT NULL,
  entity_id TEXT NOT NULL,
  entity_type TEXT NOT NULL,
  metadata TEXT NOT NULL DEFAULT '{}',
  created_at BIGINT NOT NULL
);

CREATE TABLE IF NOT EXISTS cloud_users (
  id TEXT PRIMARY KEY,
  actor TEXT NOT NULL UNIQUE,
  display_name TEXT NOT NULL,
  created_at BIGINT NOT NULL,
  updated_at BIGINT NOT NULL
);

CREATE TABLE IF NOT EXISTS cloud_orgs (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  created_at BIGINT NOT NULL,
  updated_at BIGINT NOT NULL
);

CREATE TABLE IF NOT EXISTS cloud_org_members (
  org_id TEXT NOT NULL REFERENCES cloud_orgs(id) ON DELETE CASCADE,
  user_id TEXT NOT NULL REFERENCES cloud_users(id) ON DELETE CASCADE,
  role TEXT NOT NULL,
  created_at BIGINT NOT NULL,
  PRIMARY KEY (org_id, user_id)
);

CREATE TABLE IF NOT EXISTS cloud_devices (
  id TEXT PRIMARY KEY,
  org_id TEXT NOT NULL REFERENCES cloud_orgs(id) ON DELETE CASCADE,
  user_id TEXT NOT NULL REFERENCES cloud_users(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  mode TEXT NOT NULL,
  last_seen_at BIGINT NOT NULL,
  created_at BIGINT NOT NULL,
  updated_at BIGINT NOT NULL
);

CREATE TABLE IF NOT EXISTS cloud_auth_sessions (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES cloud_users(id) ON DELETE CASCADE,
  device_id TEXT NOT NULL REFERENCES cloud_devices(id) ON DELETE CASCADE,
  token_hash TEXT NOT NULL UNIQUE,
  expires_at BIGINT NOT NULL,
  revoked_at BIGINT NOT NULL DEFAULT 0,
  created_at BIGINT NOT NULL
);

CREATE TABLE IF NOT EXISTS cloud_resource_owners (
  entity_type TEXT NOT NULL,
  entity_id TEXT NOT NULL,
  org_id TEXT NOT NULL REFERENCES cloud_orgs(id) ON DELETE CASCADE,
  created_at BIGINT NOT NULL,
  PRIMARY KEY (entity_type, entity_id)
);

CREATE TABLE IF NOT EXISTS cloud_usage_ledger (
  id TEXT PRIMARY KEY,
  org_id TEXT NOT NULL REFERENCES cloud_orgs(id) ON DELETE CASCADE,
  provider TEXT NOT NULL DEFAULT '',
  model TEXT NOT NULL DEFAULT '',
  input_tokens BIGINT NOT NULL DEFAULT 0,
  output_tokens BIGINT NOT NULL DEFAULT 0,
  cost_micros BIGINT NOT NULL DEFAULT 0,
  credit_delta_micros BIGINT NOT NULL DEFAULT 0,
  metadata TEXT NOT NULL DEFAULT '{}',
  created_at BIGINT NOT NULL
);

CREATE TABLE IF NOT EXISTS cloud_share_links (
  id TEXT PRIMARY KEY,
  share_id TEXT NOT NULL,
  org_id TEXT NOT NULL REFERENCES cloud_orgs(id) ON DELETE CASCADE,
  token_hash TEXT NOT NULL UNIQUE,
  expires_at BIGINT NOT NULL DEFAULT 0,
  revoked_at BIGINT NOT NULL DEFAULT 0,
  created_at BIGINT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_cloud_members_user ON cloud_org_members(user_id);
CREATE INDEX IF NOT EXISTS idx_cloud_devices_org ON cloud_devices(org_id, created_at);
CREATE INDEX IF NOT EXISTS idx_cloud_auth_expiry ON cloud_auth_sessions(token_hash, expires_at) WHERE revoked_at = 0;
CREATE INDEX IF NOT EXISTS idx_cloud_owners_org ON cloud_resource_owners(org_id, entity_type);
CREATE INDEX IF NOT EXISTS idx_cloud_usage_org ON cloud_usage_ledger(org_id, created_at);
CREATE INDEX IF NOT EXISTS idx_cloud_share_links_token ON cloud_share_links(token_hash) WHERE revoked_at = 0;
CREATE INDEX IF NOT EXISTS idx_provenance_source ON provenance_edges(source_id, created_at);
CREATE INDEX IF NOT EXISTS idx_provenance_target ON provenance_edges(target_id, created_at);
CREATE INDEX IF NOT EXISTS idx_audit_entity ON audit_events(entity_type, entity_id, created_at);

INSERT INTO cloud_schema_migrations (version, applied_at)
VALUES (1, (EXTRACT(EPOCH FROM clock_timestamp()) * 1000)::BIGINT)
ON CONFLICT (version) DO NOTHING;
`
