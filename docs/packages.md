# Forge — Package Catalog

Forge is a batteries-included Go framework for SaaS applications: it ships
the ~99% of boilerplate every SaaS repeats — tenancy, authentication,
authorization, background jobs, webhooks/postbacks, audit logging, rate
limiting, idempotency — as small composable packages, alongside all the
low-level bricks and helpers. This file is the roadmap of packages not yet
built: the moment a package ships it is removed from this list — its `doc.go`
(godoc) becomes the reference. All design rules — layout, naming, idioms,
dependencies, seams, anti-scope — live in [design.md](design.md).

Every entry ends with a `Deps:` line listing the forge packages it
imports: unmarked names are already shipped (buildable today); names
tagged `(planned)` are elsewhere in this file and define build order.
External module deps stay in the entry prose. Composition partners
(packages a consumer wires alongside) are not deps and are not listed.

## web/

---

**web/captcha**

Server-side CAPTCHA verification behind a `Verifier` seam; providers
(`captcha/turnstile`, …) are thin POST+JSON adapters over httpclient — no
provider SDKs.

Deps: `web/httpclient`.

---

**web/autocert**

ACME/Let's Encrypt TLS via `x/crypto/acme/autocert` wired as `tls.Config`
+ HTTP-01 handler for httpserver — pairs with `tenant`'s custom-domain
resolution for customer domains.

Deps: `web/httpserver`.

---

**web/smartlink**

The link package: a destination-decision engine plus a storage-backed
manager and redirect handler on top of it. The engine compiles ordered
rules of typed matchers — `Geo`, `Device`, `Locale`, `ParamEquals`,
`TimeWindow`, `Percent` — evaluated over a caller-built visit context (no
net/http import), first match wins, mandatory default target. Weighted
splits bucket deterministically by hash of a caller-supplied sticky key,
never RNG. The decision returns the matched rule and the final URL —
template macros from the visit context, param merge policy — and is what
the caller emits as the click event. Rule values are consumer data
hydrated into the typed vocabulary; no DSL.

The manager mints short codes over a storage-agnostic `Store` (`pgstore`
driver + migration ships with the package) with `cache.Store`
read-through: collision-retried generation, vanity codes with a
reserved-word blocklist, expiry/deactivation, and metadata stamping. A
uniform per-click pipeline serves both a fixed-destination link (a
compiled degenerate spec — no rules) and a rule-driven one (a
consumer-supplied resolver) identically: build the visit, merge the
link's metadata, decide, redirect 302/307 with no-store (never 301 — it
would kill hit observation forever), then hand the hit to a post-redirect
`OnHit` observer for the caller to log or count. Sync decorators
(fraud diversion, A/B overrides, metrics) wrap the decision itself;
`WithScope` fail-closed tenancy applies to management operations only —
resolving a code and serving it stay public, and codes are globally
unique. Not `featureflag` ("is X on for subject"); not `hostrouter`
(inbound hosts); not `magiclink` (self-contained signed token, not a
redirect) — this selects and serves outbound destinations from a stored
code. Rule storage/admin, target health checks, click counting, and bot
filtering stay consumer-side.

Deps: `core/clock`, `core/id`, `core/random`, `resilience/cache`,
`ops/logger` (+ pgx in the pgstore driver).

## view/

---

**view/flash**

One-shot messages surviving a redirect over a pluggable Store:
signed-cookie store built in (composes `cookie`); server-side store rides
`cache.Store`. For PRG and htmx flows.

Deps: `web/cookie`, `resilience/cache`.

---

**view/form**

Whole-form decode into structs (reflection confined to `structfields`),
sticky `Values`, render-friendly `Errors` carrying `validate`'s i18n keys
(translated by `core/i18n`), plus error-class/aria helpers. Backbone of
server-rendered CRUD.

Deps: `core/structfields`, `core/validate`, `core/i18n`.

## data/

---

**data/objectstore**

