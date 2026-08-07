package cloud

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
	"unicode"

	sharepkg "superscience/internal/share"
	"superscience/internal/store"
)

const defaultSessionTTL = 24 * time.Hour
const maxCloudArtifactBytes = 256 * 1024 * 1024

type Options struct {
	Store           MetadataStore
	Objects         ObjectStore
	LocalAuthSecret string
	OIDC            OIDCAuthenticator
	SessionTTL      time.Duration
	AllowedHosts    []string
}

type Server struct {
	store           MetadataStore
	objects         ObjectStore
	localAuthSecret string
	oidc            OIDCAuthenticator
	sessionTTL      time.Duration
	allowedHosts    map[string]bool
}

type principal struct {
	Session   store.CloudAuthSession
	Org       store.CloudOrganization
	TokenHash string
}

type principalKey struct{}

func New(options Options) *Server {
	ttl := options.SessionTTL
	if ttl <= 0 {
		ttl = defaultSessionTTL
	}
	hosts := map[string]bool{"localhost": true, "127.0.0.1": true, "::1": true}
	for _, host := range options.AllowedHosts {
		if trimmed := strings.TrimSpace(strings.ToLower(host)); trimmed != "" {
			hosts[trimmed] = true
		}
	}
	return &Server{
		store: options.Store, objects: options.Objects, localAuthSecret: options.LocalAuthSecret, oidc: options.OIDC, sessionTTL: ttl, allowedHosts: hosts,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/share/", s.handlePublicShare)
	mux.HandleFunc("/api/auth/local", s.handleLocalAuth)
	mux.HandleFunc("/api/auth/oidc", s.handleOIDCAuth)
	mux.HandleFunc("/api/auth/logout", s.withAuth(s.handleLogout))
	mux.HandleFunc("/api/orgs", s.withAuth(s.handleOrgs))
	mux.HandleFunc("/api/projects", s.withAuth(s.handleProjects))
	mux.HandleFunc("/api/devices", s.withAuth(s.handleDevices))
	mux.HandleFunc("/api/wallet", s.withAuth(s.handleWallet))
	mux.HandleFunc("/api/usage", s.withAuth(s.handleUsage))
	mux.HandleFunc("/api/graph/", s.withAuth(s.handleGraphSubroutes))
	mux.HandleFunc("/api/shares", s.withAuth(s.handleShares))
	mux.HandleFunc("/api/shares/", s.withAuth(s.handleShareSubroutes))
	mux.HandleFunc("/api/artifacts/", s.withAuth(s.handleArtifactSubroutes))
	mux.HandleFunc("/api/bundles/import", s.withAuth(s.handleBundleImport))
	mux.HandleFunc("/api/bundles/export/", s.withAuth(s.handleBundleExport))
	mux.HandleFunc("/api/audit", s.withAuth(s.handleAudit))
	return s.withRequestContext(s.withHostGuard(mux))
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", false, nil)
		return
	}
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "storage_unavailable", "cloud storage is not configured", true, nil)
		return
	}
	if err := s.store.Ping(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "storage_unavailable", err.Error(), true, nil)
		return
	}
	objectStorage := "inline-development"
	if s.objects != nil {
		healthCtx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		err := s.objects.Health(healthCtx)
		cancel()
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "object_storage_unavailable", err.Error(), true, nil)
			return
		}
		objectStorage = "ready"
	}
	authStatus := map[string]string{"local": "unavailable", "oidc": "unavailable"}
	authReady := false
	if s.localAuthEnabled() {
		authStatus["local"] = "ready"
		authReady = true
	}
	if s.oidc != nil {
		healthCtx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		err := s.oidc.Health(healthCtx)
		cancel()
		if err == nil {
			authStatus["oidc"] = "ready"
			authReady = true
		} else {
			authStatus["oidc"] = "unavailable"
		}
	}
	status := "ready"
	if !authReady {
		status = "degraded"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": status, "service": "superscience-cloud", "storage": "ready", "objectStorage": objectStorage, "auth": authStatus,
		"localAuth": authStatus["local"],
	})
}

