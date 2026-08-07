import Editor from "@monaco-editor/react";
import { AlertTriangle, BookOpen, CircleStop, Code2, Download, Play, RefreshCw, RotateCcw } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import type {
  LSPDiagnostic,
  LSPSession,
  NotebookDocument,
  NotebookKernelStatus,
  NotebookOutput,
  NotebookRunResult,
  Session
} from "../../../lib/api";
import { client } from "../../../lib/client";

type Notice = { kind: "status" | "error"; message: string } | null;

export function NotebookPanel(props: {
  notebooks: NotebookDocument[];
  lspSessions: LSPSession[];
  activeSession: Session | null;
  onRefresh: () => Promise<void>;
}) {
  const [mode, setMode] = useState<"notebook" | "lsp">("notebook");
  const [language, setLanguage] = useState("python");
  const [source, setSource] = useState("");
  const [path, setPath] = useState("analysis/scratch.ipynb");
  const [activeNotebook, setActiveNotebook] = useState<NotebookDocument | null>(null);
  const [activeCellId, setActiveCellId] = useState("");
  const [result, setResult] = useState<NotebookRunResult | null>(null);
  const [kernel, setKernel] = useState<NotebookKernelStatus | null>(null);
  const [running, setRunning] = useState(false);
  const [notice, setNotice] = useState<Notice>(null);
  const [lspLanguage, setLspLanguage] = useState("go");
  const [lspPath, setLspPath] = useState("analysis/main.go");
  const [lspSource, setLspSource] = useState("");
  const [lspResult, setLspResult] = useState<LSPSession | null>(null);

  useEffect(() => {
    if (activeNotebook || !props.notebooks[0]) return;
    setActiveNotebook(props.notebooks[0]);
    setActiveCellId(props.notebooks[0].cells[0]?.id ?? "");
  }, [activeNotebook, props.notebooks]);

  const selectedCell = useMemo(
    () => activeNotebook?.cells.find((cell) => cell.id === activeCellId) ?? null,
    [activeCellId, activeNotebook]
  );

  async function createAndRun() {
    if (!source.trim() || running) return;
    setRunning(true);
    setNotice(null);
    setResult(null);
    try {
      const notebook = await client.createNotebook({
        title: notebookTitle(path),
        path: path.trim(),
        cells: [{ kind: "code", language, source }]
      });
      const cell = notebook.cells[0];
      if (!cell) throw new Error("The notebook was created without an executable cell.");
      setActiveNotebook(notebook);
      setActiveCellId(cell.id);
      await executeCell(notebook.id, cell.id);
      await props.onRefresh();
    } catch (error) {
      setNotice({ kind: "error", message: error instanceof Error ? error.message : "Notebook execution failed." });
    } finally {
      setRunning(false);
    }
  }

  async function runSelected() {
    if (!activeNotebook || !selectedCell || running) return;
    setRunning(true);
    setNotice(null);
    setResult(null);
    try {
      await executeCell(activeNotebook.id, selectedCell.id);
      await props.onRefresh();
    } catch (error) {
      setNotice({ kind: "error", message: error instanceof Error ? error.message : "Notebook execution failed." });
    } finally {
      setRunning(false);
    }
  }

  async function executeCell(notebookId: string, cellId: string) {
    const run = await client.runNotebookCell(notebookId, cellId, props.activeSession?.id);
    if ("permissionId" in run) {
      setNotice({ kind: "status", message: `Notebook approval required: ${run.permissionId}` });
      return;
    }
    setResult(run);
    setKernel(await client.getNotebookKernelStatus(notebookId));
    setNotice({ kind: run.status === "ok" ? "status" : "error", message: `${run.kernel} finished with status ${run.status} in ${run.durationMs} ms.` });
  }

  async function cancel() {
    if (!activeNotebook || !activeCellId) return;
    try {
      await client.cancelNotebookCell(activeNotebook.id, activeCellId);
      setRunning(false);
      setKernel(await client.getNotebookKernelStatus(activeNotebook.id));
      setNotice({ kind: "status", message: "Kernel interrupted and the cell was marked cancelled." });
    } catch (error) {
      setNotice({ kind: "error", message: error instanceof Error ? error.message : "Kernel interrupt failed." });
    }
  }

  async function restart() {
    if (!activeNotebook) return;
    try {
      const status = await client.restartNotebookKernel(activeNotebook.id);
      setKernel(status);
      setNotice({ kind: "status", message: `${status.kernel} kernel restarted; its state was cleared.` });
    } catch (error) {
      setNotice({ kind: "error", message: error instanceof Error ? error.message : "Kernel restart failed." });
    }
  }

  async function exportNotebook() {
    if (!activeNotebook) return;
    try {
      const exported = await client.exportNotebook(activeNotebook.id);
      const blob = new Blob([JSON.stringify(exported, null, 2)], { type: "application/x-ipynb+json" });
      const url = URL.createObjectURL(blob);
      const anchor = document.createElement("a");
      anchor.href = url;
      anchor.download = notebookFileName(activeNotebook.path || `${activeNotebook.title}.ipynb`);
      anchor.click();
      URL.revokeObjectURL(url);
      setNotice({ kind: "status", message: `Exported ${exported.cells.length} cells without replacing stored outputs.` });
    } catch (error) {
      setNotice({ kind: "error", message: error instanceof Error ? error.message : "Notebook export failed." });
    }
  }

  async function analyzeLSP() {
    if (!lspPath.trim() || !lspSource.trim()) return;
    setNotice(null);
    try {
      const session = await client.createLSPSession({ language: lspLanguage, path: lspPath.trim(), text: lspSource });
      setLspResult(session);
      setNotice({ kind: "status", message: `${session.language} language server returned ${session.diagnostics.length} diagnostics.` });
      await props.onRefresh();
    } catch (error) {
      setLspResult(null);
      setNotice({ kind: "error", message: error instanceof Error ? error.message : "The language server is unavailable." });
    }
  }

  return (
    <div className="pane-body notebook-panel">
      <div className="notebook-mode-tabs" role="tablist">
        <button className={mode === "notebook" ? "active" : ""} onClick={() => setMode("notebook")}><BookOpen size={13} /> Notebook</button>
        <button className={mode === "lsp" ? "active" : ""} onClick={() => setMode("lsp")}><Code2 size={13} /> LSP</button>
      </div>
      {notice ? <div className={notice.kind === "error" ? "panel-error" : "panel-status"} role={notice.kind === "error" ? "alert" : "status"}>{notice.kind === "error" ? <AlertTriangle size={14} /> : null}{notice.message}</div> : null}
      {mode === "notebook" ? (
        <>
          <div className="notebook-toolbar">
            <input value={path} onChange={(event) => setPath(event.target.value)} aria-label="Notebook path" />
            <select value={language} onChange={(event) => setLanguage(event.target.value)} aria-label="Kernel language">
              <option value="python">Python</option><option value="r">R</option><option value="julia">Julia</option>
            </select>
            <button className="primary" onClick={() => void createAndRun()} disabled={running || !source.trim()}><Play size={13} /> New and run</button>
          </div>
          <div className="notebook-editor">
            <Editor
              language={monacoLanguage(language)}
              value={source}
              onChange={(value) => setSource(value ?? "")}
              theme={document.documentElement.dataset.theme === "dark" ? "vs-dark" : "light"}
              options={{ minimap: { enabled: false }, fontSize: 12, lineNumbersMinChars: 3, scrollBeyondLastLine: false, automaticLayout: true }}
            />
          </div>
          <div className="panel-action-row">
            <span className="muted">{kernel ? `${kernel.kernel} · ${kernel.status}` : "Kernel has not started"}</span>
            <button className="secondary" onClick={() => void runSelected()} disabled={!selectedCell || running}><Play size={13} /> Run stored cell</button>
            <button className="icon-button small" aria-label="Interrupt kernel" onClick={() => void cancel()} disabled={!activeCellId}><CircleStop size={13} /></button>
            <button className="icon-button small" aria-label="Restart kernel" onClick={() => void restart()} disabled={!activeNotebook}><RotateCcw size={13} /></button>
            <button className="icon-button small" aria-label="Export notebook" onClick={() => void exportNotebook()} disabled={!activeNotebook}><Download size={13} /></button>
          </div>
          <div className="notebook-output-list">
            {result?.outputs.map((output, index) => <NotebookOutputView key={`${output.output_type}-${index}`} output={output} />)}
            {!result?.outputs.length && result?.stdout ? <pre>{result.stdout}</pre> : null}
            {!result?.outputs.length && result?.stderr ? <pre className="output-error">{result.stderr}</pre> : null}
            {!result ? <div className="empty">Rich kernel outputs appear here after a real Jupyter execution.</div> : null}
          </div>
          <NotebookLibrary notebooks={props.notebooks} active={activeNotebook} onSelect={(notebook) => { setActiveNotebook(notebook); setActiveCellId(notebook.cells[0]?.id ?? ""); }} />
        </>
      ) : (
        <>
          <div className="notebook-toolbar">
            <input value={lspPath} onChange={(event) => setLspPath(event.target.value)} aria-label="Document path" />
            <select value={lspLanguage} onChange={(event) => setLspLanguage(event.target.value)} aria-label="Language server">
              <option value="go">Go</option><option value="python">Python</option><option value="typescript">TypeScript</option>
            </select>
            <button className="primary" onClick={() => void analyzeLSP()} disabled={!lspPath.trim() || !lspSource.trim()}><RefreshCw size={13} /> Analyze</button>
          </div>
          <div className="notebook-editor">
            <Editor
              language={monacoLanguage(lspLanguage)}
              value={lspSource}
              onChange={(value) => setLspSource(value ?? "")}
              theme={document.documentElement.dataset.theme === "dark" ? "vs-dark" : "light"}
              options={{ minimap: { enabled: false }, fontSize: 12, lineNumbersMinChars: 3, scrollBeyondLastLine: false, automaticLayout: true }}
            />
          </div>
          <Diagnostics diagnostics={lspResult?.diagnostics ?? []} />
          {lspResult?.hover ? <pre className="notebook-output">{lspResult.hover}</pre> : null}
          <div className="lsp-session-list">
            {props.lspSessions.map((session) => <span key={session.id}>{session.path}<small>{session.language} · {session.diagnostics.length} diagnostics</small></span>)}
          </div>
        </>
      )}
    </div>
  );
}

