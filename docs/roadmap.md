# Roadmap

What Bastion does, and what's next. Each step was usable on its own.

## Built

- **0.1, Docker stacks:** servers with an agent each (connecting out, no open ports), every compose stack
  found as it is, editing the compose file and `.env`, deploying, image updates, logs, consoles, history,
  Git-backed stacks, volumes, images and clean-up, alerts (ntfy, Discord, email). Two-factor sign-in with
  passkeys.
- **0.2, Nginx Proxy Manager (and NPMplus):** everything NPM's own screens do, "Expose this" from a stack,
  health checks on each host and certificate expiry warnings.
- **0.3, Game servers:** Pterodactyl eggs, a live console, file manager (archive, unpack, move), startup
  settings, schedules, backups, sharing with friends.
- **0.4:** agents updated from the panel, S3 backup storage, custom notifications with download links,
  jobs with progress in the corner of every page, game pages with figures and charts.
- **0.5:** an activity log of who did what; friends' permissions per game (console, power, files, backups,
  schedules, startup); players online (Minecraft and Steam games); backups of stacks (their folder and
  volumes, scheduled, kept to the newest few, on the server or S3); a health check and plain words when
  the panel's disk fills; a demo on GitHub Pages.

## Later

Ideas, not promises:

- Restoring a stack backup to another server (moving a stack).
- Player counts for Bedrock Edition and games with their own query protocols.
- Users besides the owner and friends (a second admin).

## Not planned

- App templates.
- SFTP for game servers (the web file manager covers it).
