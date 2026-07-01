# Autonomous P0 Run — Progress

Started: 2026-07-02 (autonomous unattended run)
Branch: claude/musing-williams-ec21ce

## Phase log
- **Phase 0 — Preflight**: branch `claude/musing-williams-ec21ce` confirmed, working tree clean, `gh auth status` OK (dmitrymomot, repo+workflow scopes). Baseline `just check` clean (0 lint issues, all tests pass). PROGRESS.md initialized. ✅
- **Phase 1 — Wave 1 complete**: all 5 leaf packages DONE (errorsx, sanitize, structfields, decimal, filetype), each verified by orchestrator (`just test ./<pkg>/... && just lint`). Wave 1 gate: full `just check` clean — every package green, 0 lint issues. ✅
- **Phase 2 — Wave 2 complete**: slug, money, validate all DONE and verified. Wave 2 gate: full `just check` clean; `go mod tidy` produces no diff (only module change in the run is slug's x/text indirect→direct promotion, committed). ✅
- **Phase 3 — Full-module gate**: `just check` clean, `go build ./...` OK, `go vet ./...` OK. All 8 package dirs present. **8/8 DONE, 0 BACKED OUT, 0 BLOCKED** → PR ships all 8. ✅
- **Phase 4 — Delivery**: PR #24 opened non-draft. First CI round: **test pass, lint pass**. Automated Claude review timed out after context-gathering (large PR: 855-line spec + 8 pkgs) — posted no inline comments / no threads / no formal review, so its literal exit criteria were already met.
- **Phase 4 — Extra correctness review** (orchestrator-run multi-agent adversarial review, since the automated one didn't substantively run): **9 CONFIRMED bugs found (0 uncertain, 0 refuted)**, all with concrete repros. Fixing all 9 via TDD before finalizing:
  - C1 errorsx (med): `Code()` misses code across `errors.Join` when a sibling has an empty-code wrapper.
  - C2 structfields (high): `Field.Set` panics on slice→array length mismatch (violates no-panic mandate).
  - C3 decimal (high): `mulInt64` misses `MinInt64 * -1` overflow → wrong result.
  - C4 decimal (high): `subInt64` guard `a > 0` should be `a >= 0` → `0 - MinInt64` wrong.
  - C5 slug (high): multi-rune separator truncation splits separator → oversized separator run mid-slug.
  - C6 slug (med): max-length truncation leaves trailing partial separator.
  - C7 slug (med): `WithCustomReplace` non-deterministic (ranges a map).
  - C8 validate (low): numeric `Between/Min/Max` accept `NaN`.
  - C9 validate (low): `bech32` doesn't enforce BIP-173 HRP ASCII range [33,126].

## Packages
| Package | Wave | Status | Last commit | Notes |
|---|---|---|---|---|
| errorsx | 1 | DONE | a86cf09 | 3 commits, 100% cov, lint clean, verified by orchestrator |
| sanitize | 1 | DONE | 1f19196 | 6 commits, 98.9% cov, lint clean, verified |
| structfields | 1 | DONE | 839931d | 5 commits, 97.1% cov, lint clean, verified; test fixture embed made exported (reflect contract) |
| decimal | 1 | DONE | ce19880 | 9 commits, 94.5% cov, lint clean, fuzz clean; fixed InEpsilon-on-zero test + removed dead isBig() |
| filetype | 1 | DONE | 9107ac0 | 3 commits, 96.8% cov, lint clean; empty-input→octet-stream guard added |
| slug | 2 | DONE | 9a157ed | 7 commits, 94.3% cov, lint clean, fuzz clean; x/text promoted to direct (only go.mod change in run); NFKD uppercase-leak fix |
| money | 2 | DONE | d0ed821 | 7 commits, 98.8% cov, lint clean, fuzz clean; built on decimal (unmodified), go.mod unchanged |
| validate | 2 | DONE | 1e78b3f | 13 commits, 98.0% cov, lint clean; zero-alloc test uses ASCII (Email allocs via net/mail); go.mod unchanged |

Status ∈ {PENDING, DONE, BACKED OUT, BLOCKED}.

## Delivery
- PR: #24 — https://github.com/dmitrymomot/forge/pull/24 (non-draft, base main)
- CI: <awaiting first run>
- Review threads: <awaiting Claude review>
- Deferred (not in this PR): none — all 8 packages green
- Loop iterations used: 0/10

## Deferred / follow-ups for the human
- none yet
