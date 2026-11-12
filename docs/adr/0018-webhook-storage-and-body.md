# 0018. Webhooks: sealed addresses, a versioned body, a queue for each target

- **Status**: Accepted
- **Date**: 2026-11-12
- **References**: SPEC.md sections 8.10 to 8.12, 14.8 and 14.10; ADR 0013

## Context

A webhook address often carries its token in the path, so it is a secret.
A hung target must never delay a stop, a restart or the monitor. Operators
script against the generic body, so it cannot change under them.

## Decision

- **Sealed addresses.** The address is sealed with ADR 0013's key and
  format, in the `url` column of the `webhooks` table, with `webhook-<id>`
  as the authenticated data. The row also holds the preset and host, which
  are all a page shows. Changing an address means typing it again. Without
  `FOREMAN_SECRET_KEY`, webhooks are unavailable.
- **Events turned off are stored**, so an event added later is on by default.
- **A versioned generic body**, `{"version":1,"event","time","world","text"}`
  plus optional fields. A new optional field keeps the version; any other
  change raises it. A golden file pins it. Event ids never change.
- **Emit never blocks.** `internal/notify` does not import `game`. The
  controller emits through a one-method interface into a queue of 256, then
  the notifier has a queue of 100, then each webhook has its own sender and
  queue of 20, so a dead target delays only itself. A full queue drops the
  event and logs it.
- **Delivery.** Each attempt takes at most 5 seconds and is retried at most
  twice (after 1 s and 5 s). A redirect is never followed, and a redirect or
  a 4xx answer other than 408 and 429 is not retried.
- **`http://` is allowed** (Todd, 2026-11-12), for a receiver on a trusted
  LAN. The form warns that messages go unencrypted.

## Consequences

A lost key loses every address, with no rotation. A dead target loses its own
events once its queue is full, and every undelivered event is lost when
Foreman stops. There is no test button and no signed body.
