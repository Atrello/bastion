# Bastion

A panel for a homelab: the Docker compose stacks on all your servers, Nginx Proxy Manager, and game servers
run from Pterodactyl's eggs, in one place.

A small agent runs on each server and connects out to the panel, so servers need no open ports. The panel
shows each server's health, lists every stack and container, and lets you edit a stack's compose file and
`.env`, deploy it (or follow it from Git), update its images, read its logs live and open a console in its
containers. It manages Nginx Proxy Manager through NPM's own API, checks each proxy host is answering,
and runs game servers with a live console, file manager, schedules, backups, and sharing with friends.

![The overview](docs/images/overview.png)

## Status

Everything in the plan's three milestones is built ([docs/plan.md](docs/plan.md)): Docker stacks (0.1),
Nginx Proxy Manager (0.2) and game servers (0.3). It's been tested against real Docker, a real Nginx Proxy
Manager and a game egg in a real container; what couldn't be tested yet (real servers, passkeys on real
devices, Let's Encrypt, a real Minecraft download) is listed in the plan.

| Part | What it is |
|---|---|
| Panel web app (`web`) | Svelte. Overview, servers, alerts; Docker (stacks, containers, volumes, images); proxy (Nginx Proxy Manager); game servers and eggs; your account |
| Panel server (`cmd/bastion`, `internal/panel`) | One Go binary with the web app built in, SQLite, two-factor sign-in with passkeys, the agents' hub, NPM, eggs, schedules |
| Agent (`cmd/bastion-agent`, `internal/agent`) | One Go binary on each server: connects out, reports what Docker runs, and does only the operations in `docs/protocol.md` |

What's planned is in [docs/roadmap.md](docs/roadmap.md), and how it's being built, step by step, in
[docs/plan.md](docs/plan.md).

## Installing

The panel runs in Docker behind your reverse proxy (which gives it HTTPS); each server then runs one command
from the panel. In full: [docs/install.md](docs/install.md).

1. In a new folder, save this as `compose.yaml` (or take [deploy/compose.yaml](deploy/compose.yaml)):

   ```yaml
   services:
     bastion:
       image: ghcr.io/atrello/bastion:${BASTION_VERSION:-latest}
       restart: unless-stopped
       environment:
         BASTION_PUBLIC_URL: ${BASTION_PUBLIC_URL:?set BASTION_PUBLIC_URL in .env}
         BASTION_TRUST_PROXY: ${BASTION_TRUST_PROXY:-true}
         BASTION_SESSION_IDLE: ${BASTION_SESSION_IDLE:-12h}
         BASTION_DNS_FALLBACK: ${BASTION_DNS_FALLBACK:-1.1.1.1,9.9.9.9}
       volumes:
         - bastion-data:/data
       ports:
         - "${BASTION_BIND:-127.0.0.1}:${BASTION_PORT:-8080}:8080"

   volumes:
     bastion-data:
   ```

2. Beside it, save [deploy/.env.example](deploy/.env.example) as `.env` and set `BASTION_PUBLIC_URL` to the
   panel's address, like `https://bastion.example.com`. If your reverse proxy is on another machine, set
   `BASTION_BIND` to this machine's LAN address.
3. `docker compose up -d`, then `docker compose logs bastion` for the one-time setup link.
4. In your reverse proxy, send the address to port 8080 here with **websockets on**. For Nginx Proxy Manager,
   add this to the host's Advanced tab, for big uploads and long jobs:

   ```nginx
   client_max_body_size 0;
   proxy_request_buffering off;
   proxy_read_timeout 3600s;
   proxy_send_timeout 3600s;
   ```

5. Open the setup link, make your account, then **Servers**, **Add a server** and run its command on each
   server.

To update: `docker compose pull && docker compose up -d`, then **Update agent** on each server.

## Trying it

Requires Go and Node.js 22.

```sh
make
BASTION_PUBLIC_URL=http://localhost:8080 ./bin/bastion     # its log prints the setup link
```

Or just the screens, on a made-up homelab: `cd web && npm install && npm run demo`.

More in [docs/development.md](docs/development.md); how it's kept secure in [docs/security.md](docs/security.md).
What still needs testing on real servers is in [docs/testing.md](docs/testing.md).

## Layout

```
web/          the panel's web app (Svelte, Vite)
docs/               the plan, security, the API, development, the roadmap
```
