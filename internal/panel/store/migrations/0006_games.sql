-- Game servers, run from eggs (Pterodactyl's game definitions), and the friends they're shared with.

-- owner: the whole panel. friend: only the game servers shared with them.
ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'owner';

CREATE TABLE eggs (
  id          INTEGER PRIMARY KEY,
  name        TEXT NOT NULL UNIQUE COLLATE NOCASE,
  category    TEXT NOT NULL DEFAULT 'Other',
  author      TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  -- The egg's JSON as it was imported, so it can be exported or looked at again.
  egg         TEXT NOT NULL,
  imported_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE TABLE games (
  id           INTEGER PRIMARY KEY,
  name         TEXT NOT NULL,
  egg_id       INTEGER NOT NULL REFERENCES eggs (id),
  server_id    INTEGER NOT NULL REFERENCES servers (id) ON DELETE CASCADE,
  image        TEXT NOT NULL,
  memory_mb    INTEGER NOT NULL,
  cpu_percent  INTEGER NOT NULL DEFAULT 0,
  disk_mb      INTEGER NOT NULL DEFAULT 0,
  -- The ports it's given, as a JSON array; the first is the game's own.
  ports        TEXT NOT NULL,
  -- Its startup variables' values, as a JSON object.
  variables    TEXT NOT NULL DEFAULT '{}',
  -- Typed into its console before a stop, with %s for the message (e.g. "say %s"); empty for no warning.
  warn_command TEXT NOT NULL DEFAULT '',
  created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE TABLE game_shares (
  game_id INTEGER NOT NULL REFERENCES games (id) ON DELETE CASCADE,
  user_id INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  PRIMARY KEY (game_id, user_id)
);

-- One-time links to share a game with a friend: they make an account with it. Only hashes are kept.
CREATE TABLE game_invites (
  token_hash TEXT PRIMARY KEY,
  game_id    INTEGER NOT NULL REFERENCES games (id) ON DELETE CASCADE,
  expires_at TEXT NOT NULL
);

CREATE TABLE game_schedules (
  id          INTEGER PRIMARY KEY,
  game_id     INTEGER NOT NULL REFERENCES games (id) ON DELETE CASCADE,
  name        TEXT NOT NULL,
  cron        TEXT NOT NULL,          -- five fields, in the panel's time zone
  action      TEXT NOT NULL,          -- restart, command or backup
  payload     TEXT NOT NULL DEFAULT '', -- the command, for command
  enabled     INTEGER NOT NULL DEFAULT 1,
  last_run_at TEXT,
  last_error  TEXT NOT NULL DEFAULT ''
);

CREATE TABLE game_backups (
  id         INTEGER PRIMARY KEY,
  game_id    INTEGER NOT NULL REFERENCES games (id) ON DELETE CASCADE,
  file       TEXT NOT NULL,          -- its file name on the server
  name       TEXT NOT NULL,
  size       INTEGER NOT NULL DEFAULT 0,
  locked     INTEGER NOT NULL DEFAULT 0, -- a locked backup can't be deleted
  state      TEXT NOT NULL DEFAULT 'making', -- making, done or failed
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
