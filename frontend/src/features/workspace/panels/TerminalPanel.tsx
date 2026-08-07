import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";
import { AlertTriangle, Play, Square } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import type { Session } from "../../../lib/api";
import { client } from "../../../lib/client";

export function TerminalPanel({ session, onRefresh }: { session: Session | null; onRefresh: () => Promise<void> }) {
  const hostRef = useRef<HTMLDivElement | null>(null);
  const terminalRef = useRef<Terminal | null>(null);
  const fitRef = useRef<FitAddon | null>(null);
  const socketRef = useRef<WebSocket | null>(null);
  const ptyIDRef = useRef("");
  const [status, setStatus] = useState<"disconnected" | "connecting" | "connected" | "approval" | "error">("disconnected");
  const [error, setError] = useState("");

  useEffect(() => {
    if (!hostRef.current) return;
    const terminal = new Terminal({ cursorBlink: true, fontSize: 12, convertEol: true, theme: { background: "#111715" } });
    const fit = new FitAddon();
    terminal.loadAddon(fit);
    terminal.open(hostRef.current);
    fit.fit();
    terminal.writeln("Superscience terminal is disconnected.");
    terminal.onData((data) => {
      const socket = socketRef.current;
      if (socket?.readyState === WebSocket.OPEN) socket.send(JSON.stringify({ type: "input", data }));
    });
    terminal.onResize(({ cols, rows }) => {
      const socket = socketRef.current;
      if (socket?.readyState === WebSocket.OPEN) socket.send(JSON.stringify({ type: "resize", cols, rows }));
    });
    const observer = new ResizeObserver(() => fit.fit());
    observer.observe(hostRef.current);
    terminalRef.current = terminal;
    fitRef.current = fit;
    return () => {
      observer.disconnect();
      socketRef.current?.close();
      terminal.dispose();
      terminalRef.current = null;
      fitRef.current = null;
    };
  }, []);

  async function connect() {
    if (status === "connecting" || status === "connected") return;
    setStatus("connecting");
    setError("");
    try {
      const created = await client.createPTY("shell", "", session?.id);
      if ("permissionId" in created) {
        setStatus("approval");
        terminalRef.current?.writeln(`\r\nApproval required: ${created.permissionId}`);
        await onRefresh();
        return;
      }
      ptyIDRef.current = created.id;
      const socket = new WebSocket(client.ptyWebSocketURL(created.id));
      socketRef.current = socket;
      socket.onopen = () => {
        setStatus("connected");
        terminalRef.current?.clear();
        const fit = fitRef.current;
        if (fit) {
          fit.fit();
          socket.send(JSON.stringify({ type: "resize", cols: terminalRef.current?.cols ?? 80, rows: terminalRef.current?.rows ?? 24 }));
        }
      };
      socket.onmessage = (event) => terminalRef.current?.write(String(event.data));
      socket.onerror = () => {
        setStatus("error");
        setError("The terminal WebSocket failed.");
      };
      socket.onclose = () => {
        socketRef.current = null;
        setStatus((current) => current === "error" ? current : "disconnected");
      };
    } catch (connectError) {
      setStatus("error");
      setError(connectError instanceof Error ? connectError.message : "The terminal could not start.");
    }
  }

  async function stop() {
    socketRef.current?.close();
    if (ptyIDRef.current) await client.stopPTY(ptyIDRef.current);
    ptyIDRef.current = "";
    setStatus("disconnected");
  }

  return (
    <div className="pane-body terminal-pane xterm-panel">
      <div className="terminal-toolbar">
        <span className={`status ${status === "connected" ? "good" : status === "error" ? "warn" : ""}`}><span className="dot" />{status}</span>
        <button className="secondary" onClick={() => void connect()} disabled={status === "connecting" || status === "connected"}>
          <Play size={13} /> Start
        </button>
        <button className="icon-button small" aria-label="Stop terminal" onClick={() => void stop()} disabled={status === "disconnected"}>
          <Square size={13} />
        </button>
      </div>
      {error ? <div className="panel-error" role="alert"><AlertTriangle size={14} /> {error}</div> : null}
      <div ref={hostRef} className="xterm-host" />
    </div>
  );
}