function NotebookOutputView({ output }: { output: NotebookOutput }) {
  if (output.output_type === "error") return <pre className="output-error">{output.traceback?.join("\n") || `${output.ename || "Error"}: ${output.evalue || "Kernel execution failed"}`}</pre>;
  if (output.text) return <pre>{output.text}</pre>;
  const data = output.data ?? {};
  const png = typeof data["image/png"] === "string" ? data["image/png"] : "";
  if (png) return <img className="notebook-image" src={`data:image/png;base64,${png}`} alt="Notebook output" />;
  const html = typeof data["text/html"] === "string" ? data["text/html"] : "";
  if (html) return <iframe className="notebook-html" title="Notebook HTML output" sandbox="" srcDoc={html} />;
  const text = data["text/plain"];
  if (typeof text === "string") return <pre>{text}</pre>;
  return <pre>{JSON.stringify(data, null, 2)}</pre>;
}

function Diagnostics({ diagnostics }: { diagnostics: LSPDiagnostic[] }) {
  return (
    <div className="diagnostics-list">
      {diagnostics.map((diagnostic, index) => <div key={`${diagnostic.line}-${diagnostic.column}-${index}`}><strong>{diagnostic.severity}</strong><span>{diagnostic.line}:{diagnostic.column}</span><p>{diagnostic.message}</p></div>)}
      {!diagnostics.length ? <div className="empty">No diagnostics returned.</div> : null}
    </div>
  );
}

function NotebookLibrary(props: { notebooks: NotebookDocument[]; active: NotebookDocument | null; onSelect: (notebook: NotebookDocument) => void }) {
  return (
    <div className="notebook-library">
      {props.notebooks.map((notebook) => <button key={notebook.id} className={props.active?.id === notebook.id ? "active" : ""} onClick={() => props.onSelect(notebook)}><span>{notebook.title}</span><small>{notebook.cells.length} cells · {notebook.path}</small></button>)}
      {!props.notebooks.length ? <div className="empty">No persisted notebooks.</div> : null}
    </div>
  );
}

function notebookTitle(path: string) {
  return notebookFileName(path).replace(/\.ipynb$/i, "") || "Research notebook";
}

function notebookFileName(path: string) {
  return path.split(/[\\/]/).filter(Boolean).at(-1) || "notebook.ipynb";
}

function monacoLanguage(language: string) {
  if (language === "r") return "r";
  if (language === "julia") return "julia";
  if (language === "typescript") return "typescript";
  if (language === "go") return "go";
  return "python";
}
