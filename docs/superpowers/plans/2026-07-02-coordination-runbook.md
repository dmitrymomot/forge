# P0 Foundation — Autonomous Coordination Runbook

> **For agentic workers:** This is the master orchestration runbook. Execute it end-to-end **without human checkpoints**. It sequences the 8 package plans + the delivery plan into one unattended run whose deliverable is a single green, fully-reviewed PR. Use superpowers:executing-plans for each package and the repo skills (`fix-ci`, `fix-review`, `commit-commands:commit-push-pr`, `ship`) for delivery.

**Goal:** Implement all 8 P0 foundation packages on the current branch, then open one pull request and drive it to fully green (CI passing) and fully resolved (every Claude-review thread addressed) — all unattended, so the human gets exactly one finished PR to review in the morning.

**Deliverable by morning:** one PR against `main`, CI `test` + `lint` green on the head commit, zero unresolved review threads, and `docs/superpowers/plans/PROGRESS.md` summarizing what shipped and anything backed out.

---

## Operating principles (READ FIRST — these override the "ask the human" default)

1. **No human in the loop.** The human is asleep. Never stop to ask for confirmation, approval, or a decision. When a plan or this runbook leaves a choice, pick the reasonable default (the plans already state defaults) and proceed. Do not use AskUserQuestion. Do not wait.
2. **Keep the branch green.** Never leave the branch in a state where `just check` fails at a wave boundary. A half-finished package that can't be made green must be **backed out** (see Failure Handling) so the final PR is green.
3. **Commit frequently, per the plans.** Use the exact conventional commit messages in each plan. **Never** add any Claude attribution / "Generated with" / "Co-Authored-By" line — to commits, PR title/body, or comments.
4. **Work only on the current branch** (`claude/musing-williams-ec21ce`). Never switch or create branches. Never `git reset --hard` across other work, never force-push destructively.
5. **Never self-merge.** Drive the PR to green + resolved, post a summary, and stop. The human merges.
6. **Bounded effort.** Respect the retry caps and the delivery-loop iteration cap below. If a cap is hit, back out / record and move on — do not loop forever.
7. **Log every phase** to `docs/superpowers/plans/PROGRESS.md` (format at the end) so the morning review is a glance.

## Prerequisites (verify in Phase 0; if unmet, log and stop)

- On branch `claude/musing-williams-ec21ce`, clean working tree.
- `gh` authenticated (`gh auth status` succeeds) with push + PR rights on the repo.
- The session runs in a permission mode that auto-approves Bash (git/gh/just/go), Edit, and Write — otherwise the run will block on prompts. (For a truly unattended run, start the fresh session accordingly; see the kickoff instructions.)

## Execution model

Sequential, single-branch. One **implementer subagent per package** executes that package's plan in full (TDD, commit per task). The orchestrator (you) verifies each package is green before moving to the next, runs a full-module gate at each wave boundary, then personally drives the delivery loop. Sequential avoids `go.mod`/`go.sum` conflicts (only `slug` adds a dependency) and yields one clean linear history — exactly what a single morning PR wants.

---

## Phase 0 — Preflight

- [ ] **0.1** `git -C <repo> branch --show-current` → must be `claude/musing-williams-ec21ce`. `git status --porcelain` → must be empty. If not, log to PROGRESS.md and STOP.
- [ ] **0.2** `gh auth status` → must succeed. If not, log and STOP (cannot deliver a PR).
- [ ] **0.3** Baseline gate: `just check`. Must be clean (the 7 renames + spec/plan docs are already committed). If it fails, fix the pre-existing breakage first or log and STOP.
- [ ] **0.4** Initialize `docs/superpowers/plans/PROGRESS.md` with the template at the end; commit it: `docs(progress): start autonomous P0 run`.

## Phase 1 — Wave 1 packages (independent leaves)

Order (any order is valid; use this one): **errorsx → sanitize → structfields → decimal → filetype**. For each package `P` in turn:

- [ ] **1.P.a — Implement.** Dispatch one implementer subagent with this instruction (fill in `P`):

  > Execute the implementation plan at `docs/superpowers/plans/2026-07-02-<P>.md` in full, using the superpowers:executing-plans skill. Do every task in order, strictly TDD (write the failing test, verify it fails, implement, verify it passes), and commit after each task with the exact conventional message in the plan — **no Claude attribution of any kind**. Follow the plan's Global Constraints. Do not stop for confirmation. Do not touch any package other than `<P>` (and shared `go.mod`/`go.sum` only if the plan explicitly says to). When done, run `just test ./<P>/...` and report the final state (tasks completed, test result, any task you could not complete and why). Return a concise status summary, not the file contents.

- [ ] **1.P.b — Verify.** After the subagent returns, the orchestrator independently confirms:
  ```bash
  just test ./<P>/... && just lint
  ```
  Expected: package tests `ok`; `golangci-lint` `0 issues`; nilaway/betteralign/modernize silent.

- [ ] **1.P.c — Retry / back out.** If verify fails: dispatch a **fix subagent** ("`just lint`/`just test ./<P>/...` is failing with `<paste output>`; fix `<P>` to green without changing other packages; commit the fix") — at most **2 retries**. If still failing after 2 retries, **back out** package `P`:
  ```bash
  git rm -r <P>/ 2>/dev/null; git checkout -- go.mod go.sum 2>/dev/null; go mod tidy
  git add -A && git commit -m "revert(<P>): back out — could not reach green in autonomous run"
  ```
  Record `P` as **BACKED OUT** in PROGRESS.md with the last error. Continue to the next package. (A backed-out package is simply not in this PR; the human ships it later.)

- [ ] **1.P.d — Log** `P`'s outcome (DONE + last commit hash, or BACKED OUT + reason) to PROGRESS.md.

- [ ] **1.gate — Wave 1 gate.** After all five: run full `just check`. Must be clean. If a stray cross-package issue appears, fix it (orchestrator or a fix subagent). Commit any gate fix. Log "Wave 1 complete".

## Phase 2 — Wave 2 packages (dependents)

Order (**must** be this order — dependencies): **slug → money → validate**.
- `slug` depends on the existing `random` package and promotes `golang.org/x/text/unicode/norm` to a direct dep (its plan includes the `go mod tidy` step).
- `money` depends on `decimal` (Wave 1). If `decimal` was BACKED OUT in Phase 1, **skip `money`** and log it as BLOCKED (dependency missing) — do not attempt it.
- `validate` is standalone.

For each: repeat steps **1.P.a → 1.P.d** exactly (implement → verify → retry/back-out → log), with the dependency guard above for `money`.

- [ ] **2.gate — Wave 2 gate.** Full `just check` clean. Then `go mod tidy && git diff --exit-code go.mod go.sum` — the only expected module change across the whole run is `slug`'s `x/text` promotion (already committed by slug's plan); if tidy shows an unexpected diff, commit it (`chore(deps): go mod tidy`). Log "Wave 2 complete".

## Phase 3 — Full-module verification gate

- [ ] **3.1** `just check` — clean.
- [ ] **3.2** `go build ./... && go vet ./...` — clean.
- [ ] **3.3** Log the set of packages that are DONE vs BACKED OUT/BLOCKED. This set defines what the PR contains.
- [ ] **3.4** If **zero** packages completed, log and STOP (nothing to ship). Otherwise continue — the PR ships the completed packages.

## Phase 4 — Delivery (open PR, drive to green + resolved)

Execute `docs/superpowers/plans/2026-07-02-delivery-ship-ci-review-loop.md`, with these autonomous-run adaptations:

- [ ] **4.1 — Open the PR** (Delivery Task 1). Non-draft (so `claude-code-review.yml` runs). Set the body from PROGRESS.md: list DONE packages, and explicitly list any BACKED OUT / BLOCKED ones under a "Deferred (not in this PR)" heading so the morning reviewer sees the full picture. No attribution line. Capture `PR=$(gh pr view --json number -q .number)`.