func (s *Server) handleLocalAuth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", false, nil)
		return
	}
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "storage_unavailable", "cloud storage is not configured", true, nil)
		return
	}
	if !s.localAuthEnabled() {
		writeError(w, http.StatusServiceUnavailable, "local_auth_unavailable", "local authentication requires a configured bootstrap secret", false, nil)
		return
	}
	var body struct {
		Actor       string `json:"actor"`
		DisplayName string `json:"displayName"`
		DeviceName  string `json:"deviceName"`
		Secret      string `json:"secret"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), false, nil)
		return
	}
	if subtle.ConstantTimeCompare([]byte(body.Secret), []byte(s.localAuthSecret)) != 1 {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "invalid local authentication credentials", false, nil)
		return
	}
	actor, err := validateActor(body.Actor)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_actor", err.Error(), false, nil)
		return
	}
	s.issueSession(w, r, actor, body.DisplayName, body.DeviceName, "local")
}

func (s *Server) handleOIDCAuth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", false, nil)
		return
	}
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "storage_unavailable", "cloud storage is not configured", true, nil)
		return
	}
	if s.oidc == nil {
		writeError(w, http.StatusServiceUnavailable, "oidc_unavailable", "OIDC authentication is not configured", false, nil)
		return
	}
	var body struct {
		IDToken    string `json:"idToken"`
		DeviceName string `json:"deviceName"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), false, nil)
		return
	}
	identity, err := s.oidc.Verify(r.Context(), body.IDToken)
	if err != nil {
		code := "invalid_oidc_token"
		if errors.Is(err, ErrOIDCIdentity) {
			code = "invalid_oidc_identity"
		}
		writeError(w, http.StatusUnauthorized, code, err.Error(), false, nil)
		return
	}
	s.issueSession(w, r, identity.Actor, identity.DisplayName, body.DeviceName, "oidc")
}

func (s *Server) issueSession(w http.ResponseWriter, r *http.Request, actor, displayName, deviceName, method string) {
	user, org, err := s.store.EnsureCloudIdentity(r.Context(), actor, displayName)
	if err != nil {
		writeResult(w, nil, err)
		return
	}
	device, err := s.store.CreateCloudDevice(r.Context(), user.ID, org.ID, deviceName)
	if err != nil {
		writeResult(w, nil, err)
		return
	}
	token, tokenHash, err := newBearerToken()
	if err != nil {
		writeResult(w, nil, err)
		return
	}
	expiresAt := time.Now().Add(s.sessionTTL).UnixMilli()
	if _, err := s.store.CreateCloudAuthSession(r.Context(), user.ID, device.ID, tokenHash, expiresAt); err != nil {
		writeResult(w, nil, err)
		return
	}
	_ = s.store.SetCloudResourceOwner(r.Context(), org.ID, "organization", org.ID)
	_, _ = s.store.CreateAuditEvent(r.Context(), store.AuditEvent{
		Actor: user.ID, Action: "cloud.auth.login", EntityID: org.ID, EntityType: "organization",
		Metadata: fmt.Sprintf(`{"method":%q}`, method),
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"token": token, "tokenType": "Bearer", "expiresAt": expiresAt, "user": user, "org": org, "device": device,
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", false, nil)
		return
	}
	p := currentPrincipal(r.Context())
	if err := s.store.RevokeCloudAuthSession(r.Context(), p.TokenHash); err != nil {
		writeResult(w, nil, err)
		return
	}
	_, _ = s.store.CreateAuditEvent(r.Context(), store.AuditEvent{
		Actor: p.Session.UserID, Action: "cloud.auth.logout", EntityID: p.Org.ID, EntityType: "organization",
	})
	writeJSON(w, http.StatusOK, map[string]any{"revoked": true})
}

func (s *Server) handleOrgs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", false, nil)
		return
	}
	organizations, err := s.store.ListCloudOrganizations(r.Context(), currentPrincipal(r.Context()).Session.UserID)
	writeResult(w, organizations, err)
}

