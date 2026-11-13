# Testing list

Almost everything has been tested for real in a lab built for it (below). What's left needs your own
domain, devices or accounts. About 20 minutes, most of which is installing it anyway.

## What's left for you

- [ ] **Install it for real** (docs/install.md): `docker compose up -d --build` in `deploy/`, behind your
      Nginx Proxy Manager with websockets on. Open the setup link from its log and make your account.
- [ ] **A passkey on your own phone or laptop** (Account page), then sign in with it. Passkeys were tested
      with Chrome's virtual authenticator over HTTPS; a real device is the last step.
- [ ] **Join your servers** with the command from Servers, Add a server.
- [ ] **A Let's Encrypt certificate** for a real domain: Proxy, New certificate (or Expose on a stack). It
      needs a domain pointing at your NPM, which the lab couldn't have.
- [ ] **Discord alerts**: Alerts, add Discord with a webhook from your server's settings, Send test.

New in 0.5, worth a minute each:

- [ ] **Back up a stack** (a stack's Backups tab, Back up now), then restore it and check its data came
      back. Tested against real Docker here with a stack and a named volume; your own stacks are the test
      that counts. Try one to S3 too, if you use it.
- [ ] **Players online** on a game's console page while you're playing on it (Minecraft; Steam games
      answer the same way but none was run here).
- [ ] **A friend's permissions**: share a game, untick Console in its settings, and check they can't see it.
- [ ] **Activity** (sidebar): your sign-ins and what you did are listed.
- [ ] **The demo** at https://atrello.github.io/bastion/ once Pages is turned on (Settings, Pages, Source:
      GitHub Actions).

Optional, if you want to:

- [ ] Join the Paper server from Minecraft itself (the lab answered Minecraft's server-list ping).
- [ ] A Steam game (Valheim, from the `pelican-eggs/games-steamcmd` collection). Steam eggs use the same
      installer and console as Paper, but none has been run.

## What was tested in the lab

A panel built from the real Dockerfile, behind Caddy with HTTPS, and a separate Debian server running
systemd, joined with the panel's own one-line command. Browser tests in Chromium.

| Area | Tested |
|---|---|
| Install | The image builds and runs as a non-root user (this found and fixed a bug: the data folder wasn't writable); the setup link; the install script downloads the agent over HTTPS, joins, and starts it with systemd; it starts again after a reboot; a new join command keeps the server's place; replacing the panel's container keeps you signed in and everything in it |
| Sign-in | Setup with a passkey over HTTPS, then signing in with the passkey alone; the session cookie is Secure, HttpOnly and SameSite=Strict; authenticator codes and one-use recovery codes (automated tests) |
| Docker | Stacks found and edited, saved and deployed; live logs and a container console over HTTPS websockets through the reverse proxy; image updates; volumes, images and clean-up |
| Git | A private repository over ssh with the stack's deploy key: refused before the key was added, deployed after; a pushed change noticed and deployed by itself with auto-deploy on |
| Alerts | ntfy (a real message to ntfy.sh, read back from it); email over STARTTLS with a login, over port 465, and a stuck mail server giving up after 30 seconds (this found and fixed two bugs: 465 didn't work, and a stuck server hung the alerts) |
| Nginx Proxy Manager | A real NPM 2.16: every kind of item made, changed, enabled, disabled and deleted; a custom certificate uploaded; Expose on a stack made a proxy host, NPM served the stack's page through it, and the health check counted it as answering |
| Game servers | Pterodactyl's real Paper egg: installed (downloaded Paper 26.3), the EULA accepted from the new prompt (this found and fixed a gap: Paper won't start without it), started, answered Minecraft's server-list ping, a console command shown in game, a restart that warned players first, the real world backed up, deleted, restored and loaded again. Files, schedules, disk limits and sharing with a friend (an invite link, an account with a passkey, and only that game visible) |
