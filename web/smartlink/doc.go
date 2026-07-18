// Package smartlink is the link package: a destination-decision engine plus
// a storage-backed manager and redirect handler built on top of it. A
// stored [Link] is a short code bound to either an inline URL template
// ([Link.Target]) or a consumer-side offer reference ([Link.Ref]) that
// resolves to a compiled engine value; a fixed-destination short link is
// simply the degenerate case — a single default target, no rules — with
// every ability intact: macros, param forwarding, metadata stamping, hooks.
//
// # Engine
//
// [Compile] validates a consumer-hydrated [Spec] fail-fast — ordered rules
// of typed matchers ([Geo], [Device], [Locale], [ParamEquals],
// [TimeWindow], [Percent]) evaluated over a caller-built [Visit], first
// match wins, with a mandatory default target — and returns an immutable
// [Compiled]; [Compiled.Decide] is the infallible per-click hot path. Rule
// values are consumer data hydrated into the typed vocabulary — there is no
// DSL, and rule storage/admin, target health checks, and bot filtering stay
// consumer-side. The engine never imports net/http: the caller builds the
// Visit from its own request facts (web/clientip + web/geoip for country,
// web/useragent for device) and emits the returned [Decision] as the click
// event.
//
// Weighted splits and [Percent] shares bucket deterministically by FNV-1a
// hash of [Spec.Salt], the rule name, and Visit.StickyKey — never RNG — so a
// visitor always lands on the same side while distinct links bucket
// independently ([Manager] salts by link code, [Cache] by ref). Target
// URLs are templates over a fixed
// macro vocabulary ({country}, {device}, {locale}, {param.NAME}) parsed at
// compile time: an unknown macro is a construction error, never an empty
// substitution at decide time. Macro values escape positionally (authority
// vs path vs query), so they can never alter the URL structure, and
// [ParamPolicy] controls merging Visit.Params into the final URL. A Target
// whose authority (host) contains a {param.NAME} macro fed by a
// visitor-controlled query parameter is a creator opt-in open-redirect
// surface — the visitor chooses the destination host outright. A creator
// who must keep the host out of visitor control should instead pin the
// host-feeding param via the Link's Metadata, since Metadata wins the merge
// into Visit.Params and so overrides whatever the query string supplies.
//
// [Decider], [DecideFunc], and [Decorator] let a consumer wrap a [*Compiled]
// (or a [Cache]-backed [Resolver]) with concerns that must affect the
// current click, composed left-to-right with [Chain]. [NewCache] is a lazy
// compile-with-invalidation cache for Ref-backed links: it loads and
// compiles a consumer's [Spec] on demand, keyed by ref string, and the
// consumer calls [Cache.Invalidate] from its own offer-save path.
//
// # Manager and Handler
//
// [NewManager] builds a management surface over a [Store] ([NewMemoryStore]
// for tests/dev; the pgstore subpackage for production): [Manager.Create]
// mints a Link (vanity or generated code via [WithCodeFunc], Target
// (scheme-allowlisted via [WithSchemes]) xor Ref (optionally prechecked via
// [WithResolver])), and [Manager.Deactivate], [Manager.Activate], and
// [Manager.Delete] drive its lifecycle. [Manager.Handler] serves the
// uniform per-click pipeline for both Target and Ref links: resolve the
// code (cache read-through via [WithCache], liveness checks) — a dead or
// unknown code redirects to [WithFallbackURL] or answers 404 — build
// [Visit] from the query string and [WithVisitFunc], merge the link's
// Metadata into Visit.Params (metadata wins — it identifies the link, not
// the click), decide (a per-hit degenerate compile for Target links under
// [WithLinkParamPolicy], the configured [WithResolver] for Ref links),
// redirect (302 or 307 via [WithRedirectStatus] — 301 is rejected, since a
// cached permanent redirect would kill hit observation forever, and every
// response carries Cache-Control: no-store), and call [WithOnHit]
// synchronously after the response is written. A store or resolver outage
// answers 500 — an outage must read as an outage, not as every link being
// gone. A cache outage never surfaces: any cache error or decode failure is
// treated as a miss and falls through to the Store (logged at debug), so a
// bad cache backend degrades to "always hit the Store", not to failures.
//
// # Hooks
//
// Decorators (composed with [Chain]) are the synchronous seam: they run
// inside Decide and can change the outcome of the current click — a fraud
// guard diverting a suspicious visit, an A/B override, an inline metrics
// tap. [WithOnHit] is the asynchronous observer: it runs after the redirect
// is already written and cannot change it. It must hand the [Hit] to a
// bounded sink — a queue push, a buffered channel — and never do work
// inline or spawn a goroutine per hit; a slow or unbounded [WithOnHit]
// blocks or leaks on every click.
//
// # Tenancy
//
// [WithScope] scopes management operations only ([Manager.Create],
// [Manager.Get], [Manager.List], [Manager.Deactivate], [Manager.Activate],
// [Manager.Delete]), failing closed with [ErrScope] on a hook error or an
// empty result. [Manager.Resolve] and [Manager.Handler] never consult it: a
// short code is a public URL, and codes are globally unique regardless of
// tenant. Without [WithScope], tenant strings pass through verbatim — zero
// ceremony for single-tenant apps.
//
// # Anti-scope
//
// This package is not fraud/bot/dup detection (a consumer decorator or
// [WithOnHit] consumer owns that); not postback delivery, click counting, or
// analytics storage ([Hit] hands the consumer everything it needs;
// comms/postback owns tracker postbacks); not offer/spec persistence (rule
// storage/admin stays consumer-side — [NewCache] only caches compiles); and
// not QR codes, link-in-bio, or preview pages. It is also not web/hostrouter
// (inbound hosts), not ops/featureflag ("is X on for subject"), and not
// auth/magiclink (a self-contained signed token, not a redirect) — this
// package selects and serves outbound destinations from a stored code.
package smartlink
