import { lazy, Suspense, useState } from "react";
import {
  AlertTriangle,
  Beaker,
  BookOpen,
  Bot,
  CheckCircle2,
  FileText,
  LoaderCircle,
  PlugZap,
  Plus,
  Send,
  ShieldAlert,
  TerminalSquare
} from "lucide-react";
import type {
  AgentDefinition,
  Artifact,
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
} from "../../lib/api";

const FilesPanel = lazy(() => import("./panels/FilesPanel").then((module) => ({ default: module.FilesPanel })));
const TerminalPanel = lazy(() => import("./panels/TerminalPanel").then((module) => ({ default: module.TerminalPanel })));
const SciencePanel = lazy(() => import("./panels/SciencePanel").then((module) => ({ default: module.SciencePanel })));
const AgentsPanel = lazy(() => import("./panels/AgentsPanel").then((module) => ({ default: module.AgentsPanel })));
const EcosystemPanel = lazy(() => import("./panels/EcosystemPanel").then((module) => ({ default: module.EcosystemPanel })));
const NotebookPanel = lazy(() => import("./panels/NotebookPanel").then((module) => ({ default: module.NotebookPanel })));

type RightTab = "files" | "terminal" | "science" | "agents" | "ecosystem" | "notebook";

export function Workspace(props: {
  project: Project | null;
  sessions: Session[];
  activeSession: Session | null;
  messages: Message[];
  databases: ScienceDatabase[];
  permissions: PermissionRequest[];
  providers: ProviderProfile[];
  agents: AgentDefinition[];
  toolsCatalog: ToolDefinition[];
  skills: SkillRecord[];
  mcpServers: MCPServer[];
  plugins: PluginRecord[];
  notebooks: NotebookDocument[];
  lspSessions: LSPSession[];
  artifacts: Artifact[];
  settings: SettingsValues;
  aiReady: boolean;
  onConfigureProvider: () => void;
  onActivateSession: (session: Session) => Promise<void>;
  onNewSession: () => Promise<void>;
  onSendMessage: (content: string) => Promise<void>;
  onPermission: (id: string, decision: "once" | "always" | "reject") => Promise<void>;
  onRefreshEcosystem: () => Promise<void>;
}) {
  const [rightTab, setRightTab] = useState<RightTab>("files");
  const [prompt, setPrompt] = useState("");
  const [sending, setSending] = useState(false);
  const [sendError, setSendError] = useState("");
  const pendingPermission = props.permissions.find((permission) => !props.activeSession || permission.sessionId === props.activeSession.id) ?? props.permissions[0];

  async function submit() {
    const content = prompt.trim();
    if (!content || sending || !props.aiReady) return;
    setSending(true);
    setSendError("");
    try {
      await props.onSendMessage(content);
      setPrompt("");
    } catch (error) {
      setSendError(error instanceof Error ? error.message : "The provider request failed.");
    } finally {
      setSending(false);
    }
  }

  return (
    <main className="workspace">
      <aside className="session-rail">
        <div className="rail-head">
          <span>Research sessions</span>
          <button className="icon-button small" aria-label="New session" onClick={() => void props.onNewSession()}><Plus size={14} /></button>
        </div>
        <div className="session-list">
          {props.sessions.map((session) => (
            <button key={session.id} className={`session-item ${props.activeSession?.id === session.id ? "active" : ""}`} onClick={() => void props.onActivateSession(session)}>
              <Bot size={14} />
              <span>{session.title}</span>
              <small>{session.agent} · {session.status}</small>
            </button>
          ))}
          {!props.sessions.length ? <div className="empty">No research sessions.</div> : null}
        </div>
      </aside>
      <section className="chat-panel">
        <div className="workspace-toolbar">
          <div><strong>{props.project?.name ?? "Workspace"}</strong><span>{props.activeSession?.title ?? "No session selected"}</span></div>
          <div className="toolbar-controls">
            <span className="badge">{props.activeSession?.agent || "research"}</span>
            <span className="badge">{modelLabel(props.settings)}</span>
            <span className="badge warn"><ShieldAlert size={12} /> approvals</span>
          </div>
        </div>
        {pendingPermission ? (
          <div className="permission-banner">
            <ShieldAlert size={16} />
            <span>{pendingPermission.message} <strong>{pendingPermission.type}:{pendingPermission.pattern}</strong></span>
            <button onClick={() => void props.onPermission(pendingPermission.id, "once")}>Approve once</button>
            <button onClick={() => void props.onPermission(pendingPermission.id, "always")}>Always</button>
            <button className="ghost" onClick={() => void props.onPermission(pendingPermission.id, "reject")}>Deny</button>
          </div>
        ) : (
          <div className="permission-banner quiet"><CheckCircle2 size={16} /><span>No pending approvals. Risky tools remain gated at execution time.</span></div>
        )}
        <div className="message-log" aria-live="polite">
          {!props.messages.length ? (
            <div className="empty-state"><Beaker size={22} /><h2>Research workspace</h2><p>Start a provider-backed conversation or open a focused tool in the right pane.</p></div>
          ) : null}
          {props.messages.map((message) => (
            <article className={`message ${message.role}`} key={message.id}>
              <header><span>{message.role === "user" ? "You" : message.role === "assistant" ? "Superscience" : "System"}</span><time>{formatTimestamp(message.createdAt)}</time></header>
              <p>{message.content}</p>
            </article>
          ))}
        </div>
        {sendError ? <div className="composer-error" role="alert"><AlertTriangle size={14} />{sendError}</div> : null}
        {!props.aiReady ? (
          <div className="composer-error" role="status">
            <AlertTriangle size={14} />
            <span>AI chat is unavailable until a provider and model pass verification.</span>
            <button className="secondary" type="button" onClick={props.onConfigureProvider}>Configure AI provider</button>
          </div>
        ) : null}
        <div className="composer">
          <span className="composer-agent"><Bot size={14} />{props.activeSession?.agent || "research"}</span>
          <textarea
            value={prompt}
            onChange={(event) => setPrompt(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter" && !event.shiftKey) {
                event.preventDefault();
                void submit();
              }
            }}
            placeholder="Ask Superscience"
            disabled={!props.aiReady}
          />
          <button className="primary send" aria-label="Send message" onClick={() => void submit()} disabled={!props.aiReady || sending || !prompt.trim()}>
            {sending ? <LoaderCircle className="spin" size={16} /> : <Send size={16} />}
          </button>
        </div>
      </section>
      <RightPane {...props} active={rightTab} onTab={setRightTab} />
    </main>
  );
}

