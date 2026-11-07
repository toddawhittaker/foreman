# Foreman: design

This document sets the type, colour, spacing, focus style and components
that every Foreman screen uses (SPEC 17.6). It covers what the screens of
Epics 1 to 3 need: sign-in and setup, the world list, saves and
snapshots, server state, the log and the console, and confirmations for
risky actions. Section 12 lists what is deliberately left for later.

The CSS is hand-written, with no framework and no build step (STACK 3).
Nothing loads from outside the binary (SPEC 3.7), so there are no web
fonts, no icon fonts and no CDN.

When this document and a screen disagree, change the screen. When this
document is wrong, change it in the same pull request and say why.

## 1. Character

Foreman is a tool for keeping a game server alive, used in a hurry, often
from a phone. It should look like a well-labelled control panel, not a
marketing page, and it should feel at home next to the game it runs.

The look is a calm, flat control panel that sits well beside the game:
charcoal greys in dark, warm off-white in light, orange for "act here or
you are here", green for success and red for destroy. Foreman is not made
by, affiliated with or endorsed by Wube Software. It uses its own colour
values and none of the game's artwork, icons, logos, fonts or textures,
and nothing in it should suggest it is an official product.

- **Flat, with 1 px lines.** The page is a header bar, then one column.
  Sections are a heading and their content, separated by space, not
  boxes. A box appears only where it means something: a field, the log,
  a message, a confirmation. There are no shadows, bevels, gradients,
  glows or blurred glass. Corners are 4 px.
- **Colour that means something.** Neutrals are greys. Orange marks the
  one primary button on a screen, the selected tab, checked boxes and
  links: "act here" or "you are here". Green means only success: the
  "running" badge and the "Done" word. Red is the final step of a
  destructive action and the failed states. Yellow is a warning. None of
  them decorates.
- **Dark first.** Dark is the main look, as in the game. The light theme
  is the same layout in warm off-white, not an inverted afterthought.
- **The logo in the header.** The rusty Foreman logo sits beside the
  product name in the header, 32 CSS pixels square (6, Shell). There is
  no hero or illustration anywhere else.
- **Monospace for what the game prints**: versions, file names, world
  ids, seeds, the console and the log. Everything a person wrote, or
  Foreman wrote for a person, is in the system sans-serif.
- **Dense, not cramped.** Lists and tables, not grids of cards. A world
  list is a list. A save list is a table.
- **Words, not icons.** No emoji and no icon set. State, actions and
  messages are words. Where a shape helps scanning (the state badge) it
  is drawn in CSS and always sits beside its word.

## 2. Type

### Font stacks

```css
--font-sans: system-ui, -apple-system, "Segoe UI", Roboto, "Noto Sans",
  "Liberation Sans", Arial, sans-serif;
--font-mono: ui-monospace, "SF Mono", "Cascadia Mono", "Segoe UI Mono",
  "DejaVu Sans Mono", "Liberation Mono", Menlo, Consolas, monospace;
```

These are the fonts the visitor's system already has. Nothing is
downloaded.

### Scale

The root size is left at the browser default (normally 16 px) so that a
person's own text-size setting works. Every size is in `rem`. The steps
are a ratio of about 1.125, which keeps headings close to body text: on a
320 px screen, big headings waste lines.

| Token | Size | Line height | Weight | Use |
|---|---|---|---|---|
| `--text-sm` | 0.875rem (14 px) | 1.45 | 400 | Hints, table captions, timestamps, the log and console |
| `--text-md` | 1rem (16 px) | 1.5 | 400 | Body, form controls, buttons, table cells; `h3` at weight 600 |
| `--text-lg` | 1.125rem (18 px) | 1.3 | 600 | `h2`, section headings, the world name in a list item |
| `--text-xl` | 1.25rem (20 px) | 1.3 | 600 | Not used for headings; kept for the scale |
| `--text-2xl` | 1.5rem (24 px) | 1.25 | 650 | `h1`, one per page |

Rules:

- Nothing is smaller than 14 px. Form controls are 16 px so phones do not
  zoom in when a field is focused.
- Body text is limited to `max-inline-size: 70ch`. Tables and the log are
  not.
- Numbers that line up in columns (sizes, times, uptime, player counts)
  use `font-variant-numeric: tabular-nums`.
- Monospace text is set at `--text-sm`, because monospace faces look
  larger than sans at the same size.
- Long unbroken strings (world ids, file names, 60-character world names,
  log lines) use `overflow-wrap: anywhere` so they wrap instead of
  pushing the page wider than 320 px.
- Copy is sentence case. Buttons start with a verb ("Start server",
  "Delete world", "Take snapshot"), never "Submit" or "OK".
- Headings never skip a level. Each page has exactly one `h1`.

## 3. Colour

### How themes work

Both themes are defined as CSS custom properties. The dark values sit on
`:root`, because dark is the main look; the light values replace them
inside `@media (prefers-color-scheme: light)`. `:root` also sets
`color-scheme: dark light` so native controls (checkboxes, scroll bars,
the file picker) follow the theme. There is no theme switch in the
interface; the system setting decides (SPEC 17.3). Both themes must meet
every ratio below (SPEC 17.1).

Every colour in the CSS comes from a token below. A raw hex value
anywhere else in the stylesheet is a bug.

### Tokens

