-- A switch in progress, so startup can see one that was cut short (SPEC 7.9,
-- 7.10). One row per world being switched away from: a second switch from the
-- same world is refused, and several active worlds can have one row each
-- later. World ids have no foreign key: worlds live on disk (SPEC 3.4). The
-- username is kept as text, as in audit_log.
CREATE TABLE world_switches (
    from_world TEXT    PRIMARY KEY,
    to_world   TEXT    NOT NULL,
    step       TEXT    NOT NULL CHECK (step IN
        ('begun', 'stopped', 'snapshotted', 'checked', 'presented', 'started')),
    started_at INTEGER NOT NULL,
    by_user    TEXT    NOT NULL
);
