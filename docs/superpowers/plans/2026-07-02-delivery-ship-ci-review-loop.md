# Delivery — PR → CI → Review Loop Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans. Steps use checkbox (`- [ ]`) syntax for tracking. This is the **final phase**, run once after all 8 package plans are implemented and merged into this branch.

**Goal:** Open the pull request for the P0 foundation batch and drive it to a fully green, fully reviewed state — CI passing and every Claude-review thread resolved — iterating until there is nothing left to fix.

**Architecture:** This is a **procedural delivery loop**, not a TDD phase (there is no failing-test → implementation cycle). It uses the repository's existing skills — `commit-commands:commit-push-pr`, `fix-ci`, `fix-review`, `ship` — plus the `gh` CLI (REST + GraphQL) to open the PR, watch checks, fetch failures, apply fixes, and resolve review threads.

**Tech Stack:** `gh` CLI, GitHub Actions (`.github/workflows/ci.yml` = `just test` + `just lint`; `.github/workflows/claude-code-review.yml` = inline review comments), repo skills.

## Global Constraints

- Module: github.com/dmitrymomot/forge. Go 1.26.
- Work ONLY in the current branch (`claude/musing-williams-ec21ce`); do not switch branches.
- Every local fix must keep `just check` (fmt + lint + test `-race`, incl. go vet, golangci-lint, nilaway, betteralign, modernize) clean before pushing.
- Conventional commit messages. NEVER add any Claude attribution, "Generated with", or "Co-Authored-By" line — this applies to commits, the PR title/body, and every PR comment.
- The CI workflow (`ci.yml`) has two required jobs: **test** (`just test`) and **lint** (`just lint`), triggered on `pull_request` to `main`.
- The Claude review workflow (`claude-code-review.yml`) auto-runs on **non-draft** PRs authored by the repo owner and posts **inline** comments + a top-level comment. The PR MUST be opened non-draft (or marked ready) for it to run.

---

## Preconditions (verify before starting)

- [ ] **Step 0a: All 8 package plans are implemented on this branch.**

Run:
```bash
git -C /Users/dmitrymomot/Dev/claude_worktrees/forge/musing-williams-ec21ce branch --show-current
```
Expected: `claude/musing-williams-ec21ce`

Confirm each package directory exists:
```bash
ls -d errorsx sanitize structfields decimal filetype slug money validate
```
Expected: all eight directories listed (no "No such file or directory").

- [ ] **Step 0b: The whole module is green locally.**

Run:
```bash
just check
```
Expected: `just fmt`, `just lint`, and `just test` all succeed with no errors (test output ends in `ok` for every package; `golangci-lint` prints `0 issues`; nilaway/betteralign/modernize print nothing). If anything fails, fix it before opening the PR — do not open a PR on a red tree.

---

### Task 1: Open the pull request

**Files:** none (Git/GitHub only).

**Interfaces:**
- Consumes: a green local branch (Preconditions).
- Produces: an open, **non-draft** PR number `$PR` targeting `main`, which triggers `ci.yml` (test + lint) and `claude-code-review.yml`.

- [ ] **Step 1: Push the branch and open the PR.**

Preferred: invoke the `commit-commands:commit-push-pr` skill (it pushes and opens the PR). If opening manually with `gh`, use a body that summarizes the 8 packages and links the spec — and contains **no** attribution line:

```bash
git push -u origin claude/musing-williams-ec21ce
gh pr create \
  --base main \
  --head claude/musing-williams-ec21ce \
  --title "feat: P0 foundation — errorsx, sanitize, structfields, decimal, filetype, slug, money, validate" \
  --body "$(cat <<'EOF'
Completes the P0 foundation layer (8 packages) per
docs/superpowers/specs/2026-07-02-p0-foundation-completion-design.md.

- errorsx — coded errors + retryable/permanent tagging
- sanitize — Apply/Compose + plain-text sanitizers
- structfields — the one sanctioned reflection helper
- decimal — exact fixed-point base-10 arithmetic
- filetype — magic-byte MIME detection
- slug — URL-safe slugs with options
- money — currency-aware money over decimal
- validate — composable Rule[T] validation (i18n keys)

Also: nullx→null, hashx→digest, randx→random, subtlex→consttime renames.
EOF
)"
```

- [ ] **Step 2: Capture the PR number and confirm it is non-draft.**

Run:
```bash
PR=$(gh pr view --json number -q .number); echo "PR=$PR"
gh pr view "$PR" --json isDraft,mergeStateStatus -q '{draft: .isDraft, state: .mergeStateStatus}'
```
Expected: `draft: false`. If `draft: true`, run `gh pr ready "$PR"` so the Claude review workflow will run.