| Token | Dark | Light | Use |
|---|---|---|---|
| `--color-bg` | `#161514` | `#f5f3ef` | The page |
| `--color-surface` | `#1f1e1c` | `#ffffff` | The header bar, the user menu, the confirmation box |
| `--color-deep` | `#0f0e0d` | `#ffffff` | Form fields, the log and console, the "stopped" badge |
| `--color-line` | `#34312e` | `#dedad3` | Decorative dividers: under the header and the tabs, between rows; never the only edge of a control |
| `--color-text` | `#ebe7e0` | `#1c1a17` | Body text, field text, nav links |
| `--color-heading` | `#ebe7e0` | `#1c1a17` | Headings and the product name; the same as `text` |
| `--color-text-muted` | `#aaa297` | `#5b554d` | Hints, placeholders, timestamps, table captions and headers, `dt`, empty states, idle tabs |
| `--color-border` | `#857e75` | `#7f776d` | Edge of fields, secondary buttons and the menu |
| `--color-link` | `#ffbd6b` | `#9a4800` | Links (always underlined), quiet buttons, the "Active" badge |
| `--color-selected` | `#f39a2a` | `#b05a00` | The current tab's underline, the current item in the menu, checked boxes, progress bars |
| `--color-primary` | `#f39a2a` | `#f39a2a` | Primary button face |
| `--color-primary-hover` | `#f7b25a` | `#f6ad52` | Primary button hover |
| `--color-primary-edge` | `#f39a2a` | `#a35200` | Primary button edge (the face is too pale to carry it in light) |
| `--color-on-primary` | `#1a1004` | `#1a1004` | Primary button text |
| `--color-secondary-hover` | `#2b2927` | `#ebe7e1` | Secondary button and menu item face under the pointer |
| `--color-danger` | `#ea6248` | `#b3301d` | Danger button face, "crash-looping" badge background, the confirmation box's edge |
| `--color-danger-hover` | `#f2836f` | `#932616` | Danger button hover |
| `--color-on-danger` | `#1a0503` | `#ffffff` | Danger button text, "crash-looping" badge text |
| `--color-danger-text` | `#ff978a` | `#a32a18` | Error words, field error text, invalid field border, "crashed" badge, the required mark |
| `--color-danger-bg` | `#3a1a15` | `#f9e3de` | Error message and "crashed" badge background |
| `--color-ok` | `#8fd77c` | `#22662a` | "Done" word, "running" badge |
| `--color-ok-bg` | `#1c2d17` | `#e2efdc` | Success message and "running" badge background |
| `--color-warn` | `#f7c548` | `#7a5000` | "Warning" word, "starting" and "stopping" badges |
| `--color-warn-bg` | `#33290f` | `#f6ebcf` | Warning message and "starting"/"stopping" badge background |
| `--color-info` | `#a3c6ff` | `#174ea6` | "Note" word |
| `--color-info-bg` | `#18243a` | `#e3ebf8` | Notice background |
| `--color-focus` | `#8fb8ff` | `#0a56c8` | Focus outline |

How the colours divide the work:

- **Orange means "act here" or "you are here".** The one primary button
  on a screen (7.1), the current tab, the current language in the menu,
  a checked box. Links are orange too, and always underlined, so a link
  is never mistaken for the button. The "Active" badge is an orange
  outline, not a fill, so it does not compete with the primary button.
- **Green means success.** Only the "Done" word and the "running" badge,
  which say an action or the server succeeded. No button is green.
- **Red means destroy or failed.** The final button of a destructive
  action, errors, the crashed states and the required mark.
- **Yellow means warning**: the "Warning" word and the in-between server
  states. It is lighter and yellower than the orange in dark, and browner
  in light, and it always comes with its word.
- **The focus ring is blue**, the one hue none of the above uses, so it
  cannot be mistaken for selection or action (orange), success (green) or
  danger (red). It works on every surface in both themes.

### Contrast

Every ratio below was computed with the WCAG 2.2 relative-luminance
formula from the hex values above, not estimated. Text needs 4.5:1
(1.4.3, normal-size text, since nothing here relies on the large-text
exception). Control edges, focus outlines and the state badge's drawn
shape need 3:1 against what is next to them (1.4.11).

The script that produced them is short enough to rerun when a token
changes: compute each colour's relative luminance L, then
`(L_lighter + 0.05) / (L_darker + 0.05)`.

The lowest text ratio is 5.94:1 in dark (`on-danger` on `danger`) and
5.17:1 in light (`link` on `danger-bg`). The lowest edge or focus ratio is
4.16:1 in dark (`border` on `surface`) and 3.98:1 in light (`border` on
`bg`).

**Text pairings (need 4.5:1)**

| Foreground | Background | Dark | Light |
|---|---|---|---|
| `text` | `bg` | 14.80 | 15.67 |
| `text` | `surface` | 13.52 | 17.36 |
| `text` | `deep` | 15.65 | 17.36 |
| `text` | `secondary-hover` | 11.76 | 14.10 |
| `text-muted` | `bg` | 7.23 | 6.65 |
| `text-muted` | `surface` | 6.60 | 7.36 |
| `text-muted` | `deep` (placeholders) | 7.64 | 7.36 |
| `link` | `bg` | 11.07 | 5.74 |
| `link` | `surface` | 10.11 | 6.36 |
| `link` | `deep` | 11.70 | 6.36 |
| `on-primary` | `primary` | 8.45 | 8.45 |
| `on-primary` | `primary-hover` | 10.24 | 9.84 |
| `on-danger` | `danger` | 5.94 | 6.26 |
| `on-danger` | `danger-hover` | 7.73 | 8.30 |
| `danger-text` | `bg` | 8.72 | 6.53 |
| `danger-text` | `surface` | 7.97 | 7.24 |
| `danger-text` | `deep` | 9.23 | 7.24 |
| `danger-text` | `danger-bg` | 7.51 | 5.88 |
| `ok` | `bg` | 10.58 | 6.31 |
| `ok` | `ok-bg` | 8.47 | 5.87 |
| `warn` | `bg` | 11.32 | 6.37 |
| `warn` | `warn-bg` | 8.90 | 5.95 |
| `info` | `info-bg` | 8.93 | 6.54 |
| `text` | `danger-bg` | 12.73 | 14.11 |
| `text` | `ok-bg` | 11.86 | 14.56 |
| `text` | `warn-bg` | 11.63 | 14.64 |
| `text` | `info-bg` | 12.60 | 14.47 |
| `link` | `danger-bg` | 9.52 | 5.17 |
| `link` | `ok-bg` | 8.87 | 5.34 |
| `link` | `warn-bg` | 8.70 | 5.36 |
| `link` | `info-bg` | 9.42 | 5.30 |

**Control edges and focus (need 3:1)**

