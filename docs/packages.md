# Forge — Packages: Layout, Catalog & Roadmap

> Single source of truth for forge's repository layout, shipped package set,
> and roadmap. Replaces `domain-structure.md`, `maximal-package-set.md`, and
> `package-set-v2.md` (consolidated 2026-07-04, after a cross-framework review
> against kratos, go-kit, go-micro, fiber v3, fiber-contrib, and forge v1).
>
> **State: 72 shipped packages** (+3 driver subpackages) across
> `core/ crypto/ resilience/ web/ data/ ops/`
> · **45 roadmap** (committed) · **27 icebox** (demand-driven, no commitment).

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
  `pgx`, official DB clients, the S3 SDK, `x/crypto`.
- **Don't wrap** large, opinionated, fast-moving frameworks whose model would
  leak through forge's API (`watermill`, `stripe-go`, provider SDKs). Expose a
  small interface; the consumer takes the dep in *their* repo.
- **Build or vendor** anything small that shapes forge's own API (`id`,
  `validate`, `request`, the SKIP LOCKED job engine).
- **Always isolate** a real dependency in a driver subpackage
  (`logger/sentry`, `cache/redis`, `jobqueue/sqlbroker`) so it stays a
  swappable leaf.

Isolated deps today: `pgx`, goose (`migration`), the mongo/redis/opensearch
clients, sentry, `gopkg.in/yaml.v3` (`ops/config`'s YAML loader, its sole
consumer). Sanctioned for the roadmap: aws-sdk-go-v2 (`objectstore/s3`),
`coder/websocket` (`ws`), `go-webauthn` (`webauthn`), `x/crypto`
(password/kdf/autocert), goldmark (`email/markdown`), the official
MCP go-sdk (`mcpserver`), `x/image` (`imageproc`), prometheus client
(`metrics/prometheus`), OTel SDK (`tracing/otel`), goquery (`htmltest`,
test-only). **Postgres is THE database**; everything outside `data/*`, the
messaging engine, and the driver leaves stays stdlib.

## Repository layout rules

- **Single Go module.** One `go.mod` at the root. No sub-modules, no
  `go.work`. (The tree is module-split-ready if dependency hygiene ever
  becomes a real consumer complaint — a domain folder can be promoted without
  moving files.)
- **Group by purpose, not by layer, tier, or build phase.** Folder names are
  domain nouns (`crypto`, `web`, `data`). Tier/build-order are doc attributes,
  not taxonomy.
- **Two levels max** (`domain/package`); a third level only for driver
  isolators (`resilience/cache/redis`, `msg/jobqueue/sqlbroker`).
- **Leaf directory = package name**, unique across all domains (no forced
  import aliasing). No packages at the repository root.
- **Admission test:** name the package's *purpose* in one sentence — it must
  end with exactly one domain noun. If it plausibly fits two domains, the
  tie-breaker is who imports it.
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
  for sessions/idempotency, so those consumers need `cache/redis` (or bring a
  durable Store of their own). Consumers: `session`, `idempotency`, `otp`,
  `lockout`, server-side `flash`. No package defines a private byte-KV store.
- **Counter seam** — windowed atomic counters (Incr-within-window) can't ride
  Get/Set KV without races. `ratelimit` owns the counter `Store` contract;
  `quota` and `lockout` share it. Two store seams total — byte-KV + counter.
- **Cross-broker messaging seam** — `jobqueue.Broker` (`Push/Claim/Ack/Nack`).
  A consumer wanting NATS/Kafka implements Broker in their own repo;
  eventbus/scheduler ride it unchanged. Fleet services may share the
  "messaging Postgres" queue schema as the sanctioned intra-fleet event bus.
- **Delivery-semantics rule** — `msg/` = durable, at-least-once,
  Postgres-backed, survives restarts (competing consumers). `realtime/` =
  ephemeral, at-most-once fan-out to currently-connected clients.
- **Fleet error contract** — problem+json is the RPC error contract between
  services: `problem` carries a machine-readable `Code`, decodes responses,
  and matches via `errors.Is`; `httpclient` surfaces decoded Problems.

## Tiers

- **Roadmap** (core / recommended) — committed, listed in build order below.
  *core* = nearly every app needs it; *recommended* = most apps.
