-- Nginx Proxy Manager instances the panel manages through their API, signing in as one of their users.
CREATE TABLE npm_instances (
  id         INTEGER PRIMARY KEY,
  name       TEXT NOT NULL,
  url        TEXT NOT NULL,
  email      TEXT NOT NULL,
  -- Kept to sign in again when NPM's token expires. Never sent back to the browser.
  password   TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

-- Proxy host and certificate alerts are on by default, like the others.
UPDATE settings SET value = '["offline","crash","unhealthy","disk","update","host","certificate"]'
 WHERE key = 'alert_kinds' AND value = '["offline","crash","unhealthy","disk","update"]';
