import { AlertTriangle, BrainCircuit, Cloud, KeyRound, PlugZap, RefreshCw, Search, ShieldCheck } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import type { Artifact, MCPDiscovery, MCPServer, PluginDiscovery, PluginRecord, ProviderProfile, Session, SettingsValues, SkillRecord } from "../../../lib/api";
import { client } from "../../../lib/client";

type EcosystemTab = "providers" | "skills" | "mcp" | "plugins" | "share";

export function EcosystemPanel(props: {
  providers: ProviderProfile[];
  skills: SkillRecord[];
  mcpServers: MCPServer[];
  plugins: PluginRecord[];
  artifacts: Artifact[];
	settings: SettingsValues;
  activeSession: Session | null;
  onRefresh: () => Promise<void>;
}) {
  const [tab, setTab] = useState<EcosystemTab>("providers");
  const [status, setStatus] = useState("");
  const [error, setError] = useState("");

  async function action(run: () => Promise<string>) {
    setError("");
    setStatus("Working...");
    try {
      setStatus(await run());
      await props.onRefresh();
    } catch (actionError) {
      setStatus("");
      setError(actionError instanceof Error ? actionError.message : "The ecosystem action failed.");
    }
  }

  return (
    <div className="pane-body ecosystem-panel">
      <div className="ecosystem-tabs" role="tablist">
        {(["providers", "skills", "mcp", "plugins", "share"] as const).map((item) => (
          <button key={item} className={tab === item ? "active" : ""} onClick={() => { setTab(item); setStatus(""); setError(""); }}>
            {item}
          </button>
        ))}
      </div>
      {error ? <div className="panel-error" role="alert"><AlertTriangle size={14} /> {error}</div> : null}
      {status ? <div className="panel-status" role="status">{status}</div> : null}
	  {tab === "providers" ? <ProviderManager providers={props.providers} settings={props.settings} onAction={action} /> : null}
      {tab === "skills" ? <SkillManager skills={props.skills} onAction={action} /> : null}
      {tab === "mcp" ? <MCPManager servers={props.mcpServers} session={props.activeSession} onAction={action} /> : null}
      {tab === "plugins" ? <PluginManager plugins={props.plugins} onAction={action} /> : null}
      {tab === "share" ? <ShareManager artifacts={props.artifacts} session={props.activeSession} onAction={action} /> : null}
    </div>
  );
}

function ProviderManager({ providers, settings, onAction }: { providers: ProviderProfile[]; settings: SettingsValues; onAction: (run: () => Promise<string>) => Promise<void> }) {
  async function check(provider: ProviderProfile) {
    const models = await client.listProviderModels(provider.id);
	const model = models[0]?.id || (settings.defaultProvider === provider.id ? settings.defaultModel : "");
	if (!model) throw new Error(`${provider.displayName} has no configured or catalog model ID.`);
	const health = await client.providerHealth(provider.id, model);
    if (!health.ok) throw new Error(health.message || `${provider.displayName} is ${health.status}.`);
	return `${provider.displayName} authenticated with ${health.model || model} in ${health.latencyMs ?? 0} ms.`;
  }
  return (
    <section className="ecosystem-card">
      <div className="section-toolbar">
        <h3><BrainCircuit size={15} /> Providers</h3>
        <button className="secondary" onClick={() => void onAction(async () => {
          const result = await client.refreshProviderModels();
          return `${result.source}: ${result.providers} providers, ${result.models} models.`;
        })}><RefreshCw size={13} /> Refresh catalog</button>
      </div>
      <div className="provider-list">
        {providers.map((provider) => (
          <div key={provider.id} className="provider-row">
            <span>{provider.displayName}</span>
            <small>{provider.kind}</small>
            <strong>{provider.enabled ? "enabled" : "disabled"}</strong>
            <button className="icon-button small" aria-label={`Test ${provider.displayName}`} disabled={!provider.enabled} onClick={() => void onAction(() => check(provider))}><ShieldCheck size={13} /></button>
          </div>
        ))}
      </div>
    </section>
  );
}

