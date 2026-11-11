-- The activity log: who did what, and whether it worked. Written from the panel's own log lines that name
-- a user (internal/panel/api/activity.go), so the two never differ. Kept for 90 days.
CREATE TABLE activity (
  id      INTEGER PRIMARY KEY,
  at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
  user    TEXT NOT NULL,
  action  TEXT NOT NULL,              -- "game server started"
  details TEXT NOT NULL DEFAULT '{}', -- what it was done to, as a JSON object: {"game": "Survival SMP"}
  ok      INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX activity_at ON activity (at);
