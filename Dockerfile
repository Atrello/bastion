# The panel's container image: the web app built in, and the agent for amd64 and arm64 for new servers to
# download. Runs as a user without privileges, with nothing else in the image.

FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

FROM golang:1.27-alpine AS go
WORKDIR /src
ENV CGO_ENABLED=0 GOTOOLCHAIN=auto
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
# The version is the VERSION file's, unless given with --build-arg VERSION=...
ARG VERSION
RUN V="${VERSION:-$(cat VERSION)}" \
 && go build -trimpath -ldflags "-s -w -X main.version=$V" -o /out/bastion ./cmd/bastion \
 && GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w -X github.com/Atrello/bastion/internal/agent.Version=$V" -o /out/agents/bastion-agent-linux-amd64 ./cmd/bastion-agent \
 && GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "-s -w -X github.com/Atrello/bastion/internal/agent.Version=$V" -o /out/agents/bastion-agent-linux-arm64 ./cmd/bastion-agent \
 && mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=go /out/bastion /bastion
COPY --from=go /out/agents /agents
# The data folder belongs to the user the panel runs as, so a new volume mounted there does too.
COPY --from=go --chown=65532:65532 /out/data /data
ENV BASTION_ADDR=:8080 BASTION_DATA_DIR=/data BASTION_AGENT_BINARIES=/agents
VOLUME /data
EXPOSE 8080
USER nonroot
ENTRYPOINT ["/bastion"]
