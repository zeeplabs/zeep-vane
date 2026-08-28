-- Moves the company logo from a filesystem path (logo_url, pointing at a
-- file under UPLOADS_DIR on whichever replica's disk received the upload)
-- into the row itself, so every replica reading the same Postgres serves
-- the same logo regardless of which one handled the upload. The served
-- URL is now a fixed "/uploads/logo" (application-derived from whether
-- logo_data is non-null), never an extension-bearing path, since the
-- content type travels with the bytes instead of the filename.
ALTER TABLE company_settings DROP COLUMN logo_url;
ALTER TABLE company_settings ADD COLUMN logo_data BYTEA;
ALTER TABLE company_settings ADD COLUMN logo_content_type TEXT;
