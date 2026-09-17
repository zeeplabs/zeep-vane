-- target_label is a write-time snapshot of the target's human-readable
-- name (email, domain, status page name, admin name/email), filled in by
-- the caller at audit.Log.Record time - it survives the target row being
-- deleted later, unlike a runtime join would (recent-team-activity
-- feature). Nullable: existing rows predate this feature and have no
-- label source to derive one from.
ALTER TABLE admin_audit_log ADD COLUMN target_label TEXT;
