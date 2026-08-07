import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError, buildUrl, parseNDJSON, SuperscienceClient } from "./api";

afterEach(() => {
  vi.restoreAllMocks();
});

describe("api helpers", () => {
  it("builds absolute API URLs from a localhost base", () => {
    expect(buildUrl("http://127.0.0.1:4096", "/api/health")).toBe("http://127.0.0.1:4096/api/health");
  });

  it("parses newline-delimited JSON stream payloads", () => {
    const parsed = parseNDJSON('{"type":"delta","content":"hello"}\n{"type":"done"}\n');
    expect(parsed).toEqual([
      { type: "delta", content: "hello" },
      { type: "done" }
    ]);
  });

  it("posts credential tests to the settings endpoint", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ ok: true, status: "ready", message: "Provider verified", model: "gpt-live", latencyMs: 42 }), {
        status: 200,
        headers: { "Content-Type": "application/json" }
      })
    );
    const client = new SuperscienceClient("http://127.0.0.1:4096");

    await expect(client.testCredential({ provider: "openai", endpoint: "https://api.openai.com/v1", key: "sk-live", model: "gpt-live" })).resolves.toEqual({
      ok: true,
      status: "ready",
      message: "Provider verified",
      model: "gpt-live",
      latencyMs: 42
    });
    expect(fetchMock).toHaveBeenCalledWith("http://127.0.0.1:4096/api/settings/credentials/test", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ provider: "openai", endpoint: "https://api.openai.com/v1", key: "sk-live", model: "gpt-live" })
    });
  });

  it("throws typed API errors from the unified backend error contract", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(
        JSON.stringify({ code: "rate_limited", message: "Try later", retryable: true, details: { retryAfter: "3" }, requestId: "req_1" }),
        { status: 429, headers: { "Content-Type": "application/json" } }
      )
    );
    const client = new SuperscienceClient("http://127.0.0.1:4096");
    try {
      await client.listProjects();
      throw new Error("expected request to fail");
    } catch (error) {
      expect(error).toBeInstanceOf(ApiError);
      expect(error).toMatchObject({ code: "rate_limited", retryable: true, requestId: "req_1", status: 429, details: { retryAfter: "3" } });
    }
  });

  it("calls ecosystem API endpoints", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
      const url = String(input);
      if (url.endsWith("/api/openapi.json")) {
        return new Response(JSON.stringify({ openapi: "3.1.0", info: {}, paths: { "/api/plugins": {} } }), { status: 200 });
      }
      if (url.endsWith("/api/providers/models/refresh")) {
        return new Response(JSON.stringify({ status: "ok", source: "bundled", providers: 13, models: 9 }), { status: 200 });
      }
      if (url.endsWith("/api/runs/run_1/events")) {
        return new Response(JSON.stringify([{ id: "step_1", type: "agent.step", status: "ok", createdAt: 1 }]), { status: 200 });
      }
      if (url.endsWith("/api/sessions/session_1/runs")) {
        return new Response(JSON.stringify([{ id: "run_1", sessionId: "session_1", agent: "research", status: "completed", goal: "verify", model: "gpt-live", createdAt: 1, updatedAt: 2 }]), { status: 200 });
      }
      if (url.endsWith("/api/runs/run_1/retry")) {
        return new Response(JSON.stringify({ id: "run_2", sessionId: "session_1", agent: "research", status: "retry_scheduled", goal: "again", model: "mock" }), { status: 200 });
      }
      if (url.endsWith("/api/runs/run_1/compact")) {
        return new Response(JSON.stringify({ status: "compacted", runId: "run_1", messageId: "message_1", artifactId: "artifact_1" }), { status: 200 });
      }
      if (url.endsWith("/api/providers/openai/health?model=gpt-live")) {
        return new Response(JSON.stringify({ id: "openai", configured: true, ok: true, status: "ready", model: "gpt-live", latencyMs: 34 }), { status: 200 });
      }
      if (url.endsWith("/api/providers")) {
        return new Response(JSON.stringify([{ id: "openai", displayName: "OpenAI" }]), { status: 200 });
      }
      if (url.endsWith("/api/agents/chemistry-review") && init?.method === "PATCH") {
        return new Response(JSON.stringify({ id: "chemistry-review", name: "Chemistry Review", enabled: false, builtIn: false }), { status: 200 });
      }
      if (url.endsWith("/api/agents/chemistry-review") && init?.method === "DELETE") {
        return new Response(JSON.stringify({ ok: true }), { status: 200 });
      }
      if (url.endsWith("/api/agents") && init?.method === "POST") {
        return new Response(JSON.stringify({ id: "chemistry-review", name: "Chemistry Review", enabled: true, builtIn: false }), { status: 200 });
      }
      if (url.endsWith("/api/agents")) {
        return new Response(JSON.stringify([{ id: "research", name: "Research" }]), { status: 200 });
      }
      if (url.endsWith("/api/tools/execute")) {
        return new Response(JSON.stringify({ tool: "glob", status: "ok", output: "1 matches" }), { status: 200 });
      }
      if (url.endsWith("/api/skills/load")) {
        return new Response(JSON.stringify({ id: "skill_1", name: "research-protocol", content: "Use provenance." }), { status: 200 });
      }
      if (url.endsWith("/api/skills/install")) {
        return new Response(JSON.stringify({ id: "skill_1", name: "kinase-evidence", enabled: true }), { status: 200 });
      }
      if (url.endsWith("/api/skills/validate")) {
        return new Response(JSON.stringify({ ok: true, name: "kinase-evidence", entrypoints: ["default"] }), { status: 200 });
      }
      if (url.endsWith("/api/mcp/servers") && init?.method === "POST") {
        return new Response(JSON.stringify({ id: "local", transport: "stdio", status: "configured" }), { status: 200 });
      }
      if (url.endsWith("/api/mcp/servers/local/oauth/start")) {
        return new Response(JSON.stringify({ serverId: "local", state: "state_1", authorizationUrl: "/api/mcp/oauth/callback?state=state_1", status: "pending" }), {
          status: 200
        });
      }
      if (url.endsWith("/api/mcp/servers/local/discovery")) {
        return new Response(
          JSON.stringify({
            tools: [{ name: "lookup", inputSchema: { type: "object" } }],
            resources: [{ uri: "file:///dataset.csv", name: "Dataset", mimeType: "text/csv" }],
            prompts: [{ name: "summarize", arguments: [{ name: "topic", required: true }] }]
          }),
          { status: 200 }
        );
      }
      if (url.endsWith("/api/mcp/servers/local/resources/read")) {
        return new Response(JSON.stringify({ status: "ok", uri: "file:///dataset.csv", text: "gene,score\nEGFR,0.91", contents: [] }), { status: 200 });
      }
      if (url.endsWith("/api/mcp/servers/local/prompts/get")) {
        return new Response(
          JSON.stringify({ status: "ok", name: "summarize", description: "Summarize selected records", text: "Summarize EGFR evidence", messages: [] }),
          { status: 200 }
        );
      }
      if (url.endsWith("/api/plugins") && init?.method === "POST") {
        return new Response(JSON.stringify({ id: "lab-plugin", name: "Lab Plugin", status: "configured", manifest: "{}" }), { status: 200 });
      }
      if (url.endsWith("/api/plugins/lab-plugin") && init?.method === "PATCH") {
        return new Response(JSON.stringify({ id: "lab-plugin", name: "Lab Plugin", status: "disabled", manifest: "{}" }), { status: 200 });
      }
      if (url.endsWith("/api/plugins/lab-plugin") && init?.method === "DELETE") {
        return new Response(JSON.stringify({ ok: true }), { status: 200 });
      }
      if (url.endsWith("/api/plugins/lab-plugin/tools")) {
        return new Response(JSON.stringify([{ name: "lab_search", description: "Search lab records", inputSchema: { type: "object" } }]), { status: 200 });
      }
      if (url.endsWith("/api/plugins/lab-plugin/tools/lab_search/execute")) {
        return new Response(JSON.stringify({ pluginId: "lab-plugin", tool: "lab_search", status: "ok", output: "plugin searched EGFR" }), { status: 200 });
      }
      if (url.endsWith("/api/plugins")) {
        return new Response(JSON.stringify([{ id: "lab-plugin", name: "Lab Plugin", status: "configured", manifest: "{}" }]), { status: 200 });
      }
      if (url.endsWith("/api/lsp/sessions")) {
        if (init?.method === "POST") {
          return new Response(JSON.stringify({ id: "lsp_1", language: "go", diagnostics: [] }), { status: 200 });
        }
        return new Response(JSON.stringify([{ id: "lsp_1", language: "go", diagnostics: [] }]), { status: 200 });
      }
      if (url.endsWith("/api/lsp/sessions/lsp_1/status")) {
        return new Response(JSON.stringify({ id: "lsp_1", language: "go", path: "main.go", status: "ready", diagnostics: 0, capabilities: [] }), { status: 200 });
      }
      if (url.endsWith("/api/notebooks/import")) {
        return new Response(JSON.stringify({ id: "notebook_2", path: "analysis/import.ipynb", title: "Imported", cells: [{ id: "cell_1", status: "idle" }] }), {
          status: 200
        });
      }
      if (url.endsWith("/api/notebooks/notebook_1/export")) {
        return new Response(JSON.stringify({ nbformat: 4, nbformat_minor: 5, metadata: {}, cells: [] }), { status: 200 });
      }
      if (url.endsWith("/api/notebooks/notebook_1/cells/cell_1/cancel")) {
        return new Response(JSON.stringify({ status: "cancelled", notebookId: "notebook_1", cellId: "cell_1" }), { status: 200 });
      }
      if (url.endsWith("/api/share/publish")) {
        return new Response(JSON.stringify({ id: "share_1", status: "published", url: "/api/share/share_1" }), { status: 200 });
      }
      if (url.endsWith("/api/share/share_1/replay")) {
        return new Response(JSON.stringify({ share: { id: "share_1" }, artifacts: [], provenance: [] }), { status: 200 });
      }
      return new Response(JSON.stringify([]), { status: 200 });
    });
    const client = new SuperscienceClient("http://127.0.0.1:4096");

    await expect(client.openAPI()).resolves.toMatchObject({ openapi: "3.1.0" });
    await expect(client.listProviders()).resolves.toEqual([{ id: "openai", displayName: "OpenAI" }]);
    await expect(client.providerHealth("openai", "gpt-live")).resolves.toMatchObject({ configured: true, status: "ready" });
    await expect(client.refreshProviderModels()).resolves.toMatchObject({ source: "bundled" });
    await expect(client.listAgents()).resolves.toEqual([{ id: "research", name: "Research" }]);
    await expect(client.saveAgent({
      id: "chemistry-review",
      name: "Chemistry Review",
      domain: "chemistry",
      description: "Reviews chemistry evidence.",
      systemPrompt: "Require structure-backed evidence.",
      tools: ["read", "grep"],
      reviewMode: "required",
      enabled: true
    })).resolves.toMatchObject({ id: "chemistry-review", builtIn: false });
    await expect(client.updateAgent("chemistry-review", { enabled: false })).resolves.toMatchObject({ enabled: false });
    await expect(client.deleteAgent("chemistry-review")).resolves.toEqual({ ok: true });
    await expect(client.listSessionRuns("session_1")).resolves.toEqual([
      expect.objectContaining({ id: "run_1", status: "completed" })
    ]);
    await expect(client.listRunEvents("run_1")).resolves.toEqual([{ id: "step_1", type: "agent.step", status: "ok", createdAt: 1 }]);
    await expect(client.retryRun("run_1", "again")).resolves.toMatchObject({ status: "retry_scheduled" });
    await expect(client.compactRun("run_1")).resolves.toMatchObject({ status: "compacted", artifactId: "artifact_1" });
    await expect(client.executeTool({ tool: "glob", input: { pattern: "*.go" } })).resolves.toMatchObject({ status: "ok" });
    await expect(client.loadSkill("research-protocol")).resolves.toMatchObject({ content: "Use provenance." });
    await expect(client.installSkill({ path: "/tmp/SKILL.md" })).resolves.toMatchObject({ name: "kinase-evidence" });
    await expect(client.validateSkill({ path: "/tmp/SKILL.md" })).resolves.toMatchObject({ ok: true });
    await expect(client.addMCPServer({ name: "local", transport: "stdio", command: "server", enabled: true })).resolves.toMatchObject({ status: "configured" });
    await expect(client.startMCPOAuth("local", { clientId: "client_1" })).resolves.toMatchObject({ state: "state_1" });
    await expect(client.discoverMCPServer("local")).resolves.toMatchObject({
      resources: [{ uri: "file:///dataset.csv" }],
      prompts: [{ name: "summarize" }]
    });
    await expect(client.readMCPResource("local", "file:///dataset.csv", "session_1")).resolves.toMatchObject({ status: "ok", text: "gene,score\nEGFR,0.91" });
    await expect(client.getMCPPrompt("local", "summarize", { topic: "EGFR" }, "session_1")).resolves.toMatchObject({
      status: "ok",
      text: "Summarize EGFR evidence"
    });
    await expect(client.savePlugin({ id: "lab-plugin", name: "Lab Plugin", manifest: { tools: ["lab_search"] } })).resolves.toMatchObject({ id: "lab-plugin" });
    await expect(client.listPlugins()).resolves.toEqual([{ id: "lab-plugin", name: "Lab Plugin", status: "configured", manifest: "{}" }]);
    await expect(client.listPluginTools("lab-plugin")).resolves.toEqual([{ name: "lab_search", description: "Search lab records", inputSchema: { type: "object" } }]);
    await expect(client.executePluginTool("lab-plugin", "lab_search", { query: "EGFR" }, "session_1")).resolves.toMatchObject({
      status: "ok",
      output: "plugin searched EGFR"
    });
    await expect(client.patchPlugin("lab-plugin", { status: "disabled" })).resolves.toMatchObject({ status: "disabled" });
    await expect(client.deletePlugin("lab-plugin")).resolves.toEqual({ ok: true });
    await expect(client.listLSPSessions()).resolves.toEqual([{ id: "lsp_1", language: "go", diagnostics: [] }]);
    await expect(client.getLSPStatus("lsp_1")).resolves.toMatchObject({ status: "ready" });
    await expect(client.importNotebook({ path: "analysis/import.ipynb", notebook: { cells: [] } })).resolves.toMatchObject({ id: "notebook_2" });
    await expect(client.exportNotebook("notebook_1")).resolves.toMatchObject({ nbformat: 4 });
    await expect(client.cancelNotebookCell("notebook_1", "cell_1")).resolves.toMatchObject({ status: "cancelled" });
    await expect(client.publishShare({ title: "Report", artifactIds: ["artifact_1"], visibility: "private-link" })).resolves.toMatchObject({ status: "published" });
    await expect(client.getShareReplay("share_1")).resolves.toMatchObject({ artifacts: [] });
    expect(fetchMock).toHaveBeenCalledWith("http://127.0.0.1:4096/api/providers");
    expect(fetchMock).toHaveBeenCalledWith("http://127.0.0.1:4096/api/agents/chemistry-review", {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ enabled: false })
    });
  });
});