| Foreground | Background | Dark | Light |
|---|---|---|---|
| `border` (field, secondary button, menu edge) | `bg` | 4.55 | 3.98 |
| `border` | `surface` | 4.16 | 4.41 |
| `border` | `deep` | 4.81 | 4.41 |
| `primary-edge` (primary button) | `bg` | 8.22 | 5.03 |
| `primary-edge` | `surface` | 7.51 | 5.58 |
| `danger` (danger button, confirmation edge) | `bg` | 5.51 | 5.64 |
| `danger` | `surface` | 5.03 | 6.26 |
| `selected` (tab underline, menu mark) | `bg` | 8.22 | 4.40 |
| `selected` | `surface` | 7.51 | 4.87 |
| `link` ("Active" badge outline) | `bg` | 11.07 | 5.74 |
| `danger-text` (invalid field border) | `bg` | 8.72 | 6.53 |
| `danger-text` | `deep` | 9.23 | 7.24 |
| `focus` | `bg` | 9.09 | 5.96 |
| `focus` | `surface` | 8.30 | 6.61 |
| `focus` | `deep` | 9.61 | 6.61 |
| `focus` | `danger-bg` | 7.82 | 5.37 |
| `focus` | `ok-bg` | 7.28 | 5.54 |
| `focus` | `warn-bg` | 7.15 | 5.57 |
| `focus` | `info-bg` | 7.74 | 5.51 |
| `ok` (badge border and shape) | `ok-bg` | 8.47 | 5.87 |
| `warn` | `warn-bg` | 8.90 | 5.95 |
| `danger-text` | `danger-bg` | 7.51 | 5.88 |
| `info` (message border) | `info-bg` | 8.93 | 6.54 |
| `text-muted` ("stopped" badge) | `deep` | 7.64 | 7.36 |
| `on-danger` ("crash-looping" shape) | `danger` | 5.94 | 6.26 |

Not covered by a ratio, on purpose:

- `--color-line` and the steps between `bg`, `surface` and `deep`. They
  divide the page, not information, and no control relies on them: every
  control's edge is one of the rows above.
- The primary button's face against the page in light (`primary` on
  `bg`): its edge is `primary-edge`, which is the row above.
- A disabled control (exempt under 1.4.3; this design avoids disabled
  controls, see 7.1), and native checkboxes and radios, whose edges the
  browser draws for the `color-scheme` in use.

## 4. Spacing

A 4 px base, in `rem` so it scales with text size.

| Token | Value | Typical use |
|---|---|---|
| `--space-1` | 0.25rem (4 px) | Gap between a badge's shape and its word; the header's block padding |
| `--space-2` | 0.5rem (8 px) | Gap between a label, its hint and its field; table cell padding block |
| `--space-3` | 0.75rem (12 px) | Gap between buttons in a row; table cell padding inline; message padding; the page's side padding at phone width |
| `--space-4` | 1rem (16 px) | Gap between form fields; confirmation box padding; page padding from 40rem up |
| `--space-5` | 1.5rem (24 px) | Gap between sections inside `main`; the page's side padding from 40rem up |
| `--space-6` | 2rem (32 px) | Gap before a page's closing actions |

Other fixed values, also tokens:

| Token | Value | Use |
|---|---|---|
| `--radius` | 4px | Buttons, fields, the menu, messages, badges |
| `--border-width` | 1px | Every border except the ones below |
| `--focus-width` | 3px | Focus outline |
| `--target-min` | 2.75rem (44 px) | Minimum block size of buttons and fields |
| `--measure` | 70ch | Maximum line length of prose |
| `--page-max` | 64rem | Maximum width of `main` |

### Depth

There is none. Nothing has a shadow, a bevel or a gradient. Every field,
button, message, badge and box has a 1 px border, so its edge shows in
forced-colours mode as well (section 6, "Forced colours"). A group inside
a long form, such as a settings fieldset, has a 1 px `line` above it
instead of a box.

### Rules

- A component never sets its own outer margin. The parent spaces its
  children with `gap` (flex or grid). The one exception is prose flow
  inside a block of text, which uses the default paragraph margins reset
  to `margin-block: 0 var(--space-3)`.
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
  (`bg`, `surface`, `deep` or a message background), which is why the
  focus ratios in section 3 are measured against those, not against
  button faces.
- The ring is blue in both themes, so it stays distinct from the orange
  primary button and tabs, the green success word and the red danger
  button (3, "How the colours divide the work").
- A 3 px ring around a control at least 44 px tall meets WCAG 2.2 focus
  appearance (2.4.13, AAA) as well as focus visible (2.4.7, AA).
- Nothing sticky may cover the focused element (2.4.11). The header is
  not sticky. The open user menu covers the top of the page, but it closes
  on Escape and on a click outside, and focus stays inside it while open.
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
    <nav aria-label="Main">
      <ul class="tabs">
        <li><a href="/" aria-current="page">Server</a></li>
        <li><a href="/worlds">Worlds</a></li>
        <li><a href="/snapshots">Snapshots</a></li>
      </ul>
    </nav>
    <details class="menu">
      <summary><span class="menu__user">todd</span> <span class="menu__lang">English</span></summary>
      <div class="menu__panel">
        <p class="hint">Signed in as todd</p>
        <form method="post" action="/language">
          <fieldset>
            <legend>Language</legend>
            <ul>
              <li><button type="submit" name="lang" value="en" lang="en" class="menu__item" aria-current="true">English</button></li>
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
    <h1>…</h1>
    …
  </main>
  <div id="announcer" class="visually-hidden" aria-live="polite"></div>
