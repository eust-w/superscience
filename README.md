<div align="center">
  <img src="docs/readme/hero.svg" width="100%" alt="Superscience workflow overview">
  <h1>Superscience</h1>
  <p><strong>A local research workbench with tools, notebooks and evidence.</strong></p>
  <p><a href="LICENSE"><img alt="License: Apache-2.0" src="https://img.shields.io/badge/License-Apache--2.0-d99536?style=flat-square"></a> <a href="docs/readme/validation.md"><img alt="Validation notes" src="https://img.shields.io/badge/Validation-repository%20checks-547ac4?style=flat-square"></a></p>
  <p><a href="#overview">Overview</a> · <a href="#quick-start">Quick start</a> · <a href="#workflow">Workflow</a> · <a href="#validation">Validation</a> · <a href="#documentation">Docs</a></p>
  <p><a href="README.md">English</a> · <a href="README.zh-CN.md">简体中文</a></p>
</div>

---

<a id="overview"></a>
## Overview

A Go/Wails/React research workspace for projects, assistant sessions, files, PTY terminals, notebooks, scientific search and inspectable artifacts. Inspired by the OpenScience product boundary; this repository contains its own implementation.

| Capability | What it provides |
| :--- | :--- |
| **Local tools** | File editing, real terminals, permissions and persistent sessions. |
| **Science workspace** | Provider adapters, scientific connector catalog, notebook and LSP integrations. |
| **Evidence** | Artifacts, provenance and optional self-hosted sharing. |

### Local demo

![superscience local demonstration](docs/readme/setup.jpg)

*First-run provider setup; no external model request was made.*

<a id="quick-start"></a>
## Quick start

```bash
git clone https://github.com/eust-w/superscience.git
cd superscience
# Requires Go 1.25+, Node.js 24 and pnpm 11.7.0.
pnpm install --frozen-lockfile
pnpm --dir frontend build
go run ./cmd/superscience serve
# Open http://127.0.0.1:4096/
```

First launch requires a provider/model configuration and authenticated verification before entering the workspace. E2E uses a localhost protocol fixture; it does not establish acceptance of a real cloud model.

<a id="workflow"></a>
## Workflow

```text
INVESTIGATE → EXECUTE → TRACE
```

```bash
python3 -m venv .venv
.venv/bin/python -m pip install -r workers/python/requirements.txt
.venv/bin/python -m ipykernel install --prefix "$PWD/.venv" --name python3
export SUPERSCIENCE_PYTHON="$PWD/.venv/bin/python"
export JUPYTER_PATH="$PWD/.venv/share/jupyter"
make verify
```

<a id="validation"></a>
## Validation

```bash
make verify
```

Recorded environment and executed checks: [validation notes](docs/readme/validation.md).

| Area | Current boundary |
| :--- | :--- |
| Providers | Adapters and protocol fixtures do not establish live acceptance of every provider. |
| Notebooks / LSP | Require Jupyter kernels and installed language servers. Permission controls are not a process sandbox. |
| Distribution | Desktop signing, cloud deployment and external model credentials are separate setup steps. |

<a id="documentation"></a>
## Docs

- [Security / 安全边界](SECURITY.md)
- [Documentation / 文档](docs/)
- [Notebook worker / Notebook worker](workers/python/)

## License and provenance

Original code and newly created demo assets are licensed under [Apache-2.0](LICENSE). Dependencies, models, datasets and external resources retain their terms; see [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
