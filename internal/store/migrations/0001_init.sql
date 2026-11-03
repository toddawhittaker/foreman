CREATE TABLE users (
    id            INTEGER PRIMARY KEY,
    username      TEXT    NOT NULL UNIQUE COLLATE NOCASE,
    password_hash TEXT    NOT NULL DEFAULT '',
    role          TEXT    NOT NULL DEFAULT 'admin' CHECK (role IN ('admin')),
    created_at    INTEGER NOT NULL
);

CREATE TABLE sessions (
    token_hash   TEXT    PRIMARY KEY,
    user_id      INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at   INTEGER NOT NULL,
    last_seen_at INTEGER NOT NULL
);

CREATE INDEX sessions_last_seen_at ON sessions (last_seen_at);
CREATE INDEX sessions_user_id ON sessions (user_id);
