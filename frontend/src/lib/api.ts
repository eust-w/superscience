export type Project = {
  id: string;
  name: string;
  path: string;
  pinned: boolean;
  createdAt: number;
  updatedAt: number;
};

export type ApiErrorPayload = {
  code: string;
  message: string;
  retryable: boolean;
  details: Record<string, unknown>;
  requestId: string;
};

export class ApiError extends Error {
  readonly code: string;
  readonly retryable: boolean;
  readonly details: Record<string, unknown>;
  readonly requestId: string;
  readonly status: number;

  constructor(payload: ApiErrorPayload, status: number) {
    super(payload.message);
    this.name = "ApiError";
    this.code = payload.code;
    this.retryable = payload.retryable;
    this.details = payload.details;
    this.requestId = payload.requestId;
    this.status = status;
  }
}

export type Session = {
  id: string;
  projectId: string;
  title: string;
  agent: string;
  status: string;
  createdAt: number;
  updatedAt: number;
};

export type Message = {
  id: string;
  sessionId: string;
  role: "user" | "assistant" | "system";
  content: string;
  createdAt: number;
};

export type FileNode = {
  name: string;
  path: string;
  kind: "file" | "directory";
  size: number;
  modified: number;
  extension?: string;
};

export type Credential = {
  id: string;
  provider: string;
  label: string;
  source: string;
  redacted: string;
  createdAt: number;
  updatedAt: number;
};

export type ScienceDatabase = {
  id: string;
  name: string;
  domain: string;
  description: string;
};

export type ScienceHit = {
  id: string;
  source: string;
  title: string;
  summary: string;
  url: string;
  score: number;
  metadata: Record<string, string>;
  provenance: { source: string; url: string };
};

export type StreamEvent = {
  type: string;
  role?: string;
  content?: string;
  title?: string;
  message?: Message;
  permission?: PermissionRequest;
};

export type PTYSession = {
  id: string;
  command: string;
  cwd: string;
  created: number;
};

export type SettingsValues = Record<string, string>;

export type PermissionRequest = {
  id: string;
  sessionId: string;
  type: string;
  pattern: string;
  message: string;
  status: string;
  createdAt: number;
};

export type ProviderProfile = {
  id: string;
  kind: string;
  displayName: string;
  baseURL: string;
  enabled: boolean;
  secretRef: string;
  capabilities: string[];
};

export type ModelProfile = {
  id: string;
  providerId: string;
  displayName: string;
  contextWindow: number;
  capabilities: string[];
  status: string;
};

export type ProviderHealth = {
  id: string;
  ok: boolean;
  configured?: boolean;
  status: string;
  model?: string;
  message?: string;
  latencyMs?: number;
  totalTokens?: number;
};

export type CredentialTestResult = {
  ok: boolean;
  status: string;
  message: string;
  provider?: string;
  model?: string;
  latencyMs?: number;
  totalTokens?: number;
};

export type ModelRefreshResult = {
  status: string;
  source: string;
  providers: number;
  models: number;
};

export type OpenAPISpec = {
  openapi: string;
  info: Record<string, unknown>;
  paths: Record<string, unknown>;
};

export type AgentDefinition = {
  id: string;
  name: string;
  domain: string;
  description: string;
  systemPrompt: string;
  tools: string[];
  reviewMode: string;
  enabled: boolean;
  builtIn: boolean;
  createdAt?: number;
  updatedAt?: number;
};

export type ToolDefinition = {
  id: string;
  name: string;
  category: string;
  description: string;
  risk: string;
  requiresApproval: boolean;
  enabled: boolean;
  inputs: string[];
};

export type ToolExecuteResult = {
  id?: string;
  runId?: string;
  sessionId?: string;
  tool: string;
  status: string;
  permissionId?: string;
  output: string;
  data?: Record<string, unknown>;
};

export type RunEvent = {
  id: string;
  type: "agent.step" | "tool.call";
  status: string;
  kind?: string;
  title?: string;
  content?: string;
  tool?: string;
  input?: string;
  output?: string;
  permissionId?: string;
  createdAt: number;
};

