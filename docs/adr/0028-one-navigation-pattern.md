# 0028. One navigation pattern: the product rail and its page lists

- **Status**: Accepted
- **Date**: 2026-11-19
- **References**: SPEC.md sections 11.1, 13.1, 17.10 and 17.18; DESIGN.md section 6

## Context

Foreman's pages found their way around in several ways at once: a header
bar of four areas, a row of tabs under each page's title, header buttons
for Favorites and Deleted worlds, a rail of section links and a "Jump to
section" foldout on Game settings, and a "Back to worlds" link on Import.
Titles moved from page to page, and the same kind of thing was reached a
different way in each area. A wide screen left most of its width empty.

## Decision

There is one navigation pattern. From 64rem a column on the start side,
the product rail, holds the logo and the four areas, and under them the
current area's own pages: the Server view, Foreman's settings, the Worlds
area (All worlds, Favorites, Deleted worlds) and a world's pages. A strip
across the top names the live world and its state and holds the user
menu. Below 64rem the same lists are tab rows under the heading. No page
carries an in-page list of its own sections; a page too long to read
whole is split into sub-pages in that column (Game settings). Pages
under Worlds open with a full breadcrumb instead of a "Back to" link.

## Consequences

- A person learns one place to look, and content starts at the same
  place on every wide page.
- Game settings' sections are sub-pages, so a setting outside Listing
  takes one more click. A session-storage draft keeps edits across
  sub-pages so one preview still covers them; without script each
  sub-page saves on its own.
- The lists sit in the rail's markup right after the four areas, so
  the rail needs no positioning; ids and addresses do not change with
  the width.
