package release

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackageBinaryCreatesUpdaterCompatibleTarAndChecksum(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, "input")
	content := []byte("linux executable")
	if err := os.WriteFile(binary, content, 0o755); err != nil {
		t.Fatal(err)
	}

	result, err := PackageBinary(PackageOptions{Version: "v1.2.3", GOOS: "linux", GOARCH: "amd64", BinaryPath: binary, OutputDir: root})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(result.ArchivePath) != "superscience_1.2.3_linux_amd64.tar.gz" {
		t.Fatalf("archive path = %s", result.ArchivePath)
	}
	archive, err := os.ReadFile(result.ArchivePath)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(archive)
	if result.SHA256 != hex.EncodeToString(digest[:]) {
		t.Fatalf("result checksum = %s", result.SHA256)
	}
	sidecar, err := os.ReadFile(result.ChecksumPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(sidecar) != result.SHA256+"  "+filepath.Base(result.ArchivePath)+"\n" {
		t.Fatalf("checksum sidecar = %q", sidecar)
	}

	gzipReader, err := gzip.NewReader(strings.NewReader(string(archive)))
	if err != nil {
		t.Fatal(err)
	}
	tarReader := tar.NewReader(gzipReader)
	header, err := tarReader.Next()
	if err != nil {
		t.Fatal(err)
	}
	if header.Name != "superscience" || header.Mode&0o111 == 0 {
		t.Fatalf("tar header = %+v", header)
	}
	packaged, err := io.ReadAll(tarReader)
	if err != nil {
		t.Fatal(err)
	}
	if string(packaged) != string(content) {
		t.Fatalf("tar executable = %q", packaged)
	}
}

func TestPackageBinaryCreatesUpdaterCompatibleWindowsZip(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, "input.exe")
	if err := os.WriteFile(binary, []byte("windows executable"), 0o755); err != nil {
		t.Fatal(err)
	}
	result, err := PackageBinary(PackageOptions{Version: "1.2.3", GOOS: "windows", GOARCH: "arm64", BinaryPath: binary, OutputDir: root})
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.OpenReader(result.ArchivePath)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	if len(archive.File) != 1 || archive.File[0].Name != "superscience.exe" {
		t.Fatalf("zip files = %+v", archive.File)
	}
	entry, err := archive.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	packaged, err := io.ReadAll(entry)
	_ = entry.Close()
	if err != nil || string(packaged) != "windows executable" {
		t.Fatalf("zip executable = %q, %v", packaged, err)
	}
}

func TestPackageBinaryRejectsInvalidReleaseCoordinates(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "superscience")
	if err := os.WriteFile(binary, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, options := range []PackageOptions{
		{Version: "latest", GOOS: "linux", GOARCH: "amd64", BinaryPath: binary, OutputDir: t.TempDir()},
		{Version: "1.2.3", GOOS: "../linux", GOARCH: "amd64", BinaryPath: binary, OutputDir: t.TempDir()},
		{Version: "1.2.3", GOOS: "linux", GOARCH: "amd64/escape", BinaryPath: binary, OutputDir: t.TempDir()},
	} {
		if _, err := PackageBinary(options); err == nil {
			t.Fatalf("PackageBinary(%+v) succeeded", options)
		}
	}
}
