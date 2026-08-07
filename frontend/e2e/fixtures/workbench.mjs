import { spawn } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import { createServer } from "node:http";
import { tmpdir } from "node:os";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const fixtureDirectory = dirname(fileURLToPath(import.meta.url));
const frontendDirectory = resolve(fixtureDirectory, "../..");
const repositoryRoot = resolve(frontendDirectory, "..");
const dataDirectory = mkdtempSync(resolve(tmpdir(), "superscience-e2e-"));
const providerSecret = "e2e-protocol-secret";
const providerPort = 4397;
const applicationPort = 4396;

const provider = createServer(async (request, response) => {
  if (request.method !== "POST" || request.url !== "/v1/chat/completions") {
    response.writeHead(404).end();
    return;
  }
  if (request.headers.authorization !== `Bearer ${providerSecret}`) {
    response.writeHead(401, { "Content-Type": "application/json" });
    response.end(JSON.stringify({ error: { message: "invalid protocol fixture credential" } }));
    return;
  }

  let rawBody = "";
  for await (const chunk of request) rawBody += chunk;
  let payload;
  try {
    payload = JSON.parse(rawBody);
  } catch {
    response.writeHead(400, { "Content-Type": "application/json" });
    response.end(JSON.stringify({ error: { message: "invalid JSON request" } }));
    return;
  }
  if (payload.model !== "e2e-research-model" || payload.stream !== true || !Array.isArray(payload.messages)) {
    response.writeHead(400, { "Content-Type": "application/json" });
    response.end(JSON.stringify({ error: { message: "invalid OpenAI-compatible request contract" } }));
    return;
  }

  const lastUserMessage = [...payload.messages].reverse().find((message) => message.role === "user");
  const content = lastUserMessage?.content === "Reply with OK."
    ? "OK"
    : "Protocol-backed research response.";
  const splitAt = Math.max(1, Math.floor(content.length / 2));
  const chunks = [content.slice(0, splitAt), content.slice(splitAt)].filter(Boolean);

  response.writeHead(200, {
    "Content-Type": "text/event-stream",
    "Cache-Control": "no-cache",
    Connection: "keep-alive"
  });
  for (const [index, chunk] of chunks.entries()) {
    response.write(`data: ${JSON.stringify({
      id: "e2e-response",
      model: "e2e-research-model",
      choices: [{ delta: { content: chunk }, finish_reason: "" }]
    })}\n\n`);
    if (index === 0) await new Promise((resolveDelay) => setTimeout(resolveDelay, 15));
  }
  response.write(`data: ${JSON.stringify({
    id: "e2e-response",
    model: "e2e-research-model",
    choices: [{ delta: {}, finish_reason: "stop" }],
    usage: { prompt_tokens: 7, completion_tokens: 4, total_tokens: 11 }
  })}\n\n`);
  response.end("data: [DONE]\n\n");
});

await new Promise((resolveListen, rejectListen) => {
  provider.once("error", rejectListen);
  provider.listen(providerPort, "127.0.0.1", resolveListen);
});

const workbench = spawn(
  "go",
  ["run", "../cmd/superscience", "serve", "--port", String(applicationPort), "--project", repositoryRoot],
  {
    cwd: frontendDirectory,
    detached: process.platform !== "win32",
    env: {
      ...process.env,
      SUPERSCIENCE_DATA_DIR: dataDirectory,
      SUPERSCIENCE_STATIC_DIR: resolve(frontendDirectory, "dist"),
      SUPERSCIENCE_API_KEY: providerSecret
    },
    stdio: ["ignore", "pipe", "pipe"]
  }
);

workbench.stdout.pipe(process.stdout);
workbench.stderr.pipe(process.stderr);

let stopping = false;
async function stop(exitCode = 0) {
  if (stopping) return;
  stopping = true;
  if (workbench.pid && !workbench.killed) {
    try {
      if (process.platform === "win32") workbench.kill("SIGTERM");
      else process.kill(-workbench.pid, "SIGTERM");
    } catch {
      // The workbench may already have exited.
    }
  }
  await new Promise((resolveClose) => provider.close(resolveClose));
  rmSync(dataDirectory, { recursive: true, force: true });
  process.exit(exitCode);
}

process.on("SIGTERM", () => void stop(0));
process.on("SIGINT", () => void stop(0));
workbench.on("exit", (code, signal) => {
  if (!stopping) {
    const exitCode = typeof code === "number" ? code : signal ? 1 : 0;
    void stop(exitCode);
  }
});
