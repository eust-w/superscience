package permissions_test

import (
	"testing"

	"superscience/internal/permissions"
)

func TestPermissionManagerApprovesAndRejectsPendingRequests(t *testing.T) {
	manager := permissions.NewManager()

	request := manager.Ask(permissions.Request{
		SessionID: "session_1",
		Type:      "shell",
		Pattern:   "python scripts/run.py",
		Message:   "Approve command",
	})

	if len(manager.Pending()) != 1 {
		t.Fatalf("pending = %+v", manager.Pending())
	}

	if err := manager.Respond(request.ID, permissions.DecisionAllowOnce); err != nil {
		t.Fatalf("allow: %v", err)
	}
	if !manager.IsAllowed("session_1", "shell", "python scripts/run.py") {
		t.Fatal("expected shell command to be allowed once")
	}

	rejected := manager.Ask(permissions.Request{SessionID: "session_1", Type: "write", Pattern: "README.md"})
	if err := manager.Respond(rejected.ID, permissions.DecisionReject); err != nil {
		t.Fatalf("reject: %v", err)
	}
	if manager.IsAllowed("session_1", "write", "README.md") {
		t.Fatal("expected write to be rejected")
	}
}
