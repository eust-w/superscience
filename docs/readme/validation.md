# Release validation

Date: 2026-10-26. Source and media research preview, tested on macOS with Go 1.26.10, Node.js 24 and pnpm 11.7.0. Screenshots use a separate local demo database and explicitly synthetic measurement data.

## Automated checks

| Check | Result |
| --- | --- |
| Go race suite | Passed across 21 packages with tests |
| `go vet ./...` | Passed |
| `govulncheck ./...` | No reachable vulnerabilities; no affected imported packages |
| Frontend typecheck, unit tests and production build | Passed; 12 unit tests |
| Browser acceptance | Passed; 10 browser tests, including provider-selection races and offline editors |
| Frontend dependency audit | Zero findings, including development dependencies |
| Python worker requirements and isolated validation environment | `pip-audit` passed |
| Launch-video dependencies | `npm audit` passed |
| GitHub Actions syntax | `actionlint` passed |
| Production placeholder scan | Passed |

The Go module graph still includes the advisory for the deprecated `golang.org/x/crypto/openpgp` package (GO-2026-5932). Superscience does not import that package; the symbol and imported-package scans report no affected code. This is not a claim that every transitive module has no advisory.

The aggregate `make verify` invocation encountered an EOF downloading the vulnerability tool. Its completed stages passed; the tool and remaining stages were rerun separately. Browser E2E uses an explicitly named localhost protocol fixture, so it tests integration rather than external model quality. The editor acceptance case blocks external HTTP requests and checks local editing and file content.

## Real application acceptance

- A real Jupyter Python kernel executed the included starter example: 10 synthetic measurements, mean 125.20 ms, median 124.50 ms and sample standard deviation 5.69 ms. These values are demonstration data, not a performance benchmark.
- A live Crossref search for “robot learning” returned 20 records. A selected record was saved with its source DOI.
- Permission approval, artifact creation, provenance and a share replay were exercised against the local server. This is not hosted Cloud acceptance.
- The configured external model provider returned HTTP 503. Local tools remained available; AI execution stayed disabled. No successful external-model response is claimed.
- The bundled Monaco editor opened and accepted input with external network access blocked.

The README contains actual application captures. The launch film combines those captures with original explanatory motion graphics and synthetic Chinese narration; both are labelled. No private research files, API keys, local database or provider endpoint is included in the release.

## Distribution boundaries

This prerelease distributes source and launch media. Native signed/notarized desktop packages, all-platform hardware acceptance, full live coverage of the 51 registered scientific connectors, and production Cloud deployment remain separate work. The GitHub updater ignores prereleases. Approved terminal, notebook and external-tool execution uses the current OS user's privileges; permission approval is not process isolation.

See [the release process](../releasing.md) and [security policy](../../SECURITY.md) before distributing binaries or deploying the optional Cloud service.
