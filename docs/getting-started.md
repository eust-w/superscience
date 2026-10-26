# Getting started

[English overview](../README.md) · [中文概览](../README.zh-CN.md) · [Architecture](architecture.md)

Superscience runs a local Go service and a React workbench. A browser is enough for the source-based interface; Wails is required only for the desktop shell. Model requests and scientific searches contact external services.

## Prerequisites

| Component | Requirement |
| :--- | :--- |
| Go | **1.26.10 or newer**, the minimum declared in `go.mod`. Allow Go to download a compatible toolchain if needed. |
| Node.js | Version 24 |
| pnpm | Version 11.7.0, matching `package.json` |
| Provider | An accessible model and provider credential for AI requests; normal provider billing applies |
| Python notebook | Python 3, the dependencies in `workers/python/requirements.txt`, and a `python3` Jupyter kernelspec |
| Desktop, optional | Wails CLI matching the module's Wails 2.12.0 plus its OS build prerequisites |

The native persistent credential backend currently implements macOS Keychain. Linux and Windows can use environment variables with CLI provider execution; the first-run UI's credential-saving flow is not yet a complete cross-platform AI setup path. **Open local tools** bypasses the need for a model credential when using files, scientific search and notebooks. It does not bypass network or execution permissions.

## Run the browser interface

```bash
git clone https://github.com/eust-w/superscience.git
cd superscience
pnpm install --frozen-lockfile
pnpm --dir frontend build
go run ./cmd/superscience serve
```

Open **http://127.0.0.1:4096/**. `web` starts the same service and also asks the OS to open its URL. `serve` leaves browser opening to you. Both bind to loopback.

Choose **Open local tools** to open a project without configuring AI. Files, scientific search and notebooks remain available, subject to their normal dependencies and permission checks. AI chat is visibly disabled until a provider and model pass verification. Scientific search still requires access to its upstream service.

For AI chat on macOS, complete the first-run form:

1. Select a provider and review the endpoint.
2. Enter a model ID your account can access. The catalog is a discovery aid; access still depends on your provider account.
3. Enter the key in the app and run verification.
4. Once verification succeeds, open a dedicated project folder or return to the workbench with AI enabled.

Provider metadata and settings persist in SQLite. Secret values are loaded through the credential backend and are not returned by the API. Do not paste keys into issue reports, recordings or chat prompts.

## Separate demo and personal data

The default database is `superscience.db` inside the OS user configuration directory's `superscience` folder. Print the resolved location with:

```bash
go run ./cmd/superscience debug paths
```

For a separate demo database and project:

```bash
mkdir -p .qa/demo-state .qa/demo-project
SUPERSCIENCE_DATA_DIR="$PWD/.qa/demo-state" \
  go run ./cmd/superscience serve --port 4098 --project "$PWD/.qa/demo-project"
```

Open **http://127.0.0.1:4098/**. `.qa/` is ignored by Git. The data-directory override isolates SQLite and local files; it does not create a separate macOS Keychain service or isolate processes.

## Enable Python notebooks

From the repository root on macOS or Linux:

```bash
python3 -m venv .venv
.venv/bin/python -m pip install -r workers/python/requirements.txt
.venv/bin/python -m ipykernel install --prefix "$PWD/.venv" --name python3
export SUPERSCIENCE_PYTHON="$PWD/.venv/bin/python"
export JUPYTER_PATH="$PWD/.venv/share/jupyter"
.venv/bin/python -m jupyter kernelspec list
go run ./cmd/superscience serve
```

On Windows, use the equivalent `.venv\Scripts\python.exe` executable and set the environment variables in your shell. R and Julia notebook support additionally requires their respective `ir` and `julia` kernelspecs; installing the Python requirements does not install those languages.

Try a small cell in a dedicated project:

```python
from statistics import mean

observations = [12, 15, 18, 21]
print({"count": len(observations), "mean": mean(observations)})
```

