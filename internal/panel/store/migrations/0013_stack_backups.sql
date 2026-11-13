-- Backups of compose stacks: the stack's folder and the volumes it made, in one archive (see the agent's
-- stackbackups.go). Kept on the server or in backup storage, like game backups.
CREATE TABLE stack_backups (
  id         INTEGER PRIMARY KEY,
  server_id  INTEGER NOT NULL REFERENCES servers (id) ON DELETE CASCADE,
  stack      TEXT NOT NULL,
  file       TEXT NOT NULL,
  name       TEXT NOT NULL,
  size       INTEGER NOT NULL DEFAULT 0,
  locked     INTEGER NOT NULL DEFAULT 0,
  state      TEXT NOT NULL DEFAULT 'making', -- making, done or failed
  store_id   INTEGER REFERENCES backup_stores (id),
  error      TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX stack_backups_stack ON stack_backups (server_id, stack);

-- How each stack is backed up: on a schedule (cron; empty for none), keeping the last few, stopped or not.
CREATE TABLE stack_backup_settings (
  server_id   INTEGER NOT NULL REFERENCES servers (id) ON DELETE CASCADE,
  stack       TEXT NOT NULL,
  cron        TEXT NOT NULL DEFAULT '',
  keep        INTEGER NOT NULL DEFAULT 7,  -- the newest this many (unlocked) are kept; 0 keeps all
  stop        INTEGER NOT NULL DEFAULT 1,  -- stopped while backed up, so databases are copied whole
  store_id    INTEGER REFERENCES backup_stores (id) ON DELETE SET NULL,
  last_run_at TEXT,
  last_error  TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (server_id, stack)
);

-- A game's backup schedule keeps only the newest this many (unlocked) backups; 0 keeps all.
ALTER TABLE game_schedules ADD COLUMN keep INTEGER NOT NULL DEFAULT 0;
