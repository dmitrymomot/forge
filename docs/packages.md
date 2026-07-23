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

## core/

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

Deps: `async/scheduler`, `async/queue`, `ops/auditlog`.

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

---

**data/comments**

Generic threaded discussions `Store[T]`: subject-addressed threads (`Subject{Kind, ID}`, no FK into consumer tables), append/edit/soft-delete with edit history, visibility tiers (public/internal) enforced at the store, participants, cursor pagination. The body is a consumer struct marshaled at the storage seam and never interpreted — mixed timelines (comments + system events) ride an envelope `T`. Exports the `Threader[T]` seam so extensions decorate rather than grow the core, each seeing into the opaque body only through a consumer-supplied extractor: `comments/mentions` (`func(T) []string` lens, edit re-diffing, per-mention seen state, unseen inbox/count), `comments/reactions` (per-user emoji toggle + counts keyed by comment ID), `comments/attachments` (`func(T) []Ref` lens tracking objectstore keys in sidecar rows — per-thread file listing, delete/edit cascade with an opt-in blob-removal hook; upload itself stays consumer → `objectstore`). Reply-nesting only ever as an additive core option; thread-level read tracking is a product, never here. In-memory store built in; `comments/pgstore` driver.

Deps: `data/postgres`, `web/pagination`.

---

**data/customfields**

Tenant-defined typed custom fields on consumer entities, in the `reportspec` mold: a per-tenant field catalog (name, type, required, enum choices) values validate against fail-closed — unknown field, type mismatch, missing required = error, never a silent write. Typed value model (string/number/bool/date/enum/multi-enum) over a storage-agnostic Store, plus `(sql, args)` filter-fragment emission for querying by custom field with a placeholder-dialect option; user input only ever binds as args. Field deletion is soft (values orphan gracefully); rendering and form UI stay consumer-side (`view/form` composes).

Deps: `core/validate`.

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

Deps: `async/queue`; drivers: `data/sqlite`, `data/nats` (planned),
`data/kafka` (planned).

## realtime/

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

Deps: `resilience/cache`; `realtime/fanout`.

## auth/

---

**auth/session**

`Manager` owns lifecycle and storage (Start/Load/Save/Destroy/Rotate/Authenticate/Elevate/Rebind) behind a pluggable `Store` and knows nothing about HTTP; `Middleware` owns the request layer — extract credential, load, run policies, expose on context, commit exactly once at the first response byte, so a redirecting login handler still gets its cookie set. `Namespace[T]` gives each app or plugin an independently-owned typed slice of the payload, with unknown-namespace passthrough so one process never clobbers another's keys. Device metadata (IP/user agent/fingerprint) is pinned once at creation, never refreshed per request — `Rebind` is the deliberate re-pin after re-authentication. Sliding-plus-absolute expiry with a separate remember-me deadline pair; step-up re-auth via `RequireElevation`, an `auth/access.Decider`; a `guard` adapter (`Extractor`/`Verifier`) keeps authentication gating in `auth/guard` rather than duplicating it. Multi-device management (ListByUser/Revoke/LogoutOthers/DeleteByUser — "log out other devices", GDPR deletion) rides the optional `UserIndex` store capability. An optional `WithScope` hook adds tenant confinement — every save stamps the resolved tenant, every load and device-management call is confined to it, a hook error or empty scope fails closed — with zero ceremony for single-tenant apps that never configure it. In-memory store built in with a `storetest` conformance suite every driver must pass; drivers forthcoming: `session/pgstore` (user-indexed), `session/mongostore`, `session/cookiestore` (stateless-encrypted, no `UserIndex`), and a generic `session/kvstore` riding `cache.Store`.

Deps: `auth/access`, `auth/guard`, `ops/supervisor`; `web/middleware`, `web/problem`.

---

**auth/impersonation**

Support-agent "log in as": time-boxed impersonation sessions with a
required reason, a context flag views render as a banner, `auditlog`
events on start/end and every action, optional `ops/approval` gate.
Composes `session` and the `access` decision seam — the hand-rolled
version is where privilege escalation lives.

Deps: `auth/access`, `ops/approval`, `ops/auditlog`; `auth/session`
(planned).

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

**comms/inbound**

Inbound email processing — the receive side `comms/email` deliberately omits: MIME parse over stdlib `net/mail`/`mime` into a typed `Message` (decoded headers, text/HTML bodies, attachments with magic-byte MIME check via `filetype`), reply/quote/signature stripping to the newly-written text, thread correlation via `In-Reply-To`/`References` plus plus-addressing and reply-token recipients, and a DKIM/SPF-results header reader (verdicts from the receiving MTA — no crypto here). Transport-agnostic: consumers feed it raw RFC 5322 bytes from an SES/Mailgun/Postmark webhook or an IMAP poller; what becomes a ticket is consumer-side.

Deps: `core/filetype`.

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

Deps: `web/httpclient`; `realtime/sse`.

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

**ops/cli**

Struct-described command tree over stdlib `flag.FlagSet`, ctx-aware Run,
auto help; no cobra, no global registry. Covers
serve/migrate/worker/seed/version.

Deps: none (stdlib only).

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

---

**ops/sla**

SLA policy engine over `bizcal`: named targets (first-response, resolution) attach deadlines to consumer subjects, computed in business time; pause/resume (waiting-on-customer) with correct deadline extension, warning thresholds, and breach/warning events delivered through `scheduler`-driven sweeps over a storage-agnostic Store. Fulfillment, escalation actions, and reporting stay consumer-side.

Deps: `async/scheduler`; `core/bizcal`.

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
