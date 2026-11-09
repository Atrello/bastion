# Building and checking Bastion. `make` builds the panel into bin/; `make check` runs everything a change
# has to pass: tests, vet, formatting, the vulnerability scans and the web app's type check.

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all build web panel test check

all: build

build: web panel

web:
	cd web && npm ci --no-audit --no-fund && npm run build

panel:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/bastion ./cmd/bastion

test:
	go test ./...

check: test
	go vet ./...
	test -z "$$(gofmt -l .)" || (gofmt -l . && false)
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...
	cd web && npm audit --omit=dev && npm run check
