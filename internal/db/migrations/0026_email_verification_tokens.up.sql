-- Email verification tokens for the SaaS public signup flow
-- (multi-tenancy-core, T9/T10): same token-hash + TTL shape as
-- password_reset_tokens (0002) and tenant_invites (0024) - only the hash is
-- ever persisted, never the raw token. Not tenant-scoped: verification
-- gates a user's own identity (users.email_verified_at), which exists
-- before any tenant is chosen, exactly like password_reset_tokens.
CREATE TABLE email_verification_tokens (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
