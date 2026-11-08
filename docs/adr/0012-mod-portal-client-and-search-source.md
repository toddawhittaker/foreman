# 0012. Mod portal client: a fixed base, a checked download, local search

- **Status**: Accepted
- **Date**: 2026-11-07
- **References**: SPEC.md sections 9.1, 9.11, 9.12 and 14.10; STACK.md section 7

## Context

Search has to work without a factorio.com account (SPEC 9.11), but the
portal's own search call needs credentials. A download needs the account's
token, which rides in the address's query string, and the real portal
answers a download with a redirect to another host. A token in a URL can
leak through error messages, logs and the `Referer` header.

## Decision

- **Search runs locally.** Foreman fetches the portal's mod list for the
  world's game line once (`/api/mods?page_size=max&version=<line>`, no
  credentials) and searches it in memory: exact name, then prefix, then
  substring of name or title, by downloads, the first 25.
- **The base is fixed** at `https://mods.factorio.com`; there is no portal
  setting. Browser tests reach a fake portal through an HTTPS proxy with a
  test certificate authority (`HTTPS_PROXY`, `SSL_CERT_FILE`), so the
  product has no test hook either. Foreman follows only `download_url`
  values that start with `/download/`.
- **A download follows at most 2 redirects**, only to `https` hosts ending
  in `.factorio.com`, with no `Referer`. A real download redirected to
  `dl-mod.factorio.com`, which the rule allows. The SHA-1 is computed while
  streaming, and a mismatch is refused.
- **Errors are rebuilt without the request URL**, so the token never
  reaches a message or a log.
- **The cache is in memory**, 15 minutes, keeps found mods and 404s but
  never an error. Nothing is fetched at startup or on a timer.

## Consequences

Search costs one list download per game line per 15 minutes, up to 16 MB,
instead of one small call per search, and it finds only what the list
holds. If the portal moves downloads to a host outside `.factorio.com`,
they fail until the rule changes. Tests never call the real portal.
