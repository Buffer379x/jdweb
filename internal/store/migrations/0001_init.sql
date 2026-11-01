-- JDWeb schema, version 1. JDWeb stores no download lists, only the account,
-- sessions and settings.

CREATE TABLE app_user (
  id              INTEGER PRIMARY KEY CHECK (id = 1),   -- exactly one account
  username        TEXT NOT NULL,
  password_hash   TEXT NOT NULL,                        -- Argon2id, PHC format
  totp_secret     TEXT,                                 -- NULL = two-factor off
  created_at      INTEGER NOT NULL,
  updated_at      INTEGER NOT NULL
);

CREATE TABLE recovery_code (
  code_hash       TEXT PRIMARY KEY,                     -- SHA-256, never the code
  used_at         INTEGER
);

CREATE TABLE session (
  token_hash      TEXT PRIMARY KEY,                     -- SHA-256, never the token
  created_at      INTEGER NOT NULL,
  last_seen_at    INTEGER NOT NULL,
  expires_at      INTEGER NOT NULL,
  user_agent      TEXT
);

CREATE TABLE setting (
  key             TEXT PRIMARY KEY,                     -- jd.url, ui.defaults, ...
  value           TEXT NOT NULL                         -- JSON
);
