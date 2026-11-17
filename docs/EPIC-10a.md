# Epic 10a: API tokens and MCP

Plan, 2026-11-17. Epic branch: `epic/10a-mcp`. Base: `877952ad`
(`origin/main`). Serves SPEC 12.10, 14.19, 21.1 to 21.6 and 18.10, with
the edits to 2, 3.2, 8.10, 8.11, 12.1, 12.9, 13.1, 14.1, 14.7, 14.8,
14.12 and 22.3. STACK 1 and 8 (go-sdk v1.8.0, stateless JSON mode).
Follow WORKFLOW and the epic-run skill. Epic 10b (admin tools, two-step
confirm, SPEC 21.7, 21.8) is a later epic.

## Rulings (Todd, 2026-11-17)

1. Three nested scopes: read, operate, admin. Admin is everything an
   admin does in the UI except users, tokens, the MCP switch, file
   uploads and downloads, and map preview pictures.
2. Destructive admin actions use a two-step confirm, covering everything
   the UI asks a person to confirm (10b).
3. Tokens: Working, Expired (owner renews, same secret, any browser
   sign-in, never through a token), Revoked (final, owner or any admin).
   30 days from creation or last renewal, every scope. Expired tokens
   stay renewable forever. Scope fixed for life. Every admin sees all
   tokens.
4. Reminder: banner with one-click Renew from 5 days before; webhook
   event `token-expiring` at 5 days and 1 day, off by default.
5. Chat through MCP is prefixed with the token name: "[Claude] ...".
6. MCP is an admin setting, off by default; `FOREMAN_MCP=off` forces it
   off.

## Design (architect, 2026-11-17)

- MCP lives in package `web` (`web/mcp*.go`). Tools call the same
  methods the UI handlers call (`s.game.*`, `s.runs.run`, `s.finish`,
  `s.serverPage`, `gameError`, `s.worlds.*`). Nothing moves out of
  `web`. Only `web/mcp*.go` imports the SDK.
- One `*mcp.Server` per scope, built at startup; the token's scope picks
  the server, so a client only sees its scope's tools. In 10a the admin
  server has the operate tools.
- Token logic copies the sessions pattern: `auth/apitokens.go` reuses
  `hashToken`; SQL in `store/apitokens.go`.
- `game.Actor` gets a `Token` field; the controller writes it to a new
  `audit_log.token_name` column.
- Request order on `/mcp`: MCP off (403) → existing cross-origin check →
  per-address failure limit (429) → bearer check via the SDK's
  `RequireBearerToken` (401 naming the state) → per-token limits (429)
  → write deadline lifted to 65 s for this request → SDK handler.
- Two code limits block the spec and change for `/mcp` only: the 30 s
  `WriteTimeout` (`web/server.go`) and the 64 KiB body cap
  (`web/middleware.go`).
- The hourly sweep (`web/sweep.go`) sends `token-expiring`, and also runs
  once at startup.
- Migration `0010_api_tokens.sql`:

```sql
CREATE TABLE api_tokens (
    id           INTEGER PRIMARY KEY,
    user_id      INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name         TEXT    NOT NULL COLLATE NOCASE,
    scope        TEXT    NOT NULL CHECK (scope IN ('read', 'operate', 'admin')),
    secret_hash  TEXT    NOT NULL UNIQUE,
    created_at   INTEGER NOT NULL,
    period_start INTEGER NOT NULL,
    revoked_at   INTEGER,
    last_used_at INTEGER,
    reminded     INTEGER NOT NULL DEFAULT 0 CHECK (reminded IN (0, 1, 2))
);
CREATE UNIQUE INDEX api_tokens_name ON api_tokens (user_id, name)
    WHERE revoked_at IS NULL;
CREATE INDEX api_tokens_user_id ON api_tokens (user_id);
ALTER TABLE audit_log ADD COLUMN token_name TEXT NOT NULL DEFAULT '';
-- token-expiring is opt-in (8.10): turn it off on existing webhooks.
UPDATE webhooks SET events_off = CASE WHEN events_off = ''
    THEN 'token-expiring' ELSE events_off || ',token-expiring' END;
```

