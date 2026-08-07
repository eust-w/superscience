package cloud

import (
	"context"

	"superscience/internal/store"
)

// MetadataStore is the durable Cloud contract. The desktop workbench's SQLite
// store satisfies it for local development; production deployments use the
// PostgreSQL implementation under cloud/postgres.
type MetadataStore interface {
	Ping(context.Context) error
	EnsureCloudIdentity(context.Context, string, string) (store.CloudUser, store.CloudOrganization, error)
	ListCloudOrganizations(context.Context, string) ([]store.CloudOrganization, error)
	CreateCloudDevice(context.Context, string, string, string) (store.CloudDevice, error)
	TouchCloudDevice(context.Context, string) error
	ListCloudDevices(context.Context, string) ([]store.CloudDevice, error)
	CreateCloudAuthSession(context.Context, string, string, string, int64) (store.CloudAuthSession, error)
	GetCloudAuthSession(context.Context, string) (store.CloudAuthSession, error)
	RevokeCloudAuthSession(context.Context, string) error
	SetCloudResourceOwner(context.Context, string, string, string) error
	CloudOwnsResource(context.Context, string, string, string) (bool, error)
	ListCloudOwnedResourceIDs(context.Context, string, string) (map[string]bool, error)
	CloudUsageSummary(context.Context, string) (store.CloudUsageSummary, error)
	CreateCloudShareLink(context.Context, string, string, string, int64) (store.CloudShareLink, error)
	GetCloudShareLinkByTokenHash(context.Context, string) (store.CloudShareLink, error)
	RevokeCloudShareLink(context.Context, string, string, string) error
	ListProjects(context.Context) ([]store.Project, error)
	CreateArtifact(context.Context, store.Artifact) (store.Artifact, error)
	GetArtifact(context.Context, string) (store.Artifact, error)
	CreateProvenanceEdge(context.Context, store.ProvenanceEdge) (store.ProvenanceEdge, error)
	ListProvenance(context.Context, string) ([]store.ProvenanceEdge, error)
	CreateShare(context.Context, store.Share) (store.Share, error)
	GetShare(context.Context, string) (store.Share, error)
	ListShares(context.Context) ([]store.Share, error)
	CreateAuditEvent(context.Context, store.AuditEvent) (store.AuditEvent, error)
	ListAuditEvents(context.Context) ([]store.AuditEvent, error)
}

var _ MetadataStore = (*store.Store)(nil)
