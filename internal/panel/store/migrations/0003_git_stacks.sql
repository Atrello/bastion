-- Stacks that follow a Git repository. Each has its own deploy key, made by the panel; the private half
-- stays here and goes to the stack's agent only when it fetches the repository.
CREATE TABLE git_stacks (
  server_id       INTEGER NOT NULL REFERENCES servers (id) ON DELETE CASCADE,
  stack           TEXT NOT NULL,
  repo            TEXT NOT NULL,
  branch          TEXT NOT NULL,
  path            TEXT NOT NULL,
  auto_deploy     INTEGER NOT NULL DEFAULT 0,
  private_key     TEXT NOT NULL,
  public_key      TEXT NOT NULL,
  -- The commit last deployed, and the branch's commit when it was last checked.
  deployed_commit TEXT NOT NULL DEFAULT '',
  remote_commit   TEXT NOT NULL DEFAULT '',
  checked_at      TEXT,
  error           TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (server_id, stack)
);