- [ ] **4.2 — Loop** (Delivery Task 2), capped at **10 iterations**. Each iteration:
  1. `gh pr checks "$PR" --watch --interval 20` (waits for CI **and** the Claude review workflow to conclude).
  2. Any CI job failed → invoke **`fix-ci`** (fetch failed logs → fix → `just check` locally → push). Then `continue` (next iteration).
  3. CI green → fetch the Claude review's inline comments and top-level review; invoke **`fix-review`** to implement fixes (keep `just check` green before pushing). For a comment you disagree with after verifying, reply on the thread with the reasoning instead of a code change.
  4. Resolve every thread via the GraphQL `resolveReviewThread` mutation (Delivery Task 2 / Step 4 has the list + resolve commands). Only resolve a thread once its issue is fixed or answered.
  5. Push accumulated fixes (`just check` first). Pushing re-runs CI + a fresh review round → next iteration.
  6. **Exit the loop** when, for the head commit: `test` = pass, `lint` = pass, the GraphQL query returns **zero** unresolved threads, and the latest review round added no new unaddressed comments. Confirm by running one final `gh pr checks "$PR"` after the last push.

- [ ] **4.3 — If the 10-iteration cap is hit without converging:** stop looping. Leave the PR as-is, and post a comment + PROGRESS.md entry listing the remaining red checks and unresolved threads (with links) so the human can finish. Do **not** keep looping.

- [ ] **4.4 — Wrap up.** Post a concise PR comment summarizing final state (packages shipped, CI green, threads resolved, anything deferred) — via `ship` or `gh pr comment` — **no attribution line**. Do **not** merge. Write the final PROGRESS.md summary and commit it.

---

## Failure handling — quick reference

| Situation | Action |
|---|---|
| Package won't go green after 2 fix retries | Back out the package (Phase 1.P.c), mark BACKED OUT, continue |
| `money` needed but `decimal` backed out | Skip `money`, mark BLOCKED, continue |
| Wave gate `just check` fails on a cross-package issue | Fix (orchestrator or fix subagent), commit, re-run gate |
| CI red on the PR | `fix-ci` skill; if a specific package's tests can't be fixed in 2 tries, back it out, push, note it, continue |
| Review thread you disagree with | Reply on the thread explaining why (verified), then resolve; never leave it silently unresolved |
| Delivery loop not converging in 10 rounds | Stop, comment the remaining items, log, hand back |
| `gh` unauthenticated / no push rights | Log and STOP at Phase 0 |
| Anything ambiguous | Pick the plan's stated default and proceed; never wait for the human |

## Definition of done (all true → the run succeeded)

- Every completable package is implemented and committed; each has green `just test ./<pkg>/...`.
- Full-branch `just check` is clean; `go mod tidy` shows no unexpected diff.
- One PR is open against `main`, non-draft, CI `test` + `lint` green on the head commit, zero unresolved review threads, no attribution lines anywhere.
- `PROGRESS.md` records: per-package DONE/BACKED OUT/BLOCKED, the PR number/URL, final CI state, threads resolved, and any deferred items.
- The PR is **not** merged.

## PROGRESS.md template

```markdown
# Autonomous P0 Run — Progress

Started: <run start, from the kickoff context>
Branch: claude/musing-williams-ec21ce

## Packages
| Package | Wave | Status | Last commit | Notes |
|---|---|---|---|---|
| errorsx | 1 | PENDING | | |
| sanitize | 1 | PENDING | | |
| structfields | 1 | PENDING | | |
| decimal | 1 | PENDING | | |
| filetype | 1 | PENDING | | |
| slug | 2 | PENDING | | |
| money | 2 | PENDING | | |
| validate | 2 | PENDING | | |

Status ∈ {PENDING, DONE, BACKED OUT, BLOCKED}.

## Delivery
- PR: <number / url>
- CI: <test/lint status on head>
- Review threads: <n resolved / n total>
- Deferred (not in this PR): <list>
- Loop iterations used: <n>/10

## Deferred / follow-ups for the human
- <list, or "none">
```