func (s *Server) handleProjects(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", false, nil)
		return
	}
	projects, err := s.store.ListProjects(r.Context())
	if err != nil {
		writeResult(w, nil, err)
		return
	}
	owned, err := s.store.ListCloudOwnedResourceIDs(r.Context(), currentPrincipal(r.Context()).Org.ID, "project")
	if err != nil {
		writeResult(w, nil, err)
		return
	}
	result := make([]store.Project, 0, len(projects))
	for _, project := range projects {
		if owned[project.ID] {
			result = append(result, project)
		}
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleDevices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", false, nil)
		return
	}
	devices, err := s.store.ListCloudDevices(r.Context(), currentPrincipal(r.Context()).Org.ID)
	writeResult(w, devices, err)
}

func (s *Server) handleWallet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", false, nil)
		return
	}
	p := currentPrincipal(r.Context())
	summary, err := s.store.CloudUsageSummary(r.Context(), p.Org.ID)
	if err != nil {
		writeResult(w, nil, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": p.Org.ID, "currency": "credits", "balance": float64(summary.CreditDeltaMicros) / 1_000_000,
		"managedProviderRouting": "disabled", "status": "active", "usage": summary,
	})
}

func (s *Server) handleUsage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", false, nil)
		return
	}
	p := currentPrincipal(r.Context())
	projects, err := s.store.ListCloudOwnedResourceIDs(r.Context(), p.Org.ID, "project")
	if err != nil {
		writeResult(w, nil, err)
		return
	}
	shares, err := s.store.ListCloudOwnedResourceIDs(r.Context(), p.Org.ID, "share")
	if err != nil {
		writeResult(w, nil, err)
		return
	}
	artifacts, err := s.store.ListCloudOwnedResourceIDs(r.Context(), p.Org.ID, "artifact")
	if err != nil {
		writeResult(w, nil, err)
		return
	}
	summary, err := s.store.CloudUsageSummary(r.Context(), p.Org.ID)
	if err != nil {
		writeResult(w, nil, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"projects": len(projects), "shares": len(shares), "artifacts": len(artifacts), "providerUsage": summary, "mode": "self-hosted",
	})
}

func (s *Server) handleGraphSubroutes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", false, nil)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/graph/")
	if id == "" || !s.requireOwned(w, r, "share", id) {
		return
	}
	share, err := s.store.GetShare(r.Context(), id)
	if err != nil {
		writeResult(w, share, err)
		return
	}
	var bundle sharepkg.Bundle
	if err := json.Unmarshal([]byte(share.Bundle), &bundle); err != nil {
		writeResult(w, nil, err)
		return
	}
	provenance, err := s.store.ListProvenance(r.Context(), id)
	if err != nil {
		writeResult(w, nil, err)
		return
	}
	nodes := []map[string]any{{"id": share.ID, "type": "share", "title": share.Title}}
	for _, artifact := range bundle.Artifacts {
		nodes = append(nodes, map[string]any{"id": artifact.ID, "type": "artifact", "title": artifact.Title})
	}
	edges := make([]map[string]any, 0, len(provenance))
	for _, edge := range provenance {
		edges = append(edges, map[string]any{
			"id": edge.ID, "source": edge.SourceID, "target": edge.TargetID, "relation": edge.Relation,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"root": id, "nodes": nodes, "edges": edges})
}

