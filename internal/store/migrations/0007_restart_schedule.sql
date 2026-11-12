-- The one restart schedule for all of Foreman (SPEC 7.12). It restarts
-- whichever world is active. minute is minutes after midnight in zone, an
-- IANA time zone name. limit_hours is how long to wait for nobody to be
-- online before that day's restart is skipped.
CREATE TABLE restart_schedule (
    id          INTEGER PRIMARY KEY CHECK (id = 1),
    enabled     INTEGER NOT NULL,
    minute      INTEGER NOT NULL CHECK (minute BETWEEN 0 AND 1439),
    zone        TEXT    NOT NULL,
    limit_hours INTEGER NOT NULL CHECK (limit_hours BETWEEN 0 AND 12),
    updated_at  INTEGER NOT NULL,
    updated_by  TEXT    NOT NULL
);
