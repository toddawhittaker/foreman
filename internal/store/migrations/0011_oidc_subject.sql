-- The identity provider's issuer and subject of a user who signs in by OIDC
-- (SPEC 12.7). Both are NULL for every other user. A user is found by the
-- pair, never by name alone.
ALTER TABLE users ADD COLUMN oidc_issuer TEXT;
ALTER TABLE users ADD COLUMN oidc_subject TEXT;
CREATE UNIQUE INDEX users_oidc ON users (oidc_issuer, oidc_subject)
    WHERE oidc_issuer IS NOT NULL;
