-- Custom notifications: when something happens (a backup made, a game crashing...), for any game or one,
-- send a message in your own words to one of the alert channels.
CREATE TABLE notification_rules (
  id         INTEGER PRIMARY KEY,
  name       TEXT NOT NULL,
  event      TEXT NOT NULL CHECK (event IN ('backup_done', 'backup_failed', 'game_started', 'game_stopped', 'game_crashed')),
  game_id    INTEGER REFERENCES games (id) ON DELETE CASCADE, -- NULL for any game
  channel_id INTEGER NOT NULL REFERENCES alert_channels (id) ON DELETE CASCADE,
  title      TEXT NOT NULL,
  message    TEXT NOT NULL,
  -- For a backup made: 0, or how many days a download link in the message works.
  link_days  INTEGER NOT NULL DEFAULT 0,
  enabled    INTEGER NOT NULL DEFAULT 1
);

-- Links that download a backup without signing in, sent in notifications. Only a hash of each is kept;
-- one stops working when it expires, is revoked, or its backup is deleted.
CREATE TABLE backup_links (
  id         INTEGER PRIMARY KEY,
  backup_id  INTEGER NOT NULL REFERENCES game_backups (id) ON DELETE CASCADE,
  token_hash TEXT NOT NULL UNIQUE,
  expires_at TEXT NOT NULL
);
