-- People who can sign in to the panel, and how. Two-factor sign-in is required, so an account is only
-- usable once it has an authenticator app (totp_secret) or a passkey.
CREATE TABLE users (
  id            INTEGER PRIMARY KEY,
  username      TEXT NOT NULL UNIQUE COLLATE NOCASE,
  password_hash TEXT NOT NULL,
  -- The authenticator app's shared secret, set once its first code has been checked.
  totp_secret   TEXT,
  -- The 30-second step of the last code used, so a code can't be used twice.
  totp_last_step INTEGER NOT NULL DEFAULT 0,
  -- WebAuthn's handle for this user: random, so it says nothing about them.
  webauthn_id   BLOB NOT NULL UNIQUE,
  created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

-- Single-use codes for when the authenticator app isn't to hand. Only hashes are kept.
CREATE TABLE recovery_codes (
  id        INTEGER PRIMARY KEY,
  user_id   INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  code_hash TEXT NOT NULL,
  used_at   TEXT
);

CREATE TABLE passkeys (
  id            INTEGER PRIMARY KEY,
  user_id       INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  name          TEXT NOT NULL,
  -- go-webauthn's Credential, as JSON: its ID, public key, sign count and flags.
  credential    TEXT NOT NULL,
  credential_id BLOB NOT NULL UNIQUE,
  created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
  last_used_at  TEXT
);

-- Signed-in browsers. Only a hash of the cookie's token is kept, so a copy of the database can't sign in.
CREATE TABLE sessions (
  token_hash   TEXT PRIMARY KEY,
  user_id      INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  created_at   TEXT NOT NULL,
  last_seen_at TEXT NOT NULL,
  user_agent   TEXT NOT NULL DEFAULT ''
);
CREATE INDEX sessions_user ON sessions (user_id);

-- Small settings the panel keeps, like the one-time setup token.
CREATE TABLE settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
