# 0013. Secrets entered in the UI are sealed in a keyed table

- **Status**: Accepted
- **Date**: 2026-11-07
- **References**: SPEC.md sections 3.2, 9.11 and 14.8; STACK.md section 7

## Context

An admin may type the factorio.com username and token into the UI instead
of setting environment variables (SPEC 9.11). The app database is a file
the game user and a backup can read, so the token must not sit in it as
plain text. Foreman has to keep working when the key is lost or changed.

## Decision

- Values are sealed with AES-GCM from the standard library. The key is
  HKDF-SHA256 of `FOREMAN_SECRET_KEY` (at least 32 characters), with the
  fixed context string `foreman stored secrets v1`.
- A sealed value is one version byte (`0x01`), the nonce, then the
  ciphertext. The version byte lets the format change later.
- The row's name is the additional authenticated data, so a sealed value
  copied into another row does not open.
- They live in a keyed table, `stored_secrets` (name, value, updated time,
  updated by), not in a table made for the account. The account is one row,
  `factorio-account`, holding the username and token together as one
  sealed JSON value. A later secret is a new row, not a new migration.
- A row that cannot be opened (wrong key, wrong name, a changed byte, an
  unknown version) is reported as unreadable, never as a crash, and saving
  the account again replaces it.

## Consequences

Losing or changing the key loses the stored account, and the admin enters
it again; there is no key rotation. The environment variables, when set,
win over the stored row. Anyone who holds both the database and the key can
read the token, so the key must not live next to the database. A key
shorter than 32 characters is refused at start rather than weakly used.