Review the notebook permission request. After approving it, rerun the selected cell if requested. The expected output is `{'count': 4, 'mean': 16.5}`. This is a synthetic arithmetic example, not a scientific dataset or model result.

Kernel state is retained for a notebook until interruption/restart or process shutdown. Notebook output can be saved as an artifact, and the notebook can be exported as `.ipynb`. Execution uses your OS user's privileges; use an isolated environment for untrusted code.

## Try the included research starter

Choose **Open local tools**, then open [`examples/research-starter`](../examples/research-starter/) as a project. This folder includes a CSV, a Python script and a notebook containing the same 10 synthetic measurements.

From that example folder, run:

```bash
python3 analysis.py
```

Expected output includes `Trials: 10` and `Mean latency: 125.20 ms`. The median is 124.50 ms, sample standard deviation is 5.69 ms, and the invented success rate is 90%. All values describe synthetic demonstration data, not measured performance or a scientific finding.

For the notebook panel, copy the standalone code cell from `analysis.ipynb`. It embeds the same CSV, so it does not depend on the kernel's working directory. Install the Python kernel as described above, review the permission request and run the cell. Both examples use only the Python standard library.

## Try a research workflow

Open a project and a session through **Open local tools** or verified provider setup:

1. In **Science**, select **Crossref** and search for `robot learning`. Approve the network request, then repeat the query when prompted. Live records vary over time.
2. Inspect the result metadata and **Open source** link. **Save artifact** stores the selected record. **Send** requires AI provider verification because it submits the title and link to the assistant session.
3. Use **Notebook** for the included synthetic analysis and inspect the resulting output. Required kernels must be installed before the server starts. The retrieved literature does not validate the invented measurements.
4. In **Ecosystem → share**, select an artifact from the current session. Review the sharing permission request before publishing a bundle.

A local share URL is served by your local process. It does not become an internet-hosted page automatically. Self-hosted Cloud is a separate deployment. Share only material you have permission to distribute.

Launch checks exercised actual Jupyter execution and live Crossref retrieval. The attempted provider verification returned HTTP 503; no successful external-model response is claimed. See the [validation record](readme/validation.md) for the completed checks and their scope.

## Environment-backed CLI

The provider resolver reads these environment variables, in order:

1. `SUPERSCIENCE_<PROVIDER_ID>_API_KEY`, with the ID uppercased and `-`/`.` replaced by `_`.
2. `SUPERSCIENCE_API_KEY`.
3. `<PROVIDER_ID>_API_KEY`, such as `OPENAI_API_KEY` or `ANTHROPIC_API_KEY`.

Set the appropriate secret through your shell or secret manager without committing it. Then use an accessible model ID:

```bash
# OPENAI_API_KEY must already be set in this terminal.
MODEL_ID='your-accessible-model-id'
go run ./cmd/superscience run \
  --project . --provider openai --model "$MODEL_ID" \
  "List the evidence needed to review this project, and state what is missing."
```

`run` persists the project, session, model usage, steps and tool calls. It shares the backend permission model. An environment variable alone does not complete the browser's provider/model settings flow; use **Open local tools** when you do not need AI chat.

On macOS, you can save an existing environment variable to Keychain without putting its value in a command argument:

```bash
go run ./cmd/superscience keys add openai --key-env OPENAI_API_KEY
go run ./cmd/superscience keys list
```

Additional entry points:

```bash
go run ./cmd/superscience doctor
go run ./cmd/superscience models
go run ./cmd/superscience agent list
go run ./cmd/superscience export SHARE_ID --output research-bundle.json
go run ./cmd/superscience import research-bundle.json
```

ACP-compatible editors can run a built `superscience acp` binary over stdin/stdout after provider/model settings are configured. Supply an existing absolute working directory from the client. Protocol messages go to stdout and diagnostics to stderr; see [`internal/acp`](../internal/acp/) for the supported protocol surface.

