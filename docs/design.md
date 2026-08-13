# Forge — Design Rules

> The framework constitution: packaging rules, idioms, dependency policy,
> seams, performance rules, and anti-scope. The package catalog & roadmap live in
> [packages.md](packages.md). Forge targets SaaS applications: it ships the
> ~99% of boilerplate every SaaS repeats (tenancy, authentication,
> authorization, background work, webhooks, audit, rate limiting,
> idempotency) as composable packages — business logic stays in consumer
> repos.

## Focus priorities

Every design decision, review, and trade-off is weighed in this order:

1. **Performance** — speed, allocations, memory footprint, CPU usage. Forge code sits on every consumer's hot path; regressions here are the most expensive to claw back (see [Performance rules](#performance-rules--hot-path-hygiene)).
2. **Developer experience** — less boilerplate for the consumer: sane defaults, `New(...Option)` with env-loadable config, zero ceremony for the common case, APIs that are hard to misuse.
3. **Everything else** — feature breadth, internal elegance, driver coverage, etc. Never trade points 1–2 for these.

When priorities collide, the higher one wins — e.g. a slightly less "clean" internal implementation is acceptable if it removes consumer boilerplate or measurably cuts allocations.

## Design DNA every package follows

- **No magic:** no reflection (one sanctioned helper, `structfields`), no
  service containers; values via params, not context (context only for
  request-scoped reads). Public methods never return unexported types.
- **One of three idioms:** stateless **free-funcs** (`render`, `htmx`) ·
  `New(...Option)` with an env-loadable `Config` + `DefaultConfig` +
  `Validate` (`logger`, `httpserver`) · a `supervisor.Service`.
- **Invalid options are reported, never panicked.** An `Option` records the
  problem on the config (`c.errs = append(...)`) instead of panicking, and the
  constructor returns `errors.Join(c.errs...)`. Errors accumulate, so one call
  names every bad value instead of stopping at the first. A `supervisor.Service`
  is the one exception: its `New` cannot fail, so `Run` returns the joined
  errors before doing any I/O. Panic only where a value is a schema mistake the
  compiler could almost have caught — a metric registered twice under two
  kinds, a label-count mismatch — never for a value a deployment supplies.
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
- Single-responsibility packages (~250–850 LOC); black-box tests only —
  white-box (`package foo`) test files are the narrow exception for pinning
  unexported primitives' behavior directly (differential/oracle tests of
  internal fast paths, decoder primitives).
