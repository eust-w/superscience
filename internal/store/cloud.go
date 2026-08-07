package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

type CloudUser struct {
	ID          string `json:"id"`
	Actor       string `json:"actor"`
	DisplayName string `json:"displayName"`
	CreatedAt   int64  `json:"createdAt"`
	UpdatedAt   int64  `json:"updatedAt"`
}

type CloudOrganization struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
}

type CloudAuthSession struct {
	ID          string `json:"id"`
	UserID      string `json:"userId"`
	Actor       string `json:"actor"`
	DisplayName string `json:"displayName"`
	DeviceID    string `json:"deviceId"`
	TokenHash   string `json:"-"`
	ExpiresAt   int64  `json:"expiresAt"`
	CreatedAt   int64  `json:"createdAt"`
}

type CloudDevice struct {
	ID         string `json:"id"`
	OrgID      string `json:"orgId"`
	UserID     string `json:"userId"`
	Name       string `json:"name"`
	Mode       string `json:"mode"`
	Status     string `json:"status"`
	LastSeenAt int64  `json:"lastSeenAt"`
	CreatedAt  int64  `json:"createdAt"`
	UpdatedAt  int64  `json:"updatedAt"`
}

type CloudUsageRecord struct {
	ID                string `json:"id"`
	OrgID             string `json:"orgId"`
	Provider          string `json:"provider"`
	Model             string `json:"model"`
	InputTokens       int64  `json:"inputTokens"`
	OutputTokens      int64  `json:"outputTokens"`
	CostMicros        int64  `json:"costMicros"`
	CreditDeltaMicros int64  `json:"creditDeltaMicros"`
	Metadata          string `json:"metadata"`
	CreatedAt         int64  `json:"createdAt"`
}

type CloudUsageSummary struct {
	InputTokens       int64 `json:"inputTokens"`
	OutputTokens      int64 `json:"outputTokens"`
	CostMicros        int64 `json:"costMicros"`
	CreditDeltaMicros int64 `json:"creditDeltaMicros"`
	Records           int64 `json:"records"`
}

type CloudShareLink struct {
	ID        string `json:"id"`
	ShareID   string `json:"shareId"`
	OrgID     string `json:"orgId"`
	TokenHash string `json:"-"`
	ExpiresAt int64  `json:"expiresAt"`
	RevokedAt int64  `json:"revokedAt,omitempty"`
	CreatedAt int64  `json:"createdAt"`
}

func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *Store) EnsureCloudIdentity(ctx context.Context, actor, displayName string) (CloudUser, CloudOrganization, error) {
	actor = strings.TrimSpace(actor)
	displayName = strings.TrimSpace(displayName)
	if actor == "" {
		return CloudUser{}, CloudOrganization{}, errors.New("cloud actor is required")
	}
	if displayName == "" {
		displayName = actor
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CloudUser{}, CloudOrganization{}, err
	}
	defer tx.Rollback()
	now := unixMillis()
	var user CloudUser
	err = tx.QueryRowContext(ctx, `SELECT id, actor, display_name, created_at, updated_at FROM cloud_users WHERE actor = ?`, actor).
		Scan(&user.ID, &user.Actor, &user.DisplayName, &user.CreatedAt, &user.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		user = CloudUser{ID: newID("user"), Actor: actor, DisplayName: displayName, CreatedAt: now, UpdatedAt: now}
		if _, err = tx.ExecContext(ctx, `INSERT INTO cloud_users (id, actor, display_name, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
			user.ID, user.Actor, user.DisplayName, user.CreatedAt, user.UpdatedAt); err != nil {
			return CloudUser{}, CloudOrganization{}, err
		}
	} else if err != nil {
		return CloudUser{}, CloudOrganization{}, err
	} else if displayName != "" && displayName != user.DisplayName {
		user.DisplayName = displayName
		user.UpdatedAt = now
		if _, err = tx.ExecContext(ctx, `UPDATE cloud_users SET display_name = ?, updated_at = ? WHERE id = ?`, displayName, now, user.ID); err != nil {
			return CloudUser{}, CloudOrganization{}, err
		}
	}

	var org CloudOrganization
	err = tx.QueryRowContext(ctx, `SELECT o.id, o.name, m.role, o.created_at, o.updated_at
FROM cloud_orgs o JOIN cloud_org_members m ON m.org_id = o.id
WHERE m.user_id = ? ORDER BY o.created_at ASC LIMIT 1`, user.ID).
		Scan(&org.ID, &org.Name, &org.Role, &org.CreatedAt, &org.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		org = CloudOrganization{ID: newID("org"), Name: displayName + " Research", Role: "owner", CreatedAt: now, UpdatedAt: now}
		if _, err = tx.ExecContext(ctx, `INSERT INTO cloud_orgs (id, name, created_at, updated_at) VALUES (?, ?, ?, ?)`, org.ID, org.Name, now, now); err != nil {
			return CloudUser{}, CloudOrganization{}, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO cloud_org_members (org_id, user_id, role, created_at) VALUES (?, ?, ?, ?)`, org.ID, user.ID, org.Role, now); err != nil {
			return CloudUser{}, CloudOrganization{}, err
		}
	} else if err != nil {
		return CloudUser{}, CloudOrganization{}, err
	}
	if err := tx.Commit(); err != nil {
		return CloudUser{}, CloudOrganization{}, err
	}
	return user, org, nil
}