func (s *Server) handleShares(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", false, nil)
		return
	}
	shares, err := s.store.ListShares(r.Context())
	if err != nil {
		writeResult(w, nil, err)
		return
	}
	owned, err := s.store.ListCloudOwnedResourceIDs(r.Context(), currentPrincipal(r.Context()).Org.ID, "share")
	if err != nil {
		writeResult(w, nil, err)
		return
	}
	result := make([]store.Share, 0, len(shares))
	for _, share := range shares {
		if owned[share.ID] {
			result = append(result, share)
		}
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleShareSubroutes(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/shares/"), "/")
	if len(parts) == 0 || parts[0] == "" || !s.requireOwned(w, r, "share", parts[0]) {
		return
	}
	if len(parts) == 2 && parts[1] == "publish" && r.Method == http.MethodPost {
		s.handleSharePublish(w, r, parts[0])
		return
	}
	if len(parts) == 2 && parts[1] == "revoke" && r.Method == http.MethodPost {
		s.handleShareRevoke(w, r, parts[0])
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", false, nil)
		return
	}
	share, err := s.store.GetShare(r.Context(), parts[0])
	if err != nil {
		writeResult(w, share, err)
		return
	}
	if len(parts) == 2 && parts[1] == "replay" {
		s.writeShareReplay(w, r, share)
		return
	}
	if len(parts) != 1 {
		writeError(w, http.StatusNotFound, "not_found", "share route not found", false, nil)
		return
	}
	writeJSON(w, http.StatusOK, share)
}

func (s *Server) handleSharePublish(w http.ResponseWriter, r *http.Request, shareID string) {
	var body struct {
		ExpiresInSeconds int64 `json:"expiresInSeconds"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), false, nil)
		return
	}
	if body.ExpiresInSeconds == 0 {
		body.ExpiresInSeconds = int64((7 * 24 * time.Hour) / time.Second)
	}
	if body.ExpiresInSeconds < 60 || body.ExpiresInSeconds > int64((30*24*time.Hour)/time.Second) {
		writeError(w, http.StatusBadRequest, "invalid_expiry", "share link expiry must be between 60 seconds and 30 days", false, nil)
		return
	}
	token, tokenHash, err := newBearerToken()
	if err != nil {
		writeResult(w, nil, err)
		return
	}
	p := currentPrincipal(r.Context())
	link, err := s.store.CreateCloudShareLink(r.Context(), p.Org.ID, shareID, tokenHash, time.Now().Add(time.Duration(body.ExpiresInSeconds)*time.Second).UnixMilli())
	if err != nil {
		writeResult(w, nil, err)
		return
	}
	_, _ = s.store.CreateAuditEvent(r.Context(), store.AuditEvent{
		Actor: p.Session.UserID, Action: "share.link.publish", EntityID: shareID, EntityType: "share",
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"id": link.ID, "shareId": shareID, "url": "/share/" + token + "/replay", "expiresAt": link.ExpiresAt,
	})
}

func (s *Server) handleShareRevoke(w http.ResponseWriter, r *http.Request, shareID string) {
	var body struct {
		LinkID string `json:"linkId"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), false, nil)
		return
	}
	if strings.TrimSpace(body.LinkID) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "linkId is required", false, nil)
		return
	}
	p := currentPrincipal(r.Context())
	if err := s.store.RevokeCloudShareLink(r.Context(), p.Org.ID, shareID, body.LinkID); err != nil {
		writeResult(w, nil, err)
		return
	}
	_, _ = s.store.CreateAuditEvent(r.Context(), store.AuditEvent{
		Actor: p.Session.UserID, Action: "share.link.revoke", EntityID: shareID, EntityType: "share",
	})
	writeJSON(w, http.StatusOK, map[string]any{"revoked": true, "linkId": body.LinkID})
}

