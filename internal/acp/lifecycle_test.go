package acp

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	acpsdk "github.com/coder/acp-go-sdk"
)

func TestPromptCancellationGenerationDoesNotClearNewerPrompt(t *testing.T) {
	agent := New(Options{})
	agent.sessions["session-1"] = &sessionState{}

	firstCtx, firstCancel := context.WithCancel(context.Background())
	firstGeneration := agent.setSessionCancel("session-1", firstCancel)
	secondCtx, secondCancel := context.WithCancel(context.Background())
	secondGeneration := agent.setSessionCancel("session-1", secondCancel)
	defer secondCancel()

	select {
	case <-firstCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("installing a newer prompt did not cancel the previous prompt")
	}
	agent.clearSessionCancel("session-1", firstGeneration)
	if err := agent.Cancel(context.Background(), acpsdk.CancelNotification{SessionId: "session-1"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-secondCtx.Done():
	case <-time.After(time.Second):
		t.Fatalf("clearing generation %d removed newer generation %d", firstGeneration, secondGeneration)
	}
}

func TestServeRejectsMissingStore(t *testing.T) {
	err := Serve(context.Background(), strings.NewReader(""), io.Discard, Options{})
	if err == nil || !strings.Contains(err.Error(), "store") {
		t.Fatalf("Serve without store error = %v", err)
	}
}
