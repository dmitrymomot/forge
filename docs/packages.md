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

---

**core/fsm**

Typed finite state machine: declared states and transitions with guards,
on-transition hooks, and illegal-transition errors; pure generics, zero
deps. Persistence is caller-owned — apply it to a status column. The
lifecycle brick under order/subscription/verification/payout flows.

Deps: none (stdlib only).

---

**core/country**

Curated ISO-3166 static data: alpha-2/alpha-3 codes, English names,
default currency, and dial prefix per country — the `money` ISO-4217
precedent applied to countries. Zero deps; consumers: registration/KYC
forms, `geoip` enrichment, `i18n`, `core/phone`.

Deps: none (stdlib only).

---

**core/phone**

E.164 phone normalization: parse/format/validate over `core/country`'s
dial-prefix table. Consumers: `comms/sms`, `auth/otp`, KYC forms. No
carrier metadata, no line-type detection — the libphonenumber swamp
stays out.

Deps: `core/country` (planned).

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

**web/shortlink**

Short-code links over a storage-agnostic Store with `cache.Store`
read-through: collision-retried generation over an unambiguous
base58-style alphabet, vanity slugs with a reserved-word blocklist,
expiry/deactivation with a configurable fallback, and an `OnHit` hook
that emits — counting stays the caller's job. Redirects are 302/307 with
no-cache headers (a cached 301 kills hit counting forever); destinations
live server-side (never `?url=`) and are scheme-allowlisted at creation.
Branded short domains compose `tenant` custom domains + `hostrouter`.
Not `magiclink` (self-contained signed token); not `smartlink` (no rules
— a code resolves to one target or one rule-table handle).

Deps: `core/random`, `resilience/cache`.

---

**web/smartlink**

