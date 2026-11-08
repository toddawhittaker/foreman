---
name: epic-run
description: Use when the orchestrator starts or continues a Foreman epic (a feature epic or a fix batch) and must take it from branch to a green epic pull request ready for Todd's merge.
---

# Running an epic end to end

The main session orchestrates; agents do the work (CLAUDE.md, "How work
gets done here"). Plans and folding follow docs/WORKFLOW.md, "Epic plans".
Keep a memory file for the epic with every ruling and a dated progress
line per step, so the run survives a context reset.

## Steps

1. **Branch.** `git fetch origin`, then cut `epic/<n>-<name>` from the
   current `origin/main` commit. Write that commit down; it is the base for
   every wave-one agent.
2. **Design (feature epics only).** Run architect before writing the plan.
   Fix batches skip this.
3. **Plan.** Write `docs/EPIC-<n>.md` (tasks, owner agent, model, files
   each task may touch, issues it closes) and land it on the epic branch
   through a plan PR. Ask Todd plan questions one at a time, each with a
   recommendation.
   **Then stop for Todd's approval.** Show the plan itself, not just a
   link: each task with its owner agent, model, files and issues, the
   waves, and the rulings made. Spawn no task agent until Todd approves,
   unless the request already said to run the epic through without
   stopping. Record the approval in the epic's memory file.
4. **Split tasks** so no two agents edit the same files. When files must
   overlap, chain them: the later task starts from the earlier task's
   head as soon as that head passes step 7, without waiting for its
   merge or its review. After the earlier PR squash-merges, the later
   agent merges the epic branch in (or merger rebuilds its branch by
   diff-apply), so its PR shows only its own change.
5. **Pick models** per the delegation rules in CLAUDE.md: Haiku for
   mechanical work, Sonnet at high effort when the prompt settles every
   choice, Opus for design or diagnosis.
6. **Write the shared rules file** in the scratchpad (template below) and
   point every task prompt at it. Each prompt adds the task row, the base
   commit, the files in scope, and what done looks like.
7. **Verify each report** before merging: in the agent's worktree run
   `make verify`, read the diff, check that the files it claims are in the
   PR (`gh pr view <n> --json files`), and rule on anything
   it did outside its file list. Then run the browser tests for every
   file the task touched (below). A task PR does not wait for its GitHub
   CI run; the full CI run gates only the epic PR into main (step 13).
   - **Browser tests by file, not by task.** For each changed file, run
     every e2e spec that loads a page using it: a template, its handler,
     and the specs that visit that page; a script, every page that loads
     it; a locale file, the i18n specs plus the pages that show its keys.
     Find them with `grep -l` over `e2e/tests` for the route, template or
     element ids. A change to a file every page uses (`foreman.css`,
     `layout.html`, `live.js`, `render.go`, `server.go`, the e2e helpers)
     runs the whole suite. Pass the list as `make e2e E2E_ARGS="tests/a.spec.ts
     tests/b.spec.ts"`.
8. **Land with gh.** Once a task PR passes step 7, run
   `gh pr merge --squash <n>`, confirm it merged, and delete its
   branch. A task PR into the epic has no human review; Todd reviews once,
   at the epic level. Never merge a PR into `main`. Main-into-epic and
   epic-into-main use merge commits. Hand a list of PRs to merger when
   there are several to land, or one that conflicts. PRs are not checked
   together, so after a wave lands, run `make verify` and the full
   `make e2e` on the epic head and chase any failure before the next wave
   or the reviews.
   Merge only after reading the check result: never chain `gh pr merge`
   after a wait in one command.
9. **Reviews, per task as it lands.** As soon as a task PR merges, start
   its reviews in the background on a read-only worktree at that merge
   commit, scoped to the task's diff, while the next tasks run:
   - code-reviewer always;
   - security-reviewer when the task touches sign-in, trusted headers,
     sessions, the Docker API, RCON, file paths or uploads, webhooks, or
     secrets;
   - a11y-reviewer when it touches the web UI.
   After the last task lands, run one integration review on the epic
   head: code-reviewer for how the tasks fit together and dead code
   across the repo, plus a11y-reviewer for flows that cross tasks.
10. **Fix round, per task.** Rule on each finding as its review returns:
    fix now, or record as a STATUS gap. A fix goes into the next task of
    the same lane when one is still to run, otherwise into a small fix
    task started at once, not saved up for the end. Land fixes the same
    way, then have the reviewer confirm its findings are closed.
11. **Real-game check** when the epic touches server control, world
    switching, snapshots, mods or versions: run the change against a real
    `factoriotools/factorio` container (the WORKFLOW.md procedure, once
    written) and confirm the game reaches "InGame" with the expected save
    and mods.
12. **Fold task** (last task PR): write the epic's STATUS section from the
    task PR bodies, move lasting rules into SPEC.md, put any "why" into an
    ADR, delete the plan, and carry a `Fixes #N` line for each issue.
13. **Epic PR into main.** Cite the SPEC.md and STACK.md sections and how
    it was verified (WORKFLOW.md, "Pull requests"). Keep the body in the
    scratchpad and create the PR with `gh pr create --base main
    --body-file <path>`.
14. **Check it yourself.** Run the full local checks on the epic head
    after merging `main` in, not just on the merger's word.
15. **Stop.** Report the PR and wait. Todd merges main.

## Gotchas

- A task PR stuck for more than 30 minutes (a flaky or failing test,
  a conflict, a slow diagnosis) becomes its own task: hand the failure to
  a separate, time-boxed agent with the failing output, and keep the
  chain moving from the last verified head. Do not hold a lane on it.

- Worktree agents start at `main`. Always give the exact base commit and
  tell them to `git reset --hard` to it.
- Keep an agent's worktree until its PR merges, so the agent can be
  resumed for a fix. After the merge, delete the branch and worktree.
- Tell reviewers never to switch branches in the main checkout.
- Agents share the scratchpad; give each its own subfolder.
- Tell running agents not to wait for their PR to land; waiting on a
  merge can deadlock.
- Run at most two full `make e2e` suites on the machine at once. More
  than that loads it so heavily that every run slows and timing tests
  flake. Tell agents which specs to run, or to queue the full suite.
- An agent that reports "waiting on its own background work" for more
  than ten minutes has usually stalled on a monitor. Resume it with
  SendMessage and tell it to finish its runs in the foreground.
- A test that fails, passes on rerun, and fails again is a bug to fix
  now, in a task of its own: it is often a real race in the code.
- Never echo "merged" without checking the PR's state
  (`gh pr view <n> --json state`, `"state": "MERGED"`).

## Shared rules file template

```markdown
# Epic <n> shared rules for task agents

Epic <n>, branch `epic/<n>-<name>`, cut from main <commit>. The plan is
docs/EPIC-<n>.md on the epic branch; read your task's row.

- In your worktree: `git fetch origin && git reset --hard <base from your
  prompt>`, then `git switch -c <epic-slug>/<task>`.
- Scratchpad <path> is shared; use a subfolder named after your task.
- Tests first: a bug fix starts with a failing test. Design tests from the
  SPEC.md invariant. Anything a user can see ships with end-to-end tests.
- A failure near your code is yours until it is shown to fail the same way
  on the base without your change.
- Before the PR: `make verify` green, and the browser tests for every
  file you touched, not only your own new tests: each e2e spec that
  loads a page using a changed template, handler, script, style or
  locale file (`make e2e E2E_ARGS="tests/a.spec.ts ..."`); a file every
  page uses means the whole suite. Run tests in the foreground; do not
  wait on monitors. Report the commands and counts.
- Do not edit docs/STATUS.md, the plan, or the SPEC.md epic list. Put a
  proposed STATUS line (delivered, gaps left) in the PR body.
- Never `git add -A`. No attribution lines. Never force-push; merge the
  epic branch in to catch up.
- PR into `epic/<n>-<name>`, not main, with `gh pr create --base
  epic/<n>-<name> --body-file <path>`. Body: `Refs #N`, the SPEC.md
  section served, how it was verified, the STATUS line. Do not merge.
- Stay inside your listed files; name any exception in the report.
- Never touch a real deployment or a container you did not create.
- If a choice is left open, stop and report it rather than guessing.
- Report: PR number, head commit, what was built and left out, test
  commands with results.
```
