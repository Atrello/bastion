# Bastion

A panel for a homelab: the Docker compose stacks on all your servers, and game servers run from
Pterodactyl's eggs, in one place. Nginx Proxy Manager follows.

A small agent runs on each server and connects out to the panel, so servers need no open ports. The panel
shows each server's health, lists every stack and container, and lets you edit a stack's compose file and
`.env`, deploy it, update its images and read its logs.

![The overview](docs/images/overview.png)

## Status

Early. The panel's screens are built and run on demo data; the panel's server and the agent are next.

| Part | State |
|---|---|
| Panel web app (`web`) | Built: overview and servers; Docker (stacks, a stack's compose file, `.env`, containers, logs and history; containers); game servers (each one's console, files, startup settings, schedules and backups; the egg library) |
| Panel server (`cmd/bastion`, `internal/panel`) | Started: one Go binary with the web app built in, SQLite, sign-in with two-factor required and passkeys. Servers and stacks next |
| Agent | Next: Go, one binary per server |

What's planned is in [docs/roadmap.md](docs/roadmap.md), and how it's being built, step by step, in
[docs/plan.md](docs/plan.md).

## Trying it

Requires Go and Node.js 22.

```sh
make
BASTION_PUBLIC_URL=http://localhost:8080 ./bin/bastion     # its log prints the setup link
```

Or just the screens, on a made-up homelab: `cd web && npm install && npm run demo`.

More in [docs/development.md](docs/development.md); how it's kept secure in [docs/security.md](docs/security.md).

## Layout

```
web/          the panel's web app (Svelte, Vite)
docs/               the plan, security, the API, development, the roadmap
```
