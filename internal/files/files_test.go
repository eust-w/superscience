package files_test

import (
	"os"
	"path/filepath"
	"testing"

	"superscience/internal/files"
)

func TestResolveProjectPathRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := files.ResolveProjectPath(root, "../secret.txt"); err == nil {
		t.Fatal("expected traversal to be rejected")
	}
}

func TestReadWriteStayInsideProjectRoot(t *testing.T) {
	root := t.TempDir()

	if err := files.WriteText(root, "notes/plan.md", "screen kinases"); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := files.ReadText(root, "notes/plan.md")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got != "screen kinases" {
		t.Fatalf("content = %q", got)
	}
}
