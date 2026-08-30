-- certmagic_storage backs internal/tls.PostgresStorage, a Postgres-backed
-- implementation of certmagic.Storage (ha-multi-replica HA-13). It replaces
-- certmagic.FileStorage's local-disk layout with a flat key/value table -
-- key mirrors FileStorage's path-based keys (e.g.
-- "certificates/acme-v02.../example.com/example.com.crt"), value holds the
-- raw bytes CertMagic itself already stores. The prefix index supports the
-- interface's "directory" (prefix) semantics for Delete/Exists/List/Stat
-- efficiently (WHERE key LIKE 'prefix/%').
CREATE TABLE certmagic_storage (
    key         TEXT PRIMARY KEY,
    value       BYTEA NOT NULL,
    modified_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX certmagic_storage_key_prefix_idx ON certmagic_storage (key text_pattern_ops);