function SkillManager({ skills, onAction }: { skills: SkillRecord[]; onAction: (run: () => Promise<string>) => Promise<void> }) {
  return (
    <section className="ecosystem-card">
      <h3><KeyRound size={15} /> Skills</h3>
      <div className="provider-list">
        {skills.map((skill) => (
          <div key={skill.id || skill.name} className="provider-row">
            <span>{skill.name}</span><small>{skill.source}</small><strong>{!skill.enabled ? "disabled" : skill.entry === false ? "helper" : "entry"}</strong>
            <button className="secondary compact" disabled={!skill.enabled} onClick={() => void onAction(async () => {
              const loaded = await client.loadSkill(skill.name);
              return `${loaded.name} loaded from ${loaded.source}.`;
            })}>Load</button>
          </div>
        ))}
        {!skills.length ? <div className="empty">No bundled, project, or user skills were found.</div> : null}
      </div>
    </section>
  );
}

function MCPManager(props: { servers: MCPServer[]; session: Session | null; onAction: (run: () => Promise<string>) => Promise<void> }) {
  const [name, setName] = useState("");
  const [transport, setTransport] = useState("stdio");
  const [command, setCommand] = useState("");
  const [args, setArgs] = useState("");
  const [endpoint, setEndpoint] = useState("");
  const [clientId, setClientId] = useState("");
  const [clientSecret, setClientSecret] = useState("");
  const [scopes, setScopes] = useState("");
  const [discovery, setDiscovery] = useState<Record<string, MCPDiscovery>>({});

  async function discover(server: MCPServer) {
    const result = await client.discoverMCPServer(server.id);
    setDiscovery((current) => ({ ...current, [server.id]: result }));
    return `${server.name}: ${result.tools.length} tools, ${result.resources.length} resources, ${result.prompts.length} prompts (${result.status || "live"}).`;
  }

  return (
    <section className="ecosystem-card">
      <h3><PlugZap size={15} /> MCP servers</h3>
      <div className="mcp-config-grid">
        <input value={name} onChange={(event) => setName(event.target.value)} placeholder="Server name" />
        <select value={transport} onChange={(event) => setTransport(event.target.value)}><option value="stdio">stdio</option><option value="streamable-http">Streamable HTTP</option><option value="sse">SSE</option></select>
        {transport === "stdio" ? (
          <><input value={command} onChange={(event) => setCommand(event.target.value)} placeholder="Executable path" /><textarea value={args} onChange={(event) => setArgs(event.target.value)} placeholder="One argument per line" /></>
        ) : <input value={endpoint} onChange={(event) => setEndpoint(event.target.value)} placeholder="HTTPS endpoint" />}
        <button className="primary" disabled={!name.trim() || (transport === "stdio" ? !command.trim() : !endpoint.trim())} onClick={() => void props.onAction(async () => {
          const saved = await client.addMCPServer({ name: name.trim(), transport, command: command.trim(), args: args.split("\n").map((item) => item.trim()).filter(Boolean), url: endpoint.trim(), enabled: true });
          return `${saved.name} configured. Discovery has not run yet.`;
        })}>Add server</button>
      </div>
      <div className="provider-list">
        {props.servers.map((server) => (
          <div key={server.id} className="mcp-server-row">
            <div><strong>{server.name}</strong><span>{server.transport} · {server.oauthStatus || server.status}</span></div>
            <button className="secondary compact" onClick={() => void props.onAction(() => discover(server))}><Search size={12} /> Discover</button>
            {server.transport !== "stdio" ? <button className="secondary compact" disabled={!clientId.trim()} onClick={() => void props.onAction(async () => {
              const flow = await client.startMCPOAuth(server.id, { clientId: clientId.trim(), clientSecret, scopes: scopes.split(/\s+/).filter(Boolean) });
              window.open(flow.authorizationUrl, "_blank", "noopener,noreferrer");
              return `OAuth flow opened for ${server.name}; expires at ${new Date(flow.expiresAt ?? Date.now()).toLocaleTimeString()}.`;
            })}>Authorize</button> : null}
            {server.oauthStatus === "authorized" ? <button className="secondary compact" onClick={() => void props.onAction(async () => { await client.removeMCPOAuth(server.id); return `${server.name} OAuth credential removed.`; })}>Remove OAuth</button> : null}
            {discovery[server.id] ? <span className="muted">{discovery[server.id].tools.length} tools</span> : null}
          </div>
        ))}
      </div>
      {props.servers.some((server) => server.transport !== "stdio") ? (
        <div className="oauth-fields"><input value={clientId} onChange={(event) => setClientId(event.target.value)} placeholder="OAuth client ID" /><input type="password" value={clientSecret} onChange={(event) => setClientSecret(event.target.value)} placeholder="Client secret (optional)" /><input value={scopes} onChange={(event) => setScopes(event.target.value)} placeholder="Scopes separated by spaces" /></div>
      ) : null}
    </section>
  );
}

