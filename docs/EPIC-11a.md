# Epic 11a: Sign-in, audit log, archive, settings and users

Plan, 2026-11-18. Epic branch: `epic/11a-features`. Base: `f5eba413` (`origin/main`, Epic
10b merged).

Serves SPEC 12.7 (OIDC), 12.9 (the audit viewer, filter and retention), 5.9 (archive),
13.1 (Foreman's settings page), 6.6, 7.6 and 14.6 (the values that become settings) and
14.2 (the operator guide). It also covers the edits this plan makes to 3.2, 5.9, 6.5, 6.6,
7.6, 12.7, 12.9, 12.12 (new), 13.1, 14.1, 14.6, 14.10, 18.11, 21.4, 21.7 and 21.8 (the
rulings below). Epic 11b, the release and Gate C, is outlined at the end.

Builds on Epic 10a and 10b (STATUS.md) and ADRs 0013, 0025 and 0026. Follow WORKFLOW and the
epic-run skill.

## Rulings (Todd, 2026-11-18)

Each ruling names the SPEC line the fold writes.

1. **Split.** Epic 11 becomes 11a (this plan) and 11b (the release and Gate C). The release
   workflow is built in 11a in a build-only mode and publishes nothing.
   SPEC 18.11: split into 18.11a and 18.11b, each saying what it delivers.
2. **OIDC group and claims.** When `oidc` is on, `FOREMAN_OIDC_REQUIRED_GROUP` is required,
   as for trusted headers: without it, anyone the identity provider (IdP) signs in would
   become an admin. The username comes from `preferred_username` and the groups from
   `groups`. These claim names are fixed, not settings. The scopes asked for are `openid
   profile email groups`.
   SPEC 12.7 and the 3.2 table: `FOREMAN_OIDC_ISSUER`, `FOREMAN_OIDC_CLIENT_ID`,
   `FOREMAN_OIDC_CLIENT_SECRET` (and `_FILE`), and `FOREMAN_OIDC_REQUIRED_GROUP`. `oidc`
   joins the `FOREMAN_AUTH_METHODS` list. `oidc` needs `FOREMAN_PUBLIC_URL`. A missing value
   is a startup error, reported with the others.
3. **Matching an OIDC user.** A user is found by the IdP's issuer and subject (`sub`), never
   by name alone. On first sight, Foreman links to an existing user of the same name only
   when that user has no password and no OIDC link: a user made by trusted headers. Any other
   name clash refuses sign-in with a page that says why. Otherwise the user is created.
   SPEC 12.7, and ADR 0027.
4. **Audit retention.** The audit log keeps 365 days by default. The setting takes 7 to 3650
   days. An hourly sweep deletes older entries. The sweep is not audited; a change to the
   setting is.
   SPEC 12.9 and 13.1.
5. **The IdP's address and calls.** The issuer must be `https`; `http` is a startup error.
   Foreman fetches the provider's discovery document the first time someone signs in, never
   at startup. It keeps the document once that works, and tries again on the next sign-in
   after a failure. Calls to the IdP take at most 10 seconds and follow no redirects. They
   read at most 64 KiB for discovery and 1 MiB for the signing keys or the token answer.
   SPEC 12.7, 14.10, and 14.1 (a new "Identity provider" zone, trusted only for ID tokens it
   signs for this client).
6. **Where the audit log lives.** It is a Settings tab, "Audit log", at `/settings/audit-log`.
   It filters by user, action and world, newest first, 50 entries a page, with an "Older"
   link.
   SPEC 12.9 and DESIGN 8.15.
7. **Archive and MCP.** `list_worlds` includes archived worlds, with `archived: true`. New
   admin tools `archive_world` and `unarchive_world` take `world_id`. Each is one call, since
   neither deletes or overwrites anything.
   SPEC 21.4 and 21.7: the fixed name list gains both.
8. **Lowering snapshot retention** deletes nothing when it is saved. Each world drops its
   extra automatic snapshots at its next automatic snapshot, as it does today. The General
   tab, and the MCP preview, say how many automatic snapshots across how many worlds that
   will remove.
   SPEC 6.6.
9. **Restoring an archived world** keeps the world's current archive state, whatever the
   snapshot's `world.json` says.
   SPEC 5.9 and 6.5.
10. **Settings through MCP.** A new admin tool, `set_foreman_settings`, changes any General-tab
    setting. Arguments left out keep their value. It is two-step (21.8): the first call
    returns a preview with each change from old to new value, plus ruling 8's count and the
    number of audit entries the next sweep removes. The confirm call is refused with a fresh
    preview if any value has changed since. `foreman_settings` reports every General-tab
    setting.
    SPEC 21.7 (the name list) and 21.8 (the list of two-step actions).
11. **Upload limits are settings.** These are on the General tab:
    - the save upload limit: default 1 GB, from 100 MB to 8 GB;
    - the mod upload limit: default 200 MB, from 10 MB to 2 GB.

    Both are given in whole MB, meaning MiB, as the pages already say them. The
    `mod-settings.dat` limit, the favorite file limit and the unpack limits stay fixed. A
    change applies to the next upload.
    SPEC 14.6 and 13.1. The mod limit also caps portal downloads (ruling 16).
12. **The General tab** (`/settings/general`) holds:
    - snapshot retention: 1 to 100, default 10;
    - the default stop countdown: 0 to 600 seconds, default 60;
    - audit retention (ruling 4);
    - the two upload limits (ruling 11).

    None of these has an environment form, so each says "set here". A scheduled restart keeps
    its fixed countdown (7.12). Changes are audited as `snapshot-retention-set`,
    `stop-countdown-set`, `audit-retention-set`, `save-upload-limit-set` and
    `mod-upload-limit-set`, with the old and new value.
    SPEC 13.1, 6.6, 7.6 ("an app setting, default 60 seconds") and 12.9.
13. **Users** (a new tab, `/settings/users`, new SPEC 12.12). See the section "Users: the
    edges" for the details. In short:
    - The tab lists every user with their sign-in kind (password, proxy or single sign-on)
      and the date they were created.
    - Any user may delete another user. Deleting yourself is refused.
    - Only a user with a password may change it: their own, after typing the current one.
    - Users made by trusted headers or OIDC have no password and see no password form.
    - New audit actions: `user-delete`, `password-change` and `user-create`.
    - Users stay out of MCP (21.7).
    - A deleted proxy or OIDC user comes back at their next request (ruling 17), and a
      forgotten password is reset from the host (ruling 18).
14. **Image platforms** (11b builds and publishes; 11a prepares it). The image is built for
    amd64 and arm64. The Dockerfile builds on `BUILDPLATFORM` for `TARGETOS` and `TARGETARCH`.
    The operator guide says the arm64 game image is experimental upstream and that real-game
    tests run on amd64 only.
    SPEC 3.1, and STACK 10.
15. **Tags and signing** (11b): `1.0.0-rc.1` first, Gate C on it, then `1.0.0` on the same
    image digest. Exact version tags only, with no `latest`. Each image carries a GitHub
    artifact attestation.
    SPEC 3.1, STACK 10 and 14, and ADR 0028. These are written by 11b's fold.
16. **One mod limit for uploads and portal downloads.** The mod upload limit (ruling 11) also
    caps a mod downloaded from the portal, so a mod that can be uploaded can also be
    downloaded again. SPEC 14.10: the fixed 200 MB becomes the mod upload limit.
17. **A deleted proxy or OIDC user** is created again at their next request while the proxy
    or IdP still vouches for them. There is no block list. The delete confirm page says to
    remove them at the proxy or the IdP to keep them out. SPEC 12.12.
18. **Forgotten password.** `foreman reset-password <username>`, run on the host with `docker
    exec`, reads the new password from standard input (at least 12 characters), ends that
    user's sessions, and is audited as `password-change` by `system`. Only users with a
    password can be reset. SPEC 12.12 and the operator guide.

## Design (architect, 2026-11-18)

### Hard-to-undo names, fixed by this plan

| What | Name |
|---|---|
| OIDC routes | `GET /signin/oidc` (start) and `GET /signin/oidc/callback`. The redirect URL is `FOREMAN_PUBLIC_URL` plus that path |
| Migration | `0011_oidc_subject.sql`: `users.oidc_issuer` and `users.oidc_subject`, both nullable, with a unique index on the two where they are set |
| `world.json` | `"archived": true`, left out when false. Added to `metaKnown` and `knownKeys`. `FormatVersion` stays 1 |
| `app_settings` names | `snapshot_retention`, `stop_countdown`, `audit_retention_days`, `save_upload_mb`, `mod_upload_mb`, stored as decimal text |
| Audit actions | `world-archive`, `world-unarchive`, `user-create` (detail: `setup`, `headers` or `oidc`), `user-delete` (detail: the name and how many tokens went with it), `password-change`, and the five `*-set` actions of ruling 12 |
| Pages | `/settings/general`, `/settings/users`, `/settings/users/{id}/delete`, `/settings/audit-log` |
| MCP tools | `archive_world`, `unarchive_world`, `set_foreman_settings` |

### OIDC (T1 and T5)

- **Library.** `github.com/coreos/go-oidc/v3` (STACK 7), which brings in
  `github.com/go-jose/go-jose/v4`. `golang.org/x/oauth2` moves from indirect to direct. Pin
  exact versions.
- **Where the code goes.**
  - New `internal/auth/oidc.go` holds the protocol:
    - building the sign-in address, with PKCE S256 (`oauth2.GenerateVerifier`), a nonce and
      a state;
    - the code exchange, and checking the ID token: issuer, audience, expiry, nonce, and the
      required group in `groups`;
    - the limited HTTP client (ruling 5), passed in with `oidc.ClientContext`.
  - New `internal/web/oidc.go` holds the two handlers.
- **Pending sign-ins** are held in memory, keyed by state, using the shape of `confirmStore`
  (`mcpconfirm.go`) as its own type. Each holds the nonce, the PKCE verifier and the `next`
  path, which `returnpath.go` checks. Each lasts 10 minutes and works once. At most 1000 are
  held, and a new one drops the oldest.
- **The binding cookie.** `foreman_oidc` holds the state: `HttpOnly`, `SameSite=Lax`, `Secure`
  as in 12.8, `Path=/signin/oidc`. It must match on the callback.
- **Failures.** Every failed callback counts against the client's address in the existing
  `auth.Limiter` (12.11). The log line names the address only (14.14), never a claim, a code
  or a token.
- **Success.** `SessionManager.Start` opens a normal session. A user seen for the first time
  is created with `CreateFirstAdmin` or `CreateUser`, copying `headerUser`, and recorded as
  `user-create`.
- **Fit with the other sign-in methods.**
  - In `whoIs` (`identity.go`), trusted headers still come first. Change the session gate
    from "local is on" to "local or oidc is on".
  - The sign-in routes are registered when local or oidc is on.
  - With only oidc on, the sign-in page shows just the "Sign in with single sign-on" button.
  - `redirectToSignIn` sends a visitor to the setup page only when local is on.
  - Signing out ends only the Foreman session.
- **The fake IdP.** New `cmd/fakeidp`, copying `cmd/fakeportal`. It serves discovery, the
  signing keys, an authorize step that approves at once, and a token endpoint over `https`
  with a test certificate. Its control routes set the next user's claims, so tests can send
  a bad nonce, a wrong audience or a missing group.

### Audit log viewer (T3)

- **Store.** `ListAudit(ctx, AuditFilter{Username, Action, WorldID, Before, Limit})` replaces
  `ListAudit(limit)`. The filters are exact matches, and `Before` is a time and id cursor
  served by the existing index. Also new: `DeleteAuditBefore`, and `AuditFilterValues`, the
  distinct users, actions and worlds that fill the filter menus.
- **Sweep.** `sweepOnce` in `sweep.go` also deletes entries older than the retention setting.
  It reads the setting through `s.store.AppSetting` with its own default, so T3 does not wait
  for T4.
- **What each entry shows.**
  - the time, in the viewer's time zone (17.11);
  - the user;
  - the token name, as a badge;
  - the action, in words from the `audit.action.<name>` keys, or as recorded when no key
    exists;
  - the world: its current name, or its id and "deleted";
  - the detail, as recorded and escaped.
- **Action keys.** The `audit` area holds a key for every action in `internal/game` and
  `internal/web` plus every name in this plan's table, so later tasks need no catalogue edits.

### Archive (T2)

- `world.Manager.SetArchived(ctx, id, on)` goes in new `internal/world/archive.go`, copying
  `rename.go`. `Summary.Archived` is added. Only `world.json` changes, so no snapshot is
  taken, as for rename.
- `restore.go` writes the world's current flag over the snapshot's (ruling 9).
- **Refusals.** Archiving the active world is refused inside the world lock.
  `Controller.Start` (`internal/game/actions.go`) and `Switch` (`switch.go`) refuse an
  archived world inside the same lock, with a new `StartError` kind whose words are in
  `gametext.go`.
- **One helper** for the UI and MCP: `s.archiveWorld(ctx, by, id, on)`, copying
  `setPublicModsBy`.
- **Where it shows.** The Worlds list shows archived worlds only in a closed "Archived (n)"
  foldout at its foot. Switch pickers leave them out. The world page shows an "Archived"
  badge and an Unarchive button.

### General tab and set_foreman_settings (T4)

- **Accessors.** `internal/web/appsettings.go` holds one typed get and set pair per setting,
  with its name, default, range and audit action, copying `mcpOn` and `setMCP`. It includes
  `s.stopCountdown(ctx)`, `s.saveUploadLimit(ctx)` and `s.modUploadLimit(ctx)`.
- **Snapshot retention.** The constant `world.KeepAutomatic` becomes
  `Manager.SetKeepAutomatic(n)`, an atomic value that `prune` reads. `NewHandler` sets it from
  the store, and saving the setting sets it again.
- **The trim count.** A world-package function counts the automatic snapshots that would go
  at retention n, for ruling 8.
- **MCP.** `set_foreman_settings` uses `mcpAsk` and `mcpSpend`. The "seen" state is the
  current values. It is listed with `DestructiveHint: true`.

### Settings reach the pages (T6)

- Every `defaultCountdown()` call site and every `game.StopCountdown` default outside the
  scheduler uses `s.stopCountdown(ctx)`. Remove the free function `defaultCountdown()`.
- **Upload limits.** Read the limit per request instead of per route:
  - `handleUpload` and `limitBody` (`middleware.go`) take a function where they took a
    number;
  - `MaxSaveUpload` and `MaxModUpload` become the defaults only;
  - drop zones, refusals and hints get the current figure from `sizeLimit`.

### Users: the edges (T7)

| Edge | Ruling |
|---|---|
| Deleting yourself | Refused. Another user must do it. This also means the last user can never be deleted, so nobody can lock everyone out through the UI |
| Deleting the last user who can sign in | Covered by the row above: the person deleting always remains. With headers or OIDC on, people in the group are created again anyway |
| A deleted user's sessions | Deleted with the user (an existing foreign key cascade). Their next request is signed out, and their live streams end at the next heartbeat (`stillSignedIn`) |
| A deleted user's API tokens | Deleted with the user (cascade, 12.10). An MCP request in progress is refused when its token is checked again after the body. Confirm codes for those tokens are useless. The confirm page says how many tokens will go |
| A proxy or OIDC user who is deleted | They are created again the next time the proxy or the IdP sends them, while they are in the required group. The confirm page says to remove them there to keep them out. No ban list (ruling 17) |
| The audit log | `user-delete` is written before the delete. The delete is refused if the entry cannot be written, as for `world-delete`. Old entries keep the name (12.9) |
| Changing a password | Your own, and only if you have one. It needs the current password, the new one twice, and at least 12 characters (12.4). Wrong current passwords count in the sign-in limiter (12.11). The hash is argon2id (`auth/password.go`). A change ends your other sessions and keeps this one. `password-change` is audited after the change, with no detail |
| Proxy and OIDC users | They have no password and get no form to set one. A password would let a proxy user get past the proxy. The tab says their sign-in is managed by the proxy or the IdP |
| Another user's password | No reset in the UI, as Todd asked. A forgotten password is reset from the host (ruling 18) |
| How the delete looks | A confirm page that names the user and their token count, with one danger button. No typed name: unlike a world, nothing on disk is lost |

## Tasks

No two tasks in the same wave edit the same file. No task edits SPEC, STATUS, DESIGN,
STACK, OPERATOR (except T8) or the ADRs. Each task puts its proposed STATUS and SPEC lines
in its PR body. Each task runs `make i18n-hash` only for the catalogue areas it owns. Agents
run fast checks only, in the foreground. The orchestrator runs full verify, e2e and
test-game.

| Task | Owner, model | Files (plus their `_test.go`) | Serves | Depends on |
|---|---|---|---|---|
| T0 Scaffold | builder, Sonnet high | `server.go` (`s.oidcRoutes()`, `s.generalRoutes()`, `s.usersRoutes()`, `s.auditRoutes()` and empty stubs in new `oidc.go`, `appsettings.go`, `users.go`, `auditlog.go`); `render.go` (path functions); `layout.html` settings tabs (General, Users, Audit log); `common.json` labels in all 8 languages | 13.1 | base |
| T1 OIDC backend | builder, Opus medium | `internal/config/*`; new `internal/auth/oidc.go`; `internal/web/oidc.go`, `identity.go`, `accounts.go`, the sign-in block of `server.go`; `internal/store/users.go`; migration 0011; new `cmd/fakeidp/`; `go.mod` and `go.sum` | 3.2, 12.2, 12.7, 14.10, 14.14 | T0 |
| T2 Archive | builder, Sonnet high | `internal/world/{meta,archive,list,restore}.go`; `internal/game/{actions,switch}.go` and the start error kinds; `internal/web/{worlds,gametext,mcptools,mcpadmin_worlds}.go`; `worlds.html`, `world.html`, the switch picker template; the `worlds` area; new `e2e/tests/archive.spec.ts` | 5.9, 6.5, 21.4, 21.7 | T0 |
| T3 Audit viewer | builder, Sonnet high | `internal/store/audit.go`; `internal/web/auditlog.go`, `sweep.go`; new `auditlog.html`; new `audit` area; new `e2e/tests/auditlog.spec.ts` | 12.9 | T0 |
| T4 General tab | builder, Sonnet high | `internal/web/appsettings.go`, `mcpadmin_foreman.go`; new `generalsettings.html`; `internal/world/snapshot.go`; new `general` area; `cmd/foreman/main.go` if wiring needs it; new `e2e/tests/general.spec.ts` | 13.1, 6.6, 21.7, 21.8 | T0 |
| R1 Release workflow (dry run) | builder, Sonnet high | new `.github/workflows/release.yml`; `Dockerfile` (multi-arch build, OCI labels, base image pinned by its index digest) | 3.1 | base |
| T5 OIDC sign-in page and e2e | ui-designer, Sonnet high | `signin.html`, `signedout.html`; the `accounts` area; `Makefile` (`build-fakeidp`, an e2e dependency); `e2e/tests/foreman.ts` (`startFakeIdP`); new `e2e/tests/oidc.spec.ts` | 12.7, 14.17 | T1 |
| T6 Settings reach the pages | builder, Sonnet high | `internal/web/{game,mods,modfiles,modsettings,update,snapshots,switch,serverpage,saves,import,worlds,upload,middleware,mcpactions,mcpadmin_versions,mcpadmin_snapshots}.go` and their templates' limit hints; `internal/modportal` download limit (ruling 16) | 7.6, 14.6, 14.10 | T2, T4 |
| T7 Users tab | builder, Opus low | `internal/web/users.go`; new `users.html`, `userdelete.html`; `internal/store/users.go`, `sessions.go`; new `users` area; new `e2e/tests/users.spec.ts` (no edits to `foreman.ts`); `cmd/foreman/main.go` (the `reset-password` command, ruling 18) | 12.12, 12.9 | T1 (and T4 for `main.go`) |
| T8 Operator guide | builder, Sonnet high | `docs/OPERATOR.md`: OIDC for any provider, naming none, with the hairpin note (Foreman must reach the issuer address browsers use); the `tcp://` warning; the General, Users and Audit log tabs; archive; API tokens and MCP; remove the "comes later" note | 14.2, 3.2 | T5, T6, T7 |
| T9 Gate and security tests | tester, Opus medium | new test files only: `oidc_security_test.go`, `archive_security_test.go`, `users_security_test.go`, `appsettings_mcp_test.go` | 14, 12, 21.8 | T5, T6, T7 |
| T10 Translation review (22.6) | builder, Sonnet high | the `accounts`, `worlds`, `audit`, `general`, `users` and `common` areas | 22.6 | T5, T6, T7 |
| Reviews | security-reviewer (12, 14, 21.8), a11y-reviewer, code-reviewer, ui-designer (review only); Opus, read-only | none | | each task as it lands |
| T11 Fold | builder, Sonnet high | `docs/STATUS.md`, `docs/SPEC.md` (every ruling's line), `docs/DESIGN.md` 8.15, `docs/STACK.md` 1 and 7, new `docs/adr/0027-oidc-users-by-subject.md`; deletes this plan | | all |

**Waves.**
1. T0.
2. T1, T2, T3, T4 and R1 in parallel, from T0's verified head. R1 can start from the base.
3. T5 (after T1), T6 (after T2 and T4) and T7 (after T1).
4. T8, T9 and T10.
5. T11.

**Why the order matters.**
- T6 waits for T2 and T4 because it shares `switch.go` and `worlds.go` with T2 and uses
  T4's accessors.
- T7 waits for T1 because both edit `store/users.go`, and T7 needs T1's sign-in kind columns.

### What every task tests

- Unit tests come from each ruling.
- Every change a user can see has an e2e test at 320 px and desktop width, in dark and light
  (`make e2e` projects).
- Every new audit action is checked by its row, with the token name when it comes through
  MCP.
- No secret appears in a page, a log or an MCP result (14.8).

### T9 checks the 11a gate in code

- **OIDC:** a replayed state, a missing or wrong binding cookie, a wrong nonce, a wrong
  audience, an expired token and a missing group are each refused. A foreign `next` goes
  home. An `http` issuer is a startup error. A name clash with a password user is refused.
  A header user is linked once. No claim, code or secret is in logs or pages.
- **Archive:** start and switch are refused through the UI, MCP and the scheduler. Restore
  keeps the flag.
- **Users:** deleting yourself is refused. Deleting another user ends their session, deletes
  their tokens (and `/mcp` then answers 401), and keeps their audit rows. `user-delete` is
  refused if its entry cannot be written. A password change needs the current password, ends
  the other sessions and is rate-limited.
- **`set_foreman_settings`:** the first call changes nothing, out-of-range values are refused,
  and a value that moved gives a fresh preview.
- **Upload limits:** an upload over a lowered limit is refused by the server.

### The 11a gate (orchestrator, on trial, before the epic PR)

- **A real IdP.** Sign in through a real OIDC provider. A user outside the group gets the 403
  page, and the existing header user is linked.
- **Archive.** Archive a world. Its start is refused in the UI and through MCP, and
  unarchiving it works.
- **Audit log.** The tab filters by user and by world.
- **Settings.** Lowering retention shows the count. The countdown default appears on the stop
  forms. An upload over a lowered limit is refused.
- **Users.** Delete a second user, and that user's token stops working.
- **Release.** The R1 dry run builds both architectures.

## Left out on purpose

- Signing out at the IdP, the userinfo endpoint, token refresh, and per-provider claim
  settings (ruling 2).
- A date-range filter and CSV export in the audit log.
- Adding local users, resetting another user's password, roles, and a ban list (rulings 13
  and 17).
- Users in MCP (21.7).
- Several active worlds: unchanged, and archive refuses only the active world.
- GoReleaser, binaries and SBOM (software bill of materials) publishing.

## Epic 11b outline: the release and Gate C

1. **R2 Release.** Turn on publishing in `release.yml`:
   - on a `v*` tag whose commit is on `main`;
   - build amd64 and arm64;
   - push `ghcr.io/toddawhittaker/foreman:<X.Y.Z>`;
   - attest it with `actions/attest-build-provenance`;
   - permissions on that job only: `packages`, `id-token` and `attestations`.

   Todd sets up a ruleset so only he can create `v*` tags, and makes the package public after
   the first push.
2. **R3 Gate C** (SPEC 19.3).
   - security-reviewer over the whole of section 14;
   - the architect's whole-system review;
   - a real-game check that a bad mod update is undone in one click;
   - idle memory measured against 16.2;
   - a P0 and P1 sweep of the spec against STATUS;
   - a live VPN-sidecar test, or Todd's ruling on 4.13;
   - fixes as tasks.
3. **R4 Publish** `1.0.0-rc.1`. Follow the operator guide from `docker pull` on a clean host
   (the Gate B path with the GHCR image). Check the attestation with `gh attestation verify`.
   Then tag `1.0.0` on the same digest.
4. **R5 Guide and fold.**
   - The guide gets the GHCR image pinned by digest, an "Upgrading" section (3.8), and the
     arm64 caveat.
   - SPEC 3.1 and 18.11b.
   - STACK 10 and 14: GoReleaser rejected, the attestation chosen.
   - ADR 0028: image tags, version and signing.
   - STATUS.