func (s *Server) handlePublicShare(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", false, nil)
		return
	}
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "storage_unavailable", "cloud storage is not configured", true, nil)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/share/"), "/")
	if len(parts) < 2 || len(parts[0]) < 32 {
		writeError(w, http.StatusNotFound, "not_found", "share link not found", false, nil)
		return
	}
	link, err := s.store.GetCloudShareLinkByTokenHash(r.Context(), hashToken(parts[0]))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "share link not found", false, nil)
		return
	}
	share, err := s.store.GetShare(r.Context(), link.ShareID)
	if err != nil || share.Status != "published" {
		writeError(w, http.StatusNotFound, "not_found", "share not found", false, nil)
		return
	}
	var bundle sharepkg.Bundle
	if err := json.Unmarshal([]byte(share.Bundle), &bundle); err != nil {
		writeResult(w, nil, err)
		return
	}
	if len(parts) == 2 && parts[1] == "replay" {
		artifacts, err := s.hydrateArtifacts(r.Context(), bundle.Artifacts)
		if err != nil {
			writeResult(w, nil, err)
			return
		}
		provenance, err := s.store.ListProvenance(r.Context(), share.ID)
		if err != nil {
			writeResult(w, nil, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"share": publicShare(share), "artifacts": artifacts, "provenance": provenance, "expiresAt": link.ExpiresAt,
		})
		return
	}
	if len(parts) == 3 && parts[1] == "artifacts" {
		for _, artifact := range bundle.Artifacts {
			if artifact.ID == parts[2] {
				hydrated, err := s.hydrateArtifact(r.Context(), artifact)
				writeResult(w, hydrated, err)
				return
			}
		}
		writeError(w, http.StatusNotFound, "not_found", "artifact not found", false, nil)
		return
	}
	writeError(w, http.StatusNotFound, "not_found", "share route not found", false, nil)
}

type artifactObjectReference struct {
	Key         string `json:"key"`
	Size        int64  `json:"size"`
	ContentType string `json:"contentType"`
	ETag        string `json:"etag"`
	SHA256      string `json:"sha256"`
}

func (s *Server) createCloudArtifact(ctx context.Context, orgID string, artifact store.Artifact) (store.Artifact, error) {
	if s.objects == nil {
		return s.store.CreateArtifact(ctx, artifact)
	}
	if len(artifact.Content) > maxCloudArtifactBytes {
		return store.Artifact{}, fmt.Errorf("artifact content exceeds %d bytes", maxCloudArtifactBytes)
	}
	metadata, err := artifactMetadata(artifact.Metadata)
	if err != nil {
		return store.Artifact{}, err
	}
	artifact.ID = randomEntityID("artifact")
	contentType := artifactContentType(artifact.Type)
	key := "organizations/" + orgID + "/artifacts/" + artifact.ID + "/content"
	digest := sha256.Sum256([]byte(artifact.Content))
	info, err := s.objects.Put(ctx, key, contentType, strings.NewReader(artifact.Content), int64(len(artifact.Content)))
	if err != nil {
		return store.Artifact{}, err
	}
	metadata["cloudObject"] = artifactObjectReference{
		Key: info.Key, Size: info.Size, ContentType: info.ContentType, ETag: info.ETag, SHA256: hex.EncodeToString(digest[:]),
	}
	encodedMetadata, err := json.Marshal(metadata)
	if err != nil {
		_ = s.objects.Delete(ctx, key)
		return store.Artifact{}, err
	}
	artifact.Content = ""
	artifact.Metadata = string(encodedMetadata)
	created, err := s.store.CreateArtifact(ctx, artifact)
	if err != nil {
		_ = s.objects.Delete(ctx, key)
		return store.Artifact{}, err
	}
	return created, nil
}

func (s *Server) hydrateArtifacts(ctx context.Context, artifacts []store.Artifact) ([]store.Artifact, error) {
	result := make([]store.Artifact, 0, len(artifacts))
	for _, artifact := range artifacts {
		hydrated, err := s.hydrateArtifact(ctx, artifact)
		if err != nil {
			return nil, fmt.Errorf("load artifact %q content: %w", artifact.ID, err)
		}
		result = append(result, hydrated)
	}
	return result, nil
}

