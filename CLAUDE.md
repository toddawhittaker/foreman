# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

Foreman (a working name) is a self-hosted web app that runs Factorio
servers through Docker and manages "worlds": a map plus its own mods,
settings and pinned game version. Keep this file under 150 lines. It holds
only pointers and process rules. Status, architecture and what the code
does belong under `docs/`, cited by file and section, never here.

## Where the project stands

Only the vision exists. There is no code, no spec and no chosen stack.
Work proceeds in this order, and each step needs the
user's line-by-line approval before the next begins:

1. **Vision** (`docs/VISION.md`, drafted). Intent, principles, non-goals.
2. **Spec** (`docs/SPEC.md`, next). Numbered requirements, each marked P0
   (no first usable version without it), P1 (works without it, but not
   well) or P2 (wanted, not scheduled). It must include a security section
   listing every trust boundary, an accessibility and design section, an
   ordered epic list with what each delivers, and milestone gates.
3. **Stack** (`docs/STACK.md`). Each technology choice, why, and what was
   rejected and why. It defines the `make` targets, including
   `make verify` (see "Testing rules").
4. **Code**, one epic at a time.

The vision ends with seven open questions. They belong to the user:
when one comes up, give two to four options with gains and costs,
recommend one, and record the answer in the spec. Don't settle one
silently. Use the architect agent to draft spec and stack options.

## Where things are documented

- `docs/VISION.md`: product intent, principles, constraints, non-goals.
  Wins on questions of intent.
- `docs/SPEC.md` (to come): requirements. Wins on implementation detail.
- `docs/STACK.md` (to come): technology choices and rejected options.
- `docs/STATUS.md` (to come, written by each epic's fold task): what each
  epic delivered and the gaps it left.
- `docs/WORKFLOW.md`: branching, epic plans, code style, pull requests,
  secret scanning, and GitHub use. Local development is added once the stack is
  chosen.
- `docs/I18N.md`: the eight languages, the glossary, the address rule per
  language, and how to write and add a translated string.
- `docs/adr/`: decision records, one for any choice a reader asks "why"
  of. Copy `docs/adr/0000-template.md`.
- GitHub issues (labels `enhancement` and `bug`): wanted work that is
  not yet an epic or task. File one with `gh issue create` when the user
  defers something.

Before implementing any subsystem, read the SPEC.md and STACK.md sections
that cover it. Cite sections by number in prompts, commits and reports.

## How work gets done here

The main session is an orchestrator: understand the request, write good
prompts, spawn agents, verify their claims, and report in plain English.
It does the work itself only for a one-line lookup or edit, or for
drafting the vision, spec and stack with the user. Agents live in
`.claude/agents/`; each one's description says when to use it.

Running an epic (task PRs and how they land, which reviewers run, the
fold, the epic PR) follows the `epic-run` skill.

Orchestration rules:

- Delegate the work itself, so the main session gets back to the user
  quickly. Hand long waits (CI, merges, long runs) to agents in the
  background too.
- Put each decision the user must make in AskUserQuestion, never as a
  question buried in a block of text: people skim text. Give each
  decision a reasoned recommendation, listed first, and plausible
  alternatives, each with what it gains and what it costs.
- Keep the work moving. While agents run, prepare the next prompts,
  reviews and checks. Start dependent work from a verified branch head
  instead of waiting for its merge. End a turn only when the work is
  done, the user must decide something, or every next step waits on a
  running agent.
- Delegate to the cheapest agent that can do the job. Run explorer first
  when a location is unknown and pass its paths on. When a builder or
  tester prompt settles every choice, spawn it on Sonnet at high effort
  and tell it to stop and report any choice the prompt leaves open.
- Run independent agents in parallel. Never let two agents edit the same
  files; split by module or directory and merge in the main session.
- A good prompt names the SPEC.md and STACK.md sections, the files in
  scope, the base commit, what done looks like, and what to leave alone.
- Agent worktrees start at `main`. Tell a worktree agent the exact commit
  to reset to, and land its work with `git diff <base> <head> | git apply
  --3way`, not a rebase.
- Verify before relaying: run `make verify` on an agent's work; a Stop
  hook runs it for builder, tester and ui-designer.
- When agents disagree, read the spec section in dispute and rule. If the
  spec does not settle it, bring both positions and a recommendation to
  the user. Say what was ruled in the report.
- Default effort is low. Raise it for one task when it needs sustained
  reasoning, high for intensive debugging. Time-box debugging agents.
- Never `git add -A`; stage paths explicitly.
- Delete local branches and agent worktrees after a merge.

## Testing rules

- `make verify` is the contract for the fast checks (typecheck, lint, unit
  tests, whatever the stack has). The stack epic must create it; until
  then the Stop hook skips. Prefer a `make` target over an invented
  command.
- Tests are part of done. A change ships with unit tests for its logic,
  and any change a user can see ships with end-to-end tests too. Design
  tests from the SPEC.md invariant and the vision's principles, not from
  what the code happens to do.
- Write the test first when the behavior is clear enough to state; at
  minimum, a bug fix starts with a failing test that reproduces it.
- Anything that touches server control, world switching, snapshots, mods
  or game versions is also checked against a real `factoriotools/factorio`
  container before its epic PR.
- Never lower a coverage floor or weaken a test to make a run pass; say so
  and let the user decide.

## Review and merge rules

- `main` changes only by pull request, and merging it is the user's
  decision. Prepare the pull request, citing the SPEC.md and STACK.md
  sections it serves and how it was verified (WORKFLOW.md, "Pull
  requests"), then report and stop.
- The repository is on GitHub (`origin`). Use `gh` for pull requests,
  issues and releases (WORKFLOW.md, "Where the code lives"). `make verify` run locally is the merge gate,
  plus the CI checks (WORKFLOW.md, "Checks").

## Project rules that override convenience

- Pin exact versions of dependencies and Foreman's own images, and commit
  the lockfile. Game images are referenced by exact Factorio version tag
  (ADR 0005). Nothing downloads at startup.
- Never write a world's files without the snapshot the spec requires.
- Never log, display or return a secret: the RCON password, portal token,
  OIDC secret or game password.
- Nothing in the product may be specific to the reference deployment in
  the vision (Caddy and Authentik in front of Foreman).
- Don't design or build the vision's non-goals. Several worlds running at
  once is the one the data model must leave room for, unbuilt.
