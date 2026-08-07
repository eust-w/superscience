package cloudpostgres

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"superscience/internal/store"
)

const migrationLockID int64 = 6004798503988513860

type Store struct {
	db *sql.DB
}

func Open(ctx context.Context, databaseURL string) (*Store, error) {
	databaseURL = strings.TrimSpace(databaseURL)
	parsed, err := url.Parse(databaseURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
		return nil, errors.New("cloud database URL must be an absolute postgres:// or postgresql:// URL")
	}
	host := parsed.Hostname()
	loopback := strings.EqualFold(host, "localhost")
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		loopback = true
	}
	sslMode := strings.ToLower(strings.TrimSpace(parsed.Query().Get("sslmode")))
	if !loopback && sslMode != "require" && sslMode != "verify-ca" && sslMode != "verify-full" {
		return nil, errors.New("remote cloud PostgreSQL requires TLS via sslmode=require, verify-ca, or verify-full")
	}
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(5)
	db.SetConnMaxIdleTime(5 * time.Minute)
	db.SetConnMaxLifetime(time.Hour)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connect cloud PostgreSQL: %w", err)
	}
	result := &Store{db: db}
	if err := result.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate cloud PostgreSQL: %w", err)
	}
	return result, nil
}

func (s *Store) migrate(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, migrationLockID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, schema); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Close() error                   { return s.db.Close() }
func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