Destination-decision engine (the TDS/smartlink core): ordered rules of
typed matchers — `Geo`, `Device`, `Locale`, `ParamEquals`, `TimeWindow`,
`Percent` — evaluated over a caller-built visit context (no net/http
import), first match wins, mandatory default target. Weighted splits
bucket deterministically by hash of a caller-supplied sticky key, never
RNG. The decision returns the matched rule and the final URL — template
macros from the visit context, param merge policy — and is what the
caller emits as the click event. Rule values are consumer data hydrated
into the typed vocabulary; no DSL. Not `featureflag` ("is X on for
subject"); not `hostrouter` (inbound hosts) — this selects outbound
destinations. Rule storage/admin, target health checks, and bot
filtering stay consumer-side.

Deps: `core/clock`.

---

**web/attribution**

Marketing-touch capture: middleware records a configured param set
(`utm_*`, click IDs, sub-IDs) into a signed cookie (`cookie` + `sign`)
under first-touch or last-touch policy with an attribution window, and
hands the stored touch back at conversion time. Includes a
tracking-pixel endpoint (correct 1×1 GIF + no-cache). The "where did
this signup come from" answer every SaaS wants and every affiliate
platform requires; multi-touch models stay out.

Deps: `web/cookie`, `crypto/sign`.

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
(translated by `i18n/catalog`), plus error-class/aria helpers. Backbone of
server-rendered CRUD.

Deps: `core/structfields`, `core/validate`; `i18n/catalog` (planned).

## i18n/

---

**i18n/catalog**

Message catalog with fs.FS/JSON loaders and curated CLDR plural rules;
`T(loc, key, args...)` selects plural forms internally; fallback chains;
translates `validate` violation keys. Zero deps — no x/text, no YAML.

Deps: none (stdlib only).

---

**i18n/locale**

Accept-Language negotiation (q-values, region fallback), context carrier,
middleware with a resolver chain (cookie → query → Accept-Language →
default), and a logger context extractor.

Deps: `ops/logger`.

---

**i18n/numbers**

Locale-aware number/currency/percent formatting; `Currency(money.Money)` —
the locale rendering `core/money` defers by design.

Deps: `core/money`.

---

**i18n/dates**

Locale + timezone date/time and relative-time ("3 hours ago") formatting
with named presets. Gregorian only.

Deps: none (stdlib only).

## data/

---

**data/pagination**

Opaque cursor codec (base64+JSON, optional HMAC via `sign`), keyset
WHERE/ORDER fragment builders emitting pgx-compatible `(sql, args)`,
`Page[T]` metadata, and the page-window view-model (ellipses, links
preserving query params) for server-rendered navigation.

Deps: `crypto/sign`.

---

**data/tenant**

The multi-tenancy package: `Resolver` chain with shipped resolvers —
subdomain (against a base domain), custom domain (via a storage-agnostic
`DomainLookup` seam), header, cookie, path prefix, API-key-derived —
precedence-ordered middleware putting `TenantID` in context, a
transport-agnostic carrier (jobqueue handlers set/read tenant without
HTTP), and explicit parameterized `ScopeClause` SQL fragments — visible at
every query, never auto-injected.

Deps: none (stdlib only).

---

**data/dataloader**

Generic per-request batch-and-cache loader collapsing N+1 lookups; pure
generics, no DB imports; the batch fn is caller-owned.

Deps: none (stdlib only).

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
`scheduler` + `jobqueue`: per-policy dry-run, progress checkpoints, and
audit events. Handles the two-sided GDPR constraint — minimum retention
and erasure deadlines — as declared policy, not cron scripts.

Deps: `async/scheduler`, `async/jobqueue`, `ops/auditlog` (all planned).

---

**data/export**

Streaming CSV/JSONL/XML writers with bounded memory and a pgx-rows
adapter — back-office exports and regulator feeds. Write-only: no XLSX,
no import/parse side.

Deps: `data/postgres`.

---

**data/ingest**

The read side `data/export` deliberately omits: streaming CSV/JSONL parse
with per-row `validate` and a row-addressed error report, feeding a
batch-insert seam — the "import your data" onboarding flow. (Named
`ingest` because `import` is a Go keyword.)

Deps: `core/validate`.

---

**data/clickhouse**

ClickHouse connection factory in the `data/postgres` mold: DSN config with
Validate, pooling, health ping. Connection only — query building and
schema stay consumer-side.

Deps: none forge-internal (driver external).

---

**data/sqlite**

SQLite connection factory in the `data/postgres` mold, owning the pragma
discipline — WAL, `busy_timeout`, `synchronous`, foreign keys — and
single-writer pool sizing; cgo-free `modernc.org/sqlite` isolated here.
The zero-infra single-node story under `jobqueue/sqlite` and dev/test
setups.

Deps: none forge-internal (modernc.org/sqlite external).

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

Deps: `core/money`; `core/fsm` (planned).

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

Deps: `ops/supervisor`; drivers: `data/postgres`, `data/redis`,
`data/sqlite` (planned).

---

**async/scheduler**

Cron/interval `supervisor.Service` that *enqueues* into the engine when
due; fires once per fleet via a `unique(name, scheduled_for)` insert race
on SQL drivers; small local cron parser, no robfig/cron.

Deps: `ops/supervisor`; `async/jobqueue` (planned).

---

**async/eventbus**

Typed events over the same `Broker` drivers, two modes: sync in-process
observer (no durability) and durable mode — each named subscription is its
own queue, publish fans out one message per subscription, competing
consumers within one, at-least-once. Transactional publish on SQL drivers;
exports the `Seen(ctx, tx, id)` idempotency inbox. Handlers must be
idempotent.

Deps: `async/jobqueue` (planned).

---

**async/eventrouter**

Event egress over `eventbus`: each destination is its own named
subscription (slow-destination isolation for free), filter/remap as
registered Go functions — no mapping DSL — and batched delivery with
size+age flush, batch-level retry, and poison-event handling. Reference
`Deliverer` adapters: generic JSON-batch HTTP and signed postbacks via
`comms/webhook`. Destination configs are consumer data; forge ships the
engine, never a Segment-style connector catalog. Delivery is
at-least-once and the router never dedups: stable event IDs ride every
delivery (`Idempotency-Key` header / payload field) and receivers dedup
— the Stripe contract (in-router suppression would trade duplicates for
silent loss).

Deps: `web/httpclient`; `async/eventbus`, `comms/webhook` (planned).

---

**async/outbox**

Transactional outbox: intent rows committed inside the business DB
transaction plus a relay `supervisor.Service` that forwards committed rows
into any `Broker` — the bridge from a Postgres/SQLite transaction to
redis/nats/kafka delivery.

Deps: `ops/supervisor`; `async/jobqueue` (planned).

---

**async/collector**

Write-behind ingestion for proven-hot fire-and-forget paths (click
streams, beacons, telemetry): bounded in-memory buffer, batch flush by
size+age into a `Sink` seam, explicit overload policy — drop-newest with
counted, logged loss, never blocking the request path — and graceful
drain as a `supervisor.Service`. No dedup — double-fires and unique-key
rules are the downstream pipeline's concern. Reach for
`outbox`/`eventbus` first: this package is justified only when per-event
publish shows up in a profile (design.md §Performance — benchmark
required).

Deps: `ops/supervisor`.

---

**async/workflow**

DB-checkpointed linear step sequences over the engine
(onboarding/provisioning chains; resume after crash), with optional
per-step compensation — on failure, completed steps' compensations run in
reverse order (a payout pipeline that must undo its ledger debit). No DAG,
no DSL, no timers — not a Temporal clone.

Deps: `async/jobqueue` (planned).

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
Composes `session` and the authorization decision seam — the hand-rolled
version is where privilege escalation lives.

Deps: `auth/session`, `ops/auditlog`, `ops/approval` (all planned).

---

**auth/lockout**

Login/OTP failure counting with exponential delay and lockout windows over
the ratelimit counter seam. (Not rate shaping — that's `ratelimit`; not
cumulative caps — that's `quota`.)

Deps: `resilience/ratelimit`.

---

**auth/totp**

The complete 2FA package: RFC 6238/4226 TOTP/HOTP secret generation,
skew-window verify, otpauth:// provisioning URI, and one-time backup codes
(generate/hash/verify-and-consume, constant-time). QR image rendering
lights up via `core/qrcode`. Persistence is consumer DB.

Deps: `core/qrcode`, `core/random`, `crypto/consttime`.

---

**auth/magiclink**

Signed, TTL'd, single-use links over `crypto/token`: passwordless login,
team invites (role/tenant claims as a documented example), verify and
unsubscribe links. Stateless by default; `WithStore` for single-use
redemption. Does not send email.

Deps: `crypto/token`, `resilience/cache`.

---

**auth/oauthclient**

OAuth2/OIDC client: auth-code + PKCE, state, token exchange,
id_token/userinfo verification via `auth/jwt` (alg-pinned), provider
presets. On net/http over `httpclient` — no x/oauth2.

Deps: `auth/jwt`, `web/httpclient`.

---

**auth/oauthserver**

Machine-to-machine OAuth2 provider for partner-facing APIs:
client-credentials grant, token endpoint issuing short-lived JWTs via
`auth/jwt`, JWKS endpoint, client registry behind a storage-agnostic
Store. No auth-code-for-third-parties, no consent screens, no JWE.

Deps: `auth/jwt`.

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
effective permission set. Feeds the shared authorization decision seam
consumed by `guard`/`RequirePermission` (401-vs-403 split). Subject→role
assignment behind a storage-agnostic Store.

Deps: none (stdlib only).

---

**auth/acl**

Per-subject / per-resource grant and deny overrides (deny wins) layered
onto rbac decisions — "this manager sees exactly these assigned agents".
The runtime-data authorization layer: storage-agnostic Store with drivers;
composes into the same decision seam.

Deps: none (stdlib only).

---

**auth/abac**

Attribute/relationship predicates as registered Go functions — "agent sees
own subtree but not subagents' player details" — evaluated in the shared
decision seam alongside rbac/acl. The relationship data (trees,
assignments) stays consumer code feeding the predicate; no policy DSL.

Deps: none (stdlib only).

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

**comms/webhook**

The complete webhooks/postbacks package, both directions: outbound
HMAC-signed deliveries (Stripe-style `t=,v1=`) with timeout, bounded
retry, and idempotency keys — durable delivery rides `async/jobqueue` —
and inbound signature-verification middleware (Stripe/GitHub/Slack HMAC
schemes, constant-time, timestamp tolerance, reads and restores
`r.Body`). Signing and verifying share one scheme implementation behind a
pluggable `Scheme` seam, so bespoke partner schemes register without
forking the package.

Deps: `web/httpclient`, `crypto/sign`; `async/jobqueue` (planned).

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
`jobqueue`.

Deps: `data/settings`, `async/jobqueue` (both planned).

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

**ops/metrics**

Counter/Gauge/Histogram `Recorder` facade with an expvar default and
request middleware; `metrics/prometheus` is the only adapter.

Deps: none (stdlib only).

---

**ops/auditlog**

Append-only structured audit events (actor/action/resource/outcome) over a
`Sink` seam; slog + JSONL sinks built in; `auditlog/pgsink` adds the
insert plus tenant-isolated, keyset-paginated queries — the audit trail
every B2B SaaS shows in its UI. Optional per-stream hash chaining
(prev-hash + a verify pass) makes the trail tamper-evident for
compliance-grade audits.

Deps: `ops/logger`, `data/postgres`; `data/pagination` (planned).

---

**ops/approval**

Maker-checker dual control: typed approval requests (action + payload) a
second person approves or rejects over a storage-agnostic Store; decisions
emit `auditlog` events and approver eligibility rides the authorization
decision seam. The two-person rule for payouts, limit overrides, and
config changes.

Deps: `ops/auditlog` (planned).

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
Postgres template-DB clone. No testcontainers.

Deps: `data/postgres`.

---

**testkit/golden**

Golden-file snapshots with `-update` and testdata fixture loading.

Deps: none (stdlib only).

---

**testkit/factory**

Generic closure-override test-data builders.

Deps: none (stdlib only).
