# Security

Bastion can run anything on every server it manages, so it's built on the assumption that someone will
try to break in. This is what it protects, from whom, and how.

## What's at stake

- **The panel** can deploy containers on every server. Whoever controls it controls them all.
- **An agent** runs as root and does what the panel tells it. Whoever can pose as the panel to an agent,
  or as an agent to the panel, matters as much.
- **Stack files** often hold secrets (`.env` files). They stay on their servers; the panel reads and writes
  them but never keeps a copy.

## Who it's protected against

| Threat | How |
|---|---|
| Someone guessing or stealing the password | Two-factor sign-in is required for every account (authenticator app or passkey). Failed sign-ins are limited per account and per address. Passwords hashed with argon2id |
| Phishing | Passkeys can't be phished: they only work on the panel's own address |
| Someone claiming a new panel before its owner | The first account can only be made with a one-time setup link printed in the panel's log |
| A stolen session cookie | Cookies are `HttpOnly`, `Secure` and `SameSite=Strict`; sessions end after idle time; stored only as hashes, so a copy of the database can't be used to sign in |
| Cross-site request forgery | `SameSite=Strict` cookies, and every change must come from the panel's own origin (checked on each request) |
| Cross-site scripting | A strict Content Security Policy (scripts only from the panel itself), no HTML from servers or containers ever rendered as HTML, Svelte escaping everything it shows |
| Clickjacking | The panel can't be put in a frame |
| A fake agent | Each agent signs in with its own secret, issued when it joins; the panel keeps only a hash. Join tokens work once and expire in an hour |
| A fake panel | Agents only connect to the address they joined, over HTTPS (plain HTTP is refused unless the address is a private network and you explicitly allow it) |
| A compromised panel reaching beyond Docker | The agent has a fixed list of operations. It finds stacks by name and only reads or writes their compose file and `.env`; it never runs a shell command the panel sends, apart from consoles into containers |
| A repository address used to run something | Only `https://` and `ssh` addresses are accepted (`GIT_ALLOW_PROTOCOL`), never `file://` or `ext::`; options can't be slipped in; the compose file's path can't leave the checkout or follow a link |
| Deploy keys | One per stack, made by the panel and kept in its database (keep the data folder private); sent to that stack's agent only while it fetches, written to a file only root can read and deleted after |
| Vulnerable dependencies | Few dependencies, each listed in `docs/plan.md`; `make check` runs `govulncheck` and `npm audit` and must pass. The Go toolchain is pinned to a patched release in `go.mod` |
| A friend reaching more than they were given | A friend's account (made from a one-time invite link, with two-factor sign-in) gets only their account and the game servers shared with them: every other route is owner-only by default, and a game not shared with them answers as if it didn't exist. They can't change a game's limits, image or ports, delete or share it, or change the settings its egg keeps from players |
| A game server breaking out | Games run as user 988 (never root), with no new privileges, Pterodactyl's dropped capabilities also dropped, memory, swap, CPU and process limits, and only their own folder mounted. What a game does inside its container is up to the game, as with Pterodactyl |
| An egg used to attack the server | Eggs are imported by hand, by the owner, and checked: image names, variable names, the installer's shell, and config file paths (which can't leave the game's folder). An egg can't add mounts, privileges or host networking: only the agent decides how containers are made |
| A file path escaping a game's folder | Every file and backup operation goes through Go's `os.Root`, which refuses `..`, absolute paths and links leading out. Restoring a backup unpacks only files and folders |
| A request too big, or slow | JSON bodies are capped (64 KB for most, 1 MB for an egg, 2 MB for a file saved from the editor), uploads at 50 GB (and a game's disk limit); headers at 64 KB and must arrive within 10 seconds |
| Flooding sign-in | Sign-in steps in progress are capped, so they can't use up the panel's memory |

## Things to know

- **Passkeys need a name.** WebAuthn won't work when the panel is reached by an IP address (like
  `http://192.168.1.10:8080`). The panel still runs, with authenticator apps for the second factor, and says
  so in its log.
- **Serve it over HTTPS.** Over plain HTTP, cookies can't be marked Secure and anyone on the network can
  read them. `docs/install.md` shows how.
- **Build tools.** `npm audit` reports a low-severity issue in `deepmerge`, used by Svelte's Vite plugin
  while building the web app. It isn't part of the panel that runs, and there's no fixed version yet.

- **NPM's password is kept.** Bastion signs in to NPM again when its token expires, so the NPM user's
  password is in Bastion's database (with deploy keys and alert settings: keep the data folder private).
  Give Bastion its own NPM user rather than yours, so it can be removed on its own.
- **Game servers' ports are open to whoever can reach the server.** Each game's ports are published on all
  of the server's addresses, as players need. Your firewall or router decides who can reach them.
- **An egg link is fetched by the panel.** Importing an egg from a link makes the panel download it (https
  only, up to 1 MB). Only the owner can do this.
- **A self-signed NPM is trusted by its certificate.** When NPM's admin page has a certificate Bastion can't
  check, linking it shows the certificate's fingerprint and asks. Trusted, that exact certificate is pinned:
  any other is refused, so nobody can stand in for NPM unnoticed. Checks are never simply switched off.
- **Proxy host checks don't check certificates.** They ask whether a site answers, not whether to trust
  it, so a self-signed certificate doesn't count as down. Expiry is watched separately.

## What's out of scope

- Someone with root on the panel's host: they have the database and can do what the panel can.
- A container that's been compromised: Bastion runs what you tell it to, like Docker does.

## Reporting a problem

Please report anything you find privately, through GitHub's Security tab on the repository.
