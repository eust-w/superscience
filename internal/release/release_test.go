package release

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestClientCheckSelectsPlatformArchive(t *testing.T) {
	archiveName := ArchiveName("v1.2.3", "darwin", "arm64")
	server := releaseFixture(t, "v1.2.3", map[string][]byte{
		archiveName:     []byte("archive"),
		"checksums.txt": []byte(strings.Repeat("0", 64) + "  " + archiveName + "\n"),
	})
	defer server.Close()

	client := Client{Repository: "synthetic-sciences/superscience", APIBaseURL: server.URL, HTTPClient: server.Client(), GOOS: "darwin", GOARCH: "arm64"}
	result, err := client.Check(context.Background(), "v1.2.2")
	if err != nil {
		t.Fatal(err)
	}
	if !result.UpdateAvailable || result.LatestVersion != "v1.2.3" {
		t.Fatalf("check result = %+v", result)
	}
	if result.Asset.Name != archiveName || result.ChecksumAsset.Name != "checksums.txt" {
		t.Fatalf("selected assets = %+v / %+v", result.Asset, result.ChecksumAsset)
	}
}

func TestClientCheckRejectsReleaseWithoutChecksumAsset(t *testing.T) {
	archiveName := ArchiveName("v1.2.3", "linux", "amd64")
	server := releaseFixture(t, "v1.2.3", map[string][]byte{archiveName: []byte("archive")})
	defer server.Close()

	client := Client{Repository: "owner/repo", APIBaseURL: server.URL, HTTPClient: server.Client(), GOOS: "linux", GOARCH: "amd64"}
	_, err := client.Check(context.Background(), "v1.2.2")
	if err == nil || !strings.Contains(err.Error(), "checksums.txt") {
		t.Fatalf("missing checksum error = %v", err)
	}
}

func TestClientCheckRejectsUnsafeRepositoryAndRemotePlainHTTP(t *testing.T) {
	for _, client := range []Client{
		{Repository: "owner/repo?ref=other", APIBaseURL: "https://api.github.com", GOOS: "linux", GOARCH: "amd64"},
		{Repository: "owner/repo", APIBaseURL: "http://downloads.example.com", GOOS: "linux", GOARCH: "amd64"},
	} {
		if _, err := client.Check(context.Background(), "v1.2.2"); err == nil {
			t.Fatalf("unsafe release client succeeded: %+v", client)
		}
	}
}

func TestClientCheckRejectsPrereleaseMetadataAndInsecureAssetURL(t *testing.T) {
	archiveName := ArchiveName("v1.2.3", "linux", "amd64")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name": "v1.2.3", "draft": false, "prerelease": true,
			"assets": []map[string]any{
				{"name": archiveName, "browser_download_url": "http://downloads.example.com/archive", "size": 10},
				{"name": "checksums.txt", "browser_download_url": "http://downloads.example.com/checksums", "size": 80},
			},
		})
	}))
	defer server.Close()
	client := Client{Repository: "owner/repo", APIBaseURL: server.URL, HTTPClient: server.Client(), GOOS: "linux", GOARCH: "amd64"}
	if _, err := client.Check(context.Background(), "v1.2.2"); err == nil || !strings.Contains(err.Error(), "prerelease") {
		t.Fatalf("prerelease metadata error = %v", err)
	}

	insecureServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name": "v1.2.3", "draft": false, "prerelease": false,
			"assets": []map[string]any{
				{"name": archiveName, "browser_download_url": "http://downloads.example.com/archive", "size": 10},
				{"name": "checksums.txt", "browser_download_url": "http://downloads.example.com/checksums", "size": 80},
			},
		})
	}))
	defer insecureServer.Close()
	insecureClient := Client{Repository: "owner/repo", APIBaseURL: insecureServer.URL, HTTPClient: insecureServer.Client(), GOOS: "linux", GOARCH: "amd64"}
	if _, err := insecureClient.Check(context.Background(), "v1.2.2"); err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("insecure asset URL error = %v", err)
	}
}

func TestClientUpgradeVerifiesChecksumAndAtomicallyReplacesBinary(t *testing.T) {
	archiveName := ArchiveName("v1.2.3", "linux", "amd64")
	archive := tarGzipFixture(t, "superscience", []byte("new executable"))
	digest := sha256.Sum256(archive)
	server := releaseFixture(t, "v1.2.3", map[string][]byte{
		archiveName:     archive,
		"checksums.txt": []byte(hex.EncodeToString(digest[:]) + "  " + archiveName + "\n"),
	})
	defer server.Close()

	target := filepath.Join(t.TempDir(), "superscience")
	if err := os.WriteFile(target, []byte("old executable"), 0o755); err != nil {
		t.Fatal(err)
	}
	client := Client{Repository: "owner/repo", APIBaseURL: server.URL, HTTPClient: server.Client(), GOOS: "linux", GOARCH: "amd64"}
	result, err := client.Upgrade(context.Background(), "v1.2.2", target)
	if err != nil {
		t.Fatal(err)
	}
	if result.Target != target || result.SHA256 != hex.EncodeToString(digest[:]) {
		t.Fatalf("upgrade result = %+v", result)
	}
	installed, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(installed) != "new executable" {
		t.Fatalf("installed binary = %q", installed)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("installed mode = %v", info.Mode())
	}
}

