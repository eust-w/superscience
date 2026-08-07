package plugins_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"superscience/internal/plugins"
	"superscience/internal/store"
)

func TestManifestToolsSupportsStringAndObjectEntries(t *testing.T) {
	plugin := store.PluginRecord{
		ID:       "lab-plugin",
		Manifest: `{"tools":["quick_search",{"name":"lab_search","description":"Search lab records","inputSchema":{"type":"object"}}]}`,
	}

	tools, err := plugins.ManifestTools(plugin)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 2 {
		t.Fatalf("tools = %+v", tools)
	}
	if tools[0].Name != "quick_search" || tools[1].Name != "lab_search" || tools[1].InputSchema["type"] != "object" {
		t.Fatalf("tools = %+v", tools)
	}
}

func TestPersistentSubprocessHandshakeDiscoveryValidationAndExecution(t *testing.T) {
	t.Setenv("SUPERSCIENCE_PLUGIN_HELPER", "1")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	plugin := store.PluginRecord{
		ID:       "lab-plugin",
		Name:     "Lab Plugin",
		Runtime:  "subprocess",
		Command:  executable,
		Args:     []string{"-test.run=^TestPluginFixtureProcess$"},
		Enabled:  true,
		Status:   "configured",
		Manifest: `{"tools":[{"name":"lab_search","description":"Search lab records","inputSchema":{"type":"object","properties":{"query":{"type":"string"}},"required":["query"],"additionalProperties":false}}]}`,
	}
	defer plugins.CloseAll()

	discovery, err := plugins.Discover(context.Background(), plugin)
	if err != nil {
		t.Fatal(err)
	}
	if discovery.ProtocolVersion != "superscience-plugin/1" || discovery.PluginName != "fixture-plugin" || len(discovery.Tools) != 1 {
		t.Fatalf("discovery = %+v", discovery)
	}

	first, err := plugins.Execute(context.Background(), plugin, "lab_search", map[string]any{"query": "EGFR"}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := plugins.Execute(context.Background(), plugin, "lab_search", map[string]any{"query": "ALK"}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.Output != "search EGFR count=1" || second.Output != "search ALK count=2" {
		t.Fatalf("persistent outputs = %q, %q", first.Output, second.Output)
	}

	_, err = plugins.Execute(context.Background(), plugin, "lab_search", map[string]any{"query": 42}, "", nil)
	if err == nil || !strings.Contains(err.Error(), "validate") {
		t.Fatalf("schema validation error = %v", err)
	}
}

func TestPluginCommandIsNotShellSplit(t *testing.T) {
	t.Setenv("SUPERSCIENCE_PLUGIN_HELPER", "1")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err = plugins.Discover(ctx, store.PluginRecord{
		ID: "unsafe-plugin", Runtime: "subprocess", Command: executable + " -test.run=^TestPluginFixtureProcess$", Enabled: true,
	})
	if err == nil {
		t.Fatal("expected executable lookup to fail instead of splitting a shell command")
	}
}

func TestPluginFixtureProcess(t *testing.T) {
	if os.Getenv("SUPERSCIENCE_PLUGIN_HELPER") != "1" {
		return
	}
	reader := bufio.NewScanner(os.Stdin)
	writer := bufio.NewWriter(os.Stdout)
	initialized := false
	count := 0
	for reader.Scan() {
		var request struct {
			ID     any            `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if err := json.Unmarshal(reader.Bytes(), &request); err != nil {
			continue
		}
		response := map[string]any{"jsonrpc": "2.0", "id": request.ID}
		switch request.Method {
		case "initialize":
			initialized = true
			response["result"] = map[string]any{
				"protocolVersion": "superscience-plugin/1",
				"pluginInfo":      map[string]any{"name": "fixture-plugin", "version": "1.0.0"},
				"capabilities":    map[string]any{"tools": true, "providers": false, "hooks": false},
			}
		case "tools/list":
			if !initialized {
				response["error"] = map[string]any{"code": -32002, "message": "not initialized"}
			} else {
				response["result"] = map[string]any{"tools": []map[string]any{{
					"name": "lab_search", "description": "Search lab records",
					"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string"}}, "required": []string{"query"}, "additionalProperties": false},
				}}}
			}
		case "tools/call":
			arguments, _ := request.Params["arguments"].(map[string]any)
			count++
			response["result"] = map[string]any{"content": []map[string]any{{
				"type": "text", "text": fmt.Sprintf("search %s count=%d", arguments["query"], count),
			}}}
		case "shutdown":
			response["result"] = map[string]any{"ok": true}
		default:
			response["error"] = map[string]any{"code": -32601, "message": "unknown method"}
		}
		data, _ := json.Marshal(response)
		_, _ = writer.Write(append(data, '\n'))
		_ = writer.Flush()
		if request.Method == "shutdown" {
			return
		}
	}
}
