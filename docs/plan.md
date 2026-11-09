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
| Panel | `github.com/go-webauthn/webauthn` | Passkeys (WebAuthn), the standard Go implementation |
| Panel | `github.com/pquerna/otp` | Authenticator app codes (TOTP) |
| Agent | `github.com/creack/pty` | A real terminal for container consoles and game consoles |
| Web app | Svelte, Vite, CodeMirror | As it is now |
| Web app | `@xterm/xterm`, `@xterm/addon-fit` | The container console's terminal, loaded only when a console is opened |

## Layout

One Go module for the panel and the agent, so they share the protocol's types:

```
cmd/bastion/          the panel's program
cmd/bastion-agent/    the agent's program
internal/protocol/    the messages between them
internal/panel/...    the panel: store (database), auth, agents (connections), api (HTTP), npm, eggs, cron
internal/agent/...    the agent: connection, docker, metrics, git, games and their files
web/                  the web app (Svelte), built into the panel's binary
```

## Milestone 0.1: Docker stacks, for real

1. **Panel server skeleton.** Config from flags and environment, SQLite with migrations, the web app
   built into the binary, security headers, request logging.
2. **Sign-in, with two-factor required.** A one-time setup link (printed in the panel's log) to make
   the owner's account, so nobody else can claim a new panel. Password (argon2id) plus a second factor,
   always: an authenticator app (with recovery codes) or a passkey. Passkeys can also sign in on their
   own. Sessions in a cookie that scripts can't read, ending after idle time; limits on failed attempts.
   The threat model this answers is in `docs/security.md`.
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
8. **Volumes and images.** Pages for each server's volumes and images: what uses each, its size, and
   which are unused. **Clean-up**: remove unused images, volumes and build cache, showing what goes and how
   much space it frees before anything is deleted.
9. **Git-backed stacks** (optional, per stack). A stack follows a folder in a Git repository; the panel
   shows when the repo has changes and deploys them on request (or by itself, if turned on). A deploy key
   per repository, kept on the panel.
10. **Alerts.** A server going offline, a container crash-looping or unhealthy, a disk over 90%, an image
    update available: sent to ntfy, Discord or email, with each kind switched on or off.
11. **Install.** `install.sh` for the join command, a systemd unit for the agent, a container image and
   compose file for the panel, `docs/install.md`.
12. **Done when:** a fresh panel and two servers can be set up from the docs alone, and every screen in the
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

## Progress

| Step | State |
|---|---|
| 0.1 step 1: panel server skeleton | Done |
| 0.1 step 2: sign-in with two-factor and passkeys | Done |
| 0.1 step 3: agent protocol | Done (`docs/protocol.md`, `internal/protocol`) |
| 0.1 step 4: agent | Done (`cmd/bastion-agent`, `internal/agent`) |
| 0.1 step 5: the API, wired up | Done: tested end to end with a real agent and Docker (a stack found, its files read and saved, a broken file refused by Docker, deployed, logs, history) |
| 0.1 step 6: live logs and container consoles | Done: tested live (new lines arriving; a command typed into a container's shell) |
| 0.1 step 7: image updates | Done: tested against Docker Hub (an out-of-date image found; Update images cleared it) |
| 0.1 step 8: volumes, images and clean-up | Done: tested for real (unused image and volume listed; image clean-up kept the volume; volumes only removed when ticked) |
| 0.1 step 9: Git-backed stacks | Done: linking and the branch check tested against GitHub; file:// refused. Deploying from Git and ssh deploy keys are on the testing list (this sandbox has no ssh client and won't run outside code) |
| 0.1 step 10: alerts | Done: a Go test sends a crash-looping container's alert to a stand-in ntfy once, then its "Resolved". Real ntfy, Discord and email are on the testing list |
| 0.1 step 11: install | Done: install.sh tested against a running panel (downloaded the agent, installed it, joined; only systemctl couldn't run in the sandbox). The Docker image build is on the testing list (this sandbox's proxy blocks Go downloads inside builds) |
| 0.1 step 12: done when a fresh panel and two servers can be set up from the docs alone | Done in a lab: the image behind Caddy with HTTPS, a systemd server joined with the one-line command (docs/testing.md) |
| 0.2 steps 1 and 2: linking NPM, and everything in it | Done: tested against a real NPM 2.16 (linking, a wrong password refused, proxy hosts with custom locations and an access list, redirects, streams, 404 hosts, access lists, enabling and disabling, a custom certificate uploaded, a Let's Encrypt request passed through). A Let's Encrypt certificate actually issued is on the testing list (it needs a public domain) |
| 0.2 step 3: Expose this | Done; on the testing list, as it needs a domain pointing at NPM |
| 0.2 step 4: health checks and expiry | Done: checks every 5 minutes and after changes, on the overview and as alerts (`host`, `certificate`) |
| 0.3 step 1: eggs | Done: import from a file, pasted JSON or a link; checked (Pterodactyl's own Paper egg is in the tests); replacing asks first |
| 0.3 step 2: install and run | Done: tested for real with a test egg in Docker (installer, config file filled in, runs as user 988 with limits). A real Paper or Valheim download is on the testing list (this sandbox's proxy breaks TLS inside containers) |
| 0.3 step 3: console and power | Done: live output, commands, stop with the egg's stop command, warning players before a stop or restart, kill |
| 0.3 step 4: file manager | Done: browse, edit, upload and download (5 MB in pieces tested; uploads carry on across pages, can be cancelled, and a cut-off one is removed), rename, delete, folders; paths can't leave the game's folder (tested, links included) |
| 0.3 step 5: startup variables, schedules and backups | Done: variables checked against the egg's rules; schedules (cron) for restart, command and backup; backups made, locked, restored, downloaded, deleted (tested) |
| 0.3 step 6: sharing | Done: an invite link makes a friend's account (two-factor required) that sees only that game; tested that every other route refuses them |
| 0.3 step 7: done when Minecraft (Paper) and Valheim run from Bastion alone | Paper: done, tested for real (installed, EULA, started, pinged, backed up and restored). Valheim: optional on the testing list |

## Changes to the plan

| Date | Change | Why |
|---|---|---|
| 9 Nov 2026 | Plan written | |
| 9 Nov 2026 | Added volumes and images pages, clean-up, Git-backed stacks and alerts to 0.1; two-factor sign-in required, with passkeys | Asked for |
| 9 Nov 2026 | One Go module at the top of the repo (`cmd/`, `internal/`, `web/`) instead of `panel/` and `agent/` | The panel and agent share the protocol's types |
| 9 Nov 2026 | New stacks can be made from the web app (writing a compose file, or from Git) | It wasn't in the plan's screens, but stacks couldn't be created without it |
| 9 Nov 2026 | Passkeys are off when the panel is reached by an IP address, rather than refusing to start | WebAuthn needs a name; authenticator apps still work |
| 9 Nov 2026 | Minecraft's EULA is accepted from a prompt above the console when the game asks for it | Paper won't start without it; Pterodactyl does the same |
| 9 Nov 2026 | Game servers don't show players online | It needs each game's own query protocol; Pterodactyl doesn't either. Kept for later |
| 9 Nov 2026 | A game's disk limit is enforced by the panel stopping it (within a minute) and refusing to start it or take files while over | Docker can't cap a folder's size without special filesystems |
| 9 Nov 2026 | Eggs' config files are filled in for properties, ini, json, yaml and plain files; not xml | Few eggs use xml; it can come later |
| 9 Nov 2026 | An egg's kind (Minecraft, Steam…) is chosen when it's imported | Eggs don't carry their nest; Pterodactyl sets it the same way |
| 9 Nov 2026 | A friend can do everything with a shared game except change its limits, image or ports, delete or share it, or change settings the egg keeps from players. No finer permissions | Simple, and what sharing with a friend needs; Pterodactyl's per-permission subusers can come later |
| 9 Nov 2026 | NPM's own users, audit log and default site settings stay in NPM | They're about running NPM itself, not your sites; Bastion's NPM user shouldn't be able to change them |
| 9 Nov 2026 | Two-factor and passkeys with `go-webauthn` and `pquerna/otp`, not Better Auth | Better Auth is a JavaScript library and the panel is Go; these are its established Go equivalents |
| 9 Nov 2026 | Version 0.4.0, from a `VERSION` file. Agents update from the panel (Update agent, or all from Servers); one that needs it is told by its program's SHA-256, not its version | Asked for; agents fell behind the panel and failed new requests ("unknown file operation unarchive") |
| 9 Nov 2026 | A server added but never joined isn't shown, and is forgotten when its join command expires | Asked for |