func (s *Store) EnsureCloudIdentity(ctx context.Context, actor, displayName string) (store.CloudUser, store.CloudOrganization, error) {
	actor = strings.TrimSpace(actor)
	displayName = strings.TrimSpace(displayName)
	if actor == "" {
		return store.CloudUser{}, store.CloudOrganization{}, errors.New("cloud actor is required")
	}
	if displayName == "" {
		displayName = actor
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.CloudUser{}, store.CloudOrganization{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, actor); err != nil {
		return store.CloudUser{}, store.CloudOrganization{}, err
	}
	now := unixMillis()
	user := store.CloudUser{ID: newID("user"), Actor: actor, DisplayName: displayName, CreatedAt: now, UpdatedAt: now}
	err = tx.QueryRowContext(ctx, `
INSERT INTO cloud_users (id, actor, display_name, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (actor) DO UPDATE SET display_name = EXCLUDED.display_name, updated_at = EXCLUDED.updated_at
RETURNING id, actor, display_name, created_at, updated_at`,
		user.ID, user.Actor, user.DisplayName, user.CreatedAt, user.UpdatedAt,
	).Scan(&user.ID, &user.Actor, &user.DisplayName, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		return store.CloudUser{}, store.CloudOrganization{}, err
	}
	var organization store.CloudOrganization
	err = tx.QueryRowContext(ctx, `
SELECT o.id, o.name, m.role, o.created_at, o.updated_at
FROM cloud_orgs o JOIN cloud_org_members m ON m.org_id = o.id
WHERE m.user_id = $1 ORDER BY o.created_at ASC LIMIT 1`, user.ID,
	).Scan(&organization.ID, &organization.Name, &organization.Role, &organization.CreatedAt, &organization.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		organization = store.CloudOrganization{
			ID: newID("org"), Name: displayName + " Research", Role: "owner", CreatedAt: now, UpdatedAt: now,
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO cloud_orgs (id, name, created_at, updated_at) VALUES ($1, $2, $3, $4)`,
			organization.ID, organization.Name, now, now); err != nil {
			return store.CloudUser{}, store.CloudOrganization{}, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO cloud_org_members (org_id, user_id, role, created_at) VALUES ($1, $2, $3, $4)`,
			organization.ID, user.ID, organization.Role, now); err != nil {
			return store.CloudUser{}, store.CloudOrganization{}, err
		}
	} else if err != nil {
		return store.CloudUser{}, store.CloudOrganization{}, err
	}
	if err := tx.Commit(); err != nil {
		return store.CloudUser{}, store.CloudOrganization{}, err
	}
	return user, organization, nil
}

func (s *Store) ListCloudOrganizations(ctx context.Context, userID string) ([]store.CloudOrganization, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT o.id, o.name, m.role, o.created_at, o.updated_at
FROM cloud_orgs o JOIN cloud_org_members m ON m.org_id = o.id
WHERE m.user_id = $1 ORDER BY o.created_at ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []store.CloudOrganization{}
	for rows.Next() {
		var organization store.CloudOrganization
		if err := rows.Scan(&organization.ID, &organization.Name, &organization.Role, &organization.CreatedAt, &organization.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, organization)
	}
	return result, rows.Err()
}

func (s *Store) CreateCloudDevice(ctx context.Context, userID, orgID, name string) (store.CloudDevice, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Superscience client"
	}
	now := unixMillis()
	device := store.CloudDevice{
		ID: newID("device"), OrgID: orgID, UserID: userID, Name: name, Mode: "self-hosted", Status: "online",
		LastSeenAt: now, CreatedAt: now, UpdatedAt: now,
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO cloud_devices (id, org_id, user_id, name, mode, last_seen_at, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		device.ID, device.OrgID, device.UserID, device.Name, device.Mode, device.LastSeenAt, device.CreatedAt, device.UpdatedAt)
	return device, err
}

func (s *Store) TouchCloudDevice(ctx context.Context, id string) error {
	now := unixMillis()
	_, err := s.db.ExecContext(ctx, `UPDATE cloud_devices SET last_seen_at = $1, updated_at = $1 WHERE id = $2`, now, id)
	return err
}

func (s *Store) ListCloudDevices(ctx context.Context, orgID string) ([]store.CloudDevice, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, org_id, user_id, name, mode, last_seen_at, created_at, updated_at
FROM cloud_devices WHERE org_id = $1 ORDER BY created_at ASC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []store.CloudDevice{}
	cutoff := time.Now().Add(-2 * time.Minute).UnixMilli()
	for rows.Next() {
		var device store.CloudDevice
		if err := rows.Scan(&device.ID, &device.OrgID, &device.UserID, &device.Name, &device.Mode, &device.LastSeenAt, &device.CreatedAt, &device.UpdatedAt); err != nil {
			return nil, err
		}
		device.Status = "offline"
		if device.LastSeenAt >= cutoff {
			device.Status = "online"
		}
		result = append(result, device)
	}
	return result, rows.Err()
}

func (s *Store) CreateCloudAuthSession(ctx context.Context, userID, deviceID, tokenHash string, expiresAt int64) (store.CloudAuthSession, error) {
	if strings.TrimSpace(tokenHash) == "" {
		return store.CloudAuthSession{}, errors.New("token hash is required")
	}
	session := store.CloudAuthSession{
		ID: newID("auth"), UserID: userID, DeviceID: deviceID, TokenHash: tokenHash, ExpiresAt: expiresAt, CreatedAt: unixMillis(),
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO cloud_auth_sessions (id, user_id, device_id, token_hash, expires_at, created_at)
VALUES ($1, $2, $3, $4, $5, $6)`,
		session.ID, session.UserID, session.DeviceID, session.TokenHash, session.ExpiresAt, session.CreatedAt)
	return session, err
}

func (s *Store) GetCloudAuthSession(ctx context.Context, tokenHash string) (store.CloudAuthSession, error) {
	var session store.CloudAuthSession
	err := s.db.QueryRowContext(ctx, `
SELECT s.id, s.user_id, u.actor, u.display_name, s.device_id, s.token_hash, s.expires_at, s.created_at
FROM cloud_auth_sessions s JOIN cloud_users u ON u.id = s.user_id
WHERE s.token_hash = $1 AND s.revoked_at = 0 AND s.expires_at > $2`, tokenHash, unixMillis()).
		Scan(&session.ID, &session.UserID, &session.Actor, &session.DisplayName, &session.DeviceID, &session.TokenHash, &session.ExpiresAt, &session.CreatedAt)
	return session, err
}

func (s *Store) RevokeCloudAuthSession(ctx context.Context, tokenHash string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE cloud_auth_sessions SET revoked_at = $1 WHERE token_hash = $2 AND revoked_at = 0`, unixMillis(), tokenHash)
	return affected(result, err)
}

func (s *Store) SetCloudResourceOwner(ctx context.Context, orgID, entityType, entityID string) error {
	if orgID == "" || entityType == "" || entityID == "" {
		return errors.New("org id, entity type, and entity id are required")
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO cloud_resource_owners (entity_type, entity_id, org_id, created_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (entity_type, entity_id) DO UPDATE SET org_id = EXCLUDED.org_id`, entityType, entityID, orgID, unixMillis())
	return err
}

func (s *Store) CloudOwnsResource(ctx context.Context, orgID, entityType, entityID string) (bool, error) {
	var found int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM cloud_resource_owners WHERE org_id = $1 AND entity_type = $2 AND entity_id = $3`, orgID, entityType, entityID).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil && found == 1, err
}

func (s *Store) ListCloudOwnedResourceIDs(ctx context.Context, orgID, entityType string) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT entity_id FROM cloud_resource_owners WHERE org_id = $1 AND entity_type = $2`, orgID, entityType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		result[id] = true
	}
	return result, rows.Err()
}

func (s *Store) CreateCloudUsageRecord(ctx context.Context, record store.CloudUsageRecord) (store.CloudUsageRecord, error) {
	if record.OrgID == "" {
		return store.CloudUsageRecord{}, errors.New("usage organization is required")
	}
	if record.ID == "" {
		record.ID = newID("usage")
	}
	if record.Metadata == "" {
		record.Metadata = "{}"
	}
	if record.CreatedAt == 0 {
		record.CreatedAt = unixMillis()
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO cloud_usage_ledger (id, org_id, provider, model, input_tokens, output_tokens, cost_micros, credit_delta_micros, metadata, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		record.ID, record.OrgID, record.Provider, record.Model, record.InputTokens, record.OutputTokens,
		record.CostMicros, record.CreditDeltaMicros, record.Metadata, record.CreatedAt)
	return record, err
}

func (s *Store) CloudUsageSummary(ctx context.Context, orgID string) (store.CloudUsageSummary, error) {
	var summary store.CloudUsageSummary
	err := s.db.QueryRowContext(ctx, `
SELECT COALESCE(SUM(input_tokens), 0), COALESCE(SUM(output_tokens), 0), COALESCE(SUM(cost_micros), 0),
       COALESCE(SUM(credit_delta_micros), 0), COUNT(*)
FROM cloud_usage_ledger WHERE org_id = $1`, orgID).
		Scan(&summary.InputTokens, &summary.OutputTokens, &summary.CostMicros, &summary.CreditDeltaMicros, &summary.Records)
	return summary, err
}

func (s *Store) CreateCloudShareLink(ctx context.Context, orgID, shareID, tokenHash string, expiresAt int64) (store.CloudShareLink, error) {
	if orgID == "" || shareID == "" || tokenHash == "" {
		return store.CloudShareLink{}, errors.New("share link organization, share, and token hash are required")
	}
	link := store.CloudShareLink{
		ID: newID("link"), ShareID: shareID, OrgID: orgID, TokenHash: tokenHash, ExpiresAt: expiresAt, CreatedAt: unixMillis(),
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO cloud_share_links (id, share_id, org_id, token_hash, expires_at, created_at)
VALUES ($1, $2, $3, $4, $5, $6)`, link.ID, link.ShareID, link.OrgID, link.TokenHash, link.ExpiresAt, link.CreatedAt)
	return link, err
}

