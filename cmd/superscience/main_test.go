package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"superscience/internal/buildinfo"
	releasepkg "superscience/internal/release"
	sharepkg "superscience/internal/share"
	"superscience/internal/skills"
	"superscience/internal/store"
)

func TestUpgradeCLIChecksBeforeInstallingAndRequiresConfirmation(t *testing.T) {
	archiveName := releasepkg.ArchiveName("v1.2.3", runtime.GOOS, runtime.GOARCH)
	archive := cliReleaseArchive(t, "superscience", []byte("new superscience"))
	digest := sha256.Sum256(archive)
	server := cliReleaseServer(t, "v1.2.3", map[string][]byte{
		archiveName:     archive,
		"checksums.txt": []byte(hex.EncodeToString(digest[:]) + "  " + archiveName + "\n"),
	})
	defer server.Close()

	target := filepath.Join(t.TempDir(), "superscience")
	if err := os.WriteFile(target, []byte("old superscience"), 0o755); err != nil {
		t.Fatal(err)
	}
	previousVersion := buildinfo.Version
	buildinfo.Version = "v1.2.2"
	t.Cleanup(func() { buildinfo.Version = previousVersion })

	baseArgs := []string{
		"upgrade", "--repository", "owner/repo", "--api-url", server.URL,
		"--target", target,
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runCommand(context.Background(), baseArgs, &stdout, &stderr); err != nil {
		t.Fatalf("upgrade check: %v\nstderr=%s", err, stderr.String())
	}
	if output := stdout.String(); !strings.Contains(output, `"updateAvailable": true`) || !strings.Contains(output, `"confirmationRequired": true`) {
		t.Fatalf("upgrade check output = %s", output)
	}
	if installed, err := os.ReadFile(target); err != nil || string(installed) != "old superscience" {
		t.Fatalf("upgrade changed target without confirmation: %q, %v", installed, err)
	}

	stdout.Reset()
	stderr.Reset()
	if err := runCommand(context.Background(), append(baseArgs, "--yes"), &stdout, &stderr); err != nil {
		t.Fatalf("upgrade install: %v\nstderr=%s", err, stderr.String())
	}
	if output := stdout.String(); !strings.Contains(output, hex.EncodeToString(digest[:])) || !strings.Contains(output, target) {
		t.Fatalf("upgrade install output = %s", output)
	}
	if installed, err := os.ReadFile(target); err != nil || string(installed) != "new superscience" {
		t.Fatalf("installed target = %q, %v", installed, err)
	}
}

func TestUninstallCLIPlansBeforeRemovalAndDeletesDataOnlyWhenRequested(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "superscience")
	if err := os.WriteFile(target, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	dataDir := filepath.Join(root, "superscience-data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "superscience.db"), []byte("database"), 0o600); err != nil {
		t.Fatal(err)
	}

	baseArgs := []string{"uninstall", "--target", target, "--data-dir", dataDir, "--remove-data"}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runCommand(context.Background(), baseArgs, &stdout, &stderr); err != nil {
		t.Fatalf("uninstall plan: %v\nstderr=%s", err, stderr.String())
	}
	if output := stdout.String(); !strings.Contains(output, `"confirmationRequired": true`) || !strings.Contains(output, target) {
		t.Fatalf("uninstall plan output = %s", output)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("uninstall plan removed target: %v", err)
	}

	stdout.Reset()
	stderr.Reset()
	if err := runCommand(context.Background(), append(baseArgs, "--yes"), &stdout, &stderr); err != nil {
		t.Fatalf("uninstall execution: %v\nstderr=%s", err, stderr.String())
	}
	if output := stdout.String(); !strings.Contains(output, `"removedInstallation": true`) || !strings.Contains(output, `"removedData": true`) {
		t.Fatalf("uninstall result = %s", output)
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("uninstall target still exists: %v", err)
	}
	if _, err := os.Stat(dataDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("uninstall data still exists: %v", err)
	}
}

func cliReleaseServer(t *testing.T, tag string, assets map[string][]byte) *httptest.Server {
	t.Helper()
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/releases/latest") {
			rows := make([]map[string]any, 0, len(assets))
			for name, payload := range assets {
				rows = append(rows, map[string]any{
					"name": name, "browser_download_url": server.URL + "/assets/" + name, "size": len(payload),
				})
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": tag, "draft": false, "prerelease": false, "assets": rows})
			return
		}
		if name := strings.TrimPrefix(r.URL.Path, "/assets/"); name != r.URL.Path {
			if payload, ok := assets[name]; ok {
				_, _ = w.Write(payload)
				return
			}
		}
		http.NotFound(w, r)
	}))
	return server
}

