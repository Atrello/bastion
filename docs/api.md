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
| `GET /servers` | Every server whose agent has joined, with its health and counts. One added but not yet joined isn't listed |
| `POST /servers` | Adds a server: `{"name"}`. Returns it and the one-time command that installs and joins its agent (it works once, for an hour). It's listed once its agent has joined; if it never joins it's forgotten when the command expires, or when a server is added again under the same name |
| `POST /servers/{id}/join-command` | A new join command for the server (a reinstalled server, say). Its agent keeps working until the command is used |
| `POST /servers/{id}/agent/update` | Has the server's agent replace itself with the panel's (by SHA-256) and restart. `agentUpdate` in `GET /servers` says when one is due |
| `DELETE /servers/{id}` | Removes a server. Its agent is disconnected; its stacks keep running |
| `GET /stacks` | Every stack on every server, with `updates`: how many of its containers have a newer image |
| `POST /updates/check` | Asks every server to check its images' registries now (they also check every 6 hours) |
| `GET /containers` | Every container on every server |
| `GET /servers/{id}/stacks/{name}/files` | The stack's compose file and `.env`, as they are on the server (`env` is null when there's no `.env`) |
| `PUT /servers/{id}/stacks/{name}/files` | Saves both, after Docker has checked them. Doesn't deploy. The compose file it replaced is kept in the history |
| `GET /servers/{id}/stacks/{name}/logs?tail=200` | The last lines of every container's logs |
| `GET /servers/{id}/stacks/{name}/history` | Earlier versions of the compose file |
| `POST /servers/{id}/stacks/{name}/actions/{action}` | `up`, `down`, `start`, `stop`, `restart`, `pull`, or `update` (pull and recreate what changed). Returns Docker's output |
| `POST /servers/{id}/containers/{id}/actions/{action}` | `start`, `stop` or `restart` one container |
| `POST /servers/{id}/stacks` | A new stack: `{"name", "compose", "env"}`. Made in `/opt/stacks/<name>`, checked by Docker, and started |
| `GET /servers/{id}/stacks/{name}/git` | The stack's repository, branch, path, deploy key (public half), and the deployed and latest commits; `null` if it doesn't follow one |
| `PUT /servers/{id}/stacks/{name}/git` | `{"repo", "branch", "path", "autoDeploy"}`: follow a compose file in a repository (makes its deploy key the first time) |
| `DELETE /servers/{id}/stacks/{name}/git` | Stop following it; the stack stays as it is |
| `POST /servers/{id}/stacks/{name}/git/deploy` | Copy the compose file from the branch and start the stack |
| `GET /servers/{id}/volumes` | The server's volumes: size, the containers using each, its stack |
| `GET /servers/{id}/images` | The server's images: size, how much is its own, how many containers use it |
| `GET /servers/{id}/cleanup` | What clean-up would remove (images and volumes nothing uses, build cache) and what each frees |
| `POST /servers/{id}/cleanup` | `{"images", "volumes", "buildCache"}`: removes what's asked. Volumes can hold data: they're never removed unless `volumes` is true |

### Alerts

| Method and path | What it does |
|---|---|
| `GET /alerts` | The channels (without their secrets), which kinds are on, and the last 100 alerts |
| `PUT /alerts/kinds` | `{"kinds": [...]}`: any of `offline`, `crash`, `unhealthy`, `disk`, `update`, `host`, `certificate` |
| `POST /alerts/channels` | `{"kind": "ntfy" \| "discord" \| "email", "name", "config"}`: ntfy `url`, `topic`, `token`; Discord `url`; email `host`, `port`, `username`, `password`, `from`, `to` |
| `DELETE /alerts/channels/{id}` | Removes a channel |
| `POST /alerts/channels/{id}/test` | Sends a test message |

The panel checks every 30 seconds. An alert is sent once when it's raised and once when it clears
(update alerts just close).

### Nginx Proxy Manager

Bastion signs in to NPM as one of its users and passes items through to NPM's API as NPM has them, so
every field NPM has comes through. NPMplus (a fork) works too: it signs in with a cookie rather than a
token, which Bastion handles, and its fields differ, so each NPM's own schema (`/api/schema`) is read and a
change is sent only the fields that NPM accepts. Each instance's `flavour` (`npm` or `npmplus`) is listed. `{kind}` is one of `proxy-hosts`, `redirection-hosts`, `streams`,
`dead-hosts`, `access-lists` or `certificates`. Private keys, DNS provider credentials and access list
passwords that NPM sends back are taken out before they reach the browser.

