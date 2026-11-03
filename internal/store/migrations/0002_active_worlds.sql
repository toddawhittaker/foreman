-- The active world is the one piece of game state kept in the app database
-- (SPEC 3.4, 5.3). A set of rows leaves room for several worlds at once.
CREATE TABLE active_worlds (
    world_id TEXT    PRIMARY KEY,
    since    INTEGER NOT NULL
);
