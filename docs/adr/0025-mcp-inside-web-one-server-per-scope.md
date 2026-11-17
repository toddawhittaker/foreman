# 0025. MCP lives in package web, one SDK server per scope, with a strict Origin rule

- **Status**: Accepted
- **Date**: 2026-11-17
- **References**: SPEC.md sections 14.19, 21.1 and 21.3; STACK.md section 8

## Context

The MCP tools do what the web pages do: start, stop, snapshot, chat. They
must run the same code, with the same checks and audit, and a client must
see only the tools its token's scope allows.

## Decision

MCP lives in package `web`, in `internal/web/mcp*.go`. The tools call the
same methods the page handlers call, so nothing moves out of `web`. Only
those files import the official Go SDK.

Foreman builds one SDK server per scope (read, operate, admin) at startup.
The token's scope picks the server, so a client's tool list holds only
what it may call, and any other tool is unknown, not forbidden. The SDK
runs stateless with plain JSON answers: each POST carries one message and
gets one answer, with no session to expire, share or leak.

On `/mcp` any request with an `Origin` header is refused unless it is
exactly `FOREMAN_PUBLIC_URL`'s origin. The general same-origin check
accepts an `Origin` equal to the `Host`, and under DNS rebinding the
attacker's page sends exactly that. Real MCP clients send no `Origin`. The
SDK's own localhost check is turned off, because this rule and the token
cover it.

## Consequences

- A tool cannot drift from its page: both call one method.
- The server count is fixed by the scopes, not by the tools.
- A browser-based MCP client on another origin cannot work. That is the
  intent.
- Rejected: a separate package for the tools. It would copy the handlers'
  helpers or force a wide interface to be exported for one caller.
- Rejected: one server that checks the scope per call. A client would be
  offered tools it could never use.
