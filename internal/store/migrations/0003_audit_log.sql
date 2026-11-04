-- Who did what and when (SPEC 12.9). The username is kept so a row still
-- says who acted after the user is deleted. world_id has no foreign key:
-- worlds live on disk, not in this database. The table never holds a secret
-- (SPEC 14.8); callers redact before inserting.
CREATE TABLE audit_log (
    id       INTEGER PRIMARY KEY,
    time     INTEGER NOT NULL,
    user_id  INTEGER REFERENCES users (id) ON DELETE SET NULL,
    username TEXT    NOT NULL,
    action   TEXT    NOT NULL,
    world_id TEXT,
    detail   TEXT    NOT NULL DEFAULT ''
);

CREATE INDEX audit_log_time ON audit_log (time, id);