</body>
```

- **The header** is a flat bar the full width of the window: background
  `surface`, a 1 px `line` under it. Its content lines up with the page
  column (`--page-max`). It is a flex row that wraps. From 40rem it is
  one row: the logo and name at the start, then the nav and the user menu
  at the end. Below 40rem it is two rows, about 96 px at 320 px: the logo
  and the menu, then the nav on a row of its own (`order: 2; flex-basis:
  100%`), its three tabs sharing the row out between them. The focus
  order is the source order (logo, tabs, menu) at every width.
- **The logo** is the rusty Foreman logo, 32 CSS pixels square, from
  `static/icons/logo-32.png` with `logo-64.png` for 2x screens (both made
  from `docs/brand/foreman-logo.webp`). It is decorative beside the word
  "Foreman", so its `alt` is empty and the link's name is "Foreman" in
  every language. Below 22rem (a 320 px phone) the word is visually
  hidden and only the icon shows; the link keeps its name.
- **Nav links are tabs**: no underline, padded to `--target-min` tall,
  `text-muted`. The current one is `text`, weight 650, with a 3 px
  `selected` underline drawn as a border, so it survives forced colours.
  Hover brightens the text to `text`; nothing else changes. The tab of
  the page itself has `aria-current="page"`; on a page inside its section
  (a world's page under Worlds, Console under Server) the tab has
  `aria-current="true"`, which looks the same but does not tell a screen
  reader it is on the list itself. There are three tabs: Server, Worlds
  and Snapshots. Console is a tab of the Server view (see "Sub-navigation").
- **The user menu** is a native `details` at the end of the header. Its
  summary shows the user's name and the current language ("todd
  English"), so a person who landed in a language they cannot read still
  finds it; on the sign-in and setup pages it shows only the language.
  The two words wrap between them, never inside one. Inside, on a
  `surface` panel with a 1 px `border`: "Signed in as todd"; the eight
  languages as eight submit buttons to the same `POST /language` (SPEC
  22.2), in a `fieldset` whose legend is "Language" (plus "(Language)" in
  English off English), each button marked with its `lang`, the current
  one with `aria-current="true"` and a 3 px `selected` mark at its start;
  then Sign out, still a `POST` form with a button, never a link, so a
  crawler or prefetch cannot sign anyone out. Each item is a 44 px
  target. It opens and closes with Enter or Space and works without
  script; `menu.js` also closes it on Escape (focus returns to the
  summary) and on a click outside. The panel is positioned under the
  header at the end edge and never wider than the window.
- The skip link is the first focusable element. It is visually hidden
  until focused, then appears at the top-left of the header.
- `main` holds one column: `max-inline-size: var(--page-max)`, centred,
  `padding: var(--space-4) var(--space-3)` at phone width and
  `var(--space-5)` from 40rem. Its children are a grid with
  `gap: var(--space-5)`, and it is the container (`container-type:
  inline-size`) that components size themselves from.
- **Sections are flat.** A `section` (class `panel` where it needs the
  grid) is a heading and its content, with `gap: var(--space-3)` inside.
  There is no box around it. Structure inside comes from headings, lists
  and lines.
- **Sub-navigation.** The tabs' look, under the page's `h1`, with a 1 px
  `line` under the row: the Server view has Overview and Console; a world
  has Overview, Saves, Snapshots and Settings. Each is a `nav` with its
  own name ("Server pages", "World pages") holding a list of links; the
  current page's link has `aria-current="page"`. On a container under
  30rem the tabs share the row out between them; in long languages they
  wrap.
- The `#announcer` region is where htmx swaps short results ("Snapshot
  taken", "Server started") so screen readers hear them. Errors are not
  announced from here; see 7.3.
- Viewport media queries are used only for the shell (header and `main`
  padding, the header's rows, the logo's word). Every component lays
  itself out from its container with `@container` queries.

### Forced colours

Windows high contrast (`forced-colors: active`) replaces every fill and
colour with the user's system colours. The current tab's underline and the
current language's mark are borders, and that mode draws them in
`Highlight`, so "selected" is still a drawn shape. The "Active" badge
keeps its border and its word, and buttons, fields and message boxes keep
their borders, so they need nothing more. The state badge's shapes (7.5)
are redrawn in `CanvasText` so their fills survive. Anything new that
marks a state with a fill alone needs a rule here. Check a screen with
Playwright's `forcedColors: "active"`.

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
   width when its row wraps, so a phone shows them stacked, primary first.
   A button in a form of its own (`.actions > form`) takes part in the row
   too: the form is `display: contents`, so "Take snapshot" stacks at the
   same width as the links beside it.
6. Tables follow 8.3. The log and console wrap their lines (8.5); they
   scroll vertically inside their own box, never horizontally.
7. Nothing is fixed or sticky to the viewport, so the on-screen keyboard
   never covers a field.

### What a phone shows first

The vision's success test runs from a phone, so the most common task on
each page is on its first screen, at 320 by 640 px, without scrolling.
The order is the source order at every width, so reading order, focus
order and the picture agree.

- **Server page.** The `h1` is the active world's name with its state
  badge (7.5), so what is running is the first thing read. Under it, when
  the operator set `FOREMAN_PUBLIC_ADDRESS` (SPEC 3.2), one line "Players
  join at host:port" with the address in `code` and a quiet "Copy address"
  button; the line is not there when the variable is blank, because
  Foreman never guesses the address. `copy.js` shows the button only where
  the Clipboard API works (HTTPS or localhost) and says "Address copied"
  beside it and through `#announcer`. Then the Overview and Console tabs,
  then the controls (and while an operation runs, its step list and
  "Cancel stop"), then the facts, then "Players online", then one row of
  quiet links: "World page", "Settings" and "Switch world". There is no
  list of other worlds: "Switch world" goes to the Worlds list, where every
  row has its Switch button, which makes a switch three clicks from the
  Server page (SPEC 17.10). After an operation, focus goes to the `h1`
  (`#control-heading`), which names the world and its new state.
- **Status facts** (7.5) are one wrapping line (`dl.facts--inline`, a
  flex row with `gap: var(--space-2) var(--space-5)`), so they sit two or
  three to a row at 320 px.
- **Force stop** is a plain link standing alone (rule 3), after the
  buttons in the control row, never a button face. It leads to its own
  confirmation (7.4), and drawn as a link it weighs less than Stop and
  Restart, so the drastic choice is not the one the eye lands on.
- **Console page.** The same `h1` and live badge as the Server page, then
  the Overview and Console tabs. The "Console" section, with the command
  field, comes before "Game log". The history box under the field is only as tall as
  what it holds (`block-size: auto`), up to `min(40vh, 20rem)`, so the
  newest reply sits close under the field. The game log keeps its fixed
  height (8.5), so new lines never move the page.