func (s *Server) hydrateArtifact(ctx context.Context, artifact store.Artifact) (store.Artifact, error) {
	reference, metadata, stored, err := artifactObject(artifact.Metadata)
	if err != nil {
		return store.Artifact{}, err
	}
	artifact.Metadata = metadata
	if !stored {
		return artifact, nil
	}
	if s.objects == nil {
		return store.Artifact{}, errors.New("artifact content requires configured object storage")
	}
	object, err := s.objects.Get(ctx, reference.Key)
	if err != nil {
		return store.Artifact{}, err
	}
	if object.Size < 0 || object.Size > maxCloudArtifactBytes || object.Size != reference.Size {
		object.Body.Close()
		return store.Artifact{}, errors.New("artifact object size does not match its metadata")
	}
	payload, readErr := io.ReadAll(io.LimitReader(object.Body, maxCloudArtifactBytes+1))
	closeErr := object.Body.Close()
	if readErr != nil {
		return store.Artifact{}, readErr
	}
	if closeErr != nil {
		return store.Artifact{}, closeErr
	}
	if int64(len(payload)) != reference.Size {
		return store.Artifact{}, errors.New("artifact object returned an incomplete payload")
	}
	digest := sha256.Sum256(payload)
	if !strings.EqualFold(hex.EncodeToString(digest[:]), reference.SHA256) {
		return store.Artifact{}, errors.New("artifact object checksum does not match its metadata")
	}
	artifact.Content = string(payload)
	return artifact, nil
}

func artifactMetadata(raw string) (map[string]any, error) {
	metadata := map[string]any{}
	if strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &metadata); err != nil || metadata == nil {
			return nil, errors.New("artifact metadata must be a JSON object")
		}
	}
	delete(metadata, "cloudObject")
	return metadata, nil
}

func artifactObject(raw string) (artifactObjectReference, string, bool, error) {
	metadata, err := artifactMetadataPreservingObject(raw)
	if err != nil {
		return artifactObjectReference{}, "", false, err
	}
	value, exists := metadata["cloudObject"]
	delete(metadata, "cloudObject")
	cleaned, err := json.Marshal(metadata)
	if err != nil {
		return artifactObjectReference{}, "", false, err
	}
	if !exists {
		return artifactObjectReference{}, string(cleaned), false, nil
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return artifactObjectReference{}, "", false, err
	}
	var reference artifactObjectReference
	if err := json.Unmarshal(payload, &reference); err != nil {
		return artifactObjectReference{}, "", false, errors.New("artifact object metadata is invalid")
	}
	if reference.Key == "" || reference.Size < 0 || len(reference.SHA256) != sha256.Size*2 {
		return artifactObjectReference{}, "", false, errors.New("artifact object metadata is incomplete")
	}
	return reference, string(cleaned), true, nil
}

func artifactMetadataPreservingObject(raw string) (map[string]any, error) {
	metadata := map[string]any{}
	if strings.TrimSpace(raw) == "" {
		return metadata, nil
	}
	if err := json.Unmarshal([]byte(raw), &metadata); err != nil || metadata == nil {
		return nil, errors.New("artifact metadata must be a JSON object")
	}
	return metadata, nil
}

func artifactContentType(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "markdown", "latex", "text", "sequence", "msa":
		return "text/plain; charset=utf-8"
	case "image":
		return "application/octet-stream"
	case "table", "plot", "notebook-output":
		return "application/json"
	default:
		return "application/octet-stream"
	}
}

func randomEntityID(prefix string) string {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	}
	return prefix + "_" + hex.EncodeToString(buffer)
}

func publicShare(share store.Share) map[string]any {
	return map[string]any{
		"id": share.ID, "title": share.Title, "visibility": share.Visibility, "status": share.Status,
		"createdAt": share.CreatedAt, "updatedAt": share.UpdatedAt,
	}
}

func (s *Server) handleArtifactSubroutes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", false, nil)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/artifacts/")
	if id == "" || !s.requireOwned(w, r, "artifact", id) {
		return
	}
	artifact, err := s.store.GetArtifact(r.Context(), id)
	if err == nil {
		artifact, err = s.hydrateArtifact(r.Context(), artifact)
	}
	writeResult(w, artifact, err)
}

