# Epic 10b: MCP admin tools and the two-step confirm

Plan, 2026-11-17. Epic branch: `epic/10b-admin-tools`. Base: `3c331433`
(`origin/main`, Epic 10a merged). Serves SPEC 21.7, 21.8, 14.19 (confirm
codes), 18.10b, with the edits this plan makes to 12.9 (`world-delete`),
21.5 (`cancel_stop`) and 21.8 (the rulings below). Builds on Epic 10a
(STATUS.md, Epic 10a) and ADR 0025. Follow WORKFLOW and the epic-run
skill.

## Rulings (Todd, 2026-11-17)

1. A tool is two-step exactly when SPEC 21.8 names the action or the UI
   shows a confirm page for it. Every other admin tool is one call.
2. The confirm call is refused, with a fresh preview and code, when the
   state the preview showed has changed (the mod plan's hash and folder
   fingerprint, the settings files' hashes, the mod settings flags). That
   state is kept with the code.
3. The code stands for a confirm page's inner confirmations (typing the
   world name, the drops tick, the "go on without" tick). The preview
   lists the drops and the "without" mods explicitly.
4. One tool per action (names below). Tool names are hard to change
   later, so they are fixed by this plan.
5. `change_game_settings` takes the settings form's fields only, never
   the raw JSON editor.
6. A snapshot's `taken_by` stays the owner's username; the audit log
   carries the token name. No change to `snapshot.json`.
7. Deleting a world is audited as `world-delete`, before the delete,
   which is refused if the record cannot be written. UI and MCP alike.
8. Add `cancel_stop` as an operate tool now (SPEC 21.5).

## Design (architect, 2026-11-17)

- All code in `internal/web/`, one file per SPEC 21.7 area, following
  `mcptools.go` and `mcpactions.go`. Only `web/mcp*.go` imports the SDK.
- `mcp.go`: the admin scope's server also calls `s.addAdminTools(srv)`
  (in `mcpadmin.go`), which calls each area's adder.
- **Confirm-code store** (`mcpconfirm.go`), modelled on `pending.go` but
  its own type. An entry holds the token id, the tool name, the SHA-256 of
  the arguments in canonical JSON (`confirm_code` removed, keys sorted),
  an optional "seen" string (ruling 2), and its expiry. Only the hash of
  the arguments is kept, so no console command or password sits in
  memory. A code is 16 random bytes in base64url (22 characters), lasts 5
  minutes, is spent by any use, right or wrong; a token holds at most 10
  and an eleventh drops the oldest. Every refusal gives one fixed message.
- **Two helpers, no generic wrapper:** `s.mcpAsk(req, tool, in, seen)`
  issues a code and `s.mcpSpend(req, tool, in) (seen, err)` spends one.
  Each two-step tool: build the same page struct the UI's confirm page
  uses; without a code, return the preview and code; with a code, spend
  it first, run every check again, refuse with a fresh preview if the
  seen state moved, then act through `mcpRun` or the lifted function.
- The shared preview shape: `{done:false, will[], snapshot,
  untrusted_online[], confirm_code, expires_at}` plus the tool's own
  fields. `will` holds the confirm page's sentences in English (through
  `inEnglish`); `snapshot` is the label the UI would take, or empty.
- Two-step tools carry `DestructiveHint: true`; every other admin tool
  sets it to `false` explicitly (MCP reads a missing hint as true). The
  gate test finds the two-step tools by this hint.
- No new message-catalogue keys and no template edits. Words used only by
  MCP are English literals, as in `mcpactions.go`.
- Tools that need Docker control register only when `s.game != nil`.
  Work that can outlast 50 seconds runs through `mcpRun` or T1's detached
  helper (clone, delete with a final snapshot, bulk snapshot delete).
- **Preview parity test:** each area's test renders the UI confirm
  template in English from the same page struct and checks every `will`
  sentence appears in it.

### Lifts (handler logic moved into request-free functions)

Each lift has one owner task; that is what keeps tasks off each other's
files.

