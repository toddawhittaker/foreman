# 0005. Managed mode only, and any patch release by version tag

- **Status**: Proposed
- **Date**: 2026-11-04
- **References**: STACK.md section 1; SPEC.md sections 3.2, 3.7, 4.1 to 4.13, 7.4, 10, 18.4, 18.5, 19.1. Supersedes ADR 0004; amends ADR 0003.

## Context

Pass-1 testing on real data on 2026-11-04 found that adopted mode, where one
operator-defined container runs every world, blocked a 2.0 world with mods
(the container ran 2.1) and caused a blocker when switching back. Most gaps
left by Epic 4 were in adopted mode. Managed mode allowed only three images,
one pinned by digest per line, so a world could not keep the exact version
it was made with.

## Decision

- Foreman has one runtime mode: it creates and owns the game container.
  Adopted mode and its settings are removed.
- A world's game version is any exact patch release of the 1.1, 2.0 or 2.1
  line. Its image is `factoriotools/factorio:<version>`, by tag, not digest.
  A world's version only moves up; rollback is the way back (SPEC 10.3).
- Accepted risk: upstream can rebuild a tag, so the image behind a version
  can change when downloaded again. Foreman uses what the local Docker holds
  under that tag and downloads only when the admin asks (SPEC 3.7).
- The version picker lists Docker Hub's tags for the image, filtered to
  supported lines, fetched only when the page is opened; a typed version
  also works. A new line needs a Foreman update. The pre-start check is
  unchanged.

## Consequences

- Each world runs on its own version; switching needs no shared container.
- Rejected: **keeping adopted mode**. Two code paths and the gaps above.
- Rejected: **freezing it as experimental**. Still tested and reviewed, for a mode nobody should pick.
- Rejected: **three pinned images updated by Renovate**. Worlds made on other patch releases could not keep their version.
- Rejected: **digests recorded at first download**. Safer against rebuilds, but more state and a mismatch path to design.
- Rejected: **a reviewed digest list**. Every new patch release would wait on a Foreman release.
- Rejected: **truly any version, unknown lines included**. The save check (SPEC 7.4, 9.9) would have to be turned off for them.
- Rejected: **Factorio's release list, or only a typed version, as the picker source**. The release list can name versions with no image yet; typing alone is error-prone on a phone.
