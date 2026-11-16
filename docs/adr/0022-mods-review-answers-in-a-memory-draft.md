# 0022. The mods review page keeps its answers in a draft in memory

- **Status**: Accepted
- **Date**: 2026-11-16
- **References**: SPEC.md sections 5.12, 9.14 and 17.17, ADR 0021

## Context

The mods review page holds answers: which optional mods are ticked, which
release of each mod is chosen, which mods to remove or turn off, and a
file for a mod the portal lacks. Every link and redraw on the page had to
carry those answers, so they travelled in the address. A favorite of 120
mods made an address of many thousand bytes, which proxies and browsers
refuse. Todd ruled that no address Foreman builds may be long (SPEC
17.17). Three places could hold the answers: the address (the problem), a
table in the app database, or memory on the server.

## Decision

- **A draft in memory, under a random token.** The draft is the page's
  whole form. The token is 18 random bytes, tied to the signed-in user and
  the world, the way the previews of SPEC 5.6 and 11.3 are. Links and
  redraws carry the token and at most one name.
- **Bounded.** A draft lives 2 hours, a user keeps at most 20, the server at
  most 200, and the oldest goes first. A token that is expired, unknown or
  another user's is the same case: the page is rebuilt from the saved picks
  (ADR 0021) and says so.
- **Not a database table.** A draft is a working copy of a form that
  disappears with the page. Nothing needs it after a restart, so a table
  would add a migration, a cleanup job and rows to expire for no gain. The
  saved picks stay the durable part.
- **Not in the address.** A list in the query string was the bug. An
  address that carries only a kind and one name stays under 512 bytes at
  any plan size, and the draft token is never logged.
- **A favorite goes by its id.** Generate passes the favorite's id and the
  review reads the names and versions from the favorite, so nothing about
  the mods rides in the redirect.

## Consequences

- No address grows with the plan, and a 120-mod favorite installs in one
  review. The plan holds up to 500 picks.
- A restart, or two hours of waiting, sends an open review back to the saved
  picks. Ticks and chosen versions made on the lost review are gone, and the
  page says so.
- A second Foreman process would not see the first's drafts. Foreman runs
  as one process (SPEC 3.1), so this is acceptable.
- The same shape fits any later page whose answers are too long for an
  address: keep a bounded draft in memory under a token.
