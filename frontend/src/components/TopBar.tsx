import { FolderOpen, Microscope, Moon, Settings, Sun } from "lucide-react";
import type { Project } from "../lib/api";

export function TopBar(props: {
  health: string;
  dark: boolean;
  activeProject: Project | null;
  onHome: () => void;
  onSettings: () => void;
  onTheme: () => void;
}) {
  return (
    <header className="topbar">
      <button className="brand" onClick={props.onHome}>
        <span className="brand-mark"><Microscope size={16} /></span>
        <span>Superscience</span>
      </button>
      <div className="project-chip">
        <FolderOpen size={14} />
        <span>{props.activeProject?.name ?? "No project open"}</span>
      </div>
      <span className="spacer" />
      <div className={`status ${props.health === "ready" ? "good" : "warn"}`}>
        <span className="dot" />
        <span>Local server {props.health}</span>
      </div>
      <button className="icon-button" onClick={props.onTheme} aria-label="Toggle theme">
        {props.dark ? <Sun size={16} /> : <Moon size={16} />}
      </button>
      <button className="icon-button" onClick={props.onSettings} aria-label="Settings">
        <Settings size={16} />
      </button>
    </header>
  );
}
