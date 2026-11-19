-- The audit log's filter menus list the distinct users, actions and worlds
-- (SPEC 12.9). These indexes let each list be read without scanning the table.
CREATE INDEX audit_log_username ON audit_log (username);
CREATE INDEX audit_log_action ON audit_log (action);
CREATE INDEX audit_log_world ON audit_log (world_id);
