# `pkg/` Package Review

**Date:** 2026-06-25  
**Scope:** All 31 packages under `pkg/` (directly-imported utility packages).  
**Method:** One deep reviewer per package reading the actual source (impl + tests + `doc.go`/README), assessed against the project design rules in `CLAUDE.md`. Every `high`/`critical` finding was then run through an independent adversarial verifier — **all confirmed real, 0 refuted.**

> **Note:** `pkg/sqlitedb` — the per-connection PRAGMA bug listed below (#6) has since been **fixed** (PRAGMAs now applied via `_pragma` DSN params so they hold across all pooled connections, with a multi-connection regression test).

---

## Summary

| Tier | Packages |
|---|---|
| ✅ **Great as-is** | `geolocation`, `secrets`, `token` |
| 🟡 **Minor polish** | `binder`, `cache`, `clientip`, `cookie`, `fingerprint`, `hostrouter`, `htmx`, `i18n`, `job`, `oauth`, `qrcode`, `randomname`, `ratelimit`, `sanitizer`, `storage`, `webhook` |
| 🔴 **Needs fixing** | `dnsverify`, `id`, `slug`, `validator`, `useragent`, `db`, `logger`, `mailer`, `redis`, `sqlitedb`, `totp`, `jwt` |

### Scores at a glance

| Package | Score | Verdict | Crit | High | Med | Low |
|---|:--:|---|:--:|:--:|:--:|:--:|
| `geolocation` | 9 | ✅ Great as-is | 0 | 0 | 0 | 3 |
| `hostrouter` | 9 | 🟡 Minor improvements | 0 | 0 | 0 | 3 |
| `oauth` | 9 | 🟡 Minor improvements | 0 | 0 | 0 | 5 |
| `qrcode` | 9 | 🟡 Minor improvements | 0 | 0 | 1 | 2 |
| `secrets` | 9 | ✅ Great as-is | 0 | 0 | 0 | 4 |
| `token` | 9 | ✅ Great as-is | 0 | 0 | 0 | 4 |
| `binder` | 8 | 🟡 Minor improvements | 0 | 0 | 2 | 3 |
| `cache` | 8 | 🟡 Minor improvements | 0 | 0 | 2 | 5 |
| `clientip` | 8 | 🟡 Minor improvements | 0 | 0 | 2 | 3 |
| `cookie` | 8 | 🟡 Minor improvements | 0 | 0 | 2 | 6 |
| `fingerprint` | 8 | 🟡 Minor improvements | 0 | 0 | 2 | 3 |
| `htmx` | 8 | 🟡 Minor improvements | 0 | 0 | 2 | 3 |
| `i18n` | 8 | 🟡 Minor improvements | 0 | 0 | 2 | 3 |
| `job` | 8 | 🟡 Minor improvements | 0 | 0 | 4 | 3 |
| `randomname` | 8 | 🟡 Minor improvements | 0 | 0 | 3 | 5 |
| `ratelimit` | 8 | 🟡 Minor improvements | 0 | 0 | 2 | 3 |
| `sanitizer` | 8 | 🟡 Minor improvements | 0 | 0 | 3 | 4 |
| `storage` | 8 | 🟡 Minor improvements | 0 | 0 | 3 | 4 |
| `webhook` | 8 | 🟡 Minor improvements | 0 | 0 | 2 | 4 |
| `mailer` | 7 | 🔴 Needs work | 0 | 1 | 2 | 3 |
| `redis` | 7 | 🔴 Needs work | 0 | 1 | 3 | 2 |
| `slug` | 7 | 🔴 Has bugs | 0 | 1 | 0 | 4 |
| `sqlitedb` | 7 | 🔴 Needs work | 0 | 1 | 2 | 2 |
| `db` | 6 | 🔴 Needs work | 0 | 1 | 4 | 4 |
| `jwt` | 6 | 🔴 Needs work | 0 | 0 | 4 | 4 |
| `totp` | 6 | 🔴 Needs work | 0 | 1 | 3 | 4 |
| `useragent` | 6 | 🔴 Needs work | 0 | 1 | 3 | 4 |
| `validator` | 6 | 🔴 Has bugs | 0 | 1 | 2 | 3 |
| `id` | 5 | 🔴 Has bugs | 0 | 2 | 2 | 1 |
| `logger` | 5 | 🔴 Needs work | 0 | 1 | 3 | 3 |
| `dnsverify` | 4 | 🔴 Has bugs | 0 | 3 | 1 | 2 |

---

## Top-priority fixes

Ranked by impact across the whole tree (security / blast-radius first).

1. **`dnsverify`** — Whitespace-only projectID yields false-positive verification (auth/ownership bypass) and strings.Contains substring matching is too loose; package has no tests.  
   _Why:_ Confirmed authentication/ownership-verification bypass with security impact, and zero tests to catch it. Highest-risk defect in the tree.
2. **`id`** — ShortID masks the low 30 bits of the timestamp (wraps every ~12.43 days, breaking lexicographic sort order) and its rand-failure fallback panics with index-out-of-range.  
   _Why:_ This is the framework-mandated ID generator (CLAUDE.md forbids all other ID generation), so both the sort-order regression and the guaranteed panic propagate to every consumer.
3. **`validator`** — DecimalPrecision wrongly rejects common 2-decimal monetary values due to float comparison.  
   _Why:_ A core validation rule silently rejects valid financial input — a correctness bug that directly affects B2B SaaS money handling, the framework's target domain.
4. **`mailer`** — A display name containing a comma breaks SMTP sending entirely (silent failure), and Date/Message-ID headers are omitted.  
   _Why:_ Confirmed: legitimate sender names silently fail to send, and missing Date/Message-ID hurts deliverability/spam scoring for all SMTP mail.
5. **`jwt`** — Parse silently skips ALL temporal (exp/nbf) validation when claims don't implement Valid().  
   _Why:_ A security footgun in an auth primitive: expired tokens can be accepted silently depending on the claims type, with no error surfaced.
6. **`sqlitedb`** — Per-connection PRAGMAs (e.g. foreign_keys) are applied to only one connection, so they silently don't hold when MaxOpenConns > 1.  
   _Why:_ Confirmed footgun that silently disables foreign-key enforcement and other PRAGMAs under normal pooling, leading to data-integrity issues.
7. **`useragent`** — Production code is hardcoded to satisfy a specific unit test (GetShortIdentifier special-casing).  
   _Why:_ Verified real: shipping logic shaped to pass a test rather than reflect correct behavior is a maintainability/correctness landmine.
8. **`slug`** — The minLength+maxLength "no room" path discards the real slug and undershoots minLength.  
   _Why:_ Confirmed correctness bug producing invalid slugs that violate the caller's own length constraints.
9. **`db`** — Entire package has zero tests, plus multiple doc.go inaccuracies (env var names, nonexistent db.Pool type, documented-but-absent migrations-table config, false exponential-backoff claim).  
   _Why:_ A foundational pgxpool wrapper used framework-wide with no test safety net and misleading docs that will misconfigure consumers.
10. **`totp`** — GetTOTPURI advertises configurable Algorithm/Digits/Period that ValidateTOTP/GenerateTOTP silently ignore.  
   _Why:_ Verified real: provisioning URIs promise parameters the validator ignores, so non-default authenticator setups will fail to validate — a silent 2FA-breaking mismatch.

---

## Cross-cutting themes

- Zero test coverage in multiple packages: pkg/db, pkg/logger, and pkg/dnsverify have no tests at all; pkg/job/riverdriver and pkg/mailer/resend have untested subpackages, and pkg/redis leaves its core retry/connect logic untested.
- Widespread violation of the project's own testing conventions: many packages use assert instead of require for critical checks (pkg/fingerprint, pkg/id, pkg/jwt, pkg/randomname, pkg/secrets, pkg/totp, pkg/useragent, pkg/validator, pkg/htmx, pkg/sanitizer, pkg/webhook) and omit t.Parallel() (pkg/cookie, pkg/clientip, pkg/randomname, pkg/sanitizer/tags_test, pkg/validator/tags_test, pkg/htmx).
- Dead/unreturned exported error sentinels recur across the tree: pkg/jwt (3 errors), pkg/ratelimit (ErrRateLimited), pkg/storage, pkg/useragent, pkg/sqlitedb (ErrSetDialect) all export errors no code path returns.
- doc.go drift is systemic: doc examples document env-var names that don't match struct tags (pkg/db, pkg/storage, pkg/totp), reference nonexistent types/functions (pkg/db db.Pool, pkg/job job.Migrate, pkg/logger multiHandler), or use signatures that don't compile (pkg/cache, pkg/redis, pkg/logger, pkg/sqlitedb, pkg/job).
- Inconsistent error wrapping: several packages discard the underlying error with %v or drop it entirely instead of %w (pkg/dnsverify, pkg/db connect failure, pkg/redis ping error, pkg/geolocation Close, pkg/secrets base64 decode).
- Undocumented SSRF / trust-boundary surfaces on user-supplied input: pkg/storage PutFromURL, pkg/webhook delivery URLs, and pkg/clientip unconditionally-trusted proxy headers all lack validation or a documented trust boundary.
- Retry loops waste a final backoff interval before returning failure in both pkg/db and pkg/redis (same defect pattern in two connection helpers).
- Regexes recompiled on every call instead of being package-level vars in pkg/sanitizer, pkg/validator, and pkg/useragent.
- Missing README.md for several directly-imported pkg/ packages (pkg/clientip, pkg/fingerprint, pkg/randomname, pkg/sanitizer, pkg/token).

---

## Per-package detail

Packages ordered by quality score (high → low).

### `geolocation` — 9/10 · ✅ Great as-is

A clean, focused, well-tested IP-geolocation utility over MaxMind GeoIP2 with correct concurrency handling, accurate docs, and full design-rule compliance; only cosmetic nits.

**Strengths:**
- Correctness verified against the actual oschwald/geoip2-golang/v2@v2.1.0 API: all field paths (Country.ISOCode, City.Names.English, Location.TimeZone, Subdivisions[0].Names.English) match the v2 models, and the record.HasData() guard (maxmind.go:69) is the correct way to return (nil,nil) for a public IP that simply isn't in the database.
- Concurrency is sound: RWMutex with the `closed` flag read under RLock in Lookup and set (plus db.Close) under Lock in Close eliminates any use-after-close race. Verified clean under `go test -race`, and the 50-goroutine TestMaxMindProvider_ConcurrentAccess passes.
- Non-routable IP handling (isNonRoutable, maxmind.go:103) correctly covers private/loopback/link-local-unicast/link-local-multicast/unspecified and is exhaustively tested (IPv4 + IPv6 loopback, all three RFC1918 ranges, link-local, 0.0.0.0, ::).
- Strong design-rule compliance: no reflection/containers/magic, inputs passed as parameters (ip + ctx) rather than extracted from context, public methods return only exported types (*Location, Provider, error), no redundant accessors, and no ID generation. Clean Provider interface with a compile-time `var _ Provider` assertion (maxmind.go:112).
- Documentation is accurate and matches real signatures: doc.go and struct comments reflect actual behavior, the memory-map claim is true (geoip2.Open uses mmap), and the referenced clientip.GetIP(r) exists with the cited signature. Tests assert real content (GB / London / Europe/London) and behavior, not just NoError. 94.6% coverage.
- Idempotent Close() (returns nil on second call) and ErrClosed after close are both explicitly tested (maxmind_test.go:118-139).

**Issues:**

| Sev | Category | Issue | Location |
|---|---|---|---|
| low |  | Close() returns the underlying db error unwrapped, inconsistent with package error style | `pkg/geolocation/maxmind.go:98` |
| low |  | "Public IP not found in DB returns (nil, nil)" is implemented but not documented | `pkg/geolocation/maxmind.go:69 / pkg/geolocation/doc.go` |
| low |  | No test exercises Region/Subdivisions population or the HasData()==false branch | `pkg/geolocation/maxmind_test.go:39` |

**Tests:** Strong. Tests assert real behavior (country=GB, city=London, timezone=Europe/London), exercise every non-routable branch for both IPv4 and IPv6, verify ErrInvalidIP via errors.Is for garbage and empty input, verify idempotent Close and ErrClosed-after-close, and include a 50-goroutine concurrency test that passes under -race. t.Parallel() is used at both function and subtest level throughout. Coverage is 94.6%. The only untested paths are Region/Subdivisions population and the (nil,nil)-for-unknown-public-IP branch.

**Design-rule compliance:** Fully compliant. No reflection, service containers, or magic. Values are passed as parameters (ctx + ip), not extracted from context inside the package. Public methods return only exported types (*Location, Provider, error) — no unexported return types. No redundant accessors (Location fields are plain exported struct fields). No ID generation, so the pkg/id rule is not implicated. doc.go/comments match actual signatures and the referenced clientip.GetIP exists. The sole rule worth a note — sync.Once for write-once fields — does not apply here: the `closed` field is not a lazy-initialized read path but a flag that must be serialized against concurrent Lookup calls, so RWMutex is the correct primitive, not sync.Once."

---

### `hostrouter` — 9/10 · 🟡 Minor improvements

A small, focused, correct, and thoroughly-tested host-based HTTP router; clean design with only minor polish items (nil-fallback panic, an undocumented wildcard-depth limitation).

**Strengths:**
- Correct and clean implementation: O(1) map-based exact/wildcard lookup, exact-over-wildcard priority, case-insensitive matching, and careful IPv6-aware port stripping (hostrouter.go:70-79) that correctly distinguishes [::1] from [::1]:8080.
- Fully immutable after construction: maps are built once in New() and only read in ServeHTTP, so the router is inherently concurrency-safe with no locks needed; the concurrency test (hostrouter_test.go:414) confirms this under -race.
- Excellent test coverage that asserts real behavior (status codes AND response bodies), not just require.NoError: exact/wildcard/priority, case-insensitivity, port stripping, IPv6, multi-level subdomain non-matching, pattern normalization/trimming, empty routes, panic propagation, and concurrency. t.Parallel() used at both function and subtest level throughout.
- Clean compliance with project design rules: no reflection, no service containers, no context-based value passing (host comes from *http.Request param), no ID generation, and all exported methods/functions return only exported types (*Router, Routes, string).
- doc.go is accurate — usage examples match real signatures and the claim that GetDomain/GetSubdomain back forge.Context.Domain()/Subdomain() is verified true in internal/context.go:513-522.

**Issues:**

| Sev | Category | Issue | Location |
|---|---|---|---|
| low |  | New() accepts a nil fallback that panics on the first unmatched request | `pkg/hostrouter/hostrouter.go:23-28,65` |
| low |  | Wildcard single-level matching limitation is not documented in doc.go | `pkg/hostrouter/doc.go:11` |
| low |  | GetSubdomain does not trim whitespace from baseDomain, unlike New() which trims patterns | `pkg/hostrouter/helpers.go:31` |

**Tests:** Strong. Two test files cover routing (exact, wildcard, priority, case-insensitivity, port stripping, IPv6 with/without port, multi-level subdomain non-matching, empty routes, pattern trimming/empty-pattern handling, panic propagation, 100-goroutine concurrency) and the helpers (GetDomain across IPv4/IPv6/localhost/ports/case, GetSubdomain across single/multi-level/deep subdomains, exact match, partial match, port stripping, case-insensitivity, empty host/base, localhost tenant cases). Tests assert concrete behavior — both status codes and response body content — rather than just require.NoError, and the panic test correctly uses require.Panics to lock in standard net/http propagation semantics. t.Parallel() is used at function and subtest levels everywhere. Tests pass under -race. Minor untested gaps: nil-fallback behavior (the issue above) and a benchmark would be nice-to-have, but neither is a real coverage hole for a utility this size.

**Design-rule compliance:** Fully compliant with CLAUDE.md design rules. No reflection, no service containers, no magic. Values are passed via parameters (host derived from *http.Request), not pulled from context. All exported functions/methods return only exported types (*Router, Routes, string) — no unexported return types leak. No redundant accessors and no exposed internal fields (Router's fields are all unexported). No ID generation occurs, so the pkg/id rule is not implicated. The router is a pure utility package with no embedded business logic, matching the framework's separation of concerns. Doc examples match actual signatures.

---

### `oauth` — 9/10 · 🟡 Minor improvements

A clean, well-tested OAuth2 authorization-code package for Google and GitHub with a small, ergonomic surface; solid behavior-asserting tests and full design-rule compliance, with only minor polish items.

**Strengths:**
- Excellent test coverage that asserts real behavior: IDs, emails, names, pictures, error sentinels via require.ErrorIs, and edge cases (unverified email, GitHub non-primary fallback, bad JSON on both endpoints, non-OK status on both endpoints, custom redirect URI propagation). t.Parallel() used at function and subtest level throughout.
- Compile-time interface conformance assertions present in both test files (var _ oauth.Provider = (*oauth.GoogleProvider)(nil)).
- Clean design-rule compliance: no reflection/containers/magic, values passed via parameters not context, public methods return only exported types (*oauth2.Token, *UserInfo), no redundant accessors, no ID generation so pkg/id rule is N/A.
- Good error ergonomics: sentinel errors with consistent 'oauth:' prefix wrapped via errors.Join so both errors.Is(err, ErrFetchFailed) and the underlying detail are available.
- Exchange's redirect-URI override builds a fresh oauth2.Config rather than mutating the shared one, so it is concurrency-safe and side-effect-free.
- doc.go accurately matches the real signatures (NewGoogleProvider, NewGitHubProvider, AuthCodeURL, Exchange(ctx, code, redirectURI), FetchUserInfo, WithHTTPClient) and documents the security expectations (state/CSRF, HTTPS redirect, token storage).
- Both providers correctly enforce email verification before returning UserInfo, as the Provider interface contract requires.

**Issues:**

| Sev | Category | Issue | Location |
|---|---|---|---|
| low |  | GitHub falls back to any verified email when no primary verified email exists | `pkg/oauth/github.go:162-174` |
| low |  | Unbounded io.ReadAll on Google error-response body | `pkg/oauth/google.go:108-111` |
| low |  | Google uses the deprecated oauth2/v2/userinfo endpoint | `pkg/oauth/google.go:18` |
| low |  | ErrEmailNotVerified conflates 'no verified email' with 'empty email list' | `pkg/oauth/github.go:174` |
| low |  | No test exercising the nil/no-custom-HTTP-client path or a transport-level network error | `pkg/oauth/google.go:100-105` |

**Tests:** Strong. Tests assert concrete behavior, not just require.NoError: returned UserInfo fields (ID, Email, Name, Picture), correct primary-vs-fallback email selection, redirect_uri propagation captured server-side, and specific error sentinels via require.ErrorIs (ErrMissingClientID/Secret, ErrEmailNotVerified, ErrRequestFailed, ErrDecodeFailed). Edge cases are well covered: default vs custom scopes, unverified email, GitHub fallback email, both /user and /user/emails failure paths, and malformed JSON on each endpoint. t.Parallel() is used at function and subtest level everywhere, and compile-time Provider conformance is asserted. Gaps are minor: the transport-error path (ErrFetchFailed), the unreachable ErrNilResponse, and the nil-custom-client branch are untested, and there is no test for Exchange network failure (only an HTTP 400 case).

**Design-rule compliance:** Fully compliant. No reflection, service containers, or magic. Values are passed via parameters (ctx, token, code, redirectURI), not pulled from context; the package only reads oauth2.HTTPClient into context for the oauth2 library, which is its documented mechanism. Public methods return only exported types (*oauth2.Token, *UserInfo, string, error) — no unexported types leak. No redundant accessors. The httpClient field is set once at construction and never mutated, so no sync.Once is needed. The package generates no IDs, so the pkg/id rule does not apply. It is a pure utility package with no embedded business logic. doc.go matches the real exported surface and signatures; there is no README to keep in sync, and oauth is a pkg/ package so no forge.go re-export is expected (and none exists).

---

### `qrcode` — 9/10 · 🟡 Minor improvements

A small, clean, well-documented QR-code utility with stateless pure functions and behavior-asserting tests; the only real gaps are an untested error path and tests that don't verify the size parameter's effect.

**Strengths:**
- Minimal, ergonomic API: two stateless functions (Generate, GenerateBase64Image) with a well-guarded variadic size param (resolveSize correctly requires size[0] > 0, qrcode.go:49-54).
- Correct error design: sentinel errors in errors.go with proper %w chaining in fmt.Errorf("%w: %w", ...) (qrcode.go:23) so errors.Is(err, ErrFailedToGenerateQRCode) works; verified the content-too-long path actually wraps as expected.
- doc.go is accurate and load-bearing: the TOTP integration example matches the real totp package signatures (GenerateSecretKey, GetTOTPURI, TOTPParams{Secret,AccountName,Issuer}) verified against pkg/totp/otp.go.
- Tests assert real behavior, not just NoError: PNG 8-byte magic header (qrcode_test.go:14,24), data-URI prefix (line 79), base64 round-trip decode to a valid PNG (lines 87-91), and sentinel-error identity via ErrorIs (lines 46,54,99).
- Good edge-case coverage: empty content, whitespace-only content, zero size, and negative size all tested. Proper t.Parallel() at both function and subtest level; require used throughout.
- Fully compliant with project design rules: no reflection, no service container, no context smuggling, no ID generation, no exported-unexported-type leaks. Stateless so no concurrency or sync.Once concerns apply.

**Issues:**

| Sev | Category | Issue | Location |
|---|---|---|---|
| medium |  | ErrFailedToGenerateQRCode path is untested (coverage gap) | `pkg/qrcode/qrcode.go:22-24` |
| low |  | Size-related tests assert 'produces output' instead of the size effect | `pkg/qrcode/qrcode_test.go:34-69` |
| low |  | No upper bound or doc note on size (large-allocation surface) | `pkg/qrcode/qrcode.go:49-53` |

**Tests:** Tests genuinely assert behavior rather than just require.NoError: they check PNG magic-header bytes, the data-URI prefix, a full base64-decode round-trip to a valid PNG, and sentinel-error identity via ErrorIs for both empty and whitespace input. Edge cases (empty, whitespace, zero, negative size) are covered, and t.Parallel() is used at function and subtest level with require throughout. Two gaps: (1) the ErrFailedToGenerateQRCode wrapping path is never exercised (the 5.9% uncovered), and (2) size-related tests only assert non-empty output instead of verifying the resulting image dimensions, so they wouldn't catch a regression where size is ignored. Coverage is 94.1% and the suite passes under -race.

**Design-rule compliance:** Fully compliant. No reflection, no service container, no magic. Values are passed as parameters, not via context. No exported method returns an unexported type. No redundant accessors. No ID generation (the pkg/id rule is N/A). Functions are stateless so the sync.Once rule does not apply. This is a thin utility wrapper around skip2/go-qrcode, exactly the kind of utility package the framework is meant to provide, with business logic (TOTP URI construction, size policy for untrusted input) left to consumers. It is a pkg/ package imported directly and correctly not re-exported from forge.go. doc.go examples match the real totp signatures, satisfying the 'doc examples must match actual signatures' rule.

---

### `secrets` — 9/10 · ✅ Great as-is

A small, focused AES-256-GCM + HKDF compound-key encryption utility that is cryptographically sound, well-tested, and fully compliant with the project's design rules; only cosmetic nits remain.

**Strengths:**
- Cryptography is correct and uses well-vetted primitives: AES-256-GCM (authenticated encryption) with a fresh crypto/rand 12-byte nonce per operation, and HKDF-SHA256 (golang.org/x/crypto/hkdf) for compound-key derivation. Nonce-prepended layout (secrets.go:173) is the standard, correct construction.
- Compound-key tenant isolation is the right design: appKey as IKM + workspaceKey as salt + fixed versioned info string 'forge-secrets-v1' (secrets.go:21,145). This is sound HKDF usage, and the info string being versioned leaves room for future key-schedule migration.
- Derived key material is zeroed via defer clearBytes(derived) on every encrypt/decrypt path (secrets.go:56,93,115,177-181) — good memory hygiene for a secrets package.
- Error handling is consistent and ergonomic: every failure path uses errors.Join(sentinel, cause) so callers can match both the operation sentinel (ErrEncryptionFailed/ErrDecryptionFailed) and the specific cause (ErrInvalidAppKey, ErrInvalidCiphertext, etc.) with errors.Is. doc.go documents this precisely and accurately.
- Keys are validated (length == 32) before any crypto work in every public entry point (secrets.go:48,85,107), so malformed input fails fast with a typed error rather than panicking in aes.NewCipher.
- Strong test suite: round-trip for strings and bytes, empty/unicode/64KiB/all-256-byte-values edge cases, tampered-ciphertext (GCM auth) rejection, wrong-app-key and wrong-workspace-key rejection, cross-tenant and cross-app isolation, and nonce-driven ciphertext uniqueness for identical plaintext. Tests assert real behavior (decrypted == plaintext, NotEqual ciphertexts, specific error sentinels), not just NoError. t.Parallel() is used at function and subtest level throughout.
- Fully compliant with design rules: no reflection, no context-based value passing (all inputs are parameters), no service container, no ID generation, and every exported function returns only exported types ([]byte, string, error). doc.go signatures match the implementation exactly.

**Issues:**

| Sev | Category | Issue | Location |
|---|---|---|---|
| low |  | assert used instead of require for a critical uniqueness check | `pkg/secrets/secrets_test.go:29` |
| low |  | Underlying base64 decode error is discarded on DecryptString | `pkg/secrets/secrets.go:71` |
| low |  | No benchmarks despite repo convention of `just bench` | `pkg/secrets/secrets_test.go` |
| low |  | deriveKey runs HKDF on every operation (no caching) | `pkg/secrets/secrets.go:144` |

**Tests:** Strong and behavior-focused. Tests cover round-trips (string + bytes), edge cases (empty string/bytes, unicode, 64KiB payload, all 256 byte values), GCM authentication (tampered-byte rejection), key-mismatch rejection (wrong app key, wrong workspace key), tenant/app isolation, malformed input (invalid base64, too-short ciphertext, nil data, short keys), and nonce-driven ciphertext uniqueness. Assertions check real outcomes — decrypted equals original plaintext, ciphertexts differ across tenants and across repeated calls, and specific error sentinels are matched via require.ErrorIs — rather than merely require.NoError. t.Parallel() is applied at both function and subtest levels. The only gaps are the single assert.NotEqual that should be require (per CLAUDE.md) and the absence of benchmarks; coverage of correctness and security behavior is otherwise comprehensive.

**Design-rule compliance:** Fully compliant. No reflection, no service container, no magic. All values are passed as parameters (no context-based passing). No IDs are generated, so the pkg/id mandate does not apply. Every exported function (GenerateKey, ValidateKeys, EncryptString/DecryptString, EncryptBytes/DecryptBytes) returns only exported types ([]byte, string, error) — no unexported return types. No redundant accessors and no exposed struct fields (the package is purely functional with sentinel error vars). doc.go documentation matches the actual signatures and error-wrapping behavior exactly. The only convention deviation is a testing-convention nit: one assert call where the project mandates require (secrets_test.go:29)."

---

### `token` — 9/10 · ✅ Great as-is

A small, clean, stdlib-only signed-token utility with correct security-first verification, generics-based ergonomic API, and thorough behavior-asserting tests; only minor doc/nit gaps.

**Strengths:**
- Security-first ordering in ParseToken: signature is verified with subtle.ConstantTimeCompare BEFORE the payload is base64-decoded or JSON-unmarshaled (token.go:60-71), so tampered payloads are never processed.
- HMAC-SHA256 with constant-time comparison; empty-secret guard in both GenerateToken and ParseToken (token.go:30,50) returns ErrEmptySecret consistently.
- Clean generics-based API returning concrete exported types (string, *T, error) - fully compliant with the 'no unexported return types' design rule; no reflection, no service containers, no context coupling.
- Distinct, well-documented sentinel errors (ErrInvalidToken, ErrSignatureInvalid, ErrEmptySecret) enabling errors.Is dispatch, demonstrated correctly in doc.go.
- Excellent doc.go: documents the exact token format, the 64-bit truncation tradeoff, secret-length guidance, and the critical 'pair with server-side state / mark consumed' replay guidance - honest about the security model rather than overselling it.
- Tests assert real behavior (round-trip field equality, base64url decoding, 8-byte signature length, URL-safety of output, determinism, tamper detection on both payload and signature, wrong-secret rejection) across struct/map/string/int/bool/slice payloads - not just require.NoError. t.Parallel() used at function and subtest level throughout.
- Benchmarks cover small/large payloads for generate, parse, and round-trip using the modern b.Loop() idiom.

**Issues:**

| Sev | Category | Issue | Location |
|---|---|---|---|
| low | docs | Doc/error text claims segment-count validation that strings.Cut does not perform | `pkg/token/errors.go:7` |
| low | security | Length-mismatch path in signature compare is not constant-time (leaks only public length) | `pkg/token/token.go:61` |
| low | design-rule | 64-bit truncated signature is an inherent design ceiling, not a defect | `pkg/token/token.go:17` |
| low | docs | No README.md for a directly-imported pkg/ package | `pkg/token/` |

**Tests:** Strong. 88.9% statement coverage; tests verify behavior rather than just absence of error: round-trip field equality, base64url decodability of both segments, exact 8-byte signature length, URL-safety (no +,/,= in output), determinism, and rejection paths (tampered payload -> ErrSignatureInvalid, tampered signature -> ErrSignatureInvalid, wrong secret -> ErrSignatureInvalid, malformed/empty token -> ErrInvalidToken, nil/empty secret -> ErrEmptySecret). Generic API exercised across struct/nested/map/string/int/bool/[]string. t.Parallel() applied at function and subtest level consistently. Per repo convention, critical checks use require while value comparisons use assert (testify/assert is used across ~69 test files repo-wide). Minor untested gaps: the JSON-unmarshal-failure path (valid signature over base64 of non-matching JSON, e.g. parsing a numeric payload as a struct) and an unmarshalable GenerateToken payload (e.g. a chan or func) are not exercised, but these are low-value edge cases.

**Design-rule compliance:** Compliant with all stated design rules. No reflection, no service containers, no magic. Values are passed by parameter (payload, secret), not via context. Public functions return concrete exported types (string, *T, error) - no unexported return types. No redundant accessors. The package generates no IDs, so the pkg/id rule does not apply. sync.Once is irrelevant (no lazy write-once fields). It is a focused utility package with no embedded business logic, matching the framework's intent. The only deviations are documentation nits (the 'segment-count' wording), not design-rule violations.

---

### `binder` — 8/10 · 🟡 Minor improvements

Solid, well-tested HTTP request binding package with strong security hardening; a few correctness/clarity gaps around sanitization (map values silently unsanitized, over-eager comma splitting, incomplete control-char filtering).

**Strengths:**
- Clean, ergonomic API: Binder is a single func type; JSON/Form/Query/Path all return the exported Binder type, no unexported types leak (complies with the public-method rule).
- Thorough security hardening that actually works: JSON size limit with +1-byte oversize detection, DisallowUnknownFields, trailing-data rejection, multipart boundary validation, filename path-traversal sanitization, and CRLF/NUL stripping are all verified by tests.
- Excellent test breadth: valid/partial/empty/error paths, pointer fields, slices (multi-value and comma-separated), all numeric widths, bool variants, multipart files, and realistic SaaS-shaped structs; tests assert concrete values/headers/side effects, not just NoError.
- Good error design: sentinel errors (ErrFailedToParseJSON etc.) wrapped with %w so errors.Is works, and doc.go documents the errors.Is switch pattern accurately.
- doc.go is comprehensive and its signatures match the real code (JSON()/Form()/Query()/Path(extractor), tag names, supported types, constants DefaultMaxJSONSize/DefaultMaxMemory).

**Issues:**

| Sev | Category | Issue | Location |
|---|---|---|---|
| medium |  | JSON sanitization silently skips all map values (no-op) | `pkg/binder/json.go:137-145` |
| medium |  | Comma splitting corrupts legitimate slice string values | `pkg/binder/reflect_utils.go:148-155` |
| low |  | sanitizeStringValue leaves DEL and C1 control characters intact; IsGraphic branch is dead | `pkg/binder/reflect_utils.go:185-191` |
| low |  | Unnecessary []byte->string->Reader copy in JSON decode path | `pkg/binder/json.go:67` |
| low |  | Security/error-boundary tests assert nothing on the untaken branch and omit t.Parallel on wrappers | `pkg/binder/security_test.go:21-29; pkg/binder/error_boundaries_test.go:18-34` |

**Tests:** Comprehensive and mostly high-quality. Per-binder tests (json/form/query/path) assert concrete bound values, error sentinels via errors.Is, and side effects (filename sanitization results, file sizes/contents, skipped fields), not just NoError; edge cases include all numeric widths, pointer fields, bool variants, multi-value and comma-separated slices, and realistic SaaS structs. t.Parallel() is used at function and subtest level throughout the per-binder files. Weak spots: the dedicated security_test.go and error_boundaries_test.go rely on `if err == nil/err != nil` guards that assert nothing on the non-taken branch (so they can pass vacuously and would miss regressions), several wrapper/nested subtests there omit t.Parallel(), and two real gaps are untested - JSON map-value sanitization (which is a silent no-op) and DEL/C1 control-char passthrough.

**Design-rule compliance:** Compliant with the spirit of the rules. Uses reflection, but the project's no-reflection rule targets DI/service-container magic; request-to-struct binding inherently requires reflection and this is the idiomatic, contained exception used consistently across the framework. Public functions return the exported Binder type (no unexported return types). Values are passed as parameters (r, v), not pulled from context - the package does not read context for data, matching the middleware-handles-context rule. No ID generation, so the pkg/id rule is not implicated. No redundant accessors or exposed internal fields. One debatable design point (not a rule violation): JSON/Query/Form binding silently mutates user string input via sanitization during bind, which can surprise callers (e.g. altering a password or free-text field); it is documented but mixing sanitization into binding couples two concerns.

---

### `cache` — 8/10 · 🟡 Minor improvements

A clean, well-designed generic cache with Memory (LRU+TTL) and Redis backends; correct and well-tested, with a doc signature error, eviction-callback-under-lock caveat, two dead files, and a couple of untested error paths.

**Strengths:**
- Clean, idiomatic generic API: single Cache[V] interface, both backends satisfy it via compile-time `var _ Cache[any]` assertions (memory.go:293, redis.go:170). No reflection, no magic, no service container — fully compliant with project design rules.
- GetOrSet stampede prevention is well-architected: a standalone generic function delegates to an unexported `deduper` interface so the public surface never leaks unexported types, and per-instance singleflight (not a package global) avoids cross-instance key collisions (cache.go:62-115).
- Memory LRU is implemented correctly with map + container/list for O(1) ops; recency is updated on both Get and Set/overwrite, overwrite correctly does not count toward MaxEntries, and capacity-1 works. deleteExpired iterates back-to-front safely capturing Prev() before removal (memory.go:252-265).
- TTL semantics (positive/zero/negative) are consistent and clearly documented across the interface, both impls, and doc.go; Redis correctly maps negative TTL to 0 via max(ttl,0) (redis.go:95).
- Redis Clear is production-safe with prefix: uses non-blocking SCAN in batches of 100 rather than KEYS (redis.go:140-163). Errors are wrapped with sentinels via errors.Join for errors.Is checks.
- Excellent test quality: t.Parallel() at function and subtest level throughout, require everywhere, miniredis for in-process Redis (no Docker), and tests assert real behavior — eviction order, prefix isolation, recency, and even the raw reversed bytes in Redis for the custom marshaler (cache_test.go:1043-1045).

**Issues:**

| Sev | Category | Issue | Location |
|---|---|---|---|
| medium |  | NewRedis doc example uses wrong MustOpen signature | `pkg/cache/redis.go:36` |
| medium |  | Eviction callback runs while holding the cache mutex (deadlock / blocking risk) | `pkg/cache/memory.go:283-285` |
| low |  | Redis Clear with empty prefix runs FLUSHDB (wipes the whole database) | `pkg/cache/redis.go:117-122` |
| low |  | GetOrSet calls Set once per caller instead of once per computation | `pkg/cache/cache.go:112` |
| low |  | Two empty dead files (memory_options.go, redis_options.go) | `pkg/cache/memory_options.go:1` |
| low |  | Read operations after Close silently succeed while writes return ErrClosed | `pkg/cache/memory.go:95` |
| low |  | Misaligned struct tag in MemoryConfig | `pkg/cache/memory.go:16` |

**Tests:** Strong, behavior-asserting suite — not NoError-only. Tests verify concrete values, LRU eviction order, recency updates on Get/Set, overwrite-not-counting-as-new, capacity-1, expiry via real sleeps (Memory) and miniredis FastForward (Redis), prefix isolation, eviction callbacks on LRU/Delete/Clear, janitor cleanup, and concurrent read/write/delete under -race. The custom-marshaler test even inspects the raw reversed bytes in Redis (cache_test.go:1043). t.Parallel() is used at both function and subtest level; require is used throughout; in-process mocks (miniredis) avoid Docker per project conventions. Gaps: (1) GetOrSet/singleflight is only tested against Memory — the Redis sfDo/deduper path is never exercised; (2) ErrMarshal and ErrUnmarshal are never triggered (no test caches an unmarshalable value or corrupt bytes), so the errors.Join wrapping in jsonMarshaler is unverified; (3) no test for Redis Clear with empty prefix (FlushDB path) or for a Get backend error other than redis.Nil. These are minor coverage holes, not correctness risks.

**Design-rule compliance:** Compliant with project design rules. No reflection, no service container, no magic. Values are passed as parameters, not via context (ctx is used only for cancellation/Redis I/O). Public methods do not return unexported types — GetOrSet returns V and routes through the unexported deduper interface, and compile-time `var _ Cache[any]` assertions confirm both backends satisfy the public interface. No redundant accessors. No ID generation occurs in this package, so the pkg/id rule is not applicable. One minor deviation from the CLAUDE.md guidance: the package does not use sync.Once for the lazily-started janitor; instead NewMemory starts the goroutine eagerly and Close uses a `closed` bool flag — acceptable here since the goroutine is created once at construction (not lazily) and Close is correctly idempotent, so the sync.Once rule (for lazy write-once fields) does not strictly apply.

---

### `clientip` — 8/10 · 🟡 Minor improvements

Small, correct, panic-free IP extractor with strong design rule compliance and extensive edge-case tests; main gaps are an undocumented trust-boundary (spoofable headers) and two test tables that define expected values but never assert them.

**Strengths:**
- Implementation is minimal and correct: clean header priority chain, proper net.ParseIP validation, normalization via ip.String(), and graceful RemoteAddr fallback that never panics and always returns a string.
- Stateless and fully goroutine-safe by construction (no shared mutable state); a dedicated TestConcurrentSafety with 100 goroutines + a 1s race loop confirms this under -race.
- Design-rule compliant: no reflection, no context coupling (takes *http.Request param), no IDs generated, single exported function returning a string (no unexported return types), no redundant accessors.
- Exceptionally broad security-focused test surface: CRLF/null-byte/tab injection, Unicode confusables, IPv4-mapped IPv6, zone identifiers, bracket confusion, DoS via 50k-element forwarded chains and header bombing, all asserting concrete fallback values.
- doc.go accurately matches the real signature and documented behavior (header order, 0.0.0.0 rejection, leftmost XFF, IPv6 normalization, never-panics guarantee).
- Used correctly across the codebase (pkg/ratelimit/keyfunc.go, pkg/fingerprint, internal/session.go) as a shared utility.

**Issues:**

| Sev | Category | Issue | Location |
|---|---|---|---|
| medium |  | Trust boundary is undocumented: all proxy headers are unconditionally trusted (IP spoofing) | `pkg/clientip/clientip.go:9-49` |
| medium |  | Two test tables define `expected` values but never assert them | `pkg/clientip/ipv6_malformed_test.go:740-756` |
| low |  | Mixed assertion libraries: security/ipv6 tests use testify assert/require, core test uses raw t.Errorf | `pkg/clientip/security_test.go:100` |
| low |  | Top-level security/ipv6 test functions omit t.Parallel() | `pkg/clientip/security_test.go:16` |
| low |  | No README.md for the package | `pkg/clientip` |

**Tests:** Coverage is very broad and mostly behavior-asserting: header priority, fallback chains, IPv6 normalization/compression, CRLF/null/tab injection, Unicode confusables, IPv4-mapped IPv6, zone identifiers, bracket confusion, malformed RemoteAddr, and DoS vectors (50k forwarded chains, header bombing) all assert concrete expected output (content), not just NoError. Concurrency is exercised under -race. The notable weakness: two subtests (numeric_string_edge_cases and remote_addr_with_invalid_port) define expected values but assert nothing (only t.Logf + NotPanics), so they cannot catch regressions and one carries a now-incorrect expected value ('192.168.001.001'). Several time-based assertions (avgDuration < 1ms, < 10ms) are environment-sensitive and could flake on loaded CI but are generously bounded. t.Parallel() is present on leaf subtests but missing on the top-level Test functions in the security/ipv6 files.

**Design-rule compliance:** Strong compliance. No reflection, no service containers, no magic. Receives the request via parameter rather than reading from context (middleware/caller handles extraction). Single exported function GetIP returns a string — no unexported return types, no redundant accessors. No ID generation, so the pkg/id rule is not implicated. The package is a pure stateless utility, matching the 'framework provides utilities, business logic in consumer repos' principle. One soft violation of the testing conventions (assert vs require for critical checks; missing function-level t.Parallel on some test funcs; two non-asserting test tables) rather than the core design rules. The only substantive design concern is the unconditional trust of spoofable proxy headers without a documented trust boundary.

---

### `cookie` — 8/10 · 🟡 Minor improvements

A clean, well-structured cookie helper with sound crypto (AES-256-GCM, HMAC-SHA256, constant-time compare) and good behavior-asserting tests; main gaps are a contradictory HttpOnly default in the docs, missing t.Parallel() per project convention, and a flash cookie that isn't cleared on decrypt failure.

**Strengths:**
- Correct, idiomatic crypto: AES-256-GCM with a fresh random nonce per Seal, HMAC-SHA256 for signing, constant-time verification via hmac.Equal, and base64.RawURLEncoding which is cookie-value safe.
- Secret length is validated (>=32 bytes) at construction, with a distinct ErrBadSecret; signed/encrypted/flash ops fail cleanly with ErrNoSecret when no secret is set.
- Clear sentinel error set (ErrNotFound/ErrNoSecret/ErrBadSecret/ErrBadSig/ErrDecrypt) that is re-exported and used consistently; tamper failures map to the right errors and never leak crypto internals.
- Adheres to design rules: no reflection, no service container, values passed as parameters (w/r), public methods return only exported types, no redundant accessors, no ID generation.
- Tests assert real behavior, not just NoError: cookie name/value/MaxAge, encrypted value differs from plaintext, tamper detection for both signed and encrypted, and the flash single-read delete side effect (MaxAge=-1).
- doc.go examples match the actual signatures (New, Set/Get, SetSigned/GetSigned, SetEncrypted/GetEncrypted, SetFlash/Flash).

**Issues:**

| Sev | Category | Issue | Location |
|---|---|---|---|
| medium | docs | HttpOnly default is contradictory between struct tag, doc, and direct construction | `pkg/cookie/cookie.go:33, pkg/cookie/doc.go:66` |
| medium | testing | No t.Parallel() anywhere, violating the project testing convention | `pkg/cookie/cookie_test.go (all functions/subtests)` |
| low | bug | Flash cookie is not deleted when decryption fails | `pkg/cookie/cookie.go:222-228` |
| low | security | Encrypted/signed values are not bound to the cookie name (no AAD) | `pkg/cookie/cookie.go:139-141, pkg/cookie/cookie.go:282` |
| low | security | Signed/encrypted cookies carry no embedded expiry, allowing indefinite replay | `pkg/cookie/cookie.go:150-168, pkg/cookie/cookie.go:196-211` |
| low | api-design | SameSite=None is accepted without requiring Secure | `pkg/cookie/cookie.go:74-85` |
| low | performance | Key is re-derived via SHA-256 on every encrypt/decrypt | `pkg/cookie/cookie.go:265, pkg/cookie/cookie.go:288` |
| low | error-handling | Unreachable error branch in Get | `pkg/cookie/cookie.go:90-95` |

**Tests:** Good coverage and the tests assert real behavior, not just NoError: they verify cookie name/value/MaxAge, that encrypted output differs from plaintext, that tampering yields ErrBadSig (signed) and ErrDecrypt (encrypted), that ErrNoSecret/ErrNotFound/ErrBadSecret surface correctly, and that Flash actually emits a delete cookie (MaxAge=-1) after a successful read. Gaps: no t.Parallel() anywhere (violates project convention); no test that a second Flash read returns ErrNotFound after deletion (the single-read guarantee is only checked via the emitted delete cookie, not via an actual re-read against a request lacking the cookie); no coverage of the flash-fails-without-delete path; no SameSite 'none'/'lax'/invalid parsing tests beyond 'strict' and default; no large-value/binary-value round-trip test for encrypt. Crypto correctness (round-trip + tamper) is solid.

**Design-rule compliance:** Strong overall. No reflection, no service container, no magic. Values are passed via parameters (w, r) rather than context, consistent with the rule that middleware handles context extraction. Public methods return only exported types (string, error, *Manager). No redundant accessors expose already-available fields. No ID generation, so the pkg/id rule is not implicated. sync.Once rule is not triggered (no lazy write-once fields today; the suggested cached key would be a candidate). The one concrete deviation is a testing-convention violation: t.Parallel() is required at function and subtest level per CLAUDE.md and is absent throughout cookie_test.go. The HttpOnly default discrepancy is a documentation/ergonomics issue rather than a design-rule violation.

---

### `fingerprint` — 8/10 · 🟡 Minor improvements

A clean, well-structured device-fingerprinting utility with a sensible API and solid behavioral tests; minor gaps in HTMX doc/test coverage and a project-convention deviation (assert vs require).

**Strengths:**
- Correct and well-reasoned hashing: empty components are filtered and joined with a '|' delimiter (fingerprint.go:49-60) specifically to prevent the ["ab","c"] vs ["a","bc"] collision class — a real concern that is explicitly handled and commented.
- Versioned fingerprint format ("v1:"+hex) with strict length+prefix validation (fingerprint.go:73) enables future algorithm changes without breaking stored sessions; the rationale is documented.
- Deterministic header-set fingerprint: getHeaders() whitelists only stable headers and sorts them (fingerprint.go:105) so Go's non-deterministic map iteration cannot affect output. Verified by the consistency test that runs Generate 100x and asserts a single unique result.
- Stateless, pure functions with no shared mutable state — inherently goroutine-safe; no concurrency concerns.
- Clean API: every generator (Cookie/JWT/Strict/HTMX) has a matching validator, errors are exported sentinels checkable via errors.Is, and no public method returns an unexported type. Complies with the framework design rules (no reflection, values passed as parameters).
- Tests assert real behavior, not just NoError: they check exact format via regex ^v1:[a-f0-9]{32}$, verify that non-whitelisted headers (Cookie/Authorization/X-Custom) do NOT change the fingerprint, and confirm cross-config validation fails with the right sentinel error.

**Issues:**

| Sev | Category | Issue | Location |
|---|---|---|---|
| medium |  | HTMX/ValidateHTMX are undocumented in doc.go | `pkg/fingerprint/doc.go:50-68` |
| medium |  | HTMX and ValidateHTMX have no test coverage | `pkg/fingerprint/fingerprint_test.go:254-342` |
| low |  | Tests use assert instead of require for critical checks | `pkg/fingerprint/fingerprint_test.go:28` |
| low |  | Fingerprint comparison in Validate is not constant-time | `pkg/fingerprint/fingerprint.go:78` |
| low |  | No README.md for the package | `pkg/fingerprint/` |

**Tests:** Strong behavioral coverage. Tests assert concrete properties rather than just NoError: exact 35-char length and regex format, determinism across 100 calls, collision-freedom across four realistic client profiles, that whitelisted vs non-whitelisted headers do/don't affect output, IP inclusion toggling, and that mismatched configs/generators fail with the correct sentinel error (ErrMismatch vs ErrInvalidFingerprint). Edge cases covered: empty request, missing headers, all-components-disabled (verifies a stable empty-input fingerprint that still round-trips). t.Parallel() is used at both function and subtest level consistently. Benchmarks exist for all major paths. Two real gaps: (1) HTMX/ValidateHTMX are completely untested while every sibling convenience pair is, and (2) critical assertions use assert instead of the project-mandated require, so failures don't halt and can cascade.

**Design-rule compliance:** Compliant with the core design rules. No reflection, no service containers, no magic. Functions receive *http.Request and Config by parameter rather than pulling from context (clientip extraction is delegated to pkg/clientip). No public method returns an unexported type; Config and the sentinel errors are all exported. No ID generation occurs, so the pkg/id rule is not implicated. The Config struct exposes only plain bool fields the caller sets directly — no redundant accessors. sync.Once is not relevant (package is stateless). The only deviations from documented project conventions are in tests/docs, not the design rules: assert used where CLAUDE.md requires require, and doc.go omits the HTMX convenience pair that the codebase actually exports.

---

### `htmx` — 8/10 · 🟡 Minor improvements

A clean, well-organized HTMX helper package with accurate docs and good coverage; the main concerns are an unvalidated open-redirect in RedirectBack and a test file that breaks the project's t.Parallel/require conventions.

**Strengths:**
- Small, focused, idiomatic surface: pure functions over http.ResponseWriter/Request plus a functional-options Config. No reflection, no context smuggling, no service containers - fully compliant with the project's design rules.
- doc.go is thorough and accurate: every example (htmx.Redirect, LocationTarget, LocationOptions{Path,Target,Swap}, NewConfig + WithRetarget/WithReswap/WithTrigger/WithOOB, ApplyHeaders) matches the real signatures in source.
- ApplyHeaders has a nil-receiver guard (render.go:46-48) and is covered by TestNilConfigApplyHeaders, so the internal/context.go call path that passes a possibly-nil *Config is safe.
- 96.1% statement coverage; the only uncovered branch is the JSON-marshal-error fallback in LocationWithOptions, which is effectively unreachable since LocationOptions holds only marshalable types.
- Header constants in headers.go are complete and correctly split into response vs request groups, matching the HTMX spec casing (HX-Push-Url, HX-Replace-Url, etc.).
- Functional options correctly append (WithOOB, WithTrigger) rather than overwrite, and TestWithOOBAppends/TestTriggerChaining assert that accumulation behavior.

**Issues:**

| Sev | Category | Issue | Location |
|---|---|---|---|
| medium |  | RedirectBack is an open-redirect sink (no destination validation) | `pkg/htmx/redirect.go:25-32` |
| medium |  | render_test.go violates project testing conventions (no t.Parallel, uses t.Errorf instead of require/assert) | `pkg/htmx/render_test.go:22-263` |
| low | api-design | WithTrigger comma-joins event names without escaping; commas in names corrupt the header | `pkg/htmx/render.go:67-69` |
| low | testing | assert used for critical checks in parallel test files where CLAUDE.md prefers require | `pkg/htmx/location_test.go:27-29` |
| low | api-design | Config exposes mutable public fields, partially bypassing the options API | `pkg/htmx/render.go:18-29` |

**Tests:** Strong coverage (96.1%) and the tests genuinely assert behavior - header names/values, status codes (200 for HTMX redirect vs 302/301/303/307 for non-HTMX), JSON round-trips of LocationOptions including special chars/emoji, omitempty behavior, and nil/empty map handling - not just NoError. Edge cases (empty path, multiple redirect params, URL-encoded params, case sensitivity of HX-Request) are covered. The one unreached branch (JSON marshal failure in LocationWithOptions) is effectively impossible to hit. Two test-quality gaps: render_test.go omits t.Parallel entirely and uses raw t.Errorf/t.Fatal instead of testify, and the other files use assert where CLAUDE.md prefers require for the load-bearing checks. No test asserts the open-redirect risk is mitigated (because it isn't).

**Design-rule compliance:** Good overall. No reflection, no service containers, no context-based value passing - functions take explicit (w, r, ...) params, matching 'packages receive values via parameters'. No ID generation, so the pkg/id rule is not implicated. No public method returns an unexported type (Config and SwapStrategy are exported; options return the exported RenderOption). The one soft tension is the 'don't expose fields users already have' rule: Config's fields are fully public to allow internal/context.go to read OOBComponents (documented at render.go:17-18) - a justified but slightly leaky exception since it also permits direct mutation. Testing-convention rules (t.Parallel everywhere, require over assert) are violated in render_test.go and partially in the other test files.

---

### `i18n` — 8/10 · 🟡 Minor improvements

A well-designed, immutable, zero-magic i18n utility with strong test coverage; a leaked mutable Languages() slice and a Languages()/translations disconnect are the only notable gaps.

**Strengths:**
- Immutable-after-construction design with functional options; genuinely safe for concurrent use without locks, and a concurrency test (i18n_test.go:535) exercises it.
- Clean compliance with project design rules: no reflection, no service containers, no ID generation, no redundant sync primitives. Verified via grep.
- O(1) flattened lookups via composite keys (buildKey), with thoughtful three-tier fallback (exact lang -> base lang en-US->en -> default lang) implemented consistently in both T and Tn.
- CLDR-style plural rules for many language families plus a per-form fallback chain (getPluralFallbackForms) so a missing 'few'/'many' key degrades gracefully to 'other'.
- Excellent, behavior-asserting test suite: plural categories, locale formatting (currency/number/percent/date), accept-language quality parsing, base-language fallback, file loaders (JSON+YAML via embed.FS). t.Parallel() used at function and subtest level throughout.
- Accept-Language parser caps header length (maxAcceptLanguageLength, accept_language.go:12) to prevent algorithmic-complexity DoS, and the cap is tested (accept_language_test.go:112).
- doc.go is accurate: signatures, option names, and example outputs match the real API (FormatDeDE currency '19,99 €', nested dot-notation lookups, M placeholder type all verified against source).

**Issues:**

| Sev | Category | Issue | Location |
|---|---|---|---|
| medium |  | Languages() leaks the internal mutable slice, breaking the immutability guarantee | `pkg/i18n/i18n.go:258-260` |
| medium |  | Languages() does not include languages loaded via WithTranslations/WithJSONDir/WithYAMLDir | `pkg/i18n/i18n.go:267-272` |
| low |  | FormatNumber/FormatCurrency produce garbage for very large floats (int64 overflow) | `pkg/i18n/format.go:113, format.go:203` |
| low |  | ParseAcceptLanguage best-match scoring logic is convoluted and partly redundant | `pkg/i18n/accept_language.go:59-67` |
| low |  | flattenTranslations silently stringifies unexpected types and can collide keys | `pkg/i18n/i18n.go:297-299` |

**Tests:** Strong. Tests assert concrete behavior — exact translated strings, formatted currency/number/percent output, selected plural categories per language, fallback resolution, accept-language picks, and file-loader content (JSON+YAML from embed.FS) — not merely require.NoError. t.Parallel() is applied at both function and subtest level consistently. A concurrency test (100 goroutines) exercises the lock-free read path. Plural rules and GetPluralRuleForLanguage have exhaustive table coverage including negatives and boundary mod-10/mod-100 cases. Gaps: no test pins the documented O(1)/immutability contract (the Languages() mutable-slice leak and the Languages()-vs-loaded-translations disconnect are both untested, which is exactly why they slipped through); no test for FormatNumber on out-of-int64-range input; loader error paths (file outside a lang dir -> ErrInvalidFile, malformed JSON/YAML -> ErrInvalidFile) are implemented but not asserted. Minor: a few subtests use assert instead of require (i18n_test.go:559-570 concurrency, plural_test.go:336), acceptable in those spots.

**Design-rule compliance:** Compliant. No reflection, no service container, no magic (grep-verified). No ID generation, so the pkg/id rule is not implicated. Public methods do not return unexported types: New returns *I18n, NewTranslator returns *Translator, formats return *LocaleFormat, Option/LocaleFormatOption/PluralRule are exported types, and accessors return string/[]string/*LocaleFormat. Immutability is achieved without sync.Once because all fields are write-once during construction (no lazy init needed), which fits the 'sync.Once for lazy-initialized write-once fields' rule. The one design-rule tension is the 'don't expose redundant accessors / don't break immutability' spirit: Languages() returns the internal slice by reference (flagged above), and Translator exposes Language()/Namespace()/Format() accessors — but those wrap values the caller did not already hold a handle to (they're set internally with defaulting), so they are justified rather than redundant. Packages receive values via parameters (lang/namespace/key passed explicitly), never via context — fully aligned with the framework's context rule.

---

### `job` — 8/10 · 🟡 Minor improvements

Well-architected, driver-agnostic job package with clean generics-based type-safety and solid SQLite/River drivers; main gaps are a stale doc.go, an untested riverdriver, and a non-atomic SQLite dedup path.

**Strengths:**
- Clean driver abstraction: job.Driver interface decouples the framework from the backend, with two well-separated implementations (riverdriver, sqlitedriver) and a shared driver-agnostic JobInsert/WorkerConfig.
- Type-safe task registration via generics (WithTask[P,T], taskWrapper) with JSON type-erasure — achieves compile-time payload safety with no reflection, fully honoring the 'no reflection/no magic' design rule.
- No design-rule violations: no reflection, no uuid/ulid ID generation, no unexported types returned from exported methods, options use the functional-option pattern consistently.
- Good concurrency hygiene in sqlitedriver: claimJob uses a transaction to atomically select+mark 'running', a semaphore bounds per-queue concurrency, panics are recovered in safeExecute (correctly capturing recover() value), and recoverOrphanedJobs handles crash recovery.
- sqlitedriver test suite asserts real behavior — completed/discarded/pending status transitions, priority ordering, retry-then-succeed, discard-on-max-attempts, panic recovery, dedup skip, tx rollback — not just require.NoError.
- Sentinel errors are well-defined and wrapped with %w throughout; healthcheck composes errors via errors.Join so callers can match both ErrHealthcheckFailed and the underlying cause.

**Issues:**

| Sev | Category | Issue | Location |
|---|---|---|---|
| medium | docs | doc.go references a non-existent job.Migrate function | `pkg/job/doc.go:180-189` |
| medium | docs | doc.go App Integration example uses wrong WithJobs signature and a non-existent type | `pkg/job/doc.go:1-110` |
| medium | testing | riverdriver has zero test coverage | `pkg/job/riverdriver/driver.go` |
| medium | concurrency | SQLite dedup is a non-atomic check-then-insert backed by a non-unique index | `pkg/job/sqlitedriver/driver.go:129-145,78-80` |
| low | concurrency | In-flight job result writes use the cancelled context on Stop | `pkg/job/sqlitedriver/poller.go:128-176` |
| low | api-design | Framework enqueue path bypasses Manager's ErrUnknownTask validation | `pkg/job/manager.go:154-159` |
| low | docs | WithPriority/WithMaxAttempts defaults documented as River-specific; standalone WithUniqueKey silently dropped | `pkg/job/enqueuer.go:124-129` |

**Tests:** Core package and sqlitedriver are well tested and assert real behavior, not just NoError: task wrapper covers success/empty/invalid-payload/handler-error; buildJobInsert covers every option and round-trips the payload; sqlitedriver covers status transitions (completed/discarded/pending), priority ordering, retry-then-succeed, discard-on-max-attempts, panic recovery, dedup skip vs allow-after-terminal, and tx commit/rollback. t.Parallel() is used at function and subtest level consistently. Gaps: (1) riverdriver has NO tests at all — translateJobInsert, parseCronSchedule, and the InsertTx invalid-tx path are pure and unit-testable yet uncovered; (2) Manager.Start/Stop lifecycle and the Manager.Enqueue ErrUnknownTask validation branch are not exercised by any unit test (healthcheck_test builds a Manager by hand but never starts it); (3) the SQLite dedup tests run single-threaded, so the concurrent-insert race in the dedup path is not surfaced. Test files correctly use require for critical assertions but lean on assert in several core-package tests where require would be more appropriate per project convention.

**Design-rule compliance:** Strong compliance. No reflection, no service container, no magic — task dispatch is generics + JSON type-erasure (options.go:61, task.go:52). No ID generation occurs in this package, so the pkg/id rule is not implicated (no uuid.New/ulid.Make found). Packages receive values via parameters, not context (executor signature passes taskName/payload explicitly). No exported method returns an unexported type. No redundant accessors for fields users already hold. One minor deviation from the 'use sync.Once for lazy write-once fields' guideline: riverdriver.getInsertClient (driver.go:182-199) lazily builds insertClient under a plain mutex + nil-check rather than sync.Once — this is defensible because initialization can return an error and the mutex approach permits retry on failure (sync.Once would cache the failure or require extra error plumbing), so I treat it as an acceptable, intentional exception rather than a violation. The only real rule-adjacent issue is documentation drift in doc.go (flagged above), which CLAUDE.md treats as a hard requirement to keep in sync.

---

### `randomname` — 8/10 · 🟡 Minor improvements

A solid, well-tested, concurrency-safe random-name generator with bias-free crypto/rand selection; the only real defects are a duplicate word, a wrong word-count constant in a test, and a "cryptographically secure" claim undercut by a predictable time-based fallback.

**Strengths:**
- Bias-free random selection in secureRandInt (generator.go:144-166) correctly computes maxValid = (2^32/n)*n and rejects biased samples, avoiding modulo bias. Numeric4 range [1000,9999] is exactly correct and matches the docs.
- Concurrency-safe by design: no shared mutable state; the sync.Pool builder pattern (generator.go:13-35) is used correctly (each Generate call leases/returns its own builder, recursion leases a distinct one). TestConcurrency exercises 10 goroutines x 100 iterations and asserts uniqueness + format.
- Clean, ergonomic API: Generate(*Options) plus seven well-named convenience constructors, all returning string (no unexported return types, no redundant accessors). nil Options is handled gracefully and the function is documented to never error.
- getWords (words.go:98-108) correctly copies into a fresh slice before appending custom words, so the package-level defaultWords map is never mutated by callers' custom lists.
- doc.go is thorough and its code examples match the real exported signatures (Options fields, WordType/SuffixType constants, Validator signature). Benchmarks cover suffixes, pattern complexity, validator rejection, and concurrent generation.

**Issues:**

| Sev | Category | Issue | Location |
|---|---|---|---|
| medium |  | Duplicate word "radiant" in Adjective list | `pkg/randomname/words.go:9,19` |
| medium |  | Test asserts wrong word count for Action (40 vs actual 36) | `pkg/randomname/words_test.go:48` |
| medium |  | "Cryptographically secure" claim is undercut by a predictable time-based fallback | `pkg/randomname/generator.go:155-158,170-173,184,194` |
| low |  | Tests use assert instead of require, against project convention | `pkg/randomname/generator_test.go:19; pkg/randomname/types_test.go:13; pkg/randomname/words_test.go:64` |
| low |  | Tests lack t.Parallel() at function and subtest level | `pkg/randomname/generator_test.go:16; pkg/randomname/types_test.go:11; pkg/randomname/words_test.go:13` |
| low |  | secureRandInt parameter shadows the built-in max | `pkg/randomname/generator.go:137` |
| low |  | Recursive fallback in Generate silently drops custom Words | `pkg/randomname/generator.go:60-67` |
| low |  | No README.md despite root README listing the package | `pkg/randomname/` |

**Tests:** Coverage is strong and behavior-focused, not just NoError: tests assert output format via regex, exact part counts, validator invocation counts (1, 3, and exactly 100 on max-retry), custom-vs-default word merging (verifying both appear), suffix formats/ranges, concurrency correctness (10x100 goroutines with uniqueness + format checks), and edge cases (empty pattern, invalid WordType(999), 10-word pattern, empty custom list). Gaps: (1) word-count assertions are loose lower bounds (expectedCount/3) which let a wrong constant (Action=40 vs 36) and a duplicate (radiant) slip through — exact-count/no-duplicate assertions are missing; (2) the crypto/rand-failure fallback path (time-based seed, zeroed hex) is untested, though it is hard to exercise without injection; (3) convention violations: assert used where require is expected for critical checks, and no t.Parallel() anywhere.

**Design-rule compliance:** Mostly compliant. No reflection, no service containers, no context-based value passing (Options passed explicitly by parameter), no magic. Public functions return only string and exported types — no unexported return types, no redundant accessors. The "all IDs via pkg/id/" rule does not apply: this package produces human-readable display names, not IDs, and correctly uses crypto/rand for its own randomness rather than reinventing ID generation. sync.Once is not relevant here (no lazy write-once field; the sync.Pool usage is appropriate). Violations against the testing section of the design rules: tests use assert instead of require for critical checks and omit t.Parallel() at function and subtest level (flagged as low-severity issues).

---

### `ratelimit` — 8/10 · 🟡 Minor improvements

A correct, well-tested sliding-window rate limiter with pluggable memory/Redis backends; clean design-rule compliance, with a few minor API/doc gaps (notably a dead ErrRateLimited export and undocumented increment-first semantics).

**Strengths:**
- Sliding-window algorithm is implemented correctly: computeInfo blends prev/current windows with proper time-decay weighting, and retryAfter solves the decay equation with correct guards against division-by-zero (prevCount==0), over-limit current window (currCount>=l.limit), and floating-point rounding (retryAt.Before(now) -> 0).
- Concurrency is handled properly: MemoryCounter is fully mutex-guarded, Close is idempotent via a closed bool guard, and RedisCounter.getFallback uses sync.Once for the lazy fallback exactly as the design rules require. The concurrent test asserts the exact final count (50), not just absence of a race.
- Strong design-rule compliance: no reflection/magic/service containers, no ID generation, values passed via parameters (not context), env tags match the framework convention (pkg/cache parity), and all public methods return exported types (*Limiter, Info, error).
- Counter abstraction is clean and minimal (Increment/Get/Close) with a compile-time interface assertion (var _ Counter) for both implementations.
- Tests assert real behavior, not just NoError: exact Remaining values, key isolation, prefix isolation, Redis fallback on s.Close(), sliding-window decay across two windows, and KeyComposite empty-value skipping. t.Parallel() is used at function and subtest level throughout, and miniredis is used as the in-process Redis mock per conventions.
- doc.go is comprehensive and its code examples match real signatures (verified redis.MustOpen(ctx, cfg) and New/Allow/Peek).

**Issues:**

| Sev | Category | Issue | Location |
|---|---|---|---|
| medium |  | ErrRateLimited is exported and documented but never returned by any code path | `pkg/ratelimit/errors.go:8` |
| medium |  | AllowN increment-first semantics are undocumented and can permanently reject an oversized batch within a window | `pkg/ratelimit/ratelimit.go:84-105` |
| low |  | Redis Increment and Get are independent round-trips with independent fallback, creating a brief inconsistency window | `pkg/ratelimit/redis.go:45-75` |
| low |  | KeyFunc HTTP extractors live in a storage/algorithm package | `pkg/ratelimit/keyfunc.go:1-50` |
| low |  | Sliding-window decay math is only bound-checked, never asserted exactly; RetryAfter/ResetAt numeric correctness untested | `pkg/ratelimit/ratelimit_test.go:246-271` |

**Tests:** Coverage is good and tests assert real behavior, not just require.NoError: exact Remaining/Limit values, per-key and per-prefix isolation, decrement-on-each-request, batch accept/reject, Peek non-mutation, sliding-window decay across two windows (expiry to 100), Redis fallback after s.Close(), expiry via miniredis FastForward, idempotent Close, janitor cleanup, and a 50-goroutine concurrency test asserting the exact final count. t.Parallel() is applied at both function and subtest level throughout, and miniredis is used per the in-process-mock convention. Gaps: the sliding-window blend is only bound-checked (20<rem<=100) rather than pinned to a deterministic value; RetryAfter magnitude and the prevCount>0 retryAfter solver branch are never asserted; KeyByFingerprint only checks non-empty. Because now is not injectable, the time-decay math is effectively smoke-tested. No test for n<=0 in AllowN.

**Design-rule compliance:** Compliant with the project design rules. No reflection, no service containers, no magic. No ID generation occurs, so the pkg/id rule is not implicated. Values are passed via parameters (key, limit, window, n) rather than pulled from context; HTTP context extraction is delegated to KeyFunc/middleware. Public methods return only exported types (*Limiter, Info, Counter, error) — no unexported return types leak. sync.Once is used correctly for the lazy write-once fallback field in RedisCounter (redis.go:99); MemoryCounter.Close uses a mutex+bool close-guard rather than sync.Once, which is appropriate since it is a close guard, not a lazy-init. env struct tags match the framework convention used in pkg/cache. No redundant accessors expose fields users already have. The only minor friction is keyfunc.go pulling HTTP concerns into the package (noted as low severity), which does not violate a stated rule.

---

### `sanitizer` — 8/10 · 🟡 Minor improvements

A broad, well-documented, heavily-tested input-sanitization utility package; the robust HTML/XSS path (bluemonday) is solid, with a few real polish gaps: byte-vs-rune filename truncation, a regex-based XSS default, tag-separator inconsistency with pkg/validator, and tags_test lacking t.Parallel.

**Strengths:**
- Extensive behavior-asserting test coverage: tests check exact output content (not just NoError), include strong edge cases (empty strings, Unicode, negative lengths), and html_test.go exercises a large battery of real XSS attack vectors against the bluemonday-backed StripHTML/SanitizeHTML.
- The primary HTML-sanitization path uses bluemonday (StrictPolicy/curated allowlist) rather than home-grown regex, with policies lazily initialized via sync.Once (html.go) — the correct, safe approach for the highest-risk surface.
- Excellent, example-rich doc.go that documents every tag, composite, and the Apply/Compose pipeline; examples generally match real signatures (including the strings.ToTitle uppercasing quirk, which is correctly reflected).
- Clean, idiomatic generics for numeric helpers (Numeric/Signed/Float constraints) and collection helpers; public API returns only exported/standard types (SanitizeHTMLCustom takes the third-party *bluemonday.Policy, which is acceptable).
- Package-scoped pre-compiled regexes in regex.go/security.go for the hot paths, and thread-safe custom-sanitizer registry guarded by sync.RWMutex.

**Issues:**

| Sev | Category | Issue | Location |
|---|---|---|---|
| medium |  | SanitizeFilename truncates on bytes, can split a multibyte UTF-8 rune | `pkg/sanitizer/format.go:303-305` |
| medium |  | safe_html tag and PreventXSS rely on regex scrubbing with known bypasses, not bluemonday | `pkg/sanitizer/security.go:71-79 (and tags.go:46,74-76)` |
| medium |  | Tag separator is comma, inconsistent with pkg/validator and the documented project convention | `pkg/sanitizer/tags.go:178` |
| low |  | Regexes recompiled on every call in SanitizeHTMLAttributes and RemoveSQLKeywords | `pkg/sanitizer/security.go:62-66, 97-101` |
| low |  | tags_test.go omits t.Parallel() entirely; whole suite uses assert instead of require | `pkg/sanitizer/tags_test.go (no t.Parallel), all *_test.go (assert)` |
| low |  | FilterSliceByPattern / FilterMapByKeys / FilterMapByValues use exclusion semantics that the names/docs don't make obvious | `pkg/sanitizer/collections.go:88-97, 144-156, 158-169` |
| low |  | No README.md in the package directory | `pkg/sanitizer/ (missing README.md)` |

**Tests:** Strong, broad coverage. Nearly every exported function has a dedicated table-driven test that asserts exact output content (not just require.NoError) with good edge cases: empty strings, Unicode/multibyte, negative and zero lengths, malformed emails/URLs. The security and HTML suites are the highlight — html_test.go runs a large battery of real-world XSS vectors (script/img/svg/iframe/meta/style-expression/data-URL/case-variation) through the bluemonday-backed StripHTML/SanitizeHTML and asserts dangerous tokens are absent; security_test.go includes realistic Compose pipelines for comments, file uploads, and SQL input. Gaps: (1) no test covers the multibyte-truncation path of SanitizeFilename (the one real correctness bug), (2) tags_test.go has zero t.Parallel() despite the project convention and exercises the reflection path, (3) the whole suite uses assert rather than require, and the custom-sanitizer test mutates the global registry without cleanup. Benchmarks exist for the main string/format/collection/tag paths.

**Design-rule compliance:** Mostly compliant. Public methods return only exported/standard types (SanitizeHTMLCustom returns string and takes the third-party *bluemonday.Policy, which is acceptable). sync.Once is correctly used for the lazily-built bluemonday policies (html.go). No service containers, no IDs generated (the pkg/id rule is N/A here). The notable tension is 'no reflection': SanitizeStruct uses reflect (tags.go) — but this mirrors the established framework pattern (pkg/validator and pkg/binder also use reflect, and internal/context.go consumes SanitizeStruct), so the 'no reflection/no magic' rule is best read as targeting core DI/wiring rather than these opt-in tag utilities; I treat it as compliant-by-convention rather than a violation. The concrete design-rule deviation worth flagging is cross-package inconsistency: the sanitize tag separator is ',' whereas pkg/validator and the CLAUDE.md gotcha specify ';' — documentation/convention mismatch rather than a hard rule break.

---

### `storage` — 8/10 · 🟡 Minor improvements

Well-structured, well-tested S3 storage utility with clean options API and strong path sanitization; main gaps are a PutFromURL SSRF vector, doc.go env-var names that don't match the struct tags, and a few unused exported sentinels.

**Strengths:**
- Clean functional-options API (Option/URLOption) with focused, well-documented constructors; interface Storage is minimal and the concrete S3Storage adds HeadObject/Copy as extras without bloating the interface.
- Path sanitization (sanitizePathSegment, s3.go:251) is solid: trims slashes, strips '..', regex-replaces unsafe chars, then url.PathEscape. Verified empirically that traversal inputs like '../../etc', 'a/../../b', '%2e%2e' cannot escape to nested paths.
- IDs are generated exclusively via pkg/id (id.NewULID in s3.go:196) per the project rule — no uuid/ulid direct calls.
- MIME detection is magic-byte based (http.DetectContentType) rather than trusting client filename/extension, which is the secure default; detectMIMEWithReader correctly seeks seekable readers back to start and only buffers non-seekable ones.
- Error wrapping uses %v (not %w) deliberately to normalize AWS error types, with a comment explaining callers should use errors.Is on sentinels; wrapS3Error covers both smithy.APIError codes and typed *types.NoSuchKey.
- Excellent test coverage: gofakes3 in-memory backend for real Put/Get/Delete/URL/Head/Copy round-trips, httptest servers for PutFromURL boundary cases (exact maxSize, +1 byte, missing Content-Length, context cancellation, connection refused), and tests assert behavior (content equality, signature presence in URLs, error codes) not just NoError. t.Parallel() is used at function and subtest level throughout.

**Issues:**

| Sev | Category | Issue | Location |
|---|---|---|---|
| medium | security | PutFromURL is a server-side request forgery (SSRF) vector | `pkg/storage/helpers.go:62` |
| medium | docs | doc.go documents env var names that do not match the struct tags | `pkg/storage/doc.go:79` |
| medium | performance | MaxSize validation runs after the entire non-seekable reader is buffered into memory | `pkg/storage/s3.go:84` |
| low | api-design | Several exported sentinel errors are never returned by the package | `pkg/storage/errors.go:13` |
| low | api-design | PutBytes silently ignores its filename parameter | `pkg/storage/helpers.go:48` |
| low | api-design | WithPrefix collapses path separators, so multi-segment prefixes become a single sanitized segment | `pkg/storage/s3.go:259` |
| low | bug | Put sets ContentLength/FileInfo.Size from the caller-supplied size even after buffering actual bytes | `pkg/storage/s3.go:111` |

**Tests:** Strong, behavior-focused coverage. s3_test.go exercises real round-trips against an in-memory gofakes3 backend and asserts side effects: content equality on Get/Copy, ACL propagation into FileInfo, MIME detection result and .png key suffix, signed-URL signature presence (X-Amz-Signature), public URL signature absence, and response-content-disposition for downloads. helpers_test.go covers PutFromURL boundaries thoroughly (exact maxSize allowed, +1 byte rejected, Content-Length-based and actual-read-based rejection, empty body, context cancellation, connection refused) and asserts wrapped error identity and embedded status codes. validation/mime/errors/options tests assert error Codes and Details map keys, not just NoError, and use table-driven form appropriately. t.Parallel() is consistently applied at function and subtest level. Gaps: no test asserts the SSRF/private-IP behavior of PutFromURL (because there is no guard), no test for the non-seekable-reader-buffering-before-MaxSize path, and the unused sentinels (ErrFileTooLarge etc.) are only checked for distinctness, not for any producing code path.

**Design-rule compliance:** Largely compliant. No reflection, no service containers, no context-smuggling of values (ctx is used only for cancellation; tenant/prefix/ACL flow through parameters/options). IDs are generated exclusively via pkg/id.NewULID (s3.go:196) per the no-custom-ID rule. Public methods return exported types (*FileInfo, io.ReadCloser, string, error); Option/URLOption are exported function types over unexported option structs, which is the standard functional-options pattern and does not leak unexported types through method signatures. No redundant accessors. The one design-rule deviation is documentation: doc.go's env-var section does not match the actual struct tags and departs from the sibling smtp package's documented convention (CLAUDE.md requires doc.go to match real signatures/tags).

---

### `webhook` — 8/10 · 🟡 Minor improvements

A solid, well-tested webhook delivery package (signing, retries/backoff, circuit breaker) that builds clean and passes; the main gaps are an undocumented SSRF surface and a half-open doc/behavior mismatch, plus a few option-validation inconsistencies.

**Strengths:**
- Clean, idiomatic API: functional options, exported interface BackoffStrategy with three concrete strategies, all exported methods return exported types (DeliveryResult, SignatureHeaders, CircuitState, CircuitStats) — no unexported leakage.
- Correct HMAC-SHA256 signing with timestamp binding and hmac.Equal constant-time comparison; replay protection via maxAge window including a future-timestamp clamp.
- Complies with project ID rule: webhook ID generated via pkg/id (id.NewULID()), not uuid/ulid directly.
- Strong, behavior-asserting tests: verifies request method/headers/body, round-trips real signatures through ExtractSignatureHeaders+VerifySignature, asserts exact attempt counts, circuit-state transitions, response-body truncation, and timing-attack resistance (identical error strings for different invalid sigs). t.Parallel() used at function and subtest level throughout, plus concurrency tests with -race and benchmarks.
- Sensible defaults and guards: response body read via io.LimitReader, error body sanitized (newlines stripped, truncated to 200 chars), nil http.Client and nil response handled, permanent vs temporary 4xx classification (408/425/429 retried).
- doc.go is accurate — every signature, option name, and error variable referenced in the examples matches the real source.

**Issues:**

| Sev | Category | Issue | Location |
|---|---|---|---|
| medium |  | No SSRF protection or documentation for user-supplied webhook URLs | `pkg/webhook/webhook.go:117` |
| medium |  | Half-open circuit breaker allows unlimited concurrent probes despite doc claiming one | `pkg/webhook/circuit.go:87` |
| low |  | Circuit breaker is consulted only once per Send, not per retry attempt | `pkg/webhook/webhook.go:73` |
| low |  | Inconsistent option validation: negative retries and zero response-size | `pkg/webhook/options.go:141` |
| low |  | VerifySignature skips future-timestamp check when maxAge <= 0 | `pkg/webhook/signature.go:72` |
| low |  | Tests use assert where require is expected for critical checks | `pkg/webhook/webhook_test.go:50` |

**Tests:** Coverage is broad and behavior-focused: success path verifies method/headers/body and JSON round-trip; signing tests round-trip real signatures through SignPayload -> ExtractSignatureHeaders -> VerifySignature and reconstruct the HMAC manually to pin the algorithm; retry tests assert exact attempt counts and error classification across a 4xx/5xx table; circuit tests cover all four state transitions, recovery timeout, reset, stats, and default-value handling; payload/response size limits and truncation are checked against the actual error message length. Concurrency is exercised with -race in dedicated tests, and there is a full benchmark suite. These go well beyond require.NoError. Gaps: no test asserts the X-Webhook-* headers are actually transmitted on the wire with the correct signature in the failure/retry path (only the success path), no negative-attempts test for WithBasicRetry/WithExponentialRetry, and no test pins the half-open single-probe expectation (the existing test instead asserts unlimited probes). Convention nit: assert is used where require is specified for critical checks.

**Design-rule compliance:** Compliant with the core design rules. No reflection, no service containers, no context-as-parameter-bag (ctx is used only for cancellation/deadline, values flow through explicit parameters and functional options). Public methods return only exported types — DeliveryResult, SignatureHeaders, CircuitState, CircuitStats, and the BackoffStrategy interface are all exported; no unexported type leaks across the API. ID generation correctly goes through pkg/id (id.NewULID() in signature.go:43) rather than uuid.New/ulid.Make. No sync.Once is needed here because there are no lazy-initialized write-once fields (Sender/CircuitBreaker are fully initialized in their constructors). No redundant accessors. The one design-adjacent concern is the SSRF surface (framework leaves destination trust entirely to the consumer without saying so), which is a documentation gap rather than a rule violation.

---

### `mailer` — 7/10 · 🔴 Needs work

Well-architected, mostly well-tested email package with clean provider abstraction, but the SMTP adapter mishandles display names containing commas (silently fails to send), omits Date/Message-ID headers, and the resend provider has zero tests.

**Strengths:**
- Clean, idiomatic architecture: Sender interface, Renderer, and Mailer are cleanly separated, enabling provider swapping. Complies with all design rules (no reflection, no context value-smuggling, no service containers, no ID generation, no unexported return types).
- Renderer caching is correctly thread-safe: double-checked locking with sync.RWMutex (renderer.go:118-188), and TestRenderer_Render_ConcurrentAccess + TestRenderer_Render_CachesTemplates verify both correctness and the cache hit/miss behavior via a counting FS.
- Good error design: sentinel errors in errors.go combined with errors.Join wrapping (mailer.go:56,72,87; renderer.go) lets callers use errors.Is on both the category and the underlying cause; tests assert this (e.g. mailer_test.go:128-129).
- Button extension escapes both URL and label via util.EscapeHTML (button_extension.go:130,132); TestButtonExtension_EscapesHTML proves XSS-style input is neutralized.
- SMTP MIME builder is thorough and correct: chooses single/alternative/mixed/nested-multipart appropriately, base64 for attachments, quoted-printable for bodies, Q-encodes non-ASCII subjects, and correctly excludes BCC from headers while including it in the SMTP envelope. mime_test.go and send_test.go assert real content/headers, not just NoError.
- Frontmatter parser handles both \n and \r\n line endings (template.go:42-48) with explicit tests for each, plus edge cases (empty, whitespace-only, missing delimiter, code blocks containing ---).

**Issues:**

| Sev | Category | Issue | Location |
|---|---|---|---|
| high |  | Display name containing a comma breaks SMTP sending entirely | `pkg/mailer/smtp/sender.go:31, pkg/mailer/types.go:24-28` |
| medium |  | SMTP messages omit Date and Message-ID headers | `pkg/mailer/smtp/mime.go:22-35` |
| medium |  | resend provider package has zero tests | `pkg/mailer/resend/sender.go` |
| low |  | Stale test-strategy docs reference non-existent files and outdated coverage | `pkg/mailer/smtp/TEST_REVIEW.md, pkg/mailer/smtp/README_TESTS.md` |
| low |  | Inline-attachment ContentID is not sanitized for pre-existing angle brackets | `pkg/mailer/smtp/mime.go:182-184` |
| low |  | Subject template recompiled on every Send | `pkg/mailer/mailer.go:112-124` |

_Adversarially verified: 1/1 high/critical findings confirmed real._

**Tests:** Strong behavioral testing in the core package and SMTP adapter; the resend provider is the notable gap. mailer_test.go asserts real outcomes via mock.MatchedBy on recipient, resolved subject, templated subject, and optional fields (CC/BCC/From/ReplyTo/attachments), plus all SendRaw validation paths and error wrapping with ErrorIs on both sentinel and cause. renderer_test.go verifies plain-text-vs-HTML separation, cache hit/miss counts via a counting FS, per-data output divergence, and 100-goroutine concurrency. template_test.go and button_extension_test.go cover extensive edge cases (line endings, empty/whitespace frontmatter, invalid YAML, XSS escaping, incomplete syntax, multiple buttons). SMTP send_test.go uses an in-process go-smtp-mock server and asserts MIME structure, envelope recipient counts (To+CC+BCC=5), From fallback, Reply-To, custom headers, UTF-8 subject encoding, and bare-email envelope extraction from RFC 5322 inputs. Tests use t.Parallel() consistently at function and subtest level and use require throughout. Gaps: (1) resend has no tests at all, including the non-trivial tagValue switch; (2) no test exercises a display name containing a comma, which is exactly the input that triggers the high-severity bug; (3) no assertion for Date/Message-ID presence.

**Design-rule compliance:** Compliant with the project design rules. No reflection, no service containers, no magic. Values are passed as parameters (SendParams, Email, Config), not smuggled through context. Public methods return exported types only (*Mailer, *Renderer, *RenderResult, *Template, error). No redundant accessors. No ID generation in the package, so the pkg/id rule is not violated (multipart boundaries come from stdlib mime/multipart, which is acceptable). Config structs use caarlos0/env tags consistently. One minor note: the package does not use sync.Once because it has no lazy write-once fields (the renderer uses an explicit double-checked RWMutex cache instead, which is appropriate for a keyed map rather than a single write-once value) - not a violation.

---

### `redis` — 7/10 · 🔴 Needs work

A clean, well-scoped Redis connection helper (Open/MustOpen/Healthcheck/Shutdown) with solid validation tests, but the retry loop wastes the final backoff interval, drops the underlying ping error despite docs promising otherwise, and one doc example won't compile.

**Strengths:**
- Small, focused, idiomatic surface: Config + Open/MustOpen/Healthcheck/Shutdown, all returning the exported go-redis interface type (no unexported return types).
- Good defensive validation: empty-URL and scheme checks return distinct sentinel errors before touching go-redis; Healthcheck guards against a nil client.
- Sentinel errors are well-named and the validation/healthcheck paths correctly use errors.Join to preserve context.
- Shutdown takes io.Closer rather than a concrete client, keeping it trivially testable and decoupled.
- Tests assert real behavior, not just NoError: error identity via errors.Is, wait() timing bounds (lower and upper), and Close side-effect/error propagation. t.Parallel() is used consistently at function and subtest level per project conventions.
- doc.go is thorough and the Health Check and Graceful Shutdown sections (Run-based) are accurate; package compiles and tests pass.

**Issues:**

| Sev | Category | Issue | Location |
|---|---|---|---|
| high |  | Retry loop sleeps a full backoff interval after the final failed attempt | `pkg/redis/connect.go:104-118` |
| medium |  | ErrConnectionFailed discards the underlying ping error, contradicting documented behavior | `pkg/redis/connect.go:107,118` |
| medium |  | shutdown.go doc example uses a signature that does not compile | `pkg/redis/shutdown.go:11-19` |
| medium |  | Core retry/backoff and happy-path connect logic are untested | `pkg/redis/redis_test.go` |
| low |  | Open returns redis.UniversalClient but always constructs a single-node NewClient | `pkg/redis/connect.go:62,101-105` |
| low |  | applyDefaults duplicates the envDefault tag values (two sources of truth) | `pkg/redis/connect.go:14-58` |

_Adversarially verified: 1/1 high/critical findings confirmed real._

**Tests:** Tests are above average in quality and genuinely assert behavior rather than just NoError: error identity via errors.Is, wait() timing with both lower- and upper-bound assertions, and Close side-effects/error propagation through a mockCloser. t.Parallel() is applied at function and subtest level throughout, matching project conventions; table-driven cases are used appropriately for the simple validation matrix. Coverage gap is the important part: the validation surface, wait(), nil Healthcheck, Shutdown, and applyDefaults are well covered, but the package's most defect-prone code — connect()'s retry/backoff loop, the trailing-wait bug, error joining on connection failure, and the Open/Healthcheck happy paths — has zero coverage. miniredis (already used in pkg/cache per CLAUDE.md) makes adding behavioral tests for these straightforward without Docker.

**Design-rule compliance:** Compliant with the project design rules. No reflection, no service containers, no magic. Values are passed via parameters, not context (Healthcheck/Shutdown take the client directly). Public methods return the exported go-redis interface types, not unexported types. No redundant accessors are exposed. No ID generation occurs, so the pkg/id rule is not implicated. sync.Once is not applicable here (no lazy write-once fields). One documentation accuracy rule from CLAUDE.md ('doc.go and README must match actual signatures') is violated by the shutdown.go inline example, which uses forge.New(WithShutdownHook(...)) instead of the correct forge.Run(...) form; this is captured as a medium issue.

---

### `slug` — 7/10 · 🔴 Has bugs

A clean, well-documented, allocation-conscious slug generator with accurate docs and strong behavioral tests, marred by one confirmed correctness bug in the minLength+maxLength "no room" path and test-convention violations (missing t.Parallel, assert instead of require).

**Strengths:**
- Core Make logic is correct and carefully written: rune-based length counting (not bytes), no leading/trailing/consecutive separators via the lastWasSep guard, with TrimSuffix as a safety net. Verified across many edge cases including multi-char separators and emoji.
- doc.go examples all match real output (verified by running them): Café & Restaurant => cafe-restaurant, München straße => munchen-strase, naïve résumé => naive-resume, etc. Documentation is honest and accurate.
- Performance-conscious: strings.Builder with Grow pre-allocation, single-pass rune iteration, ASCII fast-path before the diacritic map lookup. Includes serial, parallel, and per-scenario benchmarks.
- generateSuffix uses crypto/rand with a sensible deterministic fallback on rand.Read error (a rare but handled path), and respects the lowercase setting for the charset.
- Clean idiomatic functional-options API (Option func(*config)), no exported unexported types, no reflection, no context misuse — compliant with the framework's core design rules.
- Tests assert real behavior (exact content, lengths, prefixes, regex on suffix charset, randomness-differs-across-calls), not just require.NoError — this is the right kind of testing.

**Issues:**

| Sev | Category | Issue | Location |
|---|---|---|---|
| high |  | minLength+maxLength "no room" path discards the real slug and undershoots minLength | `pkg/slug/slug.go:256-263` |
| low |  | Modulo bias in random suffix generation | `pkg/slug/slug.go:362-366` |
| low |  | Custom random-token generation overlaps the 'IDs via pkg/id only' design rule | `pkg/slug/slug.go:343-367` |
| low |  | WithReservedSlugs + WithMinLength produces a confusing double suffix | `pkg/slug/slug.go:198-275` |
| low |  | diacriticMap simplifications may surprise users (ß→s, æ→a) | `pkg/slug/slug.go:327-331` |

_Adversarially verified: 1/1 high/critical findings confirmed real._

**Tests:** Coverage is broad and the tests assert real behavior, not just require.NoError: exact slug content, lengths, prefixes, regex on suffix charset (lowercase vs mixed case), and randomness-differs-across-calls. Good edge-case coverage for diacritics, emoji, tabs/newlines, empty input, multi-char/empty separators, and suffix/maxLength interactions. Two real gaps: (1) the minLength+maxLength 'no room' branch (slug.go:256-263) is never exercised, which is exactly why the confirmed undershoot/content-loss bug slipped through — every min+max test leaves room for the suffix. (2) Convention violations: only 3 of 7 test functions use t.Parallel() (TestMake, TestNormalizeDiacritic, TestReservedSlugs, TestGenerateSuffixErrorHandling, TestMaxLengthEdgeCases, TestMakeWithSuffix lack it at both function and subtest level), and the entire suite uses testify assert instead of require, which CLAUDE.md mandates for critical checks. TestGenerateSuffixErrorHandling's name overpromises — it never actually forces a rand.Read failure (the deterministic-fallback path at slug.go:355-359 is uncovered), it just re-runs the happy path.

**Design-rule compliance:** Mostly compliant with CLAUDE.md. Good: no reflection, no service containers, idiomatic functional-options API, values passed via parameters (no context), no exported method returns an unexported type, it's a pure utility package with no business logic. Violations/concerns: (1) Testing conventions — missing t.Parallel() in 4 of 7 test functions and use of assert instead of require throughout, both explicitly required by CLAUDE.md. (2) Design-rule gray area — generateSuffix is bespoke crypto/rand token generation, which brushes against the 'all IDs via pkg/id, no custom ID generation' rule; defensible since the suffix is a variable-length padding token rather than a primary ID, but worth a maintainer ruling. doc.go and README expectations are met: all documented examples were run and match actual output.

---

### `sqlitedb` — 7/10 · 🔴 Needs work

A clean, well-organized SQLite utility package that mirrors pkg/db nicely, but has a real per-connection PRAGMA footgun if MaxOpenConns > 1, a doc/default inconsistency for foreign_keys, an incorrect forge.Run doc example, and one dead sentinel error.

**Strengths:**
- Exported surface cleanly mirrors the sibling pkg/db (Open, MustOpen, Healthcheck, Shutdown, WithTx, Migrate, Config, Option), which is exactly what doc.go promises and aids consumer ergonomics.
- No reflection, no service containers, no IDs generated — fully compliant with the framework design rules. Healthcheck/Shutdown return the standard func(context.Context) error matching forge.CheckFunc / WithShutdownHook signatures.
- WithTx correctly handles panic rollback and re-raise, and the test (transaction_test.go:59) actually asserts the panic value AND that the row was rolled back — real behavioral assertion, not just NoError.
- applyPragmas applies PRAGMAs in the documented correct order (journal_mode first) and wraps each failure with the offending PRAGMA string for debuggability (connect.go:153).
- Migration resolveMigrationsFS lets callers embed at any depth, and tests verify migrations by inserting/selecting real rows (connect_test.go:122, migrator_test.go:24) rather than just checking no error.
- Errors are wrapped with sentinels via errors.Join and tests assert errors.Is against them (ErrInvalidConfig, ErrHealthcheckFailed). t.Parallel() is used on every test function.

**Issues:**

| Sev | Category | Issue | Location |
|---|---|---|---|
| high |  | Per-connection PRAGMAs are not applied to all pooled connections | `pkg/sqlitedb/connect.go:137-158` |
| medium |  | foreign_keys default contradicts documentation when Config is built in Go (not via env) | `pkg/sqlitedb/connect.go:114-133 and doc.go:20` |
| medium |  | doc.go forge.Run example does not match the real forge.Run / RunConfig API | `pkg/sqlitedb/doc.go:43-45` |
| low |  | ErrSetDialect is declared but never used (dead code) | `pkg/sqlitedb/errors.go:16-17` |
| low |  | MustOpen ignores the provided logger and hardcodes slog.Error before os.Exit | `pkg/sqlitedb/connect.go:104-111` |

_Adversarially verified: 1/1 high/critical findings confirmed real._

**Tests:** Good coverage of the happy paths and several edge cases, and tests genuinely assert behavior rather than just require.NoError: PRAGMA values are read back and checked (connect_test.go:55-70), migrations are verified by INSERT/SELECT of real rows, rollback/commit/panic-rollback are each confirmed by row counts (transaction_test.go), and closed-DB healthcheck/shutdown are exercised. t.Parallel() is present on every test function. Notable gaps: (1) no test exercises MaxOpenConns > 1, which is exactly where the per-connection PRAGMA bug would surface; (2) no test for journal_mode=wal on a real file DB (the WAL path is skipped because tests use :memory:, where WAL is silently ignored — connect_test.go:50); (3) no test for resolveMigrationsFS picking the wrong directory or for a malformed migration; (4) ErrOpenFailed / ErrApplyMigrations error paths are never asserted (e.g. a bad PRAGMA value or invalid migration SQL). Note the project uses require over assert per CLAUDE.md, but these tests mix in assert for non-critical follow-up checks — acceptable but slightly off-convention.

**Design-rule compliance:** Compliant with the core design rules: no reflection, no service containers, no magic; no ID generation (pkg/id rule N/A); public methods/functions return only exported types (*sql.DB, error, func(context.Context) error) — no unexported types leak. Values are passed via parameters, not context. The two violations against CLAUDE.md are documentation-accuracy ones: doc.go's forge.Run example does not match the real signature (RunConfig has Address, not Port; Run takes no app arg), and doc.go's foreign_keys 'on by default' claim does not hold for Go-constructed Config. No README.md exists in the package (doc.go serves as the doc), so the 'README must match signatures' rule is satisfied vacuously, but the doc.go example must be fixed.

---

### `db` — 6/10 · 🔴 Needs work

A small, clean pgxpool wrapper with solid core helpers, but it has zero tests, several materially inaccurate doc.go claims (env var names, exponential backoff, a nonexistent db.Pool type, a documented-but-nonexistent migrations table config), and a couple of real error-handling gaps in the retry path and migrator resource handling.

**Strengths:**
- WithTx is correct and idiomatic: rolls back on error, re-panics on panic after rollback (recover-then-panic preserves the original panic value, avoiding the *runtime.PanicNilError gotcha since it only re-raises a non-nil recover), commits on success.
- Open applies sensible defaults both via env tags and defensively in code, parses the URL with pgxpool.ParseConfig, pings to verify liveness, and closes the pool if migrations fail (connect.go:108-113) so no leaked pool on partial init.
- Public surface returns concrete/standard types only (*pgxpool.Pool, pgx.Tx, func(context.Context) error); no unexported types leak out, satisfying the design rule. The unexported gooseLoggerAdapter is purely internal.
- connect() guards Ping failures by closing the pool before retrying (connect.go:150), avoiding pool leaks across retry attempts, and respects context cancellation during backoff via wait().
- gooseLoggerAdapter.Fatalf deliberately logs at error level instead of calling os.Exit, allowing goose's returned error to propagate for proper shutdown (migrator.go:61-65) — a thoughtful choice.
- Healthcheck and Shutdown are minimal closures that integrate cleanly with the framework's health-endpoint and shutdown-hook interfaces.

**Issues:**

| Sev | Category | Issue | Location |
|---|---|---|---|
| high |  | No tests at all for the entire package | `pkg/db/ (no *_test.go present)` |
| medium |  | doc.go environment-variable documentation does not match the actual struct tags | `pkg/db/doc.go:18-26 vs pkg/db/connect.go:16-23` |
| medium |  | doc.go documents DATABASE_MIGRATIONS_TABLE but the table name is hardcoded | `pkg/db/doc.go:26; pkg/db/migrator.go:19,34` |
| medium |  | doc.go references a db.Pool type that does not exist | `pkg/db/doc.go:65` |
| medium |  | connect() discards the underlying connection error on total failure | `pkg/db/connect.go:140-160` |
| low |  | doc.go claims exponential backoff but the retry is linear | `pkg/db/doc.go:9 vs pkg/db/connect.go:143,151` |
| low |  | Final retry attempt performs a wasted backoff wait before returning | `pkg/db/connect.go:140-158` |
| low |  | Migrate leaks the *sql.DB and the migrator.go comment about it is incorrect | `pkg/db/migrator.go:29-31` |
| low |  | Migrate relies on goose package-global mutable state and is not concurrency-safe | `pkg/db/migrator.go:33-42` |

_Adversarially verified: 1/1 high/critical findings confirmed real._

**Tests:** There are zero tests in the package (no *_test.go, confirmed by find across the repo). Nothing is asserted — not the WithTx commit/rollback/re-panic branches, not the connect() retry/backoff loop, not Open's default-filling, not Healthcheck/Migrate error wrapping. This is a genuine and significant gap: the package is not trivial (it has branchy retry logic and a panic-safe transaction helper that are easy to break silently). At minimum, in-process unit tests are feasible for WithTx (commit/rollback/panic), Open's empty-URL guard and default-filling, and Healthcheck's errors.Is(ErrHealthcheckFailed) wrapping; integration tests (just test-integration) should cover Open+Migrate+WithTx against real Postgres, asserting committed vs rolled-back rows rather than only require.NoError.

**Design-rule compliance:** Mostly compliant with CLAUDE.md design rules. No reflection, no service containers, no magic. Public methods return only concrete/standard types (*pgxpool.Pool, pgx.Tx, func(context.Context) error) — no unexported types leak (the unexported gooseLoggerAdapter is internal-only). Packages receive values via parameters, not context (Open/Migrate/WithTx/Healthcheck all take explicit args). No ID generation occurs, so the pkg/id rule is not engaged. No redundant accessors and no lazy write-once fields, so the sync.Once rule does not apply. The clear violation is the documentation rule: CLAUDE.md states doc.go must match actual signatures, but doc.go misnames the env vars, documents a nonexistent DATABASE_MIGRATIONS_TABLE knob, references a nonexistent db.Pool type in a compile-broken example, and claims exponential backoff that the code does not implement.

---

### `jwt` — 6/10 · 🔴 Needs work

A correct, secure HS256 JWT utility with solid constant-time verification, but undermined by a silent-no-temporal-validation footgun, dead exported error surface, a string-context-key doc example, and tests that lean on assert and never check token structure.

**Strengths:**
- Core crypto is correct and safe: fixed HS256 via hardcoded sign(), constant-time signature comparison (subtle.ConstantTimeCompare on equal-length 43-char base64url strings), and RawURLEncoding (unpadded) per RFC 7515.
- Algorithm-confusion / alg:none is structurally prevented because the signature is always recomputed with HS256 regardless of the header alg; the explicit header.Algorithm check is belt-and-suspenders.
- Signature is verified BEFORE claims are unmarshalled and BEFORE temporal validation runs, so untrusted token bytes are authenticated before being trusted (jwt.go:122-158).
- Clean, dependency-free implementation (stdlib crypto only), zero reflection, no magic — fully aligned with the framework's no-magic design rule.
- Sentinel errors are wrapped/returned with errors.Is-friendly values, and decode/marshal failures are wrapped with %w for context.
- Benchmarks are thorough (Generate/Parse/End2End for standard and custom claims) and assert a real field round-trips rather than just timing.

**Issues:**

| Sev | Category | Issue | Location |
|---|---|---|---|
| medium |  | Parse silently skips ALL temporal validation when claims don't implement Valid() | `pkg/jwt/jwt.go:153-158` |
| medium |  | Three exported errors are never returned (dead API surface) | `pkg/jwt/errors.go:8-11` |
| medium |  | doc.go middleware example uses a string context key and context-based passing (vet warning + design-rule conflict) | `pkg/jwt/doc.go:108-110` |
| medium |  | Tests use assert for behavioral checks and never verify token structure | `pkg/jwt/jwt_test.go:7,54-55,105-107` |
| low |  | Audience modeled as a single string breaks RFC 7519 interop | `pkg/jwt/jwt.go:32` |
| low |  | No clock-skew leeway on exp/nbf | `pkg/jwt/jwt.go:40-52` |
| low |  | nbf failure returns generic ErrInvalidToken, conflating distinct cases | `pkg/jwt/jwt.go:47-49` |
| low |  | New does not enforce the documented 32-byte minimum signing key | `pkg/jwt/jwt.go:67-75` |

**Tests:** Tests cover the main happy paths (generate/parse for standard and custom claims), nil claims, malformed token, tampered signature, expired token, future (nbf) token, and cross-key signature rejection — decent breadth. But quality is mixed: behavioral assertions use assert instead of the required require; the 'standard claims' generate test has a vacuous assertion (len(token) > 0) whose comment promises a 3-part structural check it never performs; the header (typ/alg) is never decoded and verified; there is no tampered-claims-segment test, no forged-algorithm-header test, and no test proving whether temporal validation is skipped for non-Valid() claims types (the key footgun). Subtests omit t.Parallel(). Benchmarks are good and do assert a round-tripped field. Net: tests catch regressions in the common path but miss the package's most important edge cases and partly assert nothing meaningful.

**Design-rule compliance:** Mostly compliant. No reflection, no service containers, no magic; Service is constructed via New(Config) and exposes no redundant accessors; public methods return only exported types (the Valid()-detection uses an inline interface internally, not a returned unexported type). The jti claim is user-supplied, so the 'all IDs via pkg/id' rule does not apply here (the package generates no IDs itself). One concrete violation: the doc.go middleware example stuffs claims into request context with a bare string key, directly contradicting the rule 'Packages receive values via parameters, not context; middleware handles context extraction' and modeling a vet-flagged anti-pattern. doc.go signatures otherwise match the real API (jwt.New, jwt.Config{SigningKey}, Service.Generate, Service.Parse, StandardClaims fields all verified accurate), except it documents three error values that the code never returns.

---

### `totp` — 6/10 · 🔴 Needs work

Cryptographically sound TOTP/AES/recovery-code utility with correct RFC implementations, but the URI builder advertises configurable algorithm/digits/period that the validator silently ignores, plus several doc and test-quality gaps.

**Strengths:**
- Core crypto is correct and verified against RFC 6238 test vectors: GenerateHOTP(key,1,8) produces 94287082 (the canonical RFC vector), confirming the HMAC-SHA1 dynamic-truncation implementation is right.
- AES-256-GCM encryption is implemented correctly: per-message random nonce via crypto/rand, nonce prepended to ciphertext, key length strictly enforced (aes256.go:19-21,33-39), ciphertext-too-short guard before slicing (aes256.go:65-67).
- Recovery-code verification uses subtle.ConstantTimeCompare on fixed-length SHA-256 hex digests, correctly avoiding timing side channels (recovery.go:39-46).
- All randomness uses crypto/rand (secret, nonce, key, recovery codes); 160-bit secret per RFC 4226 and 256-bit AES key are appropriately sized.
- Errors are well-named, wrapped with errors.Join so callers can errors.Is the sentinel cause, and consistently returned; no panics on bad input.
- Clean, parameter-based API with no reflection, no context misuse, no service container, no ID generation — fully compliant with the framework design rules.
- Tests cover the happy path plus the important edge cases (invalid base32, bad OTP format, short ciphertext, zero/negative recovery counts) and the package builds and passes with -race.

**Issues:**

| Sev | Category | Issue | Location |
|---|---|---|---|
| high |  | GetTOTPURI advertises Algorithm/Digits/Period that ValidateTOTP and GenerateTOTP silently ignore | `pkg/totp/otp.go:96-98,118-135,150-155` |
| medium |  | ValidateTOTP/GenerateTOTP/GenerateTOTPWithTime allow zero-length (empty) secret to pass the regex but produce an all-zero HMAC key | `pkg/totp/otp.go:118` |
| medium |  | Test suite uses assert instead of require for critical checks and contains a truncated/dead test table | `pkg/totp/otp_test.go:52,140; pkg/totp/aes256_test.go:60` |
| medium |  | No test asserts the actual content/value of generated codes or HOTP against known RFC vectors | `pkg/totp/otp_test.go:145-178` |
| low |  | Doc/cmd reference TOTP_ENCRYPTION_KEY but the struct env tag is ENCRYPTION_KEY | `pkg/totp/config.go:4; pkg/totp/doc.go:123; pkg/totp/cmd/main.go:17` |
| low |  | doc.go recovery-code example uses a non-hex string | `pkg/totp/doc.go:92` |
| low |  | GenerateRecoveryCodes does not guarantee uniqueness; relies on probability | `pkg/totp/recovery.go:19-27` |
| low |  | ValidateSecretKeyRegex and DefaultDigits/DefaultPeriod/DefaultAlgorithm are exported as mutable package vars/consts with no clear consumer need | `pkg/totp/otp.go:25` |

_Adversarially verified: 1/1 high/critical findings confirmed real._

**Tests:** Tests run green with -race and cover the main edge cases (invalid base32, malformed OTP, short ciphertext, zero/negative recovery counts, empty inputs, constant-time verify consistency). However they lean on assert rather than the required require for critical assertions, and most assertions check bounds or NoError rather than known-answer values — the crypto core (GenerateHOTP) is never validated against an RFC test vector despite the implementation actually being correct. TestGetTOTPURI's table is truncated to two cases (a '// rest of the test cases remain the same' placeholder remains), leaving the error path and GetDefaults output untested. No negative test for decrypt-with-wrong-key and no test asserting EncryptSecret produces distinct ciphertexts (nonce randomness). t.Parallel is correctly applied at function and subtest level throughout.

**Design-rule compliance:** Compliant with the core design rules: no reflection, no service container, no magic; functions receive secrets/keys/config as parameters rather than pulling from context; no ID generation (so the pkg/id rule is N/A); public methods return only exported types (GetDefaults returns the exported TOTPParams). Minor friction: ValidateSecretKeyRegex is an exported mutable package var that consumers don't need and could clobber, which sits against the 'don't expose internals' spirit. The Config env tag is unprefixed (ENCRYPTION_KEY), consistent with sibling packages (oauth, mailer) that expect a consumer-applied prefix, but the docs/cmd advertise TOTP_ENCRYPTION_KEY without explaining the prefix mechanism.

---

### `useragent` — 6/10 · 🔴 Needs work

A capable, well-documented UA parser with good benchmarks, but marred by production code hardcoded to satisfy a specific test, a few brittle/over-broad detection patterns, a garbled version-truncation path, and tests that violate the repo's require-over-assert and behavior-assertion conventions.

**Strengths:**
- Clean, well-organized API surface: Parse/New plus value-receiver UserAgent methods; exported types (Browser, BrowserPattern) are concrete structs, so no public method returns an unexported type.
- doc.go is accurate and matches real signatures (Parse, New, DeviceType/OS/BrowserName/BrowserVer, IsMobile/IsBot, error sentinels); the fallback example using New(...) compiles against the real constructor.
- No reflection, no service containers, no context-smuggling, no ID generation - fully compliant with the framework's core design rules.
- Thoughtful performance design: two-tier bot detection (fast string-contains before regex), keywordSet maps, version-length caps to avoid pathological inputs, and a thorough benchmark suite using b.Loop().
- Package-level regexes are compiled once with regexp.MustCompile and the init() sort runs before any concurrent use, so the parser is effectively immutable and goroutine-safe at runtime.

**Issues:**

| Sev | Category | Issue | Location |
|---|---|---|---|
| high |  | Production code hardcoded to satisfy a specific unit test | `pkg/useragent/useragent.go:216-220` |
| medium |  | Version truncation can emit a garbled/misleading version number | `pkg/useragent/useragent.go:144-160` |
| medium |  | Tests violate repo conventions: assert instead of require, and several assert only NoError/NotNil | `pkg/useragent/useragent_test.go:181,407-417,444-454` |
| medium |  | Over-broad 'qq'+'browser' and 'mi ' detection patterns cause false positives | `pkg/useragent/browser.go:88-93, pkg/useragent/device.go:40` |
| low |  | extractBotName operates on the original mixed-case UA while the rest of Parse works on lowercased input | `pkg/useragent/useragent.go:83-112,183` |
| low |  | cases.Title transformer allocated per call in the bot-name regex fallback | `pkg/useragent/useragent.go:102,106` |
| low |  | GetShortIdentifier comma-vs-space format is arbitrary and only special-cased for two platform combos | `pkg/useragent/useragent.go:212-225` |
| low |  | Unused exported error sentinels suggest dead API surface | `pkg/useragent/errors.go:8-11` |

_Adversarially verified: 1/1 high/critical findings confirmed real._

**Tests:** Reasonable breadth: device/OS/browser parsing, bot detection, GetShortIdentifier formatting, and a solid benchmark suite (b.Loop, ReportAllocs). t.Parallel() is used consistently at both function and subtest level. However the suite leans heavily on assert (56) over require (8), violating CLAUDE.md, and several cases assert weak conditions: assert.NotNil on a value-type UserAgent is a tautology, and the long-UA and partial-info subtests assert behavior only conditionally (if err != nil). Most positive cases do assert concrete content (exact DeviceType/OS/Browser/version and identifier strings), which is good. Notable gap: tests pin GetShortIdentifier's output to the very format that the production code special-cases for them (the Firefox 100.0.1234 override), so the tests entrench a design defect rather than catch it. No negative tests for the qq/mi false positives. Edge formatting and version truncation are covered but the garbled-version path (123456789.0 -> 1234567891) is not asserted.

**Design-rule compliance:** Compliant with the core design rules: no reflection, no service containers, no magic; values are passed as parameters (Parse/New take strings, nothing read from context); no ID generation so the pkg/id rule is moot. Public methods return only exported/concrete types (Browser is exported), so the no-unexported-return rule holds. No redundant accessors beyond the legitimate getters over unexported fields, which is acceptable since the struct fields are unexported. No sync.Once is needed (no lazy write-once fields; init() sorts a package var once at load). The one clear convention violation is in tests (assert over require, and NoError/NotNil-only assertions), not in the production API. Separately, the test-coupled formatting override in useragent.go is not a stated design rule but is a code-health violation worth fixing.

---

### `validator` — 6/10 · 🔴 Has bugs

A broad, well-structured, composable validation toolkit with good translation support, but DecimalPrecision is genuinely broken for common financial values and test conventions (t.Parallel, require) are inconsistently followed.

**Strengths:**
- Clean, composable core: Rule{Check, Error} + Apply(...) is simple, allocation-light, and easy to extend; programmatic API uses no reflection (only ValidateStruct does, which is intrinsic to tag-based validation).
- Strong i18n story: every built-in rule carries a TranslationKey + TranslationValues, and ValidationErrors.Translate(fn) mutates messages in place; doc.go correctly shows it lines up with i18n Translator.TranslateMessage. Translation data is well tested (string_rules_test.go:228-250, translate_test.go).
- Type-safe generics for numeric/collection/choice rules (Numeric constraint, RequiredComparable, InList[T], MinLenSlice[T]) avoid reflection and interface{} on the hot path.
- Thread-safe custom-validator registry (registryMu RWMutex) and clean exported surface: all public methods return exported types (Rule, error, ValidationErrors) - no unexported return-type leaks, no redundant accessors.
- Sensible fast-paths and edge handling: ValidUUID pre-checks length/hyphen positions before uuid.Parse; ValidEmail rejects consecutive/edge dots; race detector passes (go test -race clean).
- Tests broadly assert real behavior (message text, TranslationKey, TranslationValues, multi-error collection) rather than only require.NoError - e.g. integration_test.go and string_rules_test.go.

**Issues:**

| Sev | Category | Issue | Location |
|---|---|---|---|
| high |  | DecimalPrecision wrongly rejects common 2-decimal money values (float bug) | `pkg/validator/financial_rules.go:76-92` |
| medium |  | tags_test.go violates the mandatory t.Parallel() convention (0 calls in 10 test funcs) | `pkg/validator/tags_test.go (whole file)` |
| medium |  | min/max/len on strings count bytes but messages and docs say "characters" | `pkg/validator/string_rules.go:27,44,61; pkg/validator/core.go:257-258,319-320,385-386` |
| low |  | Regexes compiled on every call inside several validators | `pkg/validator/financial_rules.go:221,271,296; pkg/validator/identifier_rules.go:347,377` |
| low |  | validateField holds registry RLock while executing validator Check() closures | `pkg/validator/core.go:184-219` |
| low |  | Several validators silently no-op (return always-true Rule) on type/param mismatch | `pkg/validator/core.go:251-253,307-310,418-420,480-482,1019-1021` |

_Adversarially verified: 1/1 high/critical findings confirmed real._

**Tests:** Coverage is 72% of statements; all tests pass with -race. Tests are generally good about asserting real behavior - they check Message text, TranslationKey, TranslationValues, and multi-error collection (core_test.go, string_rules_test.go, integration_test.go, translate_test.go), not just require.NoError. Two real gaps: (1) the DecimalPrecision test cherry-picks float-representable values and therefore masks a genuine bug - it should include 19.99/0.07-style values; (2) t.Parallel() is used inconsistently - tags_test.go has none at all, and most subtests across files omit subtest-level t.Parallel(), violating the stated convention. The suite also leans on testify assert rather than require for many checks that are effectively critical (e.g. assert.True(validationErr.Has(...)) after an unchecked nil), though require is used in the most important spots.

**Design-rule compliance:** Mostly compliant. No service containers, no magic, and the programmatic API uses no reflection; ValidateStruct's reflection is intrinsic to struct-tag validation and is the documented opt-in path. Values are passed as parameters, not pulled from context. Public methods return only exported types (Rule, ValidationErrors, error) - no unexported-return leaks - and there are no redundant accessors. Tag syntax follows the ';' separator / ':' param convention. ID-generation rule is N/A (package generates no IDs; uuid import is for validation only). The main deviations are testing-convention violations (t.Parallel at function+subtest level, prefer require over assert), not core design-rule breaches. sync.Once is not applicable here (the registry is a mutable map guarded by RWMutex, not a write-once field). No README.md exists, but doc.go is thorough and accurate to signatures except the byte-vs-\"characters\" wording noted above.

---

### `id` — 5/10 · 🔴 Has bugs

Clean, zero-dependency ULID/ShortID generator with a correct ULID path, but ShortID has two real defects: a 30-bit timestamp mask that wraps every ~12.43 days (breaking the sort-order guarantee) and a guaranteed panic in its rand-failure fallback.

**Strengths:**
- Zero external dependencies; uses crypto/rand and stdlib only. Stateless functions are inherently goroutine-safe (verified by concurrent tests), and crypto/rand.Read is safe for concurrent use.
- ULID implementation is correct: 48-bit timestamp used directly (no masking) covers ~8900 years, and the 80-bit/16-char Crockford Base32 packing is sound.
- Allocation-light: fixed-size [16]byte / [26]byte arrays on the stack, single string conversion. Benchmarks (serial + parallel) are present.
- Complies with framework design rules: no reflection, no magic, exported funcs return string (no unexported return types), no redundant accessors. Internal consumers (session, audit, requestid) correctly use NewULID per the 'all IDs via pkg/id' rule.
- Good happy-path test breadth: length, alphabet (regex), uniqueness, concurrency, timestamp progression, random-portion variance, and cross-type collision checks; doc.go clearly explains the two formats and their intended use.

**Issues:**

| Sev | Category | Issue | Location |
|---|---|---|---|
| high | bug | ShortID timestamp masks low 30 bits, wrapping every ~12.43 days and breaking lexicographic sort order | `pkg/id/shortid.go:28` |
| high | error-handling | ShortID rand-failure fallback panics with index-out-of-range instead of degrading | `pkg/id/shortid.go:20` |
| medium | docs | doc.go and shortid.go claim a '~34 years' ShortID range that the implementation does not provide | `pkg/id/doc.go:31` |
| medium | testing | Tests use assert for critical assertions and cannot detect the wraparound bug | `pkg/id/shortid_test.go:64` |
| low | docs | doc.go example ID strings contain characters excluded from Crockford Base32 (I, L, O) | `pkg/id/doc.go:23` |

_Adversarially verified: 2/2 high/critical findings confirmed real._

**Tests:** Coverage is 96.4% of statements and the happy paths are tested with real behavioral assertions (exact length, Crockford-alphabet regex, uniqueness over 1000 iterations, 5000-ID concurrent uniqueness, timestamp progression, random-portion variance, cross-type collision) rather than bare require.NoError — that part is good. However the suite uses assert instead of require for critical invariants (violates CLAUDE.md), and it has two meaningful blind spots that let real bugs survive: the sortability tests span only ~200ms so they cannot catch the ShortID 30-bit timestamp wraparound (~12.43 days), and the crypto/rand failure fallback branch is never exercised so the guaranteed panic in shortid.go:20 is untested. The in-test wall-clock 'performance benchmark' subtests duplicate the proper Benchmark functions and are CI-flaky.

**Design-rule compliance:** Compliant with the core framework design rules: no reflection, no service containers, no magic; values passed as parameters (nothing pulled from context); exported NewULID/NewShortID return string (no unexported return types); no redundant accessors; pure utility with no business logic. Internal consumers (middlewares/audit.go, middlewares/requestid.go, internal/session.go) correctly route all ID generation through this package per the 'all IDs via pkg/id' rule. The only design-relevant violation is the testing convention (assert instead of require for critical checks), plus doc.go inaccuracies that breach the 'docs must match real behavior' requirement.

---

### `logger` — 5/10 · 🔴 Needs work

A small, mostly-correct slog decorator + Sentry wrapper, but it ships with zero tests, has doc drift in the public Sentry example, and the MinLevel config knob is largely inert.

**Strengths:**
- Clean decorator design: NewLogHandlerDecorator filters nil extractors (decorator.go:21-26) to avoid panics, and Handle short-circuits when no extractors are configured (decorator.go:36-38) so the zero-extractor path adds no overhead.
- Correctly implements the full slog.Handler contract (Enabled/Handle/WithAttrs/WithGroup) and returns the interface type slog.Handler from the constructor, so it composes with any handler and satisfies the design rule against returning unexported types.
- Graceful degradation is genuinely handled: empty DSN and sentry.Init failure both fall back to stdout-only logging (sentry.go:46-60), and the init-failure path logs the error rather than swallowing it.
- Follows the framework design rules: no reflection, no service container, extractors are passed as parameters (not pulled from context by the package itself), and no ID generation is involved.
- parseMinLevel is defensive: trims/lowercases input, accepts both 'warn' and 'warning', and defaults sanely (sentry.go:23-36).

**Issues:**

| Sev | Category | Issue | Location |
|---|---|---|---|
| high |  | Package has zero tests | `pkg/logger/ (no _test.go files)` |
| medium |  | MinLevel does not control the stdout log level and only partially controls Sentry | `pkg/logger/sentry.go:42-69, pkg/logger/factory.go:11` |
| medium |  | doc.go SentryConfig example uses a field type that does not compile | `pkg/logger/doc.go:40-44` |
| medium |  | Context-extracted attributes get nested inside groups under WithGroup | `pkg/logger/decorator.go:35-45,57-62` |
| low |  | doc.go references a non-existent internal multiHandler | `pkg/logger/doc.go:86-87` |
| low |  | sentry.Init mutates global SDK state with no flush/close handle | `pkg/logger/sentry.go:52-56` |
| low |  | EnableLogs is hardcoded true regardless of intent | `pkg/logger/sentry.go:52-55` |

_Adversarially verified: 1/1 high/critical findings confirmed real._

**Tests:** No tests exist in the package at all. This is the single biggest gap. The behavior that most needs coverage is observable and easy to assert against (JSON output content): extractor injection, skip-on-false, nil-extractor filtering, parseMinLevel mapping (including the 'warning' alias and the default branch), the empty-DSN fallback path still applying extractors, and the WithGroup attr-placement semantics. Because nothing is tested, none of the medium/low issues above (inert MinLevel, group nesting) would be caught by regression. Tests should assert emitted content/structure, not require.NoError, and use t.Parallel() per CLAUDE.md.

**Design-rule compliance:** Compliant with the core design rules: no reflection, no service container, no magic; extractors are received as parameters rather than the package reaching into context itself (middleware/caller supplies them); constructors return *slog.Logger or the exported slog.Handler interface, not unexported types; no ID generation so the pkg/id rule is not implicated; no redundant accessors. The violations are not design-rule violations but documentation-accuracy ones: doc.go:43 shows a SentryConfig.MinLevel value (slog.LevelWarn) inconsistent with the actual string field, and doc.go:86 references a non-existent internal multiHandler — both breach the CLAUDE.md requirement that doc examples match real signatures. sync.Once is not applicable here since there are no lazy write-once fields, though the global sentry.Init side effect (sentry.go:52) is not guarded against repeated initialization.

---

### `dnsverify` — 4/10 · 🔴 Has bugs

A tiny, readable DNS TXT verification helper that compiles cleanly but has a whitespace-input verification-bypass bug, overly loose substring matching, validation-before-normalization ordering, and zero tests.

**Strengths:**
- Small, focused, single-purpose API surface (one exported function plus four well-named sentinel errors) that reads clearly.
- Uses sentinel errors wrapped with %w for ErrDNSLookupFailed so callers can errors.Is on the failure class; error taxonomy (invalid input / not found / lookup failed / not verified) is sensible.
- Correctly relies on net.Resolver.LookupTXT, which concatenates multi-string TXT records, so chunked-token splitting is not a concern.
- doc.go is accurate: the documented signature, error list, and verification steps match the real implementation in verify.go.
- Uses errors.AsType[*net.DNSError] (Go 1.26) consistently with the rest of the repo, and correctly checks DNSError.IsNotFound.
- No reflection, no service container, no IDs generated — fully compliant with the framework design rules.

**Issues:**

| Sev | Category | Issue | Location |
|---|---|---|---|
| high |  | Whitespace-only projectID causes false-positive verification (auth bypass) | `pkg/dnsverify/verify.go:21,27,41` |
| high |  | strings.Contains substring match is too loose for ownership verification | `pkg/dnsverify/verify.go:41` |
| high |  | Package has no tests at all | `pkg/dnsverify/` |
| medium |  | ErrDNSLookupFailed loses the underlying error chain (%v instead of %w) | `pkg/dnsverify/verify.go:37` |
| low |  | ErrTXTRecordNotFound conflates NXDOMAIN with missing TXT record | `pkg/dnsverify/verify.go:33-34` |
| low |  | projectID is trimmed but never lowercased while domain is | `pkg/dnsverify/verify.go:26-27` |

_Adversarially verified: 3/3 high/critical findings confirmed real._

**Tests:** No tests exist (no _test.go files). This is a real and significant gap: the package has multiple branches (empty/whitespace input, DNSError IsNotFound classification, generic lookup error wrapping, substring match success, and not-verified failure) and touches the network. The two correctness/security bugs (whitespace projectID false-positive, loose substring match) would almost certainly have been caught by basic table-driven tests. The package is not trivial enough to excuse the absence of tests. Testing currently requires a refactor because net.Resolver is hardcoded at verify.go:29 with no injection seam; a resolver interface parameter would make the logic fully unit-testable without Docker or real DNS.

**Design-rule compliance:** Compliant with the core design rules: no reflection, no service container, no magic; no ID generation (so the pkg/id rule does not apply); values are passed as parameters, not pulled from context; the only exported function returns a plain error (no unexported return types); no redundant field accessors. doc.go matches the real signature and error set. The notable rule violation is against the project's Testing conventions in CLAUDE.md (t.Parallel(), require, behavior-asserting tests): the package ships with zero tests. Also worth flagging against the framework philosophy 'business logic belongs in consumer repos' — the hardcoded substring-based matching policy is a business decision baked into a utility; exposing an injectable resolver and/or an exact-match contract would keep this a clean utility.

---

_Generated from a multi-agent review workflow (31 package reviewers + adversarial verification + synthesis)._
