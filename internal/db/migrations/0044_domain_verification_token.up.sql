-- Domain apex TXT verification (DATV-01): every registered root domain gets
-- one opaque token proving ownership of its DNS. Existing rows are
-- backfilled with a real generated value so none is left unable to verify;
-- the DEFAULT is then dropped so new rows must supply their own token from
-- application code (DomainRepository.Create), keeping token generation in
-- Go, not the database.
ALTER TABLE domains
    ADD COLUMN verification_token TEXT NOT NULL DEFAULT encode(gen_random_bytes(16), 'hex');

ALTER TABLE domains
    ALTER COLUMN verification_token DROP DEFAULT;