- **World list** (8.2). Generate and Import sit at the end of the `h1`'s
  row and wrap under it on a phone. "Deleted worlds" is a link under the
  list. Each row is a grid, `minmax(0, 1fr) auto`: the
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
```

| Kind | Face | Text | Border | Hover | Use |
|---|---|---|---|---|---|
| Primary (orange) | `primary` | `on-primary` | 1px `primary-edge` | face `primary-hover` | The one main action on a screen: "Start server", "Switch to this world", "Sign in", "Take snapshot" on the Snapshots page, "Send". At most one per screen, and none while the game runs on the Server page: Stop and Restart weigh the same. |
| Secondary (outlined) | transparent | `text` | 1px `border` | face `secondary-hover` | Every other action. The default. |
| Quiet (a link) | transparent | `link`, underlined | transparent | none | A lesser action that is still a button: "Copy address", "Delete" in a row, "Tick automatic snapshots". Still a 44 px target. |
| Danger (red) | `danger` | `on-danger` | 1px `danger` | face `danger-hover` | Only the final, confirming button of a destructive action (7.4). |

All buttons: `min-block-size: var(--target-min)`, `min-inline-size:
var(--target-min)`, `padding-inline: var(--space-4)` (`--space-2` for
quiet), `--text-md`, weight 600, `--radius`, `cursor: pointer`. While
pressed (`:active`) a button moves down 1 px with `transform`; under
`prefers-reduced-motion: reduce` nothing moves. A label wraps between
words inside the button (it never runs past the edge) when large text or a
long translation needs two lines; only a single word wider than the button
is broken (`overflow-wrap: break-word`, hyphenated where the browser can);
the block padding is `--space-2` so a two-line label is not cramped.

- Hover is a colour change only; it is never the sole sign of anything,
  because a phone has no hover.
- **Force stop** on the Server page is not a button at all: it is a plain
  link after the buttons (section 6, "What a phone shows first").
- A button that leads to a confirmation step, such as the first "Delete
  world" on a world's page, is **secondary**, not danger. Red is kept for
  the moment the thing actually happens, so it still means something.
- Use `<a class="button">` only for navigation that looks like an action
  ("Upload a save" leading to the upload page). Anything that changes
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
  does nothing.
- A row of buttons is a flex container with `gap: var(--space-3)` and
  `flex-wrap: wrap`. The primary button comes first in source and visual
  order; the cancel link comes last.

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
  `deep` in both themes (7.64:1 dark, 7.36:1 light).
- **Required fields** have the `required` attribute, which screen readers
  announce, and a red asterisk after the label: `<label class="required">`,
  whose `::after` is `content: " *" / ""`. The empty alt text keeps the
  asterisk out of the label's accessible name, so "required" is not said
  twice and the name is still the label's words. The asterisk is not colour
  alone: it is a shape. A required group puts the class on its `legend`. Each form with a required field shows the note
  once, before its first field (`{{template "required-note"}}`): an
  asterisk and the word "Required", translated. A field the server
  refuses when empty is marked; an end-to-end check fails when one is not.
- Text inputs, `select` and `textarea` are boxes: background `deep`, text
  `text`, 1px `border`, `--radius`, `min-block-size: var(--target-min)`,
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
- Hint: `--text-sm`, `text-muted`. Only present when it says something
  the label cannot.
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
  like a label. "Which save loads" (SPEC 7.11) is a radio group.
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

One component with four kinds. Each starts with a bold word, so the kind
never depends on colour.

```html
<div class="message message--error" role="alert">
  <p><strong>Error:</strong> The server did not answer over RCON within 5 minutes.
  It is still running. <a href="/logs">Read the last log lines</a>, then try Restart.</p>
</div>
```

| Kind | Word | Word colour | Background | Border | When |
|---|---|---|---|---|---|
| Error | "Error:" | `danger-text` | `danger-bg` | 1px `danger-text` | Something failed. Says what, and what to do next. |
| Warning | "Warning:" | `warn` | `warn-bg` | 1px `warn` | Something will go wrong unless acted on ("Snapshots use 41 GB and the disk has 3 GB free"). |
| Success | "Done:" | `ok` | `ok-bg` | 1px `ok` | An action finished and the result is not otherwise visible. |
| Note | "Note:" | `info` | `info-bg` | 1px `info` | Context that changes how a person reads the page ("Snapshots are using full copies, not hardlinks", SPEC 6.8). |

- Body text inside a message is `text`, not the kind colour; only the
  leading word is coloured. Links inside are `link` and underlined.
- Padding `var(--space-3)`, `--radius`, `max-inline-size: var(--measure)`.
  Messages are flat (no bevel) so they never look like a control. No icon, no left stripe, no close button: a message stays until the
  page changes.
- `role="alert"` only on an error that appears after an action without a
  page load. A message that is part of a freshly loaded page has no role.
  Success messages from htmx go to `#announcer` instead (section 6), with
  the visible message rendered beside the thing that changed.
- On the Server page, the unfinished-switch notice (SPEC 7.10) and the
  game-network warning (SPEC 4.4) are Warnings in the page, outside any
  live region. The notice sits in the control panel with its Switch back
  button after it; the network warning sits with the status facts.
- **Empty states** are plain paragraphs in `text-muted`, never an
  illustration: "No worlds yet. Import a folder of saves or upload a save
  file to make your first world." followed by the action buttons. The
  empty state always names the next step.
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

The page's one `h1` is the question, first in `main`, with no "Back to"
link above it and no second heading inside the box. The box holds only
"This will:", the list and the form. The world tabs (6, Shell) and
Cancel are the ways out.

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
      <button type="submit" class="button button--danger">Delete world</button>
      <a href="/worlds/family-base-2025">Cancel, keep the world</a>
    </div>
  </form>