function RightPane(props: Parameters<typeof Workspace>[0] & { active: RightTab; onTab: (tab: RightTab) => void }) {
  const tabs = [
    ["files", FileText],
    ["terminal", TerminalSquare],
    ["science", Beaker],
    ["agents", Bot],
    ["ecosystem", PlugZap],
    ["notebook", BookOpen]
  ] as const;
  return (
    <aside className="right-pane">
      <div className="tabs" role="tablist">
        {tabs.map(([tab, Icon]) => <button key={tab} className={props.active === tab ? "active" : ""} onClick={() => props.onTab(tab)}><Icon size={14} />{tab[0].toUpperCase() + tab.slice(1)}</button>)}
      </div>
      <Suspense fallback={<div className="panel-loading"><LoaderCircle className="spin" size={16} />Loading tool...</div>}>
        {props.active === "files" ? <FilesPanel session={props.activeSession} onRefresh={props.onRefreshEcosystem} /> : null}
        {props.active === "terminal" ? <TerminalPanel session={props.activeSession} onRefresh={props.onRefreshEcosystem} /> : null}
        {props.active === "science" ? <SciencePanel databases={props.databases} session={props.activeSession} onSend={async (content) => { if (!props.aiReady) { props.onConfigureProvider(); return; } await props.onSendMessage(content); }} onArtifactCreated={props.onRefreshEcosystem} /> : null}
        {props.active === "agents" ? <AgentsPanel aiReady={props.aiReady} onConfigureProvider={props.onConfigureProvider} agents={props.agents} toolsCatalog={props.toolsCatalog} providers={props.providers} settings={props.settings} activeSession={props.activeSession} onRefresh={props.onRefreshEcosystem} /> : null}
        {props.active === "ecosystem" ? <EcosystemPanel providers={props.providers} skills={props.skills} mcpServers={props.mcpServers} plugins={props.plugins} artifacts={props.artifacts} settings={props.settings} activeSession={props.activeSession} onRefresh={props.onRefreshEcosystem} /> : null}
        {props.active === "notebook" ? <NotebookPanel notebooks={props.notebooks} lspSessions={props.lspSessions} activeSession={props.activeSession} onRefresh={props.onRefreshEcosystem} /> : null}
      </Suspense>
    </aside>
  );
}

function modelLabel(settings: SettingsValues) {
  const provider = settings.defaultProvider?.trim();
  const model = settings.defaultModel?.trim();
  if (!provider || !model) return "provider unavailable";
  return `${provider} · ${model}`;
}

function formatTimestamp(value: number) {
  const milliseconds = value < 10_000_000_000 ? value * 1000 : value;
  return new Date(milliseconds).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}
