# Bastion

A panel for a homelab: the Docker compose stacks on all your servers in one place, with game servers and
Nginx Proxy Manager to follow.

A small agent runs on each server and connects out to the panel, so servers need no open ports. The panel
shows each server's health, lists every stack and container, and lets you edit a stack's compose file and
`.env`, deploy it, update its images and read its logs.

![The overview](docs/images/overview.png)

## Status

Early. The panel's screens are built and run on demo data; the panel's server and the agent are next.

| Part | State |
|---|---|
| Panel web app (`panel/web`) | Built: overview, servers, stacks, a stack's compose file, `.env`, containers, logs and history, containers |
| Panel server (`panel`) | Next: Go, SQLite, sign-in, the API in [docs/api.md](docs/api.md) |
| Agent (`agent`) | After the panel server: Go, one binary per server |

What's planned, in order, is in [docs/roadmap.md](docs/roadmap.md).

## Trying the screens

Requires Node.js 22.

```sh
cd panel/web
npm install
npm run demo        # http://localhost:5173, with a made-up homelab
```

More in [docs/development.md](docs/development.md).

## Layout

```
panel/web/          the panel's web app (Svelte, Vite)
  src/pages/        one file per screen
  src/components/   pieces the screens share
  src/lib/          the API client, demo data, router and helpers
docs/               how it's built, the API, development, the roadmap
```
