#!/usr/bin/env sh
set -eu

if ! command -v rg >/dev/null 2>&1; then
  echo "ripgrep is required for the production placeholder scan" >&2
  exit 2
fi

matches=$(rg -n \
  'mockAdapter|fallbackHits|example\.org|local-dev-token|local-placeholder|parity placeholder|live-with-fallback|GPT-5\.4' \
  cloud cmd internal frontend/src app.go main.go \
  --glob '!**/*_test.go' \
  --glob '!**/*.test.ts' \
  --glob '!**/*.test.tsx' \
  --glob '!**/testdata/**' || true)

# The migration must recognize legacy fallback values so it can remove them.
matches=$(printf '%s\n' "$matches" | rg -v "internal/store/store.go:.*live-with-fallback" || true)

if [ -n "$matches" ]; then
  echo "production placeholder scan failed:" >&2
  printf '%s\n' "$matches" >&2
  exit 1
fi

echo "production placeholder scan passed"
