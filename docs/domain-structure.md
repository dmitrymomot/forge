# Forge — Domain Folder Structure

> Target repository layout that replaces the flat top-level package list with
> purpose-named domain folders, plus the migration plan to get there.
> **Status: implemented** — the 57 shipped packages now live in the seven
> domain folders below (`core/ crypto/ resilience/ web/ ui/ data/ ops/`). The
> migration plan is retained as the record of how the move was performed.

## Why

The flat layout works at ~60 packages but fails at the roadmap scale
(~175 packages per [maximal-package-set.md](maximal-package-set.md)): 200+
top-level directories are impossible to scan, and discoverability ("where is
email sending?") degrades to grep. Grouping lives in the filesystem, stdlib
style: `net/http`, `crypto/aes`, `encoding/json`.

## Decisions (settled)

- **Single Go module.** One `go.mod` at the root. No sub-modules, no
  `go.work`, no separate repos.
- **Group by purpose, not by layer, tier, or build phase.** Folder names are
  domain nouns (`crypto`, `web`, `data`), never adjectives ("outbound") or
  process labels ("P0", "core-tier"). Direction/tier/build-order are
  attributes, not taxonomy — they stay in docs.
- **Two levels max.** `domain/package`. A third level exists only for driver
  isolators that wrap a real dependency (`data/kv/redis`,
  `msg/jobqueue/sqlbroker`, `ops/logger/sentry`) — the existing
  `logger/sentry` pattern.
- **Leaf directory = package name.** Import
  `github.com/dmitrymomot/forge/crypto/token`, call `token.Issue(...)`.
  Call sites never change. Leaf names stay unique across domains to avoid
  forced import aliasing.
- **No packages at the repository root.** Every package lives in a domain.
- **Build order ≠ folder layout.** The P0–P7 phases in
  maximal-package-set.md remain the build/dependency plan; this document owns
  only where files live.

## Target tree

Shipped packages are listed under `shipped:` with their current names;
`planned:` entries use the roadmap names from maximal-package-set.md (minus
dropped/folded entries: `bind`, `realip`, `pubsub`, `outbox`).

```
forge/                              # single go.mod at the root
│
├── core/          # type & value primitives, zero forge deps
│   # shipped: clock random id ctxkey typeconv structfields slicex mapx set
│   #          ptr null enum stringsx errorsx iox bufpool encoding bytesize
│   #          filetype validate sanitize slug decimal money
│   # (roadmap P0 is essentially complete)
│
├── crypto/        # security primitives
│   # shipped: consttime digest sign secret keyset kdf password token redact
│
├── resilience/    # failure handling, concurrency, load control
│   # shipped: backoff retry singleflight parallel circuitbreaker cache
│   # planned: drain lock (lock/pglock, lock/redislock) loadshed ratelimit
│   #          (ratelimit/redisstore) quota
│
├── web/           # HTTP in and out: transport, responses, boundary security, client
│   # shipped: httpserver hostrouter middleware request clientip problem
│   #          recoverer reqlog requestid render htmx
│   # planned: timeout bodylimit compress cors negotiate conditional static
│   #          assets upload proxy idempotency maintenance secheaders cookie
│   #          csrf iplist captcha (captcha/turnstile ...) webhookverify
│   #          httpclient autocert geoip (geoip/maxmind)
│
├── view/          # view-model glue for server-rendered pages
│   # planned: flash form formui pageview breadcrumb viewhelper cspnonce
│
├── i18n/          # localization (consumed by views, emails, SMS, API errors)
│   # planned: i18n i18nload i18nctx i18nplural localematch numfmt datefmt
│   # note: the i18n* prefixes become redundant inside this folder — renaming
│   #       (catalog, load, ctx, plural, locale) is a separate decision from
│   #       placement; decide it during migration or leave as-is
│
├── data/          # persistence
│   # shipped: postgres migration mongo redis opensearch
│   # planned: sqltx txcontext dbreplica scan pagination tenant audit
│   #          optimistic dataloader kv (kv/redis ...) objectstore (objectstore/s3)
│
├── msg/           # background work & messaging (one engine, typed facades)
│   # planned: jobqueue (jobqueue/sqlbroker) scheduler eventbus commandbus
│
├── realtime/      # server push
│   # planned: sse broker ws wshub realtimebus (realtimebus/redisbus)
│   #          longpoll presence streamhandler
│
├── auth/          # authn & authz
│   # planned: session sessionstore authmw throttle totp otp recoverycodes
│   #          apikey magiclink invite oauthclient webauthn rbac abac
│
├── comms/         # message-delivery channels
│   # planned: email emailtemplate sms (sms/twilio) push (push/webpush)
│   #          notify webhook
│
├── ai/            # LLM seams
│   # planned: llm (llm/openai, llm/anthropic) prompt aistream embeddings
│   #          structured aiusage tokenizer
│
├── ops/           # runtime, lifecycle, observability, app bootstrap
│   # shipped: supervisor logger (logger/sentry)
│   # planned: envconfig dotenv profile buildinfo automaxprocs health
│   #          readiness pprof metrics (metrics/prometheus) tracing
│   #          (tracing/otel) featureflag secretsource logsample configwatch
│   #          runtimeinfo auditlog watchdog cli term appmain signalctx
│
└── testkit/       # black-box test harnesses
    # planned: httptest golden fixtures fake dbtest factory seed
```

Domain sizes land at 4–32 packages. `web/` is deliberately the biggest — it
owns the entire HTTP boundary. If it ever stops fitting on a screen there is
a natural split line (inbound middleware vs response/outbound), but don't
pre-split.

### Placement rationale (judgment calls)

| Package | Domain | Why |
|---|---|---|
| `httpclient` | `web/` | Purpose is HTTP; consumed by `auth/oauthclient`, `web/captcha`, `comms/*` alike. Keeps `comms/` pure. |
| `ratelimit`, `quota` | `resilience/` | Core is a keyed limiter/counter; the HTTP middleware is a thin adapter. Load-control purpose beats delivery mechanism. |
| `cookie` | `web/` | Composes crypto, but its purpose is the HTTP boundary (`csrf`/`flash`/`session` consumers are web-side). |
| `render`, `htmx` | `web/` | Response-side transport, symmetric to `request` (inbound). `render.JSON` serves pure APIs with zero UI; `htmx` is HX-* header protocol glue. A JSON-only API importing from `view/` would read wrong. |
| `session` | `auth/` | Uses cookies, but its purpose is identity lifecycle over a Store; `web/cookie` is just the codec it composes. |
| `i18n*`, `numfmt`, `datefmt` | `i18n/` | Localization is consumed by emails, SMS, API error messages, and jobs — not just HTML views. Standalone-domain argument mirrors `ai/`. |
| `form` | `view/` | Decodes inbound `url.Values`, which smells like `web/request` — but it exists for server-rendered CRUD (sticky values, render-friendly `Errors`, pairs with `formui`). Weakest placement in the tree; kept deliberately. |
| `sse` | `realtime/` | Push transport; `view/` stays view-model glue. |
| `autocert` | `web/` | Exists to wire `httpserver` TLS (the roadmap doc filed it under ops). |
| `geoip` | `web/` | A lookup, not communication — "clientip gives the address, geoip gives its meaning". Moving it keeps `comms/` = channels only. |
| `msg/` naming | — | Rejected `jobs/` (undersells eventbus/commandbus), `async/` (collides conceptually with `resilience/`), `outbound/`-style direction names (direction doesn't discriminate — half the framework makes outbound calls). |
| `ai/` standalone | — | 7 packages is enough to stand alone; burying them in `comms/` would make that folder a junk drawer. |

Admission test for new packages: name the *purpose* in one sentence and it
should end with exactly one domain noun. If a package plausibly fits two
domains, the tie-breaker is who imports it (see `httpclient`).

## Migration plan (the 63 shipped packages)

One mechanical PR. Do it **before** more packages land — every move is an
import-path break, and pre-v1 is the window.

### 0. Preconditions

- Work in a **worktree branch**, never the main checkout
  (`/Users/dmitrymomot/Dev/forge` auto-pushes to remote main).
- Clean tree, CI green on main.

### 1. Move packages

`git mv` each shipped package into its domain per the tree above, e.g.:

```sh
git mv retry resilience/retry
git mv httpserver web/httpserver
git mv logger ops/logger          # logger/sentry moves with it
...
```

No file contents change in this step. `examples/` and `docs/` stay where
they are.

### 2. Rewrite imports

Package names are unchanged, so only import paths need rewriting — call
sites are untouched:

```sh
# for each moved package OLD -> DOMAIN/OLD:
grep -rl '"github.com/dmitrymomot/forge/retry"' --include='*.go' . \
  | xargs sed -i '' 's|forge/retry"|forge/resilience/retry"|g'
```

Generate the 63 sed pairs from the tree above (script them; don't hand-type).
Watch for prefix collisions — rewrite longer paths first
(`forge/requestid` before `forge/request`, `forge/redact` before
`forge/redis`… safest is to anchor the closing quote as shown).

### 3. Fix non-Go references

- `justfile` — any path globs or per-package recipes.
- `.github/workflows/*` — path filters, matrix entries.
- `README.md` — package index (rewrite as the domain table).
- `docs/maximal-package-set.md` — add a note pointing here for physical
  layout; the P0–P7 content itself stays valid.
- `examples/*` — import paths.

### 4. Update CLAUDE.md

Replace the "Keep repository structure flat" rule with:

> - Packages live in purpose-named domain folders (see
>   docs/domain-structure.md): max two levels, three only for driver
>   isolators (`data/kv/redis` pattern). No packages at the repo root.
>   Leaf directory = package name; leaf names unique across domains.
>   New packages must name their domain before implementation.

### 5. Add domain doc.go files (optional, recommended)

One `doc.go`-style index comment per domain folder is not possible (folders
aren't packages), so instead add a short `README.md` per domain listing its
packages and admission test — or keep the index solely in the root README.
Pick one home; don't duplicate.

### 6. Verify

```sh
go build ./...
go vet ./...
just lint
go test -race ./...
```

Zero behavior changes expected — any test failure means a botched rewrite,
not a real regression.

### 7. Ship

Standard PR flow: new PR → wait for CI → fix failures → address Claude's
review → merge. Squash-merge recommended (the diff is one logical change).

### Non-goals of the migration

- No API changes, no package renames (`random` stays `random`, not `randx`;
  reconciling shipped names with roadmap names is a separate decision).
- No `pkg/` directory — the roadmap doc's `pkg/*` label for P0 is superseded
  by `core/`.
- No sub-modules and no `go.work`, now or later, unless dependency-graph
  hygiene becomes a real consumer complaint (the tree is module-split-ready
  if that day comes: a domain folder can be promoted to its own module
  without moving files again).
