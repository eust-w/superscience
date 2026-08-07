package lsp_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"superscience/internal/lsp"
)

func TestManagerUsesExternalLSPServerDiagnostics(t *testing.T) {
	if os.Getenv("SUPERSCIENCE_FAKE_LSP") == "1" {
		runFakeLSPServer()
		os.Exit(0)
	}

	manager := lsp.NewManager()
	manager.RegisterServer("go", lsp.ServerConfig{
		Command: os.Args[0],
		Args:    []string{"-test.run=TestManagerUsesExternalLSPServerDiagnostics"},
		Env:     []string{"SUPERSCIENCE_FAKE_LSP=1"},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	session, err := manager.Analyze(ctx, lsp.Request{
		Language: "go",
		Path:     "/tmp/superscience/main.go",
		Text:     "package main\nfunc main() {}\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(session.Diagnostics) != 1 || session.Diagnostics[0].Message != "fake lsp diagnostic" {
		t.Fatalf("diagnostics = %+v", session.Diagnostics)
	}
	if session.Hover != "fake hover" {
		t.Fatalf("hover = %q", session.Hover)
	}
	if len(session.Completions) != 1 || session.Completions[0] != "fakeCompletion" {
		t.Fatalf("completions = %+v", session.Completions)
	}
	if len(session.Definitions) != 1 || session.Definitions[0].Path != "/tmp/superscience/main.go" || session.Definitions[0].Line != 2 {
		t.Fatalf("definitions = %+v", session.Definitions)
	}
}

func TestManagerReturnsUnavailableWithoutConfiguredLanguageServer(t *testing.T) {
	manager := lsp.NewManager()
	session, err := manager.Analyze(context.Background(), lsp.Request{
		Language: "unsupported-science-language",
		Path:     "/tmp/superscience/analysis.science",
		Text:     "unbalanced { content",
	})
	if err == nil || !strings.Contains(err.Error(), "language server") {
		t.Fatalf("missing server error = %v", err)
	}
	if len(session.Diagnostics) != 0 || session.Hover != "" || len(session.Completions) != 0 || len(session.Definitions) != 0 {
		t.Fatalf("unavailable server returned heuristic data: %+v", session)
	}
}

func runFakeLSPServer() {
	reader := bufio.NewReader(os.Stdin)
	for {
		message, err := readLSPMessage(reader)
		if err != nil {
			return
		}
		var request struct {
			ID     any             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		_ = json.Unmarshal(message, &request)
		switch request.Method {
		case "initialize":
			writeLSPMessage(map[string]any{
				"jsonrpc": "2.0",
				"id":      request.ID,
				"result": map[string]any{"capabilities": map[string]any{
					"textDocumentSync": 1,
					"hoverProvider":    true,
					"completionProvider": map[string]any{
						"triggerCharacters": []string{"."},
					},
					"definitionProvider": true,
				}},
			})
		case "initialized":
		case "textDocument/didOpen":
			var params struct {
				TextDocument struct {
					URI string `json:"uri"`
				} `json:"textDocument"`
			}
			_ = json.Unmarshal(request.Params, &params)
			writeLSPMessage(map[string]any{
				"jsonrpc": "2.0",
				"method":  "textDocument/publishDiagnostics",
				"params": map[string]any{
					"uri": params.TextDocument.URI,
					"diagnostics": []map[string]any{{
						"range":    map[string]any{"start": map[string]any{"line": 0, "character": 4}},
						"severity": 1,
						"message":  "fake lsp diagnostic",
					}},
				},
			})
		case "textDocument/hover":
			writeLSPMessage(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": map[string]any{"contents": map[string]any{"value": "fake hover"}}})
		case "textDocument/completion":
			writeLSPMessage(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": map[string]any{"items": []map[string]any{{"label": "fakeCompletion"}}}})
		case "textDocument/definition":
			writeLSPMessage(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": []map[string]any{{
				"uri":   "file://" + (&url.URL{Path: "/tmp/superscience/main.go"}).EscapedPath(),
				"range": map[string]any{"start": map[string]any{"line": 1, "character": 0}},
			}}})
		case "shutdown":
			writeLSPMessage(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": nil})
		case "exit":
			return
		}
	}
}

func readLSPMessage(reader *bufio.Reader) ([]byte, error) {
	contentLength := 0
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if strings.HasPrefix(strings.ToLower(line), "content-length:") {
			value := strings.TrimSpace(strings.TrimPrefix(line, "Content-Length:"))
			contentLength, _ = strconv.Atoi(value)
		}
	}
	if contentLength == 0 {
		return nil, io.ErrUnexpectedEOF
	}
	data := make([]byte, contentLength)
	_, err := io.ReadFull(reader, data)
	return data, err
}

func writeLSPMessage(payload any) {
	data, _ := json.Marshal(payload)
	_, _ = fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n%s", len(data), data)
}