---

### Task 2: Drive the PR to green + fully resolved (the loop)

**Files:** whatever the fixes touch (varies per iteration).

**Interfaces:**
- Consumes: the open PR `$PR`.
- Produces: CI conclusion `success` on both `test` and `lint`, and **zero unresolved** review threads.

Repeat the following iteration until the **Exit criteria** below are all satisfied. Each push (`synchronize`) re-runs CI and re-runs the Claude review, so re-check every signal each pass.

- [ ] **Step 1: Wait for CI + the Claude review to finish.**

Run:
```bash
gh pr checks "$PR" --watch --interval 20
```
Expected: the command exits when all checks conclude. Then snapshot conclusions:
```bash
gh pr checks "$PR"
```
Note which of `test` / `lint` (and `Claude Code Review`) are `pass` vs `fail`.

- [ ] **Step 2: If any CI job failed, fix it.**

Invoke the **`fix-ci`** skill (it fetches the failed workflow logs, diagnoses, fixes the code, and pushes). Manual equivalent:
```bash
RUN=$(gh run list --branch claude/musing-williams-ec21ce --workflow CI --limit 1 --json databaseId -q '.[0].databaseId')
gh run view "$RUN" --log-failed
```
Reproduce locally and fix until green:
```bash
just check           # must pass locally before pushing
```
Commit + push the fix (conventional message, no attribution):
```bash
git add -A && git commit -m "fix(ci): <what failed and why>"
git push
```
After pushing, **go back to Step 1** (CI + review re-run).

- [ ] **Step 3: If CI is green, address the Claude review.**

Invoke the **`fix-review`** skill (fetches PR review comments, implements fixes, commits, pushes, and posts a summary comment). For each inline comment: either fix it, or — if you disagree after verification — reply on the thread explaining why (a reply is still required so no thread is left silently unresolved). Keep `just check` green before pushing.

Manual helpers — list the review comments and the top-level review bodies:
```bash
gh api "repos/{owner}/{repo}/pulls/$PR/comments" --paginate \
  -q '.[] | {id, path, line, body: (.body[0:160])}'
gh pr view "$PR" --json reviews -q '.reviews[] | {author: .author.login, state, body: (.body[0:200])}'
```

- [ ] **Step 4: Resolve every review thread.**

`fix-review` addresses the content; this step marks the threads **resolved** in the GitHub UI so the PR shows no outstanding conversations. List unresolved threads (GraphQL), then resolve each by node id.

List unresolved thread ids:
```bash
OWNER=$(gh repo view --json owner -q .owner.login); REPO=$(gh repo view --json name -q .name)
gh api graphql -f query='
  query($o:String!,$r:String!,$p:Int!){
    repository(owner:$o,name:$r){
      pullRequest(number:$p){
        reviewThreads(first:100){ nodes{ id isResolved isOutdated
          comments(first:1){ nodes{ path body } } } } } } }' \
  -F o="$OWNER" -F r="$REPO" -F p="$PR" \
  -q '.data.repository.pullRequest.reviewThreads.nodes[] | select(.isResolved==false) | {id, path: .comments.nodes[0].path}'
```
Resolve each returned `id` (only after the underlying issue is fixed or answered):
```bash
gh api graphql -f query='
  mutation($id:ID!){ resolveReviewThread(input:{threadId:$id}){ thread{ isResolved } } }' \
  -F id="<THREAD_NODE_ID>"
```

- [ ] **Step 5: Push accumulated fixes and re-enter the loop.**

If Steps 3–4 produced commits not yet pushed:
```bash
just check
git push
```
Pushing re-triggers CI **and** a fresh Claude review round. **Return to Step 1.** Do not exit the loop on the strength of one green pass — a new review round may add comments; a fix may introduce a new CI failure.

---

## Exit criteria (all must hold on the latest commit)

- [ ] `gh pr checks "$PR"` shows **`test` = pass** and **`lint` = pass** (and no other check failing) for the head commit.
- [ ] The GraphQL query in Task 2 / Step 4 returns **zero** unresolved review threads.
- [ ] The most recent Claude review round added no new inline comments that remain unaddressed (re-run Task 2 / Step 1 once more after the final push to confirm the review came back clean).
- [ ] `git status` on the branch is clean and everything is pushed (`git status -sb` shows `...origin/... [ahead 0]` or up to date).

- [ ] **Final step: Report completion.**

Post a short wrap-up comment on the PR (via the `ship` skill or `gh pr comment "$PR" --body "..."`) summarizing the green state — **no attribution line** — and hand back to the human for merge (do not self-merge unless explicitly instructed).
