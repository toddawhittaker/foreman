-- The mods picked on a world's Mods tab, waiting for review (SPEC 9.14). Only
-- the picked names are kept; their dependencies are worked out when the panel
-- is drawn. It lives here and not in the world folder, so snapshots, clones and
-- restores never carry a plan along (SPEC 3.4). World ids have no foreign key:
-- worlds live on disk, so deleting a world clears its rows in code. The
-- username is kept as text, as in audit_log.
CREATE TABLE mod_plan_mods (
    world_id TEXT    NOT NULL,
    name     TEXT    NOT NULL,
    added_at INTEGER NOT NULL,
    added_by TEXT    NOT NULL,
    PRIMARY KEY (world_id, name)
);
