package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunPackagesReleaseBinary(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, "input")
	if err := os.WriteFile(binary, []byte("release binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{"--version", "v1.2.3", "--goos", "linux", "--goarch", "amd64", "--binary", binary, "--output", root}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run: %v\nstderr=%s", err, stderr.String())
	}
	if output := stdout.String(); !strings.Contains(output, "superscience_1.2.3_linux_amd64.tar.gz") || !strings.Contains(output, `"sha256"`) {
		t.Fatalf("releasepack output = %s", output)
	}
}

func TestRunRequiresAllReleaseCoordinates(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run(nil, &stdout, &stderr); err == nil || !strings.Contains(err.Error(), "version") {
		t.Fatalf("missing flags error = %v", err)
	}
}
