-- Webhooks Foreman sends its events to (SPEC 8.10 to 8.12). The address is
-- sealed with FOREMAN_SECRET_KEY before it reaches this table (SPEC 14.8),
-- with "webhook-<id>" as its authenticated data. preset and host are what the
-- page shows. events_off lists the events turned off, comma separated, so an
-- event added later is on by default. Usernames are kept as text, as in
-- audit_log.
CREATE TABLE webhooks (
    id         INTEGER PRIMARY KEY,
    preset     TEXT    NOT NULL,
    host       TEXT    NOT NULL,
    url        BLOB    NOT NULL,
    events_off TEXT    NOT NULL DEFAULT '',
    lang       TEXT    NOT NULL,
    created_at INTEGER NOT NULL,
    created_by TEXT    NOT NULL,
    updated_at INTEGER NOT NULL,
    updated_by TEXT    NOT NULL
);
