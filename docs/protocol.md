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
| `hello` | On connecting | The protocol version, the agent's version, hostname, OS, CPUs, Docker's version |
| `state` | On connecting, whenever Docker reports a change (a little after, so a burst is one message), and every minute | Every stack and container on the server |
| `metrics` | Every 10 seconds | CPU, memory, disk and uptime |

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
| `updates.check` | | Checks the images' registries now (otherwise every 6 hours); the results come in the next `state` |
| `logs.follow` | `stack` or `container`, `tail` | A stream of lines, until `stream.stop` |
| `exec` | `container`, `cols`, `rows` | A terminal in the container (its shell), as a stream; keys arrive as `stream.input` |

Stacks are found by name among those Docker knows and the folders in `/opt/stacks`; only their compose
file and `.env` are ever read or written. Names are checked (`[a-z0-9][a-z0-9_-]*`), so a name can't reach
anywhere else on the server.

## Image updates

For each container's image, the agent asks its registry which digest the tag points at now (a `HEAD`
request for the manifest, with an anonymous token if the registry wants one; Docker Hub doesn't count
these against pull limits) and compares it with the image the server has. Images pinned by digest never
update, and images on registries that need signing in are skipped. The result is `update` on each
container and `updates` on each stack in `state`.

## Versions

`hello` carries the protocol version. The panel refuses an agent whose protocol is older than it supports
and shows the server as needing an update.
