# Superscience

Superscience is a local AI workbench for scientific research. It is inspired by OpenScience's product boundary, but this repository is a clean Go/Wails/React implementation.

The app runs a local Go server, a React workspace UI, and a Wails desktop shell. It supports local projects, sessions, streaming assistant messages, file browsing/editing, real PTY terminals, scientific search connectors, permission approvals, persistent settings, provider catalogs, skills, MCP server configuration, stateful Python notebook execution, command-backed LSP diagnostics, artifacts, provenance, and local/self-hosted share bundles.

## Stack

- Desktop shell: Wails
- Backend: Go, standard `net/http`, SQLite
- Frontend: React, Vite, TypeScript
- Storage: SQLite under the Superscience config directory
- Cloud storage: PostgreSQL metadata plus S3-compatible artifact objects; SQLite/inline content is development-only
- Provider ecosystem: OpenAI, OpenAI-compatible, Anthropic, Gemini, OpenRouter, Azure OpenAI, AWS Bedrock, Mistral, Groq, xAI, DeepSeek, DashScope
- Science search: 50+ normalized connectors across literature, biology, chemistry, genomics, omics, pathways, structures, datasets, physics, astronomy, materials, robotics, and embodied AI. Robot/embodied sources include Hugging Face robotics datasets, GitHub robotics repositories, ROS packages, and OpenReview embodied AI submissions.
- Scientific UI: data-driven renderer contracts for literature, chemistry, structures, genomics, omics, tables, notebook outputs, artifacts, provenance, and share replay.

## Ecosystem APIs

The local API now includes the ecosystem surface:

- Providers and model catalogs: `/api/providers`, `/api/providers/:id/models`
- Agents and permission-aware runs: `/api/agents`, `/api/sessions/:id/run`
- Tools, skills, and MCP: `/api/tools`, `/api/tools/execute`, `/api/skills`, `/api/skills/load`, `/api/mcp/servers`
- Notebook and LSP: `/api/notebooks`, `/api/notebooks/:id/export`, `/api/lsp/sessions`
- Artifacts, provenance, and sharing: `/api/artifacts`, `/api/provenance/:id`, `/api/share/publish`, `/api/share/:id`, `/api/share/:id/replay`

Risky actions such as shell/write/network/MCP/notebook/share execution are routed through the backend permission model. The tool executor covers read/write/edit/multiedit/apply_patch/ls/glob/grep/codesearch/bash/todo/plan/artifact/science/notebook/LSP/MCP/webfetch/websearch/share publishing. MCP uses the official Go SDK for persistent stdio, legacy SSE, and Streamable HTTP sessions, including tools, resources, prompts, permissions, and audit behavior.

## Self-Hosted Cloud

`cloud/` exposes a clean-room Superscience Cloud service. It is intentionally self-hosted and does not depend on Synthetic Sciences services.

- Production auth: `POST /api/auth/oidc` verifies a signed ID token through live OIDC discovery/JWKS, validates issuer, audience, expiry, subject, and optional verified email domains, then returns a random application bearer token. Only its SHA-256 hash is stored.
- Bootstrap auth: `POST /api/auth/local` is available only when an explicit high-entropy secret is configured.
- Organization/project views: `GET /api/orgs`, `GET /api/projects`.
- Share hosting and replay: `GET /api/shares`, `GET /api/shares/:id`, `GET /api/shares/:id/replay`.
- Revocable public links: `POST /api/shares/:id/publish`, `POST /api/shares/:id/revoke`, then `GET /share/:token/replay`.
- Bundle exchange: `POST /api/bundles/import`, `GET /api/bundles/export/:id`.
- Artifact/audit access: `GET /api/artifacts/:id`, `GET /api/audit`.

Production mode requires PostgreSQL and S3-compatible object storage before the listener starts. Artifact content is stored only in S3; PostgreSQL keeps metadata, object size, ETag, and SHA-256. Replay and export re-read and verify the object before returning content.

