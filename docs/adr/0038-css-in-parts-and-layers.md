# 0038. CSS in parts and layers

- **Status**: Accepted
- **Date**: 2026-11-27
- **References**: STACK.md sections 3 and 14, SPEC.md section 17.6, DESIGN
  11, WORKFLOW.md "Where CSS goes"; issues #983 and #1017

## Context

`foreman.css` was one hand-written file. The UI hill climbs grew it to
about 15,900 lines, about 11,700 of them from the climbs, and most of
those were page variants: a later rule restyling a component for one page
instead of changing the component. An inventory found 140 selector groups
with conflicting values. `main h1` was set four times, the segmented
control had four heights, and the plate band's list of parents was copied
eight times. A fix to one component could miss the copy that actually
won. Comments stated values that no longer held, and raw lengths bypassed
the tokens.

STACK 3 rules out a build step, and STACK 14 rules out a CSS framework, so
the fix had to stay plain CSS served by Go.

## Decision

- The stylesheet is 15 parts under `internal/web/static/css/`: `tokens`,
  `base`, seven shared component parts (`buttons`, `tables`, `plates`,
  `forms`, `messages`, `controls`, `shell`) and six page parts under
  `pages/` (server, settings, worlds, generate, mods, design).
  `stylesheetParts` in `internal/web/stylesheet.go` lists them in order. At
  startup Go joins them and serves one `foreman.css` at the usual address,
  with one cache-busting hash. There is no `@import` and no preprocessor.
- `tokens.css` opens with `@layer tokens, base, components, pages;` and
  each part is one block in its layer. A page rule beats a component only
  because it sits in `pages`, so the order of layers, not selector weight,
  says who wins.
- A guard in `internal/web/stylesheet_test.go`, run by `make verify`,
  fails on:
  - conflicts: two rules for one selector in the same `@media`,
    `@container` and `@supports` context that give one property two
    values, with a shorthand counted as its longhands;
  - size tokens: a raw spacing, font size or radius where a token names
    that length, apart from the listed exceptions with their reasons;
  - layers: a part in a layer the `@layer` line does not declare, a part
    not listed, or a part that is not one block;
  - page restyles: a rule in a page part whose selector ends on a shared
    class and sets a property (`TestPagePartsDoNotRestyleSharedComponents`).
    The 236 that predate the rule are listed for #1017, and that list only
    shrinks.
- WORKFLOW.md "Where CSS goes" holds the rule for people and agents: a
  change to how a component looks goes in its shared part, and a page part
  holds only what that page alone has.

## Consequences

- A component is defined in one place, and a fix there reaches every page.
  The guard stops the override pattern from coming back, in a climb too.
- The browser still gets one file and one request, and `/design`, the
  design export and the token reader read the same joined stylesheet.
- A change to a shared look may now touch many pages at once, so it needs
  the layout and component tests on all of them, not just the page that
  showed the problem.
- 236 page restyles remain until #1017 folds them, and lengths that no
  token names are not checked.
- `factorymap.css` stays a separate, unlayered file linked from one page,
  so its rules beat any layered rule.

Rejected:

- **Per-page stylesheets.** Each page would load the shared parts plus its
  own file. The shared components would still need one home, a page file
  could restyle them as freely as before, and each page would cost another
  request and cache entry. Joining at startup gives the same split for
  editing with one file for the browser.
- **A CSS framework** (utility classes or a component kit). STACK 14 leaves
  frameworks out, and most need a build step, which STACK 3 rules out. It
  would also replace DESIGN's tokens and components rather than enforce
  them, and the markup of every page would change at once.