Blob `Store` seam with a path-traversal-safe disk adapter;
`objectstore/s3` driver on aws-sdk-go-v2. Magic-byte MIME validation via
`filetype`, tenant key-prefix scoping, presigned URLs.

Deps: `core/filetype`.

---

**data/seed**

Idempotent named-Seeder runner with an `app seed` cli.Command; mirrors
`migration`'s shape.

Deps: `data/postgres`; `ops/cli` (planned).

---

**data/imageproc**

Decode-limit-guarded resize/crop/re-encode over `x/image` — the
avatar/logo upload → process → store pipeline.

Deps: none forge-internal (x/image external).

---

**data/settings**

Typed, scoped (tenant/user) settings over a storage-agnostic Store:
versioned values with change history, effective-at scheduling, and
pending-change semantics — tightening applies immediately, loosening after
a configurable delay (the plan-downgrade / regulatory-limit change
discipline).

Deps: `core/clock`.

---

**data/retention**

Named retention policies run as batched delete/anonymize sweeps via
`scheduler` + `queue`: per-policy dry-run, progress checkpoints, and
audit events. Handles the two-sided GDPR constraint — minimum retention
and erasure deadlines — as declared policy, not cron scripts.

Deps: `async/scheduler` (planned), `async/queue`, `ops/auditlog` (planned).

---

**data/export**

Streaming CSV/JSONL/XML writers with bounded memory over a small
`RowSource` seam with pgx-rows and `database/sql` rows adapters (the
latter covers ClickHouse via its `OpenDB` path) — back-office exports,
regulator feeds, and partner-facing stats reports from one writer.
Write-only: no XLSX, no import/parse side.

Deps: `data/postgres`.

---

**data/ingest**

The read side `data/export` deliberately omits: streaming CSV/JSONL parse
with per-row `validate` and a row-addressed error report, feeding a
batch-insert seam — the "import your data" onboarding flow. (Named
`ingest` because `import` is a Go keyword.)

Deps: `core/validate`.

---

**data/reportspec**

Self-serve report shapes as validated structured data — the
partner/customer-facing report-builder core, no DSL: the consumer
registers a catalog of typed dimensions (column expression, groupable or
not) and metrics (aggregate expression); a `Spec` — selected columns,
group-by, typed filters (eq/in/range), date range, sort — validates
fail-closed against the catalog (unknown name, un-groupable dimension,
type-mismatched filter = error) and compiles to `(sql, args)` SELECT
fragments with a placeholder-dialect option (`$n` pgx, `?` ClickHouse).
Column/aggregate expressions are trusted registered strings; spec values
only ever bind as args — user input never enters SQL text. Tenancy via
an optional construction-time scope hook appending a scope clause to
every emitted WHERE; a configured hook yielding no scope is an error,
never an unscoped query. Composes `data/export` for streaming;
pagination, result execution, caching, and saved reports stay
consumer-side.

Deps: none (stdlib only).

## finance/

---

**finance/ledger**

Double-entry money ledger over `core/money`, Postgres-native by design:
every invariant is a SQL predicate inside the caller's `pgx.Tx` — no
storage seam (a faithful second implementation would be a second ledger;
`testkit/dbtest` precedent). `Post(ctx, tx, …)` composes with sqlc
repositories and `eventbus.Seen` in one commit; the package owns its
schema via embedded migrations — consumers never write ledger tables,
reads go through the query API or exported views.

Pairwise postings (src, dst, amount, one currency) — balanced by
construction, one row per movement — with `group_ref` correlating
multi-part events (deposit = wallet +97, fee +3; FX = one balanced pair
per currency); idempotent by unique external ref (replay returns the
original entry). Holds are rows, not shadow accounts: accounts carry
`balance` and `held` (available = balance − held), authorize opens a
hold without a posting, settle writes the single real posting, void
writes none; optional `expires_at` with an `ExpiredHolds` query — sweep
policy is consumer-side (a bet hold auto-voids, a CPA hold
auto-settles). `Void` after settle errors; post-settlement corrections
are forward entries with an `adjusts` back-reference. Rows change
status; money history only ever grows.

