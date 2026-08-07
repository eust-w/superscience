# Superscience Security

## Reporting

Do not open a public issue for a suspected vulnerability or exposed credential. Use the private security-advisory flow for the GitHub repository, include affected versions, reproduction steps, impact, and any relevant logs with secrets removed.

Never include provider keys, OIDC tokens, database URLs, S3 credentials, session bundles containing private research, or raw keychain output in reports.

## Security Boundary

Superscience is a local research workbench, not a process sandbox. Permission approval controls when risky operations execute, but an approved shell, notebook kernel, MCP server, plugin subprocess, or language server runs with the operating-system rights of the Superscience user. Use a container or VM when executing untrusted research material or tools.

The local HTTP service binds to loopback and rejects non-local Host and Origin values. File operations are constrained to the active project root. Provider secrets use the OS keychain where supported and are never returned by the API. Cloud production mode requires OIDC or an explicit bootstrap secret, PostgreSQL with transport security, and S3-compatible object storage.

## Release Integrity

CLI release archives use deterministic names and contain only `superscience` or `superscience.exe`. `checksums.txt` contains SHA-256 values for every release asset. Verify an archive before installation:

```bash
shasum -a 256 -c checksums.txt
```

`superscience upgrade` verifies the matching checksum and rejects unsafe archive paths before replacing a binary. The checksum manifest and archive are distributed through the same GitHub Release trust boundary; they do not replace platform code signing.

The release workflow creates drafts. Do not publish a desktop release until macOS notarization/signing and Windows Authenticode signing have been completed and verified. Linux and CLI consumers should still verify `checksums.txt`.

## Cloud Deployment

- Terminate public TLS at a reviewed reverse proxy and allow only the configured Cloud host names.
- Use PostgreSQL `sslmode=require` or stronger verification and private network routing.
- Use a dedicated S3 bucket, least-privilege credentials, encryption at rest, versioning, and lifecycle policies.
- Configure an OIDC audience dedicated to Superscience and restrict verified email domains when appropriate.
- Keep local bootstrap auth disabled after OIDC bootstrap unless operational recovery requires it.
- Retain audit events and provider-usage records according to the laboratory's policy.

## Dependency and Secret Scanning

CI runs Go race tests, vet, `govulncheck`, pnpm production audit, CodeQL, Gitleaks, Playwright, and a production-placeholder scan. External GitHub Actions are pinned to full commit SHAs, while their release versions remain documented inline. A green scan is supporting evidence, not a replacement for review of permission boundaries, subprocess arguments, URL handling, secret redaction, and release provenance.