export type AgentRunRecord = {
  id: string;
  sessionId: string;
  agent: string;
  status: string;
  goal: string;
  model: string;
  createdAt: number;
  updatedAt: number;
};

export type RunCompactResult = {
  status: "compacted";
  runId: string;
  messageId: string;
  artifactId: string;
};

export type SkillRecord = {
  id: string;
  name: string;
  description: string;
  source: string;
  path: string;
  enabled: boolean;
  entry?: boolean;
};

export type LoadedSkill = SkillRecord & {
  content: string;
};

export type MCPServer = {
  id: string;
  name: string;
  transport: string;
  command: string;
  args: string[];
  url: string;
  enabled: boolean;
  status: string;
  oauthSecretRef?: string;
  oauthStatus?: string;
};

export type MCPOAuthStart = {
  serverId: string;
  state: string;
  authorizationUrl: string;
  status: string;
  expiresAt: number;
};

export type MCPDiscovery = {
  tools: Array<{ name: string; description?: string; inputSchema?: Record<string, unknown> }>;
  resources: Array<{ uri: string; name?: string; description?: string; mimeType?: string }>;
  prompts: Array<{ name: string; description?: string; arguments?: Array<{ name: string; description?: string; required?: boolean }> }>;
  status?: "ready" | "degraded";
  cached?: boolean;
  error?: string;
  updatedAt?: number;
};

export type MCPResourceReadResult = {
  serverId: string;
  uri: string;
  status: string;
  permissionId?: string;
  contents?: Array<{ uri: string; mimeType?: string; text?: string; blob?: string }>;
  text?: string;
};

export type MCPPromptGetResult = {
  serverId: string;
  name: string;
  description?: string;
  status: string;
  permissionId?: string;
  messages?: Array<{
    role: string;
    content: {
      type: string;
      text?: string;
      data?: string;
      mimeType?: string;
      resource?: { uri: string; mimeType?: string; text?: string; blob?: string };
    };
  }>;
  text?: string;
};

export type PluginRecord = {
  id: string;
  name: string;
  version: string;
  description: string;
  runtime: string;
  command: string;
  args: string[];
  enabled: boolean;
  status: string;
  manifest: string;
  createdAt: number;
  updatedAt: number;
};

export type PluginDiscovery = {
  protocolVersion: string;
  pluginName: string;
  pluginVersion: string;
  capabilities: Record<string, unknown>;
  tools: PluginTool[];
  providers: Array<{ id: string; name: string; kind: string }>;
  hooks: Array<{ name: string; events: string[] }>;
};

export type PluginTool = {
  name: string;
  description?: string;
  inputSchema?: Record<string, unknown>;
};

export type PluginToolExecuteResult = {
  pluginId: string;
  tool: string;
  status: string;
  permissionId?: string;
  output?: string;
  input?: Record<string, unknown>;
};

export type NotebookCell = {
  id: string;
  notebookId: string;
  kind: string;
  language: string;
  source: string;
  output: string;
  outputJson?: string;
  status: string;
};

export type NotebookDocument = {
  id: string;
  projectId: string;
  path: string;
  title: string;
  cells: NotebookCell[];
};

export type NotebookExport = {
  nbformat: number;
  nbformat_minor: number;
  metadata: Record<string, unknown>;
  cells: Array<Record<string, unknown>>;
};

export type NotebookOutput = {
  output_type: "stream" | "display_data" | "execute_result" | "error" | string;
  name?: string;
  text?: string;
  data?: Record<string, unknown>;
  metadata?: Record<string, unknown>;
  execution_count?: number | null;
  ename?: string;
  evalue?: string;
  traceback?: string[];
};

export type NotebookRunResult = {
  cellId: string;
  status: string;
  kernel: string;
  stdout: string;
  stderr: string;
  outputs: NotebookOutput[];
  durationMs: number;
};

export type NotebookKernelStatus = {
  notebookId: string;
  kernel: string;
  status: "running" | "stopped" | "unsupported" | string;
};

export type LSPDiagnostic = {
  severity: string;
  message: string;
  line: number;
  column: number;
};

export type LSPSession = {
  id: string;
  language: string;
  path: string;
  diagnostics: LSPDiagnostic[];
  hover: string;
  completions: string[];
  capabilities: string[];
};

