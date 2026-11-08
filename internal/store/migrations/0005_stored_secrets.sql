-- Secrets entered in the UI, sealed with FOREMAN_SECRET_KEY before they reach
-- this table (SPEC 9.11, 14.8). The name is also the authenticated data of the
-- sealed value. The username is kept as text, as in audit_log.
CREATE TABLE stored_secrets (
    name       TEXT    PRIMARY KEY,
    value      BLOB    NOT NULL,
    updated_at INTEGER NOT NULL,
    updated_by TEXT    NOT NULL
);