- **Icebox** — plausible and pre-scoped, but built only on first concrete
  demand. Not a commitment.

## Target tree

```
forge/                              # single go.mod at the root
│
├── core/          # type & value primitives, zero forge deps
│   # shipped: clock random id ctxkey typeconv structfields slicex mapx set
│   #          ptr null enum stringsx errorsx iox bufpool encoding bytesize
│   #          filetype validate sanitize slug decimal money
│   # icebox:  useragent qrcode namegen
│
├── crypto/        # security primitives
│   # shipped: consttime digest sign secret keyset kdf password token redact
│   # icebox:  jwtverify
│
├── resilience/    # failure handling, concurrency, load control
│   # shipped: backoff retry singleflight parallel circuitbreaker
│   #          cache (cache/redis) ratelimit (ratelimit/redisstore)
│   # planned: quota (quota/pgstore) loadshed lock (lock/pglock)
│
├── web/           # HTTP in and out: transport, responses, boundary security, client
│   # shipped: httpserver hostrouter middleware request clientip problem
│   #          recoverer reqlog requestid render htmx subroute httpclient
│   #          cookie csrf secheaders cors timeout compress
│   # planned: assets idempotency iplist captcha (captcha/turnstile ...)
│   #          autocert
│   # icebox:  geoip (geoip/maxmind) dnsverify maintenance
│
├── view/          # view-model glue for server-rendered pages
│   # planned: flash form
│   # icebox:  viewhelper
│
├── i18n/          # localization (consumed by views, emails, API errors, jobs)
│   # planned: catalog locale numfmt datefmt
│
├── data/          # persistence
│   # shipped: postgres migration mongo redis opensearch
│   # planned: pagination tenant dataloader objectstore (objectstore/s3)
│   # icebox:  seed imageproc
│
├── msg/           # background work & messaging (one engine, typed facades)
│   # planned: jobqueue (jobqueue/sqlbroker) scheduler eventbus
│   # icebox:  workflow
│
├── realtime/      # server push
│   # planned: sse fanout (fanout/pgbus, fanout/redisbus) ws
│   # icebox:  presence
│
├── auth/          # authn & authz
│   # planned: session (session/pgstore, session/cookiestore) authmw lockout
│   #          totp otp apikey magiclink oauthclient rbac
│   # icebox:  webauthn fingerprint
│
├── comms/         # message-delivery channels
│   # planned: email (email/markdown) webhook
│   # icebox:  sms (sms/twilio) push (push/webpush)
│
├── ai/            # LLM seams
│   # planned: llm (llm/openai, llm/anthropic) prompt
│   # icebox:  structured embeddings aiusage mcpserver
│
├── ops/           # runtime, lifecycle, observability, app bootstrap
│   # shipped: supervisor logger (logger/sentry) config health
│   #          buildinfo automaxprocs logredact bootstrap
│   # planned: diag metrics (metrics/prometheus)
│   #          auditlog (auditlog/pgsink) featureflag cli
│   # icebox:  tracing (tracing/otel) secretsource logsample configwatch term
│
└── testkit/       # black-box test harnesses
    # planned: webtest htmltest dbtest
    # icebox:  golden factory
```

`web/` is deliberately the biggest domain — it owns the entire HTTP boundary.
If it ever stops fitting on a screen the natural split line is inbound
middleware vs response/outbound, but don't pre-split.

### Placement rationale (judgment calls)

