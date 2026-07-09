# packages.md SaaS Rework Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rework `docs/packages.md` into a pure SaaS-focused package catalog, move the design rules into a new `docs/design.md`, and slim `CLAUDE.md` to pointers.

**Architecture:** Three markdown files, three tasks, one commit each. All target content is written verbatim in this plan — execution is: write file, run grep verifications, commit. Spec: `docs/superpowers/specs/2026-07-09-packages-md-saas-rework-design.md`.

**Tech Stack:** Markdown only. No Go code changes, no directory moves.

## Global Constraints

- The literal string `authz` must NEVER appear in any of the three files (not even in prose).
- The domain is `async/` — the string `msg/` must not appear in packages.md or design.md.
- No icebox, no tiers (core/recommended), no build order, no placement-rationale table in packages.md.
- packages.md contains ONLY: title, one intro paragraph, domain headers, and `---`-separated entries. Zero prose between entries.
- Entry format (exact): `---` blank line `**domain/name** ✅` blank line description paragraph. `✅` only on shipped packages.
- Exactly **143 entries** total, exactly **74** marked `✅`.
- Dropped forever (must not appear): `namegen`, `term`, `jwtverify`.
- Never add Claude attribution lines to commits (user's global rule).
- Verification commands run from repo root `/Users/dmitrymomot/Dev/forge`.

---

### Task 1: Create docs/design.md

**Files:**
- Create: `docs/design.md`

**Interfaces:**
- Consumes: nothing (content below is complete).
- Produces: `docs/design.md` — referenced by packages.md intro (Task 2) and CLAUDE.md pointer (Task 3). The file must exist before Task 2's intro link is valid.

- [ ] **Step 1: Write `docs/design.md` with exactly this content**

````markdown
# Forge — Design Rules

> The framework constitution: packaging rules, idioms, dependency policy,
> seams, and anti-scope. The package catalog & roadmap live in
> [packages.md](packages.md). Forge targets SaaS applications: it ships the
> ~99% of boilerplate every SaaS repeats (tenancy, authentication,
> authorization, background work, webhooks, audit, rate limiting,
> idempotency) as composable packages — business logic stays in consumer
> repos.

## Design DNA every package follows

- **No magic:** no reflection (one sanctioned helper, `structfields`), no
  service containers; values via params, not context (context only for
  request-scoped reads). Public methods never return unexported types.
- **One of three idioms:** stateless **free-funcs** (`render`, `htmx`) ·
  `New(...Option)` with an env-loadable `Config` + `DefaultConfig` +
  `Validate` (`logger`, `httpserver`) · a `supervisor.Service`.
- **Anatomy:** `doc.go` (runnable example) · `config.go` · `options.go`
  (`type Option func(*config)`, **never** builders) · `errors.go`
  (`errors.Is`-matchable single-line sentinels) · impl.
- **Minimal dependencies, not zero:** buy the wire, build the ergonomics;
  isolate every real dep behind a subpackage.
- **Storage-agnostic state:** every stateful package defines its own small
  `Store` interface, ships an in-memory implementation for tests/dev, and
  puts every real backend in an isolated driver subpackage. Core never
  imports a driver client.
- **Composition seams:** `http.Handler`, `supervisor.Service`,
  `middleware.Middleware`, `ctxkey.Key[T]`, `logger.ContextExtractor`, and
  pluggable `Store`/`Broker`/`Sender` interfaces.
- Single-responsibility packages (~250–850 LOC); black-box tests only.
- **Test doubles live with the seam owner** (`clock.Mock`, cache's memory
  store, jobqueue's in-memory broker) — there is no central fakes package.
- **Env prefixes are baked into tags:** every env-loadable `Config` carries
  its package prefix in the tag (`COOKIE_KEYS`, `SERVER_ADDR`, `DB_URL`).
  Nest untagged to keep default names; nest tagged (`env:"APP"`) to
  separate instances (`APP_COOKIE_KEYS`).

## Dependency philosophy — minimal, not zero

Forge optimizes for a small, auditable dependency surface — not stdlib purism:

- **Depend** on what speaks a wire protocol or is dangerous to hand-roll:
  `pgx`, official DB clients, broker clients, the S3 SDK, `x/crypto`.
- **Don't wrap** large, opinionated, fast-moving frameworks whose model would
  leak through forge's API (`watermill`, `stripe-go`, provider SDKs). Expose a
  small interface; the consumer takes the dep in _their_ repo.
- **Build or vendor** anything small that shapes forge's own API (`id`,
  `validate`, `request`, the job engine).
- **Always isolate** a real dependency in a driver subpackage
  (`logger/sentry`, `cache/redis`, `jobqueue/nats`) so it stays a swappable
  leaf.

Isolated deps today: `pgx`, goose (`migration`), the mongo/redis/opensearch
clients, sentry, `gopkg.in/yaml.v3` (`ops/config`'s YAML loader). Sanctioned
for the roadmap: aws-sdk-go-v2 (`objectstore/s3`), `coder/websocket`
(`websocket`), `go-webauthn` (`webauthn`), `x/crypto`
(password/kdf/autocert), goldmark (`email/markdown`), the official MCP
go-sdk (`mcpserver`), `x/image` (`imageproc`), prometheus client
(`metrics/prometheus`), OTel SDK (`tracing/otel`), goquery (`htmltest`,
test-only), `nats.go` (`jobqueue/nats`), a Kafka client (`jobqueue/kafka`),
a cgo-free SQLite driver (`jobqueue/sqlite`). **Postgres is the primary
database**; the async engine is storage-agnostic with first-class drivers
(postgres, sqlite, redis, nats, kafka); everything else outside `data/*`
and the driver leaves stays stdlib.

## Repository layout rules

- **Single Go module.** One `go.mod` at the root. No sub-modules, no
  `go.work`. (The tree is module-split-ready if dependency hygiene ever
  becomes a real consumer complaint — a domain folder can be promoted without
  moving files.)
- **Group by purpose, not by layer, tier, or build phase.** Folder names are
  domain nouns (`crypto`, `web`, `data`, `async`).
- **Two levels max** (`domain/package`); a third level only for driver
  isolators (`resilience/cache/redis`, `async/jobqueue/postgres`) and
  colocated codegen (`web/useragent/gen`).
- **Leaf directory = package name**, unique across all domains (no forced
  import aliasing). No packages at the repository root.
- **Names are full words or industry-standard acronyms** (`sse`, `csrf`,
  `totp`, `cli`, `llm`, `rbac`, `acl`, `abac`, `jwt` — the spec/protocol
  name IS the word). No ad-hoc abbreviations a reader must decode: `debug`
  not `diag`, `websocket` not `ws`, `requestlog` not `reqlog`. Compounds of
  full words are fine (`featureflag`, `objectstore`); one sanctioned
  exception: `imageproc`.
- **Admission test:** name the package's _purpose_ in one sentence — it must
  end with exactly one domain noun. If it plausibly fits two domains, the
  tie-breaker is who imports it (this is why `jwt` lives in `auth/`, not
  `crypto/` — all its consumers are auth packages).
- **Product-or-brick test:** every package is either a **product** — a
  complete feature that can be wired, configured, and start working on its
  own — or a **brick** — a primitive with two or more real consumers. A
  slice of a feature with a single consumer is never a package; it folds
  into its product as internal code. (This is why webhook signing lives
  inside `webhook`, not beside it.)

## Framework-wide seams

- **TTL-KV seam** — `resilience/cache.Store` (byte-level Get/Set-TTL/Delete
  + atomic SetNX claim) is THE key-value seam. Backends: memory (shipped),
  `cache/redis` (shipped). The memory store is LRU-evicting and unsuitable
  for sessions/idempotency, so those consumers need `cache/redis` (or bring
  a durable Store of their own). Consumers: `session`, `idempotency`, `otp`,
  `lockout`, server-side `flash`. No package defines a private byte-KV store.
- **Counter seam** — windowed atomic counters (Incr-within-window) can't ride
  Get/Set KV without races. `ratelimit` owns the counter `Store` contract;
  `quota` and `lockout` share it. Two store seams total — byte-KV + counter.
- **Broker seam** — `jobqueue.Broker` (`Push/Claim/Ack/Nack` + optional
  capability discovery, e.g. native delay) is the cross-backend messaging
  seam; `scheduler`, `eventbus`, and `outbox` ride it unchanged. The
  **engine, not the driver, owns the hard semantics** — retry/backoff,
  delayed jobs, max-attempts → dead-letter, idempotency inbox — so app
  behavior is identical across postgres/sqlite/redis/nats/kafka; the engine
  uses a driver's native capability when declared. Transactional publish
  (`PushTx`) is native on SQL drivers; non-SQL drivers get it via
  `async/outbox`.
- **Authorization decision seam** — `rbac`, `acl`, and `abac` are composable
  bricks feeding one shared Allow/Deny decision interface consumed by
  `guard` / `RequirePermission` middleware (401-vs-403 split). External
  policy engines plug in behind the same interface. Domain invariants
  ("owner can be only one") are consumer DB constraints, never forge rules.
- **Delivery-semantics rule** — `async/` = durable, at-least-once, survives
  restarts (competing consumers). `realtime/` = ephemeral, at-most-once
  fan-out to currently-connected clients.
- **Fleet error contract** — problem+json is the RPC error contract between
  services: `problem` carries a machine-readable `Code`, decodes responses,
  and matches via `errors.Is`; `httpclient` surfaces decoded Problems.

## Anti-scope — what stays in consumer repos

- **gRPC transport** — consumers take google.golang.org/grpc directly; a
  `*grpc.Server` slots in via a ~30-LOC `supervisor.Service` adapter (recipe,
  never a wrapper). Transport-agnostic endpoint/middleware layers are
  rejected as `interface{}` ceremony.
- **Service discovery & client-side load balancing** — the platform's job
  (K8s/DNS/cloud LB/mesh); httpclient targets a stable URL; health provides
  the registration signals.
- **API codegen / IDL toolchains** — buf/oapi-codegen/sqlc live in the
  consumer repo; generated handlers compose at the `http.Handler` seam.
- **Remote config stores** (etcd/consul/nacos) — client SDKs plug in behind
  configwatch's load func / secretsource's Provider.
- **Billing / payments abstraction** — provider-coupled business logic; use
  stripe-go directly + forge webhook/httpclient/quota/money.
- **User-account lifecycle (usermanager)** — consumer domain assembled from
  forge primitives; recipe in examples/, never a package.
- **Third-party OIDC provider** — issuing tokens to *end users of other
  companies' apps* (auth-code for third parties, consent screens, JWE) is a
  product; run Hydra/Keycloak. Machine-to-machine issuance is in scope via
  `auth/oauthserver`; `auth/jwt` signs and verifies with pinned algs — alg
  negotiation and JWE stay out forever.
- **Vault / secrets-manager clients** — supplied behind secretsource's
  Provider; key material arrives as env via keyset.
- **Rich-HTML/UGC sanitization** — needs bluemonday; templ/render escaping +
  `sanitize` cover forge's side.
- **Full-text-search abstraction** — `data/opensearch` is a connection
  factory only; the non-goal is a query/index abstraction. Default answer:
  Postgres tsvector (example owed).
- **Vector store / ANN** — consumer data layer; `embeddings` covers
  brute-force in-memory similarity.
- **Agent runtime / prompt registry** — application logic; llm's tool-call
  DTOs + structured + prompt are the primitives.
- **Product analytics / event tracking** — PostHog/Segment or an eventbus
  subscription into the consumer's warehouse.
- **In-app notification inbox** — consumer domain; recipe: Postgres insert →
  fanout → sse.
- **SEO artifacts** (sitemap/robots/meta) — consumer templates over
  render/assets.
- **Read/write replica routing** — infra (pgbouncer/RDS proxy).
- **External policy engines** (casbin/OPA) — plug in behind the
  authorization decision seam; forge's own `rbac`/`acl`/`abac` cover native
  needs.
- **Long-poll transport** — SSE reconnect covers it; hand-roll over fanout if
  truly needed.
- **Sync command bus** — a command with one handler is a function call.
  Async commands are a jobqueue kind with one registered handler.
- **Logger adapters** (zap/zerolog bridges) — forge is slog-only.
- **Scaffolding / code generators** — value lives in consumer-owned
  templates.
- **Testcontainers** — CI services/local Postgres suffice.
- **Error-monad Result/Option types** — Go's multi-return idiom; ptr/null/
  errorsx cover the real needs.
- **Shared service base** — deferred until 2–3 services show real
  duplication; each service mirrors New+Config+Validate independently.

## Recipes owed (capabilities deliberately not packaged)

examples/ or doc.go entries committed so dropped capabilities aren't silently
lost: grpc-server-as-supervisor.Service · reverse proxy (hostrouter +
httputil.ReverseProxy) · chain-wide body cap (http.MaxBytesHandler + problem
413) · breadcrumb value type · notification-inbox pipeline · per-tenant
encryption / GDPR crypto-shred (kdf) · optimistic locking + audit columns
(postgres docs) · Postgres tsvector search · usermanager flow · templ
`Classes` toggle helper.
````

- [ ] **Step 2: Verify**

Run:
```bash
grep -c 'authz' docs/design.md; grep -c 'msg/' docs/design.md; grep -c 'jwtverify' docs/design.md; grep -c 'Hydra' docs/design.md
```
Expected: `0`, `0`, `0`, `1` (Hydra appears once, inside the rewritten OIDC-provider line). Note: `grep -c` exits 1 on zero matches — that is the expected success signal for the first three.

Run:
```bash
grep -n 'JWT issuing' docs/design.md; grep -n 'Cross-broker' docs/design.md
```
Expected: no output from both (those anti-scope lines are gone).

- [ ] **Step 3: Commit**

```bash
git add docs/design.md
git commit -m "Add design.md: framework design rules evicted from packages.md"
```

---

### Task 2: Rewrite docs/packages.md as the pure catalog

**Files:**
- Modify: `docs/packages.md` (full replacement)

**Interfaces:**
- Consumes: `docs/design.md` existing (Task 1) — the intro links to it.
- Produces: the catalog. 143 entries, 74 `✅`.

- [ ] **Step 1: Replace `docs/packages.md` with exactly this content**

````markdown
# Forge — Package Catalog

Forge is a batteries-included Go framework for SaaS applications: it ships
the ~99% of boilerplate every SaaS repeats — tenancy, authentication,
authorization, background jobs, webhooks/postbacks, audit logging, rate
limiting, idempotency — as small composable packages, alongside all the
low-level bricks and helpers. This file is the complete committed roadmap:
every entry below will exist; `✅` marks shipped packages (their `doc.go` is
the API reference). All design rules — layout, naming, idioms, dependencies,
seams, anti-scope — live in [design.md](design.md).

## core/

---

**core/clock** ✅

Time source abstraction (now, timers, tickers) with a controllable mock —
the seam every time-dependent package injects for tests.

---

**core/random** ✅

Crypto/rand-backed random strings, byte slices, and numeric codes
(`String`, `DigitCode`) for tokens, verification codes, and IDs.

---

**core/id** ✅

Unique identifier generation with type prefixes (Stripe-style `usr_…`) for
API-facing IDs.

---

**core/ctxkey** ✅

Typed context keys (`ctxkey.Key[T]`) — the one sanctioned way to carry
request-scoped values.

---

**core/typeconv** ✅

String ↔ typed-value conversions shared by env/config loading and form
decoding.

---

**core/structfields** ✅

The one sanctioned reflection helper: struct-tag field enumeration and
setting, used by `config`, `form`, and friends. No other package touches
reflect.

---

**core/slicex** ✅

Generic slice helpers beyond the stdlib (transform, filter, dedupe, chunk).

---

**core/mapx** ✅

Generic map helpers (keys, values, merge, invert).

---

**core/set** ✅

Generic set type with the standard operations.

---

**core/ptr** ✅

Pointer helpers for optional values in struct literals and API payloads.

---

**core/null** ✅

Nullable scalar types that round-trip JSON and SQL cleanly.

---

**core/enum** ✅

String-backed enum helper with validation and JSON/SQL round-trips.

---

**core/stringsx** ✅

String helpers beyond stdlib `strings`.

---

**core/errorsx** ✅

Error utilities complementing stdlib `errors` (single-line formatting,
matching helpers).

---

**core/iox** ✅

I/O helpers (limited readers, counting writers) beyond stdlib `io`.

---

**core/bufpool** ✅

Pooled byte buffers for allocation-sensitive hot paths.

---

**core/encoding** ✅

Encoding conveniences (base64/hex/JSON helpers) shared across packages.

---

**core/bytesize** ✅

Human-readable byte sizes: parse and format `"10MB"`-style values (config
limits, upload caps).

---

**core/filetype** ✅

Magic-byte MIME/file-type detection for upload validation — never trust the
declared Content-Type.

---

**core/validate** ✅

Struct and value validation producing i18n-keyed violations (translated by
`i18n/catalog`); the validation layer under `request` and `form`.

---

**core/sanitize** ✅

Input normalization and sanitization (trim, collapse whitespace, strip
control characters) for untrusted text.

---

**core/slug** ✅

URL-safe slugs from arbitrary strings (workspace names, vanity URLs).

---

**core/decimal** ✅

Arbitrary-precision decimal arithmetic with SQL Scan/Value and JSON
round-trips — the numeric base for money, revshare percentages, and rates.

---

**core/money** ✅

Currency-safe money type over `decimal` (arithmetic, allocation,
comparison); renders locale-free by design — locale formatting lives in
`i18n/numbers`.

---

**core/qrcode**

QR code generation to PNG / base64 data-URI from any string (vendored
encoder, no deps): 2FA enrollment URIs, referral/share links.

## crypto/

---

**crypto/consttime** ✅

Constant-time comparison helpers for secrets, tokens, and API keys.

---

**crypto/digest** ✅

Hashing helpers with canonical encodings — checksums, fingerprints,
content-addressing.

---

**crypto/sign** ✅

HMAC signing and verification primitive under signed cookies, cursors, and
webhook signatures.

---

**crypto/secret** ✅

Authenticated encryption (`secret.Box`) for values at rest (OAuth tokens,
PII columns).

---

**crypto/keyset** ✅

Versioned key rings with rotation; feeds `sign`, `secret`, and `auth/jwt`
so key rollover never breaks verification.

---

**crypto/kdf** ✅

Key derivation (HKDF): compound per-tenant keys from a master key — the
GDPR crypto-shred story (delete tenant key = data unreadable).

---

**crypto/password** ✅

Memory-hard password hashing and verification over `x/crypto`, with
tunable-cost upgrade-on-verify.

---

**crypto/token** ✅

Generic signed, TTL'd token codec with typed claims — the primitive under
`magiclink` and short-lived internal tokens.

---

**crypto/redact** ✅

Wrapper types whose `String`/`LogValue` output redacts the secret — safe to
pass around config and logs.

## resilience/

---

**resilience/backoff** ✅

Jittered exponential backoff schedules (with ceiling) consumed by retry,
httpclient, and the job engine.

---

**resilience/retry** ✅

Context-aware retry loops over `backoff`, honoring typed `RetryAfter`
errors with a floor.

---

**resilience/singleflight** ✅

Duplicate-call suppression (panic-safe) so concurrent identical loads
collapse into one.

---

**resilience/parallel** ✅

Bounded-concurrency fan-out helpers for parallel work with error
collection.

---

**resilience/circuitbreaker** ✅

Circuit breaker with per-key `Group`, `RetryAfter` surfacing, panic-safe
`Do`, and net/http middleware.

---

**resilience/cache** ✅

THE byte-KV TTL seam: `cache.Store` (Get / Set-with-TTL / Delete / atomic
SetNX claim) with an LRU-evicting memory store built in and the
`cache/redis` driver ✅. Consumers: session, idempotency, otp, lockout,
flash. No package defines a private KV store.

---

**resilience/ratelimit** ✅

Keyed rate limiting — per tenant, per partner, per API key, per IP — over
the windowed counter `Store` seam it owns; HTTP middleware with standard
RateLimit headers; `ratelimit/redisstore` driver ✅.

---

**resilience/quota**

Cumulative usage caps per subject over calendar/rolling windows
(requests/month, seats, AI tokens) — the plan-entitlement counterpart to
ratelimit. Subject is an opaque string; the limit resolver is caller-owned
(no billing coupling). Storage-agnostic counter store; Postgres driver.

---

**resilience/loadshed**

Adaptive admission control: pluggable `Criteria` (concurrency + latency in
core; CPU stays consumer-side), fail-open sampler, probabilistic rejection
ramp. `Middleware()` for HTTP plus `Acquire()` for non-HTTP admission.

---

**resilience/lock**

Distributed mutex: TTL leases, fencing tokens, auto-refresh, and
`RunAsLeader` as a `supervisor.Service`. Storage-agnostic 3-method Store;
in-process store built in, Postgres advisory-lock driver.

## web/

---

**web/httpserver** ✅

Production `*http.Server` wrapper: sane timeouts, TLS wiring, graceful
shutdown, run as a `supervisor.Service`.

---

**web/hostrouter** ✅

Host-based routing — apex vs subdomains vs customer custom domains — the
front door of a multi-tenant SaaS.

---

**web/middleware** ✅

The `middleware.Middleware` type with composition helpers (`Chain`,
conditional `When`/`Skip`).

---

**web/request** ✅

Typed request reads: path/query/header values, JSON body decode, pagination
params, and upload validation (`ValidateFile`/`Accept`).

---

**web/clientip** ✅

Real client IP extraction behind proxies and CDNs, trusted-proxy aware.

---

**web/problem** ✅

RFC 9457 problem+json responses with a machine-readable `Code`,
`errors.Is` matching, and response decoding — the error contract between
fleet services and to API consumers.

---

**web/recoverer** ✅

Panic-recovery middleware: logs the stack, responds problem+json 500.

---

**web/requestlog** ✅

Structured slog request logging middleware.

---

**web/requestid** ✅

Request-ID generation/propagation middleware with a logger context
extractor.

---

**web/render** ✅

Response rendering free-funcs (JSON, HTML templates, redirects) over
`http.ResponseWriter`.

---

**web/htmx** ✅

HTMX request detection and response headers (triggers, swaps, fragments);
gains the SSE `SendComponent` bridge when `realtime/sse` ships.

---

**web/subroute** ✅

Mount sub-routers under a path prefix, router-agnostic.

---

**web/httpclient** ✅

Resilient outbound `*http.Client` via a RoundTripper stack: per-attempt
timeout, jittered retry, opt-in per-host circuit breaker, before/after
hooks, ctx-driven header propagation; returns the stdlib type. The
transport under captcha, oauthclient, comms/*, and llm.

---

**web/cookie** ✅

Signed + encrypted cookie codec — the primitive `csrf`, `flash`, and
`session` compose.

---

**web/csrf** ✅

CSRF protection middleware over `cookie` for browser-facing forms.

---

**web/secheaders** ✅

Security-headers middleware (CSP, HSTS, frame/content-type options).

---

**web/cors** ✅

CORS middleware with explicit allowlist configuration.

---

**web/timeout** ✅

Per-route request-timeout middleware responding problem+json 504.

---

**web/compress** ✅

Response compression middleware.

---

**web/useragent** ✅

User-Agent + UA Client Hints parser producing structured browser/OS/device
/bot facts (canonical names, marketing versions, categorized bot taxonomy)
— session device lists, audit display lines, anti-fraud signals. Includes
the `useragent/gen` codegen subpackage ✅ that produces the bot table.

---

**web/assets**

Static file serving over `fs.FS`: correct content types, range requests,
ETag/304, cache headers; fingerprint-manifest mode (`URL()`/`Integrity()`,
immutable far-future headers) and SPA fallback. Not a bundler.

---

**web/idempotency**

Idempotency-Key middleware for partner-facing APIs: replays the stored
first response on retry, rejects key reuse with a different payload
fingerprint. Rides `cache.Store`'s atomic SetNX.

---

**web/ipfilter**

IP/CIDR allow/deny middleware over `clientip` — admin allowlists, partner
IP pinning, blocklists.

---

**web/captcha**

Server-side CAPTCHA verification behind a `Verifier` seam; providers
(`captcha/turnstile`, …) are thin POST+JSON adapters over httpclient — no
provider SDKs.

---

**web/autocert**

ACME/Let's Encrypt TLS via `x/crypto/acme/autocert` wired as `tls.Config`
+ HTTP-01 handler for httpserver — pairs with `tenant`'s custom-domain
resolution for customer domains.

---

**web/geoip**

IP → country/region/ASN lookup behind a `Source` seam; CDN-header source
(CF-IPCountry) built in stdlib-only; `geoip/maxmind` driver.

---

**web/dnsverify**

DNS TXT-record domain-ownership verification behind a `net.Resolver` seam:
custom-domain onboarding (with hostrouter + autocert) and email-sending
domain checks (SPF/DKIM/return-path) for `comms/email`.

## view/

---

**view/flash**

One-shot messages surviving a redirect over a pluggable Store:
signed-cookie store built in (composes `cookie`); server-side store rides
`cache.Store`. For PRG and htmx flows.

---

**view/form**

Whole-form decode into structs (reflection confined to `structfields`),
sticky `Values`, render-friendly `Errors` carrying `validate`'s i18n keys
(translated by `i18n/catalog`), plus error-class/aria helpers. Backbone of
server-rendered CRUD.

## i18n/

---

**i18n/catalog**

Message catalog with fs.FS/JSON loaders and curated CLDR plural rules;
`T(loc, key, args...)` selects plural forms internally; fallback chains;
translates `validate` violation keys. Zero deps — no x/text, no YAML.

---

**i18n/locale**

Accept-Language negotiation (q-values, region fallback), context carrier,
middleware with a resolver chain (cookie → query → Accept-Language →
default), and a logger context extractor.

---

**i18n/numbers**

Locale-aware number/currency/percent formatting; `Currency(money.Money)` —
the locale rendering `core/money` defers by design.

---

**i18n/dates**

Locale + timezone date/time and relative-time ("3 hours ago") formatting
with named presets. Gregorian only.

## data/

---

**data/postgres** ✅

pgx pool factory plus the transaction story: `WithTx`/`WithTxRetry` and
context Querier helpers. Rows→struct mapping stays pgx
`CollectRows`/sqlc — no forge wrapper.

---

**data/migration** ✅

Goose-based migrations from an embedded `fs.FS`.

---

**data/mongo** ✅

MongoDB client/connection factory (config + lifecycle only).

---

**data/redis** ✅

Redis client/connection factory (config + lifecycle only) — the client
other redis drivers accept.

---

**data/opensearch** ✅

OpenSearch client/connection factory (config + lifecycle only); no
query/index abstraction.

---

**data/pagination**

Opaque cursor codec (base64+JSON, optional HMAC via `sign`), keyset
WHERE/ORDER fragment builders emitting pgx-compatible `(sql, args)`,
`Page[T]` metadata, and the page-window view-model (ellipses, links
preserving query params) for server-rendered navigation.

---

**data/tenant**

The multi-tenancy package: `Resolver` chain with shipped resolvers —
subdomain (against a base domain), custom domain (via a storage-agnostic
`DomainLookup` seam), header, cookie, path prefix, API-key-derived —
precedence-ordered middleware putting `TenantID` in context, a
transport-agnostic carrier (jobqueue handlers set/read tenant without
HTTP), and explicit parameterized `ScopeClause` SQL fragments — visible at
every query, never auto-injected.

---

**data/dataloader**

Generic per-request batch-and-cache loader collapsing N+1 lookups; pure
generics, no DB imports; the batch fn is caller-owned.

---

**data/objectstore**

Blob `Store` seam with a path-traversal-safe disk adapter;
`objectstore/s3` driver on aws-sdk-go-v2. Magic-byte MIME validation via
`filetype`, tenant key-prefix scoping, presigned URLs.

---

**data/seed**

Idempotent named-Seeder runner with an `app seed` cli.Command; mirrors
`migration`'s shape.

---

**data/imageproc**

Decode-limit-guarded resize/crop/re-encode over `x/image` — the
avatar/logo upload → process → store pipeline.

## async/

---

**async/jobqueue**

THE durable background-work engine: supervised worker pool (bounded
concurrency, per-job retry/backoff, graceful drain), claim-with-lease
at-least-once delivery, max-attempts → dead-letter, typed handlers over
JSON, producer `Client` separate from the worker `Service`, delayed jobs.
Storage-agnostic `Broker` seam (`Push/Claim/Ack/Nack` + capability
discovery, e.g. native delay); the engine — not the driver — owns
retry/delay/dead-letter semantics so behavior is identical across
backends. In-memory broker built in; drivers: `jobqueue/postgres` (SKIP
LOCKED + LISTEN/NOTIFY), `jobqueue/sqlite` (zero-infra single-node and
dev/test), `jobqueue/redis`, `jobqueue/nats`, `jobqueue/kafka`. SQL
drivers support transactional enqueue (`PushTx(ctx, tx, …)`); non-SQL
brokers get it via `async/outbox`.

---

**async/scheduler**

Cron/interval `supervisor.Service` that *enqueues* into the engine when
due; fires once per fleet via a `unique(name, scheduled_for)` insert race
on SQL drivers; small local cron parser, no robfig/cron.

---

**async/eventbus**

Typed events over the same `Broker` drivers, two modes: sync in-process
observer (no durability) and durable mode — each named subscription is its
own queue, publish fans out one message per subscription, competing
consumers within one, at-least-once. Transactional publish on SQL drivers;
exports the `Seen(ctx, tx, id)` idempotency inbox. Handlers must be
idempotent.

---

**async/outbox**

Transactional outbox: intent rows committed inside the business DB
transaction plus a relay `supervisor.Service` that forwards committed rows
into any `Broker` — the bridge from a Postgres/SQLite transaction to
redis/nats/kafka delivery.

---

**async/workflow**

DB-checkpointed linear step sequences over the engine
(onboarding/provisioning chains; resume after crash). No DAG, no DSL, no
timers — not a Temporal clone.

## realtime/

---

**realtime/sse**

The complete Server-Sent Events package, stdlib-only: typed event
constructors (string/JSON/comment/retry), correct framing + headers,
per-event flush, keep-alive, ctx cancellation — plus the mountable
endpoint over `fanout`: per-request subscribe, heartbeat, disconnect
handling, Last-Event-ID resume via the replay ring. The low-level writer
stays exported as the brick under `web/htmx`'s SendComponent bridge and
`llm.SSE`. Requires httpserver WriteTimeout=0 (documented).

---

**realtime/fanout**

In-process pub/sub hub (bounded buffers, explicit slow-consumer policy)
with the `Bus` seam for multi-instance backplanes and an optional bounded
`WithReplay(n)` ring. Drivers: `fanout/pgbus` (LISTEN/NOTIFY — multi-
instance push with zero new infrastructure), `fanout/redisbus` (takes the
caller's `data/redis` client).

---

**realtime/websocket**

The complete WebSocket package: accept/read/write/ping-pong/close over
isolated `coder/websocket` behind exported forge `Conn`/`Message`, plus
the hub — connection registry, rooms, broadcast under supervisor, with
production bounds (payload/event-name/auth-blob limits,
drop-frames-vs-teardown overflow policy, `Shutdown(ctx)` drain).

---

**realtime/presence**

Who-is-here tracking with TTL heartbeats over `fanout` + `cache` — online
indicators, concurrent-viewer counts.

## auth/

---

**auth/session**

Server-side session lifecycle (Start/Load/Save/Destroy/Rotate) over a
pluggable Store; rotate-on-privilege-change; multi-device management via
an optional `UserIndex` store extension (ListByUser/DeleteByUser — "log
out other devices", GDPR deletion); `WithFingerprint(Warn|Strict)` hijack
detection. In-memory store built in; drivers: `session/pgstore`
(user-indexed), `session/cookiestore` (stateless-encrypted, no UserIndex —
documented); generic KV backing rides `cache.Store`.

---

**auth/guard**

Request-authentication middleware over a `Verifier` seam (session, jwt,
apikey all satisfy it) with chained credential extractors (header → cookie
→ query), built-in Basic Auth (constant-time, correct 401 +
WWW-Authenticate — gates pprof/metrics/staging/admin), and
`IdentityFromContext`.

---

**auth/jwt**

Full JWT: sign and verify with a pinned alg allowlist (RS256/ES256/EdDSA —
never negotiated), exp/nbf/aud/iss checks, JWKS serve + fetch with kid
cache/rotation, key rotation via `crypto/keyset`. No JWE. Consumed by
`oauthserver` (issuing), `oauthclient` (id_token verify), `guard` (bearer
verify), and inter-service auth.

---

**auth/lockout**

Login/OTP failure counting with exponential delay and lockout windows over
the ratelimit counter seam. (Not rate shaping — that's `ratelimit`; not
cumulative caps — that's `quota`.)

---

**auth/totp**

The complete 2FA package: RFC 6238/4226 TOTP/HOTP secret generation,
skew-window verify, otpauth:// provisioning URI, and one-time backup codes
(generate/hash/verify-and-consume, constant-time). QR image rendering
lights up via `core/qrcode`. Persistence is consumer DB.

---

**auth/otp**

Short numeric codes for email/SMS verification: attempt-limited, TTL'd,
hashed at rest; generation via `random.DigitCode`; delivery is the
caller's channel.

---

**auth/apikey**

The full API-key product for tenant- or user-owned keys: Stripe-style
prefixed keys (`sk_live_…`) with checksum for cheap rejection, hash stored,
plaintext shown once — plus management (create/list/revoke/rotate, per-key
scopes, optional expiry, last-used-at tracking) behind a storage-agnostic
Store, and request verification as a `guard.Verifier` (constant-time, key →
identity/tenant resolution).

---

**auth/magiclink**

Signed, TTL'd, single-use links over `crypto/token`: passwordless login,
team invites (role/tenant claims as a documented example), verify and
unsubscribe links. Stateless by default; `WithStore` for single-use
redemption. Does not send email.

---

**auth/oauthclient**

OAuth2/OIDC client: auth-code + PKCE, state, token exchange,
id_token/userinfo verification via `auth/jwt` (alg-pinned), provider
presets. On net/http over `httpclient` — no x/oauth2.

---

**auth/oauthserver**

Machine-to-machine OAuth2 provider for partner-facing APIs:
client-credentials grant, token endpoint issuing short-lived JWTs via
`auth/jwt`, JWKS endpoint, client registry behind a storage-agnostic
Store. No auth-code-for-third-parties, no consent screens, no JWE.

---

**auth/rbac**

Role-based access control: predefined roles, role nesting/inheritance (a
role inherits another role's permissions and adds its own),
out-of-hierarchy standalone roles, wildcard grants; resolves subject →
effective permission set. Feeds the shared authorization decision seam
consumed by `guard`/`RequirePermission` (401-vs-403 split). Subject→role
assignment behind a storage-agnostic Store.

---

**auth/acl**

Per-subject / per-resource grant and deny overrides (deny wins) layered
onto rbac decisions — "this manager sees exactly these assigned agents".
The runtime-data authorization layer: storage-agnostic Store with drivers;
composes into the same decision seam.

---

**auth/abac**

Attribute/relationship predicates as registered Go functions — "agent sees
own subtree but not subagents' player details" — evaluated in the shared
decision seam alongside rbac/acl. The relationship data (trees,
assignments) stays consumer code feeding the predicate; no policy DSL.

---

**auth/webauthn**

Passkey registration and assertion over isolated `go-webauthn` (CBOR/COSE
is the one justified heavy auth dep).

---

**auth/fingerprint**

Versioned request fingerprint (UA + Accept headers ± IP, sha256).
Multi-consumer brick: session hijack detection, anti-fraud risk scoring.

## comms/

---

**comms/email**

The complete email package: `Sender` seam + stdlib net/smtp implementation
(STARTTLS, multipart, attachments) + named-template rendering (subject +
HTML + text into a `Message`). `email/markdown` renders markdown + YAML
frontmatter (subject/preheader, CTA-button extension) — the designer-free
transactional format; goldmark confined there. Provider adapters
(SES/Postmark/…) are consumer-side or isolated subpackages.

---

**comms/webhook**

The complete webhooks/postbacks package, both directions: outbound
HMAC-signed deliveries (Stripe-style `t=,v1=`) with timeout, bounded
retry, and idempotency keys — durable delivery rides `async/jobqueue` —
and inbound signature-verification middleware (Stripe/GitHub/Slack HMAC
schemes, constant-time, timestamp tolerance, reads and restores
`r.Body`). Signing and verifying share one internal scheme implementation.

---

**comms/sms**

SMS `Sender` seam; `sms/twilio` driver is a stdlib form-POST over
httpclient — never twilio-go.

---

**comms/push**

Push `Pusher` seam + `push/webpush` (VAPID/ECDH/AES-GCM, fully stdlib).
FCM/APNs stay consumer-side behind the seam.

## ai/

---

**ai/llm**

Provider-agnostic chat/completion + streaming behind
`Completer`/`Streamer` with plain DTOs including tool-calling
(`ToolDef`/`ToolCall`/`ToolResult`); typed error contract (provider
status, stable reason, `RetryAfter()`) honored by retry/httpclient;
`llm.SSE(w, r, chunks)` response bridge; `EstimateTokens`/`Fit` budget
truncation; exported usage type (prices are consumer-supplied — never
library data). Drivers `llm/openai`, `llm/anthropic` are stdlib JSON+SSE
adapters over httpclient — never the official SDKs.

---

**ai/prompt**

Type-safe prompt templating from an fs.FS registry over text/template
with strict missing-key errors. Mechanical only — no chains/agents. Not
html/template (escaping corrupts prompts).

---

**ai/structured**

Coerce noisy LLM output into typed values: fence-strip, strict JSON decode
into T, repair-prompt on failure.

---

**ai/embeddings**

`Embedder` seam + stdlib vector math (cosine, top-k) for small in-memory
corpora; no ANN, no persistence.

---

**ai/mcpserver**

Expose explicitly-registered app operations as MCP tools (stdio +
streamable-HTTP) as `supervisor.Service` / `http.Handler`; hand-declared
schemas, no reflection; official MCP go-sdk isolated here.

## ops/

---

**ops/supervisor** ✅

Service lifecycle runner: ordered startup, signal handling, graceful
shutdown with force-quit escape hatch, pre-shutdown hooks, context
rooting — the seam every long-running forge component implements.

---

**ops/logger** ✅

slog configuration (JSON/text handlers, levels) with the
`ContextExtractor` seam (request ID, tenant, locale flow into every line)
and a test `Recorder`; `logger/sentry` driver ✅.

---

**ops/config** ✅

App config from YAML per-env files with `${VAR:default}` substitution,
`.env` inheritance, and env-tagged structs via `structfields` +
`typeconv`; exported `Profile` enum (dev/test/staging/prod) with
predicates; boot-time only.

---

**ops/health** ✅

Liveness (no checks) and readiness (`WithCheck`, critical by default,
`NonCritical` degrades instead of 503) `Handler()` factory, plus a `Gate`
for ordered pre-shutdown drain via `supervisor.WithPreShutdown`.

---

**ops/buildinfo** ✅

Value type merging ldflags over `ReadBuildInfo` for
version/commit/build-time/dirty; `fmt.Stringer` + `slog.LogValuer` +
`/version` JSON handler.

---

**ops/automaxprocs** ✅

Sets GOMAXPROCS/GOMEMLIMIT from cgroup CPU/memory quotas at startup via a
local stdlib parser (cgroup v2 primary, v1 fallback, no uber dep);
fail-open logged no-op outside containers; honors explicit env vars.

---

**ops/logredact** ✅

`slog.Handler` wrapper replacing attribute values by leaf key or dotted
group path before they reach the next handler; unconditional at every
level; redacts `WithAttrs`-baked attrs and tracks the `WithGroup` prefix.
`crypto/redact` covers values you wrap — this is the safety net for attrs
you don't control.

---

**ops/bootstrap** ✅

Thin runtime integrator: `Run` / generic `RunWithConfig[T]` wire logger →
automaxprocs → build-info log → config autoload → signal context → exit
code; the callback owns `supervisor.Run` and `defer` cleanup. NOT a DI
container.

---

**ops/featureflag** ✅

Standalone flags as serializable records (enabled → deny → allow → rollout
pipeline, token-set targeting, FNV subject bucketing): typed getters with
defaults, YAML/options/memory sources behind a one-method `Provider` seam
(ctx-scoped for multi-tenancy), scope-aware `Cached` decorator
(singleflight, serve-stale). Postgres provider is a doc.go recipe.

---

**ops/debug**

One internal diagnostics surface: `/debug/pprof/*`, `/debug/stats`
(runtime/GC/goroutines JSON), `/debug/vars`, with an auth guard and a
dedicated-port `supervisor.Service`.

---

**ops/metrics**

Counter/Gauge/Histogram `Recorder` facade with an expvar default and
request middleware; `metrics/prometheus` is the only adapter.

---

**ops/auditlog**

Append-only structured audit events (actor/action/resource/outcome) over a
`Sink` seam; slog + JSONL sinks built in; `auditlog/pgsink` adds the
insert plus tenant-isolated, keyset-paginated queries — the audit trail
every B2B SaaS shows in its UI.

---

**ops/cli**

Struct-described command tree over stdlib `flag.FlagSet`, ctx-aware Run,
auto help; no cobra, no global registry. Covers
serve/migrate/worker/seed/version.

---

**ops/tracing**

`Tracer` seam, W3C traceparent middleware, trace_id log extractor;
`tracing/otel` driver isolated. Pairs with httpclient propagation.

---

**ops/secretsource**

Hot-reloadable secrets behind a `Provider` seam, exposing `redact.Secret`
values; cloud clients stay consumer-side.

---

**ops/logsample**

`slog.Handler` that rate-samples high-volume records.

---

**ops/configwatch**

Poll-based config reload with atomic snapshot swap, as a
`supervisor.Service`.

## testkit/

---

**testkit/webtest**

Black-box HTTP harness: real `:0` server from an `http.Handler`, fluent
request builder, `testing.TB` response asserts.

---

**testkit/htmltest**

CSS-selector DOM assertions over any HTML source (`io.Reader`/string) —
webtest responses and `email/markdown` bodies are the named consumers
(text/attr/count/exists, form-field values, htmx fragments); goquery as a
test-only dep, isolated here.

---

**testkit/dbtest**

pgx test helpers: per-test tx-rollback isolation, ephemeral schema,
Postgres template-DB clone. No testcontainers.

---

**testkit/golden**

Golden-file snapshots with `-update` and testdata fixture loading.

---

**testkit/factory**

Generic closure-override test-data builders.
````

- [ ] **Step 2: Verify structure and counts**

Run:
```bash
grep -c '^\*\*' docs/packages.md && grep -c '^\*\*.*✅' docs/packages.md && grep -c '^## ' docs/packages.md
```
Expected: `143`, `74`, `14` (14 domain headers: core crypto resilience web view i18n data async realtime auth comms ai ops testkit). Note: only entry headers count as shipped — four descriptions also contain `✅` for shipped driver subpackages, which is why the second pattern anchors to `^\*\*`.

Run:
```bash
grep -nE 'authz|msg/|jwtverify|namegen|icebox|Icebox|recommended\.|— core\.' docs/packages.md
```
Expected: no output.

Run:
```bash
grep -c 'async/' docs/packages.md
```
Expected: non-zero (async domain present).

- [ ] **Step 3: Commit**

```bash
git add docs/packages.md
git commit -m "Rework packages.md into SaaS-focused package catalog"
```

---

### Task 3: Slim CLAUDE.md to pointers

**Files:**
- Modify: `CLAUDE.md` (full replacement)

**Interfaces:**
- Consumes: `docs/design.md` (Task 1) and `docs/packages.md` (Task 2) must exist — the pointers reference them.
- Produces: final CLAUDE.md.

- [ ] **Step 1: Replace `CLAUDE.md` with exactly this content**

```markdown
- Work ONLY in CURRENT branch, don't switch!
- Use `just` recipes.
- Run `just fmt file_path.go` after file changes.
- Run `just lint` after task finished.
- `docs/packages.md` = package catalog & roadmap (list only). `docs/design.md` = ALL design rules — layout/naming, package idioms & anatomy, dependency policy, seams, testing policy, anti-scope. Read design.md BEFORE creating or changing any package and follow it exactly.
- PR flow: create new PR -> whait all CI passed -> fix failed workflows -> learn Claude's review -> fix all found issues and resolve fixed threads -> commit -> repeat until all issues will be fixed.
```

- [ ] **Step 2: Verify**

Run:
```bash
wc -l < CLAUDE.md && grep -c 'design.md' CLAUDE.md && grep -nE 'builder|black-box|structfields|watermill' CLAUDE.md
```
Expected: `6`, `1`, and no output from the final grep (design rules no longer duplicated; they live in design.md).

- [ ] **Step 3: Commit**

```bash
git add CLAUDE.md
git commit -m "Slim CLAUDE.md: point to design.md and packages.md, drop duplicated rules"
```
