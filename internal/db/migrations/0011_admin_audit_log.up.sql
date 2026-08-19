-- No foreign keys with cascade delete on actor_id/target_id: audit history
-- must survive removal of the admin it references (see T3 "Done when").
CREATE TABLE admin_audit_log (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_id   UUID NOT NULL,
    target_id  UUID NOT NULL,
    action     TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
