#!/usr/bin/env bash
# Stop hook for agents that edit code: refuse to finish while the fast
# checks fail in the agent's working directory. The checks are whatever
# `make verify` runs; until the stack epic adds that target, this is a no-op.
set -u
input="$(cat)"
if command -v jq >/dev/null 2>&1; then
  cwd="$(printf '%s' "$input" | jq -r '.cwd // empty')"
else
  cwd="$(printf '%s' "$input" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("cwd",""))')"
fi
[ -n "$cwd" ] || exit 0
cd "$cwd" 2>/dev/null || exit 0

[ -f Makefile ] || exit 0
make -n verify >/dev/null 2>&1 || exit 0

log="$(mktemp)"
trap 'rm -f "$log"' EXIT
if ! make --no-print-directory verify >"$log" 2>&1; then
  {
    echo "make verify failed in $PWD. Fix it before you finish. Last lines:"
    tail -n 40 "$log"
  } >&2
  exit 2
fi
exit 0