func cliReleaseArchive(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var compressed bytes.Buffer
	gzipWriter := gzip.NewWriter(&compressed)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return compressed.Bytes()
}

type cliTestSecrets struct{ values map[string]string }

func (s *cliTestSecrets) Store(account, value string) error { s.values[account] = value; return nil }
func (s *cliTestSecrets) Load(account string) (string, error) {
	value, ok := s.values[account]
	if !ok {
		return "", os.ErrNotExist
	}
	return value, nil
}
func (s *cliTestSecrets) Delete(account string) error { delete(s.values, account); return nil }

func TestRunCommandOfflineParityCommands(t *testing.T) {
	t.Setenv("SUPERSCIENCE_DATA_DIR", t.TempDir())
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{name: "generate", args: []string{"generate"}, want: []string{`"openapi"`, "/api/plugins", "/api/runs/{id}/events", "/api/agents/{id}"}},
		{name: "models", args: []string{"models"}, want: []string{"openai", "gpt-5.4", "anthropic"}},
		{name: "agent list", args: []string{"agent", "list"}, want: []string{"research", "biology", "literature-review"}},
		{name: "completion", args: []string{"completion", "zsh"}, want: []string{"_superscience", "generate", "skill"}},
		{name: "doctor", args: []string{"doctor"}, want: []string{"Superscience doctor", "dataDir", "frontend"}},
		{name: "debug paths", args: []string{"debug", "paths"}, want: []string{"dataDir", "workingDir"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			if err := runCommand(context.Background(), tt.args, &stdout, &stderr); err != nil {
				t.Fatalf("runCommand(%v) error: %v\nstderr=%s", tt.args, err, stderr.String())
			}
			got := stdout.String()
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Fatalf("runCommand(%v) output missing %q:\n%s", tt.args, want, got)
				}
			}
		})
	}
}

