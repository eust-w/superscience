package cloudpostgres_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"superscience/cloud"
	cloudpostgres "superscience/cloud/postgres"
	"superscience/internal/store"
)

func TestPostgresMetadataStoreLifecycle(t *testing.T) {
	databaseURL := os.Getenv("SUPERSCIENCE_TEST_POSTGRES_URL")
	if databaseURL == "" {
		t.Skip("SUPERSCIENCE_TEST_POSTGRES_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db, err := cloudpostgres.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var _ cloud.MetadataStore = db

	suffix := fmt.Sprint(time.Now().UnixNano())
	user, organization, err := db.EnsureCloudIdentity(ctx, "oidc:test-"+suffix, "PostgreSQL Researcher")
	if err != nil || user.ID == "" || organization.ID == "" || organization.Role != "owner" {
		t.Fatalf("identity user=%+v org=%+v err=%v", user, organization, err)
	}
	device, err := db.CreateCloudDevice(ctx, user.ID, organization.ID, "Integration workstation")
	if err != nil {
		t.Fatal(err)
	}
	auth, err := db.CreateCloudAuthSession(ctx, user.ID, device.ID, "hash-"+suffix, time.Now().Add(time.Hour).UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	loadedAuth, err := db.GetCloudAuthSession(ctx, auth.TokenHash)
	if err != nil || loadedAuth.UserID != user.ID || loadedAuth.Actor != user.Actor {
		t.Fatalf("auth=%+v err=%v", loadedAuth, err)
	}

	artifact, err := db.CreateArtifact(ctx, store.Artifact{ID: "artifact-" + suffix, Type: "markdown", Title: "Evidence", Content: "verified"})
	if err != nil {
		t.Fatal(err)
	}
	share, err := db.CreateShare(ctx, store.Share{ID: "share-" + suffix, Title: "Research share", Bundle: `{"version":1}`})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetCloudResourceOwner(ctx, organization.ID, "artifact", artifact.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.SetCloudResourceOwner(ctx, organization.ID, "share", share.ID); err != nil {
		t.Fatal(err)
	}
	owned, err := db.CloudOwnsResource(ctx, organization.ID, "share", share.ID)
	if err != nil || !owned {
		t.Fatalf("share ownership = %v, %v", owned, err)
	}
	edge, err := db.CreateProvenanceEdge(ctx, store.ProvenanceEdge{
		SourceID: artifact.ID, SourceType: "artifact", TargetID: share.ID, TargetType: "share", Relation: "published_as",
	})
	if err != nil {
		t.Fatal(err)
	}
	edges, err := db.ListProvenance(ctx, share.ID)
	if err != nil || len(edges) != 1 || edges[0].ID != edge.ID {
		t.Fatalf("provenance=%+v err=%v", edges, err)
	}
	if _, err := db.CreateAuditEvent(ctx, store.AuditEvent{Actor: user.ID, Action: "integration.verify", EntityID: share.ID, EntityType: "share"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateCloudUsageRecord(ctx, store.CloudUsageRecord{OrgID: organization.ID, Provider: "openai", Model: "integration", InputTokens: 3, OutputTokens: 2, CostMicros: 7}); err != nil {
		t.Fatal(err)
	}
	usage, err := db.CloudUsageSummary(ctx, organization.ID)
	if err != nil || usage.Records != 1 || usage.InputTokens != 3 || usage.CostMicros != 7 {
		t.Fatalf("usage=%+v err=%v", usage, err)
	}
	link, err := db.CreateCloudShareLink(ctx, organization.ID, share.ID, "link-hash-"+suffix, time.Now().Add(time.Hour).UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetCloudShareLinkByTokenHash(ctx, link.TokenHash); err != nil {
		t.Fatal(err)
	}
	if err := db.RevokeCloudShareLink(ctx, organization.ID, share.ID, link.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetCloudShareLinkByTokenHash(ctx, link.TokenHash); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("revoked link error = %v", err)
	}
	if err := db.RevokeCloudAuthSession(ctx, auth.TokenHash); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetCloudAuthSession(ctx, auth.TokenHash); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("revoked auth error = %v", err)
	}
}

func TestPostgresStoreRejectsInsecureRemoteDatabaseURL(t *testing.T) {
	_, err := cloudpostgres.Open(context.Background(), "postgres://user:password@database.example.com/superscience?sslmode=disable")
	if err == nil || !strings.Contains(err.Error(), "TLS") {
		t.Fatalf("insecure remote PostgreSQL error = %v", err)
	}
}
