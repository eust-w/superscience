# Superscience Release Process

## Version Contract

Release tags use semantic versions such as `v0.1.0`. The tag is injected into `internal/buildinfo.Version`; the commit SHA is injected into `internal/buildinfo.Commit`.

Updater-compatible CLI assets use these names:

```text
superscience_<version>_darwin_amd64.tar.gz
superscience_<version>_darwin_arm64.tar.gz
superscience_<version>_linux_amd64.tar.gz
superscience_<version>_linux_arm64.tar.gz
superscience_<version>_windows_amd64.zip
superscience_<version>_windows_arm64.zip
checksums.txt
```

The Go release packager writes an individual `.sha256` sidecar for each matrix job. The publish job merges those sidecars, hashes desktop archives, verifies the complete manifest, removes the sidecars, and uploads one `checksums.txt`.

## Preflight

Run from a clean intended release commit:

```bash
pnpm install --frozen-lockfile
pnpm --dir frontend exec playwright install chromium
make verify
wails build
go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12
```

Review `git diff`, dependency audit results, generated OpenAPI changes, migrations, permission policy changes, and user-facing release notes. Confirm no provider keys or research data are present.

## Build a Draft

Push the semantic tag:

```bash
git tag v0.1.0
git push origin v0.1.0
```

The Release workflow validates the project, cross-builds CLI archives, builds Wails on native macOS/Windows/Linux runners, verifies checksums, and creates a draft GitHub Release. A rerun updates draft assets with `--clobber`. The workflow refuses to modify assets on an already published release.

## Sign and Publish

1. Sign and notarize the macOS application, then replace the draft macOS archive.
2. Authenticode-sign the Windows executable, then replace the draft Windows archive.
3. Recompute `checksums.txt` for every final asset and verify it on a separate machine.
4. Smoke-test `superscience version`, Provider health, one streamed turn, and `superscience upgrade --check` against the draft-equivalent artifacts.
5. Publish the GitHub Release only after artifact names, signatures, checksums, and release notes are correct.

The updater uses GitHub's latest **published** release endpoint, so drafts and prereleases do not become automatic update targets.

## Rollback

If a published release is unsafe, mark it unavailable immediately and publish a fixed higher semantic version. Do not silently replace a published executable while retaining the old checksum. Preserve the affected assets and audit evidence privately for incident review.

## Source and media previews

A prerelease may contain only GitHub source archives and inspected launch media. Its notes must explicitly state that it contains no signed desktop packages. Publish it against the reviewed commit and include SHA-256 checksums for media assets. The native build workflow must not attach unreviewed binaries to an already published preview. Prereleases remain excluded from automatic updater targets.
