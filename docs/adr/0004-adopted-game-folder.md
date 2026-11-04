# 0004. Adopted mode: the game folder and the container rules

- **Status**: Superseded by ADR 0005 (adopted mode was dropped)
- **Date**: 2026-11-04
- **References**: STACK.md section 5; SPEC.md sections 3.4, 4.4, 4.7 to 4.12, 7.1, 7.9, 7.10, 12.6, 14.15

## Context

In adopted mode the operator owns the game container, which runs the
image's own entrypoint. Foreman must switch worlds without editing the
compose file, lose no save if it stops mid-way, and treat the game as
hostile (SPEC 14.1) even though the game can write every world.

## Decision

- **Links and one atomic rename.** Fixed links `saves`, `mods` and `config`
  point through `current`, which a switch replaces by renaming
  `current.new` over it. The folder always presents one whole world.
- **Adoption keeps the originals** in `pre-foreman-<UTC time>/`. Nothing is deleted.
- **Switch and adoption records live in the app database**, because the
  game can write the game folder and could forge a marker there.
- **Strict container refusals** (SPEC 4.11), before anything stops. The
  entrypoint can load the wrong save, change mods, `chown` the folder, or
  run with more privilege than a managed game.
- **A network warning, not a startup gate.** A refusal blocks Start,
  Restart and Switch; Stop and every page keep working (SPEC 7.1).
- **Header sign-in refused by range, NET_RAW dropped.** A game is never the
  proxy, so a peer anywhere in a game network's range is refused (12.6).

## Consequences

- Rejected: **changing the bind mount per switch**. It means editing the
  operator's compose file and recreating the container.
- Rejected: **deleting the originals**. Less disk, but a bug loses saves.
- Rejected: **a marker file in the game folder**. Simpler; the game can forge it.
- Rejected: **refusing to start on a network refusal**. Safer on paper
  (SPEC 14), but the operator could not reach Foreman to stop the game.
- Rejected: **refusing only the game's own addresses**. With NET_RAW a game
  can take another free address on its network.
- Cost: the game can read and write every world (accepted risk, SPEC 14.15).
