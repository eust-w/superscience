package cloud_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"superscience/cloud"
	cloudobjects "superscience/cloud/objectstore"
	cloudpostgres "superscience/cloud/postgres"
	"superscience/internal/store"
)

const cloudBootstrapSecret = "test-bootstrap-secret-with-sufficient-entropy"

type filesystemObjectStore struct{ root string }

func (s filesystemObjectStore) Health(context.Context) error { return nil }

func (s filesystemObjectStore) Put(_ context.Context, key, contentType string, reader io.Reader, size int64) (cloud.ObjectInfo, error) {
	path := filepath.Join(s.root, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return cloud.ObjectInfo{}, err
	}
	file, err := os.Create(path)
	if err != nil {
		return cloud.ObjectInfo{}, err
	}
	written, copyErr := io.Copy(file, reader)
	closeErr := file.Close()
	if copyErr != nil {
		return cloud.ObjectInfo{}, copyErr
	}
	if closeErr != nil {
		return cloud.ObjectInfo{}, closeErr
	}
	if written != size {
		return cloud.ObjectInfo{}, fmt.Errorf("wrote %d bytes, expected %d", written, size)
	}
	return cloud.ObjectInfo{Key: key, Size: written, ContentType: contentType, ETag: "filesystem-test-etag"}, nil
}

func (s filesystemObjectStore) Get(_ context.Context, key string) (cloud.Object, error) {
	path := filepath.Join(s.root, filepath.FromSlash(key))
	file, err := os.Open(path)
	if err != nil {
		return cloud.Object{}, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return cloud.Object{}, err
	}
	return cloud.Object{ObjectInfo: cloud.ObjectInfo{Key: key, Size: info.Size(), ContentType: "text/markdown"}, Body: file}, nil
}

func (s filesystemObjectStore) Delete(_ context.Context, key string) error {
	return os.Remove(filepath.Join(s.root, filepath.FromSlash(key)))
}

func TestCloudAuthOwnershipReplayAndUsage(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "cloud.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ts := httptest.NewServer(cloud.New(cloud.Options{Store: db, LocalAuthSecret: cloudBootstrapSecret}).Handler())
	defer ts.Close()

	status, _ := requestJSON(t, http.MethodGet, ts.URL+"/api/shares/not-owned", nil, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("expected auth guard, got %d", status)
	}
	status, _ = requestJSON(t, http.MethodPost, ts.URL+"/api/auth/local", map[string]string{
		"actor": "tester", "secret": "wrong-secret",
	}, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("wrong bootstrap secret status = %d", status)
	}

	auth := postJSON[map[string]any](t, ts.URL+"/api/auth/local", map[string]string{
		"actor": "tester", "displayName": "Test Researcher", "deviceName": "Test Mac", "secret": cloudBootstrapSecret,
	}, "")
	token := auth["token"].(string)
	if len(token) < 40 || strings.Contains(token, "local-dev") {
		t.Fatalf("insecure auth token = %q", token)
	}
	org := auth["org"].(map[string]any)
	if org["id"] == "" || org["role"] != "owner" {
		t.Fatalf("auth org = %+v", org)
	}

	imported := postJSON[map[string]any](t, ts.URL+"/api/bundles/import", map[string]any{
		"title":      "Imported bundle",
		"visibility": "private-link",
		"artifacts":  []map[string]any{{"type": "markdown", "title": "Imported", "content": "# Imported\n"}},
	}, token)
	if imported["status"] != "published" || imported["id"] == "" {
		t.Fatalf("imported = %+v", imported)
	}
	shareID := imported["id"].(string)
	loaded := getJSON[map[string]any](t, ts.URL+"/api/shares/"+shareID+"/replay", token)
	if loaded["share"].(map[string]any)["id"] != shareID || len(loaded["artifacts"].([]any)) != 1 {
		t.Fatalf("replay = %+v", loaded)
	}

	audit := getJSON[[]map[string]any](t, ts.URL+"/api/audit", token)
	if len(audit) == 0 {
		t.Fatal("expected organization-scoped audit events")
	}
	devices := getJSON[[]map[string]any](t, ts.URL+"/api/devices", token)
	if len(devices) != 1 || devices[0]["name"] != "Test Mac" || devices[0]["status"] != "online" {
		t.Fatalf("devices = %+v", devices)
	}
	wallet := getJSON[map[string]any](t, ts.URL+"/api/wallet", token)
	if wallet["currency"] != "credits" || wallet["managedProviderRouting"] != "disabled" || wallet["status"] != "active" {
		t.Fatalf("wallet = %+v", wallet)
	}
	usage := getJSON[map[string]any](t, ts.URL+"/api/usage", token)
	if usage["shares"].(float64) != 1 || usage["artifacts"].(float64) != 1 {
		t.Fatalf("usage = %+v", usage)
	}
	graph := getJSON[map[string]any](t, ts.URL+"/api/graph/"+shareID, token)
	if len(graph["nodes"].([]any)) == 0 || len(graph["edges"].([]any)) == 0 {
		t.Fatalf("graph = %+v", graph)
	}
	published := postJSON[map[string]any](t, ts.URL+"/api/shares/"+shareID+"/publish", map[string]any{
		"expiresInSeconds": 3600,
	}, token)
	linkID := published["id"].(string)
	publicURL := published["url"].(string)
	if linkID == "" || !strings.HasPrefix(publicURL, "/share/") || strings.Contains(publicURL, shareID) {
		t.Fatalf("published link = %+v", published)
	}
	publicReplay := getJSON[map[string]any](t, ts.URL+publicURL, "")
	if publicReplay["share"].(map[string]any)["id"] != shareID || len(publicReplay["artifacts"].([]any)) != 1 {
		t.Fatalf("public replay = %+v", publicReplay)
	}
	artifactID := publicReplay["artifacts"].([]any)[0].(map[string]any)["id"].(string)
	publicArtifact := getJSON[map[string]any](t, strings.TrimSuffix(ts.URL+publicURL, "/replay")+"/artifacts/"+artifactID, "")
	if publicArtifact["id"] != artifactID {
		t.Fatalf("public artifact = %+v", publicArtifact)
	}
	revoked := postJSON[map[string]any](t, ts.URL+"/api/shares/"+shareID+"/revoke", map[string]any{"linkId": linkID}, token)
	if revoked["revoked"] != true {
		t.Fatalf("revoke = %+v", revoked)
	}
	status, _ = requestJSON(t, http.MethodGet, ts.URL+publicURL, nil, "")
	if status != http.StatusNotFound {
		t.Fatalf("revoked public link status = %d", status)
	}

	intruderAuth := postJSON[map[string]any](t, ts.URL+"/api/auth/local", map[string]string{
		"actor": "intruder", "deviceName": "Other Mac", "secret": cloudBootstrapSecret,
	}, "")
	intruderToken := intruderAuth["token"].(string)
	status, _ = requestJSON(t, http.MethodGet, ts.URL+"/api/shares/"+shareID+"/replay", nil, intruderToken)
	if status != http.StatusNotFound {
		t.Fatalf("cross-org replay status = %d", status)
	}
}

