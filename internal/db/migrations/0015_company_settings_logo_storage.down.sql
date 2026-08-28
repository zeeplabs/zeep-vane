-- Reverses 0015: any logo bytes stored in the database are lost on
-- rollback (same accepted trade-off as other schema-replacing migrations
-- in this project - down migrations are a structural revert, not a data
-- migration).
ALTER TABLE company_settings DROP COLUMN logo_content_type;
ALTER TABLE company_settings DROP COLUMN logo_data;
ALTER TABLE company_settings ADD COLUMN logo_url TEXT;
