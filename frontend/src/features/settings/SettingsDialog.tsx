import { CheckCircle2, LoaderCircle, X } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import type { Credential, ModelProfile, ProviderProfile, ScienceDatabase, SettingsValues } from "../../lib/api";
import { client } from "../../lib/client";
import { defaultSettings } from "../../lib/defaultSettings";

export function SettingsDialog(props: {
  credentials: Credential[];
  databases: ScienceDatabase[];
  providers: ProviderProfile[];
  settings: SettingsValues;
  onClose: () => void;
  onSetting: (key: string, value: string) => Promise<void>;
  onCredential: (input: { provider: string; label: string; key: string; source?: string }) => Promise<void>;
  onCredentialDelete: (id: string) => Promise<void>;
}) {
  const panels: SettingsPanelName[] = ["General", "Models", "Credentials", "Science Connectors", "Memory", "Compute", "Permissions", "Storage", "Network"];
  const [panel, setPanel] = useState<SettingsPanelName>("Credentials");
  const values = useMemo(() => ({ ...defaultSettings, ...props.settings }), [props.settings]);
  return (
    <div className="modal-backdrop" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) props.onClose(); }}>
      <div className="settings-modal" role="dialog" aria-modal="true" aria-label="Settings">
        <nav>
          <strong>Settings</strong>
          {panels.map((item) => <button key={item} className={panel === item ? "active" : ""} onClick={() => setPanel(item)}>{item}</button>)}
          <small>Superscience local</small>
        </nav>
        <section>
          <header>
            <div><h2>{panel}</h2><p>{settingsPanelDescription(panel)}</p></div>
            <button className="icon-button" onClick={props.onClose} aria-label="Close settings"><X size={16} /></button>
          </header>
          {panel === "Credentials" ? <CredentialsSettings {...props} /> : null}
          {panel === "Science Connectors" ? <ScienceConnectorSettings databases={props.databases} settings={values} onSetting={props.onSetting} /> : null}
          {panel !== "Credentials" && panel !== "Science Connectors" ? <SettingsPanelContent panel={panel} settings={values} onSetting={props.onSetting} /> : null}
        </section>
      </div>
    </div>
  );
}

