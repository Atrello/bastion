# Installing Bastion

Bastion is two programs: the **panel** (one, wherever suits you) and an **agent** on each server with
Docker. The agents connect out to the panel, so servers need no open ports.

## 1. The panel

The panel runs in Docker, behind the reverse proxy you already use for HTTPS (Nginx Proxy Manager,
Caddy, Traefik). HTTPS matters: sign-in cookies and passkeys need it, and agents won't talk to a panel
over plain `http://` on a public address.

```sh
git clone https://github.com/Atrello/bastion.git
cd bastion/deploy
# Set BASTION_PUBLIC_URL in compose.yaml to the address you'll reach it at, then:
docker compose up -d
docker compose logs bastion      # shows the one-time setup link
```

In your reverse proxy, point the panel's address (e.g. `bastion.home.lab`) at port 8080 on that host, with
**websockets turned on** (live logs, consoles and the agents use them).

Open the setup link from the log, make your account, and set up its second factor (an authenticator app or
a passkey). Keep the recovery codes somewhere safe.

### Settings

All are environment variables; only the first is required.

| Setting | Default | What it is |
|---|---|---|
| `BASTION_PUBLIC_URL` | | The address the panel is reached at, e.g. `https://bastion.home.lab`. Passkeys need a name, not an IP address |
| `BASTION_TRUST_PROXY` | `false` | `true` behind a reverse proxy, so sign-in limits see the visitor's address |
| `BASTION_SESSION_IDLE` | `12h` | Sign-ins end after this long without use |
| `BASTION_ADDR` | `:8080` | Where the panel listens |
| `BASTION_DATA_DIR` | `/data` in the image | Where the database lives. Back this folder up; keep it private (it holds deploy keys and alert settings) |
| `BASTION_AGENT_BINARIES` | `/agents` in the image | Where the agent's programs are, for new servers to download |

## 2. Each server

The server needs Docker with its compose plugin (and `git`, for Git-backed stacks).

1. In the panel, **Servers**, **Add a server**, and give it a name.
2. Run the command it shows on the server, as root. It downloads the agent from your panel, joins it, and
   starts it as a systemd service (`bastion-agent`). The command works once, for an hour.

The server shows as online a few seconds later, with its stacks: everything Docker Compose already runs is
found as it is, and new stacks go in `/opt/stacks/<name>`.

To join a server again (a reinstall), open it from **Servers**, use **New join command**, and run that. Its
agent keeps working until the new command is run. **Remove** on the same page forgets a server (delete its
game servers first); what it runs keeps running.

Game servers' files are kept in `/var/lib/bastion/games` and their backups in `/var/lib/bastion/backups`,
on the server each runs on. Players reach a game on its ports, which your firewall or router needs to let
through.

## Updating

- **The panel:** `git pull && docker compose up -d --build` in `deploy`.
- **An agent:** after the panel is updated, a server whose agent is older shows **Update agent** on its
  page (and the Servers page offers to update them all). The agent downloads the panel's copy of itself,
  checks it's exactly that program (its SHA-256), swaps it in and restarts; it's back in a few seconds and
  everything it runs keeps running. Agents from before 0.4.0 can't do this: update one of those once by
  running a new join command on it (from its page), which replaces the program and keeps the server's place.

The version is in the `VERSION` file, shown at the foot of the menu and for each agent. Bastion tells an
agent that needs updating by its program's fingerprint, not its version, so it notices any change.

## Removing an agent

```sh
systemctl disable --now bastion-agent
rm /usr/local/bin/bastion-agent /etc/systemd/system/bastion-agent.service
rm -r /etc/bastion /var/lib/bastion
```

Then remove the server in the panel. Its stacks keep running; Bastion never stops anything it didn't
start. Delete its game servers in the panel first if you want them gone (their containers are
`bastion-<server>-<game>`, labelled `bastion.game=<id>`; their files in `/var/lib/bastion/games`).

## Building it yourself

```sh
make            # the web app, the panel and the agent, into bin/
make agents     # the agent for amd64 and arm64, for the panel to hand out
make image      # the panel's container image
```

See [development.md](development.md).
