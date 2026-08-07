import { Activity, BrainCircuit, Database, FolderOpen, FolderSearch, KeyRound, Search, Star } from "lucide-react";
import { useMemo, useState, type ReactNode } from "react";
import type { Project, ProviderProfile, ScienceDatabase } from "../../lib/api";
import { isDesktop, pickProjectFolder } from "../../lib/client";

export function ProjectLauncher(props: {
  projects: Project[];
  databases: ScienceDatabase[];
  providers: ProviderProfile[];
  health: string;
  providerStatus: string;
  onOpen: (path: string) => Promise<void>;
}) {
  const [path, setPath] = useState(".");
  const [query, setQuery] = useState("");
  const [opening, setOpening] = useState(false);
  const [error, setError] = useState("");
  const filtered = useMemo(
    () => props.projects.filter((project) => `${project.name} ${project.path}`.toLowerCase().includes(query.toLowerCase())),
    [props.projects, query]
  );
  const enabledProviders = props.providers.filter((provider) => provider.enabled).length;

  async function open(projectPath: string) {
    if (!projectPath.trim() || opening) return;
    setOpening(true);
    setError("");
    try {
      await props.onOpen(projectPath.trim());
    } catch (openError) {
      setError(openError instanceof Error ? openError.message : "The project could not be opened.");
    } finally {
      setOpening(false);
    }
  }

  async function pickAndOpen() {
    setError("");
    try {
      const selected = await pickProjectFolder();
      if (!selected) return;
      setPath(selected);
      await open(selected);
    } catch (pickError) {
      setError(pickError instanceof Error ? pickError.message : "The native folder picker failed.");
    }
  }

  return (
    <main className="launcher">
      <section className="launcher-header">
        <div>
          <h1>Projects</h1>
          <p>Recent local research workspaces and verified system readiness.</p>
        </div>
        <div className="launcher-actions">
          <div className="path-input">
            <FolderOpen size={15} />
            <input value={path} onChange={(event) => setPath(event.target.value)} aria-label="Project path" />
          </div>
          {isDesktop() ? <button className="icon-button" title="Choose project folder" aria-label="Choose project folder" onClick={() => void pickAndOpen()} disabled={opening}><FolderSearch size={15} /></button> : null}
          <button className="primary" onClick={() => void open(path)} disabled={opening || !path.trim()}>
            <FolderOpen size={15} /> {opening ? "Opening" : "Open Folder"}
          </button>
        </div>
      </section>
      {error ? <div className="inline-error" role="alert">{error}</div> : null}
      <section className="launcher-grid">
        <div className="project-table panel">
          <div className="panel-toolbar">
            <div className="search-field">
              <Search size={14} />
              <input placeholder="Search projects" value={query} onChange={(event) => setQuery(event.target.value)} />
            </div>
            <span className="muted">{filtered.length} workspaces</span>
          </div>
          <div className="table">
            <div className="table-row table-head">
              <span>Name</span><span>Path</span><span>Last activity</span><span>Storage</span>
            </div>
            {filtered.map((project) => (
              <button className="table-row project-row" key={project.id} onClick={() => void open(project.path)} disabled={opening}>
                <span className="name-cell"><Star size={13} /> {project.name}</span>
                <span className="truncate">{project.path}</span>
                <span>{new Date(project.updatedAt).toLocaleString()}</span>
                <span className="badge">local</span>
              </button>
            ))}
            {filtered.length === 0 ? <div className="empty">No matching local projects.</div> : null}
          </div>
        </div>
        <aside className="panel launcher-side">
          <h2>System readiness</h2>
          <Readiness icon={<Activity size={15} />} label="Local server" value={props.health} good={props.health === "ready"} />
          <Readiness icon={<KeyRound size={15} />} label="Cloud provider" value={props.providerStatus} good={props.providerStatus === "ready"} />
          <Readiness icon={<BrainCircuit size={15} />} label="Registered cloud adapters" value={String(enabledProviders)} good={enabledProviders > 0} />
          <Readiness icon={<Database size={15} />} label="Connector catalog" value={String(props.databases.length)} good={props.databases.length > 0} />
        </aside>
      </section>
    </main>
  );
}

function Readiness({ icon, label, value, good }: { icon: ReactNode; label: string; value: string; good: boolean }) {
  return (
    <div className="readiness">
      {icon}
      <span>{label}</span>
      <strong className={good ? "ok-text" : "warn-text"}>{value}</strong>
    </div>
  );
}