func TestAgentCLIManagesPersistedCustomAgents(t *testing.T) {
	t.Setenv("SUPERSCIENCE_DATA_DIR", t.TempDir())
	ctx := context.Background()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	createArgs := []string{
		"agent", "create", "chemistry-review",
		"--name", "Chemistry Review",
		"--domain", "chemistry",
		"--description", "Reviews chemistry evidence.",
		"--system-prompt", "Require structure-backed evidence.",
		"--tool", "read", "--tool", "grep",
		"--review", "required",
	}
	if err := runCommand(ctx, createArgs, &stdout, &stderr); err != nil {
		t.Fatalf("create agent: %v\nstderr=%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"id": "chemistry-review"`) || !strings.Contains(stdout.String(), `"systemPrompt": "Require structure-backed evidence."`) {
		t.Fatalf("create output = %s", stdout.String())
	}

	stdout.Reset()
	if err := runCommand(ctx, []string{"agent", "disable", "chemistry-review"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"enabled": false`) {
		t.Fatalf("disable output = %s", stdout.String())
	}
	stdout.Reset()
	if err := runCommand(ctx, []string{"agent", "show", "chemistry-review"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"reviewMode": "required"`) {
		t.Fatalf("show output = %s", stdout.String())
	}
	stdout.Reset()
	if err := runCommand(ctx, []string{"agent", "remove", "chemistry-review"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"removed": true`) {
		t.Fatalf("remove output = %s", stdout.String())
	}
	if err := runCommand(ctx, []string{"agent", "show", "chemistry-review"}, io.Discard, &stderr); err == nil {
		t.Fatal("removed agent remained readable")
	}
}

func TestSkillCLISetEntriesPersistsNamespaceManifest(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("SUPERSCIENCE_DATA_DIR", dataDir)
	for _, name := range []string{"public-evidence", "internal-helper"} {
		dir := filepath.Join(dataDir, "skills", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		content := "---\nname: " + name + "\ndescription: " + name + " instructions.\n---\n# " + name + "\n"
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runCommand(context.Background(), []string{"skill", "set-entries", "user", "public-evidence"}, &stdout, &stderr); err != nil {
		t.Fatalf("skill set-entries: %v\nstderr=%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"namespace": "user"`) || !strings.Contains(stdout.String(), `"public-evidence"`) {
		t.Fatalf("set-entries output = %s", stdout.String())
	}
	records, err := skills.LoadUser()
	if err != nil {
		t.Fatal(err)
	}
	visibility := map[string]bool{}
	for _, record := range records {
		visibility[record.Name] = skills.IsEntry(record)
	}
	if !visibility["public-evidence"] || visibility["internal-helper"] {
		t.Fatalf("visibility = %+v", visibility)
	}
}

func TestACPCLIWritesOnlyJSONRPCToStdout(t *testing.T) {
	t.Setenv("SUPERSCIENCE_DATA_DIR", t.TempDir())
	request := strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\",\"params\":{\"protocolVersion\":1,\"clientCapabilities\":{}}}\n")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := handleACP(context.Background(), nil, request, &stdout, &stderr); err != nil {
		t.Fatalf("acp command: %v\nstderr=%s", err, stderr.String())
	}
	line := strings.TrimSpace(stdout.String())
	if strings.Count(line, "\n") != 0 {
		t.Fatalf("ACP stdout contains non-protocol lines: %q", line)
	}
	var response struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id"`
		Result  struct {
			ProtocolVersion int `json:"protocolVersion"`
			AgentInfo       struct {
				Name string `json:"name"`
			} `json:"agentInfo"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(line), &response); err != nil {
		t.Fatalf("ACP stdout is not JSON-RPC: %v\n%s", err, line)
	}
	if response.JSONRPC != "2.0" || response.ID != 1 || response.Result.ProtocolVersion != 1 || response.Result.AgentInfo.Name != "superscience" {
		t.Fatalf("ACP initialize response = %+v", response)
	}
}

type blockingACPWriter struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
	buffer  bytes.Buffer
}

func (w *blockingACPWriter) Write(payload []byte) (int, error) {
	w.once.Do(func() { close(w.started) })
	<-w.release
	return w.buffer.Write(payload)
}

func TestACPCLIWaitsForFinalResponseWriteBeforeReturning(t *testing.T) {
	t.Setenv("SUPERSCIENCE_DATA_DIR", t.TempDir())
	request := strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\",\"params\":{\"protocolVersion\":1,\"clientCapabilities\":{}}}\n")
	output := &blockingACPWriter{started: make(chan struct{}), release: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		done <- handleACP(context.Background(), nil, request, output, io.Discard)
	}()
	select {
	case <-output.started:
	case <-time.After(time.Second):
		t.Fatal("ACP never attempted the initialize response")
	}
	select {
	case err := <-done:
		t.Fatalf("ACP returned before the response write completed: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(output.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("ACP did not return after the response write completed")
	}
}

var errACPOutputClosed = errors.New("ACP output closed")

type failingACPWriter struct{}

func (failingACPWriter) Write([]byte) (int, error) { return 0, errACPOutputClosed }

func TestACPCLIPropagatesProtocolOutputFailure(t *testing.T) {
	t.Setenv("SUPERSCIENCE_DATA_DIR", t.TempDir())
	request := strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\",\"params\":{\"protocolVersion\":1,\"clientCapabilities\":{}}}\n")
	err := handleACP(context.Background(), nil, request, failingACPWriter{}, io.Discard)
	if !errors.Is(err, errACPOutputClosed) {
		t.Fatalf("ACP output failure = %v", err)
	}
}

func TestCloudOIDCEnvironmentRequiresIssuerAndClientTogether(t *testing.T) {
	t.Setenv("SUPERSCIENCE_CLOUD_OIDC_ISSUER", "https://identity.example.com")
	t.Setenv("SUPERSCIENCE_CLOUD_OIDC_CLIENT_ID", "")
	_, err := cloudOIDCFromEnv(context.Background())
	if err == nil || !strings.Contains(err.Error(), "CLIENT_ID") {
		t.Fatalf("incomplete OIDC configuration error = %v", err)
	}
	t.Setenv("SUPERSCIENCE_CLOUD_OIDC_ISSUER", "")
	authenticator, err := cloudOIDCFromEnv(context.Background())
	if err != nil || authenticator != nil {
		t.Fatalf("empty OIDC configuration = %T, %v", authenticator, err)
	}
}

func TestCloudMetadataRequiresPostgresUnlessDevelopmentSQLiteIsExplicit(t *testing.T) {
	t.Setenv("SUPERSCIENCE_CLOUD_DATABASE_URL", "")
	t.Setenv("SUPERSCIENCE_DATA_DIR", t.TempDir())
	_, _, err := openCloudMetadataStore(context.Background(), false)
	if err == nil || !strings.Contains(err.Error(), "SUPERSCIENCE_CLOUD_DATABASE_URL") {
		t.Fatalf("missing PostgreSQL error = %v", err)
	}
	metadata, closeStore, err := openCloudMetadataStore(context.Background(), true)
	if err != nil || metadata == nil || closeStore == nil {
		t.Fatalf("development SQLite metadata = %T, close=%v, err=%v", metadata, closeStore != nil, err)
	}
	if err := closeStore(); err != nil {
		t.Fatal(err)
	}
}

func TestCloudObjectStorageRequiresS3UnlessDevelopmentSQLiteIsExplicit(t *testing.T) {
	for _, name := range []string{
		"SUPERSCIENCE_CLOUD_S3_ENDPOINT", "SUPERSCIENCE_CLOUD_S3_BUCKET", "SUPERSCIENCE_CLOUD_S3_ACCESS_KEY", "SUPERSCIENCE_CLOUD_S3_SECRET_KEY",
	} {
		t.Setenv(name, "")
	}
	_, err := openCloudObjectStore(context.Background(), false)
	if err == nil || !strings.Contains(err.Error(), "SUPERSCIENCE_CLOUD_S3_ENDPOINT") {
		t.Fatalf("missing S3 error = %v", err)
	}
	objects, err := openCloudObjectStore(context.Background(), true)
	if err != nil || objects != nil {
		t.Fatalf("development inline object storage = %T, %v", objects, err)
	}
	t.Setenv("SUPERSCIENCE_CLOUD_S3_ENDPOINT", "https://objects.example.com")
	_, err = openCloudObjectStore(context.Background(), true)
	if err == nil || !strings.Contains(err.Error(), "S3_BUCKET") {
		t.Fatalf("partial S3 configuration error = %v", err)
	}
}

func TestSkillCLIUsesUserSkillDirectory(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("SUPERSCIENCE_DATA_DIR", dataDir)
	skillDir := filepath.Join(dataDir, "skills", "user-evidence")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: user-evidence\ndescription: User evidence skill.\n---\n# User Evidence\nUse user-level evidence from CLI.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runCommand(context.Background(), []string{"skill", "list"}, &stdout, &stderr); err != nil {
		t.Fatalf("skill list error: %v\nstderr=%s", err, stderr.String())
	}
	if got := stdout.String(); !strings.Contains(got, "user-evidence") || !strings.Contains(got, "User evidence skill") {
		t.Fatalf("skill list missing user skill:\n%s", got)
	}

	stdout.Reset()
	stderr.Reset()
	if err := runCommand(context.Background(), []string{"skill", "show", "user-evidence"}, &stdout, &stderr); err != nil {
		t.Fatalf("skill show error: %v\nstderr=%s", err, stderr.String())
	}
	if got := stdout.String(); !strings.Contains(got, "Use user-level evidence from CLI.") {
		t.Fatalf("skill show missing user skill content:\n%s", got)
	}
}

func TestSkillCLINewInstallAndRemoveChangeUserSkillDirectory(t *testing.T) {
	t.Setenv("SUPERSCIENCE_DATA_DIR", t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runCommand(context.Background(), []string{"skill", "new", "Evidence Audit", "Audit evidence provenance."}, &stdout, &stderr); err != nil {
		t.Fatalf("skill new: %v", err)
	}
	if !strings.Contains(stdout.String(), "evidence-audit") {
		t.Fatalf("skill new output = %s", stdout.String())
	}
	created := filepath.Join(os.Getenv("SUPERSCIENCE_DATA_DIR"), "skills", "evidence-audit", "SKILL.md")
	if _, err := os.Stat(created); err != nil {
		t.Fatalf("created skill: %v", err)
	}

	sourceDir := t.TempDir()
	source := filepath.Join(sourceDir, "SKILL.md")
	if err := os.WriteFile(source, []byte("---\nname: install-audit\ndescription: Installed audit skill.\n---\n# Install Audit\n\nVerify source records.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	if err := runCommand(context.Background(), []string{"skill", "add", source}, &stdout, &stderr); err != nil {
		t.Fatalf("skill add: %v", err)
	}
	if !strings.Contains(stdout.String(), "install-audit") {
		t.Fatalf("skill add output = %s", stdout.String())
	}

	stdout.Reset()
	if err := runCommand(context.Background(), []string{"skill", "remove", "evidence-audit"}, &stdout, &stderr); err != nil {
		t.Fatalf("skill remove: %v", err)
	}
	if _, err := os.Stat(created); !os.IsNotExist(err) {
		t.Fatalf("skill remove retained %s: %v", created, err)
	}
}

func TestModelsRefreshUsesEndpoint(t *testing.T) {
	t.Setenv("SUPERSCIENCE_DATA_DIR", t.TempDir())
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api.json" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		payload := map[string]any{
			"openai": map[string]any{
				"name": "OpenAI",
				"models": map[string]any{
					"gpt-cli-catalog": map[string]any{
						"name": "GPT CLI Catalog",
						"limit": map[string]any{
							"context": 64000,
						},
						"tool_call": true,
					},
				},
			},
		}
		if err := json.NewEncoder(w).Encode(payload); err != nil {
			t.Fatal(err)
		}
	}))
	defer api.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runCommand(context.Background(), []string{"models", "refresh", "--endpoint", api.URL + "/api.json"}, &stdout, &stderr); err != nil {
		t.Fatalf("models refresh error: %v\nstderr=%s", err, stderr.String())
	}
	got := stdout.String()
	for _, want := range []string{"source=" + api.URL + "/api.json", "providers=1", "models=1", "gpt-cli-catalog", "64000"} {
		if !strings.Contains(got, want) {
			t.Fatalf("models refresh output missing %q:\n%s", want, got)
		}
	}
}

func TestKeysCLIStoresMetadataAndSecretThenRemovesBoth(t *testing.T) {
	t.Setenv("SUPERSCIENCE_DATA_DIR", t.TempDir())
	t.Setenv("CLI_PROVIDER_KEY", "protocol-secret-value")
	backend := &cliTestSecrets{values: map[string]string{}}
	previous := cliSecrets
	cliSecrets = backend
	defer func() { cliSecrets = previous }()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runCommand(context.Background(), []string{"keys", "add", "protocol-provider", "--key-env", "CLI_PROVIDER_KEY", "--label", "Protocol Provider"}, &stdout, &stderr); err != nil {
		t.Fatalf("keys add: %v", err)
	}
	if backend.values["protocol-provider"] != "protocol-secret-value" {
		t.Fatal("keys add did not store the provider secret")
	}
	if output := stdout.String(); !strings.Contains(output, "protocol-provider") || strings.Contains(output, "protocol-secret-value") {
		t.Fatalf("keys add output = %s", output)
	}

	stdout.Reset()
	if err := runCommand(context.Background(), []string{"keys", "list"}, &stdout, &stderr); err != nil {
		t.Fatalf("keys list: %v", err)
	}
	if !strings.Contains(stdout.String(), "Protocol Provider") {
		t.Fatalf("keys list output = %s", stdout.String())
	}

	stdout.Reset()
	if err := runCommand(context.Background(), []string{"keys", "rm", "protocol-provider"}, &stdout, &stderr); err != nil {
		t.Fatalf("keys rm: %v", err)
	}
	if _, ok := backend.values["protocol-provider"]; ok {
		t.Fatal("keys rm retained the provider secret")
	}
}

func TestMCPCLIAddsListsAndRemovesStructuredServer(t *testing.T) {
	t.Setenv("SUPERSCIENCE_DATA_DIR", t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runCommand(context.Background(), []string{"mcp", "add", "protocol-server", "--command", "/usr/bin/true", "--arg", "--stdio"}, &stdout, &stderr); err != nil {
		t.Fatalf("mcp add: %v", err)
	}
	if output := stdout.String(); !strings.Contains(output, "protocol-server") || !strings.Contains(output, "--stdio") {
		t.Fatalf("mcp add output = %s", output)
	}

	stdout.Reset()
	if err := runCommand(context.Background(), []string{"mcp", "list"}, &stdout, &stderr); err != nil {
		t.Fatalf("mcp list: %v", err)
	}
	if !strings.Contains(stdout.String(), "protocol-server") {
		t.Fatalf("mcp list output = %s", stdout.String())
	}

	stdout.Reset()
	if err := runCommand(context.Background(), []string{"mcp", "remove", "protocol-server"}, &stdout, &stderr); err != nil {
		t.Fatalf("mcp remove: %v", err)
	}
	stdout.Reset()
	if err := runCommand(context.Background(), []string{"mcp", "list"}, &stdout, &stderr); err != nil {
		t.Fatalf("mcp list after remove: %v", err)
	}
	if strings.Contains(stdout.String(), "protocol-server") {
		t.Fatalf("removed MCP server remained: %s", stdout.String())
	}
}

func TestRunCLIUsesConfiguredProviderAndPersistsRun(t *testing.T) {
	dataDir := t.TempDir()
	projectDir := t.TempDir()
	t.Setenv("SUPERSCIENCE_DATA_DIR", dataDir)
	t.Setenv("SUPERSCIENCE_API_KEY", "cli-protocol-key")
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("provider path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer cli-protocol-key" {
			t.Fatalf("provider authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"CLI provider answer\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2,\"total_tokens\":5}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer provider.Close()

	db, err := openDefaultStore(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.UpsertProvider(context.Background(), store.ProviderProfile{
		ID: "cli-provider", DisplayName: "CLI Provider", Kind: "openai-compatible", BaseURL: provider.URL + "/v1", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err = runCommand(context.Background(), []string{"run", "--project", projectDir, "--provider", "cli-provider", "--model", "cli-model", "Verify the CLI runtime."}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run command: %v\nstderr=%s", err, stderr.String())
	}
	if output := stdout.String(); !strings.Contains(output, "CLI provider answer") || !strings.Contains(output, `"status": "completed"`) {
		t.Fatalf("run output = %s", output)
	}

	db, err = openDefaultStore(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	projects, err := db.ListProjects(context.Background())
	if err != nil || len(projects) != 1 {
		t.Fatalf("projects = %+v err=%v", projects, err)
	}
	sessions, err := db.ListSessions(context.Background(), projects[0].ID)
	if err != nil || len(sessions) != 1 {
		t.Fatalf("sessions = %+v err=%v", sessions, err)
	}
	runs, err := db.ListAgentRuns(context.Background(), sessions[0].ID)
	if err != nil || len(runs) != 1 || runs[0].Status != "completed" {
		t.Fatalf("runs = %+v err=%v", runs, err)
	}
}

func TestExportAndImportCLIUseShareBundleAndProvenance(t *testing.T) {
	t.Setenv("SUPERSCIENCE_DATA_DIR", t.TempDir())
	ctx := context.Background()
	db, err := openDefaultStore(ctx)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := db.CreateArtifact(ctx, store.Artifact{Type: "markdown", Title: "Evidence", Content: "# Verified evidence", Metadata: `{"source":"cli-test"}`})
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := sharepkg.BuildBundle(sharepkg.PublishRequest{Title: "CLI bundle", Visibility: "private-link", ArtifactIDs: []string{artifact.ID}}, []store.Artifact{artifact})
	if err != nil {
		t.Fatal(err)
	}
	shared, err := db.CreateShare(ctx, store.Share{Title: "CLI bundle", Visibility: "private-link", Status: "published", Bundle: bundle})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	outputPath := filepath.Join(t.TempDir(), "cli-bundle.json")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runCommand(ctx, []string{"export", shared.ID, "--output", outputPath}, &stdout, &stderr); err != nil {
		t.Fatalf("export: %v\nstderr=%s", err, stderr.String())
	}
	exported, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(exported), `"schema": "superscience.share.v1"`) || !strings.Contains(string(exported), "Verified evidence") {
		t.Fatalf("exported bundle = %s", exported)
	}

	stdout.Reset()
	if err := runCommand(ctx, []string{"import", outputPath}, &stdout, &stderr); err != nil {
		t.Fatalf("import: %v\nstderr=%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"status": "published"`) {
		t.Fatalf("import output = %s", stdout.String())
	}
	db, err = openDefaultStore(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	shares, err := db.ListShares(ctx)
	if err != nil || len(shares) != 2 {
		t.Fatalf("shares = %+v err=%v", shares, err)
	}
	artifacts, err := db.ListArtifacts(ctx)
	if err != nil || len(artifacts) != 2 {
		t.Fatalf("artifacts = %+v err=%v", artifacts, err)
	}
	edges, err := db.ListProvenance(ctx, shares[0].ID)
	if err != nil || len(edges) == 0 || edges[0].Relation != "imported" {
		t.Fatalf("provenance = %+v err=%v", edges, err)
	}
}