| Package | Domain | Why |
|---|---|---|
| `httpclient` | `web/` | Purpose is HTTP; consumed by `oauthclient`, `captcha`, `comms/*`, `llm` alike. Keeps `comms/` pure. |
| `ratelimit`, `quota` | `resilience/` | Core is a keyed limiter/counter; the HTTP middleware is a thin adapter. Load-control purpose beats delivery mechanism. |
| `cookie` | `web/` | Composes crypto, but its purpose is the HTTP boundary (`csrf`/`flash`/`session` consumers are web-side). |
| `render`, `htmx` | `web/` | Response-side transport, symmetric to `request`. `render.JSON` serves pure APIs; a JSON-only API importing from `view/` would read wrong. |
| `session` | `auth/` | Purpose is identity lifecycle over a Store; `web/cookie` is just the codec it composes. |
| `catalog`, `locale`, `numfmt`, `datefmt` | `i18n/` | Localization is consumed by emails, API errors, and jobs — not just HTML views. Standalone-domain argument mirrors `ai/`. |
| `form` | `view/` | Exists for server-rendered CRUD: whole-form decode, sticky values, render-friendly `Errors`. `web/request` = per-field typed reads for APIs. |
| `sse` | `realtime/` | Push transport, stdlib-only; the htmx `SendComponent` bridge lives in `web/htmx`. |
| `webhook` | `comms/` | Outbound signed delivery + inbound verification are two sides of one webhooks feature; communication purpose owns it, the inbound middleware is a thin adapter. |
| `fanout` | `realtime/` | Named to keep it distinct from `jobqueue.Broker` — ephemeral fan-out vs durable claim/ack are opposite delivery semantics. |
| `autocert` | `web/` | Exists to wire `httpserver` TLS. |
| `geoip` | `web/` | A lookup, not communication — "clientip gives the address, geoip gives its meaning". |
| `msg/` naming | — | Rejected `jobs/` (undersells eventbus), `async/` (collides conceptually with `resilience/`), direction names like `outbound/` (direction doesn't discriminate). |
| `ai/` standalone | — | Enough packages to stand alone; burying them in `comms/` would make that folder a junk drawer. |

---

## Catalog

Shipped packages are listed by name only — their `doc.go` is the reference.
Planned entries: **name** — tier. Scope. Icebox entries get one line.

### core/ — complete

Shipped: `clock random id ctxkey typeconv structfields slicex mapx set ptr
null enum stringsx errorsx iox bufpool encoding bytesize filetype validate
sanitize slug decimal money`.

Icebox:

- `useragent` — stdlib User-Agent string parser (browser/OS/device/bot); feeds
  session device lists and auditlog display. String-in primitive, hence core.
- `qrcode` — QR PNG / base64 data-URI from any string (vendored encoder):
  2FA enrollment URIs, referral/share links, crypto wallet addresses.
  General-purpose brick; first consumer is `totp`, build alongside it.
- `namegen` — adjective-noun random names over `random` (default workspace
  names, Docker/Vercel pattern).

### crypto/ — complete

Shipped: `consttime digest sign secret keyset kdf password token redact`.

Icebox:

- `jwtverify` — verify-only JWT: RS256/ES256/EdDSA allowlist (pinned, never
  negotiated), exp/nbf/aud/iss checks, static-key + JWKS-URL sources with kid
  cache/rotation. No signing, no JWE. Serves inter-service auth, satisfies
  `authmw.Verifier`, and replaces the verifier otherwise buried in
  `oauthclient`.

Recipe owed: HKDF compound-key per-tenant encryption (master key × tenant ID
→ per-tenant `secret.Box`; delete tenant key = GDPR crypto-shred) as a `kdf`
doc.go example.

### resilience/

Shipped: `backoff retry singleflight parallel circuitbreaker cache (cache/redis)
ratelimit (ratelimit/redisstore)`.

- **`quota`** (+`quota/pgstore`) — recommended. Cumulative usage caps per
  subject over calendar/rolling windows (requests/month, seats, AI tokens) —
  the plan-entitlement counterpart to ratelimit. Subject is an opaque string;
  limit resolver is caller-owned (no billing coupling). `quota/pgstore` is
  the durable backend.
- **`loadshed`** — recommended. Adaptive admission control: pluggable
  `Criteria` (concurrency + latency in core; CPU stays consumer-side),
  fail-open sampler, probabilistic rejection ramp. `Middleware()` +
  `Acquire()` for non-HTTP admission.
- **`lock`** (+`lock/pglock`) — recommended. Distributed mutex: TTL leases,
  fencing tokens, auto-refresh; `RunAsLeader` as `supervisor.Service`.
  In-process store in core; Postgres advisory locks are the only shipped
  distributed backend (a Redis consumer implements the 3-method Store).

### web/

Shipped: `httpserver hostrouter middleware request clientip problem recoverer
reqlog requestid render htmx subroute httpclient cookie csrf secheaders cors
timeout compress`. (`httpclient` builds a resilient outbound `*http.Client`
via a RoundTripper stack: per-attempt timeout, jittered retry, an OPT-IN
per-host circuit breaker, before/after hooks, and ctx-driven header
propagation — the transport under captcha, oauthclient, comms/*, and llm.
Returns the stdlib type.) (`cookie` is the signed + encrypted cookie codec
`csrf`/`flash`/`session` compose.)

- **`assets`** — recommended. Static serving over `fs.FS` with correct
  content types, range support, ETag/304 handling, and cache headers; a
  fingerprint-manifest mode (`URL()`/`Integrity()`, immutable far-future
  headers) and SPA fallback. Not a bundler.
- **`idempotency`** — recommended. Idempotency-Key middleware: replays the
  stored first response on retry, rejects key reuse with a different payload
  fingerprint. Store rides `cache.Store`'s atomic SetNX.
- **`iplist`** — recommended. IP/CIDR allow/deny middleware over `clientip`
  (admin allowlists, blocklists).
- **`captcha`** (+`captcha/turnstile` …) — recommended. Server-side CAPTCHA
  verification behind a `Verifier` seam; providers are thin POST+JSON
  adapters over httpclient, no SDKs.
- **`autocert`** — recommended. ACME/Let's Encrypt TLS via
  `x/crypto/acme/autocert` wired as `tls.Config` + HTTP-01 handler for
  httpserver.

Icebox:

- `geoip` (+`geoip/maxmind`) — IP → country/region/ASN behind a `Source`
  seam; header source (CF-IPCountry) stdlib in core.
- `dnsverify` — DNS TXT-record domain-ownership verification behind a
  `net.Resolver` seam; the missing leg of the hostrouter + autocert
  custom-domain onboarding story.
- `maintenance` — 503 + Retry-After kill-switch with bypass list; the
  featureflag + problem recipe covers most cases.

### view/

- **`flash`** — core. One-shot messages surviving a redirect over a pluggable
  Store; signed-cookie store built in (needs `cookie`); server-side store
  rides `cache.Store`. For PRG and htmx flows.
- **`form`** — core. Whole-form decode into structs (reflection confined to
  `structfields`) + sticky `Values` + render-friendly `Errors` carrying
  `validate`'s i18n keys/params (translated by `i18n/catalog`), plus
  error-class/aria/sticky-value view helpers. Backbone of server-rendered CRUD.

Icebox: `viewhelper` — dep-free templ helpers: `Classes`, `If[T]`, `Default`,
`QuerySet` (truncate/pluralize live in shipped `stringsx`).

### i18n/

- **`catalog`** — core. Message catalog + fs.FS/JSON loaders + curated CLDR
  plural rules in one package; `T(loc, key, args...)` selects plural forms
  internally; fallback chains; translates `validate` violation keys. Zero
  deps, no x/text, no YAML.
- **`locale`** — core. Accept-Language negotiation (q-values, region
  fallback) + context carrier + middleware with a resolver chain
  (cookie → query → Accept-Language → default) + `logger.ContextExtractor`.
- **`numfmt`** — recommended. Locale-aware number/currency/percent
  formatting; `Currency(money.Money)` (shipped `money` renders locale-free
  by design and defers here).
- **`datefmt`** — recommended. Locale + timezone date/time and relative-time
  ("3 hours ago") formatting with named presets. Gregorian only.

### data/

Shipped: `postgres migration mongo redis opensearch`. (`postgres` owns the
transaction story: `WithTx`/`WithTxRetry` plus the planned
`WithTxInContext`/`TxFromContext`/`Querier` DBTX helpers; rows→struct mapping
is pgx `CollectRows`/sqlc — no forge wrapper.)

- **`pagination`** — recommended. Opaque cursor codec (base64+JSON, optional
  HMAC via `sign`) + keyset WHERE/ORDER fragment builders emitting
  pgx-compatible `(sql, args)` + `Page[T]` metadata + the page-window
  view-model (ellipses window, links preserving query params) for
  server-rendered navigation. Stdlib-only; inbound parsing already lives in
  `request.QueryPage`/`QueryCursor`.
- **`tenant`** — recommended. Tenant ID on request context + explicit
  parameterized `ScopeClause` fragments — visible at every query, never
  auto-injected. Deps: `ctxkey` only.
- **`dataloader`** — recommended. Generic per-request batch-and-cache loader
  collapsing N+1 lookups; pure generics, no DB imports; batch fn is
  caller-owned.
- **`objectstore`** (+`objectstore/s3`) — recommended. Blob `Store` seam with
  a path-traversal-safe disk adapter; S3 adapter on aws-sdk-go-v2. Magic-byte
  MIME validation via `filetype`, tenant key-prefix scoping, presigned URLs.

Icebox:

- `seed` — idempotent named-Seeder runner + `app seed` cli.Command; mirrors
  `migration`'s shape (which is why it lives here, not in testkit).
- `imageproc` — decode-limit-guarded resize/crop/re-encode over
  `x/image` (avatar/logo thumbnails in the upload → process → store pipeline).

### msg/

- **`jobqueue`** (+`jobqueue/sqlbroker`) — recommended. THE durable engine:
  supervised worker pool (bounded concurrency, per-job retry/backoff,
  graceful drain), claim-with-lease at-least-once, max-attempts →
  dead-letter; typed handlers over JSON; producer `Client` separate from the
  worker Service; delayed jobs via `WithRunAt`. In-memory broker in core;
  `sqlbroker` = SKIP LOCKED on pgx with LISTEN/NOTIFY wakeups.
  `Broker{Push/Claim/Ack/Nack}` is the exported cross-broker seam.
- **`scheduler`** — recommended. Cron/interval `supervisor.Service` that
  *enqueues* into the engine when due; fires once per fleet via a
  `unique(name, scheduled_for)` insert race (sqlbroker only — in-memory mode
  is single-instance/test-only). Local ~150-LOC cron parser, no robfig/cron.
- **`eventbus`** — recommended. Typed events, two modes: sync in-process
  observer (no durability), and durable mode where each named subscription is
  its own SKIP LOCKED queue (publish fans out one row per subscription;
  competing consumers within one; at-least-once). Transactional publish in
  the business `pgx.Tx`; exports the `Seen(ctx, tx, id)` idempotency inbox
  (dedup table owned by the engine migration). Handlers must be idempotent;
  per-key ordering is a future knob.

Icebox: `workflow` — Postgres-checkpointed linear step sequences over the
engine (onboarding/provisioning chains; resume after crash). No DAG, no DSL,
no timers — not a Temporal clone.

Sync commands are direct function calls (anti-scope); async commands are a
jobqueue kind with one registered handler.

### realtime/

- **`sse`** — core. The complete Server-Sent Events package, stdlib-only:
  typed event constructors (string/JSON/comment/retry, mirroring forge v1's
  `SSEString`/`SSEJSON`/`SSERetry`), correct framing + headers, per-event
  flush, keep-alive, ctx cancellation — plus the mountable endpoint over
  `fanout`: per-request subscribe, heartbeat, disconnect handling,
  Last-Event-ID resume via the replay ring. The handler an htmx dashboard
  actually mounts; the low-level writer stays exported as the brick under
  `web/htmx`'s SendComponent bridge and `llm.SSE`. Requires httpserver
  WriteTimeout=0 (documented).
- **`fanout`** (+`fanout/pgbus`, `fanout/redisbus`) — core. In-process
  pub/sub hub (bounded buffers, explicit slow-consumer policy) + the `Bus`
  seam for multi-instance backplanes + optional bounded `WithReplay(n)` ring.
  `pgbus` (LISTEN/NOTIFY) is the first distributed driver — multi-instance
  push with zero new infrastructure; `redisbus` takes the caller's
  `data/redis` client.
- **`ws`** — recommended. The complete WebSocket package:
  accept/read/write/ping-pong/close over isolated `coder/websocket` behind
  exported forge `Conn`/`Message`, plus the hub — connection registry, rooms,
  broadcast under supervisor, with production bounds (payload/event-name/
  auth-blob size limits, drop-frames-vs-teardown overflow policy,
  `Shutdown(ctx)` drain).

Icebox: `presence` — who-is-here tracking with TTL heartbeats; consumer code
over fanout + cache until demand appears.

### auth/

- **`session`** (+`session/pgstore`, `session/cookiestore`) — core.
  Server-side session lifecycle (Start/Load/Save/Destroy/Rotate) over a
  pluggable Store; rotate-on-privilege-change. Multi-device management via an
  optional `UserIndex` store extension (ListByUser/DeleteByUser — "log out
  other devices", GDPR deletion); `WithFingerprint(Warn|Strict)` hijack
  detection. In-memory store in core; `pgstore` is user-indexed;
  `cookiestore` is stateless-encrypted (no UserIndex, documented); generic
  KV backing rides `cache.Store`.
- **`authmw`** — core. Request-authentication middleware over a `Verifier`
  seam (session/token/apikey all satisfy it) with chained credential
  extractors (header → cookie → query), built-in Basic Auth (constant-time
  via `consttime`, correct 401 + WWW-Authenticate — gates pprof/metrics/
  staging/admin), and `IdentityFromContext`.
- **`lockout`** — recommended. Login/OTP failure counting with exponential
  delay and lockout windows over the ratelimit counter seam. (Not rate
  shaping — that's `ratelimit`; not cumulative caps — that's `quota`.)
- **`totp`** — recommended. The complete 2FA package: RFC 6238/4226
  TOTP/HOTP secret generation, skew-window verify, otpauth:// provisioning
  URI (~150 LOC, no pquerna/otp; QR rendering via `core/qrcode`), and
  one-time backup codes — generate/hash/verify-and-consume, constant-time
  matching (persistence is consumer DB).
- **`otp`** — recommended. Short numeric codes for email/SMS verification:
  attempt-limited, TTL'd, hashed at rest; generation via `random.DigitCode`;
  delivery is the caller's channel.
- **`apikey`** — recommended. Stripe-style prefixed keys (`sk_live_…`) with
  checksum for cheap rejection and constant-time verify; hash stored,
  plaintext shown once.
- **`magiclink`** — recommended. Signed, TTL'd, single-use links over the
  generic `crypto/token` codec: passwordless login, team invites (role/tenant
  claims as a documented Claims example), verify/unsubscribe links. Stateless
  by default; `WithStore` for single-use redemption. Does not send email.
- **`oauthclient`** — recommended. OAuth2/OIDC client: auth-code + PKCE,
  state, token exchange, id_token/userinfo verification (via
  `crypto/jwtverify` once built; alg-pinned either way), provider presets.
  On net/http, no x/oauth2.
- **`rbac`** — recommended. In-memory role/permission matcher with wildcard +
  hierarchy, hot-reloadable; `RequirePermission(perm)` middleware with the
  401-vs-403 split; a 3-method `Authorizer` seam documented for consumers who
  bring an engine. Subject→roles mapping is consumer DB.

Icebox:

- `webauthn` — passkey registration/assertion over isolated `go-webauthn`
  (CBOR/COSE is the one justified heavy auth dep).
- `fingerprint` — versioned request fingerprint (UA + Accept headers ±IP,
  sha256). Multi-consumer brick: session's hijack detection plus future
  anti-fraud / risk-scoring modules.

### comms/

- **`email`** (+`email/markdown`) — core. The complete email package:
  `Sender` seam + stdlib net/smtp implementation (STARTTLS, multipart,
  attachments) + named-template rendering (subject + HTML + text bodies into
  a `Message`). The markdown subpackage renders markdown + YAML frontmatter
  (subject/preheader, CTA-button extension) — the designer-free
  transactional format; goldmark confined there. Provider adapters
  (SES/Postmark/…) are consumer-side or isolated subpackages.
- **`webhook`** — recommended. The complete webhooks package, both
  directions: outbound HMAC-signed deliveries (Stripe-style `t=,v1=`) with
  timeout and bounded retry (in-process delivery only — durability rides
  jobqueue), and inbound signature-verification middleware
  (Stripe/GitHub/Slack HMAC schemes, constant-time, timestamp tolerance,
  reads and restores `r.Body`). Signing/verifying share one internal scheme
  implementation — never a separate package.

Icebox:

- `sms` (+`sms/twilio`) — `Sender` seam; the twilio adapter is a stdlib
  form-POST over httpclient, never twilio-go.
- `push` (+`push/webpush`) — `Pusher` seam + Web Push (VAPID/ECDH/AES-GCM,
  fully stdlib). FCM/APNs stay consumer-side behind the seam.

### ai/

- **`llm`** (+`llm/openai`, `llm/anthropic`) — recommended. Provider-agnostic
  chat/completion + streaming behind `Completer`/`Streamer` with plain DTOs
  including tool-calling (`ToolDef`/`ToolCall`/`ToolResult`); typed error
  contract (provider status, stable reason, `RetryAfter()`) honored by
  retry/httpclient; `llm.SSE(w, r, chunks)` response bridge and
  `EstimateTokens`/`Fit` budget truncation. Provider subpackages are stdlib
  JSON+SSE HTTP adapters over httpclient — never the official SDKs.
- **`prompt`** — recommended. Type-safe prompt templating from an fs.FS
  registry over text/template with strict missing-key errors. Mechanical
  only — no chains/agents. Not html/template (escaping corrupts prompts).

Icebox:

- `structured` — coerce noisy LLM output into typed values: fence-strip,
  strict JSON decode into T, repair-prompt on failure.
- `embeddings` — `Embedder` seam + stdlib vector math (cosine/top-k) for
  small in-memory corpora; no ANN/persistence.
- `aiusage` — token/cost meter emitting slog attrs; **prices are
  consumer-supplied** — never shipped as library data.
- `mcpserver` — expose explicitly-registered app operations as MCP tools
  (stdio + streamable-HTTP) as `supervisor.Service` / `http.Handler`;
  hand-declared schemas, no reflection; official MCP go-sdk isolated here.

### ops/

Shipped: `supervisor logger (logger/sentry) config health buildinfo
automaxprocs logredact bootstrap`. (`config` —
formerly planned as `envconfig`, shipped renamed — layers app config from
YAML per-env files with `${VAR:default}` substitution, `.env` inheritance,
and env-tagged structs via `structfields` + `typeconv`; exported `Profile`
enum (dev/test/staging/prod) with predicates; boot-time only.
`health` — a single pull-evaluated `Handler()` factory for liveness (no
checks) and readiness (`WithCheck` checks, critical by default, `NonCritical`
degrades instead of 503) plus a `Gate` for ordered pre-shutdown drain via
`supervisor.WithPreShutdown`.
`buildinfo` — value type merging ldflags over `ReadBuildInfo` for
version/commit/build-time/dirty; `fmt.Stringer` + `slog.LogValuer` +
`/version` JSON handler.
`automaxprocs` — sets GOMAXPROCS/GOMEMLIMIT from cgroup CPU/memory quotas at
startup via a local stdlib parser (v2 primary, v1 fallback, no uber dep);
fail-open logged no-op outside containers, honors explicit env vars.
`logredact` — `slog.Handler` wrapper replacing attribute values by leaf key
or dotted group path before they reach the next handler; unconditional at
every level (no bypass knob), redacts `WithAttrs`-baked attrs and tracks the
`WithGroup` prefix. `crypto/redact` covers values you wrap; this is the
safety net for attrs you don't control.
`bootstrap` — thin runtime integrator (replaces the planned `appmain` name):
`Run` / generic `RunWithConfig[T]` wire logger → automaxprocs → build-info
log → config autoload → signal context (via `supervisor.NewContext`) → exit
code; the callback owns `supervisor.Run` and `defer` cleanup. NOT a DI
container. This bundle also added `supervisor.WithContext(parent)` so the
signal context can root at a caller's context.)

- **`diag`** — recommended. One internal diagnostics surface:
  `/debug/pprof/*`, `/debug/stats` (runtime/GC/goroutines JSON),
  `/debug/vars`, with an auth guard and a dedicated-port
  `supervisor.Service`.
- **`metrics`** (+`metrics/prometheus`) — recommended. Counter/Gauge/
  Histogram `Recorder` facade with an expvar default + request middleware;
  prometheus is the only adapter.
- **`auditlog`** (+`auditlog/pgsink`) — recommended. Append-only structured
  audit events (actor/action/resource/outcome) over a `Sink` seam; slog +
  JSONL sinks in core; `pgsink` adds the pgx insert + keyset-paginated
  per-tenant query every B2B SaaS shows in its UI.
- **`featureflag`** — recommended. Bool/variant flags via a `Provider` seam;
  static + env providers in core; vendor SDKs stay consumer-side.
- **`cli`** — recommended. Struct-described command tree over stdlib
  `flag.FlagSet`, ctx-aware Run, auto help; no cobra, no global registry.
  Covers serve/migrate/worker/seed/version.

Icebox:

- `tracing` (+`tracing/otel`) — Tracer seam, W3C traceparent middleware,
  trace_id log extractor; OTel isolated. Pairs with httpclient propagation.
- `secretsource` — hot-reloadable secrets behind a `Provider` seam, exposing
  `redact.Secret` values; cloud clients consumer-side.
- `logsample` — slog.Handler that rate-samples high-volume records.
- `configwatch` — poll-based config reload with atomic snapshot swap, as
  `supervisor.Service`.
- `term` — writer-injected terminal I/O (prompts/spinners/tables); a confirm
  prompt inside cli is ~20 LOC, so this waits for real demand.

### testkit/

- **`webtest`** — recommended. Black-box HTTP harness: real `:0` server from
  an `http.Handler`, fluent request builder, `testing.TB` response asserts.
  (Named to avoid shadowing stdlib httptest in every consumer test file.)
- **`htmltest`** — recommended. CSS-selector DOM assertions over webtest
  responses (text/attr/count/exists, form-field values, htmx fragments);
  goquery as a test-only dep. Without it the templ+htmx app type can only
  string-match HTML.
- **`dbtest`** — recommended. pgx test helpers: per-test tx-rollback
  isolation, ephemeral schema, Postgres template-DB clone. No testcontainers.

Icebox: `golden` — golden-file snapshots with `-update` + testdata fixture
loading; `factory` — generic closure-override test-data builders.

---

## Shipped-package work items

Queued API additions to shipped packages (each unblocks roadmap work):

| Package | Addition |
|---|---|
| `web/htmx` | SSE `SendComponent` bridge — deferred to the realtime/sse wave |

## Build order

Each wave depends only on earlier ones. Icebox packages slot in wherever
demand appears.

1. **Web boundary + ops glue** — `assets`, `iplist`, `autocert`, `captcha`,
   `idempotency`; `diag`, `metrics`, `featureflag`, `cli`.
2. **Data + messaging** — `pagination`, `tenant`, `dataloader`,
   `objectstore`; `jobqueue` → `scheduler`, `eventbus`; `lock`, `loadshed`,
   `quota`; `auditlog`.
3. **Auth** — `session`, `authmw`, `lockout`, `totp`, `otp`, `apikey`,
   `magiclink`, `oauthclient`, `rbac`.
4. **Views + i18n + delivery** — `flash`, `form`; `catalog`, `locale`,
   `numfmt`, `datefmt`; `email`, `webhook`.
5. **Realtime + AI** — `fanout` → `sse`, `ws`; `llm`, `prompt`.
6. **Test harnesses** — `webtest`, `htmltest`, `dbtest`. Deliberately last:
   built against the full, stable package surface so the harness APIs are
   shaped by real usage across every domain instead of guesses that would
   churn as later waves land.

**Minimal-core cut-line** (enough for a real API or htmx app end-to-end):
`clock random id ctxkey typeconv slicex ptr validate` · `postgres migration`
· `backoff retry ratelimit` · `middleware recoverer requestid problem
httpclient` · `session authmw` · `sse fanout` · `email` · `catalog locale` ·
`flash form` · `config health`.

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
- **OIDC provider** (issuing tokens to third parties) — a product; run
  Hydra/Keycloak. Forge ships the client side.
- **JWT issuing/signing, JWE, alg negotiation** — historically vuln-prone;
  verify-only is in scope via `crypto/jwtverify`; `token` covers internal
  needs.
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
- **Policy engines** (ABAC/casbin/OPA) — consumers write predicates or bring
  an engine behind rbac's `Authorizer` seam.
- **Long-poll transport** — SSE reconnect covers it; hand-roll over fanout if
  truly needed.
- **Sync command bus** — a command with one handler is a function call.
- **Cross-broker pub/sub packages** (Kafka/NATS/socket.io/WebTransport) —
  implement `jobqueue.Broker` or `fanout.Bus` in the consumer repo.
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
413) · maintenance 503 gate (featureflag + problem) · breadcrumb value type ·
notification-inbox pipeline · per-tenant encryption / GDPR crypto-shred (kdf)
· optimistic locking + audit columns (postgres docs) · Postgres tsvector
search · usermanager flow.