export type LSPStatus = {
  id: string;
  language: string;
  path: string;
  status: string;
  diagnostics: number;
  capabilities: string[];
  createdAt: number;
};

export type Artifact = {
  id: string;
  sessionId: string;
  type: string;
  title: string;
  content: string;
  metadata: string;
  createdAt: number;
};

export type ShareRecord = {
  id: string;
  title: string;
  visibility: string;
  status: string;
  url: string;
  bundle: string;
};

export type PendingPermissionResult = {
  status: "pending_permission";
  permissionId: string;
  type: string;
  pattern: string;
};

export type ShareReplay = {
  share: ShareRecord;
  artifacts: Artifact[];
  provenance: ProvenanceEdge[];
};

export type ProvenanceEdge = {
  id: string;
  sourceId: string;
  sourceType: string;
  targetId: string;
  targetType: string;
  relation: string;
  metadata: string;
};

export function buildUrl(base: string, path: string): string {
  const normalizedBase = base.endsWith("/") ? base.slice(0, -1) : base;
  const normalizedPath = path.startsWith("/") ? path : `/${path}`;
  return `${normalizedBase}${normalizedPath}`;
}

export function parseNDJSON(input: string): unknown[] {
  return input
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line) => JSON.parse(line) as unknown);
}

export class SuperscienceClient {
  constructor(private base = "") {}

  setBase(base: string) {
    this.base = base.replace(/\/$/, "");
  }

  async health() {
    return this.get<{ status: string; runtime: string; time: number }>("/api/health");
  }

  async openAPI() {
    return this.get<OpenAPISpec>("/api/openapi.json");
  }

  async listProjects() {
    return this.get<Project[]>("/api/projects");
  }

  async openProject(path: string) {
    return this.post<Project>("/api/projects/open", { path });
  }

  async createSession(projectId: string, title = "New research session", agent = "research") {
    return this.post<Session>("/api/sessions", { projectId, title, agent });
  }

  async listSessions(projectId: string) {
    return this.get<Session[]>(`/api/sessions?projectId=${encodeURIComponent(projectId)}`);
  }

  async listMessages(sessionId: string) {
    return this.get<Message[]>(`/api/sessions/${sessionId}/messages`);
  }

