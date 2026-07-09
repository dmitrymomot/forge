# packages.md SaaS rework — design

Date: 2026-07-09. Status: approved pending user review.

## Goal

Rework `docs/packages.md` into the roadmap of a **fully-batteries-included
framework for SaaS apps** — forge provides ~99% of the boilerplate every SaaS
needs (tenancy, auth, background work, webhooks, authorization, audit,
rate limiting, idempotency, outbox, …) while keeping all the small bricks and
helpers. Business logic (billing rules, user-account flows, domain invariants)
stays in consumer repos.

Driving consumers (all must be openable as B2B SaaS): affiliate system for
igaming (CPA + revshare), anti-fraud system, agents platform
(agent/player network management), CRM for igaming.

## Deliverables

1. **`docs/packages.md`** — rewritten as a pure package catalog. Nothing else.
2. **`docs/design.md`** — new home for the evicted design prose (rules,
   philosophy, seams, anti-scope, recipes), updated for the decisions below.
3. **`CLAUDE.md`** — updated pointers; no duplication of design.md content.

## 1. New packages.md format

- Title + one-short-paragraph intro (what forge is, SaaS focus).
- Entries grouped under thin domain headers (`## core/`, `## auth/`, …) —
  zero prose between entries.
- Each entry:

  ```
  ---

  **domain/name** ✅

  Short description: purpose, functionality, approaches, drivers/adapters.
  ```

  `✅` = shipped; absence = planned. No other tiers.
- Package name written as import path (`auth/session`) so placement stays
  visible. Driver subpackages named inside the description
  (`jobqueue/postgres`, `jobqueue/nats`, …), not as separate entries.
- **Icebox is abolished.** Every former icebox entry is either promoted into
  the single committed list or dropped. Dropped: `namegen`, `term`.
  Everything else is promoted.
- Build order, tiers (core/recommended), placement-rationale table,
  shipped-package work items: **deleted** (not moved).

## 2. Content deltas (vs. current catalog)

### Renames / restructures

- **`msg/` → `async/`** — domain rename; holds `jobqueue`, `scheduler`,
  `eventbus`, `outbox`, `workflow`. (`resilience/` = in-request failure
  handling; `async/` = deferred execution — no conceptual collision.)