## Builder choices (architect's picks; Todd approves with the plan)

- Tool names: `server_status`, `list_worlds`, `list_players`,
  `recent_log`, `list_snapshots`, `list_mods`, `start_server`,
  `stop_server`, `restart_server`, `switch_world`, `take_snapshot`,
  `send_chat`.
- `world_id` is required on actions and optional on reads (the active
  world when left out).
- Untrusted text sits in fields named `untrusted_*`; each tool's
  description names them.
- Results are structured JSON plus a one-line English summary taken from
  the Server page's wording.
- Starting despite failed pre-start checks is not offered through MCP in
  10a; the refusal lists the problems and points to the UI.
- Chat: the raw message is checked as in the UI (empty or `/` refused),
  then the prefixed line is checked for the 500-character limit.
- The MCP failure counter is separate from sign-in's, with the same
  numbers, so guessing at `/mcp` cannot lock out browser sign-in.
- If both reminders came due while Foreman was down, only the later one
  is sent.
- Token names allow Unicode letters and digits plus space, `.`, `_` and
  `-` (refines SPEC 12.10's "letters, digits"; `[`, `]`, `=` stay out).
- The create page shows `FOREMAN_PUBLIC_URL` + `/mcp`, else the
  request's own scheme and host.
- Revoke goes through a confirm page, because it is final.
- `list_mods` asks the mod portal with a time limit and says "portal not
  reached" rather than "no updates" when it cannot.
- Read tools carry the MCP `readOnlyHint`.
- The SDK's own localhost check is turned off; the token and Origin rule
  cover it.

## Tasks

Paths are under `internal/` unless they start with `docs/`, `e2e/`,
`cmd/` or `.`.

| Task | Owner, model | Files (nobody else edits them) | Order |
|---|---|---|---|
| T0 docs | orchestrator | docs/SPEC.md, docs/STACK.md (1, 8), this plan | Plan PR, first |
| T1 tokens core | builder, Opus low | `store/migrations/0010_api_tokens.sql`; `store/apitokens.go` + test; `store/audit.go` + test; `game/controller.go` (Actor.Token, audit, refused); `auth/apitokens.go` + test; `auth/tokenlimit.go` + test; `auth/limiter.go` + test; `config/config.go`, `config/env.go` + tests (`FOREMAN_MCP`); `web/server.go` (`tokens` field, setup, `s.apiTokenRoutes()` and `s.mcpRoutes()` calls); stubs `web/apitokens.go` and `web/mcp.go`; `web/mcpsetting.go` + test; `.gitleaks.toml` (rule for `fmn_` secrets) | Wave 1 |
| T2 token page and banner | ui-designer, Opus low | `web/apitokens.go` (list, create, renew, revoke, MCP switch, audits `token-create`, `token-renew`, `token-revoke`, `mcp-set`); `web/apitokens_test.go`; `web/apitokens_security_test.go`; `web/server.go` (banner lookup in `page()`); `web/render.go`; `templates/apitokens.html`, `apitokencreated.html`, `apitokenrevoke.html`; `templates/layout.html`; `static/foreman.css`; `i18n/locales/*/apitokens.json`, `i18n/translated/apitokens.json`, `common.json` only if needed; docs/I18N.md glossary (API token, scope, read, operate, admin, renew, revoke, expire); docs/DESIGN.md; `e2e/tests/api-tokens.spec.ts`; `e2e/tests/click-budgets.spec.ts` | Wave 2, from T1's verified head |
| T3 MCP endpoint and tools | builder, Opus low | `go.mod`, `go.sum`; `web/mcp.go` (request checks, three servers, handler); `web/mcptools.go` (read tools); `web/mcpactions.go` (actions, chat, the 50-second wait); `web/mcp_test.go`, `web/mcptools_test.go`, `web/mcpactions_test.go`; `web/middleware.go` + test (the `/mcp` body limit) | Wave 2, parallel with T2 |
| T4 token-expiring event | builder, Sonnet high | `notify/events.go`, `notify/body.go`, notify tests and testdata; `i18n/locales/*/notify.json`, `i18n/translated/notify.json`; `web/notify.go` + test (unticked on new webhooks); notification templates if needed; `web/sweep.go` + test; `cmd/foreman/main.go`; `e2e/tests/notifications.spec.ts` | Wave 2, parallel |
| T5 tests | tester, Opus low | `web/mcp_security_test.go` (14.19 line by line); `web/realgame3_mcp_game_test.go`; `e2e/tests/mcp.spec.ts` (create a token in the UI, call `/mcp`, revoke, turn MCP off) | Wave 3, epic head after T2 to T4 |
| T6 fold | builder, Sonnet high | docs/STATUS.md (Epic 10a; fix stale lines on tokens and audit token names); docs/SPEC.md (tool names); docs/OPERATOR.md (`FOREMAN_MCP` row, letting `/mcp` past a sign-in proxy without naming one, generic-body `token` field, "Connect an AI assistant" with a Claude Code example); docs/adr/ (token format and storage; MCP in `web` with one server per scope); this plan (deleted) | Last |

T1 adds the route calls and stubs so T2 and T3 each own one file and
never both edit `server.go`. No task edits a shared e2e helper; a task
that needs one stops and reports.

## Reviews

- T1: code-reviewer, security-reviewer (secret format, hashing, schema,
  limiters, nothing logged).
- T2: code-reviewer, a11y-reviewer (banner, secret display, revoke
  confirm), security-reviewer (`no-store`, owner-only renew, any-admin
  revoke, CSRF, banner return path, secret shown once).
- T3: code-reviewer, security-reviewer (no cookie or trusted header read,
  untrusted fields marked, limits, no secrets in results, one JSON-RPC
  message per body).
- T4: code-reviewer, plus a security check that the event is off by
  default and carries no secret.
- T5: code-reviewer.
- Integration: security-reviewer walks 14.19 line by line on the epic
  head; code-reviewer for fit and dead code.

## Checks before the epic PR

- `make verify`, the full `make e2e`, `make vuln` (new dependency),
  gitleaks over the branch, and `make test-game` with T5's real-game
  test.
- The 18.10 gate, on trial with a real `factoriotools/factorio` world,
  Claude Code as the client:
  1. Turn MCP on; make a read token and an operate token; add both to
     Claude Code.
  2. Operate token: read status; take a snapshot (audit row has the token
     name); send chat (players see "[name] ..."); restart with a 60 s
     countdown (warnings shown, call returns "still running", status
     reports the result).
  3. Read token: a restart is refused.
  4. Expiry: with Foreman stopped, set `period_start` back 31 days;
     start; the token gets a 401 naming "expired"; the banner shows;
     one-click renew; the same Claude Code setup works. At 26 days back,
     the banner shows and the token still works.
  5. Revoke: stays refused; no Renew offered.
  6. MCP off: every call gets 403; token create, renew and revoke still
     work.
  7. `curl`: the 121st request in a minute gets 429; a foreign `Origin`
     gets 403.

## Left room for 10b

- Admin tools join only the admin-scope server.
- Confirm codes go in memory beside the per-token limiter in `auth`.
- Audits that take a user rather than an Actor (`web/generate.go`,
  `web/settings.go`, `web/settingslists.go`, `web/snapshots_all.go`,
  `world.Manager` methods taking `by string`) cannot carry a token name
  yet; 10b changes them to take an Actor.

## Left out on purpose

OAuth discovery metadata, progress updates over a server stream, tokens
limited to one world, an audit viewer (Epic 11), pagination of the token
list.