func TestCloudSessionPersistsAcrossServerRestartAndLogoutRevokesIt(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "cloud.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	first := httptest.NewServer(cloud.New(cloud.Options{Store: db, LocalAuthSecret: cloudBootstrapSecret}).Handler())
	auth := postJSON[map[string]any](t, first.URL+"/api/auth/local", map[string]string{
		"actor": "persistent-user", "secret": cloudBootstrapSecret,
	}, "")
	token := auth["token"].(string)
	first.Close()

	second := httptest.NewServer(cloud.New(cloud.Options{Store: db, LocalAuthSecret: cloudBootstrapSecret}).Handler())
	defer second.Close()
	orgs := getJSON[[]map[string]any](t, second.URL+"/api/orgs", token)
	if len(orgs) != 1 || orgs[0]["role"] != "owner" {
		t.Fatalf("persisted orgs = %+v", orgs)
	}
	logout := postJSON[map[string]any](t, second.URL+"/api/auth/logout", map[string]any{}, token)
	if logout["revoked"] != true {
		t.Fatalf("logout = %+v", logout)
	}
	status, _ := requestJSON(t, http.MethodGet, second.URL+"/api/orgs", nil, token)
	if status != http.StatusUnauthorized {
		t.Fatalf("revoked session status = %d", status)
	}
}

func TestCloudLocalAuthIsUnavailableWithoutConfiguredSecret(t *testing.T) {
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "cloud.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ts := httptest.NewServer(cloud.New(cloud.Options{Store: db}).Handler())
	defer ts.Close()

	status, body := requestJSON(t, http.MethodPost, ts.URL+"/api/auth/local", map[string]string{"actor": "tester"}, "")
	if status != http.StatusServiceUnavailable || body["code"] != "local_auth_unavailable" {
		t.Fatalf("status=%d body=%+v", status, body)
	}
}