</section>
```

Rules:

- The box is a section, but unlike other sections it is boxed: background
  `surface`, 1px `danger` border, `--radius`, padding `var(--space-4)`, a
  grid with `gap: var(--space-3)`. It carries no landmark name of its own,
  so it does not repeat the `h1`. It is the only
  place where danger colour fills an area larger than a button.
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
  plain link back, padded to 44 px tall, and its text says what cancelling
  keeps where that helps.
- **Typed confirmation** is only for deleting a world (SPEC 5.10). The
  server compares the typed name and, if it differs, re-renders the page
  with a field error ("Type Family base 2025 exactly, or cancel"). Other
  confirmations are one click, so typing does not become a habit.
- **Undo is named.** When the action can be undone from a snapshot, the
  confirmation says where: "To undo, restore that snapshot from the
  Snapshots page."
- After the action, the result page shows a Done message (7.3) naming
  the snapshot that was taken, linked.

Which actions need this: every operation SPEC 6.2 counts as risky, plus
delete (5.10), restore (6.5), forced stop (7.7), "start anyway" after a
failed pre-start check (7.4, which must name exactly what the game will
delete), switching worlds (7.9), and in later epics destructive player
actions (8.2). Start and restart anyway have no page of their
own: the Warning that reports the refusal on the Server page is the
confirmation (SPEC 7.4). It lists what the game drops, then the steps,
the snapshots and the undo as above, and its danger button posts the
action at once. Restart uses the default 60-second countdown there. A
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
| stopped | "Stopped" | `text-muted` | `deep` | Hollow square |
| starting | "Starting" | `warn` | `warn-bg` | Square, left half filled |
| running | "Running" | `ok` | `ok-bg` | Filled square |
| stopping | "Stopping" | `warn` | `warn-bg` | Square, right half filled |
| crashed | "Crashed" | `danger-text` | `danger-bg` | Filled diamond (the square turned 45 degrees) |
| crash-looping | "Crash-looping" | `on-danger` | `danger` | Filled square; the whole badge is solid |
| unknown | "Unknown" | `text-muted` | `deep` | Hollow circle; the badge edge is dashed |

"Unknown" is for when Foreman cannot ask Docker (SPEC 7.1). It uses the
same pair of colours as "Stopped" (8.10 and 7.63 on dark and light), so
no new contrast row is needed; the circle and the dashed edge tell the
two apart without colour.

Classes: `state` (the badge, also the "Stopped" look) plus one modifier,
`state--stopped`, `state--starting`, `state--running`, `state--stopping`,
`state--crashed`, `state--crash-looping` or `state--unknown`. The element
holds only the word. A template always writes the modifier, even for
"Stopped", so the class names a state.

- The word is the badge's content. It is real text, read by screen
  readers and kept in forced-colours mode.
- The shape is a 0.625rem square drawn with `::before`, `content: ""`,
  `border: 2px solid currentColor` and a background or
  `linear-gradient` hard stop at 50% for the half fills (a hard-stop fill,
  not a visual gradient). It is a second cue for people scanning a list,
  and it is decorative: the word alone carries the meaning.
- The fills are `background-image`, not `background-color`, because
  forced-colours mode (6) removes background colours. A rule there draws
  the shape in `CanvasText`, so "Running" stays a filled square and
  "Stopped" a hollow one. The solid crash-looping badge gets a 3px edge.
- `display: inline-flex`, `gap: var(--space-1)`, `--text-sm`, weight 600,
  padding `0.125rem var(--space-2)`, 1px border in `currentColor`,
  `--radius`. Sentence case, never all capitals.
- Crash-looping is filled solid because it is the one state that needs a
  person to act (SPEC 8.9: Foreman has stopped the game and will not
  restart it alone).
- On the Server and Console pages the badge sits in the `h1`, after the
  world's name, inside `<span id="server-title">`. The live "facts" event
  carries a copy of that span marked `hx-swap-oob="innerHTML"`, so the
  heading follows the game without a second event and without replacing
  the `h1`, which may have focus. The other status facts (SPEC 7.2) are a
  `dl` after the controls:

  ```html
  <dl class="facts">
    <div><dt>Uptime</dt><dd><time datetime="PT3H12M">3 h 12 min</time></dd></div>
    <div><dt>Active world</dt><dd>Family base 2025</dd></div>
    <div><dt>Game version</dt><dd><code>2.0.72</code></dd></div>
    …
  </dl>
  ```

  The `dl` is a wrapping flex row (`facts--inline`) on the Server page, and
  a grid, `repeat(auto-fit, minmax(min(100%, 7rem), 1fr))`, on a world's
  page; both are two or more to a row at 320 px. `dt` is `--text-sm`
  `text-muted`; `dd` is `--text-md` `text`.
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
and the step beside it in the second, at every width, so a step is one
row even at 320 px and an 11-step switch fits in about 300 px rather
than twice that. A long step name wraps inside its own column, under
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

When an operation changes the active world (a switch), the
old world's open page reloads onto the Server page after the operation
ends, moves focus to the `h1`, and writes the result sentence
to `#announcer`.

## 8. Patterns for later screens

These are not built in Epic 1. They are here so Epics 2 and 3 start from
the same rules.

### 8.1 Headings and sections

Each page is `h1`, then `section` elements each with an `h2`. A section
with actions puts its button row directly under its heading or at its
end, never floating in a corner.

### 8.2 The world list (SPEC 5.4)