func (s *Server) handleBundleImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", false, nil)
		return
	}
	var body sharepkg.Bundle
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_bundle", err.Error(), false, nil)
		return
	}
	if strings.TrimSpace(body.Title) == "" || len(body.Artifacts) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_bundle", "bundle title and at least one artifact are required", false, nil)
		return
	}
	p := currentPrincipal(r.Context())
	artifacts := make([]store.Artifact, 0, len(body.Artifacts))
	for _, artifact := range body.Artifacts {
		artifact.ID = ""
		created, err := s.createCloudArtifact(r.Context(), p.Org.ID, artifact)
		if err != nil {
			writeResult(w, nil, err)
			return
		}
		if err := s.store.SetCloudResourceOwner(r.Context(), p.Org.ID, "artifact", created.ID); err != nil {
			writeResult(w, nil, err)
			return
		}
		artifacts = append(artifacts, created)
	}
	artifactIDs := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		artifactIDs = append(artifactIDs, artifact.ID)
	}
	bundle, err := sharepkg.BuildBundle(sharepkg.PublishRequest{Title: body.Title, Visibility: body.Visibility, ArtifactIDs: artifactIDs}, artifacts)
	if err != nil {
		writeResult(w, nil, err)
		return
	}
	record, err := s.store.CreateShare(r.Context(), store.Share{Title: body.Title, Visibility: body.Visibility, Status: "published", Bundle: bundle})
	if err != nil {
		writeResult(w, nil, err)
		return
	}
	if err := s.store.SetCloudResourceOwner(r.Context(), p.Org.ID, "share", record.ID); err != nil {
		writeResult(w, nil, err)
		return
	}
	record.URL = "/api/shares/" + record.ID
	for _, artifact := range artifacts {
		_, _ = s.store.CreateProvenanceEdge(r.Context(), store.ProvenanceEdge{
			SourceID: artifact.ID, SourceType: "artifact", TargetID: record.ID, TargetType: "share", Relation: "imported",
		})
	}
	_, _ = s.store.CreateAuditEvent(r.Context(), store.AuditEvent{
		Actor: p.Session.UserID, Action: "bundle.import", EntityID: record.ID, EntityType: "share",
	})
	writeJSON(w, http.StatusOK, record)
}

func (s *Server) handleBundleExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", false, nil)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/bundles/export/")
	if id == "" || !s.requireOwned(w, r, "share", id) {
		return
	}
	share, err := s.store.GetShare(r.Context(), id)
	if err != nil {
		writeResult(w, share, err)
		return
	}
	var bundle sharepkg.Bundle
	if err := json.Unmarshal([]byte(share.Bundle), &bundle); err != nil {
		writeResult(w, nil, err)
		return
	}
	bundle.Artifacts, err = s.hydrateArtifacts(r.Context(), bundle.Artifacts)
	if err != nil {
		writeResult(w, nil, err)
		return
	}
	writeJSON(w, http.StatusOK, bundle)
}

func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", false, nil)
		return
	}
	events, err := s.store.ListAuditEvents(r.Context())
	if err != nil {
		writeResult(w, nil, err)
		return
	}
	orgID := currentPrincipal(r.Context()).Org.ID
	result := make([]store.AuditEvent, 0, len(events))
	for _, event := range events {
		owned, ownerErr := s.store.CloudOwnsResource(r.Context(), orgID, event.EntityType, event.EntityID)
		if ownerErr != nil {
			writeResult(w, nil, ownerErr)
			return
		}
		if owned {
			result = append(result, event)
		}
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) writeShareReplay(w http.ResponseWriter, r *http.Request, share store.Share) {
	var bundle sharepkg.Bundle
	if err := json.Unmarshal([]byte(share.Bundle), &bundle); err != nil {
		writeResult(w, nil, err)
		return
	}
	artifacts, err := s.hydrateArtifacts(r.Context(), bundle.Artifacts)
	if err != nil {
		writeResult(w, nil, err)
		return
	}
	provenance, err := s.store.ListProvenance(r.Context(), share.ID)
	if err != nil {
		writeResult(w, nil, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"share": share, "artifacts": artifacts, "provenance": provenance})
}

func (s *Server) requireOwned(w http.ResponseWriter, r *http.Request, entityType, entityID string) bool {
	owned, err := s.store.CloudOwnsResource(r.Context(), currentPrincipal(r.Context()).Org.ID, entityType, entityID)
	if err != nil {
		writeResult(w, nil, err)
		return false
	}
	if !owned {
		writeError(w, http.StatusNotFound, "not_found", entityType+" not found", false, nil)
		return false
	}
	return true
}

