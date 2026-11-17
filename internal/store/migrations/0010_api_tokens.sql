CREATE TABLE api_tokens (
    id           INTEGER PRIMARY KEY,
    user_id      INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name         TEXT    NOT NULL COLLATE NOCASE,
    scope        TEXT    NOT NULL CHECK (scope IN ('read', 'operate', 'admin')),
    secret_hash  TEXT    NOT NULL UNIQUE,
    created_at   INTEGER NOT NULL,
    period_start INTEGER NOT NULL,
    revoked_at   INTEGER,
    last_used_at INTEGER,
    reminded     INTEGER NOT NULL DEFAULT 0 CHECK (reminded IN (0, 1, 2))
);
CREATE UNIQUE INDEX api_tokens_name ON api_tokens (user_id, name)
    WHERE revoked_at IS NULL;
CREATE INDEX api_tokens_user_id ON api_tokens (user_id);
ALTER TABLE audit_log ADD COLUMN token_name TEXT NOT NULL DEFAULT '';
-- token-expiring is opt-in (8.10): turn it off on existing webhooks.
UPDATE webhooks SET events_off = CASE WHEN events_off = ''
    THEN 'token-expiring' ELSE events_off || ',token-expiring' END;
