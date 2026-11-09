-- Places other than the game's own server to keep game backups: S3 buckets (AWS, or any service that
-- speaks S3: Backblaze B2, Cloudflare R2, Wasabi, MinIO...). The secret key is kept to give the agent
-- for each backup; it's never sent to the web app.
CREATE TABLE backup_stores (
  id         INTEGER PRIMARY KEY,
  name       TEXT NOT NULL UNIQUE,
  endpoint   TEXT NOT NULL,              -- host and port, e.g. s3.eu-west-2.amazonaws.com
  region     TEXT NOT NULL DEFAULT '',
  bucket     TEXT NOT NULL,
  prefix     TEXT NOT NULL DEFAULT '',   -- a folder in the bucket, e.g. "bastion/"
  access_key TEXT NOT NULL,
  secret_key TEXT NOT NULL,
  path_style INTEGER NOT NULL DEFAULT 0, -- address the bucket in the path (MinIO and some others need it)
  plain_http INTEGER NOT NULL DEFAULT 0  -- only for a store on the local network
);

-- Where a game's new backups go: NULL for its own server.
ALTER TABLE games ADD COLUMN backup_store_id INTEGER REFERENCES backup_stores (id) ON DELETE SET NULL;

-- Where each backup is: NULL for its game's server. A store with backups in it can't be removed.
ALTER TABLE game_backups ADD COLUMN store_id INTEGER REFERENCES backup_stores (id);