func (s *Store) GetCloudShareLinkByTokenHash(ctx context.Context, tokenHash string) (store.CloudShareLink, error) {
	var link store.CloudShareLink
	err := s.db.QueryRowContext(ctx, `
SELECT id, share_id, org_id, token_hash, expires_at, revoked_at, created_at
FROM cloud_share_links
WHERE token_hash = $1 AND revoked_at = 0 AND (expires_at = 0 OR expires_at > $2)`, tokenHash, unixMillis()).
		Scan(&link.ID, &link.ShareID, &link.OrgID, &link.TokenHash, &link.ExpiresAt, &link.RevokedAt, &link.CreatedAt)
	return link, err
}

func (s *Store) RevokeCloudShareLink(ctx context.Context, orgID, shareID, linkID string) error {
	result, err := s.db.ExecContext(ctx, `
UPDATE cloud_share_links SET revoked_at = $1
WHERE id = $2 AND org_id = $3 AND share_id = $4 AND revoked_at = 0`, unixMillis(), linkID, orgID, shareID)
	return affected(result, err)
}

func (s *Store) ListProjects(ctx context.Context) ([]store.Project, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, path, pinned, created_at, updated_at FROM projects ORDER BY pinned DESC, updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []store.Project{}
	for rows.Next() {
		var project store.Project
		if err := rows.Scan(&project.ID, &project.Name, &project.Path, &project.Pinned, &project.CreatedAt, &project.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, project)
	}
	return result, rows.Err()
}

