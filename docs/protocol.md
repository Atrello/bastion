# The agent protocol

How the panel and each server's agent talk. The types are in `internal/protocol`.

## Joining

1. In the panel, **Add a server** makes a one-time **join token** (it works once, for an hour; the panel keeps
   only its hash) and shows the command that installs the agent with it.
2. The agent sends it to `POST /api/agent/enrol` with the server's hostname, and gets back its own **agent ID**
   and **secret**. It saves them in `/etc/bastion/agent.json` (only root can read it). The panel keeps only a
   hash of the secret.

## Connecting

The agent opens a websocket to `wss://<panel>/api/agent/connect`, with `Authorization: Bearer <id>.<secret>`.
It connects **out**, so the server needs no open ports, and it reconnects by itself if the connection
drops, waiting a little longer each time (up to a minute).

The agent only talks to the address it joined. It refuses plain `http://` unless the address is on a
private network (`10.*`, `172.16–31.*`, `192.168.*`, `localhost`) or it was joined with `--insecure`.

A connection that sends nothing for 60 seconds is closed; both sides ping every 25.

## Messages

JSON, one per websocket message. Which fields are set says what a message is:

| Message | Direction | Fields |
|---|---|---|
| Request | panel → agent | `id`, `op`, `args` |
| Answer | agent → panel | `id`, `ok`, and `data` or `error` |
| Stream part | agent → panel | `id`, `stream` (`data` or `end`), `data` |
| Stream input | panel → agent | `id`, `op` (`stream.input`, `stream.resize`, `stream.stop`), `args` |
| Event | agent → panel | `event`, `data` |

### Events

| Event | When | Data |
|---|---|---|
| `hello` | On connecting | The protocol version, the agent's version and build (the SHA-256 of its program), hostname, OS, CPUs, Docker's version |
| `state` | On connecting, whenever Docker reports a change (a little after, so a burst is one message), and every minute | Every stack and container on the server |
| `metrics` | Every 10 seconds, and straight after any game request | CPU, memory, disk and uptime, and each game server's state and use |

The panel keeps the latest `state` and `metrics` from each agent, so its pages never wait on the servers.

### Requests

The agent does these and nothing else.

| `op` | Args | Answer |
|---|---|---|
| `stack.files` | `name` | The stack's compose file and `.env` (or `null`) |
| `stack.save` | `name`, `compose`, `env` | Checked with `docker compose config` first, then written in one step |
| `stack.create` | `name`, `compose`, `env` | Makes `/opt/stacks/<name>` and starts it |
| `stack.action` | `name`, `action` | `up`, `down`, `start`, `stop`, `restart`, `pull`, `update`: Docker's output |
| `stack.logs` | `stack` or `container`, `tail` | The last lines |
| `container.action` | `id`, `action` | `start`, `stop`, `restart` |
| `volumes.list`, `images.list` | | From `docker system df -v`: sizes, and what uses each |
| `cleanup.preview` | | What clean-up would remove, and what it frees |
| `cleanup.run` | `images`, `volumes`, `buildCache` | `docker image prune --all`, `volume prune --all`, `builder prune`, as asked |
| `git.head` | `repo`, `branch`, `path`, `key` | The branch's commit now (`git ls-remote`) |
| `git.sync` | `stack`, `repo`, `branch`, `path`, `key` | Fetches the branch into `/var/lib/bastion/git/<stack>` and copies the compose file into the stack (checked by Docker first). Only `https` and `ssh` repositories; the path can't leave the checkout; `.env` is never touched |
| `updates.check` | | Checks the images' registries now (otherwise every 6 hours); the results come in the next `state` |
| `agent.update` | `build`: the SHA-256 of the panel's agent | Downloads `/downloads/bastion-agent-linux-<arch>` from the panel it joined, puts it in place only if its SHA-256 is `build`, answers, then exits for systemd to restart it |
| `logs.follow` | `stack` or `container`, `tail` | A stream of lines, until `stream.stop` |
| `exec` | `container`, `cols`, `rows` | A terminal in the container (its shell), as a stream; keys arrive as `stream.input` |
| `game.install` | A game's spec (below) | Starts its egg's installer in the background; its state is `installing`, then `stopped` or `install-failed` |
| `game.start` | A game's spec | Fills in its config files, then makes its container afresh and starts it |
| `game.power` | `id`, `action`, `command`, `stopWith` | `command` types a line into its console; `stop` stops it the egg's way (a command, or `^C`), then with Docker after 30 seconds; `kill` |
| `game.delete` | `id` | Removes its container, files, backups and install log |
| `game.files` | `id`, `op`, `path`, `to`, `paths`, `data`, `offset` | `list`, `read`, `write`, `append`, `download`, `mkdir`, `rename`, `delete`, `move`, `archive`, `unarchive`, `install-log`. Pieces of up to 2 MB |
| `game.backup` | `id`, `op`, `name`, `offset` | `make`, `restore`, `delete`, `download` |

Stacks are found by name among those Docker knows and the folders in `/opt/stacks`; only their compose
file and `.env` are ever read or written. Names are checked (`[a-z0-9][a-z0-9_-]*`), so a name can't reach
anywhere else on the server.

## Game servers

A game's spec is everything worked out from its egg by the panel: the image, the startup command with its
`{{VARIABLES}}` filled in, the egg's variables as environment variables, memory, CPU and disk limits, its
ports, the config file settings to fill in, and the egg's installer (image, shell and script). The agent
checks it again (image names, variable names, ports, the installer's shell) before using any of it.

- Files are in `/var/lib/bastion/games/<id>`, seen in the container as `/home/container`. Every file
  operation goes through Go's `os.Root` on that folder, so no path or link can reach outside it.
- The installer runs as Pterodactyl's do: the script in `/mnt/install`, the game's files at `/mnt/server`.
- The game runs as user 988 (not root), with no new privileges, the capabilities Pterodactyl also drops
  taken away, memory and swap capped, at most 1024 processes, and each port published for TCP and UDP.
- Its console's input is a `docker attach` kept open; its output is its container's log.
- Backups are `.tar.gz` files in `/var/lib/bastion/backups/<id>`, of plain files and folders only.

## Image updates

For each container's image, the agent asks its registry which digest the tag points at now (a `HEAD`
request for the manifest, with an anonymous token if the registry wants one; Docker Hub doesn't count
these against pull limits) and compares it with the image the server has. Images pinned by digest never
update, and images on registries that need signing in are skipped. The result is `update` on each
container and `updates` on each stack in `state`.

## Versions

`hello` carries the protocol version. The panel refuses an agent whose protocol is older than it supports
and shows the server as needing an update.
