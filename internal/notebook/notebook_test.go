package notebook_test

import (
	"context"
	"fmt"
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
	t.Cleanup(func() { manager.Close() })
	resultCh := make(chan error, 1)
	go func() {
		_, err := manager.RunCell(context.Background(), store.NotebookCell{
			ID:         "cell_sleep",
			NotebookID: "notebook_interrupt",
			Kind:       "code",
			Language:   "python",
			Source:     "import time\ntime.sleep(30)",
		})
		resultCh <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for manager.Status("notebook_interrupt", "python").Status != "running" && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
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
	fresh, err := manager.RunCell(context.Background(), store.NotebookCell{
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
