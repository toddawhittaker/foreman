# 0026. MCP confirm codes live in memory and are bound to the token, the tool, the arguments and the state the preview saw

- **Status**: Accepted
- **Date**: 2026-11-17
- **References**: SPEC.md sections 14.19, 21.8 and 18.10b; ADR 0025

## Context

An admin token can delete a world, restore a snapshot or change mods.
The web UI asks a person to confirm these on a page. An assistant has no
page, so SPEC 21.8 makes each such tool work in two calls. The first call
changes nothing and returns a preview and a confirm code. The second
repeats the call with the code. The code must prove that this token saw
this preview of this exact call, and that nothing the preview showed has
since changed.

## Decision

Codes are kept in memory, in `internal/web/mcpconfirm.go`. A code is 16
random bytes in base64url (22 characters). It lasts 5 minutes. An entry
holds the token's id, the tool's name, the SHA-256 of the call's arguments
in canonical JSON (keys sorted, `confirm_code` left out), the SHA-256 of
the "seen" state, and the expiry. A token holds at most 10 codes, and an
eleventh drops the oldest.

A code is spent by any use. A wrong token, tool or arguments, an expired
code, and a code whose call is later refused for another reason all spend
it. A bad, used, expired or mismatched code gets one fixed message, so a
caller learns nothing about which part was wrong. Any other refusal, such as
a busy world or a failed page check, keeps its own message, and the code is
still spent.

The "seen" state is what the preview showed that could move: the mod
plan's hash and folder fingerprint, the settings files' hashes, the mod
setting flags, a webhook's fields, whether the server is running. The
confirm call spends the code first, runs every check again, builds the
seen state afresh and compares its hash. If it differs, the call is
refused and returns a fresh preview and code.

Each two-step tool calls two small helpers. `s.mcpAsk` issues a code, and
`s.mcpSpend` spends one and returns the stored seen hash. The tool builds
the same page struct the UI's confirm page uses, so the preview's
sentences and the snapshot label come from the same code as the page.

## Consequences

- A leaked or guessed code is useless to another token, tool or call, works
  once, and dies in 5 minutes. A Foreman restart voids every code, and the
  client asks again.
- Only hashes are kept, so a console command, a settings file or a password
  never sits in the store.
- The confirm is a guard against mistakes, not against a steered client: an
  assistant that is tricked into asking can also confirm. SPEC 14.19 accepts
  this risk. The scope is the real limit.
- Spending first means a refused confirm costs the client one more preview.
  That is cheap, and it stops a caller from probing one code with many
  argument guesses.
- Rejected: a preview hash the client echoes back. The client could forge
  or replay it, and the server would have to trust it. A server-held code
  needs no trust.
- Rejected: one generic wrapper around every two-step tool. The tools'
  checks differ in order and in what they compare, and a wrapper hid that
  behind options. Two helpers and a plain function per tool stay readable.
- Rejected: storing the raw seen state or arguments to compare later. It
  would put console commands and settings files, with their passwords, in
  memory for 5 minutes for no gain over a hash.
- Rejected: codes in the database. A code is worthless after a restart and
  would add a table and a clean-up job.
