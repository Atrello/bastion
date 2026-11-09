# Roadmap

What Bastion will do, in the order it's built. Each step is usable on its own.

## 0.1: Docker stacks

- **Agent**: one Go binary per server, joined with a one-time command, connecting out to the panel (no open
  ports). Reports CPU, memory, disk and uptime.
- **Servers**: their health at a glance, and adding one.
- **Stacks**: every compose project on every server, found and adopted as they are, and new ones in
  `/opt/stacks`.
- **A stack's page**: edit the compose file (checked by Docker before saving) and the `.env` file (which
  stays on the server: Bastion reads and writes it but keeps no copy); save and deploy; start, stop,
  restart; pull newer images and recreate; logs; earlier versions of the compose file.
- **Containers**: every container on every server, with its stack, image, status and ports.
- **Sign-in** for the panel.

## 0.2: Nginx Proxy Manager

Manage an existing Nginx Proxy Manager through its API: proxy hosts, redirects, streams, 404 hosts, access
lists, custom locations, advanced config and certificates (Let's Encrypt by HTTP or DNS). "Expose this"
from a stack, health checks on each host, and certificate expiry warnings.

## 0.3: Game servers

Pterodactyl-style game servers, from Pterodactyl's own egg files, run as containers on any server with
the agent: a live console, startup variables, resource limits, a web file manager, schedules (restarts,
commands, backups) and sharing one game server with a friend.

## Later

- Backups of volumes and game worlds, with schedules and restore.
- Alerts (server offline, container down, disk filling, certificate expiring) to Discord, ntfy or email.
- An activity log of who did what.

## Not planned

- App templates.
- SFTP for game servers (the web file manager covers it).
