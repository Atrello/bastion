# The panel's API

What the web app asks the panel for. All under `/api`, JSON in and out, signed in with the panel's
session cookie. Errors are `{"error": "..."}` with a fitting status.

| Method and path | What it does |
|---|---|
| `GET /servers` | Every server, with its health and counts |
| `POST /servers` | Adds a server: `{"name"}`. Returns it and the one-time command that installs and joins its agent |
| `GET /stacks` | Every stack on every server |
| `GET /containers` | Every container on every server |
| `GET /servers/{id}/stacks/{name}/files` | The stack's compose file and `.env`, as they are on the server (`env` is null when there's no `.env`) |
| `PUT /servers/{id}/stacks/{name}/files` | Saves both, after Docker has checked them. Doesn't deploy. The compose file it replaced is kept in the history |
| `GET /servers/{id}/stacks/{name}/logs?tail=200` | The last lines of every container's logs |
| `GET /servers/{id}/stacks/{name}/history` | Earlier versions of the compose file |
| `POST /servers/{id}/stacks/{name}/actions/{action}` | `up`, `down`, `start`, `stop`, `restart`, `pull`, or `update` (pull and recreate what changed). Returns Docker's output |

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

The shapes of the answers are in `panel/web/src/lib/types.ts`.
