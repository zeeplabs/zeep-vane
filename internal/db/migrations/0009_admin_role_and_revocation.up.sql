ALTER TABLE admins
    ADD COLUMN role TEXT NOT NULL DEFAULT 'owner'
        CHECK (role IN ('owner', 'operator', 'viewer')),
    ADD COLUMN sessions_revoked_at TIMESTAMPTZ;

-- Explicit backfill for any admin row that existed before this migration.
-- The column default already covers new rows, but this makes the intent
-- explicit and is a no-op when the table is empty.
UPDATE admins SET role = 'owner' WHERE role IS NULL;