  async streamMessage(
    sessionId: string,
    content: string,
    onEvent: (event: StreamEvent) => void,
    options: { provider?: string; model?: string; agent?: string } = {}
  ) {
    const response = await fetch(this.url(`/api/sessions/${sessionId}/messages/stream`), {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ content, ...options })
    });
    if (!response.ok || !response.body) throw new Error(`stream failed: ${response.status}`);
    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    let buffer = "";
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      buffer += decoder.decode(value, { stream: true });
      const lines = buffer.split("\n");
      buffer = lines.pop() ?? "";
      for (const line of lines) {
        const trimmed = line.trim();
        if (!trimmed) continue;
        onEvent(JSON.parse(trimmed) as StreamEvent);
      }
    }
    if (buffer.trim()) onEvent(JSON.parse(buffer) as StreamEvent);
  }

  async listFiles(path = "") {
    return this.get<FileNode[]>(`/api/files/tree?path=${encodeURIComponent(path)}`);
  }

  async readFile(path: string) {
    return this.get<{ content: string }>(`/api/files/content?path=${encodeURIComponent(path)}`);
  }

  async writeFile(path: string, content: string, sessionId?: string) {
    return this.put<{ status: "ok"; content: string } | PendingPermissionResult>("/api/files/content", { path, content, sessionId });
  }

  async searchScience(database: string, query: string, limit = 10, sessionId?: string) {
    return this.post<ScienceHit[] | PendingPermissionResult>("/api/science/search", { database, query, limit, sessionId });
  }

  async listScienceDatabases() {
    return this.get<ScienceDatabase[]>("/api/science/databases");
  }

  async listCredentials() {
    return this.get<Credential[]>("/api/settings/credentials");
  }

  async addCredential(input: { provider: string; label: string; key: string; source?: string }) {
    return this.post<Credential>("/api/settings/credentials", input);
  }

  async testCredential(input: { provider: string; endpoint?: string; key?: string; model?: string }) {
    return this.post<CredentialTestResult>("/api/settings/credentials/test", input);
  }

  async deleteCredential(id: string) {
    return this.del<{ ok: boolean }>(`/api/settings/credentials?id=${encodeURIComponent(id)}`);
  }

  async listSettings() {
    return this.get<SettingsValues>("/api/settings");
  }

  async updateSettings(input: SettingsValues) {
    return this.put<SettingsValues>("/api/settings", input);
  }

  async listPermissions() {
    return this.get<PermissionRequest[]>("/api/permissions");
  }

  async respondPermission(id: string, decision: "once" | "always" | "reject") {
    return this.post<{ ok: boolean }>(`/api/permissions/${id}/respond`, { decision });
  }

  async createPTY(command = "shell", cwd = "", sessionId?: string) {
    return this.post<PTYSession | PendingPermissionResult>("/api/pty", { command, cwd, sessionId });
  }

  async stopPTY(id: string) {
    return this.del<{ ok: boolean }>(`/api/pty/${encodeURIComponent(id)}`);
  }

  async listProviders() {
    return this.get<ProviderProfile[]>("/api/providers");
  }

  async saveProvider(input: Partial<ProviderProfile> & { id?: string; key?: string }) {
    return this.post<ProviderProfile>("/api/providers", input);
  }

  async listProviderModels(providerId: string) {
    return this.get<ModelProfile[]>(`/api/providers/${encodeURIComponent(providerId)}/models`);
  }

  async providerHealth(providerId: string, model?: string) {
    const query = model ? `?model=${encodeURIComponent(model)}` : "";
    return this.get<ProviderHealth>(`/api/providers/${encodeURIComponent(providerId)}/health${query}`);
  }

  async refreshProviderModels() {
    return this.post<ModelRefreshResult>("/api/providers/models/refresh", {});
  }

  async listAgents() {
    return this.get<AgentDefinition[]>("/api/agents");
  }

  async saveAgent(input: Omit<AgentDefinition, "builtIn" | "createdAt" | "updatedAt">) {
    return this.post<AgentDefinition>("/api/agents", input);
  }

  async updateAgent(id: string, input: Partial<Pick<AgentDefinition, "name" | "domain" | "description" | "systemPrompt" | "tools" | "reviewMode" | "enabled">>) {
    return this.patch<AgentDefinition>(`/api/agents/${encodeURIComponent(id)}`, input);
  }

  async deleteAgent(id: string) {
    return this.del<{ ok: boolean }>(`/api/agents/${encodeURIComponent(id)}`);
  }

  async runAgent(sessionId: string, input: { goal: string; agent?: string; provider?: string; model?: string; maxSteps?: number }) {
    return this.post<Record<string, unknown>>(`/api/sessions/${sessionId}/run`, input);
  }

  async listSessionRuns(sessionId: string) {
    return this.get<AgentRunRecord[]>(`/api/sessions/${encodeURIComponent(sessionId)}/runs`);
  }

  async listRunEvents(runId: string) {
    return this.get<RunEvent[]>(`/api/runs/${encodeURIComponent(runId)}/events`);
  }

  async retryRun(runId: string, reason?: string) {
    return this.post<AgentRunRecord>(`/api/runs/${encodeURIComponent(runId)}/retry`, { reason });
  }

  async compactRun(runId: string) {
    return this.post<RunCompactResult>(`/api/runs/${encodeURIComponent(runId)}/compact`, {});
  }

  async listTools() {
    return this.get<ToolDefinition[]>("/api/tools");
  }

  async executeTool(input: { runId?: string; sessionId?: string; tool: string; input?: Record<string, unknown> }) {
    return this.post<ToolExecuteResult>("/api/tools/execute", input);
  }

  async listSkills() {
    return this.get<SkillRecord[]>("/api/skills");
  }

  async saveSkill(input: Partial<SkillRecord>) {
    return this.post<SkillRecord>("/api/skills", input);
  }

  async loadSkill(name: string) {
    return this.post<LoadedSkill>("/api/skills/load", { name });
  }

  async installSkill(input: { path?: string; url?: string; name?: string; description?: string; source?: string }) {
    return this.post<SkillRecord>("/api/skills/install", input);
  }

  async validateSkill(input: { path?: string; url?: string; name?: string }) {
    return this.post<{ ok: boolean; id?: string; name?: string; description?: string; path?: string; source?: string; error?: string; entrypoints?: string[] }>(
      "/api/skills/validate",
      input
    );
  }

  async listMCPServers() {
    return this.get<MCPServer[]>("/api/mcp/servers");
  }

  async addMCPServer(input: { name: string; transport: string; command?: string; args?: string[]; url?: string; enabled?: boolean }) {
    return this.post<MCPServer>("/api/mcp/servers", input);
  }

  async executeMCPTool(serverId: string, tool: string, input: Record<string, unknown>, sessionId?: string) {
    return this.post<Record<string, unknown>>(`/api/mcp/servers/${encodeURIComponent(serverId)}/tools/${encodeURIComponent(tool)}/execute`, { input, sessionId });
  }

  async startMCPOAuth(serverId: string, input: { clientId: string; clientSecret?: string; scopes?: string[] }) {
    return this.post<MCPOAuthStart>(`/api/mcp/servers/${encodeURIComponent(serverId)}/oauth/start`, input);
  }

  async removeMCPOAuth(serverId: string) {
    return this.post<{ removed: boolean; serverId: string }>(`/api/mcp/servers/${encodeURIComponent(serverId)}/oauth/remove`, {});
  }

  async discoverMCPServer(serverId: string) {
    return this.get<MCPDiscovery>(`/api/mcp/servers/${encodeURIComponent(serverId)}/discovery`);
  }

  async readMCPResource(serverId: string, uri: string, sessionId?: string) {
    return this.post<MCPResourceReadResult>(`/api/mcp/servers/${encodeURIComponent(serverId)}/resources/read`, { uri, sessionId });
  }

  async getMCPPrompt(serverId: string, name: string, args: Record<string, unknown> = {}, sessionId?: string) {
    return this.post<MCPPromptGetResult>(`/api/mcp/servers/${encodeURIComponent(serverId)}/prompts/get`, { name, arguments: args, sessionId });
  }

  async listPlugins() {
    return this.get<PluginRecord[]>("/api/plugins");
  }

  async listPluginTools(id: string) {
    return this.get<PluginTool[]>(`/api/plugins/${encodeURIComponent(id)}/tools`);
  }

  async discoverPlugin(id: string) {
    return this.get<PluginDiscovery>(`/api/plugins/${encodeURIComponent(id)}/discovery`);
  }

  async executePluginTool(id: string, tool: string, input: Record<string, unknown> = {}, sessionId?: string) {
    return this.post<PluginToolExecuteResult>(`/api/plugins/${encodeURIComponent(id)}/tools/${encodeURIComponent(tool)}/execute`, { input, sessionId });
  }

  async savePlugin(input: Omit<Partial<PluginRecord>, "manifest"> & { manifest?: unknown }) {
    return this.post<PluginRecord>("/api/plugins", input);
  }

  async patchPlugin(id: string, input: Omit<Partial<PluginRecord>, "manifest"> & { manifest?: unknown }) {
    return this.patch<PluginRecord>(`/api/plugins/${encodeURIComponent(id)}`, input);
  }

  async deletePlugin(id: string) {
    return this.del<{ ok: boolean }>(`/api/plugins/${encodeURIComponent(id)}`);
  }

  async listNotebooks() {
    return this.get<NotebookDocument[]>("/api/notebooks");
  }

  async createNotebook(input: { title: string; path: string; cells: Array<{ kind: string; language: string; source: string }> }) {
    return this.post<NotebookDocument>("/api/notebooks", input);
  }

  async importNotebook(input: { path: string; title?: string; notebook: Record<string, unknown> }) {
    return this.post<NotebookDocument>("/api/notebooks/import", input);
  }

  async runNotebookCell(notebookId: string, cellId: string, sessionId?: string) {
    return this.post<NotebookRunResult | PendingPermissionResult>(
      `/api/notebooks/${encodeURIComponent(notebookId)}/cells/${encodeURIComponent(cellId)}/run`,
      { sessionId }
    );
  }

  async getNotebookKernelStatus(notebookId: string) {
    return this.get<NotebookKernelStatus>(`/api/notebooks/${encodeURIComponent(notebookId)}/kernel/status`);
  }

  async restartNotebookKernel(notebookId: string) {
    return this.post<NotebookKernelStatus>(`/api/notebooks/${encodeURIComponent(notebookId)}/kernel/restart`, {});
  }

  async exportNotebook(notebookId: string) {
    return this.get<NotebookExport>(`/api/notebooks/${encodeURIComponent(notebookId)}/export`);
  }

  async cancelNotebookCell(notebookId: string, cellId: string) {
    return this.post<{ status: "cancelled"; cellId: string; notebookId: string }>(
      `/api/notebooks/${encodeURIComponent(notebookId)}/cells/${encodeURIComponent(cellId)}/cancel`,
      {}
    );
  }

  async createLSPSession(input: { language: string; path: string; text: string }) {
    return this.post<LSPSession>("/api/lsp/sessions", input);
  }

  async listLSPSessions() {
    return this.get<LSPSession[]>("/api/lsp/sessions");
  }

  async getLSPStatus(id: string) {
    return this.get<LSPStatus>(`/api/lsp/sessions/${encodeURIComponent(id)}/status`);
  }

  async listArtifacts() {
    return this.get<Artifact[]>("/api/artifacts");
  }

  async createArtifact(input: { sessionId?: string; type: string; title: string; content: string; metadata?: string }) {
    return this.post<Artifact>("/api/artifacts", input);
  }

  async publishShare(input: { title: string; artifactIds: string[]; visibility?: string; sessionId?: string }) {
    return this.post<ShareRecord | PendingPermissionResult>("/api/share/publish", input);
  }

  async getShare(id: string) {
    return this.get<ShareRecord>(`/api/share/${encodeURIComponent(id)}`);
  }

  async getShareReplay(id: string) {
    return this.get<ShareReplay>(`/api/share/${encodeURIComponent(id)}/replay`);
  }

  async listProvenance(entityId: string) {
    return this.get<ProvenanceEdge[]>(`/api/provenance/${encodeURIComponent(entityId)}`);
  }

  ptyWebSocketURL(id: string) {
    const path = `/api/pty/${id}/ws`;
    const base = this.base || window.location.origin;
    const url = new URL(buildUrl(base, path));
    url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
    return url.toString();
  }

  private async get<T>(path: string): Promise<T> {
    const response = await fetch(this.url(path));
    return decode<T>(response);
  }

  private async post<T>(path: string, body: unknown): Promise<T> {
    const response = await fetch(this.url(path), {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body)
    });
    return decode<T>(response);
  }

  private async put<T>(path: string, body: unknown): Promise<T> {
    const response = await fetch(this.url(path), {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body)
    });
    return decode<T>(response);
  }

  private async patch<T>(path: string, body: unknown): Promise<T> {
    const response = await fetch(this.url(path), {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body)
    });
    return decode<T>(response);
  }

  private async del<T>(path: string): Promise<T> {
    const response = await fetch(this.url(path), { method: "DELETE" });
    return decode<T>(response);
  }

  private url(path: string) {
    if (!this.base) return path;
    return buildUrl(this.base, path);
  }
}

async function decode<T>(response: Response): Promise<T> {
  if (!response.ok) {
    const text = await response.text();
    try {
      const payload = JSON.parse(text) as Partial<ApiErrorPayload>;
      if (typeof payload.code === "string" && typeof payload.message === "string") {
        throw new ApiError(
          {
            code: payload.code,
            message: payload.message,
            retryable: payload.retryable === true,
            details: payload.details && typeof payload.details === "object" ? payload.details : {},
            requestId: typeof payload.requestId === "string" ? payload.requestId : response.headers.get("X-Request-ID") ?? ""
          },
          response.status
        );
      }
    } catch (error) {
      if (error instanceof ApiError) throw error;
    }
    throw new Error(text || `request failed: ${response.status}`);
  }
  return (await response.json()) as T;
}
