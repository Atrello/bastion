-- Servers with an agent. A server is added with a one-time join token; the agent swaps it for its own
-- secret. Only hashes of either are kept.
CREATE TABLE servers (
  id              INTEGER PRIMARY KEY,
  name            TEXT NOT NULL UNIQUE COLLATE NOCASE,
  -- The agent's public ID, sent with its secret when it connects.
  agent_id        TEXT UNIQUE,
  secret_hash     TEXT,
  join_token_hash TEXT UNIQUE,
  join_expires_at TEXT,
  -- What the agent said about the server in its last hello.
  hostname        TEXT NOT NULL DEFAULT '',
  os              TEXT NOT NULL DEFAULT '',
  agent_version   TEXT NOT NULL DEFAULT '',
  docker_version  TEXT NOT NULL DEFAULT '',
  last_seen_at    TEXT,
  created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

-- Earlier versions of stacks' compose files, kept each time one is saved from the panel. (.env files
-- are never kept: they hold secrets, and stay on their servers.)
CREATE TABLE compose_versions (
  id        INTEGER PRIMARY KEY,
  server_id INTEGER NOT NULL REFERENCES servers (id) ON DELETE CASCADE,
  stack     TEXT NOT NULL,
  compose   TEXT NOT NULL,
  saved_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
  saved_by  INTEGER REFERENCES users (id) ON DELETE SET NULL,
  note      TEXT NOT NULL DEFAULT ''
);
CREATE INDEX compose_versions_stack ON compose_versions (server_id, stack);
