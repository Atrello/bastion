-- Where alerts go (ntfy, Discord, email), which kinds are on, and the alerts raised. An alert is open
-- until what caused it clears; each is sent once when raised and once when it clears.
CREATE TABLE alert_channels (
  id         INTEGER PRIMARY KEY,
  kind       TEXT NOT NULL CHECK (kind IN ('ntfy', 'discord', 'email')),
  name       TEXT NOT NULL,
  -- Its settings as JSON: an address, a topic, a token or an SMTP password (never sent back to the browser).
  config     TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE TABLE alerts (
  id         INTEGER PRIMARY KEY,
  -- What it's about, e.g. "offline:3" or "unhealthy:3:immich_server", so it's raised once.
  key        TEXT NOT NULL,
  kind       TEXT NOT NULL,
  title      TEXT NOT NULL,
  detail     TEXT NOT NULL DEFAULT '',
  raised_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
  cleared_at TEXT
);
CREATE INDEX alerts_open ON alerts (key) WHERE cleared_at IS NULL;

INSERT INTO settings (key, value) VALUES ('alert_kinds', '["offline","crash","unhealthy","disk","update"]');
