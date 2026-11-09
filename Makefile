# Building and checking Bastion. `make` builds the panel into bin/; `make check` runs everything a change
# has to pass: tests, vet, formatting, the vulnerability scans and the web app's type check.

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all build web panel agent agents test check image

all: build

build: web panel agent

web:
	cd web && npm ci --no-audit --no-fund && npm run build

panel:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/bastion ./cmd/bastion

agent:
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X github.com/Atrello/bastion/internal/agent.Version=$(VERSION)" -o bin/bastion-agent ./cmd/bastion-agent

# The agent for each kind of server, for the panel to hand out (BASTION_AGENT_BINARIES).
agents:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X github.com/Atrello/bastion/internal/agent.Version=$(VERSION)" -o bin/bastion-agent-linux-amd64 ./cmd/bastion-agent
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X github.com/Atrello/bastion/internal/agent.Version=$(VERSION)" -o bin/bastion-agent-linux-arm64 ./cmd/bastion-agent

# The panel's container image, with the web app and both agents in it.
image:
	docker build -t bastion:$(VERSION) .

test:
	go test ./...

check: test
	go vet ./...
	test -z "$$(gofmt -l .)" || (gofmt -l . && false)
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...
	cd web && npm audit --omit=dev && npm run check