func (s *Store) CreateArtifact(ctx context.Context, artifact store.Artifact) (store.Artifact, error) {
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
	artifact.CreatedAt = unixMillis()
	_, err := s.db.ExecContext(ctx, `
INSERT INTO artifacts (id, session_id, type, title, content, metadata, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		artifact.ID, artifact.SessionID, artifact.Type, artifact.Title, artifact.Content, artifact.Metadata, artifact.CreatedAt)
	return artifact, err
}

func (s *Store) GetArtifact(ctx context.Context, id string) (store.Artifact, error) {
	var artifact store.Artifact
	err := s.db.QueryRowContext(ctx, `SELECT id, session_id, type, title, content, metadata, created_at FROM artifacts WHERE id = $1`, id).
		Scan(&artifact.ID, &artifact.SessionID, &artifact.Type, &artifact.Title, &artifact.Content, &artifact.Metadata, &artifact.CreatedAt)
	return artifact, err
}

func (s *Store) CreateProvenanceEdge(ctx context.Context, edge store.ProvenanceEdge) (store.ProvenanceEdge, error) {
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
	_, err := s.db.ExecContext(ctx, `
INSERT INTO provenance_edges (id, source_id, source_type, target_id, target_type, relation, metadata, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		edge.ID, edge.SourceID, edge.SourceType, edge.TargetID, edge.TargetType, edge.Relation, edge.Metadata, edge.CreatedAt)
	return edge, err
}

func (s *Store) ListProvenance(ctx context.Context, entityID string) ([]store.ProvenanceEdge, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, source_id, source_type, target_id, target_type, relation, metadata, created_at
FROM provenance_edges WHERE source_id = $1 OR target_id = $1 ORDER BY created_at DESC`, entityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []store.ProvenanceEdge{}
	for rows.Next() {
		var edge store.ProvenanceEdge
		if err := rows.Scan(&edge.ID, &edge.SourceID, &edge.SourceType, &edge.TargetID, &edge.TargetType, &edge.Relation, &edge.Metadata, &edge.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, edge)
	}
	return result, rows.Err()
}

func (s *Store) CreateShare(ctx context.Context, share store.Share) (store.Share, error) {
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
	_, err := s.db.ExecContext(ctx, `
INSERT INTO shares (id, title, visibility, status, url, bundle, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		share.ID, share.Title, share.Visibility, share.Status, share.URL, share.Bundle, share.CreatedAt, share.UpdatedAt)
	return share, err
}

func (s *Store) GetShare(ctx context.Context, id string) (store.Share, error) {
	var share store.Share
	err := s.db.QueryRowContext(ctx, `SELECT id, title, visibility, status, url, bundle, created_at, updated_at FROM shares WHERE id = $1`, id).
		Scan(&share.ID, &share.Title, &share.Visibility, &share.Status, &share.URL, &share.Bundle, &share.CreatedAt, &share.UpdatedAt)
	return share, err
}

func (s *Store) ListShares(ctx context.Context) ([]store.Share, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, title, visibility, status, url, bundle, created_at, updated_at FROM shares ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []store.Share{}
	for rows.Next() {
		var share store.Share
		if err := rows.Scan(&share.ID, &share.Title, &share.Visibility, &share.Status, &share.URL, &share.Bundle, &share.CreatedAt, &share.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, share)
	}
	return result, rows.Err()
}

func (s *Store) CreateAuditEvent(ctx context.Context, event store.AuditEvent) (store.AuditEvent, error) {
	if event.ID == "" {
		event.ID = newID("audit")
	}
	if event.Actor == "" {
		event.Actor = "system"
	}
	if event.Metadata == "" {
		event.Metadata = "{}"
	}
	event.CreatedAt = unixMillis()
	_, err := s.db.ExecContext(ctx, `
INSERT INTO audit_events (id, actor, action, entity_id, entity_type, metadata, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		event.ID, event.Actor, event.Action, event.EntityID, event.EntityType, event.Metadata, event.CreatedAt)
	return event, err
}

func (s *Store) ListAuditEvents(ctx context.Context) ([]store.AuditEvent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, actor, action, entity_id, entity_type, metadata, created_at FROM audit_events ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []store.AuditEvent{}
	for rows.Next() {
		var event store.AuditEvent
		if err := rows.Scan(&event.ID, &event.Actor, &event.Action, &event.EntityID, &event.EntityType, &event.Metadata, &event.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, event)
	}
	return result, rows.Err()
}

func affected(result sql.Result, err error) error {
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

func unixMillis() int64 { return time.Now().UnixMilli() }

func newID(prefix string) string {
	random := make([]byte, 12)
	if _, err := rand.Read(random); err != nil {
		return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	}
	return prefix + "_" + hex.EncodeToString(random)
}