## Develop the frontend

Build once, then run the backend and Vite in separate terminals:

```bash
# Terminal 1, from the repository root
go run ./cmd/superscience serve

# Terminal 2
pnpm --dir frontend dev
```

Use the URL printed by Vite. Its `/api` proxy targets `http://127.0.0.1:4096` by default; set `SUPERSCIENCE_SERVER` to the backend URL before starting Vite if using a different port.

## Build the desktop shell

Install the Wails CLI and required native development libraries for your OS, then run from the repository root:

```bash
wails doctor
wails dev
# Or create a local build:
wails build
```

This produces a local development build, not a signed distribution. Platform signing, notarization and checksum verification are covered in [Releasing](releasing.md).

## Self-hosted Cloud

Cloud is optional and separate from the loopback workbench. Production startup requires PostgreSQL metadata storage, S3-compatible artifact storage and configured authentication. These are the principal environment settings:

| Purpose | Variables |
| :--- | :--- |
| Database | `SUPERSCIENCE_CLOUD_DATABASE_URL` |
| Artifact storage | `SUPERSCIENCE_CLOUD_S3_ENDPOINT`, `SUPERSCIENCE_CLOUD_S3_REGION`, `SUPERSCIENCE_CLOUD_S3_BUCKET`, `SUPERSCIENCE_CLOUD_S3_ACCESS_KEY`, `SUPERSCIENCE_CLOUD_S3_SECRET_KEY` |
| OIDC | `SUPERSCIENCE_CLOUD_OIDC_ISSUER`, `SUPERSCIENCE_CLOUD_OIDC_CLIENT_ID`, optional `SUPERSCIENCE_CLOUD_OIDC_ALLOWED_EMAIL_DOMAINS` |
| Hosts | `SUPERSCIENCE_CLOUD_ALLOWED_HOSTS` |

Provide secrets through your deployment's secret manager, then run `go run ./cmd/superscience cloud`. The default listener is `127.0.0.1:4097`; public deployments require an explicitly configured host, TLS proxy and reviewed access controls. Consult [SECURITY.md](../SECURITY.md).

For a single-machine development instance only, `cloud --dev-sqlite` permits SQLite and inline artifact storage. It still requires authentication: configure OIDC or a high-entropy `SUPERSCIENCE_CLOUD_LOCAL_AUTH_SECRET` of at least 24 characters. Do not treat this mode as a production setup.

## Troubleshooting

| Symptom | Check |
| :--- | :--- |
| Go reports missing `frontend/dist` | Run `pnpm --dir frontend build` before testing the root Go package. |
| No API key, or provider verification fails | Choose **Open local tools** for files, scientific search and notebooks. AI chat stays disabled until verification succeeds. |
| Provider returns HTTP 503 | The model service is unavailable for that request. Check its status and retry later; local tools do not require a successful model call. |
| Cannot save an AI credential on Linux/Windows | Use the CLI environment path while native credential persistence is incomplete. |
| Keychain operation fails | Check macOS Keychain access for the current user. Do not paste raw keychain output into a report. |
| Notebook kernel missing | Run the kernelspec listing above and confirm the server inherited `SUPERSCIENCE_PYTHON` and `JUPYTER_PATH`. |
| Notebook worker missing | Start from the repository root, or set `SUPERSCIENCE_JUPYTER_WORKER` to `workers/python/jupyter_kernel.py` using an absolute path. |
| Scientific search requests approval | Approve the pending request, then repeat the same search. |
| Source returns no records or an error | Check the query, upstream availability, credentials and rate limits. A failed live request is not replaced by demo data. |
| Port already in use | Choose another port with `serve --port 4098`. |
| LSP diagnostics unavailable | Install the appropriate language server, such as `gopls`, Pyright or TypeScript language server. |

For reproducible failures, use the [bug report form](https://github.com/eust-w/superscience/issues/new?template=bug_report.yml) with private data removed.