| Area | Handler today | Lifted function | Task |
|---|---|---|---|
| Generate | `readGenerate(r, kind)`, tail of `handleGenerate` | `readGenerate(ctx, url.Values, kind)`, `s.generateWorld(ctx, by, f, nw)` | T2 |
| Clone | `handleClone` | `s.cloneWorld(ctx, by, sum, name, choice)` | T2 |
| Delete world | `handleDelete` | `s.deleteWorld(ctx, by, sum, final)` (writes `world-delete`) | T2 |
| Restore | `restoreTarget(w, r, u)` | `s.restorePage(ctx, id, snap)` | T3 |
| Snapshot delete | `snapshotDeleteTarget`, `bulkPage` | `s.bulkPlan(ctx, refs)` | T3 |
| Kick, ban | page built in `handlePlayerConfirm` | `s.playerConfirmPage(ctx, sum, action, name)` | T3 |
| Mods | tail of `handlePlan` | `p.confirmProblems(f)`, `s.planOp(ctx, p, f)` | T4 |
| Mod settings | `modSettingsPage(r, sum)`, `readField`, tail of `handleModSettingsSave` | `modSettingsPage(ctx, lang, sum)`, `parseField(f, raw)`, `modSettingsOp(...)` | T4 |
| Game settings | `h.load(w, r, u)`, body of `handlePreview` | `h.loadWorld(ctx, sum)`, `h.formChange(ctx, sw, form)` | T5 |
| Whitelist switch | inline in `handleWhitelistSwitch` | `s.setWhitelistSwitch(ctx, by, id, on)` | T5 |
| Rollback | `rollbackPage(w, r, u)` | `s.rollbackPage(ctx, sum)` | T5 |
| Schedule | `handleScheduleSave` | `s.saveSchedule(ctx, by, in)` | T6 |
| Webhooks | add, change, delete handlers | `s.addWebhook`, `s.changeWebhook`, `s.deleteWebhook` `(ctx, by, ...)` | T6 |
| Account | `accountPage(r, ret)`, set and remove bodies | `accountState(ctx)`, `s.setAccount(ctx, by, ...)`, `s.removeAccountBy(ctx, by)` | T6 |
| Public mods | `setPublicMods` plus audit | `s.setPublicModsBy(ctx, by, v)` | T6 |

Already request-free, reuse as they are: `switchPage`, `planPage`,
`updatePage`, `versionPicker`, `releaseNotice`, `buildSavesPage`,
`findMods`, `clonePage`, `checkSetup`, `previewPage`, `buildFiles`,
`checkCommand`, `auditText`, `hidesReply`, `game.CheckPlayerAction`,
`s.online`, `worldBusy`, `deleteRefusal`, `modUndo`, `stopOptions`,
`mcpRun`, `mcpNamed`, `mcpFail`, `inEnglish`, and the domain calls
(`worlds.Rename`, `SetLoadSave`, `Deleted`, `favorites.*`, the
controller's `ForceStop`, `PullImage`, `Player`, `Console`, `Restore`,
`ApplyMods`, `Update`, `Rollback`).

## Tools (all admin scope except `cancel_stop`)

Two-step tools are marked **2**.

| Tool | Arguments | Notes |
|---|---|---|
| `cancel_stop` (operate) | world_id | as the Server page's Cancel stop (7.6); `stop-cancel` audit |
| `generate_world` | name, game_version?, space_age?, preset?, seed?, map_exchange_string?, favorite_id?, download_image? | result names a favorite's mods for a later `change_mods` |
| `clone_world` | world_id, name, map (keep or fresh) | |
| `rename_world` | world_id, name | |
| `delete_world` **2** | world_id, final_snapshot? (default true) | `delete.html`; refused when active or busy |
| `list_deleted_worlds` | none | to restore a deleted world |
| `list_favorites`, `make_favorite`, `rename_favorite` | favorite_id or world_id, name | |
| `delete_favorite` **2** | favorite_id | `favoritedelete.html` |
| `list_saves`, `choose_save` | world_id, save ("" = latest) | |
| `restore_snapshot` **2** | world_id (deleted world allowed), snapshot, countdown_seconds? | `restore.html`; mod undo steps in `untrusted_mod_steps` |
| `delete_snapshots` **2** | world_id, snapshots[] | `snapshotbulkdelete.html` |
| `force_stop` **2** | world_id | `forcestop.html` |
| `download_game_image` | world_id | `mcpRun(holdsLock=false)` |
| `kick_player`, `ban_player` **2** | world_id, name, reason? | `playeraction.html`; reply in `untrusted_reply` |
| `player_action` | world_id, action (unban, promote, demote, whitelist_add, whitelist_remove), name | the UI acts at once |
| `run_console_command` **2** | world_id, command | preview shows `auditText(command)`; reply masked by `hidesReply`, in `untrusted_reply` |
| `search_mods` | world_id, query | portal text untrusted |
| `change_mods` **2** | world_id, kind (install, update, enable, disable, remove, sync), names[], optional[], declined[], kept[], versions{}, also_disable[], also_remove[], choices{name: nearest or without}, countdown_seconds? | `modplan.html`; seen = plan `Hash` + `Fingerprint` |
| `list_mod_settings` | world_id | bounded wait on `ModSettingDefs` |
| `change_mod_settings` **2** | world_id, values{name: text}, countdown_seconds? | `modsettings.html`; seen = `Fingerprint`, `Seen`, `Running`, `NewMap` |
| `get_game_settings` | world_id | `has_password`, never the password; names untrusted |
| `change_game_settings` **2** | world_id, settings{form field names}, game_password? (write-only), remove_password?, admins[]?, bans[]?, whitelist[]? | `settingspreview.html`; seen = file hashes |
| `set_whitelist_switch` | world_id, on | |
| `list_game_versions` | world_id? | |
| `update_game_version` **2** | world_id, version, countdown_seconds? | `update.html` |
| `rollback_game_version` **2** | world_id, countdown_seconds? | `rollback.html` |
| `foreman_settings` | none | schedule, webhooks (no address), account source (never the username), public mods |
| `set_restart_schedule` | enabled, time, zone, limit_hours | |
| `add_webhook`, `change_webhook` | (webhook_id), preset, url (write-only, kept when left out), events[], lang | |
| `delete_webhook` **2** | webhook_id | `notificationdelete.html` |
| `set_factorio_account` | username, token | nothing echoed |
| `remove_factorio_account` | none | |
| `set_public_mods` | on | |