## Development

Prerequisites: Go 1.25.12 or newer, Node.js 24+, pnpm 11+, and Wails 2.12+. The Go patch level is intentional: earlier Go 1.24 toolchains contain standard-library vulnerabilities reachable through the networked runtime.

Install dependencies:

```bash
pnpm install
go mod download
```

Run the backend and browser UI:

```bash
pnpm --dir frontend build
go run ./cmd/superscience web
```

For frontend-only iteration with API proxy:

```bash
go run ./cmd/superscience serve
pnpm --dir frontend dev
```

Run the desktop shell:

```bash
wails dev
wails build
go run ./cmd/superscience desktop --app build/bin/superscience.app
```

Run the self-hosted Cloud service in production mode:

```bash
export SUPERSCIENCE_CLOUD_DATABASE_URL='postgres://user:password@db.example.com/superscience?sslmode=require'
export SUPERSCIENCE_CLOUD_S3_ENDPOINT='https://objects.example.com'
export SUPERSCIENCE_CLOUD_S3_REGION='us-east-1'
export SUPERSCIENCE_CLOUD_S3_BUCKET='superscience'
export SUPERSCIENCE_CLOUD_S3_ACCESS_KEY='...'
export SUPERSCIENCE_CLOUD_S3_SECRET_KEY='...'
export SUPERSCIENCE_CLOUD_OIDC_ISSUER='https://identity.example.com'
export SUPERSCIENCE_CLOUD_OIDC_CLIENT_ID='superscience'
export SUPERSCIENCE_CLOUD_OIDC_ALLOWED_EMAIL_DOMAINS='lab.example,research.example'
go run ./cmd/superscience cloud
```

For an explicit single-machine development instance, SQLite and inline artifact content remain available:

```bash
export SUPERSCIENCE_CLOUD_LOCAL_AUTH_SECRET='replace-with-at-least-24-random-characters'
go run ./cmd/superscience cloud --dev-sqlite
```

Optional S3 settings include `SUPERSCIENCE_CLOUD_S3_PATH_STYLE=true`, `SUPERSCIENCE_CLOUD_S3_CREATE_BUCKET=true`, and `SUPERSCIENCE_CLOUD_S3_SESSION_TOKEN`. Plain HTTP OIDC/S3 endpoints are rejected except on loopback addresses.

Build checks:

```bash
pnpm --dir frontend exec playwright install chromium
make verify
wails build
```

`make verify` runs Go race tests, vet, and `govulncheck`; frontend typechecking/unit/build; Playwright against a real Go/SQLite workbench process; the production dependency audit; and the production-placeholder scan. The Playwright OpenAI-compatible protocol fixture exists only under `frontend/e2e/`; it is not included in frontend or Go production builds.

## Model Providers

Superscience does not ship a production mock provider. Agent execution remains unavailable until a real cloud provider credential and model pass health verification.

Configure and test a provider in the first-run screen or Settings. The provider endpoint, model, and non-secret metadata are persisted in SQLite; credentials are stored in the OS keychain on macOS when available. The UI does not expose stored secret values.

Credentials can also be managed non-interactively without placing the secret on the command line:

```bash
export SUPERSCIENCE_API_KEY='...'
go run ./cmd/superscience keys add openai
go run ./cmd/superscience keys list
```

The CLI uses the same SQLite, provider adapters, permission model, and share schema as the UI:

```bash
go run ./cmd/superscience run --project . --provider openai --model gpt-4.1 "Review the evidence in this project"
go run ./cmd/superscience export SHARE_ID --output research-bundle.json
go run ./cmd/superscience import research-bundle.json
```

`run` persists the project, session, model usage, agent steps, tool calls, and final message. `import` restores artifacts, the share record, provenance edges, and an audit event in one SQLite transaction.

## Custom Agents and Skill Entries

