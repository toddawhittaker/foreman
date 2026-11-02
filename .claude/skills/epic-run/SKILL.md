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
   overlap, put the later task in a second wave cut from the epic head
   after the first wave lands.
5. **Pick models** per the delegation rules in CLAUDE.md: Haiku for
   mechanical work, Sonnet at high effort when the prompt settles every
   choice, Opus for design or diagnosis.
6. **Write the shared rules file** in the scratchpad (template below) and
   point every task prompt at it. Each prompt adds the task row, the base
   commit, the files in scope, and what done looks like.
7. **Verify each report** before merging: in the agent's worktree run
   `make verify`, read the diff, check that the files it claims are in the
   PR (`tea api /repos/todd/foreman/pulls/<n>/files`), and rule on anything
   it did outside its file list. Once a CI workflow exists, its run on the
   PR must be green as well (WORKFLOW.md, "Checks").
8. **Land with tea.** Once a task PR passes step 7, run
   `tea pr merge --style squash <n>`, confirm it merged, and delete its
   branch. A task PR into the epic has no human review; Todd reviews once,
   at the epic level. Never merge a PR into `main`. Main-into-epic and
   epic-into-main use merge commits. Hand a list of PRs to merger when
   there are several to land, or one that conflicts. PRs are not checked
   together, so after a wave lands, run `make verify` on the epic head
   and chase any failure before the next wave or the reviews.
9. **Reviews** after every task PR has landed, on a read-only worktree at
   the epic head, in parallel:
   - code-reviewer always, including dead code across the repo;
   - security-reviewer when the epic touches sign-in, trusted headers,
     sessions, the Docker API, RCON, file paths or uploads, webhooks, or
     secrets;
   - a11y-reviewer when it touches the web UI.
10. **Fix round.** Rule on each finding: fix now through task PRs, or
    record as a STATUS gap. Land fixes the same way, then have each
    reviewer confirm its findings are closed.
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
    scratchpad and create the PR with `tea pr create --base main
    --description-file <path>`.
14. **Check it yourself.** Run the full local checks on the epic head
    after merging `main` in, not just on the merger's word.
15. **Stop.** Report the PR and wait. Todd merges main.

## Gotchas

- Worktree agents start at `main`. Always give the exact base commit and
  tell them to `git reset --hard` to it.
- Keep an agent's worktree until its PR merges, so the agent can be
  resumed for a fix. After the merge, delete the branch and worktree.
- Tell reviewers never to switch branches in the main checkout.
- Agents share the scratchpad; give each its own subfolder.
- Tell running agents not to wait for their PR to land; waiting on a
  merge can deadlock.
- Never echo "merged" without checking the PR's state
  (`tea api /repos/todd/foreman/pulls/<n>`, `"merged": true`).
- Use `tea`, never `gh`; the repository is on Forgejo.

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
- Before the PR: `make verify` and the affected tests green. Report the
  commands and counts.
- Do not edit docs/STATUS.md, the plan, or the SPEC.md epic list. Put a
  proposed STATUS line (delivered, gaps left) in the PR body.
- Never `git add -A`. No attribution lines. Never force-push; merge the
  epic branch in to catch up.
- PR into `epic/<n>-<name>`, not main, with `tea pr create --base
  epic/<n>-<name> --description-file <path>`. Body: `Refs #N`, the SPEC.md
  section served, how it was verified, the STATUS line. Do not merge.
- Stay inside your listed files; name any exception in the report.
- Never touch a real deployment or a container you did not create.
- If a choice is left open, stop and report it rather than guessing.
- Report: PR number, head commit, what was built and left out, test
  commands with results.
```