| Method and path | What it does |
|---|---|
| `GET /npm` | Linked NPMs: `id`, `name`, `url`, `email` (never the password) |
| `POST /npm` | `{"name", "url", "email", "password"}`: links one, signing in to check the details first. An https:// NPM with a self-signed certificate answers `409` with its `certificate` (`fingerprint`, `subject`, `expires`); sent again with `trustCertificate` set to that fingerprint, only that certificate is trusted from then on |
| `DELETE /npm/{id}` | Unlinks one. Nothing in NPM changes |
| `GET /npm/summary` | `{"hosts", "up", "problems"}`: proxy hosts checked and answering, and anything wrong (a host not answering, a certificate expiring within 14 days) |
| `GET /npm/{id}/{kind}` | Everything of that kind. Proxy hosts carry `bastion_health`: `{"status", "ms", "error", "checkedAt"}` from the panel's last check |
| `POST /npm/{id}/{kind}` | Makes one, from a JSON object in NPM's own shape (`certificate_id: "new"` asks Let's Encrypt for one) |
| `PUT /npm/{id}/{kind}/{item}` | Replaces one |
| `DELETE /npm/{id}/{kind}/{item}` | Deletes one |
| `POST /npm/{id}/{kind}/{item}/enable` (or `/disable`) | For hosts and streams |
| `POST /npm/{id}/certificates/{item}/renew` | Renews a Let's Encrypt certificate |
| `POST /npm/{id}/certificates/{item}/upload` | `{"certificate", "certificateKey", "intermediate"}` as PEM text, for a certificate made with `provider: "other"` |

The panel checks each enabled proxy host every 5 minutes, and straight after one is changed: a GET to
its first domain, not following redirects. Anything under 500 counts as answering.

### Live streams (websockets)

Only the panel's own pages can open these (the `Origin` must be the panel's address).

| Path | What it does |
|---|---|
| `GET /streams/logs?server=&stack=` (or `&container=`) | Logs following live: the last 200 lines, then each new one. Messages are `{"type": "data", "data": "<line>"}`, then `{"type": "end"}` |
| `GET /streams/console?server=&container=&cols=&rows=` | A shell in the container. Output is `data` (base64); send `{"type": "input", "data": "<base64>"}` and `{"type": "resize", "cols", "rows"}` |

### For agents

Not for browsers: these refuse requests with an `Origin` header. See `docs/protocol.md`.

| Method and path | What it does |
|---|---|
| `POST /agent/enrol` | `{"token", "hostname"}`: swaps a join token for the agent's `{"agentId", "secret"}` |
| `GET /agent/connect` | The agent's websocket, with `Authorization: Bearer <agentId>.<secret>` |

### Eggs

Owner only.