Accounts are an explicit registry — unique (tenant, owner, purpose,
currency), idempotent `EnsureAccount`, never created implicitly by a
posting (a typo must fail, not mint an account). Per-account floor,
NULL = floor-free (house/mint: bonus grants, commission expense,
jackpot seeds). The floor drives locking: floored accounts carry
materialized `balance`/`held`, checked and moved by one conditional
`UPDATE … RETURNING` (floor predicate in the WHERE; zero rows =
insufficient funds), multi-account entries updating in sorted order;
floor-free hot accounts have no materialized balance — derived via a
snapshots table (snapshot + sum-since), so no house-row bottleneck. A
drift-check job recomputes materialized balances from postings: balance
is a verified cache, postings are the truth. `Hold`/`Settle`/`Void`/
`Post` each execute as one data-modifying-CTE statement (replay-gated
inside; a unique-ref race aborts the whole statement atomically), so
the contested row lock lives for server-side execution only.

Custom currencies (points, coins) are ordinary `money.Currency` values;
a nullable lot dimension is reserved in schema for expiring balances
(FIFO loyalty — not in v1). Not owned: limit rules (domain code in the
same tx — the ledger supplies the tx and available-balance primitives;
`quota` stays a drift-tolerant shadow, never the regulatory gate), FX
conversion math (`fxrate` records the rate), statements/invoices
(readers), carry-over policy (`formula` input), and process history —
deposits, game rounds, provider calls including rejected ones live in
consumer tables with their own balance snapshots, linked 1:1 by posting
ref; the ledger records money that moved, never attempts. Mongo-only
stacks give the ledger its own Postgres and bridge with idempotent refs
+ a reconcile sweep (recipe owed).

Deps: `core/money`, `core/clock`, `data/postgres`, `data/migration`;
tests: `testkit/dbtest` (planned).

---

**finance/fxrate**

Exchange rates behind a `RateSource` seam with stored snapshots; `Convert`
records the rate applied, so audits answer "what rate at transaction
time". Math is multiply-and-round via `core/decimal`; providers are thin
JSON adapters over httpclient — no provider SDKs, no live streaming.

Deps: `core/decimal`, `web/httpclient`.

---

**finance/tariff**