func (s *Store) ListCloudOrganizations(ctx context.Context, userID string) ([]CloudOrganization, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT o.id, o.name, m.role, o.created_at, o.updated_at
FROM cloud_orgs o JOIN cloud_org_members m ON m.org_id = o.id
WHERE m.user_id = ? ORDER BY o.created_at ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	organizations := []CloudOrganization{}
	for rows.Next() {
		var org CloudOrganization
		if err := rows.Scan(&org.ID, &org.Name, &org.Role, &org.CreatedAt, &org.UpdatedAt); err != nil {
			return nil, err
		}
		organizations = append(organizations, org)
	}
	return organizations, rows.Err()
}

func (s *Store) CreateCloudDevice(ctx context.Context, userID, orgID, name string) (CloudDevice, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Superscience client"
	}
	now := unixMillis()
	device := CloudDevice{
		ID: newID("device"), OrgID: orgID, UserID: userID, Name: name, Mode: "self-hosted", Status: "online",
		LastSeenAt: now, CreatedAt: now, UpdatedAt: now,
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO cloud_devices (id, org_id, user_id, name, mode, last_seen_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		device.ID, device.OrgID, device.UserID, device.Name, device.Mode, device.LastSeenAt, device.CreatedAt, device.UpdatedAt)
	return device, err
}

func (s *Store) TouchCloudDevice(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE cloud_devices SET last_seen_at = ?, updated_at = ? WHERE id = ?`, unixMillis(), unixMillis(), id)
	return err
}

func (s *Store) ListCloudDevices(ctx context.Context, orgID string) ([]CloudDevice, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, org_id, user_id, name, mode, last_seen_at, created_at, updated_at FROM cloud_devices WHERE org_id = ? ORDER BY created_at ASC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	devices := []CloudDevice{}
	cutoff := time.Now().Add(-2 * time.Minute).UnixMilli()
	for rows.Next() {
		var device CloudDevice
		if err := rows.Scan(&device.ID, &device.OrgID, &device.UserID, &device.Name, &device.Mode, &device.LastSeenAt, &device.CreatedAt, &device.UpdatedAt); err != nil {
			return nil, err
		}
		if device.LastSeenAt >= cutoff {
			device.Status = "online"
		} else {
			device.Status = "offline"
		}
		devices = append(devices, device)
	}
	return devices, rows.Err()
}

func (s *Store) CreateCloudAuthSession(ctx context.Context, userID, deviceID, tokenHash string, expiresAt int64) (CloudAuthSession, error) {
	if strings.TrimSpace(tokenHash) == "" {
		return CloudAuthSession{}, errors.New("token hash is required")
	}
	now := unixMillis()
	session := CloudAuthSession{ID: newID("auth"), UserID: userID, DeviceID: deviceID, TokenHash: tokenHash, ExpiresAt: expiresAt, CreatedAt: now}
	_, err := s.db.ExecContext(ctx, `INSERT INTO cloud_auth_sessions (id, user_id, device_id, token_hash, expires_at, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		session.ID, session.UserID, session.DeviceID, session.TokenHash, session.ExpiresAt, session.CreatedAt)
	return session, err
}

func (s *Store) GetCloudAuthSession(ctx context.Context, tokenHash string) (CloudAuthSession, error) {
	var session CloudAuthSession
	err := s.db.QueryRowContext(ctx, `SELECT s.id, s.user_id, u.actor, u.display_name, s.device_id, s.token_hash, s.expires_at, s.created_at
FROM cloud_auth_sessions s JOIN cloud_users u ON u.id = s.user_id
WHERE s.token_hash = ? AND s.revoked_at = 0 AND s.expires_at > ?`, tokenHash, unixMillis()).
		Scan(&session.ID, &session.UserID, &session.Actor, &session.DisplayName, &session.DeviceID, &session.TokenHash, &session.ExpiresAt, &session.CreatedAt)
	return session, err
}

