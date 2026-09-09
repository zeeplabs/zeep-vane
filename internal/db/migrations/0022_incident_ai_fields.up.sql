ALTER TABLE incidents
    ADD COLUMN description          TEXT,
    ADD COLUMN pending_close_comment TEXT,
    ADD COLUMN auto_created         BOOLEAN NOT NULL DEFAULT false;
