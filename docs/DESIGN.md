# Foreman: design

This document sets the type, colour, spacing, focus style and components
that every Foreman screen uses (SPEC 17.6). It covers what the screens of
Epics 1 to 3 need: sign-in and setup, the world list, saves and
snapshots, server state, the log and the console, and confirmations for
risky actions. Section 12 lists what is deliberately left for later.

The CSS is hand-written, with no framework and no build step (STACK 3).
Nothing loads from outside the binary (SPEC 3.7), so there is no CDN and
no hosted font. The two typefaces and the icon set are vendored in
`internal/web/static/vendor/` with their licences (2, "Font stacks"; 7.9).

When this document and a screen disagree, change the screen. When this
document is wrong, change it in the same pull request and say why.

## 1. Character

Foreman is a tool for keeping a game server alive, used in a hurry, often
from a phone. It should look like a well-labelled control panel, not a
marketing page, and it should feel at home next to the game it runs.

The look is a calm control panel that sits well beside the game: warm
charcoal greys in dark, warm off-white in light, orange for "act here or
you are here", green for success and red for destroy. Foreman is not made
by, affiliated with or endorsed by Wube Software. It uses its own colour
values and none of the game's artwork, icons, logos, fonts or textures,
and nothing in it should suggest it is an official product.

- **Cards with 1 px lines.** On a wide screen the page is a rail on the
  start side, a status strip across the top, then the cards; on a phone it
  is a header bar, then one column of cards (6, "Shell"; 7.8). A card is a
  section on `surface` with a 1 px `line` edge and 8 px corners, set on
  the `bg` page; inside it, headings, lists and thin rules give the
  structure. A box also appears where it means something: a field, the
  log, a message, a confirmation. There are no bevels (apart from a
  secondary button's raised plate and a field's sunk well, 4), gradients
  (apart from the hard-stop fills inside state shapes), glows (apart from
  the faint ring round the running badge's square) or blurred glass. A
  shadow comes from one of two tokens: the card's faint edge
  light (`--shadow-card`) and what floats over the page (`--shadow-pop`):
  the user menu, a row's More menu, the selection bar and "Jump to
  newest", and the sign-in box.
  Corners are 6 px on controls and foldout rows under the pointer, and
  8 px on cards and the menu.
- **Colour that means something.** Neutrals are warm greys. Orange marks
  the one primary button on a screen, the current place, checked boxes,
  links, the line across the top of the header and the live world's edge in
  the world list: "act here" or "you are here". Green means only success:
  the "running" badge, the Server plate's state rule while the game runs, and the
  "Done" word. Red is the final step of a destructive action, the failed
  states and the outlined buttons that open a destructive step (7.1).
  Yellow is a warning. Blueprint blue (`info`) marks a note and what is
  planned but not yet done: a mod in the plan, a review that destroys
  nothing, an empty state's drawing. None of them decorates.
- **Dark first.** Dark is the main look, as in the game. The light theme
  is the same layout in warm off-white, not an inverted afterthought.
- **The logo in the header.** The rusty Foreman logo sits beside the
  product name in the header, 32 CSS pixels square (6, Shell), and above
  the heading of the sign-in and setup pages at 64 px. A faint drafting
  grid sits behind the rail's logo and behind the sign-in and signed-out
  pages. There is no hero or illustration anywhere else.
- **IBM Plex, with Plex Mono for what the game prints**: file names,
  world ids, folder ids, seeds, commands, the console and the log are in
  IBM Plex Mono. So are the figures a machine writes when they stand as
  values: a version (`.ver`, `.mod-ver`), and the sizes, times and versions
  in table cells and in the world list's facts, at 13 px with tabular
  digits. The overline is Plex Mono capitals too (2). Everything a person
  wrote, or Foreman wrote for a person, is in IBM Plex Sans, and so are
  table column names (8.3) and fact names (2).
- **Dense, not cramped.** Lists and tables inside cards, not grids of
  tiles. A world list is a list of rows divided by rules. A save list is a
  table.
- **Words first, with small line icons.** State, actions and messages are
  words. A small line icon (7.9) sits before the words of a tab, a section
  heading, a control button and a few links, to help scanning; it is drawn
  in CSS, takes its text's colour and is never the only sign. Where an icon
  stands alone (a message's close button, a plan row's remove), its name
  is hidden text. State badges
  keep a shape drawn in CSS beside their word. No emoji and no icon font.

## 2. Type

### Font stacks

```css
--font-sans: "IBM Plex Sans", system-ui, -apple-system, "Segoe UI", Roboto,
  "Noto Sans", "PingFang SC", "Microsoft YaHei", "Noto Sans CJK SC",
  "Liberation Sans", Arial, sans-serif;
--font-mono: "IBM Plex Mono", ui-monospace, "SF Mono", "Cascadia Mono",
  "Segoe UI Mono", "DejaVu Sans Mono", "Liberation Mono", Menlo, Consolas,
  monospace;
```

IBM Plex Sans and IBM Plex Mono are vendored as WOFF2 files in
`internal/web/static/vendor/` and served by Foreman itself, so nothing is
downloaded from outside (SPEC 3.7). Plex Sans is one variable-weight file
per script (weights 100 to 700). Plex Mono is regular (400) and semi-bold
(600). Each family comes in three scripts, Latin, Latin Extended and
Cyrillic, chosen by `unicode-range`, so a page loads only the files its
text needs. All are `font-display: swap`, so text shows at once in a
system face and switches when the font arrives. Scripts Plex lacks, such
as Chinese, fall through to the system faces later in the stack. The
licence of each family (the SIL Open Font License 1.1) sits beside the
files as `IBM-Plex-Sans-OFL.txt` and `IBM-Plex-Mono-OFL.txt`, and
`SHA256SUMS` lists the checksum of every vendored file.

### Scale

The root size is left at the browser default (normally 16 px) so that a
person's own text-size setting works. Every size is in `rem`. The steps
are about 1.125 to 1.25 apart, which keeps headings close to body text: on
a 320 px screen, big headings waste lines. The page heading (32 px) and the
section heading (20 px) are the two steps that jump, so a person scrolling
a phone can find where a page and a section start.

| Token | Size | Line height | Weight | Use |
|---|---|---|---|---|
| `--text-xs` | 0.8125rem (13 px) | 1.23 | 600 | Column names and fact names (Plex Sans, sentence case, `text-muted`), hints (weight 400, a 1.5 line height, 66ch), badges, a mod's portal news line, figures in table cells (Plex Mono); the game log below 40rem |
| `--text-sm` | 0.875rem (14 px) | 1.43 | 400 | A card's one-line description under its heading (a 1.5 line height, 60ch), table captions, timestamps, the log and console, labels (600), state badges (600), units beside a figure, breadcrumbs (500) |
| `--text-md` | 0.9375rem (15 px) | 1.5 | 400 | Body, form controls, buttons (600), table cells, row titles (600) |
| `--text-lg` | 1.125rem (18 px) | 1.333 | 650 | Group headings (`h3`, an operation's heading), the product name (700), the Server page's state badge |
| `--text-xl` | 1.25rem (20 px) | 1.3 | 650 | `h2`, section headings; the Server plate's figures on a phone |
| `--text-2xl` | 1.5rem (24 px) | 1.167 | 600 | Stat figures: the Server plate's gauges, tabular |
| `--text-3xl` | 2rem (32 px) | 1.2 | 700 | `h1`, one per page, with `letter-spacing: -0.015em`; 26/32 below 40rem |

Column names (table heads and the heads of the world, token and user
lists) and fact names (`dt`) are labels set apart from content by colour
and weight, not by capitals: `--text-xs` (13 px), weight 600,
`text-muted`, in Plex Sans and sentence case, so a long German or Russian
name stays easy to read. Only the overline (`p.eyebrow`, 8.1) keeps the
stamped look: 0.75rem (12 px, no token), weight 600, `text-muted`, in Plex
Mono capitals tracked `0.06em`. Its text stays sentence case in the
markup, and a Chinese overline uses Plex Sans untracked.

`h3` is 1.0625rem (17 px, no token) at weight 600. So is an operation's
heading (`h2.op-heading`) and the connected-players heading in the
settings: each is an `h2` for the outline that sits inside its section, so
it is set as an `h3`.

Rules:

- Body is 15 px, helper text 13 px and section headings 20 px, so help,
  body and headings are three distinct steps. Text smaller than 13 px is
  only the 12 px overline and the rail's list name (6). Form controls take the body size.
- Body text is limited to `max-inline-size: 68ch` (`--measure`). A hint is
  13 px at a 1.5 line height, held to 66ch, so long help never reads as a
  grey wall. Tables and the log are not limited.
- Numbers that line up in columns (sizes, times, uptime, player counts)
  use `font-variant-numeric: tabular-nums`: tables, facts, the log and
  every `time`.
- Monospace text is set smaller than sans would be at the same size,
  because monospace faces look larger: `code` is 0.9em, the log and
  console are `--text-sm`, and a field that holds game text takes the
  field's size.
- Prose breaks at syllables (`hyphens: auto`) in the page's own language,
  but only a word of ten letters or more, and never leaving fewer than four
  letters on either side of the hyphen. A name a program reads, such as a
  mod's (`.ident`), and `code` are never hyphenated.
- Long unbroken strings (world ids, file names, 60-character world names,
  log lines) use `overflow-wrap: anywhere` so they wrap instead of
  pushing the page wider than 320 px.
- Copy is sentence case. Buttons start with a verb ("Start server",
  "Delete world", "Take snapshot"), never "Submit" or "OK". The one
  exception is drawn by CSS, not written: the overline and the rail's list
  name (6) are shown in capitals, and their text stays sentence case.
- Headings never skip a level. Each page has exactly one `h1`.

## 3. Colour

### How themes work

Both themes are defined as CSS custom properties on `:root`. Each colour
token holds both values as `light-dark(light, dark)`, and each shadow token
holds both themes' layers, the other theme's drawn transparent. So the
`color-scheme` property alone decides the theme, and native controls
(checkboxes, scroll bars, the file picker) follow it too. Each theme has
four depths of surface: `bg` (the page), `surface` (cards and bars), `raised` (controls
and boxes sitting on a card) and `deep` (fields and the log). In light,
`surface` and `deep` are both white.

The user menu has a Theme choice: Auto, Light or Dark (SPEC 17.3). Auto is
the default and follows the system setting: `:root` sets
`color-scheme: dark light` and the page's `color-scheme` meta says the
same. Light or Dark posts to `POST /theme`, which keeps the choice in a
`foreman_theme` cookie for this browser and goes back to the page through
the same return-path check as the language menu. The server then writes
`data-theme="light"` or `"dark"` on `html` and that one scheme in the
meta, and `[data-theme]` sets `color-scheme` to it, overriding the
system. The same attribute on any other element draws that part of the page
in one theme; such an element sets its own `color` and `background` again,
because the inherited ones were resolved in the page's theme. The
component page (`GET /design`, linked from nowhere) uses this to show every
component in both themes side by side, with the tokens and icons read from
`foreman.css` itself. The page is drawn in the chosen theme from the first paint, with
no script. Both themes must meet every ratio below (SPEC 17.1).

Every colour in the CSS comes from a token below. A raw hex value
anywhere else in the stylesheet is a bug, except the dimming behind a
dialog (black at 50%) and the shadows' own values (4).

### Tokens

| Token | Dark | Light | Use |
|---|---|---|---|
| `--color-bg` | `#131210` | `#e9e3d6` | The page |
| `--color-surface` | `#1e1c19` | `#fffdf9` | The header bar, the rail and the status strip, cards, the user menu panel, the confirmation box, dialogs |
| `--color-raised` | `#292622` | `#f5f0e6` | The secondary button's face, the current item in the rail, the user menu's button, the Generate preview column and picture frames, the bar under the Generate form, the "stopped" and "unknown" badges, a table row under the pointer (60% of it) |
| `--color-deep` | `#0d0c0b` | `#ffffff` | Form fields, the log and console |
| `--color-line` | `#3b3732` | `#d8d0bf` | Decorative dividers and card edges: under the header and the tabs, between rows; never the only edge of a control |
| `--color-text` | `#ece7df` | `#1c1915` | Body text, field text |
| `--color-heading` | `#f8f5ef` | `#13110e` | Headings, the product name, secondary button text, nav links when current or under the pointer, the figures in the status card; a touch stronger than `text` |
| `--color-text-muted` | `#c2baae` | `#4f493f` | Hints, placeholders, timestamps, table captions and headers, fact names, empty states, idle tabs, the icons before section headings |
| `--color-border` | `#958b7e` | `#8a8073` | Edge of fields, secondary buttons and the menu |
| `--color-link` | `#ffb357` | `#9a4800` | Links (always underlined), quiet buttons, the "Active" badge |
| `--color-selected` | `#f39a2a` | `#bd6500` | The current tab's underline, the current item's edge in the rail, the current item in the menu, the live world's edge, checked boxes, progress bars |
| `--color-accent-bg` | `#352610` | `#fdecd5` | A pale orange tint: the current item in the menu, the "Active" badge |
| `--color-primary` | `#f39a2a` | `#f39a2a` | Primary button face, the line across the top of the header |
| `--color-primary-hover` | `#f7ae4f` | `#f6ad52` | Primary button hover |
| `--color-primary-edge` | `#f39a2a` | `#a35200` | Primary button edge (the face is too pale to carry it in light) |
| `--color-on-primary` | `#1a1004` | `#1a1004` | Primary button text |
| `--color-secondary-hover` | `#34302b` | `#eee8de` | Secondary button, menu item and menu button face under the pointer |
| `--color-danger` | `#e5553c` | `#b3301d` | Danger button face, "crash-looping" badge background, the confirmation box's edge, the Server plate's state rule when the game has crashed |
| `--color-danger-hover` | `#ef7560` | `#932616` | Danger button hover |
| `--color-on-danger` | `#1a0503` | `#ffffff` | Danger button text, "crash-looping" badge text |
| `--color-danger-text` | `#ff978a` | `#a32a18` | Error words, field error text, invalid field border, "crashed" badge, the required mark, the outlined (risky) and remove buttons, Force stop |
| `--color-danger-bg` | `#3a1913` | `#fbe6e1` | Error message and "crashed" badge background, risky and remove buttons under the pointer |
| `--color-ok` | `#86d672` | `#1f6a2a` | "Done" word, "running" badge, the Server plate's state word and rule while the game runs |
| `--color-ok-bg` | `#172a12` | `#e3f1de` | Success message and "running" badge background |
| `--color-warn` | `#f2c14e` | `#665400` | "Warning" word, "starting" and "stopping" badges, the Server plate's state word and rule while stopping |
| `--color-warn-bg` | `#33280c` | `#f5f0c2` | Warning message and "starting"/"stopping" badge background |
| `--color-info` | `#8ec1ea` | `#1d5a8a` | "Note" word and a note's edge; the Server plate's state word and rule while starting; a planned mod's edge, a harmless plan's top edge, an empty state's drawing |
| `--color-info-bg` | `#13222f` | `#e2eef7` | Notice background |
| `--color-focus` | `#f7ae4f` | `#b35c00` | Focus outline |

The map's own colours are tokens too, the same in both themes, because
they sit on the map's picture, which does not follow the theme (8.8):

| Token | Value | Use |
|---|---|---|
| `--color-pin-face` | `#ffffff` | The map pin's face; the text of a territory's size label |
| `--color-pin-edge` | `#000000` | The map pin's edge and dot |
| `--color-territory-stripe` | `rgb(200 20 20 / 33.3%)` | A demolisher territory's stripes |
| `--color-territory-border` | `rgb(255 30 30)` | The border between territories |
| `--color-territory-outline` | `#101010` | The ring around a territory's size label |

How the colours divide the work:

- **Orange means "act here" or "you are here".** The one primary button
  on a screen (7.1), the current place (a 3 px edge in the rail, an
  underline in a phone's tab row), the current language in the menu, a
  checked box, the live world's edge in the world list, the line across
  the top of the phone header, and focus. Links in running text are orange
  too, and always underlined, so a link is never mistaken for the button.
  Breadcrumbs and disclosure triangles are not orange: they are quiet
  (`text-muted`), so orange stays for those meanings. The "Active"
  badge is an orange outline on a pale orange tint (`accent-bg`), not a
  solid fill, so it does not compete with the primary button.
- **Green means success.** Only the "Done" word, the "running" badge and
  the Server plate's state rule while the game runs, which say an action or the
  server succeeded. No button is green.
- **Red means destroy or failed.** The final button of a destructive
  action, the outlined and remove buttons that open one (7.1), errors, the
  crashed states and the required mark.
- **Blueprint blue means noted or planned.** The `info` pair marks a
  note (7.3), the starting state, a mod chosen for the plan and a plan
  that destroys nothing (8.11), and the drawing in an empty state (7.3). It always comes
  with words or a badge.
- **Yellow means warning**: the "Warning" word, the in-between server
  states, and the Server plate's state word and rule while the game is
  stopping (starting is `info` blue there). It is lighter and
  yellower than the orange in dark, and browner in light, and it always
  comes with its word.
- **The focus ring is orange**, a tone close to the primary button's:
  `#f7ae4f` in dark and `#b35c00` in light. It is told apart from the
  other oranges by its shape, a 3 px ring held 2 px off the control, not by
  its hue, and it meets 3:1 on every surface in both themes (below).

### Contrast

Every ratio below was computed with the WCAG 2.2 relative-luminance
formula from the hex values above, not estimated. Text needs 4.5:1
(1.4.3, normal-size text, since nothing here relies on the large-text
exception). Control edges, focus outlines and the state badge's drawn
shape need 3:1 against what is next to them (1.4.11).

The script that produced them is short enough to rerun when a token
changes: compute each colour's relative luminance L, then
`(L_lighter + 0.05) / (L_darker + 0.05)`.

The lowest text ratio is 5.34:1 in dark (`on-danger` on `danger`) and
4.97:1 in light (`link` on `bg`). The lowest edge or focus ratio is
3.91:1 in dark (`border` on `secondary-hover`) and 3.03:1 in light
(`border` on `bg`).

**Text pairings (need 4.5:1)**

| Foreground | Background | Dark | Light |
|---|---|---|---|
| `text` | `bg` | 15.21 | 13.69 |
| `text` | `surface` | 13.81 | 17.24 |
| `text` | `raised` | 12.23 | 15.42 |
| `text` | `deep` | 15.88 | 17.51 |
| `text` | `secondary-hover` | 10.64 | 14.37 |
| `text` | `accent-bg` | 11.88 | 15.12 |
| `heading` | `surface` | 15.62 | 18.55 |
| `heading` | `raised` | 13.84 | 16.59 |
| `heading` | `secondary-hover` | 12.03 | 15.47 |
| `text-muted` | `bg` | 9.74 | 6.97 |
| `text-muted` | `surface` | 8.85 | 8.77 |
| `text-muted` | `raised` | 7.84 | 7.84 |
| `text-muted` | `deep` (placeholders) | 10.17 | 8.91 |
| `text-muted` | `secondary-hover` | 6.81 | 7.31 |
| `link` | `bg` | 10.55 | 4.98 |
| `link` | `surface` | 9.58 | 6.26 |
| `link` | `raised` | 8.49 | 5.60 |
| `link` | `deep` | 11.02 | 6.36 |
| `link` | `accent-bg` | 8.24 | 5.49 |
| `on-primary` | `primary` | 8.45 | 8.45 |
| `on-primary` | `primary-hover` | 9.93 | 9.84 |
| `on-danger` | `danger` | 5.34 | 6.26 |
| `on-danger` | `danger-hover` | 6.92 | 8.30 |
| `danger-text` | `bg` | 8.96 | 5.66 |
| `danger-text` | `surface` | 8.13 | 7.12 |
| `danger-text` | `raised` | 7.20 | 6.37 |
| `danger-text` | `deep` | 9.35 | 7.24 |
| `danger-text` | `danger-bg` | 7.56 | 6.03 |
| `ok` | `bg` | 10.59 | 5.21 |
| `ok` | `surface` | 9.61 | 6.56 |
| `ok` | `ok-bg` | 8.63 | 5.68 |
| `warn` | `bg` | 11.16 | 5.80 |
| `warn` | `surface` | 10.13 | 7.30 |
| `warn` | `warn-bg` | 8.63 | 6.40 |
| `info` | `info-bg` | 8.45 | 6.18 |
| `text` | `danger-bg` | 12.84 | 14.60 |
| `text` | `ok-bg` | 12.40 | 14.93 |
| `text` | `warn-bg` | 11.77 | 15.11 |
| `text` | `info-bg` | 13.15 | 14.85 |
| `link` | `danger-bg` | 8.91 | 5.30 |
| `link` | `ok-bg` | 8.60 | 5.42 |
| `link` | `warn-bg` | 8.17 | 5.49 |
| `link` | `info-bg` | 9.12 | 5.39 |

**Control edges and focus (need 3:1)**

| Foreground | Background | Dark | Light |
|---|---|---|---|
| `border` (field, secondary button, menu edge) | `bg` | 5.59 | 3.03 |
| `border` | `surface` | 5.08 | 3.82 |
| `border` | `raised` | 4.50 | 3.41 |
| `border` | `deep` | 5.84 | 3.88 |
| `border` | `secondary-hover` | 3.91 | 3.18 |
| `primary-edge` (primary button) | `bg` | 8.44 | 4.36 |
| `primary-edge` | `surface` | 7.66 | 5.49 |
| `primary-edge` | `raised` | 6.79 | 4.91 |
| `danger` (danger button, confirmation edge) | `bg` | 5.08 | 4.89 |
| `danger` | `surface` | 4.61 | 6.16 |
| `danger` | `raised` | 4.08 | 5.51 |
| `selected` (tab underline, menu mark, live world edge) | `bg` | 8.44 | 3.27 |
| `selected` | `surface` | 7.66 | 4.11 |
| `selected` | `raised` | 6.79 | 3.68 |
| `selected` | `accent-bg` | 6.59 | 3.61 |
| `link` ("Active" badge outline) | `accent-bg` | 8.24 | 5.49 |
| `danger-text` (invalid field border) | `bg` | 8.96 | 5.66 |
| `danger-text` | `deep` | 9.35 | 7.24 |
| `focus` | `bg` | 9.92 | 3.69 |
| `focus` | `surface` | 9.00 | 4.65 |
| `focus` | `raised` | 7.98 | 4.16 |
| `focus` | `deep` | 10.35 | 4.72 |
| `focus` | `accent-bg` | 7.74 | 4.08 |
| `focus` | `danger-bg` | 8.37 | 3.94 |
| `focus` | `ok-bg` | 8.08 | 4.03 |
| `focus` | `warn-bg` | 7.67 | 4.08 |
| `focus` | `info-bg` | 8.57 | 4.00 |
| `ok` (running card edge) | `surface` | 9.61 | 6.56 |
| `warn` (starting or stopping card edge) | `surface` | 10.13 | 7.30 |
| `ok` (badge border and shape) | `ok-bg` | 8.63 | 5.68 |
| `warn` | `warn-bg` | 8.63 | 6.40 |
| `danger-text` | `danger-bg` | 7.56 | 6.03 |
| `info` (planned row edge, plan's top edge) | `surface` | 8.88 | 7.17 |
| `info` (message border) | `info-bg` | 8.45 | 6.18 |
| `text-muted` ("stopped" badge) | `raised` | 7.84 | 7.84 |
| `on-danger` ("crash-looping" shape) | `danger` | 5.34 | 6.26 |

Not covered by a ratio, on purpose:

- `--color-line` and the steps between `bg`, `surface` and `deep`. They
  divide the page, not information, and no control relies on them: every
  control's edge is one of the rows above.
- The primary button's face against the page in light (`primary` on
  `bg`): its edge is `primary-edge`, which is the row above.
- A disabled control (exempt under 1.4.3; this design avoids disabled
  controls, see 7.1; it is drawn at half opacity), and native checkboxes and
  radios, whose edges the browser draws for the `color-scheme` in use.
- The drawings in empty states, a line icon in `info` over a dashed
  tile on a faint grid. They are decoration beside a sentence that says
  the same.
- The faint drafting grid behind the rail's logo and the sign-in pages,
  which is decoration.

## 4. Spacing

A 4 px base, in `rem` so it scales with text size.

| Token | Value | Typical use |
|---|---|---|
| `--space-1` | 0.25rem (4 px) | The header's block padding; the gap between a sub-navigation's tabs |
| `--space-2` | 0.5rem (8 px) | Gap between a label, its hint and its field; table cell padding block; the gap between a badge's shape and its word; the space under a section's `h2`; the gap between a mod row's action and its More menu |
| `--space-3` | 0.75rem (12 px) | Gap between buttons in a row; table cell padding inline; the gap between a card's parts; message padding; the page's side padding at phone width |
| `--space-4` | 1rem (16 px) | Gap between form fields; confirmation box padding; card padding below 48rem (capped at 4vw); the gap between sections at phone width |
| `--space-5` | 1.5rem (24 px) | Gap between sections inside `main` from 40rem; card padding from 48rem; the page's side padding from 48rem to 64rem |
| `--space-6` | 2rem (32 px) | The page's bottom padding, and its top padding from 40rem; its side padding from 64rem; the gap between Generate's settings and its preview |

Other fixed values, also tokens:

| Token | Value | Use |
|---|---|---|
| `--radius` | 6px | Buttons, fields, the menu button, messages, the log |
| `--radius-lg` | 8px | Cards, the confirmation box, the menu panel, the Generate preview column |
| `--border-width` | 1px | Every border except the ones below |
| `--focus-width` | 3px | Focus outline |
| `--target-min` | 2.75rem (44 px) | Minimum block size of buttons and fields |
| `--target-inline` | 1.5rem (24 px) | Minimum block size of a link inside an error or an empty state's sentence (WCAG 2.5.8) |
| `--measure` | 68ch | Maximum line length of prose |
| `--page-max` | 100rem | Maximum width of `main`'s content; from 64rem `main` starts after the rail, left-aligned, and adds `--space-6` each side |
| `--rail` | 15rem | Width of the start-side rail from 64rem |
| `--card-pad` | `var(--space-4)`; `min(var(--space-4), 4vw)` below 48rem, `var(--space-5)` from 48rem, `0px` below 22.4rem | A card's padding (7.8) |
| `--dur-1` | 120ms | Hover and pressed fills easing in |
| `--dur-2` | 200ms | A chevron turning, a meter moving |
| `--ease` | `cubic-bezier(0.2, 0.8, 0.2, 1)` | The one easing curve; all motion is off under reduced motion |

The shadows, each theme's own (the other theme's layers are transparent):

| Token | Dark | Light | Use |
|---|---|---|---|
| `--shadow-card` | `inset 0 1px 0 rgb(255 255 255 / 3%)` | `0 1px 2px rgb(60 45 25 / 8%), 0 1px 1px rgb(60 45 25 / 4%)` | A card's faint lift |
| `--shadow-pop` | `0 8px 24px rgb(0 0 0 / 45%)` | `0 10px 28px rgb(60 45 25 / 18%)` | What floats: the user menu's panel, a row's More menu, the selection bar, "Jump to newest"; the sign-in box |

Badges and the state pill are fully rounded (999px), which is not a token.

### Depth

There is very little. A card (7.8) has a 1 px `line` border and
`--shadow-card`: a faint top highlight in dark and a hairline shadow in
light, enough to lift it off the page without looking raised. The user
menu's panel floats over the page and carries `--shadow-pop`. Controls
carry one more cue, so a button beside a field never reads as a second
field: a secondary button is a raised plate (`raised` face, a light inset
line along its top and a 1 px shadow under it, pressed in while active),
the primary button has the same top line, and a field is a sunk well (a
faint inset shadow at its top). Nothing else has a shadow, a bevel or a
gradient (state shapes use hard-stop fills, 7.5).
Every field, button, message, badge and box has a 1 px border, so its edge
shows in forced-colours mode as well (section 6, "Forced colours"); the
shadows are decoration only. A group inside a long form, such as a
settings fieldset, has a 1 px `line` above it instead of a box. Below
22.4rem (a 320 px phone) a card gives up its frame and its padding: it
becomes a flat section with a 1 px `line` rule above it and `--space-4`
under the rule, so the content keeps its width. The space under a
section's `h2` is `--space-2`, closer than the content's own parts, which
use `--space-3` or `--space-4`. A confirmation box is a card with a red
frame (7.4).

### Rules

- A component never sets its own outer margin. The parent spaces its
  children with `gap` (flex or grid). There are two exceptions. The first is
  prose flow inside a block of text, which uses the default paragraph margins reset
  to `margin-block: 0 var(--space-3)`. The second is a section heading: a
  panel's `h2` or section head pulls the panel's gap in with a negative
  block-end margin, so the space under it is `--space-2`.
- Use logical properties (`padding-inline`, `margin-block`,
  `inset-inline-start`), never left and right.

## 5. Focus

```css
:focus-visible {
  outline: var(--focus-width) solid var(--color-focus);
  outline-offset: 2px;
}
```

- Every focusable element shows this ring: links, buttons, fields,
  `summary`, and scrollable regions given `tabindex="0"`.
- Never `outline: none` without this replacement. Fields show the ring on
  any focus, because browsers treat text fields as `:focus-visible`
  whether reached by mouse or keyboard.
- The 2 px offset puts the ring on the surface around the control
  (`bg`, `surface`, `raised`, `deep`, `accent-bg` or a message
  background), which is why the focus ratios in section 3 are measured
  against those, not against button faces. Two controls sit too close to
  their neighbours for that: a toggletip button's ring has no offset, and a
  map-settings tab's ring sits 3 px inside the tab.
- The ring is orange in both themes, a tone close to the primary button's
  (3, "How the colours divide the work"). Because it is a ring with a gap,
  not a fill, it is not read as the primary face or the selected underline.
- A 3 px ring around a control at least 44 px tall meets WCAG 2.2 focus
  appearance (2.4.13, AAA) as well as focus visible (2.4.7, AA).
- Nothing sticky may cover the focused element (2.4.11). On a phone the
  header is not sticky. From 64rem the status strip, with the logo and
  the user menu in its row, sticks to the top of the window. It is 56 px
  tall, so the page sets `scroll-padding-block-start` to the strip's
  height plus a small gap from 64rem, and an element focused by keyboard
  scrolls into view below it. The Audit log's sticky column names stick
  just below the strip. The open user menu covers the top of the
  page, but it closes on Escape and on a click outside, and focus stays
  inside it while open. Generate's preview column is also sticky on a wide
  screen; it sits beside the form, never over it.
  The one-row bars that stick to the foot of a card or form (6, "The 320 px
  rules", rule 7) hold the action or the field they keep in reach.
- In Windows high-contrast mode (`forced-colors: active`) the outline
  still shows, and every button, field and box keeps its 1 px border
  (section 4, "Depth") so its edge shows too.

## 6. Page layout

### Shell

```html
<body>
  <a class="skip" href="#main">Skip to content</a>
  <header class="site">
    <a href="/" class="product"><img src="logo-32.png" srcset="logo-64.png 2x" alt="" width="32" height="32"><span class="product__name">Foreman</span></a>
    <div class="site-strip"><span class="site-world" aria-hidden="true">Family base 2025</span><span id="site-state" class="site-state" aria-hidden="true">…</span></div>
    <nav aria-label="Main">
      <ul class="tabs">
        <li><a href="/" aria-current="page">Server</a></li>
        <li><a href="/worlds">Worlds</a></li>
        <li><a href="/snapshots">Snapshots</a></li>
        <li><a href="/settings/general">Settings</a></li>
      </ul>
    </nav>
    <details class="menu">
      <summary><span class="menu__user">todd</span> <span class="menu__lang">English</span></summary>
      <div class="menu__panel">
        <p class="hint">Signed in as todd</p>
        <details class="menu__langs">
          <summary class="menu__item"><img src="flags/gb.svg" alt="" width="20" height="15"><span class="visually-hidden">Language</span> <span class="menu__name">English</span></summary>
          <form method="post" action="/language">
            <ul>
              <li><button type="submit" name="lang" value="de" lang="de" class="menu__item"><img src="flags/de.svg" alt="" width="20" height="15" loading="lazy"> <span class="menu__name">Deutsch</span></button></li>
              …
            </ul>
          </form>
        </details>
        <form method="post" action="/theme">
          <fieldset class="menu__themes">
            <legend>Theme</legend>
            <ul>
              <li><button type="submit" name="theme" value="auto" class="menu__icon" title="Auto" aria-current="true"><span class="visually-hidden">Auto</span></button></li>
              …
            </ul>
          </fieldset>
        </form>
        <form method="post" action="/signout">
          <button type="submit" class="menu__item">Sign out</button>
        </form>
      </div>
    </details>
  </header>
  <main id="main" tabindex="-1">
    <nav class="crumb" aria-label="Breadcrumb"><ol role="list"><li><a href="/worlds">Worlds</a></li><li><span aria-current="page">Family base 2025</span></li></ol></nav>
    <h1>…</h1>
    <nav class="subnav" aria-label="World pages">…</nav>
    …
  </main>
  <div id="announcer" class="visually-hidden" aria-live="polite"></div>
</body>
```

- **The frame from 64rem** is a grid of two columns, the rail and the
  page, under one markup at every width:
  - **The rail** (the product rail, `--rail`, 240 px) runs the full height of the window on
    `surface` with a 1 px `line` on its end side. At its top sits the logo
    and name, over a faint drafting grid, in a 56 px cell. Under it, the
    four areas (Server, Worlds, Snapshots, Settings) are a column of 44 px
    rows: icon and word, `--text-md`, weight 500, in `text`. The current
    area has a `raised` face, a 1 px `line` inset edge and a 3 px
    `selected` edge on its start side, in `heading` at weight 600; a row
    under the pointer takes `secondary-hover`. Under the areas, after a
    hairline, the current area's own pages (see "Sub-navigation").
  - **The status strip** is 56 px tall across the top of the page's column,
    on `surface`, and sticks to the top of the window while the page
    scrolls (the logo cell and the user menu stick with it). It names the
    live world in `heading`, weight 650 (cut with an ellipsis when long),
    then the server's state as a content-sized 24 px pill in the state's
    colours (7.5, "The nav's state"), and ends with the user menu. A 2 px
    rule under it takes the state's colour: `ok` running, `info` starting,
    `warn` stopping, `danger` crashed, `line` otherwise.
  - **`main`** starts after the rail, left-aligned, at most `--page-max`
    plus `--space-6` padding on each side, so content starts at the same
    place on every page.
- **The header below 64rem** is a bar the full width of the window:
  background `surface`, a 3 px `primary` line across its top and a 1 px
  `line` under it. Its side padding follows `main`'s. It is a flex row
  that wraps. From 40rem it is one row: the logo and name at the start,
  the server's state in words after them (7.5, "The nav's state"), then
  the nav and the user menu at the end. Below 40rem it is two rows, about
  100 px at 320 px: the logo, the server's state in words and the menu,
  then the nav on a row of its own (`order: 2; flex-basis: 100%`), its
  four tabs sharing the row out between them. At 320 px with 200% text in
  German or Russian the tabs wrap onto a second row of that line rather
  than widen the page. The live world's name shows only in the wide
  strip. The focus order is the source order (logo, state, tabs, menu) at
  every width.
- **The logo** is the rusty Foreman logo, 32 CSS pixels square, from
  `static/icons/logo-32.png` with `logo-64.png` for 2x screens (both made
  from `docs/brand/foreman-logo.webp`). It is decorative beside the word
  "Foreman", so its `alt` is empty and the link's name is "Foreman" in
  every language. The name is `--text-lg`, weight 700. Below 22rem (a 320 px
  phone) the word is visually hidden and only the icon shows; the link
  keeps its name. The sign-in and setup pages also show the 64 px mark
  (`logo-64.png`, decorative, empty `alt`) centred above their heading.
- **Nav links below 64rem are tabs**: no underline, padded to
  `--target-min` tall, `text-muted`, weight 500, each with a line icon
  before its word (7.9). The current one is `heading` with a 3 px
  `selected` underline drawn as a border, so it survives forced colours;
  it is not bolder, so the tabs keep their widths from page to page. Hover
  brightens the text to `heading`; nothing else changes. Below 23rem the
  icons are dropped and the tabs
  are tighter (`--space-1` side padding) and a step smaller (`--text-sm`),
  so the four tabs fit one row at 320 px in English and German. The tab of the page itself has
  `aria-current="page"`; on a page inside its section (a world's page under
  Worlds, Console under Server) the tab has `aria-current="true"`, which
  looks the same but does not tell a screen reader it is on the list
  itself. There are four tabs: Server, Worlds, Snapshots and Settings. Console is a
  tab of the Server view (see "Sub-navigation"). Settings is Foreman's own
  settings (8.15): it opens General, and is marked on every settings page and
  the pages under them, "page" on General and "true" on the others. It once
  opened the restart schedule, as the one most used; Todd reversed that on
  2026-11-19 (#726), so the first tab is the one it opens. No page shows two
  navigation links with the same name, so a world's own settings tab is
  "Game settings", and a world's own Snapshots tab keeps its one word on
  screen but is named "Snapshots of this world" (an `aria-label` that
  starts with the shown word, WCAG 2.5.3).
- **The user menu** is a native `details` at the end of the header (from
  64rem, the end of the status strip). Its summary is a bordered button on
  `raised` (1 px `border`, `--radius`, `--text-sm`) that shows the user's
  name in weight 600 and the current language in `text-muted` ("todd
  English"), so a person who landed in a language they cannot read still
  finds it, and a small chevron drawn in CSS that turns over when the menu
  is open. From 64rem a round person badge leads the name; the language
  follows as quiet text. Its accessible name says it is the account menu
  ("todd, English: account menu"), and the language list is a row inside
  it. On the sign-in and setup pages
  it shows only the language. The two words wrap between them, never inside
  one. Inside, on a `surface` panel (1 px `border`, `--radius-lg`,
  `--shadow-pop`, at most 18rem wide): "Signed in as todd", then four
  rows, each under a divider line after the first:
  1. **Language**, one row: the current language's flag, its own name, and
     an arrow at the end that turns over when open. It is a `details`
     inside the menu, so it works without script. Opened, it lists the other
     seven languages under it, each a submit button to the same
     `POST /language` (SPEC 22.2) with its flag and its own name, marked
     with its `lang`. The summary's hidden words start with "Language"
     (plus "(Language)" in English off English), so a screen reader hears
     "Language English" and a person lost in a language they cannot read
     still finds the way back. The flags are decorative (`alt=""`): the
     United Kingdom for English, Spain for Spanish, and Germany, Russia,
     China, France, Poland and Czechia for the others, from flag-icons
     (STACK 3), 20 by 15 px with a 1 px `line` outline. The listed ones
     load lazily, so a closed list costs nothing.
  2. **Theme**, one row: the word "Theme" in `text-muted` at the start, then
     three icon buttons, Auto, Light and Dark (sun and moon, sun, moon), to
     `POST /theme` (3, "How themes work"), in a `fieldset` whose legend is
     "Theme" (the legend floats so it lays out as part of the row). Each is
     named in visually hidden words and in its `title`, so it has hover
     text; the current one has `aria-current="true"`, an `accent-bg` face
     and a 1 px `selected` edge.
  3. **About**, a link to the `/about` page, so it works without script.
     With script (`about.js`) it opens the same content in a native modal
     `dialog` (the leave box's look, `surface` on a dimmed page, 32rem at
     most): "About Foreman" as its heading, with the rusty riveted
     wordmark above the words inside it (`static/icons/wordmark-480.webp`,
     made from `docs/brand/foreman-wordmark.webp` at twice the 240 by 74
     px it is drawn, shrinking with a narrower box; decorative, empty
     `alt`, so the heading's name stays "About Foreman"); then a row of facts: the **Version**, a release tag, shown only
     when one reaches the build; the **Commit** (twelve characters of the
     hash, with "(modified)" after it when the tree had changes); the
     **Go version** it was built with; and the **Licence** (MIT). Then a
     line that sends the person to GitHub, and a Close button that takes
     focus. When the update check (SPEC 14.10) knows of a newer release, a
     note message comes first: "Foreman 1.3.0 is available. See what's
     new", the link opening that release's page on GitHub in a new tab.
     Everyone who can open About sees it; the user menu itself shows
     nothing. A build with neither version nor commit shows "development
     build" as its version. The browser keeps focus inside, Escape and
     Close shut it, and focus goes back to the menu's button (the menu
     closes as focus enters the dialog). The names Foreman and MIT are not
     translated, and no game versions are listed.
  4. **Sign out**, still a `POST` form with a button, never a link, so a
     crawler or prefetch cannot sign anyone out.
  The menu holds no links to Foreman's settings pages (the Settings tab is
  their one home); About is its one link. An item under the pointer takes `secondary-hover`.
  Each item is a 44 px target. It opens and closes with Enter or Space and
  works without script; `menu.js` also closes it on Escape (focus returns
  to the summary) and on a click outside. Escape with focus in the open
  language list folds the list first, focus back on its row; opening the
  menu folds the list, so the menu always opens with it folded. The panel is positioned under the
  header at the end edge and never wider than the window.
- The skip link is the first focusable element. It is visually hidden
  until focused, then appears at the top-left of the header.
- `main` holds one column: `max-inline-size: var(--page-max)`, centred,
  `padding: var(--space-5) var(--space-3) var(--space-6)` at phone width,
  `var(--space-6)` block padding from 40rem and `var(--space-5)` inline
  padding from 48rem. From 64rem it sits beside the rail as above, with
  `var(--space-6)` padding all round. Its children are a grid with `gap: var(--space-4)`
  (`var(--space-5)` from 40rem), and it is the container
  (`container-type: inline-size`) that components size themselves from.
  The page always keeps room for the scroll bar (`scrollbar-gutter:
  stable`), so the tabs do not shift sideways between a short page and a
  long one.
- **Sections are cards.** A `section` (class `panel`) is a heading and its
  content, with `gap: var(--space-3)` inside, drawn as a card (7.8). A
  page's sections are the cards in `main`'s grid. Some pages set cards
  side by side where there is room: the Server page's players, snapshots
  and pages (below), a Mods tab's list and its rail (8.11), a world's map
  exchange string and Make a favorite (8.2), the Saves list and Upload a
  save (8.3), the Users list and Your password (8.15), the public mods
  page's two cards (8.20), the factorio.com account page, and Generate's
  settings and preview (8.6).
- **Sub-navigation: one pattern everywhere** (ADR 0028). Any area with
  sub-pages lists them in one place and nowhere else, the product rail
  (below 64rem, a tab row under the heading): no page
  carries an in-page list of its own sections, and a page too long to read
  whole is split into sub-pages in that column (Game settings, 8.10). The
  lists are the Server view's Overview, Console and Chat (8.14); Foreman's
  own settings, eight pages (8.15); the Worlds area's All worlds,
  Favorites and Deleted worlds; and a world's Overview, Saves, Snapshots,
  Mods and Game settings, with Game settings' eight sub-pages nested under
  it while one is open. Each is a `nav` with its own name ("Server pages",
  "Foreman settings", "Worlds pages", "World pages") holding a list of
  links. In the markup the list sits in the rail right after the four
  areas, so from 64rem it is drawn there in flow, with no offset; its ids
  and URLs do not change with the width.
  - **From 64rem** the list is drawn in the rail, under the four areas,
    after a hairline, headed by a caption taken from the `nav`'s own name
    (`aria-label`, shown by CSS with empty alt text, 12 px Plex Sans
    capitals in `text-muted`). Each page is a 44 px row in `text` at
    `--text-md`, weight 500; the current page is marked as the current area
    is (a `raised` face, a hairline inset and a 3 px `selected` start
    edge, `heading`, weight 600). Game settings' sub-pages sit indented
    under it, behind a 1 px `line` on their start side, as 36 px rows at
    `--text-sm` in `text-muted`; the open one is `heading`, weight 600,
    with the same marked face, and Game settings itself then reads as a
    group title (weight 600, no face). Focus draws the ring 1 px inside
    the row.
  - **Below 64rem** each list is a tab row under the heading, with a 1 px
    `line` under it. On a phone (below 40rem) the Server, Worlds and world
    lists are an even grid of 44 px cells that shows every label whole,
    never a row that scrolls sideways or cuts a word: three tabs take three
    equal columns, four take two over two, and five take three over two (a
    six-column grid in which the last two cells are each half the width, so
    no tab sits alone). Each cell has its own 1 px `line` underneath, 4 px
    apart from its neighbour's, and the current cell's `selected` underline
    covers exactly that cell. The words are `--text-sm`, weight 600,
    centred, and wrap inside their cell (`--text-xs` below 23rem). From
    40rem the tabs are one row that wraps. Two lists differ on a phone:
    Foreman's eight settings tabs are one row that scrolls sideways, each
    tab whole, and `subnav.js` scrolls the current one into view; Game
    settings' sub-pages are a second row under the world's tabs, of 44 px
    pill-shaped chips (`surface` face, 1 px `line` edge; the current one
    `accent-bg` with a `selected` edge) that also scrolls sideways. From
    40rem to 64rem the chips wrap instead.
  - The current page's link has `aria-current="page"`. On a Game settings
    sub-page, the sub-page's link has `aria-current="page"` and Game
    settings has `aria-current="true"`.
- The `#announcer` region is where htmx swaps short results ("Snapshot
  taken", "Server started") so screen readers hear them. Errors are not
  announced from here; see 7.3.
- Viewport media queries are used for the shell (the rail and strip from
  64rem, header and `main` padding, card padding, the header's rows, the
  logo's word, the tab icons)
  and where a page's whole layout changes with the window: the Server page
  (below), Generate (8.6), the phone log (8.5), the world Overview and the
  account page (two columns from 64rem). Every other component lays itself
  out from its container with `@container` queries.

### Forced colours

Windows high contrast (`forced-colors: active`) replaces every fill and
colour with the user's system colours. The current tab's underline and the
current theme's edge are borders, and that mode draws them in
`Highlight`, so "selected" is still a drawn shape. The "Active" badge
keeps its border and its word, and buttons, fields and message boxes keep
their borders, so they need nothing more. The state badge's shapes (7.5)
are redrawn in `CanvasText` so their fills survive. The line icons (7.9)
are masks painted with a background, which this mode would replace with
the page colour, so the ones that carry words are repainted in system
colours: `ButtonText` for icons on buttons, `LinkText` for tab and shortcut
icons, and `CanvasText` for the icons before section headings. The rest
(chevrons, empty states, the map preview's held box) may disappear; each
one sits beside its words or has a hidden name. Anything new that marks a
state with a fill alone needs a rule here. Check a screen with Playwright's
`forcedColors: "active"`.

### The 320 px rules

These hold for every page (SPEC 17.2), checked in end-to-end tests at a
320 px wide viewport:

1. `document.documentElement.scrollWidth` is not larger than the
   viewport. No element may force horizontal scrolling of the page.
2. Text wraps. `body` sets `overflow-wrap: anywhere`, so a long name or
   file name breaks instead of widening the page; table cells after the
   first reset it (8.3). The shell grids use `minmax(0, 1fr)` columns.
   Long strings use `overflow-wrap: anywhere`. Flex children
   holding text get `min-width: 0`.
3. Every button and field is at least `--target-min` (44 px) tall and
   44 px wide. Links inside running text are exempt (WCAG 2.5.8 inline
   exception); a link standing alone, such as "Cancel", is padded to 44 px
   tall.
4. Adjacent targets are separated by at least `--space-2`, so two 24 px
   minimum targets can never overlap even when a person's text size
   shrinks things.
   The parts the server swaps in (`[sse-swap]`) and `#console-panel` are
   grids with `gap: var(--space-3)`, so a button is never flush under a
   step list, message or log.
5. Button rows wrap (`flex-wrap: wrap`). Each button may grow to the full
   width when its row wraps, so a phone shows them stacked in code order
   (primary first in an ordinary row, Cancel first in a foot bar, 7.1).
   A button in a form of its own (`.actions > form`) takes part in the row
   too: the form is `display: contents`, so "Take snapshot" stacks at the
   same width as the links beside it.
6. Tables follow 8.3. The log and console wrap their lines (8.5); they
   scroll vertically inside their own box, never horizontally.
7. Nothing is fixed to the viewport. A few one-row bars are sticky at
   the foot of their own card or form, so an action stays in reach: the
   Console's command bar on a phone (8.5; the field is in the bar), the
   save bars of long forms (Game settings, Mod settings, General settings,
   the webhook form and Generate) and the Snapshots selection bar. (The
   status strip and Generate's preview column are sticky too, but only on
   wide screens: the strip above the page, the column beside the form.)
   On a phone (30rem and narrower) Generate's stacked action bar stops
   sticking when the window is also 30rem tall or less, such as a phone on
   its side, so it never covers most of the page (WCAG 1.4.10).

### What a phone shows first

The vision's success test runs from a phone, so the most common task on
each page is on its first screen, at 320 by 640 px, without scrolling.
The order is the source order at every width, so reading order, focus
order and the picture agree.

- **Server page.** Under the "Server" overline, the `h1` is the active
  world's name, so what is running is the first thing read; its state
  badge is read by screen readers only, because the header (or the strip)
  and the plate show the state (7.5). Under it, when
  the operator set `FOREMAN_PUBLIC_ADDRESS` (SPEC 3.2), one line "Players
  join at host:port" with the address in `code` and a quiet "Copy address"
  button; the line is not there when the variable is blank, because
  Foreman never guesses the address. `copy.js` shows the button only where
  the Clipboard API works (HTTPS or localhost) and says "Address copied"
  beside it and through `#announcer`. Then the Overview, Console and Chat tabs,
  then the **Server plate** (7.5, the status card of 7.8): its band, which
  states the server's state large, then the controls (and while an
  operation runs, its step list and "Cancel stop"), then the facts. Below
  the plate come the live world's pages (World page, Mods, Game settings
  and Switch world), then "Players", then "Snapshots", in that
  order in the code too. The Players card (SPEC 8.1) is one table: online
  rows first, the name then an "Online" badge (`ok` text on `ok-bg`, in
  words) and the session in the time column; then the save's other
  players, name with "Last seen" and "Played before Foreman" hints under
  it and play time in the time column. The time column stays one line.
  At most ten rows, then an "All N players" link to the Overview list;
  short hint lines carry the inexact and "tracked since" notes. On a phone (to 40rem) the pages are a two-column
  grid of 48 px tiles with 8 px gaps (`surface` face, 1 px `border` edge,
  `--radius`, icon and name, no chevron), so the players start on the
  first screen. The Snapshots card lists the world's three newest
  snapshots, each its kind, its label and its time (filled on page load,
  not live), then an "All snapshots" link to the world's Snapshots tab, so
  the last safe point is in view before a restart or a stop; with none it
  says so. From 64rem the grid
  places the players card in a wide column on the left and stacks the
  pages card over the Snapshots card on the right (`grid-row` and
  `grid-column`, not `order`; two parts to one), each card ending after
  its own content; the pages are one card of four 56 px rows divided by
  hairlines (icon, name and a chevron). From 100rem the row is three
  columns: players, snapshots, then the world's pages. There is no list
  of other worlds: "Switch world"
  goes to the Worlds list, where every row has its Switch button, which
  makes a switch three clicks from the Server page (SPEC 17.10). After an
  operation, focus goes to the `h1` (`#control-heading`), which names the
  world and its new state.
- **Controls.** The row runs: download the image or Start, Take snapshot,
  Restart, Stop. Force stop is not in the row (below). Each has an icon
  (7.9). While the game runs, Take snapshot is the primary button, because
  it is the safe everyday action, and Restart and Stop stay secondary, Stop
  in danger-coloured words and edge (7.1); while it is stopped, Start is
  the primary one (7.1). From 64rem Stop server is set apart from the
  routine actions by a 1 px `line` rule before it.
- **Status facts** (7.5) sit in the card as a grid,
  `repeat(auto-fit, minmax(min(100%, 8.5rem), 1fr))` with
  `gap: var(--space-3) var(--space-5)`, two to a row when the card is under
  24rem wide, so they are two or three to a row at 320 px. Each is a small
  name in 13 px sentence case over its value at `--text-xl`, weight 600, in `heading`;
  the game version is set in the same face and size as the other values.
  Elsewhere `dl.facts--inline` is a wrapping flex row with `gap:
  var(--space-2) var(--space-5)` (the disk strip, 7.8).
- **Loads next.** While an older save is chosen (SPEC 7.11), a second
  `dl.facts--inline` under the facts says "Loads next" with the save's
  name as a quiet button-faced link to the Saves tab, named "old.zip, on
  the Saves tab" (a plain link measured 20 px tall on a phone, under the
  target floor). It is a region of its own (`loads`), sent only when it
  changes, because the facts are replaced whenever a figure changes and
  the link may have focus. It shows stopped or running; with no choice,
  the newest chosen, or the chosen file gone, it is empty.
- **Force stop** is a bordered link standing alone (rule 3), not a
  `.button`, drawn like the risky button (7.1): `danger-text`, a 1 px
  `currentColor` edge, 44 px tall, `danger-bg` under the pointer. At every
  width it folds under a closed "Emergency actions" disclosure (7.8) at
  the controls' foot, a quiet foldout like any other (summary in
  `text-muted`, `--text-sm`, weight 600), so a rare action that loses data
  takes no permanent row. Opened, it shows Force stop first in an area
  with a `danger-bg` face and a 1 px `danger` edge, with its consequence in
  one muted line beside it ("Stop the game at once, without warning
  players and without saving."). It leads to its own confirmation (7.4).
- **Console page.** The same overline and `h1` as the Server page, its
  state word again for screen readers only, then the Overview, Console and
  Chat tabs. It reads as a terminal at every
  width: one card holds the "Game log", then the "Console" section (its
  heading is visually hidden, because the page and tab already name it)
  with the command row and the past commands under it. On a phone the
  command row is a sticky bar at the card's foot (`surface` face, a 1 px
  `line` rule on top), so it stays in view while the page scrolls the log;
  on a 320 by 640 phone it is in view on the first screen once the card is
  reached. The history box is only as tall as what it holds (`block-size:
  auto`), up to `min(40vh, 20rem)`. The game log keeps its fixed height
  (8.5), so new lines never move the page.
- **World list** (8.2). Generate (primary) and Import sit at the end of
  the `h1`'s row and wrap under it on a phone. Favorites and Deleted worlds
  are not buttons: they are the Worlds area's pages, beside All worlds, in
  the product rail (6, "Sub-navigation"). Each row is a grid, `minmax(0, 1fr) auto`: the
  name heading in the first column and, when the world can be switched
  to, its "Switch" button beside it in the second (`.facts ~ .actions`
  moves to row 1). Everything else spans both columns, and the facts sit
  two to a row, so a world takes about 170 px at 320 px instead of 290.
  A long name wraps in its column; Switch keeps its width.

### Motion

Only `opacity` and `transform` may animate, for at most 150 ms. Under
`prefers-reduced-motion: reduce`, nothing animates, including the
indeterminate progress bar, which is hidden in that mode (`.steps
progress { display: none }`) because the browser animates it itself; the
"In progress" word carries the meaning. Nothing on these screens needs motion to be
understood.

## 7. Components

Each component lists its markup first; the styling follows from the
tokens. Classes are short, flat and named for what the thing is (`button`,
`field`, `message`, `state`). There is no utility-class system.

### 7.1 Buttons

```html
<button type="submit" class="button button--primary">Start server</button>
<button type="button" class="button">Take snapshot</button>
<button type="button" class="button button--quiet">Copy address</button>
<button type="submit" class="button button--danger">Delete world</button>
<a href="/worlds/family-base-2025/delete" class="button button--risky">Delete world</a>
<a href="/worlds/family-base-2025" class="button button--ghost">Cancel</a>
<a href="…" class="button button--remove"><span aria-hidden="true">Delete</span>
  <span class="visually-hidden">Delete snapshot Before restore</span></a>
```

| Kind | Face | Text | Border | Hover | Use |
|---|---|---|---|---|---|
| Primary (orange) | `primary` | `on-primary` | 1px `primary-edge` | face `primary-hover` | The one main action on a screen: "Start server", "Switch to this world", "Sign in", "Generate a world" on the Worlds page, "Take snapshot" on the Snapshots page and on the Server page while the game runs, "Send". At most one per screen. On the Server page Restart and Stop stay secondary (6, "Controls"). |
| Secondary (a raised plate) | `raised`, a light inset line along its top and a 1 px shadow under it; pressed in while active | `heading` | 1px `border` | face `secondary-hover`, edge `text-muted` | Every other action. The default. |
| Quiet (a link) | transparent | `link`, underlined | transparent | none | A lesser action that is still a button: "Copy address". Still a 44 px target. |
| Ghost | transparent | `heading` | transparent | face `secondary-hover` | The way out of a form or confirmation: Cancel in every foot bar and confirmation. A link drawn this way needs no class there. |
| Danger (red) | `danger` | `on-danger` | 1px `danger` | face `danger-hover` | Only the final, confirming button of a destructive action (7.4). |
| Risky (outlined red) | transparent | `danger-text` | 1px `currentColor` | face `danger-bg` | The button that opens a destructive step: "Delete world" on a world's page, "Delete selected" on the Snapshots pages. Force stop is a bordered link drawn the same way. |
| Remove (red ghost) | transparent | `danger-text`, a trash icon (a cross for Revoke) before the word | transparent; `currentColor` under the pointer | face `danger-bg` | Every destructive action in a row: Delete on a snapshot, a favorite, a webhook or a user, and Revoke on a token. The word shows; the row's name is hidden text. |
| Icon only | transparent | `danger-text` or `text` | transparent | face `danger-bg` | Only where a word does not fit: a plan row's remove (a cross) and a message's close button. A 44 px square; its name is hidden text. |

All buttons: `min-block-size: var(--target-min)`, `min-inline-size:
var(--target-min)`, `padding-inline: var(--space-4)` (`--space-2` for
quiet, `--space-3` in a mod's row and for ghost and remove), `--text-md`,
weight 600, `--radius`, `cursor: pointer`. Buttons in a row of a list or
table are sized to their words; from 64rem they are 40 px tall, and with
a fine pointer (`pointer: fine`) 36 px, while a touch screen keeps
44 px. While
pressed (`:active`) a button moves down 1 px with `transform`; under
`prefers-reduced-motion: reduce` nothing moves. A label wraps between
words inside the button (it never runs past the edge) when large text or a
long translation needs two lines; only a single word wider than the button
is broken (`overflow-wrap: break-word`, hyphenated where the browser can);
the block padding is `--space-2` so a two-line label is not cramped.

- Hover is a colour change only; it is never the sole sign of anything,
  because a phone has no hover.
- **Stop server** on the Server page stays a secondary button, but its
  words, its stop icon and its edge are `danger-text` and `danger`, and its
  face turns `danger-bg` under the pointer, so it reads differently from
  Restart server: stopping disconnects every player. Take snapshot is
  still the one filled orange button. The component page shows the same.
- **Force stop** on the Server page is not a button at all: it is a
  bordered link drawn like the risky button, at the foot of the controls
  under a dashed danger rule (section 6, "What a phone shows first").
- A button that leads to a confirmation step, such as the first "Delete
  world" on a world's page, is **risky** (outlined red) or a red ghost
  (remove), not the filled danger button. Filled red is kept for the moment the thing
  actually happens, so it still means something; the outline says "this
  opens something destructive" without that weight.
- **Destructive means something is lost**: a world, a snapshot or a save
  deleted, a restore over a world, a mod removed, what a mod built dropped
  from the save, a forced stop. A change that only stops and starts the
  game again, such as installing or updating a mod while it runs, is not:
  its button is primary, and its steps say in words that the game restarts.
- **One row-action rule.** In a row of a list or table, a safe action is
  a secondary button sized to its word, and a destructive one is a red
  ghost with an icon and a word (remove, above), never a bare icon or
  plain red text. Remove in a Mods tab row is the one exception: it is the
  last item of the row's More actions menu, in `danger-text` (8.11).
  Either opens the confirmation, so the filled danger button is still
  kept for the confirming step (7.4).
- **Links take you somewhere; buttons do something.** A link to another
  tab or page inside Foreman is plain underlined text ending in a chevron
  (class `link-tab`, drawn in CSS from `--i-chevron`), never a button: the
  three links to another tab's upload (Mods to Saves, mod settings to Mods,
  an empty Worlds list to Import) are of this kind. A link that leaves
  Foreman opens in a new tab, says so in hidden text ("opens in a new tab"),
  and ends in `↗`. One rule on `a[target="_blank"]` draws the arrow, so every
  link out has it, including a mod's title linked to the portal. Both marks
  are drawn in CSS with empty alt text, so neither is read aloud or part of
  the link's name. A link is still a 44 px target. `<a class="button">` is
  kept for the few links that are drawn as actions in their own right
  (Force stop, the risky "Delete world" link above). Anything that changes
  state is a `<button>` inside a `<form method="post">`.
- **No disabled buttons to hide a reason.** If an action is not available
  ("Delete world" on the active world, SPEC 5.10), leave the button out
  and say why in text: "This is the active world. Switch to another world
  before deleting it." Disabled buttons cannot be focused and cannot
  explain themselves.
- **While a request runs**, htmx sets `disabled` on the button
  (`hx-disabled-elt="this"`) and Foreman changes its label to the
  present participle ("Starting…"). The button keeps its size, so nothing
  shifts. The busy state is the only time a button is disabled. The one
  exception is a button the person pressed that must keep focus, such as
  Preview map (8.8): it is marked `aria-disabled="true"` and a click on it
  does nothing. The second case is "Delete selected" on the Snapshots
  pages: with script (`ticks.js`) it is disabled until a snapshot is
  ticked; without script it stays enabled and the server says nothing was
  ticked. A disabled button is drawn at half opacity with a not-allowed
  cursor.
- A row of buttons is a flex container with `gap: var(--space-3)` and
  `flex-wrap: wrap`. In an ordinary row the primary button comes first.
- **One completion footer.** Every form foot bar and confirmation ends
  with its choices together at the end edge: Cancel, a ghost button, just
  before the primary (or danger) action, as on step 3 of Generate (Cancel,
  Preview map, Save map settings) and the mod plan. A note about what is pending
  (unsaved changes, where a setting comes from) leads the bar at its start.
  A confirmation's row sits under a 1 px `line`. Cancel comes first in the
  markup too, so the screen order and the keyboard order agree.

### 7.2 Form fields

```html
<div class="field">
  <label for="world-name">World name</label>
  <p class="hint" id="world-name-hint">Shown in the world list. You can change it later.</p>
  <input id="world-name" name="name" type="text" required
         aria-describedby="world-name-hint world-name-error"
         aria-invalid="true" autocomplete="off">
  <p class="error" id="world-name-error"><strong>Error:</strong> Enter a world name.</p>
</div>
```

- The `div.field` is a grid with `gap: var(--space-2)`: label, hint,
  field, error, top to bottom. The hint sits above the field so it is read
  before typing, and so a phone keyboard does not cover it.
- The label is always visible, `--text-md`, weight 600. A placeholder is
  never the label. It may give an example of what to enter where one
  helps ("Family base"), in `text-muted`, which meets text contrast on
  `deep` in both themes (10.17:1 dark, 8.91:1 light).
- **Required fields** have the `required` attribute, which screen readers
  announce, and a red asterisk after the label: `<label class="required">`,
  whose `::after` is `content: " *" / ""`. The empty alt text keeps the
  asterisk out of the label's accessible name, so "required" is not said
  twice and the name is still the label's words. The asterisk is not colour
  alone: it is a shape. A required group puts the class on its `legend`. Each form with a required field shows the note
  once, before its first field (`{{template "required-note"}}`): an
  asterisk and the word "Required", translated. A field the server
  refuses when empty is marked; an end-to-end check fails when one is not.
  The one exception is the Console's command field (8.5): it is the only
  field of a form that exists only to send it, so it has no asterisk and
  no note, and keeps only the `required` attribute.
- Text inputs, `select` and `textarea` are sunk wells: background `deep`,
  a faint inset shadow along the top, text `text`, 1px `border`, `--radius`, `min-block-size: var(--target-min)`,
  `padding-inline: var(--space-3)`, `--text-md`, `inline-size: 100%` up to
  a `max-inline-size` that suits the content: `40ch` for names and text.
- **Number fields** (`inputmode="numeric"`, and `.field--short`) are 10
  characters wide plus their padding, whatever the class: a countdown,
  a port, an interval, a seed. The style sheet sets this from the
  attribute, so a template needs no class.
- A field and the one button that acts on it may share a row
  (`div.inline-row`): the countdown and "No warning", a snapshot label and
  "Take snapshot". The row wraps when it does not fit.
- Fields that hold game text (a world id, a seed, a map exchange string, a
  console command) use `--font-mono`.
- Hint: `--text-xs` (13 px) at a 1.5 line height, `text-muted`, held to
  66ch, so it reads as help under its label and not as content. Only
  present when it says something the label cannot. It is shown under the
  label, except in the Console's sticky command bar on a phone, where it
  is visually hidden and still linked (8.5).
- Error: shown only after a submit fails. `--text-sm`, `danger-text`,
  starting with the bold word "Error:" so the message is not colour alone.
  The field gets `aria-invalid="true"` and a 2px `danger-text` border
  (the extra width keeps the edge visible without colour).
  The error says what to do: "Enter a world name", "Choose a save file
  that ends in .zip", "Passwords must be at least 12 characters", never
  "Invalid input".
- Use the `type`, `autocomplete` and `inputmode` that match the data
  (`autocomplete="username"`, `"current-password"`, `"new-password"`;
  `inputmode="numeric"` for a port) so phones show the right keyboard
  and password managers work.
- **Checkboxes and radios** are native, with `accent-color:
  var(--color-selected)`, so a checked box is orange like any other
  selected thing, and `inline-size` and `block-size` of 1.25rem. The
  input sits inside its `label`, and the label is a flex row with
  `min-block-size: var(--target-min)`, so the whole row is the target. The
  box styling above never applies to them, even inside a
  `div.field`: the `.field` rule excludes `[type="checkbox"]` and
  `[type="radio"]`, so the input stays 1.25rem square.
- A group of radios or checkboxes is a `fieldset` with a `legend` styled
  like a label. The save that loads next (SPEC 7.11) is chosen with a "Load next" button on each row of the saves table (DESIGN 8.3).
- **File inputs** are native, inside the same 44 px box as other
  fields. `::file-selector-button` is styled as a secondary button (7.1):
  transparent face, text `text`, weight 600, `secondary-hover` on hover, `padding-inline: var(--space-4)`, a 1px `border` rule on its
  inline end, and filling the box's full height so it stays a 44 px target.
  The hint names the accepted type and size limit.

**Error summary.** When a submitted form has errors, the page shows a
message (7.3, error kind) at the top of the form, headed "Fix N problems
to continue" (or "Fix this problem to continue"), with a list of links,
one to each field with an error, using the same words as the field's
error. On a full page load the summary receives focus (`tabindex="-1"`);
after an htmx swap, the swapped fragment moves focus to it with
`hx-on::after-swap`. Focus on the summary is how a screen reader user
learns the submit failed.

### 7.3 Messages

One component with four kinds, the same on every page. Each starts with a
bold word, so the kind never depends on colour.

```html
<div class="message message--error" role="alert">
  <p><strong>Error:</strong> The server did not answer over RCON within 5 minutes.
  It is still running. <a href="/logs">Read the last log lines</a>, then try Restart.</p>
</div>
```

| Kind | Word and icon | Tone (word, icon, 4 px edge) | Face | When |
|---|---|---|---|---|
| Error | "Error:", a circled cross | `danger-text` | `danger-bg` | Something failed. Says what, and what to do next. |
| Warning | "Warning:", a triangle | `warn` | `warn-bg` | Something will go wrong unless acted on ("Snapshots use 41 GB and the disk has 3 GB free"). |
| Success | "Done:", a tick | `ok` | `ok-bg` | An action finished and the result is not otherwise visible. |
| Note | "Note:", a circled "i" | `info` (blueprint blue) | `info-bg` | Context that changes how a person reads the page ("Snapshots are using full copies, not hardlinks", SPEC 6.8). |

- Body text inside a message is `text`, not the kind colour; only the
  leading word is coloured. Links inside are `link` and underlined.
- The face is the kind's tint, with a 1 px `line` hairline all round and
  a 4 px edge at the start in the kind's tone; the edge is a border, so it
  survives forced colours. A 20 px line icon in the tone sits at the start
  of the first line (repainted `CanvasText` in forced colours), and the
  text keeps clear of it. Padding `var(--space-3) var(--space-4)`,
  `--radius`. A message as a direct child of `main`, of a card or of the
  settings column is as wide as its text, up to 52rem. A message has no
  shadow, so it never looks like a control. No close button: a message
  stays until the page changes. The one
  exception is the Server page's Done box after a success or a cancelled
  stop (SPEC 17.9). It carries Undo or Switch back, which can stay on the
  page for hours, so the person may close it once read. It has an icon-only
  close button ("×", named "Close this message", a 44 px target) at the
  box's top end corner, and the message's text keeps room for it (the button's width and a gap, on the end side), so no line runs under it. A failure's box never
  has one: it stays until the next action, so a problem is not hidden by
  accident.
- `role="alert"` only on an error that appears after an action without a
  page load. A message that is part of a freshly loaded page has no role.
  Success messages from htmx go to `#announcer` instead (section 6), with
  the visible message rendered beside the thing that changed.
- On the Server page, the unfinished-switch notice (SPEC 7.10) and the
  game-network warning (SPEC 4.4) are Warnings in the page, outside any
  live region. The notice sits in the control panel with its Switch back
  button after it; the network warning sits with the status facts.
- **Empty states** are a plain paragraph in `text-muted`, centred under a
  drawing of the thing that is missing, as a plan not yet built: its line
  icon (3rem, in `info`) over a 160 by 96 px tile with a dashed `info`
  edge, an `info-bg` face and a faint 16 px drafting grid. It is
  decorative. The icons: a camera for snapshots, a globe for worlds, a box for mods,
  a terminal for the console, people for players, a clock for saves, a
  trash can for deleted worlds, sliders for settings, a speech bubble for
  chat (8.14), a bell for notifications (8.16). Never a picture:
  "No worlds yet. Import a folder of saves or upload a save file to make
  your first world." followed by the action buttons. The empty state always
  names the next step. It is the `empty empty--art empty--<thing>` classes
  on the paragraph.
- **A state plate** says once whether a feature is on or off, with its
  one control at the end (`state-plate`; MCP access on the API tokens
  page, 8.21; the public mods page's state sentence, 8.20, is drawn the
  same way). It is a box on `surface` with a 1 px `line` hairline and a
  4 px start edge in the state's colour: `ok` when on; when off, `warn`,
  with a `warn-bg` face. The heading carries its On or Off badge, then the
  state sentence, then the one Turn on or Turn off button.
- **A mod's portal news** (the availability flags in a Mods row, 8.11) is
  one quiet 13 px line in `text-muted` per flag, led by a 14 px icon for
  its kind. The leading word ("Error:", "Warning:", "Note:") stays in the
  markup for screen readers and is visually hidden, so the exceptions do
  not read as paragraphs between mod names.
- **Loading.** Server-rendered pages arrive complete, so most screens
  have no loading state. Where htmx loads part of a page later (live
  status, player list), the slot reserves its final height with
  `min-block-size` and shows "Loading…" in `text-muted`, so nothing below
  it moves when the content arrives.

### 7.4 Confirming risky actions

SPEC 17.5: a destructive action says exactly what it will do and which
snapshot will be taken first, and needs a confirmation. SPEC 6.2: the
snapshot comes first, and if it fails nothing else happens.

The confirmation is **a step, not a pop-up**. The first button
(secondary, 7.1) goes to a confirmation page at its own URL, such as
`GET /worlds/{id}/delete`. With htmx the same fragment can replace the
action area in place (`hx-get`, `hx-target`, `hx-push-url`), and focus
moves to its heading. Without JavaScript the page still works. No
`window.confirm()`, which cannot say enough, and no modal dialog, which
is harder to get right on a phone and adds nothing here.

**The one exception is the Generate page's leave dialog**, because
leaving there throws away typed settings and no step can come between a
link and its page. When the settings are unsaved, a plain click on a link
or the submit of a form other than Generate's (language, Sign out) opens a
native `dialog` with `showModal()`, styled like the confirmation box (a
`surface` face, a 1 px `border`, `--radius`, padding `--space-4`) with a
`::backdrop` that dims the page (black at 50%), and checked at 320 px. Its name is the question, "Leave without saving these
map settings?"; one sentence says the new world's settings are not saved;
"Stay on this page" (primary) has focus on open and "Leave without
saving" (secondary) goes on. Stay and Escape close it and return focus to
what was used, or to the user menu's button when that was a button inside
the menu, which has closed. Reload, closing the tab, the address bar and
Back keep the browser's own question (`beforeunload`), which Leave
silences for the navigation it starts (SPEC 5.6, 17.15).

The page's one `h1` is the question, first in `main` (after the
breadcrumb on the mod plan and a favorite's delete step, 8.1), with no
"Back to" link above it and no second heading inside the box. The box
holds only "This will:", the list and the form. The world tabs (6,
"Sub-navigation") and Cancel are the ways out.

```html
<h1 id="confirm-heading">Delete the world "Family base 2025"?</h1>
<nav class="subnav" aria-label="World pages">…</nav>
<section class="confirm">
  <p>This will:</p>
  <ul>
    <li>Delete the folder <code>worlds/family-base-2025</code>, with its 14 saves,
        38 mods and settings (2.1 GB).</li>
    <li>Keep its snapshots. They stay listed under Snapshots and can be restored.</li>
  </ul>
  <form method="post" action="/worlds/family-base-2025/delete">
    <label class="check">
      <input type="checkbox" name="final_snapshot" checked>
      Take a final snapshot first (about 2.1 GB, kept until you delete it)
    </label>
    <div class="field">
      <label for="confirm-name">Type the world's name to confirm</label>
      <input id="confirm-name" name="confirm_name" type="text" required autocomplete="off">
    </div>
    <div class="actions">
      <a href="/worlds/family-base-2025">Cancel, keep the world</a>
      <button type="submit" class="button button--danger">Delete world</button>
    </div>
  </form>
</section>
```

Rules:

- The box is a section drawn as a card with a red frame: background
  `surface`, a 1px `danger` border, a 6 px hazard stripe of `danger` and `danger-bg`
  across its top (in forced colours a 4 px top border instead), `--radius-lg`,
  `--shadow-card`, padding `max(var(--space-4), var(--card-pad))`, a grid
  with `gap: var(--space-3)`. It carries no landmark name of its own, so it
  does not repeat the `h1`. The hazard stripe is kept for confirmations
  that destroy something: a mod plan that destroys nothing (8.11) keeps
  the frame but has a 4 px `info` (blueprint blue) top edge instead
  (`confirm--plain`). Besides the Danger zone (below), it is the only
  place where danger colour frames an area larger than a button.
- **The heading is a question that names the thing**: the world, the
  snapshot, the save. Never "Are you sure?".
- **The list says what will happen, in order, with real numbers**: which
  files, how many, how large, whether the server stops and for how long,
  what players will see. If anything is lost, say so plainly: "Progress
  since the last save, 4 minutes ago, will be lost" (forced stop, SPEC
  7.7).
- **The snapshot line says which snapshot.** Where the snapshot is
  automatic (SPEC 6.2), it is a plain list item: "First, Foreman takes a
  snapshot of Family base 2025, labelled 'Before restore'. If the
  snapshot fails, nothing else happens." Where the spec lets the admin
  choose (the final snapshot on delete, SPEC 5.10), it is a checkbox,
  checked by default. The exact label format belongs to Epic 2.
- **The confirming button repeats the verb** of the first button ("Delete
  world", "Restore snapshot", "Force stop"), styled danger. "Cancel" is a
  link back drawn as a ghost button (7.1), 44 px tall, set just before the
  confirming button at the end of the row, under a 1 px `line` (7.1, "One
  completion footer"). Its text says what cancelling keeps where that
  helps.
- **Typed confirmation** is only for deleting a world (SPEC 5.10). The
  server compares the typed name and, if it differs, re-renders the page
  with a field error ("Type Family base 2025 exactly, or cancel"). Other
  confirmations are one click, so typing does not become a habit.
- **Undo is named.** When the action can be undone from a snapshot, the
  confirmation says how. Where the result offers a one-click Undo (a
  restore, a rollback, an update, a switch, a mod change), it says so, as
  the mod plan does: "When it is done, a one-click Undo appears with the
  result, which restores that snapshot." Where the result offers no such
  button (a settings change, "start anyway"), it says where to restore:
  "To undo, restore that snapshot from the Snapshots page."
- **Undoing a mod change says what it undoes.** The Undo button of a mod
  change opens the restore confirmation (SPEC 6.5), headed "Undo the mod
  changes in <world>?". Its first list names the mods the undo removes,
  puts back, returns to an earlier version, or turns back on or off, one
  line each, ahead of the usual "This will" list. After the second and later
  change of a run on a stopped world the button reads "Undo the mod changes
  since <time>" and the Done text says no new snapshot was taken. When a
  mods folder cannot be read, the page is the plain restore confirmation.
- **The Mod settings editor's form is its own confirmation** (SPEC 9.6),
  with no page between the form and the change, but it keeps the
  confirmation's key facts short so they stay near the save action. Its
  last card, "Save the settings", carries the countdown while the game
  runs and a two-line summary of what saving does: on a running world
  that it warns the players and restarts the server, with a snapshot
  first; otherwise that it takes a snapshot first and then writes the
  changed settings; and in both that Undo restores that snapshot. The
  full "This will" list folds under a closed "What saving does"
  disclosure (7.8). The save button ("Save and restart", or "Save
  settings" on a stopped world) sits with Cancel in the sticky save bar at
  the form's foot (6, "The 320 px rules", rule 7).
- After the action, the result page shows a Done message (7.3) naming
  the snapshot that was taken, linked.

**The Danger zone.** On a world's Overview the last card is the Danger
zone, set apart from the safe actions above it: what takes the world out
of use or destroys it. It is a card of setting rows on `surface` with a
1 px `danger` edge and its row headings in `danger-text`, whether or not
its actions can be taken now: a lighter fill read as disabled rather than
dangerous. While the world can be deleted, a 6 px hazard stripe of
`danger` and `danger-bg` also crosses its top (in forced colours a 4 px
top border); while it cannot, there is no stripe. Its rows are
Archive world (when the world is not archived, newer or unreadable; on
the active world its line gives the reason and offers a "Switch world"
link to the Worlds list in place of the button) and Delete world (one
line on what deleting does and the risky "Delete world" button, 7.1;
when the world cannot be deleted, because it is active, newer than this
Foreman reads or unreadable, its line gives the reason and there is no
button; when the reason is the running game, a link to the Server page
takes its place).

Which actions need this: every operation SPEC 6.2 counts as risky, plus
delete (5.10), restore (6.5), forced stop (7.7), "start anyway" after a
failed pre-start check (7.4, which must name exactly what the game will
delete), switching worlds (7.9), and kicking or banning a player (8.12).
Promote, demote, unban and whitelist changes act at once and offer Undo
instead (8.12, 8.13). Start and restart anyway have no page of their
own: the Warning that reports the refusal on the Server page is the
confirmation (SPEC 7.4). It states the problem (one problem is a
sentence after the lead; several are a list), then a row with the main fix
first, such as "Match the mods to the save", and the restore of the
snapshot the action took second. Going ahead anyway sits last, in a closed
`details` drawn as a disclosure row (7.8; "Or start anyway"): opening it shows the steps, the snapshots
and the undo as above, and its danger button posts the action at once. It
never tells the admin to copy files on the host. Restart uses the default 60-second countdown there. A
switch has its own confirmation page (SPEC 7.9, 17.8) that shows the new
world's problems before anything runs, and offers "Switch, leave stopped"
and, when every problem allows it, a danger button "Switch and start
anyway". The page carries the countdown field only when the game is
running. The Server page's own switch box appears only when problems arise
after that page. A graceful stop or restart does not need a danger
confirmation, because nothing is lost. Its button opens a short form at its
own URL (`GET /worlds/{id}/stop` or `/restart`) that asks for the
countdown (SPEC 7.6): a text field for seconds, 0 to 600, filled in with
60, where 0 means no warning (there is no checkbox); beside it a "No
warning" button sets the field to 0 and moves focus there. That button
needs script, so without it the button stays hidden and the hint says to
type 0. Then a line names who
is online. The form is a plain `panel` with a
primary button ("Stop server", "Restart server"), not the danger box, and
its Cancel link says the server keeps running. The result is shown on the
Server page.

### 7.5 Server-state badge

SPEC 7.1 and 17.4: state is never shown by colour alone.

```html
<span class="state state--running">Running</span>
```

| State | Word | Text | Background | Shape (drawn in CSS) |
|---|---|---|---|---|
| stopped | "Stopped" | `text-muted` | `raised` | Hollow square |
| starting | "Starting" | `warn` | `warn-bg` | Square, left half filled |
| running | "Running" | `ok` | `ok-bg` | Filled square, with a faint ring |
| stopping | "Stopping" | `warn` | `warn-bg` | Square, right half filled |
| crashed | "Crashed" | `danger-text` | `danger-bg` | Filled diamond (the square turned 45 degrees) |
| crash-looping | "Crash-looping" | `on-danger` | `danger` | Filled square; the whole badge is solid |
| unknown | "Unknown" | `text-muted` | `raised` | Hollow circle; the badge edge is dashed |

"Unknown" is for when Foreman cannot ask Docker (SPEC 7.1). It uses the
same pair of colours as "Stopped" (7.84 on dark and light), so
no new contrast row is needed; the circle and the dashed edge tell the
two apart without colour.

Classes: `state` (the badge, also the "Stopped" look) plus one modifier,
`state--stopped`, `state--starting`, `state--running`, `state--stopping`,
`state--crashed`, `state--crash-looping` or `state--unknown`. The element
holds only the word. A template always writes the modifier, even for
"Stopped", so the class names a state.

- The word is the badge's content. It is real text, read by screen
  readers and kept in forced-colours mode.
- The shape is a 0.625rem square (2px radius) drawn with `::before`,
  `content: ""`, `border: 2px solid currentColor` and a background or
  `linear-gradient` hard stop at 50% for the half fills (a hard-stop fill,
  not a visual gradient). It is a second cue for people scanning a list,
  and it is decorative: the word alone carries the meaning.
- The fills are `background-image`, not `background-color`, because
  forced-colours mode (6) removes background colours. A rule there draws
  the shape in `CanvasText`, so "Running" stays a filled square and
  "Stopped" a hollow one. The solid crash-looping badge gets a 3px edge.
- `display: inline-flex`, `gap: var(--space-2)`, `--text-sm`, weight 600,
  padding `0.125rem 0.625rem 0.125rem var(--space-2)`, 1px border in
  `currentColor`, fully rounded (a pill). Sentence case, never all
  capitals. The running square has a static 3 px ring of its own colour at
  25%, which is decoration.
- Crash-looping is filled solid because it is the one state that needs a
  person to act (SPEC 8.9: Foreman has stopped the game and will not
  restart it alone).
- On the Server view's three tabs the badge sits in the `h1`, after the
  world's name, inside `<span id="server-title">`. The live "facts" event
  carries a copy of that span marked `hx-swap-oob="innerHTML"`, so the
  heading follows the game without a second event and without replacing
  the `h1`, which may have focus. On all three the heading's badge is
  visually hidden and still read by screen readers: the header (from
  64rem, the status strip) states the state on every page, and on
  Overview the plate's band states it large. No page title carries a
  second visible state badge.
- **The Server plate** is the Overview's status card (`card status-card
  plate`, 7.8). Its band (`plate__band`, at least 64 px tall, on `raised`,
  a 1 px `line` under it) leads with the state word at `--text-2xl`,
  weight 700, the largest thing on the plate, in the state's colour with
  the badge's shape (0.875rem) before it, behind a 6 px rule at the
  band's start edge in the same colour: `ok` running, `info` (blue)
  starting, `warn` stopping, `danger` crashed or crash-looping, `border`
  otherwise. So the state reads from across the room and is said once per
  screen. After the word come the uptime and who is online. The rule is a
  border, so forced colours keeps it. The plate has no corner marks or
  rivets: its state edge is its one mark. The riveted logo in the header
  is unchanged. The plate's gauges run the width of their cell, up to
  20rem, 8 px tall.
- **The nav's state.** Every page's header says the server's state in
  words, with the badge's shape as a small lamp (a 0.625rem square in the
  state's colour) before the word, so no state is told by shape or colour
  alone (`site-state`, `--text-sm`, weight 600). Below 64rem the lamp and
  word sit in the header's first row, after the product name, in
  `text-muted`; the word takes the room between the logo and the tabs (on
  a phone, the menu), and a long state wraps there instead of widening the
  header; the lamp stays with its word (a no-break space). From 64rem they
  sit in the status strip after the live world's name (6, "Shell"), as a
  pill as wide as its word, 24 px tall, in the state's pair (`ok` on
  `ok-bg` running, `info` on `info-bg` starting, `warn` on `warn-bg`
  stopping, `danger-text` on `danger-bg` crashed; `heading` on `raised`
  otherwise), with a hairline edge in the state's colour. Server's tab
  shows only "Server" and keeps the state as visually hidden text, so its
  name is "Server Running", its words in the order they are read (WCAG
  2.5.3). The copy beside the logo is `aria-hidden`, so the state is heard
  once. The Server page's live facts replace both copies, so the header
  follows the game without a reload.
- **The live world's chip.** In the Worlds list the live world's mark and
  the server's state are one chip, never two pills side by side: a pill on
  `raised` with a 1 px `border` edge holding the pin and "Active", a
  hairline, then the state's shape and word in its colour (`live-chip`,
  `--text-sm`, weight 600), inside the world's heading (8.2). A world's
  own pages (Overview, Saves, Snapshots, Mods, Mod settings and Game
  settings) show only the visible "Active" badge in the `h1`; the state is
  in the header or strip. The component page shows the plain state badges
  and the chip.
- The other status facts (SPEC 7.2) are a `dl` after the controls:

  ```html
  <dl class="facts">
    <div><dt>Uptime</dt><dd><time datetime="PT3H12M">3 h 12 min</time></dd></div>
    <div><dt>Active world</dt><dd>Family base 2025</dd></div>
    <div><dt>Game version</dt><dd><code>2.0.72</code></dd></div>
    …
  </dl>
  ```

  The `dl` is a grid in the Server page's status card (section 6, "What a
  phone shows first") and a grid, `repeat(auto-fit, minmax(min(100%, 7rem),
  1fr))`, on a world's page; both are two or more to a row at 320 px.
  `dt` is `--text-xs` (13 px) Plex Sans, weight 600, `text-muted`, in
  sentence case; `dd` is
  `--text-md`, weight 500, in `text`, with tabular figures. On a world's
  Overview the facts card reads as a nameplate: its values are
  `--text-lg`, weight 600, in `heading`. In the world list a fact's value
  is 13 px Plex Mono (8.2). A second figure
  under a value (`fact-note`, such as what a world's snapshots add) is
  `--text-xs`, weight 400, `text-muted`.
- **Play time** (SPEC 8.15) sits below the Server page's facts grid: a
  "Play time" fact in hours and minutes (hours are not folded into days)
  and a quiet link "N players in this save" to the players panel on the
  world's Overview (8.2). It is a live region of its own, because its link
  takes focus, and is sent only when its HTML changes. It says nothing
  until a record exists, and "Being read" while the first read runs.
- When the state changes live (SPEC 16.3), the server sends the new
  badge, and a short sentence goes to `#announcer` ("Server is now
  running"). Uptime, CPU and memory update silently; they must never be
  in a live region, or a screen reader would talk every few seconds.

### 7.6 Long-running operations

Start can take up to 5 minutes (SPEC 7.3); restore and, later, switching
run several steps. While one runs, the page shows an ordered list of the
steps, each with a word:

```html
<ol class="steps">
  <li><span class="step-state step-state--done">Done</span> <span class="step-name">Snapshot taken</span></li>
  <li aria-current="step"><span class="step-state step-state--progress">In progress</span> <span class="step-name">Waiting for the game to reach InGame</span>
    <progress aria-label="Waiting for the game"></progress></li>
  <li><span class="step-state">Waiting</span> <span class="step-name">Check RCON answers</span></li>
</ol>
```

Step words are "Done", "In progress", "Waiting", "Failed" and "Skipped",
coloured `ok`, `warn`, `text-muted`, `danger-text` and `text-muted`. A step
the pre-start check refused reads "Refused", coloured like its message:
`warn` (`step-state--warning`) when the message offers going ahead anyway,
`danger-text` otherwise. The
in-progress step may carry a native `<progress>` with no value
(indeterminate). A failed step is followed by an error message (7.3)
that says what to do next, as SPEC 7.9 requires for switching.

Classes: `steps` (the `ol`; no numbers, one row per step), `step-state`
(the word) and its modifiers `step-state--done`, `step-state--progress`
and `step-state--failed`, and `step-name` (the step and any time left,
"Warn players: 40 seconds left"). "Waiting" and "Skipped" take no
modifier. The current step has `aria-current="step"` and is bold.

Each `li` is a grid, `6rem minmax(0, 1fr)`: the word in the first column
and the step beside it in the second, at every width down to a 14rem
panel, so a step is one row even at 320 px and an 11-step switch fits in
about 300 px rather than twice that. In a panel under 14rem (200% text on a
phone) the word sits above the step instead. A long step name wraps inside its own column, under
itself, never under the word. The `<progress>` sits in the second
column, under the step it measures. The list sits under the operation's
heading in the "Start and stop" section, which leads the Server page
(section 6, "What a phone shows first"), so the step in progress and
"Cancel stop" are on a phone's first screen. Steps that are done stay in
the list: it is the record of what happened (SPEC 7.10), and it is
redrawn live, so a collapsed part would close again on every update.
A failed step's message goes inside that `li` as
`<div class="message message--error">` (or `message--warning` for a refusal
with a choice); it spans both columns (`grid-column: 1 / -1`) at any
width, up to the message's reading width. Text inside a message fills that one width,
so a paragraph and a nested list item wrap alike. The words carry the state, so forced
colours needs no rule.

After an operation fails, the steps after the failed one never ran. When
there are two or more, they fold into one line under the list, a native
`<details class="disclosure">` whose summary says how many: "9 later
steps were not run." Opening it shows them as a second `ol.steps`, each
"Skipped". This keeps the error and its controls next to the page's
buttons on a phone, instead of a screen of grey rows. While an operation
runs nothing folds: the waiting steps are its progress. A refusal by the
pre-start check already leaves out the steps that never ran.

When an operation changes the active world (a switch), the
old world's open page reloads onto the Server page after the operation
ends, moves focus to the `h1`, and writes the result sentence
to `#announcer`.

When an operation's result shows on the same page after a reload (the
update of a world that is not active, on its world page), focus moves to
the result message itself (`tabindex="-1"`), which reads the whole
result with its reason, and nothing is also written to `#announcer`, so
it is said once.

### 7.7 Toggletip

A short explanation of one setting, behind an (i) button after its name,
as the game's map generator has it (SPEC 5.6, 17).

```html
<div class="field">
  <div class="field__head">
    <label for="ms-time-factor">Time factor</label>
    <button type="button" class="tip" aria-expanded="false"
            aria-controls="ms-time-factor-tip" aria-describedby="ms-time-factor-tip">
      <svg aria-hidden="true" focusable="false" viewBox="0 0 16 16">…</svg>
      <span class="visually-hidden">About Time factor</span>
    </button>
  </div>
  <input id="ms-time-factor" … aria-describedby="ms-time-factor-tip ms-time-factor-ach">
  <p class="ach" id="ms-time-factor-ach"></p>
  <p class="tip__text" id="ms-time-factor-tip">Controls how fast evolution increases over time. …</p>
</div>
```

- **Words.** The game's own tooltip (`*-description` in the game's
  `core.cfg`) in every language, without its "Note:" line about
  achievements, which Foreman shows its own way (8.7). Where the game
  has none, plain words, translated as usual (SPEC 22.6). A tip may add
  what the game's number means ("40 means 0.000004").
- **The button** is named "About" and the setting; for a group's
  "Enabled" box it names the group. The icon is a 16 px circled "i" in
  `text-muted` (`text` on hover and while open), in a 24 px round target,
  44 px with a coarse pointer (`pointer: coarse`). Focus is the usual
  ring (5).
- **The text** is in the page, so the field's `aria-describedby` and the
  button's name it: a screen reader hears it with the field, and with the
  button. Without script the button is hidden and the text is a hint
  under the field (`--text-sm`, `text-muted`).
- **With script** (`toggletip.js`) the text is a box over the page:
  `surface`, 1px `border`, `--radius`, `--text-sm` in `text`, at most
  22rem wide. It opens on a click or tap (and stays), on keyboard focus,
  and on mouse hover (and closes when the pointer leaves both the button
  and the text). Escape, a tap elsewhere or moving focus away closes it.
  One is open at a time. The script places it under its button, or above
  it when there is no room below, and moves it left to keep 8 px from the
  window's right edge, so it fits at 320 px. `aria-expanded` follows.
- Nothing moves when a tip opens: the box lies over what follows.

### 7.8 Cards, the status card and disclosure rows

A card is a page section drawn as a panel, so a page reads as a few panels
on its background instead of one long list.

```html
<section class="panel" aria-labelledby="players-heading">
  <h2 id="players-heading">Players online</h2>
  …
</section>
```

- **What is a card.** A `section.panel` that is a direct child of `main`
  (or of a `.panels` wrapper, a column of a `.split`, or the mod settings
  editor), a settings `form.panel`, and anything given the class `card`. A `.panel` anywhere
  else is only a grid, with no frame.
- **The look.** `surface` face, a 1 px `line` border, `--radius-lg`,
  `--shadow-card` and `padding: var(--card-pad)`. The padding is
  `min(1rem, 4vw)` below 48rem and 1.5rem from 48rem. It follows the
  window, not the text size, so at 200% text the content keeps its width
  (WCAG 1.4.4). Below 22.4rem (a 320 px phone) the frame and padding go: the
  card becomes a flat section under a 1 px `line` rule (4, "Depth").
- **Inside.** A grid with `gap: var(--space-3)`, the section's `h2` pulled
  to `--space-2` of its content. A card is also a size container
  (`container-type: inline-size`), so what is inside lays itself out from
  the card's width, not the window's. The headings that CSS names by id
  carry a small line icon in `text-muted` before their words (7.9).
- **Side by side.** A `.split` is a main column and a rail: from 64rem
  the Mods tab puts "Plan" alone in a 22.5rem rail, and "Find a mod", the
  results, "Match mods to the save", "Installed mods" and the rarer tools
  (upload, mod settings) in the main column, in that order; narrower,
  the plan follows the results and the tools fold under one "Mod tools"
  disclosure after "Installed mods" (8.11). The Saves tab puts the list in the main column and
  Upload a save in the rail (8.3); the Users page puts the list beside
  Your password (8.15). Other two-column places: the world Overview (map
  exchange string and Make a favorite side by side from 64rem, 8.2), the
  factorio.com account page and Generate (8.6). The account card spans the full content width, with the
  same section-heading column and content column as the world Overview,
  so a wide screen shows no empty space beside it.
- **The status card** is the Server page's `card status-card plate`, the
  Server plate. Its band states the server's state large, behind a 6 px
  rule in the state's colour (7.5); then come the controls, the step list
  of a running operation and the Emergency actions (section 6), then the
  facts, "Loads next" and the mods notice. The rule is drawn as a border,
  so forced colours keeps it. A live part with nothing in it takes no row.
- **The pages card** (`card shortcuts`) lists the live world's pages
  (World page, Mods, Game settings, Switch world). On a wide screen it is
  one card of four 56 px rows divided by hairlines: icon, name and a
  chevron. On a phone it is a two-column grid of 48 px bordered tiles with
  8 px gaps and no chevrons, without a card frame (section 6).
- **The recent snapshots card** (`card recent-snaps`) on the Server page
  lists the live world's three newest snapshots between hairlines, each
  its kind and label (`heading`, weight 600) with its time at the end in
  `text-muted`, then "All snapshots" (section 6).
- **The disk strip** (`panel disk-strip`, on the Snapshots pages) is a
  compact card: the two figures
  (name beside value) and the one-line explanation on a single wrapping row,
  so the forms and the list stay near the top. A warning inside it takes a
  row of its own.
- **Foldouts** (`details.disclosure`). One style for every foldout; the
  class stays `disclosure`. The summary is a `list-item` with the browser's own triangle (`::marker`) at
  its start, coloured `text-muted` so orange stays for the primary action,
  the current place and focus: it points ahead when closed and down when
  open. The label is `heading` at weight 600, with no face, border or
  shadow at rest. Under a pointer that can hover (`@media (hover: hover)`) the row fills with
  `secondary-hover` at `--radius`; its side padding of `--space-1` and a matching negative
  margin keep the words still. Focus keeps the normal ring. With a fine
  pointer the row is compact (`min-block-size: 1.75rem`, `--space-1` above
  and below); on touch it is `--target-min`. A lone foldout has no line
  above it; one directly after another gets a 1 px `line` rule above it,
  so stacked rows such as Generate's planets and the raw settings read as
  a list. "Emergency actions" is as quiet as any other (`--text-sm`); only
  its opened area is red (section 6). The plan list's summary on the mod
  portal keeps its 44 px bar and draws its own triangle, without the
  compact size or the hover fill. Used for "Or start anyway" (7.4), the steps that were not
  run (7.6), "N other mods stay as they are" and the upload and replace
  forms (8.11), "Replace the account" (8.11), the version not in the list
  (8.6), Generate's planet rows and raw settings, the resource amounts as
  text (8.8), "Mod tools" on the Mods tab below 64rem (8.11, keeping its
  settings icon), "Emergency actions" on the Server page (6), a snapshot
  row's "Details" on a phone (8.3), "Upload limits" under Game settings'
  Network, past console messages, the Worlds list's archived worlds (8.2),
  a favorite's Details (7.11) and "What saving does" (7.4). Not foldouts: a player's name in the players table (8.12,
  its chevron at the end), the row menus, the user menu and the mod search
  results' own Details toggle.

### 7.9 Line icons

A small set of line icons helps a person scan tabs, headings and controls.
Words stay first (1).

- **Source.** A subset of 43 icons from Lucide, under the ISC licence,
  among them `info`, `alert` and `error` for messages (7.3).
  `internal/web/static/vendor/LUCIDE-LICENSE.txt` holds the licence, with
  the notice for the icons Lucide derived from Feather, and `SHA256SUMS`
  lists it with the fonts.
- **How they are drawn.** Each icon is an SVG on a 24 by 24 grid, stroked
  1.75 with round caps and joins, held as a data URI in a custom property
  at the top of `foreman.css` (`--i-server`, `--i-trash`, and so on). An
  element takes one by setting `--icon` and by being named in the rule that
  draws the icon on its `::before`: a `currentColor` background cut to the
  icon's shape with `mask`. So an icon takes its text's colour in both
  themes, costs no request, and is not read by a screen reader, because a
  pseudo-element with `content: ""` has no text.
- **Size.** 1.125rem beside text and in a remove button, 1.25rem in a
  message and an icon-only button, 1rem for a chevron, 14 px before a
  mod's portal news, 2rem in the map preview's held box and 3rem in an
  empty state.
- **Where.** The three main tabs (dropped below 23rem); the Server page's
  control buttons and the pages card; the Generate and Import buttons; the
  section headings that have an id the stylesheet names; remove and
  icon-only buttons; messages; a mod row's More toggle (three dots) and
  portal news; empty states; the person and globe in the user menu's
  button from 64rem; the chevrons of the user menu, breadcrumbs, the pages
  card on a wide screen and disclosure rows.
- **Rules.** An icon never stands alone without a name: it is beside its
  words, or its button holds the words as hidden text. An icon is never the
  only sign of anything. It is decoration, so it carries no meaning of its
  own. In forced colours only the button, tab, shortcut and heading icons
  are repainted (6, "Forced colours"); the rest may vanish. Adding one
  means adding it to the set and to the selectors, here first. The map
  preview's large view and the toggletip keep their own inline SVG icons
  (8.8, 7.7).

### 7.10 Uploads (SPEC 14.6)

Every file upload uses one control, the drop zone: the partial
`dropzone` in `templates/dropzone.html` with `static/dropzone.js`, which
a page loads in its `scripts` block. The page builds a `DropZone`
(`internal/web/dropzone.go`) and passes it to the partial:

| Field | What it is |
|---|---|
| `ID` | The file input's id. The hint, chosen line and errors use it as a prefix. |
| `Name` | The form field the file posts as, such as `save`. |
| `Label` | The visible label, as a message. |
| `Accept` | The one file ending the field takes, such as `.zip`. |
| `MaxBytes` | The server's size limit, shown in the hint. |
| `Error` | The server's refusal of the last upload, shown under the field. |
| `DescribedBy` | The id of a paragraph outside the field that also describes it; it reads after the limits hint. Empty for none. |
| `Form` | The id of the form that posts the file, when the field sits outside it (a form cannot nest). Empty for the enclosing form. |

- **Without script** it is a plain form field (7.2): a label with the
  required asterisk, a hint naming the type and size limit, and the
  native file input. The form posts as it always did.
- **With script** the zone is a dashed box at least 6rem tall that says
  "Drop the file here, or choose a file". The native input is stretched,
  transparent, over the whole box, so it stays the control: a click
  anywhere, or Enter or Space when it has focus, opens the picker, and a
  screen reader meets an ordinary labelled file field. Focus draws the
  focus ring on the box. On a phone, where dragging is rare, it is simply
  a big button.
- **A drag** of files anywhere on the page turns every zone's border
  solid and changes its words to "Drop the file to choose it"; over the
  zone the border grows to 4px. The change is never colour alone.
- **Once chosen**, the line under the box says "Chosen: *name*, *size*"
  with a Clear button. Clear empties the field and puts focus back on it;
  choosing again replaces the file.
- **Refused before uploading**: a file of another type, several files,
  or a file over the limit. The field is emptied, gets a field error
  (7.2) that says what to choose instead, and the error is read out. The
  page's own listeners never see a refused file. The server still checks
  every upload as before; the zone's checks are only a quicker answer.
- A page never adds its own capture-phase `change` listener on an
  ancestor of a zone: it would run before the zone's and see refused files.
- A new file, good or refused, hides the server's error from the last
  upload; a refused drop also empties any file chosen before.
- One file per zone.

**Where an upload happens.** An upload happens in place, in a drop zone,
only where its result is used on that page. Otherwise the page links to the
tab that owns the file, as a `link-tab` link (7.1), not a button: "Upload a
save" on the Mods tab goes to the Saves tab, mod settings link to the Mods
tab, and an empty Worlds list links to Import. The link's words name the
file: "Upload a save", "Upload a mod zip", "Replace mod-settings.dat".

### 7.11 Favorites: export and import (SPEC 5.12)

The Favorites page is one of the Worlds area's pages (6,
"Sub-navigation"), under the breadcrumb "Worlds > Favorites", with a
one-line intro. It lists each favorite as one closed row, stacked with a
1 px `--color-line` line between rows, as stacked foldouts are (7.8). On
a wide page the list and Import sit side by side (a `.split`, Import in
the rail).

- **The closed row.** The favorite's name, bold, is the summary of a
  "Details" foldout (`details.disclosure`), with a muted line of facts
  under it: game version · Space Age or No Space Age · N mods. Generate
  sits at the row's right end, on phones too, outside the summary and
  beside it on the row's grid. Its face is the Worlds tab's globe with a
  small plus at its lower right and the word "Generate"; its accessible
  name says which favorite: "Generate a world from {name}". Details shares
  the row's grid (subgrid), so its body spans the whole row once open.
- **Inside Details, in order:** made from and made on; the mod list, two
  columns once there is room; the map exchange string with its Copy
  button; Rename (a name field and a Rename button on one line); then the
  export Warning; then Export and Delete (a red ghost with its icon and
  word, 7.1) on one line, last. The mod list and the
  exchange string are headed sections, not foldouts of their own. In a
  browser without `::details-content` (Firefox before 143, older Safari),
  an `@supports not selector(::details-content)` rule places the open body on
  the second row across the whole row, so it neither squeezes into the name's
  column nor overlaps Generate.
- **Rename** posts to `/favorites/{id}/rename` and follows Make's name
  rules and error text (not empty, at most 100 characters, no name another
  favorite has, ignoring case; a change of case to its own name is
  allowed). It rewrites only `favorite.json`. Success returns to the list
  with "Renamed the favorite to "{name}"."; a refusal shows the list again
  with that favorite's Details open and the error on its field. It works
  without script.
- **Mods by title.** The mod list shows each mod with the `mod-name`
  partial: its title from `info.json`, kept in `favorite.json` when the
  favorite was made, linked to its portal page in a new tab, with its
  pinned version beside it. A favorite made before titles were kept shows
  the mod's name instead.
- **Export** is a quiet button that downloads `<name>-favorite.zip`: the
  favorite's folder as it is on disk, plus `foreman-favorite.json`, which
  holds the file's format version. A Warning message (7.3) in each opened
  favorite, just above its Export button and tied to it
  (`aria-describedby`), says the file holds the mod settings and that a
  mod may keep a token there, so it should be shared only with people
  trusted.
  Server settings never hold a password (SPEC 14.8).
- **Import** is its own section beside the list (under it on a narrow
  page), with one drop zone (7.10)
  for a `.zip` of at most 8 MiB and an "Import favorite" button. It works
  without script as a plain upload form.
- **Checks before writing.** The server reads the whole file in memory and
  checks all of it before anything is written: the size, the format
  version, that every entry is one of the six known file names with no
  folder, link or repeat, that each entry unpacks within its own limit,
  that both JSON descriptions have no unknown field, and that every mod
  name, version and title and the game version are well formed. Server
  settings are stripped of secrets again. A refused file gets one field
  error, "This is not a favorite file Foreman can read, or it is damaged.
  Nothing was imported.", which never repeats what the file holds.
- **What is not checked.** A mod this Foreman lacks is still imported,
  and Generate's mod review shows it. A game version this Foreman does not
  support is still imported; Generate replaces it with the default version.
- **A name in use** is refused, not renamed, so the person sees which
  favorite they already have: rename or delete it, then import again.
- **An imported favorite** is dated when it was imported and names no
  source world, since that world is on another Foreman.
- **A refused import** shows the error summary (7.2) at the top of the
  Import section, which takes focus and links to the file field.
- **Writing** builds the favorite in a staging folder and renames it into
  place, so a failure leaves nothing. Success shows the list with "Done:
  Imported the favorite *name*." in focus.

### 7.12 World files (SPEC 5.13, 5.14)

**Export** is a card on a world's Overview, after sharing (8.2) and before
the managing rows. It is a form that posts, because it does work and is
audited; the answer is the file. In order: the heading "Export world file";
a hint with what it is and the estimated size; a radio group "Saves in the
file" ("All saves" checked, and "Only the save that loads next" with a
caption); a note that a running game exports its last save on disk and
that each export is recorded in the audit log; one line on what is left
out; the Warning message (7.3) about mod-held data, tied to the button by
`aria-describedby`; then the Export world button. A world a newer Foreman
wrote has no card.

**Import** is a third panel on the Import page, after the folder form: a
heading, one sentence, one drop zone (7.10) for a `.zip` and an "Upload
world file" button, which goes to the preview page. It is a plain upload
form without script. No file chosen, or a file over the limit, shows on the
field with the error summary (7.2).

**The preview** is a form page: the facts list, then the warnings (a game
password was set; what mods may hold), then the name field, the image
download box, and Import world with Cancel. Warnings sit above the name, as
in the favorites export (7.11). A refusal that can be fixed here (the name,
disk space) shows the preview again with the field error and the error
summary, so the person types another name with no second upload. A file
that cannot be imported gets an error page with a way back to Import. No new
CSS.

## 8. Patterns for later screens

These are not built in Epic 1. They are here so Epics 2 and 3 start from
the same rules.

### 8.1 Headings and sections

Each page is `h1`, then `section` elements each with an `h2`. A section
with actions puts its button row directly under its heading or at its
end, never floating in a corner.

A page head (`header.page-head`) may carry a line above the `h1` that
says where the page sits. There are two kinds, and no page has a "Back
to" link above its heading:

- **An overline** (`p.eyebrow`: 12 px Plex Mono capitals, weight 600,
  `text-muted`, 2) names the area on the pages of the Server view and on
  the top-level Worlds and Snapshots pages ("Server", "Worlds",
  "Snapshots"). The Server page has no breadcrumb: its trail would be one
  item.
- **A breadcrumb** (`nav.crumb`, named "Breadcrumb", holding an `ol` with
  one `li` per step) opens the pages under Worlds: a world's pages
  (Overview, Saves, Snapshots, Mods, Mod search, the mod plan, Mod
  settings, Game settings), Generate, Import, Favorites, a favorite's
  delete step and Deleted worlds. It also opens each of Foreman's settings
  pages as "Settings > *page*", with Settings linking to General; on
  General itself, Settings is plain text, so it never links to the page
  shown. It is the full path, in Plex Sans at `--text-sm`, weight 500:
  each ancestor a link in `text-muted` with a 1 px underline at 68 % of
  its colour (so a link differs from the current page by more than colour;
  3:1 or better against the page and a card, WCAG 1.4.11), and `heading`
  with a full-colour underline under the pointer, then an 8 px chevron; the
  current page last, in `heading`, not underlined, marked
  `aria-current="page"`; no trailing mark. The chevrons are drawn by CSS,
  so a screen reader does not read them, and in forced colours they are
  painted `CanvasText`. For example "Worlds > Nauvis Prime > Mods",
  "Worlds > Nauvis Prime > Mods > Mod settings", "Worlds > Import a world"
  or "Settings > Audit log". Each link is a 44 px target through an
  invisible hit area above and below it. The chevrons turn over in
  right-to-left text. Four trails end on a link rather than on the page
  shown, because the page is a step under another: a favorite's delete
  step, the notification form, the created API token and the mod plan.

A page's one-line description under its title (`p.page-intro`) is
`text-muted` at the measure.

**Sign-in.** When both ways to sign in are offered, single sign-on and
the Foreman account form are one composed panel: single sign-on above,
the account form below a hairline, with one shadow. The account form's
button is the one filled button; "Sign in with single sign-on" is
secondary, and primary only when there is no account form.

### 8.2 The world list (SPEC 5.4)

A `ul`, one `li` per world, each holding an `h2` or `h3` with the world's
name (a link to the world's page), then a `dl.facts` with game version,
Space Age, last played and disk use. The active world carries the live
world's chip (7.5) inside its heading, after its name: the pin and the
word "Active", a hairline, then the game's state when Foreman controls
the game, as one pill rather than two. It is listed first. The values
(game version, last played, disk use) are 13 px Plex Mono with tabular
digits, and what the world's snapshots add, under Disk use, is
`--text-xs`, weight 400, `text-muted` (7.5). A size in the Disk use
fact, and the size in its "snapshots add" note, are each one
`span.nowrap`, so "0 B" never wraps onto a line of its own at 320 px. A world that can be switched
to has a "Switch" button beside its name (its accessible name is "Switch to"
and the world's name); section 6 ("What a phone shows first") gives the
row's grid. On a narrow panel the Last played fact shows only the age
("1 hour ago"), so it fits its half of the row in every language; the
world's own page and a wide panel keep the date, clock with zone and age.
`time.js` writes both texts into the `time` element (`data-age-only`),
the style sheet shows one by the panel's width, and the exact time stays in
`datetime`. Without script the page keeps the server's UTC text. The word carries the
meaning. A list with a `dl` per item
reflows at any width, where a six-column table would not fit 320 px.
From 64rem it reads as a table: one header row (World, Game version,
Space Age, Last played, Disk use) in the column-name style (8.3), over a
1 px `line`, heads the list. The header row is `aria-hidden`, because each row
keeps its own fact names for screen readers, visually hidden there. Every
row is at least 56 px tall, with its name, chip and values centred on the
row, on columns shared by every row. The columns are shares of the row,
not fixed widths, so they fit beside the rail; Last played takes the
widest share and wraps between its date, clock and age when it must,
24 px clear of Disk use. Below 64rem an archived row's two buttons take
a line under its facts. The list sits in the page's card, and its rows are separated by a 1px `line`
border, not boxed as cards of their own. A world's name is its row's title,
a link in `heading` colour that is underlined only under the pointer. The
active world's row has a 3px leading edge in `selected` that reaches the
card's edge, and takes the server's state colour while the badge shows
(`ok`, `warn` or `danger`, as the Server plate's rule does).

The page is the Worlds area's All worlds page: under the "Worlds"
overline, the `h1` with Generate (primary) and Import at the end of its
row, then the area's list of pages (All worlds, Favorites, Deleted
worlds; 6, "Sub-navigation"), then the list.

Archived worlds (SPEC 5.9) are not in this list. They sit in a closed
`details.disclosure` at its foot, "Archived worlds (n)", whose rows share
the list's `world-facts` and use `h2` headings with an "Archived" badge.
Each archived row ends with two secondary buttons, "Saves" (to the
world's Saves tab) and "Unarchive world" (left out for a world newer than
this Foreman reads), both described by the world's name; from 64rem they
sit on one line at the row's end and share the live list's columns. When
every world is archived, the list's place says so and keeps the upload
link. A world's page shows the badge and an Unarchive button, and the
other worlds' pages an Archive row in the Danger zone (7.4). The switch
pickers leave archived worlds out.

**A world's Overview**, in order: the facts card (7.5); then Players in
this save (below); then sharing, the map exchange string (8.8) and Make a
favorite (7.11), as two cards side by side from 64rem; then Export world
file (7.12); then one card of setting rows for managing the world, Game
version, Rename and Clone; then the Danger zone with Archive and Delete
(7.4). A setting row is a title column (heading and hint) and a content
column; from a 60rem card the title column is 240 px.

**Players in this save** (SPEC 8.14) is a panel under the facts,
`id="save-players"`, the target of the Server page's "N players in this
save" link. It is rendered with the page and works without script. It
shows one sentence for the state (reading, never read, tracked since a date
after a failed read, no save, unsupported version), then a two-column
table: the player's name with "Last seen <date>", "Online now" or "Not seen
by Foreman" under it, and the play time as hours and minutes (a figure
never splits) with "Played before Foreman tracked this world" under it.
Two columns, because three do not fit 320 px with the table rules of 8.3.
An inexact record adds a note. "Read again" (or "Read players from the
newest save" before any read) is a normal button, there whenever a read is
possible. While a game runs, a hint under it says the read needs about as
much memory as the game uses now and shows that figure. After a click the
status takes focus and is read out. With script the panel asks again every
3 s while a read runs, and every 5 s while the world runs.

### 8.3 Tables (saves, snapshots, players)

`table` with a `caption`, `th scope="col"`, and at most four columns,
one of which may be actions. The name column wraps with
`overflow-wrap: anywhere`; numbers are right-aligned and tabular. Column
names (and the token, user and world lists' header rows) are labels, set
apart from content: 13 px Plex Sans in sentence case, weight 600, in
`text-muted`, on one line. The figures in cells (sizes, times,
versions) are 13 px Plex Mono with tabular digits, so the numbers carry
the mono voice, not the labels. Every row has a bottom border of
1px `line` (the last has none). No zebra stripes; a body row under the
pointer is tinted with `raised`. If a table needs more than
four columns, it becomes a list with a `dl` per row (8.2) instead.
A size cell (`class="num"`) never wraps, so "125" is not parted from
"B". A timestamp must not split at its hyphens, but a whole `2026-11-03
14:25 UTC` is too wide beside other columns at 320 px (it crushed the
name column to 24 px), so a template wraps the date and the time each in
`<span class="nowrap">`, and the timestamp breaks only between them.
With four columns even that is too wide at 320 px, so a four-column
table moves its second column (such as a snapshot's reason) into the
first cell, above the label, and drops to three.
A link that stands alone in a cell, such as a save's name, is padded to
`--target-min` tall; a badge may follow it in the same cell. A button in
a cell is an ordinary button (7.1). Sizes have one decimal and a unit ("2.1 GB"). Times use `time` with a
`datetime` attribute and are shown as section 9 describes.
A snapshot list that has row actions is laid out from its panel's width.
At 30rem or wider every row is one line on columns shared by all rows (a
subgrid, so times and sizes line up from one world to the next): the box and
label, when it was taken, the size, then Restore (secondary) and Delete (a
red ghost with its icon and word, 7.1); from 48rem the time column is 15rem
and the size 6rem.
On a phone (a panel 30rem or narrower) each row is stacked and shows only
what an operator scans for: the label with its box at `--text-md`, weight
600; then one muted `--text-sm` line with its age and its size; then a
44 px "Details" disclosure holding the exact time, who took it and, on
the main Snapshots page, what deleting it frees; then Restore and Delete together, 12 px apart. The header row is visually hidden. "Delete selected" (risky) waits
for a ticked box (7.1). Its selection bar comes before the list in the
code, after "Select all automatic", so a keyboard user reaches it with one
Shift+Tab from the first box (two on the Snapshots tab, past the first
world's name) instead of passing every row; `order: 1` draws it at the
end of its form, where it sticks to the window's foot (6, rule 7), and
"Show older" after the list keeps its place after the bar (`order: 2`). A list without row actions, such as the deleted
worlds, keeps its columns. A snapshot table also stacks each cell (snapshot,
then "Taken:", then "Size:") when its panel is narrower than 14rem, which is
200% text on a 320 px phone. Changing `display` can drop table semantics in some browsers, so
these tables carry explicit `table`, `rowgroup`, `row`, `columnheader` and
`cell` roles (SPEC 17.12).
**The Saves tab** puts the list first and Upload a save after it; on a
wide page they sit side by side, Upload a save in the rail (a `.split`).
The hint about the times shown follows the table.
The saves table's Load next buttons sit in its own last column, under a
visually hidden header, so they line up whatever the names' lengths; the
newest save's button is named "Load next: the newest save". When the panel
is 14 to 24rem wide (a phone), each saves row is a grid with fixed size
and time columns and the button on its own line under them.
An autosave's label in the saves table is "World, date time" in tabular
numbers, and its file name sits on a second line under it, muted, so every
file name starts at the label's left edge; the "Loads next" badge stays
beside the label.
A time is three units: the date, the clock and the zone. None breaks inside
itself, and a wide zone such as `GMT+5:45` drops to its own line after the
clock. Each language keeps its own order (Chinese puts the zone first), and
`checkPage` fails a page where any unit is broken.

### 8.4 Long lists

A world's saves can number 30 or more. Lists and tables show everything
up to 50 rows; past that, Epic 2 decides between paging and "Show older".
No infinite scroll.

### 8.5 Log and console (SPEC 8.4, 8.6)

```html
<section aria-labelledby="log-heading">
  <h2 id="log-heading">Game log</h2>
  <div class="log-frame">
    <div class="log" tabindex="0" role="region" aria-label="Game log lines">
      <pre>…</pre>
    </div>
    <button type="button" class="button" hidden>Jump to newest</button>
  </div>
</section>
```

The game log and the console share one card (`card terminal`), and the
console section comes after the game log, as a terminal reads (section 6).
Inside it, the command field comes above its history:

```html
<section aria-labelledby="console-heading">
  <h2 id="console-heading">Console</h2>
  <form class="field console-form" method="post" action="…">
    <label for="command">Command</label>
    <p class="hint" id="command-hint">…</p>
    <div class="console-row">
      <input id="command" name="command" type="text" required aria-describedby="command-hint" autocomplete="off" autocapitalize="off" spellcheck="false">
      <button type="submit" class="button button--primary">Send</button>
    </div>
  </form>
  <div class="log" tabindex="0" role="region" aria-label="Console history">
    <ol class="console-history">
      <li>
        <p class="console-command"><span class="visually-hidden">You sent </span>/players online</p>
        <pre>Online players (2): …</pre>
      </li>
    </ol>
  </div>
</section>
```

Classes and their contract:

- `log`: the scroll box. Holds one `pre` (the game log) or one `ol.console-history`. It
  must keep `tabindex="0"`, `role="region"` and an `aria-label`, because a
  scrolling box needs a name and a keyboard stop. Nothing inside it may set
  its own width; lines wrap.
- `log-frame`: optional wrapper, used only when the box carries a "Jump to
  newest" button. The button floats over the log's bottom end corner
  (`--space-3` up from the foot, the card's padding in from the end, with
  `--shadow-pop`) only while there is something newer, and takes no room
  while hidden: there is no empty row under the log. The command row sits
  in the same card, `--space-4` under the log. The script toggles the
  button's `hidden` attribute, which also takes it out of the tab order and
  the accessibility tree. The button stays a sibling of `log`.
- `console-row`: the field and "Send" on one row. They sit side by side
  and wrap onto two lines when the panel is too narrow. The field's text is
  mono at 16px, so a phone does not zoom in on focus.
- `console-form`: the command field is the form's only field and the form
  exists only to send it, so its label has no asterisk and the form has no
  "* Required" note; the input keeps its `required` attribute, so
  assistive technology still announces it (7.2). On a phone, where the
  form is the sticky bar at the card's foot (section 6), the field's hint
  is visually hidden and still linked by `aria-describedby`, so the bar is
  one label and one 44 px row; the log's own hint already says the RCON
  password is hidden.
- `console-history`: the `ol` of past commands, newest last.
  `console-command` is the sent command, bold, with a drawn "> " prompt
  (ignored by screen readers) and a visually hidden "You sent" in front, so
  a command and its reply are told apart without colour. The reply is a
  `pre` in the same `li`.
- `button[hidden]` is hidden even though `.button` sets `display`.

- A deep box: background `deep`, 1px `border`, text `text`, `--space-3`
  padding, `--font-mono` at `--text-sm`,
  `white-space: pre-wrap` on the `pre`, `overflow-wrap: anywhere`, so lines
  wrap and never scroll sideways. When lines rewrap (the web font
  arriving, a turned phone) a view that was at the bottom is taken back
  there. (The `ol` of the console history keeps normal white space, or the
  markup's line breaks would show as blank lines.)
- **Times in the game log.** `log.js` splits each line into its time (a
  date and a clock, or the game's elapsed seconds) and its words; the text
  of every line is unchanged, and a path in the words may break after a
  slash. At every width the date is shown once, as a divider row over the
  first line of each day: 12 px, weight 600, `text-muted`, in the sans,
  followed by a `line` hairline. Each line's clock or elapsed seconds sits
  in one right-aligned 9ch column in `text-muted` with tabular figures, and
  its words beside it, so dated and elapsed-time lines share one grid and
  nothing is indented on its own; a line with no time starts in the words'
  column. On a phone (below 40rem) each line's time sits above its words in
  12 px `text-muted` type, and the words take the full width at 13 px on
  20 px lines, with 8 px between entries. Without script the lines keep the
  time as the server wrote it.
- The game log's box has a fixed height and scrolls vertically. Its lines
  take the window's height less 34rem, but never less than 15rem or more
  than 34rem, rounded down to whole 1.25rem lines
  (`round(down, clamp(15rem, 100vh - 34rem, 34rem), 1.25rem)`), plus the
  box's padding and border. The same height holds on a phone. The console's history box is only as tall as what it holds,
  up to `min(40vh, 20rem)`, then scrolls (section 6). Both are focusable so
  keyboard users can scroll them.
- **The game's own Error and Warning lines** (a line whose message, after
  the game's seconds counter, starts with "Error" or "Warning") are drawn
  in `danger-text` and `warn`. A player joining (a `[JOIN]` line) takes
  the running colour, `ok`, and a player leaving (a `[LEAVE]` line) takes
  `text-muted` (classes `log-join` and `log-join log-leave`), so the two
  events read apart. The word is in the line itself, so the colour is
  never the only sign (SPEC 17.4). `console.go` marks the lines the page
  starts with, and `log.js` the same patterns for lines that arrive
  live.
- **Download log** is a link at the end of the "Game log" heading's row
  (`div.section-head`). It is served by `GET /worlds/{id}/console/log` as
  a `text/plain` attachment named `foreman-<world id>-log.txt`, and holds
  exactly the lines the Console keeps, already masked, so nothing leaves
  that the page does not show (SPEC 8.6, 14.8).
- It is not a live region. A game log can print many lines a second; a
  screen reader announcing each would make the page unusable. Crashes
  and state changes are announced through the badge (7.5) instead.
- New lines are appended, not re-rendered, and the DOM keeps a fixed
  maximum (Epic 3 sets the number; 1,000 lines is a reasonable start).
  Older lines are dropped from the top.
- Auto-scroll follows new lines only while the view is at the bottom; if
  a person scrolls up to read, it stops following, and a "Jump to newest"
  button appears.
- The console is a `form` with a labelled mono text field ("Command") and
  a "Send" button, above an `ol` of past commands and responses, newest
  last, in the same sunken mono box. The response to the person's own
  command goes to `#announcer`, because they asked for it.

### 8.6 Generate (SPEC 5.6)

The task is "make a new world". It is three steps, and each is a plain
page that works without script (SPEC 17.19). From the Server page the
defaults take three clicks: Worlds, "Generate a world", then "Generate
world" on step 1, which makes the world and lands on step 2. The Mods and
map settings steps are optional.

**The step bar** (`genflowsteps.html`) is a `nav.subnav` holding an `ol` of
three links: "1. World", "2. Mods" and "3. Map settings". The current one
carries `aria-current="step"`, and the links are the way between steps
when there is no script. The `nav` sits in a `div`, because a subnav
straight under `main` is the area tabs' phone copy, which is hidden on a
wide screen. It shows on step 1 with "1. World" current, where steps 2 and 3
are plain text (a `span`, styled like the other tabs) because the world does
not exist yet. It shows as links on step 3, and on the Mods tab and the
review while the world is Unfinished.

**Step 1**, `GET /generate`: one `h1` with a one-line hint, then one form,
in this order:

1. The required note (7.2), then Name (required, a placeholder example).
2. Game version: the same picker as Import (SPEC 10.7). Typing a version
   not in the list sits in a closed `details`, "Use a version not in the
   list", opened when it holds a value or an error.
3. Space Age, and the "download the game image" checkbox.
4. "Start from a favorite" (one row on a card at least 64rem wide).
5. The action bar: Cancel and "Generate world" (primary).

There is no map form, preview or leave dialog on this page. The world is
made at once and the browser lands on step 2,
`/worlds/{id}/mods?generated=1`, where a Done box takes focus: "{name} is
made. Next, pick its mods or skip to map settings." A favorite with mods
lands on the review instead (SPEC 5.12).

**Step 2** is the world's Mods tab, or the review (8.11). For an Unfinished
world each shows the step bar with "2. Mods" current. The Mods tab has a
"Skip to map settings" link under the bar. After a mod change its Done box
shows "Next: map settings", a button (SPEC 17.9), in place of Skip. The
review has "Skip to map settings" after the Install button in source
order, or "Next: map settings" when the plan changes nothing. A world with a save shows none of this.

**Unfinished worlds** carry a plain `badge` "Unfinished" beside the name
on the Worlds list and on the world page. A list row's one action is
Continue, a primary button, with "Continue setting up {name}" for screen
readers. On the world page a note says setting up is not finished, with
Continue as its one primary button, and Switch becomes a plain button.
Continue opens step 2 while mod picks are not installed, otherwise step 3
(SPEC 5.6, 9.14). Checked with a Continue click from the Worlds list: one
click.

**Step 3**, `/worlds/{id}/new-map`, is the rest of this section and 8.7
and 8.8. Its form id is still `generate-form`. In this order:

1. The step bar and a line of intro. With script the page shows at once
   and the form part loads after it, with a progress bar and one
   sentence in the live region; without script the page refreshes itself
   to the same address with `?wait=1`, and the server waits for the
   planet run.
2. Notes: the built-in planets are used (with the reason, and a link to
   the Mods tab when the planets could not be read), and the controls
   that are saved but no longer in the mods, as a list.
3. "Fill from a map exchange string", a closed `details` with a mono
   `textarea` and a "Fill the settings" button. A string from another
   game line is refused with a link to the world's page.
4. The preset select with "[modified]" (the game's word) and a quiet
   "Reset to defaults" button after it; the seed with a "Random seed"
   button; then the tabs of 8.7.
5. The preview slot (8.8), beside the settings on a wide screen (below).
6. One achievements summary (below), then the action bar: Cancel (a link
   to the world), "Preview map" and "Save map settings" (primary).

**Layout.** Once the form is 56rem wide (a container query, so the rail's
width counts) the page is two columns, `gen-grid`: the settings, never
narrower than 34rem so their steps stay sliders,
(items 1 to 4 and the achievements summary) on the left, and the preview in
a column of 20 to 26rem on the right that stays in view while the settings
change. That column (`gen-preview`) is sticky at `--space-5` from the top,
as tall as the window between the top strip and the action bar. Once a
picture is drawn its order is: the heading and seed, the One planet and All
planets tabs, Show resource amounts, Preview size, the picture, then the
download links and the kept note as one tight group. Preview size is above
the picture so it is always in reach without scrolling. One planet's picture
is a square sized by the smaller of the column's width and the height left,
never under 16rem so its amounts stay readable; where the window is tall
enough the whole pane fits, and below that the pane scrolls on its own; it has a `raised` face, a 1 px `line` edge, `--radius-lg`
and `--space-4` of padding, and sizes its own controls from its width, as
the settings do from theirs (8.7). Several planets are thumbnails, two to a
row. From 100rem the preview column takes about two fifths of the page.
From 90rem the world's name and its game version pair up, and on a map
section at least 40rem wide the preset and the seed pair up; a pair shares a label row, a hint row and a control row, so the controls line up and a field without a hint leaves that row blank; starting
from a favorite is one row on a card at least 64rem wide. Narrower than that it is one column in source order, and on a phone (30rem
and narrower) the preview comes straight after the preset and seed, so it
is on the first screens, and the planets' rows in the settings start
closed. **The action bar** is one bar at the foot of the card, `raised`
with a 1 px `line` rule above it, running to the card's edges. It ends
with Cancel (a ghost button), "Preview map" and "Save map settings"
(primary), set together at the bar's end edge (7.1, "One completion
footer"). The code has them in the same order, so the screen order, the
keyboard order and the code order agree (WCAG 1.3.2, 2.4.3). On a phone
(30rem and narrower) they stack full width, top to bottom: Cancel,
Preview map, Save map settings. At 200% text a label wraps rather than push the page
wider.

**Preset, "[modified]" and reset.** Choosing a preset fills every tab with
its values (htmx redraws `#map-settings`; the preset and the seed are
outside it, so focus and a typed seed stay). "[modified]" shows once any
setting differs from the preset; the seed is not a setting of the
preset. "Reset to defaults" is the one reset for the whole world (SPEC
5.6): it shows while a setting differs or a preset other than the
default is chosen, and puts the default preset and its values back,
keeping the seed. Their room is kept while they are not shown (the
word and the button are there but invisible, and out of the
accessibility tree), so the row wraps the same way either way, and
nothing below it, such as the slider being dragged, moves when they come
or go. On a phone they hold a line of their own under the select. Reset is a submit button to the refresh route, so
it also works without script; with htmx it redraws the tabs and the
preset select in place. It asks nothing first, because nothing saved
changes; instead "Undo reset" (quiet, script only) takes its place in
the row with the focus, and the live region says "Map settings reset to
the defaults for this game version. Undo reset brings your changes
back." Undo sends the form as it was before the reset to the refresh
route, so the server draws every value back; focus returns to the reset
and the live region says "Your map settings are back." Any change in the
settings, the preset or the seed ends the Undo, since it would throw
that change away, and so does a game version or Space Age change, since
Undo would mix two game lines; the tab shown is a view and keeps it. An
Undo that ends while it has the focus hands it to the reset (or the
preset, when there is nothing to reset). A change made while the reset
is on its way does not end it: the reply replaces the fields anyway, so
Undo is offered as ever and brings back the settings as they were when
Reset was pressed. Focus stays where the person moved it (a field keeps
it across the redraw), and the live region says the usual sentence.
Clicks: one to reset, one to undo.
A hidden `filled_from` names the preset the tabs were filled from. A page
without script that sends a new preset with the old preset's values gets
the page back, filled from the new preset, with a Note "The settings
below were filled from the preset ..."; nothing is made until the person
has seen them. A version or Space Age change redraws everything that
follows the game line (`#line-box`) at once; text typed while that redraw
was on its way is kept.

**What is written** (SPEC 5.6). The preset unchanged: `--preset` and the
seed, no file. Anything changed: both files complete and no `--preset`,
because the game lets a preset win over the files. On a version whose
map-settings layout no tested image has (2.1.13 to 2.1.17) the
map-settings fields are not offered (a Note says why); a changed default
writes the map-gen file alone, and a changed other preset is refused on
the form, with its next steps (reset, the default preset, another
version).

**Achievements** (SPEC 5.6). A short warning, "Turns off some
achievements.", in `warn` colour, sits under each setting as soon as its
value crosses the game's line, and is part of that field's description.
One `message--warning` by the action row lists the game's own reasons
("Cannot be unlocked in peaceful mode."), each once; it is hidden while
none applies. The script decides from `data-ach`, `data-ach-limit` and
`data-ach-reason` on each field, which the server writes from
`mapgen.AchievementRules`; the server renders the same warnings and
summary on every page it sends. When a warning first appears it is said
once through `#announcer`.

**Preview slot.** The preview sits in its own column beside the settings
when the form is two columns, and after the settings otherwise (Layout,
above; 8.8).

On success step 3 shows again with a Done box that takes focus (7.3):
"Saved the map settings of Family base 2026. The map is made from them
when the world first starts." Its button is "Next: go to the world". When
the world's own planets could not be read, saving is refused with "Try
again" and a link to step 2, because saving the built-in planets would drop
the settings of the mods' planets.

### 8.7 The map settings (SPEC 5.6, 11.5)

```html
<fieldset class="tabset"><legend class="visually-hidden">Settings to show</legend>
  <div class="tablist">
    <label class="tab"><input type="radio" name="tab" id="tab-resources" value="resources" checked>Resources</label>
    … Terrain, Enemy, Advanced …
  </div>
</fieldset>
<fieldset class="tab-panel" id="panel-resources"><legend>Resources</legend> … </fieldset>
…
```

- **Tabs, as the game has them.** Resources, Terrain (with cliffs and
  1.1's water), Enemy and Advanced. The tabs are radios drawn as the
  sub-navigation's tabs (6): one tab stop, the arrow keys move between
  them, the current one bold with the 3px `selected` underline. The
  script adds `js-on` to the form and names the shown tab in `data-tab`,
  and the style sheet shows that one panel; the panel's own legend is
  then for screen readers only. Without script the radios are hidden and
  the four panels stack under their legends. After a failed submit the
  page opens on the tab holding the first error, and a summary link (or
  focus) that lands in another tab opens it first.
- **Per planet.** Resources and Terrain hold one `details` per planet;
  a world with one planet shows its groups directly. Each tab's planet
  rows form one exclusive group (`name="planet-<tab>"`): opening one
  closes the other, and focus stays on the summary the person used. One
  click opens a row and it stays open. The open planet is the same in
  every tab, is kept over a round trip in the hidden `open_planet`
  field, and is the planet with the first error when there is one.
  Opening a row switches the preview to that planet, and the live region
  says "Preview: Fulgora" once (with One planet chosen, 8.8); a tab
  switch says nothing. A planet the large view moves to opens its row
  without moving focus.
- **Cliffs off.** Cliffs that the game lets you switch off have an
  on/off box in their legend, as resources do, and their continuity
  select no longer offers "None". There is no achievement warning. The
  box comes by rule, never by planet name: on 2.x a cliff or enemy
  control gets one when the game says it can be disabled (Nauvis, Gleba
  and Fulgora cliffs, and a mod's, such as Muluna's or Corrundum's;
  not Carna's or Foliax's, which say they cannot), as does any resource
  that can be. On 1.1 Nauvis's cliffs keep their box. A mod's terrain
  control, such as Muluna's lunar rocks, has none.
- **Needed resources.** While a Nauvis resource the game needs (iron
  ore, copper ore, stone, crude oil and coal) is off, a warning shows
  under its box ("…unless a mod provides it"), and a `message--warning`
  summary beside the achievements summary lists each one. It is a
  warning, not a block. The live region says it once when the box goes
  off and once when it comes back on.
- **All resources.** A planet with two or more resources (counted, not
  named: Gleba and Fulgora have one each on 2.x) has, at the top of its
  Resources list, a fieldset with the legend "All resources", a one-line
  hint under it ("Sets every resource of this planet that is on."), and the
  same three settings as a resource: Frequency, Size and Richness, with the
  same steps (and "None" where the resources offer it), as selects or
  sliders like theirs. Only the Resources tab has it; cliffs and terrain are
  never touched. It is for script: the server draws it, the style sheet
  shows it under `js-on`, and its selects have no `name`, so the form sends
  nothing for it and the server's reading of the form is unchanged. A
  rule under it, a little more room than between resources, sets it apart.
  Moving a setting sets that value on each resource whose on/off box is
  ticked (or that has none) and leaves the others alone, by setting the
  resource's select and firing the events a person's change fires, so its
  slider, the achievement and needed-resource warnings, "[modified]" and
  the preview follow. The row shows the value every such resource holds, or
  "Mixed" when they differ: an entry the script selects, never one to
  choose (it is `hidden` and `disabled`), over a slider that stands at the
  default step with "Mixed" for its words, so from Mixed the first arrow
  key moves to the step beside the default. It is worked out again when a
  resource, a box or a preset or reset changes the values. The row applies
  a committed choice, not every step of a drag, and says it once in the
  polite live region ("Richness set to 600% on 5 resources."), after any
  achievement news the change caused. Each select is described by the
  hint. While every resource of the planet is off, the row is disabled and
  the hint says why; ticking one box brings it back. The on/off boxes are
  as before.
- **One fieldset per control**, its legend the game's name, small and
  muted so the groups read as one list. Its settings sit in a grid
  `repeat(auto-fit, minmax(min(100%, 10rem), 1fr))`, 13rem with sliders.
- **Steps: selects, and sliders on a wide panel.** Every frequency, size,
  richness, scale, coverage, continuity and the starting area is a
  `select` of the game's own steps (the game's words: a percentage,
  "None", "(default)"; the game has no other word for a step). The
  select is always what is sent. The script adds, after each select, an
  `input type="range"` over the same steps (no name, labelled by the
  select's label, `aria-valuetext` the step's words) and an `output` with
  those words. On a `map-section` container at least 34rem wide the
  style sheet shows the slider and output and hides the select; narrower
  (phones, 200% text) it shows the select. The row has fixed lines: the
  name (with its tip button, if any) and the value, then the slider, then
  the warning and the tip. The value keeps room for the longest step in
  any language (`min-inline-size: 20ch`, tabular numbers), so moving the
  slider never reflows the row; when the name leaves no room beside it,
  the value takes a line of its own, and keeps it. The name never breaks
  inside a word. The slider is not a boxed field: no padding, edge or
  width cap, so its knob reaches both ends of the track at the first and
  last step, and the fill ends under the knob. The style sheet draws it in
  neutrals, not the browser's orange, so orange keeps meaning the primary
  action: a 4 px track in `border`, filled up to the knob in `text`
  (`generate.js` sets the fill), and an 18 px knob in `heading` ringed with
  `surface`. Orange appears only in its focus ring.
- **The default step sits in the middle.** A slider's track is not the
  step index. Each step has a place on the track so that the default
  ("100", or "0" for a bias) is at the middle, with the steps below it
  spread over the left half and those above over the right (so the
  terrain scale, with 5 steps below and 6 above, and the sizes with
  "None", with 6 and 5, are balanced too). A click takes the nearest step;
  an arrow or a screen reader's increment always moves one step, even
  where the steps lie far apart on the track; Home, End and the Page keys
  work as they do natively. A select with no default step, or with the
  default at an end, keeps the plain index track. `aria-valuetext` is still
  the step's words, and the select, the values sent and the labels do not
  change (SPEC 5.6).
- **While an on/off box is off**, its name stays at full strength (it is
  a control); the names and values of the settings it turns off turn
  `text-muted`, and their sliders take `border` for both fill and knob.
- **Density on a wide panel.** On a `map-section` at least 34rem wide the
  groups sit closer (8px between groups and under a group's name), and
  with a fine pointer (`pointer: fine`) the sliders and the planet
  summaries are 24px tall, the WCAG 2.2 floor (SPEC 17.2). Narrow panels
  and touch screens keep 44px targets. Text sizes do not change.
- **Enemy tab:** the enemy controls (Nauvis's and Gleba's), No enemies
  (2.x) and Peaceful mode with their hints, the starting area, then the
  map-settings groups "Enemy expansion" and "Evolution".
- **Advanced tab:** Width and Height, the groups Technology (on 1.1
  Recipe difficulty, Technology difficulty and Research queue as
  selects, then the price multiplier), Pollution, Asteroids and Spoiling
  (2.x), then the raw map-gen JSON `details`.
- **Map-settings fields** (`ms.<dotted path>`) show what the version's
  file holds, in the units the game's map generator shows: cooldowns in
  minutes, ratios and modifiers in percent, evolution factors as plain
  numbers ("40"), as the game shows them, with no unit; the tip says what
  the number means. A group's "Enabled" checkbox heads
  it on its own row. Numbers are mono text fields 12ch wide with the unit
  beside them. 2.1's evolution group size factor replaces 2.0's group
  sizes.
- **Tips.** Every setting of the Enemy and Advanced tabs has a
  toggletip (7.7): the enemy bases' frequency and size, the starting
  area, No enemies, Peaceful mode, and each map-settings field, a
  group's "Enabled" box explaining the group. Width and Height keep their
  visible hint instead, as the game has no tip for them.
- **Spoiling rate** is shown as the game shows it, 10% to 1000%, the
  inverse of the stored `spoil_time_modifier` (shown % = 100 / stored).
- **Limits.** Every number field takes whole numbers within the game
  screen's own ends (SPEC 5.6), carried as `data-min` and `data-max`. A
  maximum that must follow its minimum also carries `data-min-field` and
  `data-gap`. When a change pushes a minimum past its maximum, or the
  reverse, the script moves the other field to fit, as the game does,
  and announces it ("set to … to keep the range valid") together with
  anything the change itself said. Without script the server refuses the
  bad pair with an error on the maximum field.
- **Raw map-gen JSON** (SPEC 11.5) is as before: the fields win for every
  key they cover.

### 8.8 Map preview (SPEC 5.6, P1)

Preview is optional and adds no screen to Generate. The tasks are "see
what this map looks like" (no clicks: it is drawn as the page opens and
as the settings change), "look at another planet" (one click on its
settings row, which the person opens anyway to change it), "see every
planet" (one click, All planets, or Preview map), "back to one planet"
(one click, One planet), "look closely" (one click on the picture),
"keep the pictures" (one click on a download) and, on a wide screen,
"change the setting of the planet I am looking at large" (one click, on
the setting beside the map; it was three, or four for a Terrain setting:
close the view, the tab, the setting, and the picture again).

- **Where.** A "Map preview" `section` (h2): in the preview column beside
  the settings when the form is two columns, after the settings otherwise
  (8.6, Layout).
  The heading's line also holds the seed used, "Seed
  123456", at its end while it fits, so the picture sits close to its
  controls.
- **Two pictures, and what each cannot show** (SPEC 5.6). Under the
  heading one `hint` line says "This fast preview cannot show terrain a mod
  makes by script." with a toggletip (7.7) for the detail. It shows on step
  3 only, because a picture of no world has no mods. When a world's planet
  list could not be read, a note says the game's own planets are drawn and
  planets the mods add are left out. When a picture is about 99% one
  color, a note under the list of pictures (not inside a card, because the
  pane sizes the cards to fit) says everything near the start is outside
  the map and a larger preview size may show more; it names the planets
  and is marked `translate="no"` for mod names.
- **The real-game picture.** On a 2.1 world showing one planet, the
  Download line ends with the quiet button "Draw from a real game (about 15
  to 40 seconds)" and a toggletip. Pressed, the section is `aria-busy`, a
  wait line and a progress bar show, and Preview map, Save map settings
  and the button itself are `aria-disabled` and blocked until the picture
  arrives, because the draw takes the one preview slot. The result is
  announced once, "Real-game picture ready.", and the focus moves to "Back
  to the fast preview". Its caption is "From a real game, 1024 tiles", and
  its `img` is square like the others, so the pane's size does not change.
  Busy, timeout and out of memory each have a sentence of their own; any
  other failure says Foreman could not draw the planet. A change of the
  settings redraws the fast preview, and a picture asked for again with
  the same inputs is the kept one (SPEC 5.6). The button needs script, and the fast preview does not. 2.0 and 1.1
  worlds do not offer it.
- **A picture's own actions share its Download line.** On a 2.1 world's
  one picture, "Draw from a real game" and its toggletip follow the
  Download link on the same line, so the pane gives no row of its own to
  an action the person seldom takes. The line wraps when the pane is
  narrow and when "Show resource amounts" makes the link longer; the
  button and its toggletip wrap together, so the icon stays beside the
  button, and the picture always leaves room for the second row. The
  "Download all drawn planets (zip)" link has its own line below both.
- **Size.** A "Preview size" setting of steps (DESIGN 8.7: a slider on a
  wide panel, a select of 17 sizes elsewhere) above the section, outside
  the part a reply replaces: 1024 to 3072 tiles square in steps of 128,
  1280 by default. The most is the demolisher territory cap, so every
  size offered can show them (SPEC 5.6). Its hint: "How much of the map,
  around its center, the preview shows. The pin is where players
  start." A change redraws like any other, and the choice is kept over a
  form error.
- **Drawn by itself** (with script). On page load, and 0.8 s after the
  last change to anything in the form that changes the map: the preset,
  the seed, any setting, a map exchange string filled in, and the settings a version or Space Age change redraws (asked
  once that redraw has landed). The name, the tabs and the download box
  do not count. The newest request wins: a change aborts the request on
  its way (the server stops its run) and a late reply is ignored. A
  pressed "Preview map" is aborted by a later change too.
- **One planet or all.** On a world with several planets, a choice under
  the heading line, drawn as tabs (radios, as 8.7's tabs, one tab stop;
  legend "Preview shows", for screen readers only): **One planet**
  (checked by default) and **All planets**. It stays when a draw fails,
  still showing the choice made, so One planet is one click back. It
  needs script, so without it the style sheet hides it. There is no planet picker: opening a
  planet's settings row already picks the planet.
  - One planet: an automatic request (`auto=1`, `preview_view=one`)
    draws only the planet whose settings row is open, as one large
    picture; opening another row draws that planet, and the live region
    says "Preview: Fulgora" once. Returning to a planet already drawn
    with the same settings shows the stored picture with no run.
  - All planets (`preview_view=all`): every planet, as thumbnails in the
    grid below; only the planets not drawn yet are run, and each later
    change redraws them all. Opening a row then draws nothing, but it is
    the planet One planet shows next.
  - "Preview map" draws every planet and chooses All planets, so One
    planet is always one click back. It is also the page without script.
  - The section's hidden `datalist` of the world's planets and their
    names serves the large view's Previous and Next.
- **Seed.** A blank seed field is filled with the seed the preview used,
  so the world made is the one previewed; a seed the person typed is
  never overwritten.
- **Focus and news.** Nothing automatic moves focus; a control in the
  section that had focus (the One planet or All planets choice, the
  territory box) keeps it across the redraw.
  While a request runs, the previous picture stays, faded to 60%, the
  section is `aria-busy`, and a one-line note above it says "Drawing the
  map preview for the current settings…"; that line's height is held once
  a preview shows, so nothing moves. When a preview arrives after a
  change, `#announcer` says "Map preview ready." once; the page load says
  nothing. Settings that are not valid get the problems in the section,
  each a link to its field, with no focus moved.
- **Pictures.** A `ul` (no bullets) of `figure`s, each with a
  `figcaption` naming the planet. The `img` has `width` and `height`,
  `inline-size: 100%`, `aspect-ratio: 1` and `image-rendering:
  pixelated`, so its space is held and its tiles stay sharp. Each item
  fills its column but is no taller than the window less its caption and
  download line (`min(100%, max(16rem, 100dvh - 2 x target - space-4))`),
  centred, with the caption and download lined up with its edge; the
  held box before the first picture follows the same rule, so nothing
  moves. Alt text: "Map preview of Nauvis, seed 123456. The pin is where players start."
  Several pictures (Preview map) sit in a grid,
  `repeat(auto-fit, minmax(16rem, 1fr))`. The picture is a link to its
  file; its name adds "(opens the large view)" with script and "(opens
  the full-size picture)" without, from two visually hidden spans the
  style sheet switches with `js-on`.
- **The pin.** An inline SVG map pin, its tip on the picture's centre
  (the game draws the preview around 0,0), in the thumbnail and the large
  view. White face, black edge and dot (`--color-pin-face`,
  `--color-pin-edge`, the same in both themes, because the map's own
  colours do not follow the theme), so it reads on every planet. It is
  `aria-hidden`; the size hint and the picture's alt text say what it
  marks.
- **Downloads** (#237). Under each picture, "Download Nauvis (PNG)"; on
  a world with several planets, one "Download all drawn planets (zip)"
  after the list. Both are links with `download`, each a 44px target.
  Files are named after the world, the seed and the planet.
- **The large view** is a native `dialog` opened with `showModal()`, so
  the browser keeps focus inside it and Escape closes it. It works as a
  picture viewer and fills the screen (`bg` face, no shadow). A bar holds
  the planet's name (h2, the dialog's name) with a download icon link beside it ("Download Nauvis
  map"), and under them a `role="status"` line whose height is held
  (content outside a modal dialog is inert, so the dialog says its own
  news). The key help sits in the bar too, at the end of the name's line
  while it fits: "Keys: ← → planet (pan when zoomed), + − zoom, 0 fit,
  Esc close. Tap to center." It is one line on a desktop and may take two
  at 320 px; the frame names it as its description. Then the picture in
  a scrolling frame, which has focus on open.
  - Over the picture: Previous and Next arrows on its left and right
    edges, named after where they lead ("Previous planet: Aquilo"); a
    round X in the top corner ("Close"); Zoom out and Zoom in in the
    bottom corner, the single-pointer way to zoom (WCAG 2.5.1). They show
    while the pointer is over the picture or a control in it has
    keyboard focus, and always under `(hover: none)`. Each is an
    icon button (8.8 icons below), 24 px, 44 px under
    `(pointer: coarse)`. Previous and Next step through the world's
    planets and wrap; a planet not drawn yet is drawn and shown in place.
  - Zoom is a factor of the fitted size, from 1 up to the larger of 8
    and 8 CSS pixels per tile (so a 3072 picture still reaches real
    tiles). Each wheel notch, plus or minus key, or zoom button
    multiplies it by 1.15; the wheel zooms around the pointer, the keys
    and buttons around the frame's centre, by setting the frame's scroll
    position, so the pin stays on the map with no sums. A zoom from a key
    or button is said in the status line ("Zoom 132%", from
    `Intl.NumberFormat`). The picture scales with `image-rendering:
    pixelated` (one tile is one pixel on 2.x).
  - Panning: drag the zoomed picture (pointer capture), or use the
    frame's scroll bars, or the arrow keys while the frame has focus. On
    touch screens one finger drags and two pinch around their middle
    (`touch-action: none` on the frame). A tap (one pointer, under 6 px
    of movement) on a zoomed picture centres the spot tapped, so panning
    never needs a drag (WCAG 2.5.7).
  - Keys: left and right arrows step (inside the zoomed frame they scroll
    it instead), plus and minus zoom, 0 fits, Escape closes; with Ctrl,
    Alt or Meta held the browser keeps its own zoom keys. Focus decides:
    a settings field (below) keeps every key of its own, so an arrow
    moves a slider and never the map; Escape closes from anywhere. Enter
    in a field there does nothing but end the edit; it never creates the
    world. Tab goes round the view's controls; from the last it returns
    to the first, and Shift+Tab the other way, never out to the browser.
    Close comes first in the view's body, so Tab reaches it right after
    the bar and before the settings, though it is drawn over the picture's
    top corner. While a download is being prepared (below), the left and
    right arrows do nothing, since stepping would end it; the first press
    says so once.
  - **Settings beside the map** (#399). Where the view is at least 77rem
    wide (a 1280 px desktop is), the planet shown has its Resources and
    Terrain settings in a 38rem column on the map's left, under the
    headings "Resources" and "Terrain" (h3, at `--text-lg` with
    line-height 1.3 over a `line` rule, as the panel names are without
    script, 8.7), with above them the hint "In a setting, the keys change
    the setting: an arrow moves a slider, not the map.", scrolling on
    their own; the hint sits in the column, not on the key line, which
    must stay one line at 1280 px. The
    map keeps the rest, never narrower than the column. 38rem leaves the
    column's fields a 34rem `map-section` after its padding and scroll
    bar, so they are sliders as on the page. Narrower, the view is map
    only, as before. They are the form's own fields, not copies: the
    script moves the planet's rows into the view as it opens (only in
    Map settings mode; an exchange string does not use them) and puts
    them back as it closes, however it closes, and before a version,
    Space Age, preset or reset redraw; a redraw already on its way when
    the view opens is let land first, so there is never a second copy.
    A tab that has no planet rows lends its fields only on a world of one
    planet. So the form sends, checks, marks "[modified]", resets,
    undoes and warns on leaving as for any change on the page. Moving to
    another planet moves its settings in. A change there redraws as one
    on the page does, 0.8 s after the last change, the newest request
    winning; while it runs the picture fades to 60%, the frame is
    `aria-busy`, and a line beside the status line, seen but not read
    out as on the page, says "Drawing the map preview for the current
    settings…". The status line (`role="status"`) then says "Map preview
    ready." It holds only that, warnings and errors: achievement and
    needed-resource warnings are said there, as the page around the
    dialog is inert. When the view narrows past 77rem while a field in
    the column has focus, focus moves to the map's frame.
  - Nothing animates under reduced motion; otherwise only the controls'
    opacity fades, in 120 ms.
  - On close, focus goes to the link of the picture last shown. A click
    with a modifier key opens the file as the browser would. When the
    view moves to another planet the page gets a `preview:planet` event,
    which opens that planet's settings row; an opened row tells the
    preview with a `settings:planet` event. The view starts from the
    hidden `#open-planet` field when the settings set it.
- **Demolisher territories** (Vulcanus, Space Age). A "Show demolisher
  territories" checkbox (7.2) under the One planet or All planets
  choice, shown only while Vulcanus's picture is shown, off by default,
  with the hint "Finding them takes a few seconds more." Every preview
  size offered can show them, so it is never turned off. It is a named field,
  so "Preview map" without script reads the territories after the
  pictures, in the same request; a ticked choice is kept in a hidden field
  while another planet is viewed. With script, the territories are asked
  for (`POST /generate/preview/territories`, the same form) only once the
  picture has arrived, so the picture never waits; the newest request
  wins, and a new draw, a change of settings or another planet viewed
  drops the one on its way. Meanwhile a line under the picture says
  "Finding the demolisher territories…"; that line's height is held while
  the box is ticked. In the large view the box shares one line with "Show resource
  amounts" (amounts first) where the view is 48rem wide or more, so paging
  to another planet only empties the line's end; narrower, its row is held
  on every planet of a map with Vulcanus, so the controls never move. Every
  line of the large view's header keeps one height whatever it holds: the
  status line is one line, cut rather than wrapped; the counting line is
  the words and a bar no taller than them, beside the box when wide and on
  a line always held under it when narrow; the title and its download
  button hold their place while a planet is drawn. Amount and territory
  labels are cut at the map's edge, and the map's frame pans without
  scroll bars, so paging, ticking or unticking either box, counting and its
  end never move a control or the map's edge.
  - The large view's territory box sits in the view's bar, under the
    status line and above "Show resource amounts", with its hint beside
    it while they fit and under it when they do not. It has no name; the
    script keeps it equal to the page's box (or the hidden field that
    carries the choice) and shows it only while Vulcanus is viewed. It
    is disabled while Vulcanus is still being drawn. It never moves
    focus, except that a box going away under focus hands focus to the
    picture.
  - The overlay is an inline SVG inside the picture's `.map`, in picture
    pixels (`viewBox` of the size, `preserveAspectRatio="none"`), so it
    scales and pans with the picture: each territory's chunk runs as
    rectangles filled with a stripe pattern matching the downloaded PNG
    (pixels where x - y modulo 56 is under 28, bars from top left to bottom right as in the game, `--color-territory-stripe`,
    rgb 200 20 20 at a third), one border path along every edge between
    owners and around the free start area (`--color-territory-border`, 2
    CSS px at any zoom with `vector-effect: non-scaling-stroke`); the start
    area has no stripes. Each territory's size is an HTML label placed by
    percent at its chunks' mean, white with a near-black ring of text
    shadows, so it keeps its size under zoom like the pin: in the
    thumbnail a short word ("Sm.", "Med.", "Big", "?",
    `territory.ShortLabel`) at 12 px regular, so it does not crowd a small
    picture; in the large view the full word ("Small", "Medium", "Big",
    "Unknown") at `--text-sm` bold. The overlay is `aria-hidden`; a
    visually hidden `p` under the picture says the same in words for
    screen readers: "Demolisher territories in this picture: 12 small, 6
    medium." Its line stays held while the box is ticked, so nothing
    under the picture moves when the counts arrive. The large view shows
    a copy of the same overlay.
  - While they are shown, Vulcanus's download reads "Download Vulcanus
    with territories (PNG)" and its file, and Vulcanus's file in the zip,
    have the overlay drawn in by the server (`?territories=1`), named
    with "-territories". Unticking takes the overlay, the list and the
    query away again.
  - A busy or failed run, or a failed request, is an error (7.3) under
    the picture with a "Try again" button; the page's picture stays.
    The announcement is the error sentence alone, not the button label.
    In the large view a failed request shows the same "Try again" in the
    view's box. It asks again, moves focus back to the box and says
    "Finding the demolisher territories..." in the status line.
- **Resource amounts** (every planet; SPEC 5.6, 17.15). A "Show resource
  amounts" checkbox (7.2) in the large view's bar, under the status lines,
  so it is there with the settings column and in a map-only view, and a
  second one on the One planet picture, under the territory box and above
  the picture, with its counting line, result and text list in a slot
  below the picture. Each has no `name`, so it is a view and never part of
  the form; each is off when the page loads and stays as set while the page
  lives, for every planet, and is its own choice. Both share the
  `.resource-amounts` block and one count path, and one count runs at a
  time.
  - **Asking.** The amounts are asked for (`POST
    /generate/preview/resources`, the form and the planet) only once the
    shown planet's picture is there, so the picture never waits. A new
    draw, a change in the settings column, another planet, unticking or
    closing aborts the request and removes the labels at once; the
    server's stored counts make going back quick.
  - **While counting.** A visible line, "Counting resources in the map…
    This can take up to a minute.", and an indeterminate `progress` in the
    `.wait` look (hidden under reduced motion). Unlike the drawing line, the
    sentence is announced once by the status line when a count starts, also
    after a redraw or a change of planet, so a person who cannot see the
    bar still knows it can take a minute; the bar itself is not named
    again. "Resource amounts ready." follows in the status line. At 320 px
    the map shifts down while the line shows; that is accepted. On the One
    planet picture a short "Counting…" shows beside the box while it
    counts, seen and not said (`aria-hidden`), as the line below the
    picture may be out of view at 320 px.
  - **Labels.** One HTML label per patch placed by percent of the picture
    inside `.map`, beside the territory overlay and before the pin,
    `aria-hidden`, in the same face as the territory labels (white with a
    near-black ring of text shadows, at `--text-sm`, bold; on the thumbnail
    at 0.75rem, weight 400, inset by the border, as the territory labels
    there), by the same CSS rule with a second selector, and kept in forced
    colours. The text is the amount through
    `Intl.NumberFormat(lang, {notation: "compact", maximumFractionDigits:
    1})`, such as "1.2M", "1,2 Mio." or "120万", and for a well its yield as
    `{style: "percent", maximumFractionDigits: 0}`; no new number strings.
    A greedy pass in the script measures each label once, works out its
    box at the map's current scale, and keeps labels in the server's order
    (largest patch first), hiding any that meets one already placed, a
    demolisher label or the pin. It runs when the labels arrive and from
    a `ResizeObserver` on the map, at most once a frame, with a 64 px cell
    grid so a few thousand labels stay cheap. Zooming in therefore shows
    more labels, and a label is never smaller or larger.
  - **The text list.** A collapsed `details`, "Resource amounts as
    text (N patches)", under the box; in the large view with the settings
    column it goes last in that column instead, so the header keeps one
    height, and the arrow keys still page planets from it, as it is not a
    setting. It is readable inside the dialog. A hidden `h3` heads it; each resource is an
    `h4` giving its total (or its total yield) and a list of its patches
    sorted by amount, each as "12,345,678 at (-50, -30)" or "300% yield at
    (50, -50)". One sentence says positions are tiles from where players
    start and that an edge cuts a patch. A picture without patches says so.
    Lists, not a table, so the reflow rules (SPEC 17.12) do not apply.
  - **Failures.** Busy, timeout, out of memory, missing image, failed and
    invalid each show an error (7.3) beside the box with "Try again" where
    a retry can work. The sentence is announced once, by a visually hidden
    `role="status"` in the bar, so the status line does not also carry it
    and it is not read twice; the button's label is not announced. When the
    list or the button had focus and the result is cleared, focus moves to
    the box. A page count that finishes or fails while the large view is
    open is said once on the page when the view closes.
  - **Downloads.** While a box is ticked, a counted planet's download link
    says "with resource amounts" (or "with territories and resource
    amounts" on Vulcanus) and its file, and that planet's file in the zip,
    carry the labels drawn by the server in the same look, worded as the
    page's `Intl` output, named with "-amounts". A planet not yet counted
    downloads plain; a download never starts a count.
  - **Territory counts.** On Vulcanus, with the territory box ticked, the
    visually hidden sentence that counts the demolisher territories in words
    appears inside the large view too, for the same reason: the page
    outside the dialog is inert.
- **A download's preparation** (SPEC 5.6, 17.1, 17.15). A download that
  asks for amounts or territories is counted first, so its link shows a
  block (`.download-prep`, a `template` in the page) instead of starting at
  once. The block has a hint saying which planet of how many is being
  counted, a bar that is seen and not read (`aria-hidden`, and never empty:
  half a step for the planet counted now, or the indeterminate bar for one
  planet), and a Cancel button. On a page it follows the link, or the zip's
  paragraph. In the large view it lies over the top of the stage, between
  the Previous and Next arrows and above zoom, so the picture keeps its
  place and no control or focus ring is covered; there the words are said
  by a visually hidden `role="status"` (`#large-prep-news`) in the bar and
  not shown twice. A failure replaces the wait with an error (7.3) naming
  the planet, with "Try again: (planet)" and, for the zip with other
  planets left, "Download without (planet)"; focus goes to the first of
  them when it was on the link or in the block. Cancel, a finished
  preparation, or the picture shown changing ends it, and focus in the
  block returns to the link. Only one runs at a time: another download link
  says so and moves focus to Cancel. A count waits while the pictures are
  being redrawn, and says so.
- **Icons** (the large view only; the rest of the interface uses the set
  in 7.9). Inline SVG on a 16 by 16 grid, drawn
  with a 2 px `currentColor` stroke and no fill, `aria-hidden`, inside a
  round `.icon-button` (`bg` face, 1 px `border`) whose name is visually
  hidden text. No icon stands without that name.
- **Without script** "Preview map" posts the form to
  `/generate/preview` and the answer is the same page, every value kept,
  a blank seed now holding the seed used, with every planet's picture;
  each link opens the picture itself.
- **Refusals** are errors (7.3) in the section, each with its next step
  as a control: the game image is not downloaded; another preview is
  running; the game failed (Generate still works). A request that fails
  outright puts "The map preview request failed..." in the section, from
  a `template` in the page.
- Pictures are deleted after 15 minutes.

### 8.9 Clone (SPEC 5.7, P1)

A world's page has "Clone world" (secondary). It opens
`GET /worlds/{id}/clone`, a plain `form.panel`, not the danger box,
since nothing is lost. From the Server page: the active world's link,
"Clone world", then "Clone world" on the form: three clicks.

- Heading "Clone Family base 2025", then a short `ul` of what is copied:
  "Its 38 mods, its settings, game version 2.0.77 and Space Age."
- Name, filled in with "Family base 2025 (copy)".
- A "Map" `fieldset` with two radios:
  - **Keep the save** (checked when the world has one). The label names
    the save and its time: "Keep autosave_3.zip, saved `<time>`", using
    section 9's time rules. It is the save the world would load (SPEC
    7.11).
  - **Start a fresh map**, hint "A new map with a random seed, made when
    the world first starts."
  A world with no save shows only the fresh-map line, as text, not a
  lone radio.
- "Clone world" (primary) and a Cancel link back to the world. The
  result is the new world's page with a Done message naming both worlds.

### 8.10 Game settings (SPEC 11)

A world's page and the Server page (for the active world) each have a
"Game settings" link. From the Server page, changing a setting in Listing
takes three clicks: Game settings, "Preview changes", "Save settings",
plus Restart when the game is running. A setting in any other section
takes one more, its sub-page.

The settings pages (form, raw editor and preview) carry the world's tabs
(6) with Game settings current, and no "Back to" link. The form's `h1` is
the world's name, under the breadcrumb "Worlds > *world* > Game settings".
Server name is required (7.2).

**Sub-pages.** Game settings is too long to read whole, so it is split
into eight sub-pages, listed under Game settings in the world's page
column (6, "Sub-navigation"): Listing, Players and access, Gameplay,
Saving, Network, Admins, Ban list and Whitelist. Each shows one section;
the plain settings link opens Listing, and the others add
`?section=<key>`. A sub-page's link has `aria-current="page"` and Game
settings then has `aria-current="true"`. There is no in-page list of
sections and no "Jump to section". The form is still one form: the other
sections stay in it, hidden, so a save leaves them as they are; while
the form has errors every section shows, so none is out of sight. A
player action's result opens on the sub-page it belongs to (bans, the
whitelist, or admins). "Edit server-settings.json as JSON" follows the
form as a plain link.

**One draft across sub-pages.** With script (`settingsdraft.js`), edits
made on one sub-page are kept in the tab's session storage and put back
on the next, so one "Preview changes" covers every section. The foot
bar's note counts them ("3 unsaved changes, kept while you move between
sections.", with an orange dot), and the rail marks each sub-page
that holds one with a dot. Saving or Cancel drops the draft. Without
script each sub-page saves on its own.

**The form** (`GET /worlds/{id}/settings`) is one `form.panel` of
fieldsets, one per sub-page, each a setting row: its legend in a title
column (240 px on a card at least 60rem wide) and its fields beside it.
On a card at least 72rem wide a section's fields flow two to a row. The
sections:

- "Listing": name, description (`textarea`, 3 rows), and visibility as
  two radios, "On the local network" and "Hidden".
- "Players and access": game password (below), maximum players (numeric,
  hint "0 means no limit"), returning players skip the cap, require user
  verification, AFK kick.
- "Gameplay": pause when no one is online, pause while someone joins (2.0
  and later), only admins can pause, who may use console commands.
- "Saving": autosave interval in minutes, autosave slots, and "Save
  without pausing" (fork saving), with its note about the experimental flag
  and memory (SPEC 11.1); disabled with its reason on a non-Linux host.
- "Network": upload limit and upload slots, under an "Upload limits"
  disclosure that starts open on this sub-page.

Every unset field shows what applies (SPEC 17.16): a number box is empty
with the game's default as its placeholder and in a hint ("Game default: 10.
Clear the box to use it."); a checkbox the file does not mention shows the
game's default state. Autosave slots on an older file without the key say
"Game default: 5" and that new worlds get 20.
- "Admins": a `textarea`, one player name per line, as its hint says. On
  a world that cannot be changed it is `readonly`, not disabled, so the
  keyboard can focus and scroll a long list; the other controls stay
  disabled.
- "Ban list" and "Whitelist" (8.13).

Then the sticky foot bar: the unsaved-changes note at its start, then
Cancel (a ghost button) and "Preview changes" (primary) at its end (7.1,
"One completion footer").

**Write-only fields** (SPEC 11.4). No value ever reaches the browser.

```html
<fieldset>
  <legend>Game password</legend>
  <p class="hint" id="pw-state">A password is set.</p>
  <div class="field">
    <label for="pw-new">New password</label>
    <p class="hint" id="pw-hint">Leave blank to keep the current password.</p>
    <input id="pw-new" name="game_password" type="password"
           autocomplete="new-password" aria-describedby="pw-state pw-hint">
  </div>
  <label class="check"><input type="checkbox" name="game_password_remove"> Remove the password</label>
</fieldset>
```

With no password set, the state reads "No password is set. Anyone who
can reach the server can join.", the hint "Leave blank to keep no
password", and there is no remove checkbox. A new password and remove
together is a field error: "Enter a new password or remove it, not
both." The factorio.com username, password and token follow the same
pattern if a page ever shows them.

**The raw editor** (`GET /worlds/{id}/settings/raw/{file}`):

- One `textarea` in `--font-mono` at 16px, `rows="24"`, `wrap="off"`,
  `spellcheck="false"`, `autocapitalize="off"`, `autocomplete="off"`,
  full panel width. Long lines scroll inside it; the page never scrolls
  sideways. No line-number gutter and no syntax colour: both need script.
- Secret keys are left out; the hint says "The game password is not
  shown here. It is kept unless you change it on the form."
- Errors use the error summary (7.2), all at once. Each names the line,
  the reason and the line's text: "Line 12: max_players must be a whole
  number. The line reads `"max_players": "ten",`". The `textarea` gets
  `aria-invalid` and `aria-describedby` pointing at the summary list,
  and keeps the text exactly as typed.
- "Preview changes" (primary) and Cancel, as on the form.

**The preview of changes** follows the 7.4 structure in a plain panel,
not the danger box, because the snapshot makes it undoable.

- Heading: "Save these changes to Family base 2025?"
- A `ul.changes`, one `li` per key: a bold leading word, "Added",
  "Changed" or "Removed" (like a step word in 7.6, so the kind never
  rests on colour), the key in `code`, then `<del>` old value and
  `<ins>` new value on lines of their own, each led by a visually hidden
  "Before:" or "After:". Values wrap with `overflow-wrap: anywhere`; no
  red or green fills. A secret shows no value, only "will be set",
  "changed" or "removed". An unknown key adds "Foreman does not check
  this key; it is saved as written." Admins read "Added admin Alice",
  "Removed admin Bob".
- What will happen, in order: save the running game (only when it
  runs), take a snapshot labelled for the settings change, write the
  file. Then the undo line naming the Snapshots page.
- When the world is running, a Note (7.3): "The server is running. These
  changes take effect after a restart." When only admins changed, it
  says instead that admins change at once.
- "Save settings" (primary) and "Go back and edit", a link back to the
  form or raw editor with the edits kept.
- No changes: "Nothing changed." with a link back and no save button.
- If the file changed since the preview, confirming shows this page
  again with a Warning: "server-settings.json changed since you
  previewed. These are the changes against the file as it is now." The
  Warning takes focus, as the "nothing left" note does, so the new diff
  is announced.

**After saving**, the browser returns to the settings form, on the
sub-page the preview came from, with a Done message naming the snapshot,
linked. When a restart is needed the
message ends with a "Restart server" link to the restart form (7.4), so
the next step is a control.

### 8.11 Mods (SPEC 9)

Every world has a Mods tab (6), `GET /worlds/{id}/mods`, between Snapshots and Game settings.
The Server page has a Mods link beside "World page" and "Game settings" for the
active world. The pages follow 7.2 (fields), 7.4 (the plan page is a
confirmation step) and 7.6 (the result), and use no new component but the
small ones below. All counts use plural messages (SPEC 22).

A mod's own text keeps its rich-text emphasis (SPEC 14.6): bold stays
`<strong>` (a heading font never changes the size), red is the danger text
colour, orange and yellow the warning colour, green the success colour,
blue, cyan, purple and pink the info colour, and gray the muted text colour
(`.rt-*` in `foreman.css`). A key-binding action shows in a bordered
`kbd`, and the game's warning sprite as the warning icon messages use.

**Mod names** are wrapped in `span.ident` (`translate="no"`,
`hyphens: manual; overflow-wrap: anywhere`) in tables, plan text,
conflicts, notices, headings and button labels, so a name such as
`visible-planets` never hyphenates or loses the space before it. Inside a
`.button` the name breaks only when it does not fit, and `.button .ident`
is `display: contents`. Mod badges are `white-space: nowrap`.

**Mod names (SPEC 9.15).** A mod is shown by its title, drawn by the
`mod-name` partial in `templates/modportal.html`: the title from the mod's
`info.json` or the portal, the slug as fallback, escaped, cut to 100
characters and marked `translate="no"`. A mod named on its own is a link to
`https://mods.factorio.com/mod/<name>` with `target="_blank"` and
`rel="noopener noreferrer"`, and the hidden text "on the mod portal, opens in
a new tab"; in the installed list and on the review page that name is the
link (a 44 px target), so every row has the same shape. A built-in mod, a
folder mod and a mod the portal lacks show plain text; the portal status
turns the name of a mod the portal does not have back into plain text. A
mod named inside a sentence, a button or a summary line is plain text, never
a link, because a link there fights the sentence or the toggle. The slug is
shown (`ident`) only where the title would be ambiguous. Saved records (a
snapshot label, an audit entry, a sent notification) keep the slug they were
written with.

**The Mods tab.** One `h1`, the world's name with the Active badge, and the
game's state badge beside it (7.5), then sections in the order below, each
a `section.panel` (a card, 7.8). The markup order is the order seen at
every width, so focus follows what is seen (WCAG 1.3.2, 2.4.3): "Plan",
then "Match mods to the save", where a save mismatch is seen before the
list it would change, then "Installed mods", then the two rarer tools
("Upload a mod", "Mod settings") inside one `details.disclosure`, "Mod
tools", so each tool appears once on the page. Below 64rem that
disclosure is a closed 44 px row after "Installed mods", with the tools'
full cards inside. From 64rem `subnav.js` opens it and hides its summary
(`data-open-wide`); "Plan" is alone in a 22.5rem rail beside the rest, so
nothing passes under it while it sticks, and every other card is in the
main column. A refused upload opens the disclosure, so the error summary's
link reaches the field. From 64rem the tools' buttons (Edit mod settings,
Upload a mod zip) are as wide as their words, not the column's width, so
they do not compete with the rows' own Update buttons; they are 40 px tall under a
fine pointer and 44 px on touch. The sections, in order:

- A Note when Foreman has no factorio.com account: "Mod downloads are off
  ... Searching still works", with a link to the account page that comes
  back to this tab.
- "Find a mod": one `role="search"` form, a field and Search, then
  "Mods matching" and "Plan" as described under Search below.
- "Match mods to the save": the tab reads the save that loads (SPEC 9.9)
  and says "The mods match the save base.zip." with no button, or "The mods
  differ from the save base.zip:" with a list of counts (not installed,
  disabled here, at another version, enabled and not in the save) and a
  secondary button to the sync plan. A save Foreman cannot read keeps the
  general sentence and the button; a world with no save says so and links
  to upload one.
- "Installed mods". A status block (`role="status"`, so a late answer is
  announced) loads after the page ("Checking the mod portal for updates…"),
  then shows one quiet line of counts in `text`, each led by its shape and
  not drawn as a tag: how many mods stop players joining, are deprecated,
  or are not on the portal, then how many have an update; or "All N mods
  from the mod portal are up to date", counting only mods the portal has at
  their installed version, and nothing about it when there are none. On a
  phone the line wraps to a second line. When there are updates, "Update
  all" is filled in beside the "Installed mods" heading: a secondary
  button, not the primary, carrying the number of updates in a small count
  tag. On a phone the heading, its count and the button keep one row,
  with the button drawn tighter (12 px sides, `--text-sm`). Orange on this
  tab is kept for a pending plan's Install button. The line is
  an Error box (7.3) only when a mod stops players joining. If the portal
  cannot be reached it says so with "Check again". There is no "Check
  again" otherwise: the portal's answers are kept 15 minutes, so it would
  show the same answer.
- One line for the built-in mods, which follow Space Age: "Built in,
  version 2.0.77: base, elevated-rails, quality, space-age. Space Age is
  on, so all of them are on. Change Space Age", or "Built in, version
  2.0.77: base. Space Age is off, so elevated-rails, quality, space-age are
  off. Turn on Space Age"; both links go to the Game settings tab. A game
  without Space Age has no last sentence and no link.
- A table (8.3) of the other mods in two columns, Mod and Actions, at the
  width of its column. The first cell holds the name (the portal
  link), the version as 13 px `text-muted` sans text with tabular figures
  (not a mono chip), a "Disabled" badge only when the mod is off (on is
  the usual state, so it has no word; the caption says how many are on),
  and a "Local folder" badge for a folder mod. Under them, filled in by the
  status, each flag of the availability check is one quiet 13 px line led
  by a 14 px icon for its kind (7.3, "A mod's portal news"); its leading
  word stays for screen readers: "Error: Players cannot join: the mod
  portal no longer has version 1.5.5 …", "Warning: Deprecated on the mod
  portal …", "Note: Not on the mod portal …". Each row has one visible action, a framed
  secondary button: "Update to X" when the status finds one (or as the fix
  for a gone version), or "Enable" for a mod that is off. Change version,
  Disable and Remove sit in the row's More actions menu at its end, in the
  same place in every row: a `details.row-menu` whose 44 px toggle shows
  three dots and the word "More" (its name is "More actions for *mod*"),
  opening a small panel of 44 px items. Change version shows only when the
  portal lists another release of the mod for the world's line, and opens
  the plan with the mod held at its version. Disable shows for a mod that
  is on. Remove is the last item, in `danger-text`, the only red control
  on the tab, since it is the one change that destroys something; its
  confirmation is the plan page. A folder mod has neither Change version
  nor Remove. The toggle is framed (1 px `line` edge, `raised` face) on
  phones and touch screens; only on a wide screen with a fine pointer does
  it rest frameless, taking its frame under the pointer. Opening one row's
  menu closes any other (`name="mod-menu"`); it works without script, and
  `menu.js` closes it on Escape, with focus back on the toggle, and on a
  click outside. The status fills Update and Change version into empty
  slots, so neither appears and then disappears. The action and the menu
  keep one line at the row's end. Each action's hidden text names its
  mod. Built-in mods are never in the table. When the panel is narrower
  than 36rem the row stacks: the menu stays on the name's line at its end,
  and the visible action takes a line of its own under the name and
  notes, so a name never breaks letter by letter (17.12: the table roles
  stay).
- "Upload a mod": one line, then a closed `details.disclosure` "Upload a
  mod zip" holding a drop zone (7.10) and the upload button, which goes to
  the plan page. A refused file opens it.
- "Mod settings", only when the world has a mod that is not built in, a
  `mod-settings.dat`, or a refused upload: the text, the "Edit mod
  settings" button to the editor and the download link stay in view; the
  replace form is in a closed `details.disclosure`
  "Replace mod-settings.dat": the help saying where the Factorio client
  keeps the file (Windows, macOS and Linux paths in `code`) and, for a
  running world, that Foreman restarts it; then the field, the countdown
  while the game runs, the 7.4 "This will" list and the button. Replacing
  is undone by its snapshot, so the form is not a red box. A refused file
  opens it.

On the mod settings page each mod's group is a `fieldset` with an id. Under a
mod that others name, a `p.hint.mod-origin` says "Required by", "Recommended
by" or "Offered by" and the naming mods, each a link to its own group (SPEC
9.6); those groups follow the ones nobody names. Without script the progress
line is followed by a link that loads the page with the form already read.

A change to a world that is not active reports on its own Mods tab: a
steps list (7.6) while it runs, then a Done message with "Undo this mod
change", which goes to the restore confirmation (7.4).

**Search and the plan panel** (SPEC 9.1, 9.2, 9.14). The Mods tab and the
search page (`/worlds/{id}/mods/search?q=`, the same results without
script) show three things in this order: "Find a mod", "Mods matching" and
"Plan". On a wide screen (64rem and up) "Plan" is alone in the rail,
beside the other two, and holds to the top of the screen
(`position: sticky`) while the results scroll past it. It is never taller
than the window below the top strip: the heading and Review stay in view
and only the list of mods scrolls, the newest pick at its top. In a window
under 30rem tall it does not stick. On a phone it
follows the results; once it has a mod it holds to the bottom of the
screen as a bar with the count ("2 mods in the plan"), the names cut to
one line, and "Review and install N mods". The bar's line is a `details`
summary that opens the full list (up to 40% of the screen, scrolling);
the panel's heading stays for screen readers and focus. Below 360 px the
bar keeps its frame while the other cards lie flat.

- "Mods matching" is always there. Before a search it says "Type at
  least 2 characters of a mod's name in Find a mod. Matching mods show
  here." and is drawn as one quiet line, not a card: its heading at
  `--text-md` in `text-muted` and the sentence beside it, inside a dashed
  `line` edge with no face, so the installed mods start higher on the
  first screen. With script, typing replaces only this panel 300 ms after the
  last key (htmx); under 2 characters the server sends that sentence back
  and asks the portal nothing. A reply that no longer matches the box is
  dropped. The count, the empty sentence or the error is read out through
  the shared announcer (7.5). On the search page the address and title
  follow the text without new history entries; on the Mods tab the
  address stays the tab's, and Enter searches in place.
- The results are a list (`ul`, named by its count line: "25 mods for
  Factorio 2.0 match. Showing 1 to 10."), ten rows a page, rows divided by
  rules, not cards. Each row: an `h3` whose title (weight 600, underlined
  only under the pointer) links to the mod's portal page in a new tab (hidden text "on the mod portal, opens in a new tab";
  a 24 px tall target), the name in `code` when it differs; the summary,
  two lines at most; then one line of facts as a `dl` (Latest, Downloads,
  Updated, the last only when the portal gives a date; `time.js` writes
  the date in the reader's language) with the row's control at its end.
- The control: "+" (a framed square, 44 px on touch, 32 px with a mouse,
  named "Add *mod* to the plan"); "Installed" badge; for a pick an "In
  plan" badge with a tick and a "−" named "Remove *mod* from the plan",
  the row keeping the card's face with a 3 px `info` (blueprint blue)
  start edge, and the badge in `info` on `info-bg` (the words carry it,
  never colour alone); for a mod in the plan only as a dependency a "Needed by *mod*"
  badge and no button. While the plan is worked out after "+" the row says
  "Adding…" beside the button. Changes to the plan go one at a time; a
  second click waits and is sent once the first answer is in.
- Paging is the same bar above and below the list, in three columns:
  "Previous page", a page box ("Page [n] of N" and Go, a plain GET form),
  "Next page". With script only the results panel is replaced and focus
  goes to its heading; without script they are plain links and forms. A
  page past the end shows the last page.
- "Plan" lists each pick with a quiet cross ("Remove *mod* from the
  plan"), its version, and under it, indented on a rule, each required
  dependency that changes, labelled "Needed by *mod*", nested as deep as
  it goes. A dependency two picks share shows once, under the topmost pick that
  needs it, its label naming every mod that needs it ("Needed by Beta and
  Alpha"); the newest pick is at the top. Built-in mods
  and installed mods that already fit are left out; an installed one that
  must change says "Will update to *x.y.z*" ("Will change to" for an older
  one, "Will be enabled" for one that is off). A dependency that cannot be
  had or conflicts shows a warning triangle and a short reason. Under the
  list: when the plan was started, that Review shows every change, and a
  quiet "Clear plan" in `danger-text` (no underline; `danger-bg` under the
  pointer), since clearing throws work away. Last, the primary "Review and install N mods"
  (each distinct mod counted once) opens the plan page with the picks
  (`kind=install&name=…`). An empty plan says "No mods in the plan yet.
  Choose + beside a mod to add it, then search again for the next." If
  the portal cannot be reached the picks are listed by name with a
  Warning and "Check again", and the button reads "Review the plan".
- Each change says what it did through the announcer ("Krastorio2 added
  to the plan. The plan has 2 mods.") and keeps focus where the hand is:
  on the row's new control, on the next cross in the panel, or on the
  panel's heading after Clear plan. Without script every control is a
  form that comes back to the same search, page and row
  (`#mod-<name>-hit`), or to the panel (`#plan-panel`). A "+" past the
  plan's 500 mods is refused with an error in the panel.

Under the field, on this page and on the Mods tab, a hint says a pasted
mod link works, with a plain link "Browse the mod portal" (hidden text
"(opens in a new tab)") to
`https://mods.factorio.com/browse/downloaded?factorio_version=<line>`
in a new tab.

**The plan page** (`/worlds/{id}/mods/plan`) is a 7.4 confirmation. The
`h1` is a question naming the world and the mod: "Install visible-planets
in Family base 2025?", "Install mods in Family base 2025?" for several,
"Change the mods of Family base 2025?" once a conflict's fix adds a mod to
turn off. The `h1` lays out as one run of text, so a mod's name has one
space each side and wraps like a word. The plan's answers live in a draft on
the server, in memory, under a random token the page carries (SPEC 9.14,
ADR 0022), so no address grows with the plan (SPEC 17.17); an entry link
holds only its kind and one name. Without script the form posts to the
redraw route and is redirected to `?draft=<token>`, so a reload keeps the
answers. A lost draft is rebuilt from the saved picks and a Note says so.
Every kind of change (install, update, enable, disable, remove,
sync, upload) has the same shape, changes first. In order:

- At most one of these at the top, each taking focus on a full page load
  and not on a redraw: a Warning "The plan changed since you saw it ...",
  an Error when the portal cannot be reached, an Error listing conflicts
  with every code worded (a conflict has no apply button). A conflict that
  turning off one installed mod clears carries its fix as a secondary
  button under it (SPEC 17.9): "Remove *mod* as part of this plan" for two
  mods that cannot run together (the one not asked for; a folder mod is
  disabled instead), "Disable *mod* too" for a mod another needs. The fix
  redraws the plan with both changes and puts focus on the "Changes:"
  label, or on the conflicts if one is left. An install plan's conflict box
  also has "Back to the mods", a link to the Mods tab, where the picks stay
  in the panel.
- A Note when the plan downloads and Foreman has no account, with the link.
- The required note and, when the change turns off or removes a mod the
  loading save uses, a Warning that says it once ("The save that loads
  uses foreman-test. Without it, the game deletes everything foreman-test
  built in the world.") with its required box ("Delete what foreman-test
  built. The snapshot "Before mod change" keeps a copy."). It is first so a
  phone shows it without scrolling. Unticked, the field error and the error
  summary (7.2) name the mod, or count several.
- "Changes:", a label (the box holds no heading, 7.4), then the changes
  grouped Install, Upgrade, Downgrade, Enable, Disable and Remove in a
  `dl`, one mod a line with its reason in brackets ("visible-planets needs
  it"). The mods an install asked for come first in their group, each line
  with its title as the portal link and, in the same row, its version list
  and a quiet cross named "Remove *title* from the plan" (a 44 px target).
  The mods the plan needs and leaves as they are fold into one
  closed `details`, "2 other mods stay as they are".
- For each mod an install or update names, a `select` "Version of *mod*"
  (SPEC 9.2): "Newest that fits (2.0.0)", then the releases for the
  world's line, newest first, the installed one marked "(installed)". One
  mod's list is a plain field with the hint under its label; several share
  a `fieldset` "Versions". Choosing one redraws the plan with htmx and focus
  stays on the list. While the chosen release is the installed one, "Nothing
  changes at this version" takes the place of the changes, and there are no
  steps and no apply button.
- "Optional mods" (and the recommended ones) as rows: the tick box with a
  hidden name, then the title link, the portal's summary, the version it
  would install and its release date. The link is its own target and never
  ticks the box. Ticking one redraws the plan with htmx; a "Recalculate the
  plan" button does it without script.
- "Mods no longer needed" (SPEC 9.4), a `fieldset` shown when a removal frees
  other mods: a hint ("Only the mods being removed need these. They are
  removed too unless you untick them."), then one ticked checkbox for each,
  "Mod, only needed by Mod". Ticking redraws the plan like an optional mod.
- For each unresolved mod a `fieldset` with its legend ("The mod portal
  does not have X 1.2.3"), radios for upload, nearest and continue without,
  and the zip field with its own Upload zip button tied to an empty form so
  choosing a file never redraws. "Nearest" has its hint beside it only when
  offered. Continue without shows its own required box.
- "This will:" and an ordered list of short steps: "Download 3 mods from
  the mod portal and check their checksums." (when it downloads), "Warn the
  players, then save and stop Base." (only while the game runs), "Take a
  snapshot labelled "Before mod change".", and "Change the mods, then start
  Base again." or "Change the mods. Base stays stopped, as it is now." Then
  one sentence for every failure, "If a step fails, the steps after it do
  not happen.", and the undo: the one-click Undo the result offers, not a
  trip to the Snapshots tab. A plan with a conflict leaves the steps out,
  since it cannot be applied.
- The countdown field while the game runs, then the confirm button, which
  repeats the verb and the count ("Install 2 mods", "Update 1 mod"; "Change
  2 mods" when a fix mixes kinds). Box and button are danger (7.1) only
  when the plan destroys something: it removes a mod, drops what a mod
  built from the save, or goes on "continue without". Anything else is
  primary in a box that keeps the confirmation's frame with a 4 px `info`
  (blueprint blue) top edge in place of the hazard stripe
  (`confirm--plain`), on a running world too: the steps say the restart.
  Cancel is a ghost button just before the confirm button (7.1). "This
  will:" is a station track: each step a numbered 28 px square plate
  (`raised`, 1 px `border`, the number in Plex Mono) joined to the next
  by a 2 px `line` rail; it is still an ordered list. On a box at least
  64rem wide the changes and choices lead in a main column, and "This
  will:", the countdown and the buttons sit in a column beside them,
  after a 1 px `line`.
- The review page has no search or "Add another mod". Adding happens only
  on the Mods tab (SPEC 9.2). Cancel and "Back to the mods" link to the Mods
  tab. Removing a mod on the review page redraws the plan in place with
  htmx, and `#announcer` says "Visible Planets removed from the plan. The
  plan has 1 mod."; focus goes to the "Changes:" label. Without script the
  cross is a button in a form that posts to the redraw route.

A redraw moves focus only when the control used is gone (a removal or a
fix, above), always answers 200 so htmx swaps it in, and says the new plan
in one sentence through `#announcer`; a conflict is announced, not focused,
unless a fix's redraw leaves one. Control ids follow the mod's name
(`mod-<name>-optional`, `-upload`, `-nearest`, `-without`, `-confirm`,
`-zip`) so focus survives a redraw. A refused zip is in the error summary,
which takes focus and links to the field, and its form carries every
answer on the page so none is lost. Each `.check` label wraps its words in
one `span`, so a name never becomes its own column at 320 px.

**The Server page notice.** Next to the status facts, where the game
network warning sits, a `role="status"` block loads after the page and
takes no room when empty. With one flagged mod: "visible-planets 1.5.5 is
gone from the mod portal, so players cannot join." and, on a line of its
own with a full-size target, "Update visible-planets" (or "Remove
visible-planets" when the portal has no release to update to). With
several: "Review N mods". It links straight to the plan, so fixing it is
two clicks (SPEC 17.10). When the pre-start refusal shows with the same
plan as its fix, the notice keeps its text and drops its button, so the
page has one "Update *mod*" button per plan.

**The account page** (`/settings/factorio-account`) leads with the state, as
one sentence: "Mod downloads are on. The account was stored on `<date>` by
`<user>`." or "Mod downloads are off: no account is set." (an unreadable
stored account leads with a Warning instead). An intro and a line that all
admins share this one account follow. Then one of three things. "Set by
environment" is a `section.panel` naming the two variables, with no form.
With a secret key and no stored account, the form is open: a username, and
a token as a password field with `autocomplete="off"` whose hint links to
the factorio.com profile page in a new tab, and a primary "Save account".
With a stored account the form is folded into a closed `details.disclosure`
"Replace the account", and its button and Remove are secondary, since
replacing is rare and the state sentence already answers the common
question; a refused replace opens it again. Nothing the account holds is
ever put in a value attribute or shown: after a refused form both fields are
empty and the page asks for both again. With no secret key and no
environment account the page is a note and a Go back link, with no
disabled form. A `return` parameter, checked as in SPEC 14.17, sends the
browser back to the page that asked after a save, and Cancel goes there
too.

### 8.12 Player actions (SPEC 8.2, Epic 9)

The task is "do something to a player who is in the game", and it starts
where the player is listed: the "Players online" card on the Server page.
Kick and ban go through a confirmation step (7.4); promote, demote, unban
and the whitelist act at once and offer Undo (17.9). A change sent to a
running game takes no snapshot, because the game writes its own list
file; the result says so in the confirmation, not in a warning.

**The player row.** The players table (8.3) keeps its two columns. The
player's name becomes the summary of a `details` in its cell, so opening a
player's actions is one click on the thing the admin is looking at:

```html
<tr role="row" id="pr-Alice">
  <td role="cell">
    <details class="player-actions disclosure" name="player-actions" id="pd-Alice">
      <summary id="ps-Alice">Alice<span class="visually-hidden">, actions</span>
        <span class="badge">Admin</span></summary>
      <div class="actions">
        <form method="post" action="/worlds/family-base-2025/players/demote">
          <input type="hidden" name="name" value="Alice">
          <input type="hidden" name="return" value="/">
          <button type="submit" class="button" id="pa-demote-Alice">Remove admin<span class="visually-hidden"> Alice</span></button>
        </form>
        <form method="post" action="/worlds/family-base-2025/players/whitelist-add">…<button … id="pa-wl-add-Alice">Add to whitelist…</button></form>
        <a href="/worlds/family-base-2025/players/kick?name=Alice" class="button button--risky" id="pa-kick-Alice">Kick<span class="visually-hidden"> Alice</span></a>
        <a href="/worlds/family-base-2025/players/ban?name=Alice" class="button button--risky" id="pa-ban-Alice">Ban<span class="visually-hidden"> Alice</span></a>
      </div>
    </details>
  </td>
  <td class="num" role="cell" data-label="Online for">1 h 4 min</td>
</tr>
```

- **Order of the buttons**: the change that acts at once first ("Make
  admin" or "Remove admin", then "Add to whitelist" or "Remove from
  whitelist"), then the two risky ones, Kick then Ban, drawn risky (7.1),
  since each opens a confirmation. Every button names the player in hidden
  text after its visible word, so the label is in the name (SPEC 22.7) and
  a screen reader's list of buttons tells the rows apart.
- **The admin mark** is the existing neutral `badge` (8.2's pill without
  the `--active` tint) with the word "Admin", after the name. It is read
  from the world's admin list, so the row offers "Make admin" or "Remove
  admin" and never both.
- **The whitelist buttons** show only while the running game keeps the
  whitelist, that is, when it was started with "Only whitelisted players
  can join" on (8.13), and when the list can be read.
- **The ids** are a prefix and the player's name: `pr-` (the row), `pd-`
  (the `details`), `ps-` (the summary) and `pa-<action>-` for each button
  (`promote`, `demote`, `wl-add`, `wl-remove`, `kick`, `ban`). No prefix
  begins another or any fixed id on the page, so no name can make one id
  into another.
- `details[name="player-actions"]` makes the rows an exclusive group: one
  player's actions are open at a time. Without support for `name`, several
  may be open, which is harmless. The summary is a full-row target at least
  `--target-min` tall with the disclosure chevron at its end (7.8); the
  name wraps with `overflow-wrap: anywhere`; the `badge` never wraps inside
  itself. Open, the buttons wrap under the name inside the first cell, and
  on a phone each takes the cell's full width (6, rule 5).
- **Live updates.** The players region is swapped when the list changes.
  An open row must stay open and keep focus across a swap: every summary
  and button has an id built from the player's name, and the script that
  swaps the region copies `open` from the old `details` to the new one with
  the same id before swapping (htmx already returns focus by id). A player
  who left takes their row, open or not, with them; focus then goes to the
  players heading (`#players-heading`, given `tabindex="-1"`).
- Rows show actions only while the game runs and the list is known. A name
  the game reports that fails the name check (SPEC 14.9: a space, a control
  or invisible character, over 64 bytes) is shown as plain text with no
  summary: Foreman never escapes a name to act on it.

**One URL scheme.** Every player action posts to
`POST /worlds/{id}/players/{action}` with the name in a `name` field
(`kick`, `ban`, `unban`, `promote`, `demote`, `whitelist-add`,
`whitelist-remove`), plus `reason` for kick and ban and a `return` path
(SPEC 14.17). The name never goes in the path, so it needs no escaping
there. Element ids carry the name as it is, which is safe because a name
that passed the check has no spaces.

**Kick and ban confirm** (7.4), at `GET /worlds/{id}/players/kick?name=`
and `…/ban?name=`, in `playeraction.html`. Like the stop form, the page has no
tabs; its `h1` is the question and Cancel is the way out.

```html
<h1 id="confirm-heading">Ban Alice from Family base 2025?</h1>
<section class="confirm">
  <p>This will:</p>
  <ul>
    <li>Disconnect Alice now, if they are in the game.</li>
    <li>Add Alice to the ban list, so they cannot join again until unbanned.
        The ban list is on the world's Game settings tab.</li>
    <li>Take no snapshot. The game records the ban in its own ban list.</li>
  </ul>
  <form method="post" action="/worlds/family-base-2025/players/ban">
    <input type="hidden" name="name" value="Alice">
    <input type="hidden" name="return" value="/">
    <div class="field">
      <label for="player-reason">Reason</label>
      <p class="hint" id="player-reason-hint">Optional. Alice sees it. One line, up to 200 characters.</p>
      <input id="player-reason" name="reason" type="text" maxlength="200" autocomplete="off" aria-describedby="player-reason-hint">
    </div>
    <div class="actions">
      <button type="submit" class="button button--danger" id="act-confirm-ban">Ban Alice</button>
      <a href="/">Cancel, keep Alice in the game</a>
    </div>
  </form>
</section>
```

- Kick reads "Kick Alice from Family base 2025?" with one list item,
  "Disconnect Alice now. They can join again at once.", then the
  snapshot line. Its button is "Kick Alice". Ban's list is as above.
- The reason is optional, one line (no NUL, CR or LF; the server refuses
  them with a field error, never strips them) and at most 200 characters.
  It is player-facing text, sent as typed.
- If the player left between the row and the confirmation, the kick page
  says so in a Note ("Alice is no longer in the game, so there is no one
  to kick.") with a link back to the Server page and no button (SPEC
  17.8). A ban still works on a name that is not online, so the ban page
  keeps its button and drops "Disconnect Alice now".
- Cancel returns to the page the admin came from (the `return` parameter,
  checked as in SPEC 14.17): the Server page, or the Game settings tab when the
  ban started from the ban list (8.13).

**The result** shows in the players card, at its top, above the table,
not in the status card: the result belongs beside the thing that changed
(7.3). It is a Done message (`#player-done`, `tabindex="-1"`), and focus
moves to it after the redirect, as the Server page's other results do.

```html
<div class="message message--success" id="player-done" tabindex="-1">
  <p><strong>Done:</strong> Alice is now an admin of Family base 2025.</p>
  <div class="actions">
    <form method="post" action="/worlds/family-base-2025/players/demote">
      <input type="hidden" name="name" value="Alice">
      <input type="hidden" name="undo" value="1">
      <button type="submit" class="button" id="act-player-undo">Undo<span class="visually-hidden">: remove Alice as admin</span></button>
    </form>
  </div>
</div>
```

- Each change that acts at once offers Undo, which posts the opposite
  change at once: Make admin and Remove admin undo each other, as do Add to
  and Remove from the whitelist. A ban's result offers "Unban Alice" in the
  same place (it acts at once). A kick has nothing to undo, so its Done
  message stands alone.
- The result is one-shot: it shows on the page the action redirected to,
  and a reload drops it. The Undo button needs no state, since it is only
  the opposite action.
- A failure is an Error in the same place: "The game did not answer over
  RCON, so Foreman cannot tell whether Alice was kicked. Check the players
  list, then try again." The game's own reply to a refused command (for
  example a name it does not know) is shown after the sentence, marked
  `lang="en"`, as console replies are (SPEC 22.3).
- With htmx, the action may swap only the players card and send the Done
  sentence to `#announcer`; without script the redirect does the same.

Strings: the row buttons, the kick and ban pages and the results go in
the `players` area (`players.kick.heading`, `players.ban.button`,
`players.done.promoted`, `players.undo.demote` and so on); the "Admin"
badge word too.

### 8.13 The ban list and whitelist (SPEC 8.3, Epic 9)

The world's Game settings tab (8.10) has "Ban list" (`#settings-bans`) and
"Whitelist" (`#settings-whitelist`) as its last two sub-pages, after
"Admins".
How a list is edited depends on whether the game runs, because that
decides who writes the file.

**While the game runs**, a list is sent to the game over RCON and the
game writes its own file, so no snapshot is taken (SPEC 8.2) and each
change acts at once (SPEC 8.2). The two lists are then not fieldsets of
the settings form but two cards of their own, right after it (before the
raw editor's link), with their own small forms. A button inside the
settings form would post the whole form and lose whatever the admin had
typed in its other fields; cards of their own cannot. Each shows on its
own sub-page all the same. The name rows are the `ul.changes` list that the
settings preview uses (8.10); no style of their own is added.

```html
<section class="panel" id="settings-bans" aria-labelledby="bans-heading">
  <h2 id="bans-heading">Ban list</h2>
  <p class="hint">The server is running, so changes here go to the game at once. No snapshot is taken.</p>
  <ul class="changes" role="list" aria-labelledby="bans-heading">
    <li><span>Bob</span> <span class="hint">griefing</span>
      <form method="post" action="/worlds/family-base-2025/players/unban">
        <input type="hidden" name="name" value="Bob">
        <input type="hidden" name="return" value="/worlds/family-base-2025/settings">
        <button type="submit" class="button" id="act-unban-Bob">Unban<span class="visually-hidden"> Bob</span></button>
      </form></li>
  </ul>
  <form method="get" action="/worlds/family-base-2025/players/ban" class="field">
    <label for="ban-name">Ban a player</label>
    <p class="hint" id="ban-name-hint">The player's name. You can give a reason on the next page.</p>
    <input type="hidden" name="return" value="/worlds/family-base-2025/settings">
    <div class="console-row">
      <input id="ban-name" name="name" type="text" autocomplete="off" autocapitalize="off" spellcheck="false" aria-describedby="ban-name-hint">
      <button type="submit" class="button button--risky" id="act-ban-name">Ban</button>
    </div>
  </form>
</section>
```

- "Unban" acts at once and the Game settings tab returns with a Done message
  at its top (the existing `#done-message`), "Bob is unbanned from Family
  base 2025.", with "Undo" (it bans Bob again at once, with the reason the
  file had). "Ban" goes to the ban confirmation (8.12) with the name
  filled in and `return` set to the Game settings tab, because a ban always
  confirms. A name that fails the check (SPEC 14.9) is a field error on
  the confirmation page's return, never sent.
- A ban list entry shows its reason after the name, in `hint` style, when
  the file has one. Names and reasons are shown as written (SPEC 22.3).
- The whitelist card (`#settings-whitelist`) is the same: one `li` per
  name with "Remove from whitelist" (acts at once; Undo adds the name
  back), then "Add a player" with "Add to whitelist" (acts at once; Undo
  removes it).
- An empty list is one `p.empty` line: "Nobody is banned." / "Nobody is
  on the whitelist."
- The Admins fieldset is unchanged by this epic: it stays in the form, as
  8.10 describes.

**While the game is stopped** (or the world is not the active one), the
game is not there to write its files, so Foreman writes them, and the
change takes the usual settings snapshot (SPEC 11.3). Each list is then a
fieldset of the settings form after Admins, holding a `textarea` like
Admins (8.10): one name per line, `rows="5"`, with
`autocapitalize="off"` and `spellcheck="false"`, saved through "Preview
changes" with the rest of the form.

- Hints: "One player name per line. Reasons already in the file are kept;
  a name added here has no reason." (ban list) and "One player name per
  line." (whitelist).
- The preview of changes (8.10) lists them as it lists admins: "Banned
  Bob", "Unbanned Carl", "Added Dana to the whitelist", "Removed Eve from
  the whitelist", under "Changes to the ban list" and "Changes to the
  whitelist". The steps write `server-banlist.json` and
  `server-whitelist.json` after the one snapshot.
- A list file that is not a list of names is read-only, as Admins is,
  with the same "Fix the file in the world's config folder" hint.
- Each bad line is a field error naming the line, as Admins does.

**Whitelist enforcement.** A whitelist only keeps players out when the
game enforces it, and an in-game `/whitelist enable` is lost when the game
restarts. So enforcement is a per-world switch, "Only whitelisted players
can join", with the Note (7.3):

> **Note:** The game checks this list only while "Only whitelisted players
> can join" is on.

The switch is a checkbox and a "Save whitelist enforcement" button that
post their own small form (`form` attribute), so the switch saves apart
from the settings form. Where it sits follows how the list is edited.
While the list is edited through the settings form (the game is stopped,
or the running game does not keep the list), the switch, its note and
its button sit in a card of their own, "Whitelist enforcement", right
after the settings form, never inside it, so each save button saves one
thing; under the whitelist field a hint says "List changes are included
in Preview changes." While the list is edited live, the switch stays in
the live whitelist card (above). The form's action, the field's name and
what the switch does are the same in both places. On, the game starts with
`--use-server-whitelist --server-whitelist <file>`; off, with neither.
The game reads it only at start, so on a running world the hint says a
change takes effect after a restart, and while the running game was
started the other way a hint and a Restart server button follow it, as
the settings page does for other settings that need a restart. The
whitelist is edited live (the card above) only while the running game
was started with the switch on, since only then does the game keep the
file; otherwise Foreman writes it through the settings form.

Strings: legends, hints, list labels, empty lines and the preview lines go
in `settings`; the live actions' results and Undo labels in `players`.

### 8.14 Chat (SPEC 8.5, Epic 9)

Chat is the third tab of the Server view: Overview, Console, Chat
(`GET /chat`, `chat.html`). It is a tab of its own
because reading and answering players is a task of its own; it is apart
from the Console because a chat line is never a command. The page has
the Server page's overline and `h1` (its state word for screen readers
only, 7.5), then the Server tabs with Chat current.

```html
<section class="panel" aria-labelledby="chat-heading">
  <h2 id="chat-heading" class="visually-hidden">Chat</h2>
  <div class="log-frame">
    <div class="log" tabindex="0" role="log" aria-label="Chat messages"><ol class="chat-lines">
      <li><time datetime="2026-11-12T19:04:11Z">19:04</time>
        <span class="chat-name">Alice</span> <span class="chat-text" dir="auto">is anyone near the oil field?</span></li>
      <li class="chat-line--server"><time …>19:04</time>
        <span class="chat-name">Server</span> <span class="chat-text" dir="auto">todd: on my way</span></li>
    </ol></div>
    <button type="button" class="button" id="chat-jump" hidden>Jump to newest</button>
  </div>
  <form method="post" action="/worlds/family-base-2025/chat" id="chat-form" novalidate>
    <div class="field">
      <label for="chat-message" class="required">Message</label>
      <p class="hint" id="chat-hint">Players see this in the game as a message from the server.</p>
      <div class="console-row">
        <input id="chat-message" name="message" type="text" maxlength="500" autocomplete="off" aria-describedby="chat-hint">
        <button type="submit" class="button button--primary" id="act-chat-send">Send</button>
      </div>
    </div>
  </form>
</section>
```

- **History above, field below**, newest last, as in any chat. The box is
  `min(50vh, 24rem)` tall, so at 320 by 640 the field is on the first
  screen under it. It follows new lines while at the bottom and offers
  "Jump to newest" otherwise, exactly as the game log does (8.5,
  `log-frame`).
- **The lines** are the game's `[CHAT]` lines since the container
  started (the same history the game log keeps, SPEC 8.6), at most the
  last 200 in the page; older ones drop from the top. Each line is the
  time (clock only, from `time.js`; the title gives the full time), the
  name in weight 600, and the message as written, with `dir="auto"` and
  `overflow-wrap: anywhere`. A message the server sent shows the name
  "Server"; the game's own prefix is not shown. Player text is never made
  into a link.
- **Live.** New lines are appended, never re-rendered. The list is
  `role="log"`, a polite live region, because chat is written at human
  speed and someone following it by ear needs to hear new lines. (The game
  log is not one, 8.5.)
- **Sending.** One line of 1 to 500 characters, with no NUL, CR or LF.
  With htmx the form posts in place; on success the field empties and
  keeps focus, and the message arrives in the list from the game's own
  echo, so it is never shown twice. Without script the post redirects
  back to the Chat tab.
- **A message that starts with `/`** (after any spaces) is refused, never
  sent: a field error reads "A chat message cannot start with /. To run a
  command, use the Console." with "Console" a link to the Console tab. So
  a typed command never runs, and the admin is pointed at the place that
  does run it.
- **Not running.** When the game is not running there is no chat to show
  or send: the section shows the history it still has, if any, then
  `p.empty empty--art empty--chat`: "The server is not running, so no one
  can read chat. Start it from the Server page." with "Server page" a link.
  With no history and the game running: "No chat since the server
  started."
- RCON failure: an Error above the form, "The game did not answer over
  RCON, so the message was not sent.", and the field keeps the text.

Strings: all in the `chat` area (`chat.tab`, `chat.message.label`,
`chat.error.slash`, …), plus the jump button and the "Message sent"
announcement in `js.json` under `chat.`. The tab's word goes in `common`
with the other tab words.

### 8.15 Foreman settings: the restart schedule (SPEC 7.12, Epic 9)

Foreman has eight pages that belong to Foreman, not a world, listed in
one sub-navigation (6, "Sub-navigation"), named "Foreman settings": in
the product rail from 64rem, a tab row under each page's `h1` below it.
In order: General, Restart schedule, Notifications
(8.16), Public mods (8.20), API tokens (8.21), Users, Audit log and the
factorio.com account (8.11). The main navigation's Settings tab (6) opens
General and is marked current on all of them and the pages under them. Moving between them is one click, and reaching any of them from any
page is two at most. A page that sits under a tab, such as the General
confirm page or a user's delete page, marks that tab `aria-current="true"`,
not `"page"`, since it is the way to the tab but not the tab itself.

**The schedule page** (`GET /settings/restart-schedule`,
`schedule.html`). One schedule restarts whichever world is active
(SPEC 7.12). The page leads with its state in one sentence, as the account
page does:

- On: "Foreman restarts the active world every day at 04:00
  (Europe/London). Next restart: `<time>`." The `time` is local to the
  viewer, as everywhere (SPEC 17.11), while the set time is shown in the
  schedule's own zone, named.
- Off: "No restart is scheduled."

Then a short "How it works" list, in plain words, before the form, because
the behaviour is the thing an admin must trust:

1. At the set time, Foreman waits until nobody is online.
2. Then it restarts the server after a 60-second warning in the game.
3. If someone joins during the warning, the restart stops, they are told
   in the game, and Foreman waits again.
4. If players are still online when the wait limit runs out, that day's
   restart is skipped.
5. A stopped or crashed server is never started by the schedule, and a
   restart missed while Foreman was not running is not made up.

The form is a `form.panel`:

```html
<form method="post" action="/settings/restart-schedule" class="panel" novalidate>
  <label class="check"><input type="checkbox" id="schedule-on" name="enabled" checked> Restart the active world every day</label>
  <div class="field">
    <label for="schedule-time" class="required">Time</label>
    <input id="schedule-time" name="time" type="time" value="04:00" required>
  </div>
  <div class="field">
    <label for="schedule-zone" class="required">Time zone</label>
    <select id="schedule-zone" name="zone" required>
      <optgroup label="Europe"><option value="Europe/London" selected>Europe/London</option>…</optgroup>
      …
    </select>
  </div>
  <div class="field">
    <label for="schedule-limit" class="required">Wait for players to leave for up to</label>
    <p class="hint" id="schedule-limit-hint">Hours, 0 to 12. 0 skips the restart at once if anyone is online.</p>
    <input id="schedule-limit" name="limit_hours" type="text" inputmode="numeric" value="1" autocomplete="off" aria-describedby="schedule-limit-hint">
  </div>
  <div class="actions">
    <button type="submit" class="button button--primary" id="act-schedule-save">Save schedule</button>
    <a href="/">Cancel</a>
  </div>
</form>
```

- **Time** is the native `input type="time"`, in steps of one minute. The
  browser shows it in the viewer's 12- or 24-hour style; the value is
  always `HH:MM`.
- **Time zone** is a native `select` of the IANA zones, grouped by region
  in `optgroup`s, each shown as its id (`translate="no"`). A new schedule
  starts with the viewer's own zone selected, set by a small script from
  `Intl.DateTimeFormat().resolvedOptions().timeZone`; without script, UTC.
- **Wait limit** is whole hours, 0 to 12, default 1 (SPEC 7.12).
- **Turning it off** is unticking and saving. The fields stay, with their
  values, so turning it back on is one tick. While unticked the fields are
  not required, and the server ignores them.
- The countdown is fixed at 60 seconds and is not a field (SPEC 7.12).
- After saving, the page returns with a Done message (`#done-message`)
  naming the next restart time: "Saved. The next restart is `<time>`." or
  "Saved. No restart is scheduled."
- Field errors follow 7.2: "Enter a time such as 04:00.", "Choose a time
  zone from the list.", "Enter a whole number of hours from 0 to 12."

**On the Server page.** The schedule shows in the status card in three
ways, none of them a new component:

- **Next restart.** While the schedule is on and the game runs, a fact
  "Next restart" sits under the facts, in a region of its own
  (`sse-swap="schedule"`, `div.schedule-next`), like "Loads next" (6):
  its value is a quiet button-faced link, the `time` as its text, to the
  schedule page (`#act-schedule`), named "`<time>`, change the restart
  schedule". It is its own region because the facts region holds nothing
  focusable. When the game is stopped the fact is not shown, since a
  stopped game is never restarted.
- **Waiting for nobody online.** From the set time until the restart or
  the skip, the control region shows a Note (7.3, `#schedule-waiting`):
  "Note: A scheduled restart is waiting for nobody to be online. If players
  are still on at `<time>`, it is skipped today." with a link "Restart
  schedule" to the page (17.9). When `/players online` cannot be asked,
  Foreman counts that as someone online (SPEC 7.12) and the note
  says: "Foreman cannot ask the game who is online, so it waits as if
  someone were." Restart, Stop and the other controls stay as they are:
  the admin can still restart by hand (that ends the wait).
- **The 60-second countdown** is the restart operation's own step list
  (7.6), under the heading "Scheduled restart" instead of "Restarting",
  with "Warn players: 40 seconds left" and "Cancel stop", exactly as a
  restart the admin started. If a player joins, Foreman presses Cancel
  stop itself, and the card goes back to the waiting note with one more
  sentence: "Alice joined during the warning, so the restart waits again."
  If an admin presses Cancel stop, that day's scheduled restart is
  skipped, and the note below says so.
- **Skipped.** A Note in the control region (`#schedule-skipped`):
  "Note: Today's scheduled restart was skipped at `<time>` because players
  were still online. The next is `<time>`." (or "…because an admin
  cancelled it."), with the "Restart schedule" link. It stays until the
  next scheduled time or until another operation's result takes the card.
- A completed scheduled restart ends like any restart: the Done message
  "Restarted Family base 2025 on schedule."

Strings: the page, its fields and errors go in `schedule`; the Server
page's fact, notes and the operation heading in `server`; the menu and
sub-navigation link words in `common`.

**The General tab** (`GET /settings/general`, `generalsettings.html`, SPEC
13.1). One form, `.setting-rows`, with one section per setting group, each
a heading and a hint beside its number fields, in this order: snapshot
retention, audit retention, the default stop countdown, and the save and
mod upload limits. When the form is at least 92rem wide its groups sit
two to a row, the two retention groups first, then Stopping the server
beside Uploads, each pair under one rule; the markup order matches, so
reading and tab order go row by row. A hint under each field states the
range. The form ends in the sticky foot bar (6, "The 320 px rules", rule
7), 24 px below the last field: the line that says each value is "set
here", since none has an environment form (17.16), leads it, and "Save
settings" ends it. Each
field is a text input with `inputmode="numeric"`, never a number spinner. A
bad value refuses the whole form, with the errors in the summary and beside
the fields (7.2). A save that lowers retention below what worlds hold shows
the **confirm page** (7.4, `generalconfirm.html`) first: it lists the
snapshot and world counts, a line saying removed snapshots cannot be
restored and that raising the figure before the next automatic snapshot
keeps them, and each change from old to new, with one danger button and a
Cancel link. After any save, the Done message repeats the count when it
is above zero.

**The Users tab** (`GET /settings/users`, `users.html`). The list leads, in
`.token-list` like the API tokens (8.21): each user holds the name (with a
"you" badge on your own), how the user signs in (password, proxy or single
sign-on) and the day they were created, and Delete (a red ghost, 7.1) on
every user but you. On a narrow card each user is a card of its own; once
the list's card is 34rem wide the same markup is laid out as table rows
under one row of column names (Username, Sign-in, Created; `aria-hidden`,
since each row keeps its own fact names for screen readers). "Your
password" sits beside the list in a rail on a wide screen (a `.split`),
under it on a narrow one: a form of the current password, the new one and
its confirmation, for a user with a password only. A proxy or single sign-on user sees a sentence
saying their sign-in is managed there, and no form. Delete leads to a
**confirm page** (7.4) that names the user, lists what will happen (signed
out, tokens deleted with a count, audit entries kept), shows a warning
message for a proxy or single sign-on user (they come back unless removed
at the proxy or provider), and has one danger button and no typed name.

**The Audit log tab** (`GET /settings/audit-log`, `auditlog.html`). The
filters and the entries are one card, like the other settings pages. The
filter form is its head band, one row that wraps only when it must (two to
a row at 320 px): three selects (user, action, world), each with an "all"
first choice, "Entries per page" (25, 50, 100 or 200; 50 is the default),
one Filter button (secondary) and, when a filter is on, a "Clear filters"
link, which keeps the page size. It is a plain GET form, so a filtered view
can be bookmarked. The entries follow as a real table under a heading
"Entries" for screen readers only, with six fixed columns, in order: Time,
User, API token, Action, World and Detail. The column names stay in view
while the rows scroll, and every cell starts on the row's top line, so a
wrapped detail leaves the short cells level. On a wide card User and World
keep one line, and from 64rem rows are compact, at `--text-sm`.
The token cell is empty when no token was used. A world that still exists
is a link, with its current name; a deleted one shows its id and the word
"deleted". The links "Newer entries" and "Older entries" sit at the foot
in a navigation named "Log pages"; both keep the filters and a page size
other than 50. The newest page has no Newer link and the last page has no
Older link. A link to a page past either end of the log goes to the first
page. An empty log, and an empty filtered view, each say so.

*Narrow width.* Where the entries section is under 52 rem wide (a container
query, so it follows the section, not the screen), each row stacks into a
compact block: the action is the title, then Time, User, API token and
World as inline label and value pairs that wrap, then Detail on its own
line. An empty token or world cell is hidden from sight but still read, so
a screen reader gives each cell under its own column. The column headers stay
for screen readers. Chosen over a table that scrolls sideways because six
columns cannot fit 296 px in any of the eight languages, and a sideways
scroll hides Detail and the token off screen with nothing to say they are
there; it would also need a focusable scroll region. The stack follows the
`data-label` pattern the other narrow tables use. A short entry is about
130 px tall at 320 px. The page uses a keyset cursor (time and id), not an
offset, so entries that arrive while a page is open do not shift or repeat
rows.

### 8.16 Notifications (SPEC 8.10 to 8.12, Epic 9)

`GET /settings/notifications` (`notifications.html`), under the
"Foreman settings" sub-navigation (8.15). Webhooks belong to Foreman as a
whole, and every message names its world.

**Without `FOREMAN_SECRET_KEY`** the page is a Note and a link back, with
no disabled form, as the account page is (8.11): "Note: Notifications
need `FOREMAN_SECRET_KEY`, because Foreman keeps webhook addresses sealed
with it. The operator guide says how to set it." Existing webhooks, if the
key was removed, are not listed, since their addresses cannot be read.

**With the key**, "Add a webhook" (primary, `#act-webhook-add`), the
page's main action, sits at the end of the `h1`'s row, as Generate does on
Worlds. Then one card holds a sentence of state, "2 webhooks get
messages.", and the list, or the empty state.

```html
<ul class="webhook-list" role="list">
  <li id="webhook-3">
    <h2 class="webhook-list__title">Discord <span class="ident" translate="no">discord.com</span></h2>
    <dl class="facts facts--inline">
      <div><dt>Language</dt><dd>English</dd></div>
      <div><dt>Events</dt><dd><ul class="chips" role="list"><li>Start</li><li>Crash</li><li>Low disk</li></ul></dd></div>
    </dl>
    <div class="actions">
      <a href="/settings/notifications/3" class="button" id="act-webhook-edit-3">Edit<span class="visually-hidden"> the Discord webhook to discord.com</span></a>
      <a href="/settings/notifications/3/delete" class="button button--remove" id="act-webhook-delete-3"><span aria-hidden="true">Delete</span>
        <span class="visually-hidden">Delete the Discord webhook to discord.com</span></a>
    </div>
  </li>
</ul>
```

- **Only the preset and the host** are shown, never the address, its path,
  query or token (SPEC 14.8): not in the page, a value
  attribute, a title, a log line or an error. Two webhooks to the same
  host look alike; their events and languages tell them apart.
- The list is the world list's shape (8.2): rows separated by a 1 px
  `line`, the title as the row's heading, `facts--inline` for the facts,
  and the actions after them; from 64rem Edit and Delete sit side by side
  at the row's end, each as wide as its word. The events are 12 px chips
  (`raised`, a 1 px `line` edge, fully rounded) that wrap under the label.
  With every event on, Events reads "All events".
- **The empty state** is `p.empty empty--art empty--notify`: "No webhooks
  yet. Add one to hear about crashes, restarts and players in Discord, ntfy
  or your own service." followed by "Add a webhook".
- **Delete** opens a confirmation step (7.4) without a snapshot line:
  "Delete the Discord webhook to discord.com?", "This will: Stop sending
  messages to it. To add it again you need its address.", a danger button
  "Delete webhook", and "Cancel, keep the webhook".

**The add and edit form** (`/settings/notifications/new`,
`/settings/notifications/{id}`), a `form.panel`:

```html
<form method="post" action="/settings/notifications/new" class="panel" novalidate>
  <fieldset>
    <legend>Service</legend>
    <label class="check"><input type="radio" name="preset" value="discord" checked aria-describedby="preset-discord-hint"> Discord</label>
    <p class="hint" id="preset-discord-hint">In Discord: Server settings, Integrations, Webhooks, then Copy webhook URL.</p>
    <label class="check"><input type="radio" name="preset" value="ntfy" aria-describedby="preset-ntfy-hint"> ntfy</label>
    <p class="hint" id="preset-ntfy-hint">The topic's full address, such as https://ntfy.sh/your-topic.</p>
    <label class="check"><input type="radio" name="preset" value="generic" aria-describedby="preset-generic-hint"> Generic JSON</label>
    <p class="hint" id="preset-generic-hint">Foreman posts a JSON body to the address. The operator guide describes it.</p>
  </fieldset>
  <div class="field">
    <label for="webhook-url" class="required">Webhook address</label>
    <input id="webhook-url" name="url" type="url" required autocomplete="off" autocapitalize="off" spellcheck="false">
  </div>
  <fieldset>
    <legend>Send a message when</legend>
    <label class="check"><input type="checkbox" name="events" value="start" checked> The server starts</label>
    …
  </fieldset>
  <div class="field">
    <label for="webhook-lang">Language of the messages</label>
    <select id="webhook-lang" name="lang">…</select>
  </div>
  <div class="actions form-foot">
    <a href="/settings/notifications">Cancel</a>
    <button type="submit" class="button button--primary" id="act-webhook-save">Add webhook</button>
  </div>
</form>
```

- **Layout.** Each service is a framed option (a 1 px `line` edge on
  `surface`, its hint under its name); the chosen one turns `raised` with
  a `selected` edge and a 3 px `selected` bar on its start side. From a
  30rem form the four services are a 2-by-2 choice and the nine events
  sit in three columns, so no option is left alone on a last row. From
  80rem the services and the address (or keys) sit side by side. Cancel
  and Add webhook (or Save webhook) end the sticky foot bar (7.1, "One
  completion footer").

- **Service** defaults to Discord, the most common, so adding a Discord
  webhook takes no choice. The presets are Discord, ntfy, Pushover and
  Generic JSON. Each preset's hint says where its address comes from.
- **Pushover's keys.** The address is fixed, so choosing Pushover hides the
  address field and shows a "Pushover keys" fieldset (`#webhook-keys`) in its
  place: "Application token" and "User key", each a `type="password"` field
  with `autocomplete="off"`. A hint says where to find them at pushover.net.
  `notifyurl.js` swaps the two blocks on the radio change; without script
  both show. Both are write-only like the address: never in a value
  attribute, empty after a refused form (its error says "Type the key
  again."), and on the edit page each is optional, with a hint that the keys
  are kept and that an empty field keeps its key. A key error names the
  field and says to use letters and digits, at most 64 characters, without
  echoing it. The `hidden` attribute needs its own rule, because `.field`
  and `fieldset` are `display: grid`.
- **The address** is write-only, as the game password is (8.10). It is
  never put back in a value attribute: after a refused form the field is
  empty and its error asks for it again ("Type the address again. Foreman
  does not keep an address it refused."). On the edit page the field is
  optional, and a hint above it gives the state: "The address is kept:
  Discord, discord.com. Type a new address to change it." Address errors
  say what to fix without echoing it: "Enter an address that starts with
  https://." and the other URL checks, each in this form.
- **Events**, all but the last ticked by default, in this order, each a
  checkbox:
  "The server starts", "The server stops", "The server crashes", "The
  server crash-loops", "A player joins", "A player leaves", "A scheduled
  restart runs or is skipped", "Disk space runs low", and "An API token is
  about to expire", which starts unticked on a new webhook. At least one
  must be ticked ("Choose at least one event.").
- **Language** is a `select` of the eight languages, each named in its own
  language with its `lang`, defaulting to the language of the admin who
  adds it (SPEC 8.11). Hint: "Names from the game and the world's name are
  sent as written."
- The edit page's button is "Save webhook"; its `h1` is "Edit the Discord
  webhook to discord.com".
- After add, edit or delete, the list returns with a Done message
  (`#done-message`), "Added the Discord webhook to discord.com."
- There is no "send a test" button. A delivery that fails is
  logged (SPEC 8.12); the page does not show delivery history.

Strings: all in the `notify` area, including the event names and the
message bodies the webhooks carry; the menu and sub-navigation words in
`common`.

### 8.17 Low disk space (SPEC 8.13, Epic 9)

While the game runs and the disk that holds Foreman's data has less than
4 GB free (twice the 6.11 reserve), the Server page's status card shows a
Warning with the facts, where the game-network warning sits (7.3). It
clears only once more than 4.5 GB is free, so it does not flicker on and
off around one figure (SPEC 8.13). The thresholds are fixed.

```html
<div id="disk-notice" class="disk-notice" sse-swap="disk">
  <div class="message message--warning">
    <p><strong>Warning:</strong> Only 3.1 GB of disk space is free. Autosaves
    can fail below 2 GB, and snapshots stop below it. Delete snapshots or
    worlds you no longer need.</p>
    <div class="actions"><a href="/snapshots" class="button" id="act-disk-snapshots">Snapshots</a></div>
  </div>
</div>
```

- It is a region of its own, sent only when the warning appears, clears
  or its figure changes by 0.1 GB, because it holds a link and the facts
  region is replaced every few seconds. Empty, it takes no room.
- The next step is a control (17.9): "Snapshots" goes to the all-worlds
  Snapshots page, where the disk strip and "Delete selected" are.
- The figures come from the same reading as the disk strip, formatted
  with one decimal and the language's number rules (SPEC 22.4). The
  reserve figure is read from the code, not written into the string.
- It is not announced: it is a fact about the page, not a result. The
  webhook message for it is 8.16's "Disk space runs low".

Strings: `server.disk.low`, `server.disk.snapshots` in the `server` area.

### 8.18 Click budgets for Epic 9 (SPEC 17.10)

Counted from the Server page, defaults accepted, as SPEC 17.10 counts.
SPEC 17.10 holds these rows; each one has an end-to-end test that counts
its clicks.

| Task | Clicks |
|---|---|
| See why the server crashed | 0 |
| See that a scheduled restart is waiting, and when the next one is | 0 |
| Make an online player admin, or remove one (the player, Make admin) | 2 |
| Make a player admin, then undo it (the player, Make admin, Undo) | 3 |
| Add an online player to the whitelist (the player, Add to whitelist) | 2 |
| Kick a player (the player, Kick, Kick *player*) | 3 |
| Ban a player (the player, Ban, Ban *player*) | 3 |
| Unban a player while the game runs (Game settings, Ban list, Unban) | 3 |
| Edit the ban list or whitelist of a stopped world (Game settings, Ban list or Whitelist, Preview changes, Save settings) | 4 |
| Read the chat (Chat) | 1 |
| Send a chat message (Chat, Send) | 2 |
| Set the first restart schedule (Settings, Restart schedule, Save schedule) | 3 |
| Change the restart schedule while one is set (Next restart, Save schedule) | 2 |
| Open Notifications (Settings, Notifications) | 2 |
| Add a Discord webhook (Settings, Notifications, Add a webhook, Add webhook) | 4 |
| Act on the low-disk warning (Snapshots, tick a snapshot, Delete selected, Delete) | 4 |

### 8.19 New CSS for Epic 9

The rules the Epic 9 pages add to `foreman.css`.

- `.player-actions`: the `details` in a player cell. Summary is a full-cell
  target (`--target-min`), with the disclosure chevron (7.8) but no
  raised face or border, so the table still reads as a table; open, the
  `.actions` row sits under the name with `gap: var(--space-2)`.
  `.player-actions .badge` sits on the name's line and never wraps.
- `.chat-lines`: in the `log` box but in `--font-body` at `--text-md`, not
  mono; `li` a block with a hanging indent of `2ch`; `time` in
  `text-muted` with `font-variant-numeric: tabular-nums`; `.chat-name`
  weight 600 in `heading`; `.chat-line--server .chat-name` in `link`
  colour, so a server line reads as one, with the word "Server" carrying
  it. Block size `min(50vh, 24rem)`.
- `.schedule-next`: as `.loads-next` (the selector is added to that rule).
- `.disk-notice`: as `.mods-notice` (takes no room when empty).
- `.webhook-list`: the world list's row rules (8.2) without the active
  edge: rows separated by a 1 px `line`, `.webhook-list__title` in
  `heading` at `--text-md` with the host in `text-muted`.
- Two icons for 7.9: `--i-bell` (Lucide `bell`) for `empty--notify` and
  `--i-chat` (Lucide `message-square`) for `empty--chat`, added to the set,
  the licence's list and the empty-state rule.
- The Foreman settings sub-navigation reuses `.subnav`; no new rule.

### 8.20 The public mods page (SPEC 9.16)

**The page** (`GET /public/mods`, `publicmods.html`) is for players, who
are not signed in. It has its own shell (`public-layout`): the logo and
product name, the only link back into Foreman, and the user menu with
only the language, which posts to `POST /public/language`. No theme
choice, no nav, no About. Every file loads from `/public/static/`, so a
proxy rule for `/public/` is all it needs.

- `h1` "Mods for this server", then per running world a card with an
  `h2` of its name, the game version as a `.facts` pair, "Download all" as the primary
  button-faced link, and the mods as a plain list (`ul.public-mods`), one
  row each, never cards. A row is the mod's title and version as one
  download link, so each link's name holds both; marks follow as `.badge`
  words in a list: "Not on the mod portal", "Hidden on the mod portal",
  and, for a folder mod, which has no link, "Ask the server admin for this
  mod". A row is at least `--target-min` tall, divided by a 1 px `line`.
- No game running: one `p.empty` saying so, and nothing listed. The
  portal not checked: one `.hint` line above the list.
- "Where the mods go" is a card of its own: three numbered steps
  (choose Download all, unpack that one file into the client's mods
  folder, leave each mod's own zip packed), the second
  naming each system's folder (Windows, macOS, Linux) on its own line in a
  mono code box, written as the mod settings note writes them (8.11). From
  64rem it is a 30rem card beside the running world's card, so players see
  the list and where the files go at once; narrower, it follows the list.
  The page's frame is at most 80rem wide.

**The setting** (`GET /settings/public-mods`, `publicmodssettings.html`)
is a fourth tab, "Public mods", in the Foreman settings sub-navigation
(8.15), so from any page it is two clicks away and turning it on is three.
The state sentence leads, drawn as a switch plate (7.3, "A state plate"):
a box as wide as its sentence, up to 52rem, with a 4 px start edge, on
`ok-bg` with an `ok` edge while on, on `surface` with a `border` edge
while off. Then a `.setting-row` panel: what turning it on shares on the
left (half the card once the card is 60rem wide), the proxy rule with a
link to the operator guide on the right, and one button that flips the setting ("Turn on the public mods
page" or "Turn off the public mods page"). While on, "Open the public mods
page" sits beside it. A Done message confirms each change.

New CSS: `.public-world`, `.public-mods` and its `__name`, `__version`
and `__marks`, at the end of `foreman.css`.

### 8.21 API tokens (SPEC 12.10, 21.2, Epic 10a)

`/settings/api-tokens` is the fifth "Foreman settings" tab, after Public
mods. Its order follows what a person came to do:

1. **MCP access** comes first, under the page's one-line intro: one state
   plate (7.3, "A state plate") with the heading "MCP access" and its On
   or Off badge, the state sentence, and its one Turn on or Turn off
   button (secondary). Off, the plate has an amber (`warn`) edge and a
   `warn-bg` face; on, a green (`ok`) edge. The prerequisite is said once
   here, with its control, and nowhere else on the page. When
   `FOREMAN_MCP=off` forces it off, the sentence names the variable and
   no button shows; a user who is not an admin sees no button either.
2. **The tokens**, one list in one card. An admin sees every user's
   tokens with a "Your tokens" and "All tokens" choice above the list (a
   segmented pair of radios; the filter is CSS only, so nothing reloads),
   "Your tokens" chosen first; anyone else sees "Your tokens" as the
   heading and only their own. A token holds the name, a state badge
   (Valid, Expired, Revoked, reusing the running, stopping and crashed
   shapes so no state is told by colour alone), owner, scope, expiry,
   creation and last use to the minute. Renew is a one-click form post on
   the owner's own tokens only; Revoke (a red ghost with a cross, 7.1)
   links to a confirm page (7.4), because it is final. A revoked token
   has no buttons. On a narrow card each token is a card of its own, so
   seven facts wrap at 320 px; once the card is 52rem wide the same markup
   is laid out as table rows under one row of column names (`aria-hidden`,
   since each row keeps its own fact names for screen readers).
3. **Create a token**: name, then the scope as radio buttons with read
   checked, each with a hint. The warning that the two-step confirm
   guards mistakes, not a steered AI, shows only while Admin is chosen.
   There is no second MCP warning or button here. Creating is one click
   on this page.

The create answer is its own page: one panel at most 52rem wide, read top
to bottom. The Done line comes first and takes focus; then the secret, in
a read-only box that selects whole, with the warning that it is shown
once right above it and a Copy button joined to the box; then the
endpoint the same way; then scope and expiry as facts; then, when MCP
access is off, a short Note with a "Turn on MCP access" link for an admin;
then the way back to the list. It is sent with `no-store`.

The **expiry banner** sits at the top of `main` on every signed-in page,
a warning message with one line per token from 5 days before expiry and
while Expired. Each line names the token and the time, with a Renew
button that returns to the same page.

### 8.22 Factory map (SPEC 5.16)

`/worlds/{id}/map` is a world tab after Snapshots. In order: the summary
(the state, and "Drawn <time> from <save>, Factorio <version>"); the draw
form; the surface list (links with `aria-current`, shown when there is more
than one surface); the viewer; the layer checkboxes; one download link; then
the "When to draw" panel (radios Automatic, Manual and Off with hints, the
"Do not draw a running world while players are online" checkbox, and Save).

- **The viewer** is on the page, not in a dialog. Its frame has a fixed
  height (min(70svh, 48rem), at least 16rem), the `deep` face, and focus. It
  is named by the surface heading and described by the key line "Keys:
  arrows pan, + − zoom, 0 fit. Drag to pan, tap to center." Zoom out, Fit
  and Zoom in icon buttons sit in its bottom corner (24 px, 44 px under a
  coarse pointer; `aria-disabled` at the limits), with a status line under
  it. The zoom steps and limits are those of the Generate page's large view
  (8.8). A zoom from a key or a button is announced; a wheel or a pinch is
  not.
- **Layers** are a fieldset "Buildings to show on the map", one checkbox per
  category in the fixed order. Each has a 1rem swatch in the layer's
  colour with a black edge, which gives 3:1 or better on both themes. The
  text names the layer, so colour is never the only sign. A ticked layer is
  drawn over the base on the same grid, and the download includes the
  ticked layers.
- **Beside a running game** the draw button reads "Draw it beside the
  running game", under a note with the memory figure.
- **While drawing** the page polls every 3 s and replaces the region,
  keeping focus, zoom and ticked layers. The outcome is said once, through
  a visually hidden status outside the region.
- The page's own styles are in `static/factorymap.css`, linked from its
  scripts block, and use the tokens.

## 9. Writing

- Plain words, sentence case, no exclamation marks, no "Oops".
- Name things the way the game and the spec do: world, save, snapshot,
  mod, RCON, "InGame".
- Errors say what happened and what to do next, in that order.
- Never show a secret: no RCON password, token or game password appears
  in any message, field value or log view (SPEC 14.8).
- A refused single sign-on says so in its heading ("Single sign-on did not
  work") and offers the next step as a button: "Try single sign-on again"
  when trying again can help, "Back to sign in" when it cannot. It never
  repeats anything from the callback address (SPEC 12.7, 14.14).
- Numbers carry units ("2.1 GB", "60 seconds"), and times say whose zone
  when it matters (scheduled restarts).
- Times (SPEC 17.11): the server writes UTC in a `time` element with a
  `datetime` attribute, and `time.js` rewrites it with `Intl` into the
  viewer's zone, such as "Nov 3, 2:25 PM EST (2 days ago)". The year shows
  only when it is not the current year. The relative part is visible text
  only and is refreshed every minute. A time inside `.visually-hidden`
  text, such as a link's spoken name, or inside a button label (the Undo
  button, the Restore offer and links), gets the absolute local time and no
  relative part, so the name stays short and never goes stale. Without
  JavaScript the UTC text stays. A table cell keeps its date and clock in
  separate `nowrap` spans, and the zone is its own unit after the
  clock, in each language's order (8.3). Durations are not rewritten.
- Text and translations (SPEC 22): every visible word comes from the
  catalogue in eight languages. docs/I18N.md holds the glossary, the
  address rule per language and the rules for writing a string. Leave room
  for text about 40 percent longer than English, and put no text in CSS.

## 10. Checks every screen passes

The end-to-end tests for each screen check, in both themes
(`colorScheme: 'dark'` and `'light'` in Playwright):

- an axe scan with no violations (SPEC 17.1);
- at 320 px wide, no horizontal scroll (6, rule 1);
- every button and field at least 44 by 44 CSS pixels (a row's buttons
  may be 36 to 40 px tall from 64rem with a fine pointer, 7.1);
- the page can be used with the keyboard alone, with a visible focus ring;
- state badges contain their word.

## 11. CSS file layout

One stylesheet, `internal/web/static/foreman.css`, in this order: tokens
(both themes on `:root`, then the forced themes), a small reset, base elements
(`body`, headings, links, `code`, `table`), the shell (with its wide-screen
rail), then one block per component in the order of section 7 and section 8.
A component's narrow, wide-screen, forced-colours and reduced-motion rules sit
with the component. A rule that restyles another is folded into that rule,
never added as a later override, and nothing at the end of the file restyles
what came before. The shared groups that remain on purpose (the reset and base
type, the line icons, the single-column grids, and pairs of selectors that
share a look) may list a member that also has a rule of its own, where
splitting the group would copy its values or change which rule wins. No
preprocessor, no `@import`.

## 12. Deliberately deferred

Left out because no Epic 1 to 3 screen needs it. Add each when a real
screen does, in this document first.

- Icons beyond the set in 7.9, the map preview's pin, the large view's
  controls (8.8) and the toggletip's (i) (7.7). No icon font, and no icon
  that stands without a name.
- Modal dialogs other than the map preview's large view (8.8) and the
  Generate page's leave dialog (7.4), menus,
  tab panels and toasts. (The nav links
  only look like tabs; they are ordinary links.) The confirmation step
  (7.4) and the message (7.3) cover Epics 1 to 3.
- Charts for CPU and memory. Numbers in the status `dl` are enough for
  SPEC 7.2.
- Pagination. See 8.4.
- The world-switch progress screen beyond the step list in 7.6 (Epic 4).
- A print stylesheet.
