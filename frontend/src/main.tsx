import React from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App";
import { AppProviders } from "./app/AppProviders";
import { configureClient } from "./lib/client";
import "./styles.css";

async function bootstrap() {
  const root = createRoot(document.getElementById("root")!);
  try {
    await configureClient();
    root.render(<React.StrictMode><AppProviders><App /></AppProviders></React.StrictMode>);
  } catch (error) {
    const message = error instanceof Error ? error.message : "Desktop initialization failed.";
    root.render(<main className="startup-failure" role="alert"><h1>Superscience could not start</h1><p>{message}</p></main>);
  }
}

void bootstrap();
