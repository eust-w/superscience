.PHONY: dev web desktop test lint build e2e verify package release-cli

dev:
	pnpm --dir frontend dev

web:
	go run ./cmd/superscience web

desktop:
	wails dev

test:
	pnpm --dir frontend build
	go test ./...
	pnpm --dir frontend test

lint:
	pnpm --dir frontend lint
	./scripts/check-production-placeholders.sh

build:
	pnpm --dir frontend build
	go build ./cmd/superscience

e2e:
	pnpm --dir frontend e2e

verify:
	pnpm --dir frontend build
	go test -race ./...
	go vet ./...
	go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...
	pnpm --dir frontend lint
	pnpm --dir frontend test
	pnpm --dir frontend build
	pnpm --dir frontend exec playwright test
	pnpm audit --prod
	./scripts/check-production-placeholders.sh

package:
	wails build

release-cli:
	test -n "$(VERSION)"
	go build -trimpath -ldflags "-s -w -X superscience/internal/buildinfo.Version=$(VERSION)" -o dist/input/superscience ./cmd/superscience
	go run ./cmd/releasepack --version "$(VERSION)" --goos "$$(go env GOOS)" --goarch "$$(go env GOARCH)" --binary dist/input/superscience --output dist
