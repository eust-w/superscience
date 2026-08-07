import { Activity, AlertTriangle, Bot, BrainCircuit, CheckCircle2, Plus, Power, Save, Trash2, X } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import type { AgentDefinition, ModelProfile, ProviderProfile, RunEvent, Session, SettingsValues, ToolDefinition } from "../../../lib/api";
import { client } from "../../../lib/client";

export function AgentsPanel(props: {
  agents: AgentDefinition[];
  toolsCatalog: ToolDefinition[];
  providers: ProviderProfile[];
  settings: SettingsValues;
  activeSession: Session | null;
  onRefresh: () => Promise<void>;
}) {
  const enabledAgents = props.agents.filter((agent) => agent.enabled);
  const enabledProviders = props.providers.filter((provider) => provider.enabled);
  const [goal, setGoal] = useState("");
  const [agentId, setAgentId] = useState("");
  const [providerId, setProviderId] = useState(props.settings.defaultProvider || "");
  const [modelId, setModelId] = useState(props.settings.defaultModel || "");
  const [models, setModels] = useState<ModelProfile[]>([]);
  const [events, setEvents] = useState<RunEvent[]>([]);
  const [runStatus, setRunStatus] = useState("");
  const [running, setRunning] = useState(false);
  const [error, setError] = useState("");
  const [managerOpen, setManagerOpen] = useState(false);
  const [savingAgent, setSavingAgent] = useState(false);
  const [agentAction, setAgentAction] = useState("");
  const [managerError, setManagerError] = useState("");
  const [draft, setDraft] = useState({
    id: "",
    name: "",
    domain: "general",
    description: "",
    systemPrompt: "",
    reviewMode: "optional",
    tools: ["read", "grep", "artifact", "provenance"] as string[]
  });

  useEffect(() => {
    if (!enabledAgents.some((agent) => agent.id === agentId)) setAgentId(enabledAgents[0]?.id ?? "");
  }, [agentId, enabledAgents]);

  useEffect(() => {
    if (!providerId) return;
    let active = true;
    client.listProviderModels(providerId).then((rows) => {
      if (!active) return;
      const catalog = Array.isArray(rows) ? rows : [];
      setModels(catalog);
      if (!modelId) {
        const configured = props.settings.defaultProvider === providerId ? props.settings.defaultModel : "";
        setModelId(configured || catalog[0]?.id || "");
      }
    }).catch((loadError: unknown) => {
      if (!active) return;
      setModels([]);
      setError(loadError instanceof Error ? loadError.message : "Model catalog is unavailable.");
    });
    return () => { active = false; };
  }, [providerId, modelId]);

  useEffect(() => {
    const sessionId = props.activeSession?.id;
    setEvents([]);
    setRunStatus("");
    if (!sessionId) return;
    let active = true;
    client.listSessionRuns(sessionId).then(async (runs) => {
      if (!active || !runs.length) return;
      const latest = runs[0];
      const persistedEvents = await client.listRunEvents(latest.id);
      if (!active) return;
      setEvents(persistedEvents);
      setRunStatus(latest.status);
    }).catch((loadError: unknown) => {
      if (!active) return;
      setError(loadError instanceof Error ? loadError.message : "The latest agent run could not be restored.");
    });
    return () => { active = false; };
  }, [props.activeSession?.id]);

  const selectedAgent = useMemo(() => enabledAgents.find((agent) => agent.id === agentId), [agentId, enabledAgents]);

  async function runAgent() {
    if (!props.activeSession || !goal.trim() || !agentId || !providerId || !modelId) return;
    setRunning(true);
    setError("");
    setEvents([]);
    try {
      const result = await client.runAgent(props.activeSession.id, { goal: goal.trim(), agent: agentId, provider: providerId, model: modelId });
      const runId = typeof result.id === "string" ? result.id : "";
      setRunStatus(typeof result.status === "string" ? result.status : "submitted");
      if (runId) setEvents(await client.listRunEvents(runId));
      await props.onRefresh();
    } catch (runError) {
      setRunStatus("error");
      setError(runError instanceof Error ? runError.message : "The agent run failed.");
    } finally {
      setRunning(false);
    }
  }

  function toggleDraftTool(toolId: string) {
    setDraft((current) => ({
      ...current,
      tools: current.tools.includes(toolId) ? current.tools.filter((id) => id !== toolId) : [...current.tools, toolId]
    }));
  }

  async function createAgent() {
    const id = draft.id.trim() || slugAgentID(draft.name);
    if (!id || !draft.name.trim() || !draft.description.trim()) return;
    setSavingAgent(true);
    setManagerError("");
    try {
      const created = await client.saveAgent({
        id,
        name: draft.name.trim(),
        domain: draft.domain.trim() || "general",
        description: draft.description.trim(),
        systemPrompt: draft.systemPrompt.trim(),
        tools: draft.tools,
        reviewMode: draft.reviewMode,
        enabled: true
      });
      await props.onRefresh();
      setAgentId(created.id);
      setDraft({ id: "", name: "", domain: "general", description: "", systemPrompt: "", reviewMode: "optional", tools: ["read", "grep", "artifact", "provenance"] });
      setManagerOpen(false);
    } catch (saveError) {
      setManagerError(saveError instanceof Error ? saveError.message : "The agent could not be saved.");
    } finally {
      setSavingAgent(false);
    }
  }

  async function toggleAgent(agent: AgentDefinition) {
    if (agent.builtIn) return;
    setAgentAction(agent.id);
    setManagerError("");
    try {
      await client.updateAgent(agent.id, { enabled: !agent.enabled });
      await props.onRefresh();
    } catch (updateError) {
      setManagerError(updateError instanceof Error ? updateError.message : "The agent could not be updated.");
    } finally {
      setAgentAction("");
    }
  }

  async function deleteAgent(agent: AgentDefinition) {
    if (agent.builtIn || !window.confirm(`Delete agent ${agent.name}?`)) return;
    setAgentAction(agent.id);
    setManagerError("");
    try {
      await client.deleteAgent(agent.id);
      await props.onRefresh();
    } catch (deleteError) {
      setManagerError(deleteError instanceof Error ? deleteError.message : "The agent could not be deleted.");
    } finally {
      setAgentAction("");
    }
  }

  return (
    <div className="pane-body agents">
      <section className="ecosystem-card agent-composer">
        <h3><BrainCircuit size={15} /> Agent run</h3>
        <div className="agent-selectors">
          <select aria-label="Agent" value={agentId} onChange={(event) => setAgentId(event.target.value)}>
            {enabledAgents.map((agent) => <option key={agent.id} value={agent.id}>{agent.name}</option>)}
          </select>
          <select aria-label="Provider" value={providerId} onChange={(event) => setProviderId(event.target.value)}>
            <option value="">Select provider</option>
            {enabledProviders.map((provider) => <option key={provider.id} value={provider.id}>{provider.displayName}</option>)}
          </select>
          <select aria-label="Model" value={modelId} onChange={(event) => setModelId(event.target.value)} disabled={!providerId}>
            <option value="">Select model</option>
            {modelId && !models.some((model) => model.id === modelId) ? <option value={modelId}>{modelId}</option> : null}
            {models.map((model) => <option key={model.id} value={model.id}>{model.displayName || model.id}</option>)}
          </select>
        </div>
        <textarea value={goal} onChange={(event) => setGoal(event.target.value)} placeholder="Research objective" />
        <div className="panel-action-row">
          <span className="muted">{selectedAgent ? `${selectedAgent.reviewMode} review · ${selectedAgent.tools.length} tools` : "No agent available"}</span>
          <button className="primary" onClick={() => void runAgent()} disabled={running || !props.activeSession || !goal.trim() || !providerId || !modelId}>
            <Power size={13} /> {running ? "Running" : "Run"}
          </button>
        </div>
        {error ? <div className="panel-error" role="alert"><AlertTriangle size={14} /> {error}</div> : null}
      </section>
      <section className="run-timeline" aria-live="polite">
        <div className="timeline-head"><Activity size={14} /><strong>Timeline</strong><span>{runStatus || "idle"}</span></div>
        {events.map((event) => (
          <div className={`timeline-row ${event.status}`} key={event.id}>
            {event.status === "ok" || event.status === "completed" ? <CheckCircle2 size={13} /> : <Activity size={13} />}
            <div><strong>{event.title || event.tool || event.kind || event.type}</strong><span>{event.content || event.output || event.status}</span></div>
          </div>
        ))}
        {!events.length ? <div className="empty">Run events will appear here without synthesized steps.</div> : null}
      </section>
      <section className="ecosystem-card agent-manager">
        <div className="section-toolbar">
          <h3><Bot size={15} /> Agent registry</h3>
          <button type="button" className="secondary compact" onClick={() => setManagerOpen((open) => !open)}>
            {managerOpen ? <X size={13} /> : <Plus size={13} />}{managerOpen ? "Cancel" : "New agent"}
          </button>
        </div>
        {managerOpen ? (
          <div className="agent-definition-form">
            <div className="agent-form-grid">
              <input aria-label="Agent name" placeholder="Agent name" value={draft.name} onChange={(event) => setDraft((current) => ({ ...current, name: event.target.value }))} />
              <input aria-label="Agent ID" placeholder={slugAgentID(draft.name) || "agent-id"} value={draft.id} onChange={(event) => setDraft((current) => ({ ...current, id: event.target.value }))} />
              <input aria-label="Agent domain" placeholder="Domain" value={draft.domain} onChange={(event) => setDraft((current) => ({ ...current, domain: event.target.value }))} />
              <select aria-label="Review mode" value={draft.reviewMode} onChange={(event) => setDraft((current) => ({ ...current, reviewMode: event.target.value }))}>
                <option value="none">No review</option>
                <option value="optional">Optional review</option>
                <option value="recommended">Recommended review</option>
                <option value="required">Required review</option>
              </select>
            </div>
            <textarea aria-label="Agent description" placeholder="Description" value={draft.description} onChange={(event) => setDraft((current) => ({ ...current, description: event.target.value }))} />
            <textarea aria-label="Agent system prompt" placeholder="System prompt" value={draft.systemPrompt} onChange={(event) => setDraft((current) => ({ ...current, systemPrompt: event.target.value }))} />
            <div className="agent-tool-picker">
              {props.toolsCatalog.filter((tool) => tool.enabled).map((tool) => (
                <label key={tool.id} className={tool.requiresApproval ? "warn" : ""}>
                  <input type="checkbox" checked={draft.tools.includes(tool.id)} onChange={() => toggleDraftTool(tool.id)} />
                  <span>{tool.id}</span>
                </label>
              ))}
            </div>
            <div className="panel-action-row">
              <span className="muted">{draft.tools.length} tools</span>
              <button type="button" className="primary" onClick={() => void createAgent()} disabled={savingAgent || !draft.name.trim() || !draft.description.trim()}>
                <Save size={13} />{savingAgent ? "Saving" : "Save agent"}
              </button>
            </div>
          </div>
        ) : null}
        {managerError ? <div className="panel-error" role="alert"><AlertTriangle size={14} />{managerError}</div> : null}
        <div className="agent-registry-list">
          {props.agents.map((agent) => (
            <div className={`agent-registry-row ${agent.builtIn ? "built-in" : ""}`} key={agent.id}>
              <button type="button" className="agent-registry-select" onClick={() => agent.enabled && setAgentId(agent.id)} disabled={!agent.enabled}>
                <strong>{agent.name}</strong>
                <span>{agent.domain} · {agent.reviewMode} · {agent.tools.length} tools</span>
              </button>
              <span className={`status-chip ${agent.enabled ? "ready" : "unavailable"}`}>{agent.builtIn ? "built-in" : agent.enabled ? "enabled" : "disabled"}</span>
              {!agent.builtIn ? (
                <>
                  <button type="button" className="icon-button small" title={agent.enabled ? "Disable agent" : "Enable agent"} aria-label={agent.enabled ? `Disable ${agent.name}` : `Enable ${agent.name}`} onClick={() => void toggleAgent(agent)} disabled={agentAction === agent.id}><Power size={13} /></button>
                  <button type="button" className="icon-button small danger" title="Delete agent" aria-label={`Delete ${agent.name}`} onClick={() => void deleteAgent(agent)} disabled={agentAction === agent.id}><Trash2 size={13} /></button>
                </>
              ) : null}
            </div>
          ))}
        </div>
      </section>
      <div className="tool-matrix">
        {props.toolsCatalog.filter((tool) => tool.enabled).slice(0, 16).map((tool) => (
          <span key={tool.id} className={`tool-pill ${tool.requiresApproval ? "warn" : ""}`}><Bot size={11} />{tool.id}</span>
        ))}
      </div>
    </div>
  );
}

function slugAgentID(value: string) {
  return value.trim().toLowerCase().replace(/[^a-z0-9_-]+/g, "-").replace(/^-+|-+$/g, "").slice(0, 64);
}