Tiered/banded rate calculation over `core/money`/`core/decimal`:
graduated vs volume band semantics ("25% up to 10, 30% to 50, 35%
above") with deterministic rounding, as a pure calculator — bands are
caller-supplied values; effective-dating is the caller choosing which
band set applies (composes `data/settings` for deal changes). Consumers:
usage-billing overage tiers, revenue-share deals, commission plans.

Deps: `core/money`, `core/decimal`.

---

**finance/formula**

Formulas as structured, versioned data — never text: a spec of named
derived metrics, each a list of (metric, decimal coefficient) terms over
inputs or prior stages plus an optional clamp, evaluated in
`core/decimal` with deterministic rounding. Evaluation returns an
explanation record — every stage, every term's contribution, spec
version, inputs — the statement line item and the dispute answer (the
`fxrate` record-the-evaluation philosophy). Specs are immutable once
referenced; recomputes byte-match. Derives the base (NGR, billable
usage, commission bases) that `tariff` rates and `ledger` posts. Hard
anti-scope: no string parsing, no conditionals, no user-typed
expressions — anything beyond staged linear terms + clamp is a
registered Go function; for fixed deal shapes the documented default is
named Go functions with per-deal parameters as data.

Deps: `core/decimal`.

---

**finance/invoice**

The invoice document model — invariants, not rendering: numbering via a
per-series `Sequence` with two explicit modes (strict-gapless
transactional counter vs monotonic-with-gaps — the requirement is
jurisdictional); immutable once issued, corrections are credit notes
back-referencing the original (the corrections-post-forward rule shared
with `ledger`); line items → tax lines → totals in `money` with per-line
vs per-total rounding policy via `Allocate`; draft → issued →
paid/partially-paid/void/overdue over `fsm`, paid-matching by `ledger`
posting refs; self-billing direction (platform issues on the supplier's
behalf — affiliate/agent payouts); multi-currency with the `fxrate`
snapshot recorded. Tax rates are caller-supplied data — never
determined; rendering stays out (HTML is a `render` recipe, PDF
consumer-side); no dunning, no e-invoicing formats, no subscription or
pricing logic (the billing anti-scope stands).

Deps: `core/money`; `core/fsm`.

## async/

---

**async/queue/sqlite · async/queue/nats · async/queue/kafka**

Additional `queue.Broker` drivers for the shipped `async/queue` engine
(engine, in-memory broker, `queue/postgres`, `queue/redis`, and
`queue/mongo` already ship — see their godoc): `queue/sqlite` (zero-infra single-node and
dev/test), `queue/nats`, `queue/kafka`. The engine — not the driver —
owns retry/backoff, delay, and max-attempts → dead-letter, so behavior is
identical across backends; each driver only moves bytes behind the
strictly-pull `Broker` seam. Brokers without real multi-document ACID
transactions (redis, nats, kafka) get transactional enqueue via
`async/outbox`; stores that have them implement `TxPusher` natively
(`queue/postgres`, `queue/mongo`, `queue/sqlite`).

Deps: `async/queue`; drivers: `data/sqlite` (planned), `data/nats`
(planned), `data/kafka` (planned).

---

**async/scheduler**

Cron/interval `supervisor.Service` that *enqueues* into the engine when
due; fires once per fleet via a `unique(name, scheduled_for)` insert race
on SQL drivers; small local cron parser, no robfig/cron.

Deps: `ops/supervisor`; `async/queue`.

---

**async/outbox**

Transactional outbox: intent rows committed inside the business DB
transaction plus a relay `supervisor.Service` that forwards committed rows
into any `Broker` — the bridge from a Postgres/SQLite transaction to
redis/nats/kafka delivery.

Deps: `ops/supervisor`; `async/queue`.

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

Deps: `realtime/fanout` (planned).

---

**realtime/fanout**

In-process pub/sub hub (bounded buffers, explicit slow-consumer policy)
with the `Bus` seam for multi-instance backplanes and an optional bounded
`WithReplay(n)` ring. Drivers: `fanout/pgbus` (LISTEN/NOTIFY — multi-
instance push with zero new infrastructure), `fanout/redisbus` (takes the
caller's `data/redis` client).

Deps: drivers ride `data/postgres`, `data/redis`.

---

**realtime/websocket**

The complete WebSocket package: accept/read/write/ping-pong/close over
isolated `coder/websocket` behind exported forge `Conn`/`Message`, plus
the hub — connection registry, rooms, broadcast under supervisor, with
production bounds (payload/event-name/auth-blob limits,
drop-frames-vs-teardown overflow policy, `Shutdown(ctx)` drain).

Deps: `ops/supervisor` (coder/websocket external).

---

**realtime/presence**

Who-is-here tracking with TTL heartbeats over `fanout` + `cache` — online
indicators, concurrent-viewer counts.

Deps: `resilience/cache`; `realtime/fanout` (planned).

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

Deps: `resilience/cache`, `data/postgres`; `web/fingerprint`.

---

**auth/impersonation**

Support-agent "log in as": time-boxed impersonation sessions with a
required reason, a context flag views render as a banner, `auditlog`
events on start/end and every action, optional `ops/approval` gate.
Composes `session` and the `access` decision seam — the hand-rolled
version is where privilege escalation lives.

Deps: `auth/access`; `auth/session`, `ops/auditlog`, `ops/approval` (all
planned).

---

**auth/scim**

SCIM 2.0 provisioning server for enterprise directory sync (Okta/Entra):
Users/Groups resources, PATCH semantics, soft-delete mapping, and the
filter subset IdPs actually send — behind a storage-agnostic Store,
authenticated via `apikey` or `oauthserver` tokens. The enterprise
checkbox next to SSO; SAML stays out (`oauthclient` OIDC covers modern
IdPs).

Deps: `auth/guard`.

---

**auth/rbac**

Role-based access control: predefined roles, role nesting/inheritance (a
role inherits another role's permissions and adds its own),
out-of-hierarchy standalone roles, wildcard grants; resolves subject →
effective permission set. Implements the `access` decision seam consumed
by `guard`/`RequirePermission` (401-vs-403 split). Subject→role
assignment behind a storage-agnostic Store.

Deps: `auth/access`.

---

**auth/acl**

Per-subject / per-resource grant and deny overrides (deny wins) layered
onto rbac decisions — "this manager sees exactly these assigned agents".
The runtime-data authorization layer: storage-agnostic Store with drivers;
composes into the `access` decision seam.

Deps: `auth/access`.

---

**auth/abac**

Attribute/relationship predicates as registered Go functions — "agent sees
own subtree but not subagents' player details" — evaluated in the
`access` decision seam alongside rbac/acl. The relationship data (trees,
assignments) stays consumer code feeding the predicate; no policy DSL.

Deps: `auth/access`.

---

**auth/webauthn**

Passkey registration and assertion over isolated `go-webauthn` (CBOR/COSE
is the one justified heavy auth dep).

Deps: none forge-internal (go-webauthn external).

---

**auth/idverify**

Identity-verification (KYC/sanctions) `Verifier` seam in the `web/captcha`
mold: applicant/check status mapped to a small enum, inbound
webhook-status mapping; providers (`idverify/sumsub`, …) are thin
POST+JSON adapters over httpclient — no provider SDKs. Decisioning stays
consumer-side.

Deps: `web/httpclient`.

## comms/

---

**comms/email**

The complete email package: `Sender` seam + stdlib net/smtp implementation
(STARTTLS, multipart, attachments) + named-template rendering (subject +
HTML + text into a `Message`). `email/markdown` renders markdown + YAML
frontmatter (subject/preheader, CTA-button extension) — the designer-free
transactional format; goldmark confined there. Provider adapters
(SES/Postmark/…) are consumer-side or isolated subpackages.

Deps: none forge-internal (goldmark external, isolated).

---

**comms/sms**

SMS `Sender` seam; `sms/twilio` driver is a stdlib form-POST over
httpclient — never twilio-go.

Deps: `web/httpclient`.

---

**comms/push**

Push `Pusher` seam + `push/webpush` (VAPID/ECDH/AES-GCM, fully stdlib).
FCM/APNs stay consumer-side behind the seam.

Deps: `web/httpclient`.

---

**comms/notify**

Notification routing above the channel seams: typed named notifications
dispatched per user-over-tenant preference resolution (rides
`data/settings`) to channels registered by name — the `email`/`sms`/`push`
Sender seams, or any consumer-registered channel (in-app inbox stays
consumer-side per the anti-scope recipe). Fallback chains trigger on send
failure or missing binding (no push token → email), never on "unread" —
read tracking is a product, not a brick. Durable delivery rides
`async/queue`.

Deps: `data/settings` (planned), `async/queue`.

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

Deps: `web/httpclient`; `realtime/sse` (planned).

---

**ai/prompt**

Type-safe prompt templating from an fs.FS registry over text/template
with strict missing-key errors. Mechanical only — no chains/agents. Not
html/template (escaping corrupts prompts).

Deps: none (stdlib only).

---

**ai/structured**

Coerce noisy LLM output into typed values: fence-strip, strict JSON decode
into T, repair-prompt on failure.

Deps: none (stdlib only).

---

**ai/embeddings**

`Embedder` seam + stdlib vector math (cosine, top-k) for small in-memory
corpora; no ANN, no persistence.

Deps: none (stdlib only).

---

**ai/mcpserver**

Expose explicitly-registered app operations as MCP tools (stdio +
streamable-HTTP) as `supervisor.Service` / `http.Handler`; hand-declared
schemas, no reflection; official MCP go-sdk isolated here.

Deps: `ops/supervisor` (MCP go-sdk external, isolated).

## ops/

---

**ops/debug**

One internal diagnostics surface: `/debug/pprof/*`, `/debug/stats`
(runtime/GC/goroutines JSON), `/debug/vars`, with an auth guard and a
dedicated-port `supervisor.Service`.

Deps: `ops/supervisor`, `auth/guard`.

---

**ops/auditlog**

Append-only structured audit events (actor/action/resource/outcome) over a
`Sink` seam; slog + JSONL sinks built in; `auditlog/pgsink` adds the
insert plus tenant-isolated, keyset-paginated queries — the audit trail
every B2B SaaS shows in its UI. Optional per-stream hash chaining
(prev-hash + a verify pass) makes the trail tamper-evident for
compliance-grade audits.

Deps: `ops/logger`, `data/postgres`.

---

**ops/approval**

Maker-checker dual control: typed approval requests (action + payload) a
second person approves or rejects over a storage-agnostic Store; decisions
emit `auditlog` events and approver eligibility rides the `auth/access`
decision seam. The two-person rule for payouts, limit overrides, and
config changes.

Deps: `auth/access`; `ops/auditlog` (planned).

---

**ops/cli**

Struct-described command tree over stdlib `flag.FlagSet`, ctx-aware Run,
auto help; no cobra, no global registry. Covers
serve/migrate/worker/seed/version.

Deps: none (stdlib only).

---

**ops/tracing**

`Tracer` seam, W3C traceparent middleware, trace_id log extractor;
`tracing/otel` driver isolated. Pairs with httpclient propagation.

Deps: none forge-internal (otel external, isolated).

---

**ops/secretsource**

Hot-reloadable secrets behind a `Provider` seam, exposing `redact.Secret`
values; cloud clients stay consumer-side.

Deps: `crypto/redact`.

---

**ops/configwatch**

Poll-based config reload with atomic snapshot swap, as a
`supervisor.Service`.

Deps: `ops/supervisor`.

## testkit/

---

**testkit/webtest**

Black-box HTTP harness: real `:0` server from an `http.Handler`, fluent
request builder, `testing.TB` response asserts.

Deps: none (stdlib only).

---

**testkit/htmltest**

CSS-selector DOM assertions over any HTML source (`io.Reader`/string) —
webtest responses and `email/markdown` bodies are the named consumers
(text/attr/count/exists, form-field values, htmx fragments); goquery as a
test-only dep, isolated here.

Deps: none forge-internal (goquery external, test-only).

---

**testkit/dbtest**

pgx test helpers: per-test tx-rollback isolation, ephemeral schema,
Postgres template-DB clone. Layered on `testkit/pgtest` provisioning.

Deps: `data/postgres`, `testkit/pgtest`.

---

**testkit/pgtest · redistest · mongotest · clickhousetest · opensearchtest** (shipped)

Integration-tier provisioning helpers: each returns a DSN/addr/URI for a real
backend, spun once per test process via testcontainers (Ryuk-reaped) or taken
from a `FORGE_TEST_<BACKEND>_*` env override. Built only under the `integration`
build tag. Consumed by every `//go:build integration` test across `data/*`,
`auth/*/pgstore`, `resilience/*`, `gaming/rng/pgstore`, and `async/queue/*`.

Deps: none forge-internal (testcontainers external, test-only).

---

**testkit/golden**

Golden-file snapshots with `-update` and testdata fixture loading.

Deps: none (stdlib only).

---

**testkit/factory**

Generic closure-override test-data builders.

Deps: none (stdlib only).
