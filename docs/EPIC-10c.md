# Epic 10c: REST API

Branch `epic/10c-rest-api`, cut on 2026-11-21 from `b460d0f6`, the head
of `epic/fixes-15` (which holds the audit follow-up `fix/audit-decisions`).
Todd chose this base because agents are working on fixes-15 now. Until
those pull requests merge, this epic's pull request also shows their
commits, and it merges after them; main is then merged in.

The REST API accepts the same personal API tokens as `/mcp` and runs the
same tools through the same code (SPEC 23, STACK 3 and 8). The fold task
(FD) deletes this file.

## Design in one paragraph

`internal/web/api.go` turns each REST call into an MCP `tools/call`
message and hands it, in the same process, to the SDK server for the
token's scope, the same one `/mcp` uses. It then turns the JSON-RPC
answer back into the REST envelope (SPEC 23.5). The tool handlers read
only the token from the request context and the tool name, so no tool
file changes, and scopes, schema checks, confirm codes, the snapshot cap,
the chat prefix and the audit log cannot drift. The request checks in
`mcp.go` (Origin, address limit, bearer token, token limits, second
token check after the body) are built once and shared, so one token has
one budget across both paths.

## Tasks

| Task | Owner, model | Files it may touch |
|------|--------------|--------------------|
| A1 API core | builder, Opus low | `internal/web/mcp.go` (share the checks and limiters), new `internal/web/api.go`, new `internal/web/apisetting.go` and its test (the API switch, mirroring `mcpsetting.go`), `internal/config/config.go` and `config_test.go` (`FOREMAN_API`), `internal/web/middleware.go` (4 MiB cap on `/api/v1/`), `internal/web/server.go` (mount the routes only), new `internal/web/api_test.go` |
| D1 operator guide | builder, Sonnet high | `docs/OPERATOR.md` (a REST subsection under MCP with `curl` examples, `FOREMAN_API` in the settings table, the forward-auth note covering `/api/`), the MCP lines of `README.md` |
| T1 token page and switch | builder, Sonnet high | `internal/web/apitokens.go`, `templates/apitokens.html`, `templates/apitokencreated.html`, `internal/web/auditlog.go` (`api-set` only), `internal/i18n/locales/*/apitokens.json`, `internal/i18n/locales/*/audit.json`, `internal/i18n/translated/` for those, `apitokens_test.go`, `e2e/tests/api-tokens.spec.ts`, new `e2e/tests/api.spec.ts` |
| O1 OpenAPI | builder, Sonnet high | new `internal/web/apiopenapi.go` and its test, `internal/web/api.go` (mount one route only) |
| V1 invariant tests | tester, Opus low | new `internal/web/api_security_test.go`, new `internal/web/api_parity_test.go`, new `internal/web/realgame5_api_game_test.go` |
| FD fold | builder, Sonnet high | `docs/STATUS.md`, `docs/SPEC.md` (only rules this plan settled that are not yet there), new `docs/adr/0029-rest-api-translates-mcp.md`, `docs/adr/README.md`; deletes this plan |

No two tasks in a wave share a file. O1 edits `api.go` only after A1 has
merged. No task touches a file the fixes-15 tasks own.

What each task's tests cover:

- **A1**: the envelope and every status of 23.4 and 23.5 (200, 422 with
  and without a result, 404, 400, 415, 405, 413, empty body), the write
  deadline, the switch's 403 before the token is read, and that every
  existing `mcp_*_test.go` passes unchanged.
- **T1**: both addresses shown and copyable, using `FOREMAN_PUBLIC_URL`
  when set; the API switch, its forced-off state and its audit entry;
  `Cache-Control: no-store` kept. End to end: make a token in the UI,
  then call `/api/v1` with it through Playwright's request API, with the
  switch on and off.
- **O1**: the document is OpenAPI 3.1, lists exactly the scope's tools,
  and carries each tool's input and output schema unchanged.
- **V1**, written from SPEC 14.19 and 23, not from A1's code: REST and
  MCP list the same tools with the same schemas for every scope; every
  tool outside a scope gives 404; switch off gives 403 even with a
  malformed token and makes no database lookup; each switch leaves the
  other path working; Origin rule; a token in the query string is
  ignored; expired and revoked tokens get 401s naming their state; a
  revoke while the body is held back is refused; per-token and address
  limits are shared across both paths; a confirm preview changes
  nothing, a code is refused for another tool or other arguments, spent
  on any use, and works across endpoints; a moved state gives 422 with a
  fresh code; the snapshot cap is shared; the audit entry names the
  token; no secret in any answer or log line. The real-game test runs
  the 18.10c gate.

## Waves

1. A1 and D1 together, from the plan's merge commit.
2. T1, O1 and V1 together, from A1's verified head, without waiting for
   its review.
3. FD from the epic head after all five merge and after `epic/fixes-15`
   has merged to main (FD writes `docs/STATUS.md`, which fixes-15's fold
   also writes).

## Rulings in this plan

1. **Routes** (Todd, 2026-11-21): `GET /api/v1/tools` and
   `POST /api/v1/tools/<name>`, not resource-style routes.
2. **Switch** (Todd): REST API access has its own switch, off by
   default, with `FOREMAN_API=off` and the audit action `api-set`. The
   MCP switch, `FOREMAN_MCP` and `mcp-set` are unchanged.
3. **Priority** (Todd): P1, built before Epic 11b, so Gate C's security
   review covers it.
4. **OpenAPI** (Todd): generated from the tool schemas at
   `/api/v1/openapi.json`. Built in this epic as P1. It needs a token
   and describes that token's scope, like the tool list; no OpenAPI
   library is added.
5. **Errors** (Todd): every tool refusal is 422 with an English `error`.
6. **One token budget** across `/mcp` and `/api/`, so a token cannot
   double its rate limit by using both.
7. **The hand-off is in process through the SDK server.** If A1 finds it
   cannot pass the token to the tool handlers this way, it stops and
   reports instead of building a tool registry around `mcp.AddTool`.
8. **No new dependency.** OpenAPI validation in the gate uses a pinned
   linter run once by the orchestrator, not a Go library.

## Checks

Each task: the orchestrator runs `make verify` and the e2e specs for every
file it touched; A1 touches `server.go`, so its pull request gets the
full suite. Every task gets a code review. A1, O1 and V1 get a security
review (trust boundary: tokens and the new endpoint). T1 gets an
accessibility review. Before the epic pull request: `make test-game`
and the real-game gate of SPEC 18.10c.