| Method and path | What it does |
|---|---|
| `GET /eggs` | Every egg: `id`, `name`, `category`, `author`, `description`, and how many game servers use it |
| `GET /eggs/{id}` | Its `images` (in the egg's order; the first is the default), `startup` and `variables` |
| `POST /eggs` | `{"json"}` or `{"url"}` (https only, up to 1 MB), with `category`. Checked first. An egg with the same name answers `409` with `existing`, unless `replace` is `true` |
| `DELETE /eggs/{id}` | Only when no game server uses it |

### Game servers

The owner sees every game server. A friend (an account made from an invite link) sees only those shared
with them, and gets `404` for the rest. Friends can use everything below except what's marked "owner".

| Method and path | What it does |
|---|---|
| `GET /games` | The game servers the viewer can see, with state (`starting` until its console shows its egg's "done" line, `running`, `stopping`, `stopped`, `crashed`, `installing`, `install-failed`, `offline`), use and limits |
| `POST /games` | Owner. `{"name", "eggId", "serverId", "image", "memoryMb", "cpuPercent", "diskMb", "ports", "warnCommand"}`. Ports are 1024 to 65535 and can't be another game's on the same server. The egg's installer starts straight away |
| `GET /games/{id}` | One game server |
| `PUT /games/{id}` | Owner. Its name, image, limits, ports and warning command. They apply from its next start |
| `DELETE /games/{id}` | Owner. Removes its container, files and backups |
| `POST /games/{id}/reinstall` | Owner. Runs its egg's installer again |
| `POST /games/{id}/power/{action}` | `start`, `stop`, `restart` or `kill`. Stop and restart take `{"warn": seconds}` (up to 600): the warning command is typed into its console first, then it waits. A game over its disk limit won't start |
| `GET /games/{id}/console` | Its recent output; the install log while it installs and until it first runs |
| `POST /games/{id}/command` | `{"command"}`: one line typed into its console |
| `GET /games/{id}/startup` | The startup command (filled in), image, the egg's images, and its variables. Friends see only the ones the egg lets players see |
| `PUT /games/{id}/startup` | `{"KEY": "value"}`, checked against the egg's rules. Friends can only change the ones the egg lets players change |
| `GET /games/{id}/files?path=/` | A folder's files, folders first |
| `GET` / `PUT /games/{id}/files/content` | Reads (`?path=`) or saves (`{"path", "content"}`) a text file, up to 2 MB |
| `POST /games/{id}/files/folder` | `{"path"}` |
| `POST /games/{id}/files/rename` | `{"from", "to"}`; a path into another folder moves it |
| `DELETE /games/{id}/files?path=` | A file, or a folder and everything in it |
| `POST /games/{id}/files/upload?path=` | The file as the body (not JSON), up to 50 GB, and no more than the room left under the game's disk limit. If it doesn't all arrive (cancelled, or the browser closed), the part that did is removed |
| `POST /games/{id}/files/archive` | `{"path", "paths", "to"}`: packs files and folders in `path` into the `.tar.gz` at `to` |
| `POST /games/{id}/files/unarchive` | `{"path"}`: unpacks a `.zip`, `.tar.gz`, `.tgz` or `.tar` into its folder (files and folders only; up to 50 GB and 200,000 files) |
| `POST /games/{id}/files/move` | `{"path", "paths", "to"}`: moves files and folders in `path` into the folder `to`. Nothing is replaced: a name already taken there stops it |

Archive, unarchive and move can take minutes, so each starts a **job** on the panel and answers `202` with
it at once. A job carries on whatever the browser does; its progress is in `GET /jobs`.

### Jobs

| Route | What it does |
|---|---|
| `GET /jobs` | Your jobs: running, and those ended in the last 10 minutes. Each has `id`, `title`, `detail`, `gameId`, `state` (`running`, `stopping`, `done`, `failed`, `stopped`), `done` and `total` (bytes, or items for a move; `total` is 0 while not known) and `error` |
| `DELETE /jobs/{id}` | Stops a running job (the agent is told to stop), or clears an ended one. Only your own |

Up to 5 jobs each at once (`429` past that); a job runs for 6 hours at most.
| `GET /games/{id}/files/download?path=` | The file, as a download |
| `GET` / `POST /games/{id}/backups` | Its backups; or starts one (`{"name"}`, optional), which shows as `making` until it's `done` or `failed` |
| `PUT /games/{id}/backups/{backup}` | `{"locked"}`: a locked backup can't be deleted |
| `POST /games/{id}/backups/{backup}/restore` | Empties its files and unpacks the backup. It has to be stopped |
| `GET /games/{id}/backups/{backup}/download` | The backup as a `.tar.gz` |
| `GET /games/{id}/history` | Its use over the last ten minutes, for its charts: `[{"t", "cpu", "mem", "netIn", "netOut"}]`, about every 10 seconds while it runs. `netIn` and `netOut` are bytes since it started; the rate is the change between samples |
| `PUT /games/{id}/backup-store` | `{"storeId"}`: where its new backups go (0 for its own server). Owner only. Each backup's `store` says where it is |

### Stack backups (owner only)

A stack's folder and the volumes Docker made for it, in one archive. Under `/servers/{id}/stacks/{name}/backups`:

| Route | What it does |
|---|---|
| `GET` | `{"backups", "settings"}`: its backups (`name`, `size`, `state` making/done/failed, `error`, `store`, `locked`), and `settings` (`when`: cron or "" for none, `keep`: the newest this many, 0 for all, `stop`: stopped while backed up, `storeId`: 0 for the server, `nextRun`, `lastError`) |
| `POST` | `{"name"}` (optional): makes one now, shown as `making` until it's done |
| `PUT /settings` | `{"when", "keep", "stop", "storeId"}` |
| `PUT /{backup}` | `{"locked"}`: a locked backup isn't deleted, nor pruned |
| `DELETE /{backup}` | Deletes it, wherever it's kept |
| `POST /{backup}/restore` | A job: the stack is stopped, its folder and volumes are emptied and filled from the backup (a deleted volume is made again), and it's started again if it was running |
| `GET /{backup}/download` | The archive: `stack/...` and `volumes/<name>/...` |

### Activity (owner only)

| Route | What it does |
|---|---|
| `GET /activity` | `{"entries", "users"}`: who did what, newest first, 100 at a time. `?before=<id>` for older, `?user=`, `?q=` to search. Each entry: `at`, `user`, `action`, `details` (what it was done to), `ok`. Failed sign-ins too. Kept 90 days |

### Health

| Route | What it does |
|---|---|
| `GET /health` | Signed out. `{"ok": true}` while the panel can write its database; `503` and why (full or read-only disk) when it can't. The image's health check asks it |

### Custom notifications (owner only)

| Route | What it does |
|---|---|
| `GET /notifications` | The notifications made |
| `POST /notifications`, `PUT /notifications/{id}` | `{"name", "event", "gameId", "channelId", "title", "message", "linkDays", "enabled"}`. `event` is `backup_done`, `backup_failed`, `game_started`, `game_stopped` or `game_crashed`; `gameId` 0 for any game; `channelId` one of the alert channels. The title and message take `{game}`, `{server}`, `{backup}`, `{size}`, `{error}`, `{time}` and `{link}`. `linkDays` (1 to 30, for `backup_done` only) adds a link that downloads the backup without signing in for that many days |
| `DELETE /notifications/{id}` | Deletes it |
| `POST /notifications/{id}/test` | Sends it now with made-up details (its link doesn't work) |
| `DELETE /games/{id}/backups/{backup}/links` | Stops every download link to that backup |
| `GET /backup-links/{token}` | Signed out: downloads the backup a link is for, while it works. Wrong links count towards the sign-in limits |

A game's state changes are noticed within 30 seconds. A backup's name is the one given (or "Backup")
with the date and time, like `Nightly-2026-11-09-1915`, and its file is named the same.

### Backup storage (owner only)

| Route | What it does |
|---|---|
| `GET /backup-stores` | S3 buckets for game backups, each with how many backups are in it. The secret key is never given |
| `POST /backup-stores` | `{"name", "endpoint", "region", "bucket", "prefix", "accessKey", "secretKey", "pathStyle", "plainHttp"}`: saved only once Bastion has written a small file there and deleted it. Plain http only for a private address |
| `PUT /backup-stores/{id}` | The same; an empty `secretKey` keeps the one it has. With backups in it, its address, bucket and folder can't change |
| `DELETE /backup-stores/{id}` | Forgets it, if it has no backups in it. Nothing in the bucket is touched |
| `DELETE /games/{id}/backups/{backup}` | |
| `GET` / `POST /games/{id}/schedules` | Its schedules; or a new one: `{"name", "when", "action", "payload", "enabled"}`. `when` is cron (minute hour day month weekday, in the panel's time zone); `action` is `restart` (players are warned a minute before), `command` (`payload` is the command) or `backup` |
| `PUT` / `DELETE /games/{id}/schedules/{schedule}` | |
| `POST /games/{id}/schedules/{schedule}/run` | Runs it now |
| `GET /games/{id}/shares` | Owner. Who it's shared with |
| `POST /games/{id}/shares` | Owner. `{"username"}` shares it with a friend who has an account; with no body, gives a one-time invite `link` (a week) for a new friend |
| `PUT /games/{id}/shares/{user}` | Owner. `{"permissions": [...]}`: what the friend may do, from `console`, `power`, `files`, `backups`, `schedules` and `startup`. A share no one has chosen for has all of them. Every route checks; a schedule also needs what it does (`command` needs `console`, `restart` needs `power`, `backup` needs `backups`). A game's `can` says what the viewer may do, and `players` who's playing (`online`, `max`, `names`) when the game can be asked |
| `DELETE /games/{id}/shares/{user}` | Owner. Stops sharing it with them |
| `GET /streams/game?id=` | A websocket: its console output, live |

Every minute the panel also stops any game over its disk limit: Docker can't cap a folder's size.

The shapes of the answers are in `web/src/lib/types.ts`.
