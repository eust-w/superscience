# Contributing to Superscience

Thank you for helping make research workflows easier to inspect and reproduce. Contributions can be code, a minimal bug reproduction, a connector acceptance report, documentation or an accessible UI improvement. English and Chinese reports are welcome.

## Choose a focused change

Search existing [issues](https://github.com/eust-w/superscience/issues) before opening a new one. Describe the research workflow or user-visible failure first. For larger changes, propose the API, permission boundary and data flow in an issue before implementing them.

Useful areas include provider interoperability, real-source connector checks, native credential storage, notebook reliability and clear evidence presentation. Planned features in `docs/design/` are design material; check the implementation before treating them as shipped behavior.

For vulnerabilities or exposed credentials, follow [SECURITY.md](SECURITY.md) rather than opening a public issue. Never attach real API keys, private research, full database dumps or unredacted share bundles.

## Set up the workspace

Follow [Getting started](docs/getting-started.md), including the Python kernel setup when running notebook tests. Build the frontend before Go package tests because the desktop entry point embeds `frontend/dist`.

```bash
pnpm install --frozen-lockfile
pnpm --dir frontend build
pnpm --dir frontend exec playwright install chromium
```

Run the backend and frontend development server in separate terminals:

```bash
# Terminal 1
go run ./cmd/superscience serve

# Terminal 2
pnpm --dir frontend dev
```

Use a dedicated project and `SUPERSCIENCE_DATA_DIR` for demonstrations. Avoid recording your normal research history or credentials.

## Implement and verify

Keep the change focused and follow adjacent code. Validate input at API and subprocess boundaries, use environment variables or the credential backend for secrets, and preserve existing permission checks. Keep third-party notices intact.

Choose tests that exercise the changed behavior:

| Change | Useful checks |
| :--- | :--- |
| Go behavior or API | Targeted `go test` package, then `go test -race ./...` and `go vet ./...` |
| React behavior | `pnpm --dir frontend lint`, `pnpm --dir frontend test`, production build |
| User workflow | Relevant Playwright scenario with a real Go/SQLite backend |
| Scientific connector | Request/response fixtures plus an optional, clearly labeled live-source check |
| Permissions, filesystem or credentials | Failure paths, denial behavior, path boundaries and redaction checks |
| Documentation or media | Working links, rendered layout, accurate captions and no private data |

Before submitting a code change, run the repository verification target:

```bash
make verify
```

It includes dependency and vulnerability checks. Report any failure or skipped integration honestly. Cloud PostgreSQL/S3/OIDC integration and provider acceptance need their own environments; a local unit test cannot establish those results.

## Open a pull request

Explain the problem and the resulting behavior, then list the verification that you actually ran. Add screenshots for visual changes and identify fixture, synthetic or design-only content in captions. For a bug fix, include the smallest reproduction and the relevant regression check.

Review `git diff` before pushing. Exclude `.venv`, `node_modules`, generated build output, databases, secrets and personal research material. Use a descriptive commit message, such as `fix: preserve notebook output after rerun` or `docs: explain provider setup`.

By submitting a contribution, you agree that your original contribution is provided under this repository's [Apache-2.0 license](LICENSE). You must have the right to contribute it; third-party material keeps its own license and notices.

## Media and demonstrations

Use a dedicated demo project, cite external sources, and distinguish a live service result from a protocol fixture or design illustration. Never imply benchmark wins, scientific conclusions, provider support or platform availability that the evidence does not establish. Label synthetic narration and generated imagery when used. Keep the reproducible source of original diagrams and demonstrations alongside their documentation where practical.
