import Editor, { localEditorOptions } from "../../../lib/localEditor";
import { AlertTriangle, FileText, FolderOpen, Save } from "lucide-react";
import { useEffect, useState } from "react";
import { client } from "../../../lib/client";
import type { FileNode, Session } from "../../../lib/api";

export function FilesPanel({ session, onRefresh }: { session: Session | null; onRefresh: () => Promise<void> }) {
  const [nodes, setNodes] = useState<FileNode[]>([]);
  const [selected, setSelected] = useState<FileNode | null>(null);
  const [content, setContent] = useState("");
  const [status, setStatus] = useState<"loading" | "ready" | "saving" | "saved" | "error">("loading");
  const [error, setError] = useState("");

  useEffect(() => {
    let active = true;
    client.listFiles("").then((result) => {
      if (!active) return;
      setNodes(result);
      setStatus("ready");
    }).catch((loadError: unknown) => {
      if (!active) return;
      setStatus("error");
      setError(loadError instanceof Error ? loadError.message : "File tree is unavailable.");
    });
    return () => { active = false; };
  }, []);

  async function open(node: FileNode) {
    setSelected(node);
    setError("");
    if (node.kind !== "file") return;
    setStatus("loading");
    try {
      const result = await client.readFile(node.path);
      setContent(result.content);
      setStatus("ready");
    } catch (openError) {
      setStatus("error");
      setError(openError instanceof Error ? openError.message : "The file could not be opened.");
    }
  }

  async function save() {
    if (!selected || selected.kind !== "file") return;
    setStatus("saving");
    setError("");
    try {
      const result = await client.writeFile(selected.path, content, session?.id);
      if ("permissionId" in result) {
        setStatus("ready");
        setError(`Write approval required: ${result.permissionId}`);
        await onRefresh();
      } else {
        setStatus("saved");
      }
    } catch (saveError) {
      setStatus("error");
      setError(saveError instanceof Error ? saveError.message : "The file could not be saved.");
    }
  }

  return (
    <div className="pane-body split-vertical file-panel">
      <div className="file-list" aria-busy={status === "loading"}>
        {nodes.map((node) => (
          <button key={node.path} className={selected?.path === node.path ? "active" : ""} onClick={() => void open(node)}>
            {node.kind === "directory" ? <FolderOpen size={14} /> : <FileText size={14} />} {node.name}
          </button>
        ))}
        {status === "loading" && nodes.length === 0 ? <span className="panel-state">Loading file tree...</span> : null}
      </div>
      {error ? <div className="panel-error" role="alert"><AlertTriangle size={14} /> {error}</div> : null}
      <div className="editor-frame">
        {selected?.kind === "file" ? (
          <Editor
            path={selected.path}
            value={content}
            onChange={(value) => setContent(value ?? "")}
            theme={document.documentElement.dataset.theme === "dark" ? "vs-dark" : "light"}
            options={{ ...localEditorOptions, minimap: { enabled: false }, fontSize: 12, lineNumbersMinChars: 3, scrollBeyondLastLine: false, automaticLayout: true }}
          />
        ) : (
          <div className="empty">Select a file to inspect it.</div>
        )}
      </div>
      <div className="panel-action-row">
        <span className="muted">{selected?.path ?? "No file selected"}</span>
        <button className="secondary" disabled={!selected || selected.kind !== "file" || status === "saving"} onClick={() => void save()}>
          <Save size={13} /> {status === "saving" ? "Saving" : status === "saved" ? "Saved" : "Save"}
        </button>
      </div>
    </div>
  );
}