func TestClientUpgradeRejectsChecksumMismatchWithoutChangingBinary(t *testing.T) {
	archiveName := ArchiveName("v1.2.3", "linux", "amd64")
	archive := tarGzipFixture(t, "superscience", []byte("untrusted executable"))
	server := releaseFixture(t, "v1.2.3", map[string][]byte{
		archiveName:     archive,
		"checksums.txt": []byte(strings.Repeat("0", 64) + "  " + archiveName + "\n"),
	})
	defer server.Close()

	target := filepath.Join(t.TempDir(), "superscience")
	if err := os.WriteFile(target, []byte("trusted executable"), 0o755); err != nil {
		t.Fatal(err)
	}
	client := Client{Repository: "owner/repo", APIBaseURL: server.URL, HTTPClient: server.Client(), GOOS: "linux", GOARCH: "amd64"}
	_, err := client.Upgrade(context.Background(), "v1.2.2", target)
	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("checksum mismatch error = %v", err)
	}
	installed, readErr := os.ReadFile(target)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(installed) != "trusted executable" {
		t.Fatalf("target changed after failed verification: %q", installed)
	}
}

func TestExtractExecutableRejectsArchiveTraversal(t *testing.T) {
	archive := tarGzipFixture(t, "../superscience", []byte("escape"))
	_, _, err := extractExecutable(archive, "linux")
	if err == nil || !strings.Contains(err.Error(), "unsafe") {
		t.Fatalf("archive traversal error = %v", err)
	}

	var zipped bytes.Buffer
	writer := zip.NewWriter(&zipped)
	entry, err := writer.Create("../superscience.exe")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = entry.Write([]byte("escape"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	_, _, err = extractExecutable(zipped.Bytes(), "windows")
	if err == nil || !strings.Contains(err.Error(), "unsafe") {
		t.Fatalf("zip traversal error = %v", err)
	}
}

func TestExtractExecutableRejectsZipSymlink(t *testing.T) {
	var zipped bytes.Buffer
	writer := zip.NewWriter(&zipped)
	header := &zip.FileHeader{Name: "superscience.exe"}
	header.SetMode(os.ModeSymlink | 0o777)
	entry, err := writer.CreateHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = entry.Write([]byte("another-file.exe"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	_, _, err = extractExecutable(zipped.Bytes(), "windows")
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("zip symlink error = %v", err)
	}
}

func TestUninstallRequiresConfirmationAndPreservesDataByDefault(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(binDir, executableName(runtime.GOOS))
	if err := os.WriteFile(target, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	dataDir := filepath.Join(root, "superscience")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "superscience.db"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := Uninstall(UninstallOptions{Target: target, DataDir: dataDir})
	if !errors.Is(err, ErrConfirmationRequired) {
		t.Fatalf("unconfirmed uninstall error = %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("target removed without confirmation: %v", err)
	}

	result, err := Uninstall(UninstallOptions{Target: target, DataDir: dataDir, Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.RemovedData {
		t.Fatalf("data was removed by default: %+v", result)
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("target still exists after uninstall: %v", err)
	}
	if _, err := os.Stat(dataDir); err != nil {
		t.Fatalf("data directory should be preserved: %v", err)
	}
}

func TestUninstallRemovesValidatedDataOnlyWhenRequested(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, executableName(runtime.GOOS))
	if err := os.WriteFile(target, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	dataDir := filepath.Join(root, "research-data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "superscience.db"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := Uninstall(UninstallOptions{Target: target, DataDir: dataDir, RemoveData: true, Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	if !result.RemovedData {
		t.Fatalf("uninstall result = %+v", result)
	}
	if _, err := os.Stat(dataDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("data directory still exists: %v", err)
	}
}

func TestUninstallRefusesUnrecognizedInstallationAndUnsafeDataPaths(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "unrelated-tool")
	if err := os.WriteFile(target, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := PlanUninstall(UninstallOptions{Target: target}); err == nil || !strings.Contains(err.Error(), "Superscience") {
		t.Fatalf("unrecognized target error = %v", err)
	}

	recognized := filepath.Join(root, executableName(runtime.GOOS))
	if err := os.WriteFile(recognized, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := PlanUninstall(UninstallOptions{Target: recognized, DataDir: root, RemoveData: true}); err == nil || !strings.Contains(err.Error(), "data directory") {
		t.Fatalf("unsafe data directory error = %v", err)
	}
}

func TestUninstallRefusesDataDirectoryThroughSymlinkedParent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on some Windows runners")
	}
	root := t.TempDir()
	target := filepath.Join(root, executableName(runtime.GOOS))
	if err := os.WriteFile(target, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	realParent := filepath.Join(root, "real")
	realData := filepath.Join(realParent, "superscience")
	if err := os.MkdirAll(realData, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realData, "superscience.db"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	linkedParent := filepath.Join(root, "linked")
	if err := os.Symlink(realParent, linkedParent); err != nil {
		t.Fatal(err)
	}
	_, err := PlanUninstall(UninstallOptions{Target: target, DataDir: filepath.Join(linkedParent, "superscience"), RemoveData: true})
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlinked data directory error = %v", err)
	}
}

func releaseFixture(t *testing.T, tag string, assets map[string][]byte) *httptest.Server {
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

func tarGzipFixture(t *testing.T, name string, content []byte) []byte {
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
