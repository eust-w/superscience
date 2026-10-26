import { loader } from "@monaco-editor/react";
import * as monaco from "monaco-editor";
import EditorWorker from "monaco-editor/esm/vs/editor/editor.worker?worker";
import JSONWorker from "monaco-editor/esm/vs/language/json/json.worker?worker";
import CSSWorker from "monaco-editor/esm/vs/language/css/css.worker?worker";
import HTMLWorker from "monaco-editor/esm/vs/language/html/html.worker?worker";
import TypeScriptWorker from "monaco-editor/esm/vs/language/typescript/ts.worker?worker";

// Bundle the editor and workers with the app so local files never require a CDN.
self.MonacoEnvironment = {
  getWorker(_moduleId, label) {
    if (label === "json") return new JSONWorker();
    if (["css", "scss", "less"].includes(label)) return new CSSWorker();
    if (["html", "handlebars", "razor"].includes(label)) return new HTMLWorker();
    if (["typescript", "javascript"].includes(label)) return new TypeScriptWorker();
    return new EditorWorker();
  }
};

loader.config({ monaco });

// Monaco 0.55's WordHighlighter leaves its delayed promise unhandled when a
// focused model is disposed. Disable that optional decoration, not error reporting.
export const localEditorOptions: monaco.editor.IStandaloneEditorConstructionOptions = {
  occurrencesHighlight: "off"
};

export { default } from "@monaco-editor/react";
