package notebook_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"superscience/internal/notebook"
	"superscience/internal/store"
)

func TestManagerPreservesPythonStateAcrossCells(t *testing.T) {
	ctx := context.Background()
	manager := notebook.NewManager()
	t.Cleanup(func() { manager.Close() })

	first, err := manager.RunCell(ctx, store.NotebookCell{
		ID:         "cell_1",
		NotebookID: "notebook_1",
		Kind:       "code",
		Language:   "python",
		Source:     "x = 41",
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != "ok" {
		t.Fatalf("first cell = %+v", first)
	}

	second, err := manager.RunCell(ctx, store.NotebookCell{
		ID:         "cell_2",
		NotebookID: "notebook_1",
		Kind:       "code",
		Language:   "python",
		Source:     "from IPython.display import HTML, display\ndisplay(HTML('<b>EGFR</b>'))\nprint(x + 1)",
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Status != "ok" || strings.TrimSpace(second.Stdout) != "42" {
		t.Fatalf("second cell = %+v", second)
	}
	if second.Kernel != "python3" || len(second.Outputs) < 2 {
		t.Fatalf("Jupyter kernel metadata or rich outputs missing: %+v", second)
	}
	foundHTML := false
	for _, output := range second.Outputs {
		if data, ok := output.Data["text/html"]; ok && strings.Contains(fmt.Sprint(data), "EGFR") {
			foundHTML = true
		}
	}
	if !foundHTML {
		t.Fatalf("rich HTML output missing: %+v", second.Outputs)
	}
}

func TestManagerInterruptStopsRunningKernelAndAllowsFreshSession(t *testing.T) {
	manager := notebook.NewManager()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	startedPath := filepath.Join(t.TempDir(), "cell-started")
	t.Cleanup(func() {
		cancel()
		manager.Close()
	})
	resultCh := make(chan error, 1)
	go func() {
		_, err := manager.RunCell(ctx, store.NotebookCell{
			ID:         "cell_sleep",
			NotebookID: "notebook_interrupt",
			Kind:       "code",
			Language:   "python",
			Source:     fmt.Sprintf("from pathlib import Path\nimport time\nPath(%q).write_text('started')\ntime.sleep(30)", startedPath),
		})
		resultCh <- err
	}()
	// Wait for the cell itself to start, separately from the interrupt deadline.
	// A running kernel can still be importing its environment on a cold CI host.
	readyDeadline := time.NewTimer(30 * time.Second)
	defer readyDeadline.Stop()
	poll := time.NewTicker(50 * time.Millisecond)
	defer poll.Stop()
	for {
		if _, err := os.Stat(startedPath); err == nil {
			break
		}
		select {
		case err := <-resultCh:
			t.Fatalf("cell returned before interrupt: %v", err)
		case <-readyDeadline.C:
			t.Fatal("cell did not start within the kernel readiness deadline")
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-poll.C:
		}
	}
	if manager.Status("notebook_interrupt", "python").Status != "running" {
		t.Fatal("kernel did not enter running state")
	}
	if err := manager.Interrupt("notebook_interrupt", "python"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-resultCh:
		if err == nil {
			t.Fatal("interrupted kernel execution returned success")
		}
	case <-time.After(4 * time.Second):
		t.Fatal("kernel execution did not stop after interrupt")
	}
	fresh, err := manager.RunCell(ctx, store.NotebookCell{
		ID:         "cell_fresh",
		NotebookID: "notebook_interrupt",
		Kind:       "code",
		Language:   "python",
		Source:     "print('fresh')",
	})
	if err != nil || strings.TrimSpace(fresh.Stdout) != "fresh" {
		t.Fatalf("fresh kernel result=%+v err=%v", fresh, err)
	}
}
