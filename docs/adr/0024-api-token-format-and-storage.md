# 0024. API tokens: an `fmn_` secret, stored only as its SHA-256

- **Status**: Accepted
- **Date**: 2026-11-17
- **References**: SPEC.md sections 12.10 and 14.19; STACK.md section 7

## Context

An AI assistant needs a long-lived secret to call `/mcp`. The secret must
be easy to revoke, safe if the database leaks, and recognisable if it ends
up in a repository or a chat. Sessions already store only a hash of a
random token (STACK 7).

## Decision

A secret is `fmn_` followed by 43 base64url characters, which encode 32
bytes from the system's secure random source. Foreman stores only the
SHA-256 of the whole secret, through the same hash helper sessions use. A
request's token is hashed and looked up by that hash, so no secret is
ever compared directly. A malformed token is refused without a lookup.

The state (Working, Expired, Revoked) is worked out from the stored times,
never stored. Renewing restarts the 30 days and keeps the same secret, so
the assistant's configuration needs no change. A revoked token cannot be
renewed.

## Consequences

- The `fmn_` prefix lets a secret scanner (the repository's gitleaks rule)
  and a person recognise a leaked token.
- A random 256-bit secret cannot be guessed, so a fast hash is enough; a
  slow password hash would only cost time on every request.
- A database leak gives no usable token, but Foreman can never show a
  secret again. A lost one is replaced, not recovered.
- Renewing keeps the secret, so a token suspected of leaking must be
  revoked, not renewed. SPEC 14.19 lists this as an accepted risk.
- Rejected: a new secret at every renewal. It would make the assistant's
  setup break every 30 days, and people would respond by never renewing.