func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.store == nil {
			writeError(w, http.StatusServiceUnavailable, "storage_unavailable", "cloud storage is not configured", true, nil)
			return
		}
		token, err := bearerToken(r.Header.Get("Authorization"))
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", err.Error(), false, nil)
			return
		}
		tokenHash := hashToken(token)
		session, err := s.store.GetCloudAuthSession(r.Context(), tokenHash)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "missing, expired, or revoked bearer token", false, nil)
			return
		}
		organizations, err := s.store.ListCloudOrganizations(r.Context(), session.UserID)
		if err != nil || len(organizations) == 0 {
			writeError(w, http.StatusForbidden, "organization_required", "the authenticated user has no organization membership", false, nil)
			return
		}
		selected := organizations[0]
		if requested := strings.TrimSpace(r.Header.Get("X-Superscience-Org")); requested != "" {
			found := false
			for _, org := range organizations {
				if org.ID == requested {
					selected = org
					found = true
					break
				}
			}
			if !found {
				writeError(w, http.StatusForbidden, "organization_forbidden", "the authenticated user is not a member of the requested organization", false, nil)
				return
			}
		}
		_ = s.store.TouchCloudDevice(r.Context(), session.DeviceID)
		ctx := context.WithValue(r.Context(), principalKey{}, principal{Session: session, Org: selected, TokenHash: tokenHash})
		next(w, r.WithContext(ctx))
	}
}

func (s *Server) withHostGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !allowedHost(r.Host, s.allowedHosts) {
			writeError(w, http.StatusForbidden, "forbidden_host", "forbidden host", false, nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) withRequestContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if requestID == "" {
			requestID = shortRandomID()
		}
		w.Header().Set("X-Request-ID", requestID)
		next.ServeHTTP(w, r)
	})
}

func (s *Server) localAuthEnabled() bool {
	return len(s.localAuthSecret) >= 24
}

func currentPrincipal(ctx context.Context) principal {
	value, _ := ctx.Value(principalKey{}).(principal)
	return value
}

func validateActor(actor string) (string, error) {
	actor = strings.TrimSpace(actor)
	if len(actor) < 2 || len(actor) > 128 {
		return "", errors.New("actor must contain between 2 and 128 characters")
	}
	for _, r := range actor {
		if unicode.IsControl(r) {
			return "", errors.New("actor contains control characters")
		}
	}
	return actor, nil
}

func newBearerToken() (string, string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", "", err
	}
	token := base64.RawURLEncoding.EncodeToString(buffer)
	return token, hashToken(token), nil
}

func hashToken(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

func shortRandomID() string {
	buffer := make([]byte, 8)
	if _, err := rand.Read(buffer); err != nil {
		return fmt.Sprintf("req-%d", time.Now().UnixNano())
	}
	return "req-" + hex.EncodeToString(buffer)
}

func bearerToken(header string) (string, error) {
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || len(parts[1]) < 32 {
		return "", errors.New("missing or invalid bearer token")
	}
	return parts[1], nil
}

func readJSON(r *http.Request, out any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 2_000_000))
	decoder.DisallowUnknownFields()
	return decoder.Decode(out)
}

func writeResult(w http.ResponseWriter, value any, err error) {
	if err != nil {
		status := http.StatusInternalServerError
		code := "internal_error"
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
			code = "not_found"
		}
		writeError(w, status, code, err.Error(), status >= 500, nil)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code, message string, retryable bool, details map[string]any) {
	if details == nil {
		details = map[string]any{}
	}
	writeJSON(w, status, map[string]any{
		"code": code, "message": message, "retryable": retryable, "details": details, "requestId": w.Header().Get("X-Request-ID"),
	})
}

func allowedHost(hostport string, allowed map[string]bool) bool {
	if hostport == "" {
		return true
	}
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		host = hostport
	}
	host = strings.ToLower(strings.Trim(host, "[]"))
	return allowed[host]
}
