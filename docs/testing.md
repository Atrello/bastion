# Testing list

What couldn't be tested where Bastion was built (no real servers, no systemd, no ssh client, and a proxy
that breaks TLS inside containers), to check by hand. Everything else in `docs/plan.md`'s progress table
has been tested for real. Tick each one off; anything that fails, note what you saw.

## 1. Install (about 20 minutes)

- [ ] `docker compose up -d --build` in `deploy/` builds the image (this couldn't be built here).
- [ ] The panel's log shows the setup link; opening it makes your account with an authenticator app.
- [ ] Behind Nginx Proxy Manager with websockets on, over HTTPS: sign in, sign out, sign in again.
- [ ] Add a passkey on your phone and on your laptop (Account page); sign in with each.
- [ ] Use a recovery code once; it doesn't work a second time.
- [ ] Add a server: the join command installs the agent and starts `bastion-agent` with systemd; the server
      shows as online within seconds, with its existing stacks.
- [ ] A second server the same way. Reboot one: it comes back online on its own.

## 2. Docker (about 15 minutes)

- [ ] A stack you already run: edit its compose file and `.env`, save and deploy; check the change took.
- [ ] Live logs and a console into a container, from a phone too.
- [ ] Check for updates on a stack with an old image; Update images clears it.
- [ ] Volumes and images pages; Clean up removes only what it said it would.
- [ ] Git-backed stack from a **private** repo: add the deploy key to GitHub, link it, deploy from Git.
      Push a change and see "a newer commit" appear (and deploy itself, with auto-deploy on).
- [ ] Alerts: add ntfy (your topic), Discord (a webhook) and email; Send test on each. Stop a container
      that's meant to run and see the alert, then the "Resolved" when it's back.

## 3. Nginx Proxy Manager (about 15 minutes)

- [ ] Proxy: link your NPM (give Bastion its own NPM user). Your existing hosts and certificates appear.
- [ ] Make a proxy host with a new Let's Encrypt certificate (HTTP challenge) for a real domain.
- [ ] One with a DNS challenge (Cloudflare) for a wildcard.
- [ ] On a stack's Containers tab, Expose a container at a new domain; it loads over HTTPS.
- [ ] Edit a host: add a custom location and an access list; both work.
- [ ] Disable and enable a host; the Answering column follows within a few minutes.
- [ ] The overview shows "proxy hosts answering"; stop a site's container and its host shows red (and
      alerts, if host alerts are on).

## 4. Game servers (about 30 minutes)

- [ ] Eggs: import Paper by link
      (`https://raw.githubusercontent.com/pterodactyl/panel/1.0-develop/database/Seeders/eggs/minecraft/egg-paper.json`),
      then import it again: it asks before replacing.
- [ ] New game server from Paper, 4096 MB, port 25565. The console shows the installer downloading Paper,
      then it can be started.
- [ ] Join it from Minecraft. Type `say hi` in the console; it shows in the game.
- [ ] Restart with a 30-second warning: players see "Server restarting in 30 seconds".
- [ ] Files: edit `server.properties`, upload a datapack, download the world's `level.dat`.
- [ ] Backups: back up, break something, stop it, restore, start: it's as it was. Download a backup.
- [ ] Schedules: a restart at a time a few minutes away; it runs, with the warning.
- [ ] Valheim from a Steam egg (from the `pelican-eggs/games-steamcmd` collection), join it from the game.
- [ ] Sharing: make an invite link, open it in a private window, make a friend's account (with two-factor).
      They see only that game server and the Game servers menu; they can start, stop and use the console
      and files, but not delete it or change its limits.
- [ ] Set a game's disk limit below what it uses: it's stopped within a minute and won't start.

## 5. Updating

- [ ] `git pull && docker compose up -d --build` in `deploy/`: you stay signed in, everything's still there.
- [ ] A new join command on a server updates its agent and keeps its stacks and games.
