# 0029. The REST API hands each call to the MCP tool servers in process

- **Status**: Accepted
- **Date**: 2026-11-21
- **References**: SPEC.md sections 23, 14.19 and 18.10c; STACK.md sections 3 and 8; ADR 0025, ADR 0026

## Context

STACK 3 first said there would be no public JSON API. On 2026-11-21 I ruled
to add a REST API on the same API tokens, for scripts that cannot speak MCP.
It must offer the same tools, scopes, refusals, snapshots, confirm codes and
audit entries, and it must not be able to drift from `/mcp` later.

## Decision

The routes are `GET /api/v1/tools` and `POST /api/v1/tools/<name>`. Each call
becomes an MCP `tools/call` message and goes, in the same process, to the SDK
server for the token's scope, the one `/mcp` uses. The tool handlers do not
change, so the two paths cannot disagree about a tool.

The token checks and limiters are built once and shared, so a token has one
budget across `/mcp` and `/api/`, and a confirm code works on either. REST
access has its own switch (`FOREMAN_API`, audited as `api-set`), separate
from MCP's. Every refusal by a tool is 422 with its English message. A fault
Foreman cannot word is 500. Within `/api/v1/`, things are only added, never
removed or renamed (SPEC 23.7). An OpenAPI 3.1 document is generated from
the tools' own schemas.

Rejected:

- **A registry wrapper around `mcp.AddTool`:** 53 edits, a copied schema
  check, and drift.
- **Resource-style routes:** about 55 hand-mapped routes and a second naming
  scheme.
- **One `/api/v1/call` route:** the logs cannot see which tool ran.
- **One switch for both:** I chose separate switches.
- **A specific error code for each refusal:** it edits every error site.
- **A hand-written OpenAPI file:** it drifts.

## Consequences

Changing an MCP tool changes the REST API with no further work. The MCP tool
names, arguments and results are now a versioned public contract, and a
breaking change needs a new version path. A fix to the shared token checks
protects both paths.