`stop.html` and `switch.html` stay one-step (operate tools, 21.5). Not
offered: token revoke, "start anyway", file replace (all excluded by
21.7 or Epic 10a).

## Tasks

No two tasks edit the same file. Wave-2 tasks never edit `mcp.go`,
`mcpconfirm.go`, `mcpactions.go`, `mcptools.go`, `game.go`, `server.go`,
any template or any message catalogue.

| Task | Owner, model | Files (plus their `_test.go`) | Depends on |
|---|---|---|---|
| T1 Foundation | builder, Opus low | new `mcpconfirm.go`, `mcpadmin.go`, `audit.go`, the seven empty area files below; edits `mcp.go`, `mcpactions.go` (`mcpRun` lock parameter, `cancel_stop`), `server.go` (settings handlers reachable), `mcp_test.go` (scope listing), and the actor change: `s.audit(ctx, by game.Actor, ...)` moved from `generate.go:436` to `audit.go`, its callers in `account.go`, `apitokens.go`, `clone.go`, `generate.go`, `notify.go`, `publicmodsettings.go`, `schedule.go`; `deleteSnapshot` in `snapshots_all.go` and its caller in `snapshots_bulk.go`; `settingsHandlers.save` in `settings.go` and the inline audit in `settingslists.go` | base |
| T2 Worlds, favorites, saves | builder, Opus low | `mcpadmin_worlds.go`, `generate.go`, `clone.go`, `worlds.go`, `favorites.go`, `saves.go`; `world-delete` audit (UI and MCP) | T1 |
| T3 Snapshots and live | builder, Sonnet high | `mcpadmin_snapshots.go`, `mcpadmin_live.go`, `snapshots.go`, `snapshots_all.go`, `snapshots_bulk.go`, `players.go`, `console.go` | T1 |
| T4 Mods | builder, Opus low | `mcpadmin_mods.go`, `mods.go`, `modsettings.go` | T1 |
| T5 Game settings, versions | builder, Opus low | `mcpadmin_settings.go`, `mcpadmin_versions.go`, `settings.go`, `settingsform.go`, `settingslists.go`, `update.go` | T1 |
| T6 Foreman-wide | builder, Sonnet high | `mcpadmin_foreman.go`, `schedule.go`, `notify.go`, `account.go`, `publicmodsettings.go` | T1 |
| T7 Gate, security, real game, e2e | tester, Opus low | new `mcpadmin_security_test.go`, `realgame4_mcp_admin_game_test.go` (tag `game`), `e2e/tests/mcp-admin.spec.ts` | T2-T6 (store tests may start from T1) |
| T8 Fold | builder, Sonnet high | `docs/STATUS.md`, `docs/SPEC.md`, `docs/OPERATOR.md` ("Connect an AI assistant"), new `docs/adr/0026-confirm-codes.md`; deletes this plan | T7 |

Waves: T1; then T2 to T6 in parallel from T1's verified head; then T7;
then T8.

### Tests every wave-2 task writes

- Each tool works, and is refused for the same reasons as its page.
- The audit row carries the token name.
- No secret appears in a result; untrusted fields are named in the
  tool's description.
- Each two-step tool: the first call leaves the data folder and the audit
  log unchanged; the preview parity test passes; a moved seen state is
  refused with a fresh preview.
- The existing e2e specs for every page whose handler was lifted still
  pass (the orchestrator runs them by file).

### T7 covers the 18.10b gate

- `mcpadmin_security_test.go`: for every tool with `DestructiveHint`, the
  first call changes nothing (data-folder tree and audit rows compared)
  and the preview names the snapshot label the UI would take. A code is
  refused for another token, tool or arguments, after 5 minutes, and on
  a second use. No code or secret (factorio.com token, webhook address,
  game password, RCON password) in logs or results.
- `realgame4_mcp_admin_game_test.go`: restore, a mod change (disable a
  probe mod placed in the world, no portal needed) and a version update
  (2.0.77 to 2.1.24) through MCP take the same snapshots and audit rows
  as the UI path, against a real `factoriotools/factorio` container.
- `e2e/tests/mcp-admin.spec.ts`: make an admin token in the browser, call
  `/mcp` with no cookie, check the UI shows nothing changed after the
  preview call and shows the effect after the confirm call (a restore on
  the Server page and "Before restore" on Snapshots; a deleted world
  gone from Worlds).

## Left out on purpose

Uploads and downloads, users, tokens and the MCP switch (21.7); map
preview pictures; Generate's resource-count and territory preview;
importing a folder; "start anyway"; the mod plan's picks panel
(`change_mods` takes names); archive (Epic 11); snapshot `taken_by_token`
(ruling 6); the raw settings editor (ruling 5).
