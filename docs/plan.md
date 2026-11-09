# The build plan

How Bastion gets from screens on demo data to something running a homelab, in order. It's followed as
written; anything that has to change is changed here first, with the reason, in "Changes to the plan" at
the bottom.

## Principles

- **Lean.** Go's standard library first; a dependency only when writing it ourselves would be worse (the
  SQLite driver, the websocket library, CodeMirror). Every dependency is listed in this file.
- **Performance first.** One process for the panel, one for each agent. No polling where a push will do:
  agents send their state when it changes, and the web app gets updates over one websocket.
- **Readable.** Code laid out the way a person would lay it out: small packages with one job each, a
  comment at the top of every file saying what it's for, names that say what things are.
- **Documented as it's built.** A step isn't done until its docs are.
- **Tested where it matters.** The protocol, the API, sign-in, and anything that writes to a server. Not
  tests for the sake of numbers.
- **Safe by default.** The agent only does what's in its list of operations, touches only stack files it
  knows about, and connects out (servers need no open ports).

## Dependencies

| Where | Dependency | Why |
|---|---|---|
| Panel and agent | `github.com/gorilla/websocket` | The agent connection and live updates to the browser |
| Panel | `modernc.org/sqlite` | SQLite without a C compiler, so the panel builds anywhere |
| Panel | `golang.org/x/crypto` (argon2id) | Hashing the sign-in password |
| Agent | `github.com/creack/pty` | A real terminal for container consoles and game consoles |
| Web app | Svelte, Vite, CodeMirror | As it is now |

## Milestone 0.1: Docker stacks, for real

1. **Panel server skeleton.** `panel/cmd/bastion`: config from flags and environment, SQLite with
   migrations, the web app built into the binary, request logging. `panel/internal/...` packages: `store`
   (database), `api` (HTTP handlers), `agents` (connected agents), `auth`.
2. **Sign-in.** One owner account made on first run (a setup page), password hashed with argon2id,
   session cookie, sign-in page in the web app, limits on failed attempts.
3. **Agent protocol.** One websocket per agent, JSON messages: requests and answers, streams (logs,
   consoles), and events (hello, metrics, state changes). Written down in `docs/protocol.md` first, with
   shared Go types.
4. **Agent.** `agent/`: join with a one-time token, run as a systemd service, reconnect with back-off,
   metrics, list stacks (running and in `/opt/stacks`) and containers, read and save compose and `.env`
   (checked by `docker compose config` first, written atomically), stack and container actions.
5. **The API, wired up.** Every endpoint in `docs/api.md` for servers, stacks and containers, answered
   from the agents. Compose history kept in the panel (`.env` never is).
6. **Live logs and container consoles.** Logs follow live over the websocket (not just the last 200
   lines). A console tab on a stack's page: a terminal into any of its containers (xterm.js, loaded only
   when opened).
7. **Image updates.** The agent checks image digests against the registry; stacks show "update
   available" and Update images pulls and recreates.
8. **Install.** `install.sh` for the join command, a systemd unit for the agent, a container image and
   compose file for the panel, `docs/install.md`.
9. **Done when:** a fresh panel and two servers can be set up from the docs alone, and every screen in the
   Docker section works against real Docker.

## Milestone 0.2: Nginx Proxy Manager

1. Connect an NPM instance (address and an NPM user), checked on save.
2. Proxy hosts, redirects, streams, 404 hosts, access lists, custom locations, advanced config and
   certificates (Let's Encrypt by HTTP or DNS): list, add, edit, delete, everything NPM's own screens do.
3. "Expose this" on a stack: pick a domain, and the host and certificate are made pointing at the right
   server and port.
4. Health checks on each host and certificate expiry warnings, shown on the overview.
5. **Done when:** everything done in NPM's own interface can be done in Bastion instead.

## Milestone 0.3: Game servers

1. Eggs: import (file, pasted JSON or a link), check, and replace an older version after asking.
2. The agent runs an egg's installer, then the game in its container with memory, CPU and disk limits.
3. Console both ways (output live, commands in), power actions with a warning to players before stop.
4. File manager: browse, edit text files, upload, download, rename, delete.
5. Startup variables, schedules (restart with warning, command, backup) and backups (make, lock,
   restore, download).
6. Sharing a game server with a friend, who sees only that one.
7. **Done when:** a Minecraft (Paper) and a Steam game (Valheim) server can be made, run, backed up and
   restored from Bastion alone.

## After 0.3

Alerts, backups of stack volumes, an activity log. Kept in `docs/roadmap.md`, planned in detail when
0.3 is done.

## Changes to the plan

| Date | Change | Why |
|---|---|---|
| 9 Nov 2026 | Plan written | |