function PluginManager({ plugins, onAction }: { plugins: PluginRecord[]; onAction: (run: () => Promise<string>) => Promise<void> }) {
  const [discovery, setDiscovery] = useState<Record<string, PluginDiscovery>>({});
  return (
    <section className="ecosystem-card">
      <h3><PlugZap size={15} /> Plugins</h3>
      <div className="provider-list">
        {plugins.map((plugin) => (
          <div key={plugin.id} className="mcp-server-row">
            <div><strong>{plugin.name}</strong><span>{plugin.runtime} · {plugin.status}</span></div>
            <button className="secondary compact" disabled={!plugin.enabled} onClick={() => void onAction(async () => {
              const result = await client.discoverPlugin(plugin.id);
              setDiscovery((current) => ({ ...current, [plugin.id]: result }));
              return `${result.pluginName} registered ${result.tools.length} tools, ${result.providers.length} providers, and ${result.hooks.length} hooks.`;
            })}>Handshake</button>
            {discovery[plugin.id] ? <span className="muted">{discovery[plugin.id].protocolVersion}</span> : null}
          </div>
        ))}
        {!plugins.length ? <div className="empty">No subprocess plugins are configured.</div> : null}
      </div>
    </section>
  );
}

function ShareManager(props: { artifacts: Artifact[]; session: Session | null; onAction: (run: () => Promise<string>) => Promise<void> }) {
  const sessionArtifacts = useMemo(
    () => props.artifacts.filter((artifact) => artifact.sessionId === props.session?.id),
    [props.artifacts, props.session?.id]
  );
  const preferredArtifact = sessionArtifacts.find((artifact) => artifact.type !== "agent-run") ?? sessionArtifacts[0];
  const [artifactId, setArtifactId] = useState("");

  useEffect(() => {
    if (!sessionArtifacts.some((artifact) => artifact.id === artifactId)) {
      setArtifactId(preferredArtifact?.id ?? "");
    }
  }, [artifactId, preferredArtifact?.id, sessionArtifacts]);

  const selectedArtifact = sessionArtifacts.find((artifact) => artifact.id === artifactId);
  return (
    <section className="ecosystem-card">
      <h3><Cloud size={15} /> Publish</h3>
      <p className="muted">{sessionArtifacts.length} artifacts are available for the current session.</p>
      <label>
        <span>Artifact</span>
        <select aria-label="Artifact" value={artifactId} onChange={(event) => setArtifactId(event.target.value)} disabled={!sessionArtifacts.length}>
          {!sessionArtifacts.length ? <option value="">No session artifacts</option> : null}
          {sessionArtifacts.map((artifact) => <option key={artifact.id} value={artifact.id}>{artifact.title} · {artifact.type}</option>)}
        </select>
      </label>
      <button className="primary" disabled={!selectedArtifact} onClick={() => void props.onAction(async () => {
        if (!selectedArtifact) throw new Error("Select an artifact from the active session.");
        const result = await client.publishShare({ title: selectedArtifact.title, artifactIds: [selectedArtifact.id], visibility: "private-link", sessionId: props.session?.id });
        if ("permissionId" in result) return `Approval required: ${result.permissionId}`;
        const replay = await client.getShareReplay(result.id);
        return `Published ${result.url}; replay contains ${replay.artifacts.length} artifact(s).`;
      })}><Cloud size={13} /> Publish selected artifact</button>
    </section>
  );
}