function CredentialsSettings(props: {
  credentials: Credential[];
  providers: ProviderProfile[];
  settings: SettingsValues;
  onSetting: (key: string, value: string) => Promise<void>;
  onCredential: (input: { provider: string; label: string; key: string; source?: string }) => Promise<void>;
  onCredentialDelete: (id: string) => Promise<void>;
}) {
  const cloudProviders = useMemo(() => props.providers.filter((provider) => provider.enabled && !["mock", "local", "ollama"].includes(provider.id)), [props.providers]);
  const [providerID, setProviderID] = useState(props.settings.defaultProvider || cloudProviders[0]?.id || "");
  const provider = cloudProviders.find((item) => item.id === providerID) ?? cloudProviders[0];
  const [endpoint, setEndpoint] = useState(provider?.baseURL ?? "");
  const [models, setModels] = useState<ModelProfile[]>([]);
  const [model, setModel] = useState(props.settings.defaultModel || "");
  const [key, setKey] = useState("");
  const [busy, setBusy] = useState(false);
  const [testMessage, setTestMessage] = useState("");
  const [testOK, setTestOK] = useState<boolean | null>(null);

  useEffect(() => {
    if (!providerID && cloudProviders[0]) setProviderID(cloudProviders[0].id);
  }, [cloudProviders, providerID]);

  useEffect(() => {
    const selected = cloudProviders.find((item) => item.id === providerID);
    setEndpoint(selected?.baseURL ?? "");
    if (!providerID) return;
    let active = true;
    client.listProviderModels(providerID).then((rows) => {
      if (!active) return;
      setModels(rows);
      if (!rows.some((item) => item.id === model)) setModel(rows[0]?.id ?? "");
    }).catch((error: unknown) => {
      if (!active) return;
      setModels([]);
      setTestOK(false);
      setTestMessage(error instanceof Error ? error.message : "Model catalog is unavailable.");
    });
    return () => { active = false; };
  }, [cloudProviders, providerID]);

  async function verifyAndSave() {
    if (!provider || !model.trim() || !key.trim()) {
      setTestOK(false);
      setTestMessage("Provider, model, and API key are required.");
      return;
    }
    setBusy(true);
    setTestMessage("");
    try {
      const result = await client.testCredential({ provider: provider.id, endpoint: endpoint.trim(), key: key.trim(), model: model.trim() });
      if (!result.ok) {
        setTestOK(false);
        setTestMessage(result.message);
        return;
      }
      await client.saveProvider({ ...provider, baseURL: endpoint.trim() || provider.baseURL, enabled: true });
      await props.onCredential({ provider: provider.id, label: provider.displayName, key: key.trim(), source: "keychain" });
      await props.onSetting("defaultProvider", provider.id);
      await props.onSetting("defaultModel", model.trim());
      setKey("");
      setTestOK(true);
      setTestMessage(`${provider.displayName} authenticated with ${model}${result.latencyMs !== undefined ? ` in ${result.latencyMs} ms` : ""}.`);
    } catch (error) {
      setTestOK(false);
      setTestMessage(error instanceof Error ? error.message : "Provider verification failed.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <>
      <div className="credential-form credential-provider-form">
        <select value={providerID} onChange={(event) => setProviderID(event.target.value)} aria-label="Provider">
          {cloudProviders.map((item) => <option key={item.id} value={item.id}>{item.displayName}</option>)}
        </select>
        <input value={endpoint} onChange={(event) => setEndpoint(event.target.value)} placeholder="Provider endpoint" aria-label="Provider endpoint" />
        <input list="settings-provider-models" value={model} onChange={(event) => setModel(event.target.value)} placeholder="Model ID" aria-label="Model" />
        <datalist id="settings-provider-models">{models.map((item) => <option key={item.id} value={item.id}>{item.displayName}</option>)}</datalist>
        <input type="password" autoComplete="off" placeholder="API key" value={key} onChange={(event) => setKey(event.target.value)} />
        <button className="primary" onClick={() => void verifyAndSave()} disabled={busy || !providerID || !model || !key.trim()}>{busy ? <LoaderCircle className="spin" size={14} /> : <CheckCircle2 size={14} />} Verify and save</button>
      </div>
      {testMessage ? <div className={`settings-note ${testOK === false ? "warn" : "ok"}`} role="status"><CheckCircle2 size={14} /><span>{testMessage}</span></div> : null}
      <div className="settings-table">
        {cloudProviders.map((item) => {
          const credential = props.credentials.find((row) => row.provider === item.id);
          return (
            <div className="settings-row" key={item.id}>
              <span>{item.displayName}</span><span>{credential?.redacted ?? "missing"}</span>
              <span className={credential ? "ok-text" : "warn-text"}>{credential ? "stored" : "not configured"}</span>
              <button className="ghost" disabled={!credential} onClick={() => credential && void props.onCredentialDelete(credential.id)}>Remove</button>
            </div>
          );
        })}
        {!cloudProviders.length ? <div className="empty">No cloud provider adapters are registered.</div> : null}
      </div>
    </>
  );
}

type SettingsPanelName = "General" | "Models" | "Credentials" | "Science Connectors" | "Memory" | "Compute" | "Permissions" | "Storage" | "Network";

type SettingSpec = {
  key: string;
  label: string;
  description: string;
  options: string[];
  kind?: "select" | "toggle";
  enabledValue?: string;
  disabledValue?: string;
  warnValues?: string[];
};

function settingsPanelDescription(panel: SettingsPanelName) {
  if (panel === "Credentials") return "Verify a real cloud request before storing a secret in the operating system credential service.";
  if (panel === "Science Connectors") return "Choose connector availability; live query failures remain visible and never produce fabricated records.";
  if (panel === "Permissions") return "Execution policies for shell, writes, notebook kernels, and networked tools.";
  if (panel === "Storage") return "SQLite, credential service, and reference repository retention behavior.";
  return "Configure persisted workbench behavior.";
}

function SettingsPanelContent(props: { panel: Exclude<SettingsPanelName, "Credentials" | "Science Connectors">; settings: SettingsValues; onSetting: (key: string, value: string) => Promise<void> }) {
  return <div className="settings-grid">{settingsSpecs(props.panel).map((item) => <SettingCard key={item.key} spec={item} value={props.settings[item.key] ?? defaultSettings[item.key] ?? item.options[0]} onSetting={props.onSetting} />)}</div>;
}

function ScienceConnectorSettings(props: { databases: ScienceDatabase[]; settings: SettingsValues; onSetting: (key: string, value: string) => Promise<void> }) {
  return (
    <div className="settings-grid">
      {props.databases.map((database) => {
        const key = `science.${database.id}.enabled`;
        const value = props.settings[key] ?? "enabled";
        return (
          <div className="settings-card" key={database.id}>
            <div><strong>{database.name}</strong><span>{database.description}</span><small>{database.domain}</small></div>
            <button className={`switch ${value === "enabled" ? "on" : ""}`} role="switch" aria-checked={value === "enabled"} onClick={() => void props.onSetting(key, value === "enabled" ? "disabled" : "enabled")}><span /></button>
            <em className={value === "enabled" ? "ok-text" : "warn-text"}>{value}</em>
          </div>
        );
      })}
    </div>
  );
}

function SettingCard(props: { spec: SettingSpec; value: string; onSetting: (key: string, value: string) => Promise<void> }) {
  const enabledValue = props.spec.enabledValue ?? "enabled";
  const disabledValue = props.spec.disabledValue ?? "disabled";
  const isOn = props.value === enabledValue;
  const warning = props.spec.warnValues?.includes(props.value) ?? false;
  return (
    <div className="settings-card">
      <div><strong>{props.spec.label}</strong><span>{props.spec.description}</span><small className={warning ? "warn-text" : "ok-text"}>{props.value}</small></div>
      {props.spec.kind === "toggle" ? <button className={`switch ${isOn ? "on" : ""}`} role="switch" aria-checked={isOn} onClick={() => void props.onSetting(props.spec.key, isOn ? disabledValue : enabledValue)}><span /></button> : <select value={props.value} onChange={(event) => void props.onSetting(props.spec.key, event.target.value)}>{props.spec.options.map((option) => <option key={option} value={option}>{option}</option>)}</select>}
    </div>
  );
}

function settingsSpecs(panel: Exclude<SettingsPanelName, "Credentials" | "Science Connectors">): SettingSpec[] {
  switch (panel) {
    case "General": return [
      { key: "themePreference", label: "Theme", description: "Window color mode preference.", options: ["system", "light", "dark"] },
      { key: "startupView", label: "Startup view", description: "First screen after readiness checks.", options: ["launcher", "workspace"] },
      { key: "notifications", label: "Notifications", description: "Native and browser notices for long-running tools.", kind: "toggle", options: ["enabled", "disabled"], warnValues: ["disabled"] }
    ];
    case "Models": return [{ key: "reasoningMode", label: "Reasoning mode", description: "Default runtime budget for agent planning.", options: ["fast", "balanced", "deep"] }];
    case "Memory": return [
      { key: "sessionMemory", label: "Session memory", description: "Persist session messages and tool outputs.", kind: "toggle", options: ["enabled", "disabled"], warnValues: ["disabled"] },
      { key: "textIndex", label: "Text index", description: "Local search backend for workspace files.", options: ["fts5", "filesystem", "disabled"], warnValues: ["disabled"] },
      { key: "vectorIndex", label: "Vector index", description: "Optional retrieval backend.", options: ["deferred", "sqlite-vec", "zvec"], warnValues: ["zvec"] }
    ];
    case "Compute": return [
      { key: "shellExecution", label: "Shell execution", description: "Policy for commands launched by tools.", options: ["approval", "disabled", "always"], warnValues: ["always"] },
      { key: "pythonWorker", label: "Jupyter worker", description: "Kernel worker preference; actual readiness is checked at execution.", options: ["available", "disabled"], warnValues: ["disabled"] },
      { key: "gpuProviders", label: "GPU providers", description: "Optional accelerated compute path.", options: ["disabled", "external-provider"], warnValues: ["external-provider"] }
    ];
    case "Permissions": return [
      { key: "shellPolicy", label: "Shell commands", description: "Approval model for terminal and bash tools.", options: ["approve", "deny", "always"], warnValues: ["always"] },
      { key: "writePolicy", label: "File writes", description: "Approval model for workspace modifications.", options: ["approve", "deny", "always"], warnValues: ["always"] },
      { key: "networkPolicy", label: "External network", description: "Approval model for networked tools.", options: ["approve", "deny", "always"], warnValues: ["always"] }
    ];
    case "Storage": return [
      { key: "databaseRetention", label: "Database retention", description: "SQLite retention behavior.", options: ["keep", "prune-on-exit"] },
      { key: "secretStorage", label: "Secret storage", description: "Provider credential backend.", options: ["keychain", "environment-only"], warnValues: ["environment-only"] },
      { key: "referencePolicy", label: "Reference repository", description: "How analysis references are treated.", options: ["ignored", "read-only", "disabled"] }
    ];
    case "Network": return [
      { key: "hostGuard", label: "Host guard", description: "Accepted hosts for the local API.", options: ["localhost-only", "disabled"], warnValues: ["disabled"] },
      { key: "originGuard", label: "Origin guard", description: "Accepted browser origins.", options: ["strict", "relaxed"], warnValues: ["relaxed"] },
      { key: "scienceNetwork", label: "Science APIs", description: "Connector failures are returned directly.", options: ["live-only"] }
    ];
  }
}
