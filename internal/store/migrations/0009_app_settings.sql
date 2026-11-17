-- Settings of the whole app that an admin changes in the web UI, one row per
-- setting (SPEC 9.16). A setting with no row has its default. The username is
-- kept as text, as in audit_log.
CREATE TABLE app_settings (
    name       TEXT    PRIMARY KEY,
    value      TEXT    NOT NULL,
    updated_at INTEGER NOT NULL,
    updated_by TEXT    NOT NULL
);