- **Test doubles live with the seam owner** (`clock.Mock`, cache's memory
  store, queue's in-memory broker) — there is no central fakes package.
- **Two test tiers.** The default `go test ./...` is unit-only: fast, no
  Docker, no network. DB-backed tests carry `//go:build integration` and run
  via `just test-integration` (a dedicated CI job). They provision real
  Postgres/Redis/Mongo/ClickHouse/OpenSearch through the `testkit/*test`
  helpers (testcontainers, one container per package process, Ryuk-reaped); a
  `FORGE_TEST_<BACKEND>_*` env var overrides the container to target an existing
  server. ClickHouse and OpenSearch are integration-only (no in-process option).
- **Env prefixes are baked into tags:** every env-loadable `Config` carries
  its package prefix in the tag (`COOKIE_KEYS`, `SERVER_ADDR`, `DB_URL`).
  Nest untagged to keep default names; nest tagged (`env:"APP"`) to
  separate instances (`APP_COOKIE_KEYS`).

## Performance rules — hot-path hygiene

Forge code runs on every consumer's request/job hot path, so packages are
allocation-conscious by default. The meta-rule outranks everything below:
**readable first — optimize only what a benchmark or profile proves hot, and
any perf-motivated complexity requires a benchmark in the PR proving it.**

- **Allocation discipline:** preallocate slices/maps when size is known or
  estimable (`make([]T, 0, n)`); `strings.Builder` over `+=` in loops; no
  `[]byte(s)`/`string(b)` round-trips on hot paths (each one copies);
  `strconv` over `fmt.Sprintf` for simple conversions; `sync.Pool` for
  per-request buffers only where a benchmark justifies it. Zero allocs is
  the target for parse/encode/middleware hot paths (`useragent` precedent).
- **Hot structs:** keep pointer-free where practical to reduce GC scan
  work; field layout is enforced by betteralign (`just lint`).
- **Loops:** hoist invariants out; `regexp.MustCompile` at package level
  only; honor `ctx.Done()` in every loop and worker.
- **Bounded concurrency:** never spawn unbounded goroutines — worker pools
  or semaphores with configurable bounds; bounded queues + backpressure
  over unbounded buffering.
- **Lock hygiene:** never hold a mutex across I/O or channel ops; plain
  `sync.Mutex` unless reads vastly outnumber writes; mutexes for simple
  shared state, channels for pipelines.
- **I/O:** every external call takes a context and an explicit timeout with
  a safe default; clients and pools are created in `New` and reused, never
  per-call; wrap network/file I/O in bufio; stream with `io.Copy` /
  `json.Decoder`; never read unbounded input into memory — cap request
  bodies (`http.MaxBytesReader`).
- **Logging:** slog attrs only, no eager formatting; guard expensive
  attribute computation with `Enabled`/level checks.

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
  (`logger/sentry`, `metrics/prometheus`, `tracing/otel`) so it stays a
  swappable leaf.

Isolated deps today: `pgx`, goose (`migration`), the mongo/redis/opensearch/
clickhouse clients (connection factories in `data/*`), sentry,
`gopkg.in/yaml.v3` (`ops/config`'s YAML loader). Sanctioned for the roadmap:
`coder/websocket` (`websocket`), `go-webauthn` (`webauthn`), `x/crypto`
(password/kdf/autocert), goldmark (`email/markdown`), the official MCP
go-sdk (`mcpserver`), `x/image` (`imageproc`), prometheus client
(`metrics/prometheus`), OTel SDK (`tracing/otel`), goquery (`htmltest`,
test-only). **Storage stays behind seams**: every Store/Broker/Bus/Sink
seam ships an in-memory implementation plus a conformance suite; persistent
drivers live in consumer repos. `data/*` provides connection factories and
health checks only; everything else outside `data/*` stays stdlib.

## Repository layout rules

- **Single Go module.** One `go.mod` at the root. No sub-modules, no
  `go.work`. (The tree is module-split-ready if dependency hygiene ever
  becomes a real consumer complaint — a domain folder can be promoted without
  moving files.)
- **Group by purpose, not by layer, tier, or build phase.** Folder names are
  domain nouns (`crypto`, `web`, `data`, `async`).
- **Two levels max** (`domain/package`); a third level only for driver
  isolators (`ops/metrics/prometheus`, `ops/logger/sentry`), conformance
  suites (`async/queue/brokertest`), and colocated codegen
  (`web/useragent/gen`).
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
- **The layout rules are enforced by `tests/architecture_test.go`**, which reads
  `go list -json` and fails the build on a `core/` package that names another
  domain or an unlisted third party, a `crypto/` package above `core/`, a
  `data/` package that names a product domain, a third path level outside the
  sanctioned isolator/suite/codegen set, a leaf directory that disagrees with
  its package name, a duplicated leaf name, or a `testkit/` import outside a
  test file. Adding a rule to prose means adding it there too.
- **The roadmap lists only unbuilt packages.** `packages.md` is the backlog;
  the moment a package ships, delete its entry — a shipped package's `doc.go`
  / godoc is its reference, not the roadmap. The catalog carries no shipped
  markers: if a package is listed, it does not exist yet.

## Framework-wide seams

- **TTL-KV seam** — `resilience/cache.Store` (byte-level Get/Set-TTL/Delete
  + atomic SetNX claim) is THE key-value seam. The shipped memory store is
  LRU-evicting and unsuitable for sessions/idempotency, so those consumers
  bring a durable Store implementation of their own (Redis is the usual
  backend). Consumers: `session`, `idempotency`, `otp`, `lockout`,
  server-side `flash`. No package defines a private byte-KV store.
- **Counter seam** — windowed atomic counters (Incr-within-window) can't ride
  Get/Set KV without races. `ratelimit` owns the counter `Store` contract;
  `quota` and `lockout` share it. Two store seams total — byte-KV + counter.
- **Broker seam** — `queue.Broker` (`Push/Claim/Ack/Nack` + optional
  capability discovery, e.g. native delay) is the cross-backend messaging
  seam; `scheduler`, `eventbus`, and `outbox` ride it unchanged. The
  **engine, not the driver, owns the hard semantics** — retry/backoff,
  delayed jobs, max-attempts → dead-letter, idempotency inbox — so app
  behavior is identical across every backend; the engine uses a driver's
  native capability when declared. Drivers are consumer-side and must pass
  `queue/brokertest`. Transactional publish (`PushTx`) is native on drivers
  whose store has real multi-document ACID transactions; drivers without
  them get it via `async/outbox`.
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

- **Persistent storage drivers** — no package ships a Postgres/Redis/Mongo
  store implementation. Every Store/Broker/Bus/Sink seam ships the memory
  implementation plus a conformance suite (`storetest`, `brokertest`);
  consumers implement the seam in their repo and prove it with the suite.
  `data/*` connection factories and `data/migration` stay — they open and
  health-check connections, they never own schema or queries.
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
- **Product analytics / event tracking** — PostHog/Segment, or
  `async/eventrouter` feeding the consumer's warehouse: forge ships the
  egress engine and reference adapters only — a Segment-style destination
  catalog stays out forever.
- **In-app notification inbox** — consumer domain; recipe: Postgres insert →
  fanout → sse.
- **SEO artifacts** (sitemap/robots/meta) — consumer templates over
  render/assets.
- **Read/write replica routing** — infra (pgbouncer/RDS proxy).
- **External policy engines** (casbin/OPA) — plug in behind the
  authorization decision seam; forge's own `rbac`/`acl`/`abac` cover native
  needs.
- **Expression engines / stored text formulas** (cel-go/expr/govaluate) —
  user-typed expressions evaluate in float and can't explain a payout;
  `finance/formula` covers structured staged-linear specs, and anything
  beyond them is a registered Go function.
- **Long-poll transport** — SSE reconnect covers it; hand-roll over fanout if
  truly needed.
- **Sync command bus** — a command with one handler is a function call.
  Async commands are a queue kind with one registered handler.
- **Logger adapters** (zap/zerolog bridges) — forge is slog-only.
- **Scaffolding / code generators** — value lives in consumer-owned
  templates.
- **Testcontainers as a consumer concern** — adopted in-repo instead: the
  integration test tier provisions real backends via the `testkit/*test`
  helpers (see Testing rules above).
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
`Classes` toggle helper · click-capture pipeline (clientip/geoip/useragent
enrichment → outbox → eventrouter).
