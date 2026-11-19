# 0027. OIDC users are matched by issuer and subject, never by name

- **Status**: Accepted
- **Date**: 2026-11-18
- **References**: SPEC.md sections 12.5, 12.7 and 12.12; STACK.md section 7

## Context

Epic 11a adds OpenID Connect (OIDC) sign-in. Foreman already has users
made by local passwords and by trusted proxy headers. An ID token from the
identity provider carries a username (`preferred_username`) and a stable
identifier for the person (`sub`, the subject, unique within the issuer).

A name is not a safe key. Many providers let people change their own name,
and two different people can hold the same name at different times. If
Foreman matched on name alone, someone who renamed themselves at the
provider could sign in as another Foreman user and, since every user is an
admin (12.1), take over that account.

There is one case where a name is all Foreman has. A user made by trusted
headers has no password and no OIDC link. An operator who turns on `oidc`
beside `headers` expects the same person to keep their history (their audit
entries, their API tokens) when they first sign in through OIDC.

## Decision

A user is found by the provider's issuer and subject. Both are stored on the
user row (migration 0011: `oidc_issuer` and `oidc_subject`, nullable, with a
unique index on the pair where set). A rename at the provider changes
nothing, because the link is the subject.

On the first sign-in by a subject Foreman has not seen:

- If no user has that name, it creates the user, and writes the issuer and
  subject in the same insert. The first user ever is the first admin.
- If a user of that name has no password and no OIDC link (a user made by
  trusted headers), it links that user to the issuer and subject, once. The
  link can never be moved to another subject.
- In every other case (the name belongs to a user with a password, or to a
  user already linked to another identity) it refuses the sign-in with a
  page that says why.

**Accepted risk (Todd's ruling, 2026-11-18).** The by-name link for a
headers-made user trusts the provider's name for one moment. Until that user
has signed in through OIDC once, anyone in the required group who can rename
themselves at the provider could take that name and so the account. After
the first sign-in the link is by subject and a rename no longer matters.
Operators must turn off self-service username changes at the provider. The
operator guide (OPERATOR.md, "Optional: sign in with OpenID Connect") says
so in a warning. An operator who never combines `headers` and `oidc` has no
such user and no such risk.

Rejected:

- **Never link; refuse every name clash.** It is the safest, but a person
  who was a header user would be locked out of their own history: the
  operator would have to delete the user and lose their audit trail and
  tokens, or rename them by hand, and Foreman has no UI for renaming a user.
  It makes switching from `headers` to `oidc` costly for the one case where
  the match is almost always right.
- **Admin approval of each link.** It removes the window, but it needs a new
  pending-link state, a page and an audit action, and an admin who is
  themselves the person being linked has no one to approve it. It builds a
  workflow for a one-time step in a single-admin-group tool.
- **Match on name alone.** Rejected outright: a rename at the provider takes
  over a user, whatever the user's kind.

## Consequences

- A person who is renamed at the provider keeps their Foreman account, and a
  new person who takes an old name does not inherit it.
- Switching from trusted headers to OIDC keeps users' history, at the cost
  of the accepted risk above, which is closed for each user by their first
  OIDC sign-in.
- A name clash with a password user is refused and the person needs an admin
  to delete the other user on the Users tab (12.12), since Foreman cannot
  rename a user. A name Foreman cannot use as a username is refused too.
- The match needs a provider that always sends `sub`. A token with no
  subject, or no `preferred_username`, is refused.
- Group membership is read at sign-in only (12.7). Removing someone from the
  group does not end their session. Deleting their user does.
- A deleted OIDC user comes back at their next sign-in while the provider
  still puts them in the group, as a new user. There is no block list
  (12.12).
