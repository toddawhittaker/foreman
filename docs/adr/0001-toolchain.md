# 0001. Local Go, pinned images for the other tools

- **Status**: Accepted
- **Date**: 2026-11-02
- **References**: STACK.md sections 1, 9 and 13

## Context

Every build and check must give the same result on any machine and in CI,
and nothing may download tools at run time. Go is used all day, so it
should be fast and easy to run in an editor. Running everything inside
containers would make each `go test` slower and awkward on a shared CI
runner, where bind mounts arrive empty. The linter and the browser tests
are different: they are large, need exact versions, and are run rarely.

## Decision

Go is installed locally. Its exact version is pinned once, as `GO_VERSION`
in the Makefile. `make verify` runs `make check-go`, which fails with a
plain message when the local Go, or the Go in the build image's name,
differs from that pin. The Makefile sets `GOTOOLCHAIN=local`, so Go never
fetches another toolchain. CI installs the same version and checks its
SHA-256.

golangci-lint and Playwright run in images pinned by digest. Nothing is
installed for them.

The `go` line in `go.mod` states the language minimum, `go 1.27.0`, not the
pinned toolchain `1.27.3`. The golangci-lint image is built with an older
1.27 patch (1.27.2) and refuses to load a module that asks for a newer one.
The pin that matters lives in the Makefile; `go.mod` only says which
language features the code may use.

## Consequences

- A contributor installs one tool, Go, at one version, and the
  instructions are in WORKFLOW.md "Local development".
- Raising Go means changing `GO_VERSION`, `GO_IMAGE`, the CI version and
  checksum together; `check-go` catches a mismatch between the first two.
- Raising the `go.mod` minimum is a separate decision, and needs a
  linter image built with at least that patch.
- The lint image and Playwright image are large, but pulled once and
  cached.