A `ul`, one `li` per world, each holding an `h2` or `h3` with the world's
name (a link to the world's page), then a `dl.facts` with game version,
Space Age, last played and disk use. The active world carries the word
"Active" in a badge drawn as an outline in `link` colour (the same shape as
the state badge in 7.5 but with no drawn square) beside its name, followed by the game's state badge (7.5) when
Foreman controls the game, and is listed first. A size in the Disk use
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
reflows at any width, where a six-column table would not fit 320 px. The
list is the page's content, with no box around it, and its rows are
separated by a 1px `line` border, not boxed as cards.

### 8.3 Tables (saves, snapshots, players)

`table` with a `caption`, `th scope="col"`, and at most four columns,
one of which may be actions. The name column wraps with
`overflow-wrap: anywhere`; numbers are right-aligned and tabular. Header
row: `--text-sm`, weight 600, bottom border 1px `border`. Body rows:
bottom border 1px `line`. No zebra stripes. If a table needs more than
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
A snapshot table stacks each row (snapshot, then "Taken:", then "Size:")
when its panel is narrower than 14rem, which is 200% text on a 320 px
phone. Changing `display` can drop table semantics in some browsers, so
these tables carry explicit `table`, `rowgroup`, `row`, `columnheader` and
`cell` roles (SPEC 17.12).
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

The console section comes before the game log on the page (section 6).
Inside it, the command field comes above its history:

```html
<section aria-labelledby="console-heading">
  <h2 id="console-heading">Console</h2>
  <form class="field" method="post" action="…">
    <label for="command">Command</label>
    <div class="console-row">
      <input id="command" name="command" type="text" autocomplete="off" autocapitalize="off" spellcheck="false">
      <button type="submit" class="button button--primary">Send</button>
    </div>
    <p class="hint">…</p>
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
  newest" button. It is a grid: the box, then the button under it, at the
  end edge, so the button never covers a line (at 320 px an overlay hid the
  last lines). While `hidden`, the button is `visibility: hidden` and keeps
  its row, so showing or hiding it moves nothing below it, and it is out of
  the tab order and the accessibility tree. The script toggles the
  button's `hidden` attribute. The markup is unchanged: the button stays a
  sibling of `log`.
- `console-row`: the field and "Send" on one row. They sit side by side
  and wrap onto two lines when the panel is too narrow. The field's text is
  mono at 16px, so a phone does not zoom in on focus.
- `console-history`: the `ol` of past commands, newest last.
  `console-command` is the sent command, bold, with a drawn "> " prompt
  (ignored by screen readers) and a visually hidden "You sent" in front, so
  a command and its reply are told apart without colour. The reply is a
  `pre` in the same `li`.
- `button[hidden]` is hidden even though `.button` sets `display`.

- A deep box: background `deep`, 1px `border`, text `text`, `--font-mono` at `--text-sm`,
  `white-space: pre-wrap` on the `pre`, `overflow-wrap: anywhere`, so lines
  wrap and never scroll sideways. (The `ol` of the console history keeps
  normal white space, or the markup's line breaks would show as blank lines.)
- The game log's box has a fixed `block-size: min(60vh, 32rem)` and scrolls
  vertically. The console's history box is only as tall as what it holds,
  up to `min(40vh, 20rem)`, then scrolls (section 6). Both are focusable so
  keyboard users can scroll them.
- **The game's own Error and Warning lines** (a line whose message, after
  the game's seconds counter, starts with "Error" or "Warning") are drawn
  in `danger-text` and `warn`. The word is in the line itself, so the
  colour is never the only sign (SPEC 17.4). `console.go` marks the lines
  the page starts with, and `log.js` the same pattern for lines that
  arrive live.
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

The task is "make a new world". From the Server page it takes three
clicks: Worlds, "Generate a world", then "Generate world" on the form.
Everything but the name has a default.

One page, `GET /generate`: one `h1` with a one-line hint, then one form,
in this order:

1. The required note (7.2), then Name (required, a placeholder example).
2. Game version: the same picker as Import (SPEC 10.7). Typing a version
   not in the list sits in a closed `details`, "Use a version not in the
   list", opened when it holds a value or an error.
3. Space Age, and the "download the game image" checkbox.
4. "How to make the map": two radios, each with a one-line description
   under its words: **Map settings** (checked) and **Map exchange
   string**. CSS shows only the chosen way's fields
   (`form:has(#mode-exchange:checked) .mode-panel--settings` and the
   reverse); without `:has()` both show and the server reads the chosen
   one.
   - Map settings: the preset select with "[modified]" (the game's word)
     and a quiet "Reset to preset default" button after it; the seed with
     a "Random seed" button; then the tabs of 8.7.
   - Map exchange string: a mono `textarea`, as before.
5. The preview slot (8.8).
6. One achievements summary (below), then the action row: "Generate
   world" (primary), "Preview map", and a Cancel link.

**Preset, "[modified]" and reset.** Choosing a preset fills every tab with
its values (htmx redraws `#map-settings`; the preset and the seed are
outside it, so focus and a typed seed stay). "[modified]" and the reset
button show once any setting differs from the preset; the seed is not a
setting of the preset. Reset is a submit button to the refresh route, so
it also works without script; with htmx it redraws the tabs in place.
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

**Preview slot.** The preview sits full width under the settings, on
every width (8.8).

On success the browser lands on the new world's page with a Done message
(7.3): "Generated Family base 2026. Its map is made when it first
starts."

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
- **Per planet.** Resources and Terrain hold one closed `details` per
  planet (the first open, any with an error open); a world with one
  planet shows its groups directly.
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
  (phones, 200% text) it shows the select. The name and the value share
  the first line while they fit, and the value drops under the name
  (between words) when they do not; the name never breaks inside a word.
  The slider has a line to itself, so the value's width never moves it.
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
  minutes, ratios and modifiers in percent, evolution factors as "40"
  with a "× 10⁻⁷" unit after the field. A group's "Enabled" checkbox heads
  it on its own row. Numbers are mono text fields 12ch wide with the unit
  beside them. 2.1's evolution group size factor replaces 2.0's group
  sizes.
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
as the settings change), "look at another planet" (one choice), "look
closely" (one click on the picture) and "keep the pictures" (one click
on a download).

- **Where.** A "Map preview" `section` (h2) full width under the
  settings, on every width, above the achievements summary and the
  action row. Its first line: "Drawn from the current settings, which
  are not saved yet, with seed 123456. It redraws as you change them.
  "Generate world" makes exactly this map. The pin marks the centre of
  the map, where players start."
- **Size.** A "Preview size" setting of steps (DESIGN 8.7: a slider on a
  wide panel, a select of 25 sizes elsewhere) above the section, outside
  the part a reply replaces: 1024 to 4096 tiles square in steps of 128,
  1280 by default, and a hint that big sizes show their detail in the
  large view. A change redraws like any other, and the choice is kept
  over a form error.
- **Drawn by itself** (with script). On page load, and 0.8 s after the
  last change to anything in the form that changes the map: the preset,
  the seed, any setting, the way to make the map, the pasted exchange
  string, and the settings a version or Space Age change redraws (asked
  once that redraw has landed). The name, the tabs and the download box
  do not count. The newest request wins: a change aborts the request on
  its way (the server stops its run) and a late reply is ignored. A
  pressed "Preview map" is aborted by a later change too. Exchange mode
  with nothing pasted draws nothing and empties the section.
- **One planet at a time.** An automatic request (`auto=1`) draws only
  the planet being viewed: the first, or the one picked. A "Planet"
  select under the first line (script only, and only on a world with
  several planets) picks it and draws it at once; returning to a planet
  already drawn with the same settings shows the stored picture with no
  run. "Preview map" still draws every planet, which is also the page
  without script.