- **`auth/rbac` replaces the old single `rbac` scope** and gains siblings:
  authorization is three composable bricks, no `authz` naming anywhere:
  - **`auth/rbac`** — predefined roles, role nesting/inheritance
    (role inherits another role's permissions + adds own), out-of-hierarchy
    standalone roles, wildcard grants; resolves subject → effective
    permission set.
  - **`auth/acl`** — per-subject/per-resource grant/deny overrides
    (deny wins); the runtime-data layer; storage-agnostic Store + drivers.
  - **`auth/abac`** — attribute/relationship predicates as registered Go
    funcs (e.g. "agent sees own subtree but not subagents' player details" —
    tree traversal itself stays consumer code feeding the predicate).
  - All three feed one shared decision seam consumed by `guard` /
    `RequirePermission` middleware with the 401-vs-403 split.
  - Domain invariants ("owner can be only one") are consumer DB constraints —
    documented stance, not shipped code.
- **`crypto/jwtverify` (icebox, verify-only) → `auth/jwt`** — full sign +
  verify: pinned alg allowlist (never negotiated), exp/nbf/aud/iss checks,
  JWKS serve + fetch with kid cache/rotation, key rotation via
  `crypto/keyset`. No JWE, no alg negotiation. Lives in `auth/` by the
  placement tie-breaker (all consumers are auth packages: `oauthserver`
  issues, `oauthclient` verifies id_tokens, `guard` verifies bearers);
  `crypto/` keeps the primitives it composes (`sign`, `keyset`).

### New packages

- **`auth/oauthserver`** — machine-to-machine OAuth2 provider:
  client-credentials grant, token endpoint issuing short-lived JWTs via
  `auth/jwt`, JWKS endpoint, client registry behind a storage-agnostic
  Store. No auth-code-for-third-parties, no consent screens.
- **`async/outbox`** — transactional outbox: intent rows committed in the
  business DB transaction + a relay `supervisor.Service` that forwards
  committed rows into any broker. THE bridge from "my Postgres tx" to
  non-transactional brokers (nats/kafka/redis).

### Expanded scopes

- **`auth/apikey`** — from key codec to the full API-key product: Stripe-style
  prefixed keys with checksum, hash-at-rest, plaintext shown once (as before),
  **plus** storage-agnostic management — create/list/revoke/rotate, per-key
  scopes, optional expiry, last-used-at tracking — with keys owned by a
  tenant or a user, and request verification wired as a `guard.Verifier`
  (constant-time compare, key → identity/tenant resolution).
- **`data/tenant`** — full multi-tenancy package: `Resolver` chain with
  shipped resolvers (subdomain against a base domain, custom domain via a
  storage-agnostic `DomainLookup` seam, header, cookie, path prefix,
  API-key-derived), precedence-ordered middleware putting `TenantID` in
  context, transport-agnostic context carrier (jobs/workers set/read tenant
  without HTTP), plus the existing explicit `ScopeClause` SQL fragments.
- **`async/jobqueue` / `async/eventbus` / `async/scheduler`** —
  storage-agnostic engine:
  - `Broker` contract stays minimal (`Push/Claim/Ack/Nack` + optional
    capability discovery, e.g. native delay).
  - The **engine, not drivers, owns hard semantics** — retry/backoff, delayed
    jobs, max-attempts → dead-letter, idempotency inbox — so behavior is
    identical across drivers; engine uses native capabilities when declared.
  - Shipped drivers (isolated subpackages, each takes its client dep):
    **postgres, sqlite, redis, nats, kafka**. All committed roadmap.
  - Transactional publish (`PushTx(ctx, tx, …)` / publish in business tx) is
    native for SQL drivers; non-SQL drivers get it via `async/outbox`.
- **Storage-agnostic mandate** — every stateful package (rbac/acl stores,
  oauthserver stores, tenant lookups, auditlog sinks, quota, lock, session,
  …) defines its own small Store interface, ships an in-memory impl for
  tests, and puts every real backend in an isolated driver subpackage. Core
  never imports a driver client.

### Description-level updates (no scope change)

- `resilience/ratelimit` — state per-tenant/per-partner keying explicitly.
- `ops/auditlog` — state tenant-isolated queries explicitly.
- `comms/webhook` + `async/jobqueue` — partner postback story (signed
  delivery, retries, idempotency keys) called out.
- `web/idempotency` — unchanged scope, SaaS angle stated.

## 3. docs/design.md (new)

Moves from packages.md, trimmed and updated:

- Design DNA (idioms, anatomy, no-magic, options-not-builders, env prefixes,
  test doubles with seam owner, LOC band, black-box tests).
- Dependency philosophy (minimal-not-zero; updated isolated-deps list to
  include nats.go + kafka client + sqlite driver; "Postgres is THE database"
  softened to "Postgres is the primary database; async drivers are
  storage-agnostic").
- Repository layout & naming rules (two levels, leaf = package name, full
  words/standard acronyms, admission test, product-or-brick test).
- Framework-wide seams (byte-KV, counter, Broker — updated for `async/` and
  driver set; delivery-semantics rule `async/` vs `realtime/`; problem+json
  fleet contract; authorization decision seam added).
- Anti-scope list, **with these lines removed/rewritten**:
  - ~~"JWT issuing/signing … verify-only"~~ → gone (`crypto/jwt` signs).
  - ~~"OIDC provider — run Hydra/Keycloak"~~ → rewritten: full third-party
    OIDC provider with consent/auth-code stays out; M2M issuance is in
    (`auth/oauthserver`).
  - ~~"Cross-broker pub/sub packages (Kafka/NATS…) — consumer repo"~~ →
    gone (drivers ship in forge).
  - "Policy engines (casbin/OPA)" → kept, reworded: external engines plug in
    behind the decision seam; forge's own rbac/acl/abac cover native needs.
  - Billing/payments, usermanager, product analytics, etc. — kept (business
    logic stays out).
- Recipes owed (kept as-is, minus any that the new packages now cover).
- Build order and placement-rationale table are **not** moved — deleted.

## 4. CLAUDE.md update

Replace the packaging/design bullets (currently duplicating packages.md
content) with one pointer line: rules live in `docs/design.md`, catalog in
`docs/packages.md`. Keep workflow bullets (branch, just recipes, fmt, lint,
black-box tests, PR flow) untouched.

## Out of scope

- No code changes, no package implementation, no renames of shipped
  directories (`msg/` doesn't exist on disk yet — nothing to move).
- No re-litigation of shipped package APIs.
- Build-order planning (deliberately removed from the doc).
