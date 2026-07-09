# Forge — Package Catalog

Forge is a batteries-included Go framework for SaaS applications: it ships
the ~99% of boilerplate every SaaS repeats — tenancy, authentication,
authorization, background jobs, webhooks/postbacks, audit logging, rate
limiting, idempotency — as small composable packages, alongside all the
low-level bricks and helpers. This file is the roadmap of packages not yet
built: the moment a package ships it is removed from this list — its `doc.go`
(godoc) becomes the reference. All design rules — layout, naming, idioms,
dependencies, seams, anti-scope — live in [design.md](design.md).

## core/

---

**core/qrcode**

QR code generation to PNG / base64 data-URI from any string (vendored
encoder, no deps): 2FA enrollment URIs, referral/share links.

## resilience/

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