Custom agents are persisted in SQLite and merged with the read-only built-in catalog. The HTTP API, CLI, workspace selector, direct chat prompt, and multi-step runtime all resolve the same definition, including its system prompt, tool allowlist, review mode, and enabled state.

```bash
superscience agent create chemistry-review \
  --name "Chemistry Review" \
  --domain chemistry \
  --description "Reviews chemistry evidence and uncertainty." \
  --system-prompt "Require structure-backed evidence and explicit uncertainty." \
  --tool read --tool grep --tool science_search \
  --review required
superscience agent show chemistry-review
superscience agent disable chemistry-review
superscience agent enable chemistry-review
superscience agent remove chemistry-review
```

Skill entry manifests separate picker visibility from runtime availability. For example, this writes `$SUPERSCIENCE_DATA_DIR/skills/superscience-skills.json`; unlisted user skills remain loadable by name as internal helpers.

```bash
superscience skill set-entries user evidence-audit,lab-search
```

Installed namespaces use the same command and are loaded from `$SUPERSCIENCE_DATA_DIR/installed-skills/<namespace>/`.

## Upgrade, Uninstall, and Releases

The updater reads the latest published GitHub Release, selects the exact `GOOS/GOARCH` CLI archive, verifies its SHA-256 entry from `checksums.txt`, validates the archive paths, and only then performs an atomic replacement. A normal invocation checks only; installation requires explicit confirmation:

```bash
superscience upgrade --check
superscience upgrade
superscience upgrade --yes
```

Uninstall also defaults to a non-destructive plan. Application data is preserved unless both data removal and confirmation are explicit:

```bash
superscience uninstall
superscience uninstall --yes
superscience uninstall --remove-data --yes
```

For a local CLI release archive:

```bash
make release-cli VERSION=v0.1.0
```

Tag pushes matching `v*.*.*` run tests, build six CLI targets and native Wails desktop targets, verify a combined checksum manifest, and create or update a **draft** GitHub Release. Complete macOS/Windows code signing and inspect the artifacts before publishing the draft. See [docs/releasing.md](docs/releasing.md).

## Agent Client Protocol

`superscience acp` exposes the same persisted runtime to ACP-compatible editors over line-delimited JSON-RPC on stdin/stdout. Build the binary first, configure and verify a default provider/model in Superscience, then point the ACP client at:

```bash
/absolute/path/to/superscience acp
```

The ACP process writes protocol messages only to stdout and diagnostics only to stderr. It supports protocol v1 initialization, session creation and prompt turns, cancellation, permission requests, plan/tool/message updates, session modes, transcript loading, and session list/resume/close. The client must provide an absolute existing `cwd` when creating or restoring a session.

ACP-provided MCP servers can use stdio, SSE, or Streamable HTTP. Per-session environment variables and HTTP headers stay in memory, are redacted from transport fingerprints, and are never written to SQLite. Additional working directories, image/audio prompt blocks, MCP-over-ACP, and ACP authentication are not advertised; clients must not assume those capabilities.

Scientific connectors make live requests only. Each external database request is permission-gated and returns an explicit source failure instead of fabricated records. Stateful notebooks require a real Jupyter kernelspec (`python3`, `ir`, or `julia`); LSP features require the corresponding language server such as `gopls`, Pyright, or TypeScript language server.

## Security

Superscience is a local workbench, not a sandbox. The HTTP server binds to localhost and rejects non-local hosts/origins. File APIs are constrained to the active project root and reject traversal. Shell/write-style operations are designed to pass through permission gates, but strong isolation requires a container or VM.

See [SECURITY.md](SECURITY.md) for vulnerability reporting, release verification, cloud deployment boundaries, and the current signing policy.

## Reference

`reference/openscience` is kept only as a local analysis reference and is ignored by Git. Do not vendor OpenScience code into this repository without preserving Apache-2.0 license and NOTICE obligations.
