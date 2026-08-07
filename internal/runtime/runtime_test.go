package runtime

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"superscience/internal/store"
)

func TestInstanceCloseIsIdempotentAndReleasesSQLite(t *testing.T) {
	dataDir := t.TempDir()
	projectDir := t.TempDir()
	t.Setenv("SUPERSCIENCE_DATA_DIR", dataDir)
	runtimeCtx, cancelRuntime := context.WithCancel(context.Background())
	instance, err := Start(runtimeCtx, Options{Port: 0, ProjectRoot: projectDir})
	if err != nil {
		t.Fatal(err)
	}
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelShutdown()
	if err := instance.Close(shutdownCtx); err != nil {
		t.Fatal(err)
	}
	if err := instance.Close(shutdownCtx); err != nil {
		t.Fatal(err)
	}
	cancelRuntime()

	db, err := store.Open(context.Background(), filepath.Join(dataDir, "superscience.db"))
	if err != nil {
		t.Fatalf("reopen after close: %v", err)
	}
	defer db.Close()
	if _, err := db.ListProjects(context.Background()); err != nil {
		t.Fatalf("query after reopen: %v", err)
	}
}