- **Seed.** A blank seed field is filled with the seed the preview used,
  so the world made is the one previewed; a seed the person typed is
  never overwritten.
- **Focus and news.** Nothing automatic moves focus; a control in the
  section that had focus (the planet select) keeps it across the redraw.
  While a request runs, the previous picture stays, faded to 60%, the
  section is `aria-busy`, and a one-line note above it says "Drawing the
  map preview for the current settings…"; that line's height is held once
  a preview shows, so nothing moves. When a preview arrives after a
  change, `#announcer` says "Map preview ready." once; the page load says
  nothing. Settings that are not valid get the problems in the section,
  each a link to its field, with no focus moved.
- **Pictures.** A `ul` (no bullets) of `figure`s, each with a
  `figcaption` naming the planet. The `img` has `width` and `height`,
  `inline-size: 100%`, `max-inline-size: 32rem` and `aspect-ratio: 1`, so
  its space is held. Alt text: "Map preview of Nauvis, seed 123456".
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
  `aria-hidden`; the section's first line says what it marks.
- **Downloads** (#237). Under each picture, "Download Nauvis (PNG)"; on
  a world with several planets, one "Download all drawn planets (zip)"
  after the list. Both are links with `download`, each a 44px target.
  Files are named after the world, the seed and the planet.
- **The large view** is a native `dialog` opened with `showModal()`, so
  the browser keeps focus inside it and Escape closes it. It fills the
  screen (`bg` face, no shadow): a bar with the planet's name (h2, the
  dialog's name), then Previous, Next, Zoom out, the zoom level ("Zoom
  200%", from `Intl.NumberFormat`), Zoom in, the planet's download and
  Close, wrapping as needed, and a one-line key hint; then the picture in
  a scrolling frame. Previous and Next step through the world's planets
  and wrap; a planet not drawn yet is drawn and shown in place. Zoom is
  1, 2, 4 and 8 times the fitted size, scaling the picture with
  `image-rendering: pixelated` (one tile is one pixel on 2.x), keeping the
  middle of the view. Keys: left and right arrows step (inside the
  zoomed frame they scroll it instead), plus and minus zoom, 0 fits.
  Content outside a modal dialog is inert, so the dialog has its own
  `role="status"` line for its news. On close, focus goes to the link of
  the picture last shown. A click with a modifier key opens the file as
  the browser would. Pinch zoom on phones is the browser's own.
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
"Settings" link. From the Server page, changing a setting takes three
clicks: Settings, "Preview changes", "Save settings", plus Restart when
the game is running.

The settings pages (form, raw editor and preview) carry the world's tabs
(6) with Settings current, and no "Back to" link. The form's `h1` is the
world's name. On a container at least 44rem wide a rail of section links
(Listing, Joining, Saving, Admins, and "Edit server-settings.json as
JSON") sits beside the form, 12rem wide and sticky, since it never covers
the form; narrower, there is no rail and the raw editor's link follows
the form. Server name is required (7.2).

**The form** (`GET /worlds/{id}/settings`) is one `form.panel` of
fieldsets, in this order:

- "Listing": name, description (`textarea`, 3 rows), and visibility as
  two radios, "On the local network" and "Hidden".
- "Joining": game password (below), maximum players (numeric, hint "0
  means no limit"), require user verification (checkbox).
- "Saving": autosave interval in minutes, autosave slots, pause when no
  one is online (checkbox).
- "Admins": a `textarea`, one player name per line, as its hint says. On
  a world that cannot be changed it is `readonly`, not disabled, so the
  keyboard can focus and scroll a long list; the other controls stay
  disabled.

Then "Preview changes" (primary) and Cancel. Below the form, one plain
link per editable file: "Edit server-settings.json as JSON".

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

**After saving**, the browser returns to the settings form with a Done
message naming the snapshot, linked. When a restart is needed the
message ends with a "Restart server" link to the restart form (7.4), so
the next step is a control.

## 9. Writing

- Plain words, sentence case, no exclamation marks, no "Oops".
- Name things the way the game and the spec do: world, save, snapshot,
  mod, RCON, "InGame".
- Errors say what happened and what to do next, in that order.
- Never show a secret: no RCON password, token or game password appears
  in any message, field value or log view (SPEC 14.8).
- Numbers carry units ("2.1 GB", "60 seconds"), and times say whose zone
  when it matters (scheduled restarts).
- Times (SPEC 17.11): the server writes UTC in a `time` element with a
  `datetime` attribute, and `time.js` rewrites it with `Intl` into the
  viewer's zone, such as "Nov 3, 2:25 PM EST (2 days ago)". The year shows
  only when it is not the current year. The relative part is visible text
  only and is refreshed every minute. A time inside `.visually-hidden`
  text, such as a link's spoken name, gets the absolute local time and no
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
- every button and field at least 44 by 44 CSS pixels;
- the page can be used with the keyboard alone, with a visible focus ring;
- state badges contain their word.

## 11. CSS file layout

One stylesheet, `internal/web/static/foreman.css`, in this order: tokens
(dark on `:root`, then the light media query), a small reset, base elements
(`body`, headings, links, `code`, `table`), the shell, then one block per
component in the order of section 7. No preprocessor, no `@import`.

## 12. Deliberately deferred

Left out because no Epic 1 to 3 screen needs it. Add each when a real
screen does, in this document first.

- A manual theme switch. The system setting decides (SPEC 17.3).
- Icons of any kind, except the map preview's pin (8.8).
- Modal dialogs other than the map preview's large view (8.8), pop-overs,
  menus, tab panels and toasts. (The nav links
  only look like tabs; they are ordinary links.) The confirmation step
  (7.4) and the message (7.3) cover Epics 1 to 3.
- Charts for CPU and memory. Numbers in the status `dl` are enough for
  SPEC 7.2.
- Pagination. See 8.4.
- Mod search results, dependency plans and the mod list (Epic 8).
- The world-switch progress screen beyond the step list in 7.6 (Epic 4).
- Chat (Epic 9).
- A print stylesheet.