func (s *Store) RevokeCloudAuthSession(ctx context.Context, tokenHash string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE cloud_auth_sessions SET revoked_at = ? WHERE token_hash = ? AND revoked_at = 0`, unixMillis(), tokenHash)
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

func (s *Store) SetCloudResourceOwner(ctx context.Context, orgID, entityType, entityID string) error {
	if orgID == "" || entityType == "" || entityID == "" {
		return errors.New("org id, entity type, and entity id are required")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO cloud_resource_owners (entity_type, entity_id, org_id, created_at) VALUES (?, ?, ?, ?)
ON CONFLICT(entity_type, entity_id) DO UPDATE SET org_id=excluded.org_id`, entityType, entityID, orgID, unixMillis())
	return err
}

func (s *Store) CloudOwnsResource(ctx context.Context, orgID, entityType, entityID string) (bool, error) {
	var found int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM cloud_resource_owners WHERE org_id = ? AND entity_type = ? AND entity_id = ?`, orgID, entityType, entityID).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil && found == 1, err
}

func (s *Store) ListCloudOwnedResourceIDs(ctx context.Context, orgID, entityType string) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT entity_id FROM cloud_resource_owners WHERE org_id = ? AND entity_type = ?`, orgID, entityType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids[id] = true
	}
	return ids, rows.Err()
}

func (s *Store) CreateCloudUsageRecord(ctx context.Context, record CloudUsageRecord) (CloudUsageRecord, error) {
	if record.OrgID == "" {
		return CloudUsageRecord{}, errors.New("usage organization is required")
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
	_, err := s.db.ExecContext(ctx, `INSERT INTO cloud_usage_ledger (id, org_id, provider, model, input_tokens, output_tokens, cost_micros, credit_delta_micros, metadata, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ID, record.OrgID, record.Provider, record.Model, record.InputTokens, record.OutputTokens, record.CostMicros, record.CreditDeltaMicros, record.Metadata, record.CreatedAt)
	return record, err
}

func (s *Store) CloudUsageSummary(ctx context.Context, orgID string) (CloudUsageSummary, error) {
	var summary CloudUsageSummary
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(input_tokens), 0), COALESCE(SUM(output_tokens), 0), COALESCE(SUM(cost_micros), 0), COALESCE(SUM(credit_delta_micros), 0), COUNT(*) FROM cloud_usage_ledger WHERE org_id = ?`, orgID).
		Scan(&summary.InputTokens, &summary.OutputTokens, &summary.CostMicros, &summary.CreditDeltaMicros, &summary.Records)
	return summary, err
}

func (s *Store) AssertCloudOrgMember(ctx context.Context, userID, orgID string) error {
	var role string
	if err := s.db.QueryRowContext(ctx, `SELECT role FROM cloud_org_members WHERE user_id = ? AND org_id = ?`, userID, orgID).Scan(&role); err != nil {
		return err
	}
	if role == "" {
		return fmt.Errorf("user %q is not a member of organization %q", userID, orgID)
	}
	return nil
}

func (s *Store) CreateCloudShareLink(ctx context.Context, orgID, shareID, tokenHash string, expiresAt int64) (CloudShareLink, error) {
	if orgID == "" || shareID == "" || tokenHash == "" {
		return CloudShareLink{}, errors.New("share link organization, share, and token hash are required")
	}
	now := unixMillis()
	link := CloudShareLink{
		ID: newID("link"), ShareID: shareID, OrgID: orgID, TokenHash: tokenHash, ExpiresAt: expiresAt, CreatedAt: now,
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO cloud_share_links (id, share_id, org_id, token_hash, expires_at, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		link.ID, link.ShareID, link.OrgID, link.TokenHash, link.ExpiresAt, link.CreatedAt)
	return link, err
}

func (s *Store) GetCloudShareLinkByTokenHash(ctx context.Context, tokenHash string) (CloudShareLink, error) {
	var link CloudShareLink
	err := s.db.QueryRowContext(ctx, `SELECT id, share_id, org_id, token_hash, expires_at, revoked_at, created_at
FROM cloud_share_links WHERE token_hash = ? AND revoked_at = 0 AND (expires_at = 0 OR expires_at > ?)`, tokenHash, unixMillis()).
		Scan(&link.ID, &link.ShareID, &link.OrgID, &link.TokenHash, &link.ExpiresAt, &link.RevokedAt, &link.CreatedAt)
	return link, err
}

func (s *Store) RevokeCloudShareLink(ctx context.Context, orgID, shareID, linkID string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE cloud_share_links SET revoked_at = ? WHERE id = ? AND org_id = ? AND share_id = ? AND revoked_at = 0`,
		unixMillis(), linkID, orgID, shareID)
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