func TestCloudArtifactContentLivesInObjectStoreAndHydratesReplayAndExport(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "cloud.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	objects := filesystemObjectStore{root: t.TempDir()}
	ts := httptest.NewServer(cloud.New(cloud.Options{Store: db, Objects: objects, LocalAuthSecret: cloudBootstrapSecret}).Handler())
	defer ts.Close()
	auth := postJSON[map[string]any](t, ts.URL+"/api/auth/local", map[string]string{
		"actor": "object-researcher", "secret": cloudBootstrapSecret,
	}, "")
	token := auth["token"].(string)
	content := "# Object-backed evidence\n\nVerified in storage.\n"
	imported := postJSON[map[string]any](t, ts.URL+"/api/bundles/import", map[string]any{
		"title": "Object-backed bundle", "visibility": "private-link",
		"artifacts": []map[string]any{{"type": "markdown", "title": "Evidence", "content": content}},
	}, token)
	shareID := imported["id"].(string)
	artifacts, err := db.ListArtifacts(ctx)
	if err != nil || len(artifacts) != 1 {
		t.Fatalf("artifacts=%+v err=%v", artifacts, err)
	}
	if artifacts[0].Content != "" || !strings.Contains(artifacts[0].Metadata, `"cloudObject"`) {
		t.Fatalf("artifact content leaked into metadata store: %+v", artifacts[0])
	}
	storedShare, err := db.GetShare(ctx, shareID)
	if err != nil || strings.Contains(storedShare.Bundle, content) {
		t.Fatalf("share bundle leaked artifact content: err=%v bundle=%s", err, storedShare.Bundle)
	}
	replay := getJSON[map[string]any](t, ts.URL+"/api/shares/"+shareID+"/replay", token)
	replayArtifact := replay["artifacts"].([]any)[0].(map[string]any)
	if replayArtifact["content"] != content {
		t.Fatalf("replay artifact = %+v", replayArtifact)
	}
	exported := getJSON[map[string]any](t, ts.URL+"/api/bundles/export/"+shareID, token)
	exportedArtifact := exported["artifacts"].([]any)[0].(map[string]any)
	if exportedArtifact["content"] != content {
		t.Fatalf("exported artifact = %+v", exportedArtifact)
	}
}

func TestCloudProductionPostgresAndS3Flow(t *testing.T) {
	databaseURL := os.Getenv("SUPERSCIENCE_TEST_POSTGRES_URL")
	endpoint := os.Getenv("SUPERSCIENCE_TEST_S3_ENDPOINT")
	if databaseURL == "" || endpoint == "" {
		t.Skip("production Cloud integration endpoints are not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	metadata, err := cloudpostgres.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer metadata.Close()
	objects, err := cloudobjects.NewS3(ctx, cloudobjects.S3Config{
		Endpoint: endpoint, Region: os.Getenv("SUPERSCIENCE_TEST_S3_REGION"), Bucket: os.Getenv("SUPERSCIENCE_TEST_S3_BUCKET"),
		AccessKey: os.Getenv("SUPERSCIENCE_TEST_S3_ACCESS_KEY"), SecretKey: os.Getenv("SUPERSCIENCE_TEST_S3_SECRET_KEY"),
		PathStyle: true, EnsureBucket: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(cloud.New(cloud.Options{Store: metadata, Objects: objects, LocalAuthSecret: cloudBootstrapSecret}).Handler())
	defer ts.Close()
	auth := postJSON[map[string]any](t, ts.URL+"/api/auth/local", map[string]string{
		"actor": fmt.Sprintf("production-flow-%d", time.Now().UnixNano()), "secret": cloudBootstrapSecret,
	}, "")
	token := auth["token"].(string)
	content := "PostgreSQL metadata and S3 object content"
	imported := postJSON[map[string]any](t, ts.URL+"/api/bundles/import", map[string]any{
		"title": "Production storage flow", "artifacts": []map[string]any{{"type": "markdown", "title": "Evidence", "content": content}},
	}, token)
	replay := getJSON[map[string]any](t, ts.URL+"/api/shares/"+imported["id"].(string)+"/replay", token)
	artifact := replay["artifacts"].([]any)[0].(map[string]any)
	if artifact["content"] != content || artifact["metadata"] != "{}" {
		t.Fatalf("production replay artifact = %+v", artifact)
	}
	persisted, err := metadata.GetArtifact(ctx, artifact["id"].(string))
	if err != nil || persisted.Content != "" || !strings.Contains(persisted.Metadata, `"cloudObject"`) {
		t.Fatalf("production metadata artifact=%+v err=%v", persisted, err)
	}
}

func getJSON[T any](t *testing.T, url string, token string) T {
	t.Helper()
	status, body := requestJSONAny(t, http.MethodGet, url, nil, token)
	if status != http.StatusOK {
		t.Fatalf("GET %s = %d: %+v", url, status, body)
	}
	data, _ := json.Marshal(body)
	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func postJSON[T any](t *testing.T, url string, payload any, token string) T {
	t.Helper()
	status, response := requestJSONAny(t, http.MethodPost, url, payload, token)
	if status != http.StatusOK {
		t.Fatalf("POST %s = %d: %+v", url, status, response)
	}
	data, _ := json.Marshal(response)
	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func requestJSON(t *testing.T, method, url string, payload any, token string) (int, map[string]any) {
	t.Helper()
	status, value := requestJSONAny(t, method, url, payload, token)
	response, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("expected JSON object, got %T", value)
	}
	return status, response
}

func requestJSONAny(t *testing.T, method, url string, payload any, token string) (int, any) {
	t.Helper()
	var body *bytes.Reader
	if payload == nil {
		body = bytes.NewReader(nil)
	} else {
		data, _ := json.Marshal(payload)
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatal(err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, out
}
