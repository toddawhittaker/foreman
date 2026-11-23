# 0035. Images come from Foreman only, with an opt-in for the Factorio wiki

- **Status**: Accepted; mod icons from zips were added by ADR 0037
- **Date**: 2026-11-23
- **References**: SPEC.md sections 13.1, 14.6 and 14.10; issue #882

## Context

Mod text can name game objects (`[item=space-science-pack]`) and a reader
expects to see the icon. Foreman has no copy of the game's art, and an
image address in a page makes the viewer's browser contact that host and
tells it which page asked. Foreman is self-hosted, often offline or behind
a private network, and its pages had no rule about where images may come
from.

## Decision

- Every HTML page sends `Content-Security-Policy: frame-ancestors 'none';
  img-src 'self' data:`, so a page can load images only from Foreman and
  `data:` URLs.
- An admin switch on the General tab, off by default, adds
  `https://wiki.factorio.com` to `img-src`, never on a public page. With it
  on, an object tag becomes an `<img>` for
  `https://wiki.factorio.com/images/<Name>.png` with
  `referrerpolicy="no-referrer"`, built only from a prototype name that
  matches `^[a-z0-9_-]+$`. A failed load shows the object's name.
- Foreman never fetches, stores or proxies an icon.

## Consequences

An offline or privacy-minded install makes no outside request, and a page
that tried to load an image from somewhere else would be blocked. With the
switch on, the wiki can see prototype names, modded ones included, and the
viewer's address, but not the page. Wiki file names follow the prototype
name only, so some objects fall back to their name. Rejected: Foreman
fetching and caching wiki images (an outbound request and a cache with no
need); shipping the game's art (a licence and size problem); mod icons from
the world's zips (a bigger change, filed apart); and no policy for images
(a page bug could then call any host).
