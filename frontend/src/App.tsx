import { useEffect, useRef, useState } from "react";
import { useLocation, useNavigate } from "react-router";
import { AlertTriangle } from "lucide-react";
import type {
  AgentDefinition,
  Artifact,
  Credential,
  LSPSession,
  MCPServer,
  Message,
  NotebookDocument,
  PermissionRequest,
  PluginRecord,
  Project,
  ProviderProfile,
  ScienceDatabase,
  Session,
  SettingsValues,
  SkillRecord,
  ToolDefinition
} from "./lib/api";
import { client, onDesktopEvent } from "./lib/client";
import { defaultSettings } from "./lib/defaultSettings";
import { OnboardingScreen, type ProviderSetupInput, type ProviderVerificationResult } from "./features/onboarding/OnboardingScreen";
import { deriveStartupRoute } from "./features/onboarding/state";
import { TopBar } from "./components/TopBar";
import { ProjectLauncher } from "./features/launcher/ProjectLauncher";
import { Workspace } from "./features/workspace/Workspace";
import { SettingsDialog } from "./features/settings/SettingsDialog";

type View = "onboarding" | "launcher" | "workspace";
type VerifiedSelection = { provider: string; model: string; generation: number };

export function App() {
  const location = useLocation();
  const navigate = useNavigate();
  const view: View = location.pathname.startsWith("/workspace") ? "workspace" : location.pathname.startsWith("/projects") ? "launcher" : "onboarding";
  const [health, setHealth] = useState("checking");
  const [providerStatus, setProviderStatus] = useState("unconfigured");
  const [startupError, setStartupError] = useState("");
  const [ecosystemError, setEcosystemError] = useState("");
  const [projects, setProjects] = useState<Project[]>([]);
  const [activeProject, setActiveProject] = useState<Project | null>(null);
  const [sessions, setSessions] = useState<Session[]>([]);
  const [activeSession, setActiveSession] = useState<Session | null>(null);
  const [messages, setMessages] = useState<Message[]>([]);
  const [databases, setDatabases] = useState<ScienceDatabase[]>([]);
  const [credentials, setCredentials] = useState<Credential[]>([]);
  const [permissions, setPermissions] = useState<PermissionRequest[]>([]);
  const [providers, setProviders] = useState<ProviderProfile[]>([]);
  const [agents, setAgents] = useState<AgentDefinition[]>([]);
  const [toolsCatalog, setToolsCatalog] = useState<ToolDefinition[]>([]);
  const [skills, setSkills] = useState<SkillRecord[]>([]);
  const [mcpServers, setMcpServers] = useState<MCPServer[]>([]);
  const [plugins, setPlugins] = useState<PluginRecord[]>([]);
  const [notebooks, setNotebooks] = useState<NotebookDocument[]>([]);
  const [lspSessions, setLspSessions] = useState<LSPSession[]>([]);
  const [artifacts, setArtifacts] = useState<Artifact[]>([]);
  const [settings, setSettings] = useState<SettingsValues>(defaultSettings);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [dark, setDark] = useState(false);
  const localToolsRequested = useRef(false);
  const settingsRef = useRef<SettingsValues>(defaultSettings);
  const providerGeneration = useRef(0);
  const [verifiedSelection, setVerifiedSelection] = useState<VerifiedSelection | null>(null);
  const aiReady = health === "ready" && providerStatus === "ready" && verifiedSelection !== null
    && verifiedSelection.generation === providerGeneration.current
    && verifiedSelection.provider === settings.defaultProvider && verifiedSelection.model === settings.defaultModel;

  function applySettings(values: SettingsValues) {
    settingsRef.current = values;
    setSettings(values);
  }

  function beginProviderVerification() {
    const generation = ++providerGeneration.current;
    setVerifiedSelection(null);
    setProviderStatus("checking");
    return generation;
  }

  async function checkProvider(provider: string, model: string, generation: number) {
    const result = await client.providerHealth(provider, model);
    if (generation !== providerGeneration.current) return null;
    const ready = result.ok && result.status === "ready";
    setProviderStatus(ready ? "ready" : "error");
    setVerifiedSelection(ready ? { provider, model, generation } : null);
    setStartupError(ready ? "" : result.message ?? "Provider verification failed.");
    return result;
  }

  function setView(next: View) {
    navigate(next === "workspace" ? "/workspace" : next === "launcher" ? "/projects" : "/onboarding", { replace: true });
  }

  function openLocalTools() {
    if (health !== "ready") return;
    localToolsRequested.current = true;
    setView("launcher");
  }

  function configureProvider() {
    localToolsRequested.current = false;
    setView("onboarding");
  }

  useEffect(() => {
    document.documentElement.dataset.theme = dark ? "dark" : "light";
  }, [dark]);

  useEffect(() => {
    void initialize();
  }, []);

  useEffect(() => {
    const stopOpen = onDesktopEvent("desktop:open-project", (path) => {
      if (path) void openProject(path).catch(reportEcosystemError);
    });
    const stopError = onDesktopEvent("desktop:error", (message) => setEcosystemError(message));
    return () => { stopOpen(); stopError(); };
  }, []);

  async function initialize() {
    const generation = beginProviderVerification();
    setStartupError("");
    try {
      await client.health();
      setHealth("ready");
    } catch (error) {
      setHealth("offline");
      setStartupError(error instanceof Error ? error.message : "Local Superscience server is unavailable.");
      setView("onboarding");
      return;
    }

    void refreshHome().catch(reportEcosystemError);
    void refreshEcosystem().catch(reportEcosystemError);
    void client.listScienceDatabases().then(setDatabases).catch(reportEcosystemError);
    void client.listPermissions().then(setPermissions).catch(reportEcosystemError);

    try {
      const [values, providerRows, credentialRows] = await Promise.all([client.listSettings(), client.listProviders(), client.listCredentials()]);
      if (generation !== providerGeneration.current) return;
      const merged = { ...defaultSettings, ...values };
      applySettings(merged);
      setProviders(providerRows);
      setCredentials(credentialRows);
      setDark(merged.themePreference === "dark");

      const providerID = merged.defaultProvider ?? "";
      const modelID = merged.defaultModel ?? "";
      let status = "unconfigured";
      if (providerID && modelID) {
        const result = await checkProvider(providerID, modelID, generation);
        if (!result) return;
        status = result.ok && result.status === "ready" ? "ready" : "error";
      } else {
        setStartupError("Configure and verify a provider to enable AI chat, or open local tools.");
      }
      setProviderStatus(status);
      if (!localToolsRequested.current) setView(deriveStartupRoute({ serverStatus: "ready", providerId: providerID, modelId: modelID, providerStatus: status }));
    } catch (error) {
      if (generation !== providerGeneration.current) return;
      setProviderStatus("error");
      setStartupError(error instanceof Error ? error.message : "Provider readiness check failed.");
      if (!localToolsRequested.current) setView("onboarding");
    }

  }

  function reportEcosystemError(error: unknown) {
    setEcosystemError(error instanceof Error ? error.message : "A workspace service failed to load.");
  }

  async function refreshHome() {
    setProjects(await client.listProjects());
  }

  async function refreshEcosystem() {
    const [providerRows, agentRows, toolRows, skillRows, mcpRows, pluginRows, notebookRows, lspRows, artifactRows, permissionRows] = await Promise.all([
      client.listProviders(),
      client.listAgents(),
      client.listTools(),
      client.listSkills(),
      client.listMCPServers(),
      client.listPlugins(),
      client.listNotebooks(),
      client.listLSPSessions(),
      client.listArtifacts(),
      client.listPermissions()
    ]);
    setProviders(providerRows);
    setAgents(agentRows);
    setToolsCatalog(toolRows);
    setSkills(skillRows);
    setMcpServers(mcpRows);
    setPlugins(pluginRows);
    setNotebooks(notebookRows);
    setLspSessions(lspRows);
    setArtifacts(artifactRows);
    setPermissions(permissionRows);
    setEcosystemError("");
  }

  async function verifyProvider(input: ProviderSetupInput): Promise<ProviderVerificationResult> {
    const generation = beginProviderVerification();
    const superseded = (): ProviderVerificationResult => ({ ok: false, status: "error", message: "Provider selection changed. Verify the current selection again." });
    try {
      const probe = await client.testCredential(input);
      if (generation !== providerGeneration.current) return superseded();
      if (!probe.ok || probe.status !== "ready") {
        setProviderStatus("error");
        setStartupError(probe.message);
        return { ...probe, ok: false };
      }
      const profile = providers.find((item) => item.id === input.provider);
      if (!profile) {
        setProviderStatus("error");
        return { ok: false, status: "unsupported", message: "Selected provider is not registered." };
      }
      await client.saveProvider({ ...profile, baseURL: input.endpoint || profile.baseURL, enabled: true, secretRef: "" });
      if (generation !== providerGeneration.current) return superseded();
      const credential = await client.addCredential({ provider: input.provider, label: profile.displayName, key: input.key, source: "keychain" });
      if (generation !== providerGeneration.current) return superseded();
      setCredentials((current) => [credential, ...current.filter((item) => item.provider !== credential.provider)]);
      const saved = await client.updateSettings({ defaultProvider: input.provider, defaultModel: input.model, scienceNetwork: "live-only" });
      if (generation !== providerGeneration.current) return superseded();
      applySettings({ ...settingsRef.current, ...saved });

      const persistedProbe = await checkProvider(input.provider, input.model, generation);
      if (!persistedProbe) return superseded();
      if (!persistedProbe.ok || persistedProbe.status !== "ready") {
        return { ok: false, status: persistedProbe.status, message: persistedProbe.message ?? "Stored credentials could not be verified.", model: input.model, latencyMs: persistedProbe.latencyMs };
      }
      setView("launcher");
      await Promise.all([refreshHome(), refreshEcosystem()]);
      return { ok: true, status: "ready", message: `Verified ${profile.displayName} with ${input.model}.`, model: input.model, latencyMs: persistedProbe.latencyMs };
    } catch (error) {
      if (generation === providerGeneration.current) {
        setProviderStatus("error");
        setVerifiedSelection(null);
        setStartupError(error instanceof Error ? error.message : "Provider verification failed.");
      }
      throw error;
    }
  }

  async function openProject(path: string) {
    const project = await client.openProject(path);
    setActiveProject(project);
    const loaded = await client.listSessions(project.id);
    setSessions(loaded);
    if (loaded[0]) {
      await activateSession(loaded[0]);
    } else {
      const session = await client.createSession(project.id, "New research session", "research");
      setSessions([session]);
      await activateSession(session);
    }
    await Promise.all([refreshHome(), refreshEcosystem()]);
    setView("workspace");
  }

  async function activateSession(session: Session) {
    setActiveSession(session);
    setMessages(await client.listMessages(session.id));
  }

  async function newSession() {
    if (!activeProject) return;
    const session = await client.createSession(activeProject.id, "New research session", "research");
    setSessions((current) => [session, ...current]);
    await activateSession(session);
  }

  async function sendMessage(content: string) {
    if (!aiReady || verifiedSelection?.generation !== providerGeneration.current) throw new Error("AI chat is unavailable until a provider and model pass verification.");
    const selection = verifiedSelection;
    if (!activeProject) throw new Error("Open a project before sending a message.");
    let session = activeSession;
    if (!session) {
      session = await client.createSession(activeProject.id);
      setSessions((current) => [session!, ...current]);
      setActiveSession(session);
    }
    if (selection.generation !== providerGeneration.current) throw new Error("Provider selection changed. Verify it before sending a message.");
    const draftID = `draft_${Date.now()}`;
    let draft = "";
    let streamError = "";
    setMessages((current) => [...current, { id: draftID, sessionId: session!.id, role: "assistant", content: "", createdAt: Date.now() }]);
    try {
      await client.streamMessage(session.id, content, (event) => {
        if (event.type === "error") {
          streamError = event.content || "The provider stream failed.";
          return;
        }
        if (event.type === "message" && event.message) {
          setMessages((current) => {
            const withoutDraft = current.filter((message) => message.id !== draftID || event.message?.role !== "assistant");
            return withoutDraft.some((message) => message.id === event.message!.id) ? withoutDraft : [...withoutDraft, event.message!];
          });
        }
        if (event.type === "delta" && event.content) {
          draft += event.content;
          setMessages((current) => current.map((message) => message.id === draftID ? { ...message, content: draft } : message));
        }
        if (event.type === "permission" && event.permission) {
          setPermissions((current) => [event.permission!, ...current.filter((item) => item.id !== event.permission!.id)]);
        }
      }, { provider: selection.provider, model: selection.model, agent: session.agent });
    } finally {
      setMessages(await client.listMessages(session.id));
    }
    if (streamError) throw new Error(streamError);
  }

  async function updateSetting(key: string, value: string) {
    const changesProvider = key === "defaultProvider" || key === "defaultModel";
    const generation = changesProvider ? beginProviderVerification() : providerGeneration.current;
    try {
      const saved = await client.updateSettings({ [key]: value });
      if (changesProvider && generation !== providerGeneration.current) return;
      const nextSettings = changesProvider ? { ...settingsRef.current, ...saved } : { ...settingsRef.current, [key]: saved[key] ?? value };
      applySettings(nextSettings);
      if (key === "themePreference") setDark(value === "dark");
      if (!changesProvider) return;
      if (!nextSettings.defaultProvider || !nextSettings.defaultModel) {
        setProviderStatus("unconfigured");
        return;
      }
      await checkProvider(nextSettings.defaultProvider, nextSettings.defaultModel, generation);
    } catch (error) {
      if (changesProvider && generation === providerGeneration.current) {
        setProviderStatus("error");
        setStartupError(error instanceof Error ? error.message : "Provider verification failed.");
      }
      throw error;
    }
  }

  async function saveCredential(input: { provider: string; label: string; key: string; source?: string }) {
    if (input.provider === settingsRef.current.defaultProvider) beginProviderVerification();
    const credential = await client.addCredential(input);
    setCredentials((current) => [credential, ...current.filter((item) => item.provider !== credential.provider)]);
  }

  async function deleteCredential(id: string) {
    if (credentials.find((item) => item.id === id)?.provider === settingsRef.current.defaultProvider) {
      beginProviderVerification();
      setProviderStatus("unconfigured");
      setStartupError("The selected provider credential was removed. Verify a provider to enable AI.");
    }
    await client.deleteCredential(id);
    setCredentials((current) => current.filter((item) => item.id !== id));
  }

  async function respondPermission(id: string, decision: "once" | "always" | "reject") {
    await client.respondPermission(id, decision);
    setPermissions(await client.listPermissions());
    if (activeSession) setMessages(await client.listMessages(activeSession.id));
    await refreshEcosystem();
  }

  if (view === "onboarding") {
    return <OnboardingScreen providers={providers} initialError={startupError || (providerStatus !== "unconfigured" ? `Provider status: ${providerStatus}` : undefined)} loadModels={(providerID) => client.listProviderModels(providerID)} onVerify={verifyProvider} onOpenLocalTools={health === "ready" ? openLocalTools : undefined} />;
  }

  return (
    <div className="app-shell">
      <TopBar health={health} dark={dark} activeProject={activeProject} onHome={() => setView("launcher")} onSettings={() => setSettingsOpen(true)} onTheme={() => setDark((value) => !value)} />
      {ecosystemError ? <div className="global-error" role="alert"><AlertTriangle size={14} /><span>{ecosystemError}</span><button type="button" onClick={() => setEcosystemError("")}>Dismiss</button></div> : null}
      {view === "launcher" ? (
        <ProjectLauncher projects={projects} health={health} providerStatus={providerStatus} databases={databases} providers={providers} onOpen={openProject} />
      ) : (
        <Workspace
          project={activeProject} sessions={sessions} activeSession={activeSession} messages={messages} databases={databases} permissions={permissions}
          providers={providers} agents={agents} toolsCatalog={toolsCatalog} skills={skills} mcpServers={mcpServers} plugins={plugins}
          notebooks={notebooks} lspSessions={lspSessions} artifacts={artifacts} settings={settings} aiReady={aiReady} onConfigureProvider={configureProvider}
          onActivateSession={activateSession} onNewSession={newSession} onSendMessage={sendMessage} onPermission={respondPermission} onRefreshEcosystem={refreshEcosystem}
        />
      )}
      {settingsOpen ? (
        <SettingsDialog
          credentials={credentials} databases={databases} providers={providers} settings={settings} onClose={() => setSettingsOpen(false)} onSetting={updateSetting}
          onCredential={saveCredential}
          onCredentialDelete={deleteCredential}
        />
      ) : null}
    </div>
  );
}
