# The panel's API

What the web app asks the panel for. All under `/api`, JSON in and out, signed in with the panel's
session cookie. Errors are `{"error": "..."}` with a fitting status.

Anything that changes something (POST, PUT, DELETE) must come from the panel's own pages: it's refused
without an `Origin` header matching `BASTION_PUBLIC_URL`, or with `Sec-Fetch-Site` saying another site
sent it. Bodies are JSON (`Content-Type: application/json`), at most 64 KB, with no unknown fields.

### Setting up and signing in

| Method and path | What it does |
|---|---|
| `GET /auth/state` | `{"setupNeeded", "signedIn", "username", "passkeys"}`: what the web app shows first. `passkeys` is false when the panel is reached by an IP address |
| `POST /setup` | First-run setup: `{"token", "username", "password"}`, the token from the setup link. The account is saved once its second factor is set up |
| `POST /auth/signin` | `{"username", "password"}`. Gives which second factors the account has: `{"code", "passkey"}` |
| `POST /auth/signin/code` | `{"code"}` from the authenticator app. Each code works once |
| `POST /auth/signin/recovery` | `{"code"}`, a recovery code. Each works once |
| `POST /auth/passkey/begin`, `/auth/passkey/finish` | Signing in with a passkey, on its own or after the password |
| `POST /auth/authenticator/begin` | A new authenticator app: `{"password"}` (empty during setup). Gives `{"secret", "qrCode"}` |
| `POST /auth/authenticator/finish` | `{"code"}`, its first code. Gives `{"recoveryCodes"}`; during setup, saves the account and signs in |
| `POST /auth/passkey/register/begin`, `/register/finish` | Adding a passkey: `{"name", "password"}` |
| `POST /auth/signout` | Signs this browser out |

Wrong passwords and codes are limited: after 5 for an account, or 20 from one address, it waits 15
minutes, then longer.

### Your account

| Method and path | What it does |
|---|---|
| `GET /account` | `{"username", "hasAuthenticator", "recoveryCodesLeft", "passkeys"}` |
| `POST /account/password` | `{"current", "next"}`. Signs out every other browser |
| `POST /account/recovery-codes` | `{"password"}`. Ten new codes, replacing the old ones |
| `DELETE /account/passkeys/{id}` | Removes a passkey, unless it's the only second factor |
| `POST /account/signout-everywhere` | Signs out every other browser |

### Servers, stacks and containers

Lists come from what each agent last reported, so they answer at once whether or not a server is
online. Anything that reads files or changes something is passed to the server's agent; if the agent
isn't connected the answer is 503, and if the server refused (a compose file Docker says is wrong, say)
it's 400 with Docker's own message.

| Method and path | What it does |
|---|---|
| `GET /servers` | Every server, with its health and counts |
| `POST /servers` | Adds a server: `{"name"}`. Returns it and the one-time command that installs and joins its agent (it works once, for an hour) |
| `POST /servers/{id}/join-command` | A new join command for the server (a reinstalled server, say). Its agent's old secret stops working |
| `DELETE /servers/{id}` | Removes a server. Its agent is disconnected; its stacks keep running |
| `GET /stacks` | Every stack on every server |
| `GET /containers` | Every container on every server |
| `GET /servers/{id}/stacks/{name}/files` | The stack's compose file and `.env`, as they are on the server (`env` is null when there's no `.env`) |
| `PUT /servers/{id}/stacks/{name}/files` | Saves both, after Docker has checked them. Doesn't deploy. The compose file it replaced is kept in the history |
| `GET /servers/{id}/stacks/{name}/logs?tail=200` | The last lines of every container's logs |
| `GET /servers/{id}/stacks/{name}/history` | Earlier versions of the compose file |
| `POST /servers/{id}/stacks/{name}/actions/{action}` | `up`, `down`, `start`, `stop`, `restart`, `pull`, or `update` (pull and recreate what changed). Returns Docker's output |
| `POST /servers/{id}/containers/{id}/actions/{action}` | `start`, `stop` or `restart` one container |

### For agents

Not for browsers: these refuse requests with an `Origin` header. See `docs/protocol.md`.

| Method and path | What it does |
|---|---|
| `POST /agent/enrol` | `{"token", "hostname"}`: swaps a join token for the agent's `{"agentId", "secret"}` |
| `GET /agent/connect` | The agent's websocket, with `Authorization: Bearer <agentId>.<secret>` |

### Game servers

| Method and path | What it does |
|---|---|
| `GET /games` | Every game server, with its state, players and memory |
| `POST /games` | A new game server: `{"name", "eggId", "serverId", "memoryMb", "port"}`. The egg's installer runs first |
| `GET /games/{id}` | One game server |
| `POST /games/{id}/power/{action}` | `start`, `stop` (players are warned first), `restart` or `kill` |
| `GET /games/{id}/console` | The console's recent output |
| `POST /games/{id}/command` | Sends a console command: `{"command"}` |
| `GET /games/{id}/files?path=/` | A folder's files |
| `GET /games/{id}/startup` | The startup command, image and the egg's variables with their values |
| `PUT /games/{id}/startup` | Saves variables' values: `{"KEY": "value"}`. They apply from the next restart |
| `GET /games/{id}/schedules` | Its schedules |
| `GET /games/{id}/backups` | Its backups |
| `POST /games/{id}/backups` | Starts a backup |
| `GET /eggs` | The egg library |
| `POST /eggs` | Imports a Pterodactyl egg (its JSON file as exported) |

The shapes of the answers are in `web/src/lib/types.ts`.
