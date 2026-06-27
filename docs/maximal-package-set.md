# Forge — Maximal Package Set

> A research-backed proposal for the maximal set of packages forge could ship to cover **any** app, while staying true to its "no-magic" utility-framework DNA. **167 proposed packages** across **12 layers** (29 core · 86 recommended · 52 optional), on top of the 7 packages shipped today.

_Produced by a 19-agent research workflow (rubric extraction → 12 parallel domain researchers → synthesis → 4 adversarial completeness critics → reconciliation). This is a catalog/roadmap, not a commitment — tiers and APIs are starting points._

## Design DNA every package follows

- **No magic:** no reflection (one sanctioned helper, `structfields`), no service containers; values via params, not context (context only for request-scoped reads). Public methods never return unexported types.
- **One of three idioms:** stateless **free-funcs** (`render`, `htmx`) · `New(...Option)` with an env-loadable `Config` + `DefaultConfig` + `Validate` (`logger`, `httpserver`) · a `supervisor.Service`.
- **Anatomy:** `doc.go` (runnable example) · `config.go` · `options.go` (`type Option func(*config)`, **never** builders) · `errors.go` (`errors.Is`-matchable single-line sentinels) · impl.
- **Minimal dependencies, not zero** (see next section): buy the wire, build the ergonomics; isolate every real dep behind a subpackage.
- **Composition seams:** `http.Handler`, `supervisor.Service`, `middleware.Middleware`, `ctxkey.Key[T]`, `logger.ContextExtractor`, and pluggable `Store`/`Broker`/`Sender` interfaces.
- Flat, single-responsibility packages (~250–850 LOC); black-box tests only.

## Dependency philosophy — minimal, not zero

forge optimizes for a **small, auditable dependency surface — not stdlib purism.** Rebuilding stdlib-adjacent utilities is worth it; rebuilding a hardened protocol client or a crypto primitive is not. The decision rule:

- **Depend** on what speaks to the outside world or is dangerous to hand-roll — a wire protocol / hardened client (**`pgx`** for Postgres, the **S3 SDK**, a **Mongo** driver) or a security primitive (**`x/crypto`** for argon2/bcrypt). Reimplementing these is huge, risky, and buys no differentiation.
- **Don't wrap** large, opinionated, or fast-moving frameworks whose model would leak through forge's API (**`watermill`**, **`stripe-go`**). Expose a small interface; let the consumer take the dep in *their* repo.
- **Build — or copy a small local package** — for anything that is small, that *shapes forge's own API*, or where a third party's opinions would infect the core: `id`, `validate`, `request`, `slugx`, `money`, the SKIP LOCKED job engine.
- **Always isolate** a real dependency behind a forge interface in a subpackage so it stays a swappable leaf, never woven through core: `logger/sentry`, `jobqueue/sqlbroker`, `objectstore`'s S3 adapter.

**Buy the wire, build the ergonomics.** First-party runtime deps today: `sentry` (isolated in `logger/sentry`) and — newly sanctioned — **`pgx`** for the Postgres-committed data + messaging layer. Everything outside `data/*` and the messaging engine stays stdlib.

## Shipped today (the seed · 7)

`supervisor` (lifecycle) · `httpserver` · `hostrouter` · `render` · `htmx` · `logger` (+`logger/sentry`) · `request`. The layers below wrap and build on these — they are not re-proposed.

`request` = stdlib-only, reflection-free typed request reading: generic per-field accessors (`Query[T]`/`Path[T]`/`Header[T]`/`Cookie[T]`/`FormValue[T]` + `Func`/`Slice`/`Split`), body decode (`DecodeJSON`/`RawBody`/`File`/`Files` with size caps + content-type checks), `ClientIP` (`WithTrustedProxies`), `QueryPage`/`QueryCursor`, `BearerToken`, and an `Error`+`StatusCode`→400/413/415 mapping. **No struct-tag binding by design** — per-field accessors are forge's no-reflection answer to "binding".

### Reconciliation — what `request` already covers

This package supersedes or re-scopes several proposed entries below (the per-package text predates it):

- **`bind` (P3, core) — drop.** `request` *is* the binding story; the proposed struct-tag `bind` violated the no-reflection rule. Use `request`'s per-field accessors + `validate`.
- **`realip` (P3) — done.** `request.ClientIP` + `WithTrustedProxies` already covers spoof-resistant client IP. A thin context-middleware wrapper is the only possible delta.
- **`pagination` (P2) — re-scope.** Inbound parse exists (`QueryPage`/`QueryCursor`); keep only the cursor *codec* (encode/decode opaque tokens) + SQL keyset side.
- **`upload` (P3) — re-scope to a validator.** `request.File/Files` parses; only MIME-allowlist + `filetype` magic-byte sniff remains.
- **`negotiate` (P3) / `authmw` (P5)** — slivers exist (`request.IsContentType`, `request.BearerToken`); these build on top.

## Tier legend

**core** = nearly every app needs it · recommended = most apps · optional = some apps. The maximal set is all 167; the sane default is the 29 core packages (see _Cut-lines_).

---

## Catalog

### P0 · Core primitives (`pkg/*`) · 24 packages

_pkg/\* foundation: tiny, stdlib-only, mostly free-func or generic value-type utilities with zero forge dependencies. Everything above builds on these. Includes the framework-mandated id/clock/randx seams, the single sanctioned reflection helper (structfields), the typed-context-key seam (ctxkey), and scalar coercion (typeconv) that make the rest testable, reflection-honest, and ID-consistent._

<sub>8 core · 12 recommended · 4 optional</sub>

- **`clock`** — **core**  
  Testable time: Clock interface with a real impl and a controllable Fake, passed as a constructor param (never via context). Makes every TTL/retry/ratelimit/token package deterministic in tests.  
  `Clock interface{ Now() time.Time; Since(time.Time) time.Duration; NewTimer(d) *time.Timer; After(d) <-chan time.Time; Sleep(d) }; System() Clock; NewFake(t) *Fake with Advance/Set. Goroutine-safe Fake.`  
  <sub>deps: stdlib-only: time, sync. ~150 LOC; avoids benbjohnson/clock.</sub>

- **`randx`** — **core**  
  Cryptographically secure randomness: bytes, bias-free ints, hex/base62/url-safe strings, human OTP digit codes. The entropy source under tokens/keys/api-keys.  
  `free-funcs: Bytes(n)([]byte,error); Int(maxExclusive int64)(int64,error); Hex(n)(string,error); Token(n)(string,error); String(n,alphabet)(string,error); DigitCode(n)(string,error); MustBytes(n). Sentinel ErrInvalidLength.`  
  <sub>deps: stdlib-only: crypto/rand, encoding/hex, encoding/base64, math/big. ~120 LOC; never math/rand.</sub>

- **`id`** — **core**  
  Framework-mandated sortable, optionally type-prefixed unique IDs (UUIDv7 + Crockford-base32 ULID). The single ID source for the whole framework.  
  `free-funcs + exported ID value type: New() string; NewULID() string; NewPrefixed(prefix string) string; Parse(s string) (ID, error); (ID).Time() time.Time; (ID).String() string. Optional WithClock seam for tests; New() stays zero-arg.`  
  <sub>deps: stdlib-only: crypto/rand, encoding/binary, time, encoding/hex. UUIDv7+ULID ~150 LOC each; no google/uuid or oklog/ulid. · depends on: `clock`</sub>

- **`ctxkey`** — **core**  
  Type-safe, collision-free context key/value primitive: declare a typed Key[T] once, get From/With accessors that never alias across packages and never return any. The single shared seam for all request-scoped context plumbing.  
  `generic: New[T any](name string) Key[T] (each backed by a distinct unexported sentinel pointer); (Key[T]).From(ctx)(T,bool); (Key[T]).MustFrom(ctx) T; (Key[T]).With(ctx, v) context.Context. Pairs with logger.ContextExtractor (extractor = key.From adapted to slog.Attr).`  
  <sub>deps: stdlib-only: context, generics. ~40 LOC. No global registry, no reflection.</sub>

- **`typeconv`** — **core**  
  Parse/coerce strings to Go scalars and back (bool, int/uint widths, float, time.Duration, time.Time) via a generic Parse[T] and lossless Format. The scalar substrate envconfig/bind/form/featureflag build field decoders on; distinct from encoding (byte codecs).  
  `free-funcs: ParseBool(s)(bool,error); ParseInt[T constraints.Signed](s)(T,error); ParseFloat; ParseDuration; Format(v any) string; generic Parse[T any](s)(T,error) dispatching on T. Sentinels ErrUnsupportedType/ErrSyntax (wraps strconv).`  
  <sub>deps: stdlib-only: strconv, time. Scalar-at-a-time; no struct reflection.</sub>

- **`slicex`** — **core**  
  Generic slice helpers stdlib `slices` lacks: Map, Filter, Reduce, GroupBy, KeyBy, Chunk, Unique, Flatten. Fills gaps only; avoids samber/lo.  
  `generic free-funcs: Map[T,U](s,fn); Filter[T](s,pred); Reduce; GroupBy[T,K](s,keyfn) map[K][]T; KeyBy; Chunk[T](s,n); Unique[T comparable]; Flatten.`  
  <sub>deps: stdlib-only: slices, iter. ~200 LOC. Does NOT re-implement Sort/Contains/Index/Equal.</sub>

- **`ptr`** — **core**  
  Generic pointer helpers (ptr.To literal, deref-with-default) for optional struct fields, JSON omitempty, and SQL nullables. Absorbs the optional/result micro-types as one tiny package to avoid sprawl.  
  `generic free-funcs: To[T](v) *T; From[T](p) T; FromOr[T](p,def) T; Equal[T comparable](a,b). Plus Optional[T]{present flag} with json.Unmarshaler distinguishing absent-vs-null for PATCH semantics.`  
  <sub>deps: stdlib-only; pure generics. ~120 LOC.</sub>

- **`validate`** — **core**  
  Reflection-free, composable value/field validation returning structured i18n-ready errors. The deliberate no-magic alternative to go-playground/validator (no struct-tag DSL).  
  `free-funcs + generics: rules Required/MinLen/MaxLen/Email/URL/OneOf/Match/Range; runner v:=validate.New(); v.Field("email", validate.Email(x)); v.Err(). Errors type (map[string][]string) implements error + JSON/HTML friendly.`  
  <sub>deps: stdlib-only: regexp, net/mail, net/url, strings, unicode. ~400 LOC. Explicitly avoids tag-based reflection.</sub>

- **`encoding`** — recommended  
  Base62 and Crockford base32 encode/decode for compact, URL-safe, human-typable IDs and short codes (the ULID alphabet, no ambiguous chars). Byte/integer codecs only.  
  `free-funcs: EncodeInt(uint64) string; DecodeInt(string)(uint64,error); Encode([]byte) string; Decode(string)([]byte,error); Crockford Encode32/Decode32 excluding I,L,O,U.`  
  <sub>deps: stdlib-only: math/big, strings. ~120 LOC; encoding/base64 URL variant already covers that case so not re-wrapped.</sub>

- **`structfields`** — recommended  
  The one sanctioned reflection helper: walk an exported struct's fields once yielding name/value/parsed-tag/Set, so bind/form/envconfig/scan confine all reflect usage to a single audited primitive and stay reflection-free themselves.  
  `free-funcs + exported Field: Walk(v any, tagKey string, fn func(Field) error) error; Field{ Name string; Tag StructTag; Value reflect.Value; Set func(any) error }. StructTag parses name,opt1,opt2. Sentinel ErrNotStruct.`  
  <sub>deps: stdlib-only: reflect, strings. reflect.Value is stdlib so allowed as a public type. Pairs with typeconv for the scalar parse.</sub>

- **`iox`** — recommended  
  Small io.Reader/Writer/Closer shims every streaming package reuses: an over-limit-ERRORING LimitReader (io.LimitReader silently truncates, wrong for 413 semantics), guaranteed body Drain+Close for keep-alive reuse, MultiCloser, CountingWriter, NopWriteCloser.  
  `free-funcs + tiny types: LimitReader(r, n) io.Reader (errors past limit); DrainClose(rc io.ReadCloser) error; MultiCloser(...io.Closer) io.Closer; CountingWriter{} with N(); NopWriteCloser(w). Sentinel ErrLimitExceeded.`  
  <sub>deps: stdlib-only: io. <150 LOC.</sub>

- **`bufpool`** — recommended  
  Shared sync.Pool of *bytes.Buffer with size-capped recycling, so transactional renderers/encoders stop each defining a private pool (render's getBuf/putBuf, emailtemplate, sse, sign/token build).  
  `free-funcs: Get() *bytes.Buffer; Put(b *bytes.Buffer) (drops oversized); Do(fn func(*bytes.Buffer) error) error to borrow-and-return. No config, no state beyond the package-level pool.`  
  <sub>deps: stdlib-only: bytes, sync. Caps recycled buffer cap (~64KiB) to avoid pinning large buffers.</sub>

- **`mapx`** — recommended  
  Generic map helpers complementing stdlib `maps`: Merge, MapValues, Invert, FilterMap, Entries/FromEntries, plus an insertion-ordered map for stable JSON.  
  `generic free-funcs: Merge[K,V](maps...); MapValues; Invert[K,V comparable]; FilterMap; Entries/FromEntries. Plus Ordered[K,V] type with order-preserving MarshalJSON and All() iter.Seq2.`  
  <sub>deps: stdlib-only: maps, iter, encoding/json. ~250 LOC. Does NOT duplicate maps.Clone/Keys/Values.</sub>

- **`set`** — recommended  
  Generic Set[T comparable] with union/intersection/difference and deterministic iteration. No stdlib equivalent; needed for permissions/tags/dedup.  
  `Set[T comparable] over map[T]struct{}: New(items...), Add/Remove/Contains/Len, Union/Intersect/Diff, Slice()/Sorted(less), All() iter.Seq[T].`  
  <sub>deps: stdlib-only: maps, slices, iter. ~150 LOC.</sub>

- **`nullx`** — recommended  
  Generic Null[T] that round-trips cleanly through database/sql (Scanner/Valuer) AND encoding/json (marshals as null, not {V,Valid}). Single type instead of the sql.NullString family.  
  `Null[T any]{V T; Valid bool} implementing sql.Scanner, driver.Valuer, json.Marshaler/Unmarshaler; Of[T](v)/Empty[T](); conversions to/from *T (composes with ptr).`  
  <sub>deps: stdlib-only: database/sql, database/sql/driver, encoding/json, time. ~200 LOC; driver-agnostic. · depends on: `ptr`</sub>

- **`sanitize`** — recommended  
  Plain-text input normalization and escaping at trust boundaries: trim/collapse/strip-control, email/username canonicalization, filename + header-value (CRLF) sanitization, HTML escape. NOT a rich-HTML sanitizer.  
  `free-funcs: Trim/Collapse/StripControl(s); Email(s); Username(s); SingleLine(s); Truncate(s,n); HTML(s); Filename(s); HeaderValue(s).`  
  <sub>deps: stdlib-only: strings, unicode, net/mail, html, path/filepath. ~200 LOC. No bluemonday.</sub>

- **`slugx`** — recommended  
  URL-safe slug generation from arbitrary text with ASCII folding and uniqueness suffixing (-2,-3...). Storage-agnostic via an exists predicate.  
  `free-funcs: Make(s) string; MakeLang(s,lang) string; Unique(s, exists func(string) bool) string.`  
  <sub>deps: stdlib-first: unicode, unicode/utf8, strings; golang.org/x/text/unicode/norm (Go-team quasi-stdlib) for NFKD, or local ASCII table. ~200 LOC. · depends on: `sanitize`</sub>

- **`bytesize`** — recommended  
  Parse/format human byte sizes ('10MB','1.5GiB') and a ByteSize type implementing TextUnmarshaler so it drops into env-tagged Config and JSON. Powers upload/body-limit config.  
  `Parse(s)(int64,error); Format(n)/FormatIEC(n) string; ByteSize int64 type with String() + encoding.TextUnmarshaler; consts KB/MB/GB/KiB/MiB.`  
  <sub>deps: stdlib-only: strconv, strings. ~150 LOC.</sub>

- **`money`** — recommended  
  Money value with currency and exact arithmetic over a decimal core, penny-perfect Allocate split, and formatting. No binary floats. Currency conversion/FX is out of scope.  
  `Money{Amount decimal.Decimal; Currency Currency}; New(amount,cur); Add/Sub (ErrCurrencyMismatch); Mul(factor); Allocate(ratios...) []Money; Parse/Format. Currency is exported with minor-unit metadata.`  
  <sub>deps: stdlib-only: math, strconv, fmt, errors. ISO-4217 subset as local data. Uses decimal for exact tax/percentage math; avoids shopspring/decimal and go-money as public deps. · depends on: `decimal`</sub>

- **`filetype`** — recommended  
  Detect a file's real MIME type from magic-byte signatures (not the client-supplied extension/Content-Type), closing the renamed-.exe-as-.png upload hole. Curated signature table (images/pdf/zip-office/audio/video); finer than net/http.DetectContentType.  
  `free-funcs: Detect(head []byte)(Type,bool); DetectReader(r io.Reader)(Type, io.Reader, error) (returns re-readable wrapper); Is(head []byte, mime string) bool; Type{MIME,Ext string}.`  
  <sub>deps: stdlib-only: bytes, io, net/http (DetectContentType fallback). Pure data table, no I/O. Avoids h2non/filetype. · depends on: `iox`</sub>

- **`enum`** — optional  
  Generic string-backed closed-value-set primitive: declare a fixed set of string values once, get Parse/Valid/Values plus marshal/scan-ready conversion without per-enum boilerplate. Distinct from set (mutable runtime collection); enum is a fixed declared value-domain.  
  `generic value type: New[T ~string](vals ...T) Set[T]; (Set[T]).Parse(s)(T,error); (Set[T]).Valid(T) bool; (Set[T]).Values() []T. Caller defines `type Status string`+`var Statuses = enum.New(...)`. Sentinel ErrInvalidValue.`  
  <sub>deps: stdlib-only. Data-only, no I/O. ~60 LOC.</sub>

- **`decimal`** — optional  
  Fixed-point base-10 decimal for exact monetary/percentage/proration math without binary-float drift. The numeric substrate money and aiusage need (tax/percentage/per-unit pricing); int-cents alone silently mishandles 8.25% tax.  
  `value type Decimal: Parse(s)(Decimal,error); Add/Sub/Mul/Div(scale,rounding)/Cmp; Round(places,mode); String; implements TextMarshaler. Sentinels ErrOverflow/ErrDivByZero. Proposed as money's substrate, not a competing money type.`  
  <sub>deps: stdlib-first: int64 coefficient+scale on the common path, math/big only on the overflow path (stdlib, no external dep — exact decimal is impossible with float64).</sub>

- **`errorsx`** — optional  
  Lightweight error helpers complementing stdlib errors: code+message app-error wrapper and retryable/permanent tagging, for mapping internal errors to HTTP/API codes. No stack capture (honors single-line-error rule).  
  `free-funcs+types: WithCode(err,code) error; Code(err)(string,bool); MarkPermanent(err)/IsRetryable(err). Composes with render/problem to pick status.`  
  <sub>deps: stdlib-only: errors, fmt. ~150 LOC. Does NOT reimplement Is/As/Join.</sub>

- **`stringsx`** — optional  
  String formatting helpers stdlib lacks: ToSnake/ToCamel/ToKebab case conversion, Truncate/Ellipsis, Mask (PII for logs), basic Pluralize. For trusted strings (sanitize handles untrusted input).  
  `free-funcs: ToSnake/ToCamel/ToKebab(s); Truncate(s,n); TruncateWords(s,n); Mask(s,keep); Pluralize(word,n) (basic s/es).`  
  <sub>deps: stdlib-only: strings, unicode. ~200 LOC; avoids iancoleman/strcase.</sub>

### P1 · Crypto primitives · 9 packages

_stdlib-first cryptographic building blocks: constant-time compare, AEAD, HMAC signing, KDF, password hashing, opaque tokens, key rotation, redaction. The only justified external dep is golang.org/x/crypto (argon2/bcrypt/chacha), isolated to the packages that need it._

<sub>0 core · 6 recommended · 3 optional</sub>

- **`subtlex`** — recommended  
  Constant-time comparison helpers (string/bytes) that handle unequal length safely, removing the #1 timing-attack footgun. Internal dep of most crypto/auth packages.  
  `free-funcs: BytesEqual(a,b []byte) bool; StringEqual(a,b string) bool; length-safe via HMAC-of-both-sides for secret compare.`  
  <sub>deps: stdlib-only: crypto/subtle, crypto/hmac, crypto/sha256. ~60 LOC.</sub>

- **`sign`** — recommended  
  HMAC sign/verify opaque values with constant-time verification, for unsubscribe links, signed download URLs, integrity checks. Lower-level than token.  
  `New(key []byte, opts...) *Signer with WithHash(crypto.Hash); Sign(msg)([]byte); Verify(msg,mac) bool; SignString/VerifyString. Sentinels ErrBadSignature/ErrInvalidKey.`  
  <sub>deps: stdlib-only: crypto/hmac, crypto/sha256, encoding/base64. ~120 LOC. · depends on: `subtlex`</sub>

- **`secret`** — recommended  
  Authenticated symmetric encryption (AEAD secret-box) of bytes/strings with versioned ciphertext for rotation. Safe two-call API over cipher.AEAD; underpins cookie/session/fieldcrypt.  
  `New(key []byte, opts...)(*Box,error) WithAAD/WithKeyVersion; Encrypt/Decrypt([]byte); EncryptString/DecryptString. Sentinels ErrInvalidKeySize/ErrDecryptFailed.`  
  <sub>deps: stdlib-first: crypto/aes + crypto/cipher (AES-256-GCM default). Strong-reason golang.org/x/crypto/chacha20poly1305 (XChaCha 24-byte nonce) behind an option only. · depends on: `keyset`</sub>

- **`password`** — recommended  
  Password hashing/verification (Argon2id default, bcrypt fallback) with PHC-string encoding and needsRehash detection for transparent param upgrades.  
  `Hash(password, opts...)(string,error); Verify(password,encoded)(ok bool, needsRehash bool, err error). PHC self-describing. WithArgon2Params/WithBcryptCost/WithAlgorithm. Sentinel ErrMismatch.`  
  <sub>deps: stdlib-first plumbing + strong-reason golang.org/x/crypto/argon2 + bcrypt (no stdlib password hashing). The single approved crypto dep, documented. · depends on: `subtlex`</sub>

- **`token`** — recommended  
  Opaque, signed, optionally-encrypted, expiring tokens carrying a small payload for email-verify/reset/magic-link/invite flows. Deliberately NOT JWT.  
  `New(key []byte, opts...)(*Codec,error) WithTTL/WithEncrypt(*secret.Box)/WithPurpose; Issue(payload []byte, ttl)(string,error); Parse(token)([]byte,error). Generic Signer[T] variant. Sentinels ErrExpired/ErrBadSignature/ErrMalformed.`  
  <sub>deps: stdlib-first: composes sign (+ optional secret), encoding/base64, encoding/json, time. No JWT library. · depends on: `sign`, `secret`, `randx`, `clock`</sub>

- **`redact`** — recommended  
  Secret[T] wrapper that prints/marshals/logs as \*\*\*\* (fmt/json/slog.LogValuer) plus string/map scrubbing helpers. Prevents API-key/token leakage into logs; the seam with logger.  
  `generic Secret[T]: New(v) Secret[T]; Expose() T; implements Stringer/json.Marshaler/slog.LogValuer returning REDACTED. Free-funcs String(s) mask; Map(m, keys...).`  
  <sub>deps: stdlib-only: fmt, encoding/json, log/slog, strings. Does NOT fetch from a vault.</sub>

- **`hashx`** — optional  
  Convenience digest helpers: hex/base64 SHA-256/512, streaming file hashes, HMAC digests with a uniform API. Removes per-call boilerplate for ETags/cache keys/dedup.  
  `free-funcs: SHA256Hex([]byte) string; SHA256([]byte) []byte; SHA512Hex(...); HMACHex(key,msg,h); FileSHA256(path)(string,error).`  
  <sub>deps: stdlib-only: crypto/sha256, crypto/sha512, crypto/hmac, encoding/hex, encoding/base64, io, os. ~120 LOC. Excludes MD5/SHA1.</sub>

- **`keyset`** — optional  
  In-memory versioned key management: primary + retired keys for encryption/signing rotation, loaded from base64 env material, with By-version lookup. Backs secret/sign/token rotation instead of each reinventing a keyring.  
  `New(opts...)(*Keyset,error) WithPrimary(version,key)/WithRetired/WithBase64Keys; Primary()(int,[]byte); ByVersion(v)([]byte,bool); All() iter.Seq2[int,[]byte]. Sentinels ErrNoPrimary/ErrBadKeyMaterial.`  
  <sub>deps: stdlib-only: encoding/base64, crypto/subtle, iter. NOT a cloud KMS client. · depends on: `subtlex`</sub>

- **`kdf`** — optional  
  Key derivation: HKDF (per-purpose keys from a master) and Argon2id/scrypt (passphrase to key). Centralizes safe parameters for secret; distinct from password (verification) and keyset (storage).  
  `free-funcs+Params: DeriveKey(passphrase,salt,Params)([]byte,error); HKDF(secret,salt,info,length)([]byte,error); DefaultParams(); Params.Validate().`  
  <sub>deps: stdlib-first: crypto/hkdf (Go 1.24+ stdlib). Strong-reason golang.org/x/crypto/argon2 + scrypt (no stdlib memory-hard KDF), isolated here.</sub>

### P2 · Data & persistence · 13 packages

_Postgres-native data utilities: pool factory, read/write replica routing, transaction/unit-of-work helpers, reflection-free row scanning, migrations, pagination/cursors, multi-tenancy and concurrency guards, per-request batch loading, a blob object store, and a shared KV seam. No ORM._

> **Driver stance: pgx, not `database/sql`.** forge declares **Postgres as its database** and targets **`pgx/v5` (`pgxpool` + the sqlc `DBTX` interface)** across `data/*` and the messaging engine. Rationale: you always use pgxpool, so `database/sql`'s portability buys nothing while taxing you (manual scan, no `LISTEN/NOTIFY`, no `Batch`/`CopyFrom`); the SKIP LOCKED engine actively needs pgx's `LISTEN/NOTIFY`, `Batch`, `CopyFrom`, and SQLSTATE codes. `pgx` is forge's **one sanctioned DB dependency**, confined to these layers (the rest of the framework stays DB-free). The per-package `deps` lines below that still say "database/sql" predate this — read them as pgx; `sqltx`/`txcontext` carry a `pgx.Tx`, `scan` uses `pgx.CollectRows`, `sqldb` wraps `pgxpool.Pool`. (Anyone wanting the generic path can still drive pgx through `pgx/stdlib`.)

<sub>3 core · 4 recommended · 6 optional</sub>

- **`sqldb`** — **core**  
  Thin *sql.DB pool factory with env-loadable pool config (max conns, lifetimes), secure defaults over database/sql's footgun zero-values, and a Ping helper. Returns stdlib *sql.DB (does not wrap). Driver-agnostic.  
  `New(driver,dsn string, opts...)(*sql.DB,error); Config{MaxOpenConns,MaxIdleConns,ConnMaxLifetime,ConnMaxIdleTime,PingTimeout}+DefaultConfig+Validate; WithMaxOpenConns/...; Ping(ctx,*sql.DB) error.`  
  <sub>deps: stdlib-only: database/sql, context, time. Driver registered by caller's blank import; never imported here.</sub>

- **`sqltx`** — **core**  
  Transaction/unit-of-work helper: run a closure inside a tx with panic-safe auto commit/rollback. Removes the most error-prone DB boilerplate. The raw *sql.Tx is passed to fn (no ORM).  
  `free-funcs: Transact(ctx, db DB, fn func(*sql.Tx) error) error; TransactOpts(ctx, db, *sql.TxOptions, fn) error. Rolls back on error/panic (re-panics), commits on nil.`  
  <sub>deps: stdlib-only: database/sql, context. DB declared as a 1-method structural interface satisfied by *sql.DB.</sub>

- **`migrate`** — **core**  
  Forward/rollback SQL migration runner over embed.FS, tracked in a schema_migrations table with advisory locking, runnable at boot or as a CLI subcommand. Avoids golang-migrate/goose.  
  `New(db *sql.DB, fsys fs.FS, opts...)(*Migrator,error); Up(ctx)/Down(ctx,n)/Version(ctx)/Pending(ctx). WithTableName/WithDialect/WithLockTimeout/WithLogger. Reads NNN_name.up.sql/.down.sql. cli.Command helper.`  
  <sub>deps: stdlib-only: database/sql, io/fs, embed, sort, strings. Caller supplies \*sql.DB; dialect SQL via a small Dialect enum, no driver import. · depends on: `sqldb`</sub>

- **`txcontext`** — recommended  
  Carry an active *sql.Tx on request-scoped context so repository funcs transparently join an outer transaction or fall back to the pool, via an exported sqlc-compatible DBTX interface, plus the force-primary-after-write flag dbreplica reads. The one legitimate tx-on-context use.  
  `WithTx(ctx,*sql.Tx) context.Context; FromContext(ctx)(*sql.Tx,bool); Querier(ctx, db *sql.DB) DBTX where DBTX{ExecContext/QueryContext/QueryRowContext}; MarkWrite(ctx)/WroteRecently(ctx) bool.` 
<sub>deps: stdlib-only: context, database/sql. ~80 LOC. DBTX declared locally; context keys via ctxkey. · depends on:`sqltx`, `ctxkey`</sub>

- **`pagination`** — recommended — **re-scoped:** inbound parse already exists (`request.QueryPage`/`QueryCursor`); this keeps only the cursor codec + SQL keyset side  
  CANONICAL offset + keyset(cursor) pagination: parse/validate page params, encode/decode opaque cursors, build Page metadata. UI link-building lives in i18n-ui/pageview.  
  `ParseOffset(limit,offset)(Offset,error); EncodeCursor(v)/DecodeCursor[T](s)(T,error) (base64+json opaque; optional HMAC via sign); Page[T]{Items,Next,Prev,HasMore}. Feeds render.JSON directly.`  
  <sub>deps: stdlib-only: net/http, encoding/base64, encoding/json, strconv, generics. Emits Offset/Limit + tokens; never SQL or DB imports. · depends on: `encoding`, `sign`</sub>

- **`dataloader`** — recommended  
  Generic per-request batch-and-cache loader that coalesces N+1 association lookups into one batched fetch (the classic templ-list-row-query problem). Per-request scoped (constructed in middleware, read from context), not a global cache.  
  `New[K comparable,V any](batchFn func(ctx, keys []K)([]V,[]error), opts...) *Loader[K,V]; Load(ctx,key)(V,error); LoadMany(ctx,keys)([]V,[]error); WithMaxBatch/WithWait/WithCache. Sentinel ErrNoResult.`  
  <sub>deps: stdlib-only: context, sync, time, generics. Pure generics, no deps. · depends on: `txcontext`, `ctxkey`</sub>

- **`objectstore`** — recommended  
  Blob store behind a Store interface with a built-in local-disk adapter (path-traversal safe); S3 adapter isolated in objectstore/s3 (minimal local SigV4 first). Backs upload/imageproc and render.File.  
  `Store{ Put(ctx,key,r,PutOptions) error; Get(ctx,key)(io.ReadCloser,error); Delete/Stat; URL(ctx,key,ttl)(string,error) }; NewDisk(root,opts...)(*Disk,error). Sentinels ErrNotFound/ErrInvalidKey.`  
  <sub>deps: stdlib-first: os, io, path/filepath, crypto/sha256 for disk. objectstore/s3 isolates SigV4/SDK; core stays stdlib-only. · depends on: `iox`</sub>

- **`dbreplica`** — optional  
  Read/write split router over a primary + replica *sql.DB handles with round-robin reads, replica health checks, and a force-primary-after-write window to avoid read-after-write staleness. Returns stdlib *sql.DB (no wrapping) so it composes with sqltx/txcontext unchanged.  
  `New(primary *sql.DB, replicas []*sql.DB, opts...)(*Router,error); Writer() *sql.DB; Reader() *sql.DB; ReaderFor(ctx) *sql.DB (honors force-primary flag); WithPolicy(RoundRobin|Random). Sentinels ErrNoReplicas/ErrInvalidConfig.`  
  <sub>deps: stdlib-only: database/sql, context, sync. Reads txcontext's after-write signal for ReaderFor. · depends on: `sqldb`, `txcontext`</sub>

- **`scan`** — optional  
  Reflection-free helpers mapping \*sql.Rows into structs/slices via explicit per-row scan closures, removing Next/Scan/Err/Close boilerplate WITHOUT becoming an ORM. Optional (sqlc covers the same ground for generated code).  
  `generic free-funcs: All[T](rows, fn func(RowScanner)(T,error))([]T,error); One[T](rows,fn)(T,error); Each(rows, fn). Closes/err-checks rows.`  
  <sub>deps: stdlib-only: database/sql, errors, generics. RowScanner is a 1-method structural interface; no struct-tag reflection.</sub>

- **`tenant`** — optional  
  Multi-tenancy scoping: carry tenant ID on request-scoped context and hand back an explicit parameterized filter clause + value (never auto-injected). Makes the tenant filter visible at every query to prevent data-leak bugs.  
  `WithTenant(ctx,id) context.Context; FromContext(ctx)(string,bool); Require(ctx)(string,error) ErrNoTenant; ScopeClause(col)(frag string, val string).`  
  <sub>deps: stdlib-only: context, errors. No SQL execution, no reflection. Context keys via ctxkey. · depends on: `txcontext`, `ctxkey`</sub>

- **`audit`** — optional  
  Soft-delete and audit-column helpers: embeddable Timestamps/SoftDelete column groups + tiny SQL-fragment builders and Touch/MarkDeleted. Strictly column groups + fragments, never a model registry or hooks.  
  `Timestamps{CreatedAt,UpdatedAt}; SoftDelete{DeletedAt sql.NullTime}; free-funcs NotDeletedClause(col) string; Touch(*Timestamps,clock); MarkDeleted(*SoftDelete,clock).`  
  <sub>deps: stdlib-only: database/sql, time. ~150 LOC. Distinct from the ops auditlog package (event logging). · depends on: `clock`</sub>

- **`optimistic`** — optional  
  Optimistic concurrency: version-column predicate builder + NextVersion + Check(sql.Result) returning ErrVersionConflict from a zero row-count, for lost-update protection and retry/4xx logic.  
  `VersionClause(col, current int64)(frag string, args []any); NextVersion(current) int64; Check(sql.Result) error (ErrVersionConflict).`  
  <sub>deps: stdlib-only: database/sql, errors. ~60 LOC.</sub>

- **`kv`** — optional  
  Minimal shared key-value Store interface (Get/Set/Delete with TTL) plus a typed JSON helper and in-memory default, giving stateful packages (lock/idempotency/otp/sessionstore/ratelimit) ONE backend seam so a single Redis/SQL adapter backs all of them instead of N adapters.  
  `Store{ Get(ctx,key)([]byte,bool,error); Set(ctx,key, val []byte, ttl time.Duration) error; Delete(ctx,key) error }; typed New[T any](Store, codec). In-memory impl in core. ErrNotFound optional (bool-ok preferred).`  
  <sub>deps: stdlib-only core + pluggable driver interface (in-memory default over ttlcache). Redis/SQL adapters in kv/<driver> subpackages. · depends on: `ttlcache`, `clock`</sub>

### P2–P4 · Async, jobs & resilience · 17 packages

_Concurrency, resilience, and background-execution primitives plus supervised long-running services (queues, scheduler, event bus, command bus, distributed lock + leader election, backpressure). The resilience primitives are stdlib; the messaging layer is one durable engine (Postgres SKIP LOCKED via pgx) with typed facades._

> **Messaging model (revised — supersedes the `eventbus`/`jobqueue`/`scheduler`/`pubsub`/`outbox` entries below).** The four app-level needs — periodic jobs, one-time jobs, events, commands — are **two contracts on one engine**: an *event* = "something happened", 0..N handlers, fan-out; a *command* = "do this", exactly 1 handler, returns an outcome. All durable work rides **one SKIP LOCKED engine** (`jobqueue` core + `jobqueue/sqlbroker` on pgx: claim-with-lease, retry/backoff, dead-letter, at-least-once). Facades:
> - **`jobqueue`** — one-time jobs (engine, used directly; delayed = `WithRunAt`). Typed handlers over JSON payloads.
> - **`scheduler`** — periodic jobs; a `supervisor.Service` that *enqueues* into the engine when due, fired once per fleet via a `unique(name, scheduled_for)` constraint (no leader needed).
> - **`eventbus`** — events. **Sync in-process mode** (observer, no durability) *and* **durable mode** where each handler registration is a named **subscription = its own SKIP LOCKED queue**: publish fans out one delivery row per subscription; within a subscription, instances are competing consumers (one delivery each); at-least-once. Transactional publish (insert in the business `pgx.Tx`) folds in the old `outbox`.
> - **`commandbus`** *(new)* — commands. **Exactly one handler**, enforced at registration (dup registration fails); returns a result. Sync/in-process by default, optional async-durable via the engine.
>
> At-least-once ⇒ **handlers must be idempotent** (stable id + `Seen(id)` inbox check). SKIP LOCKED does not preserve order — per-key ordering is a future knob, not built now. **`pubsub` (cross-broker) is dropped** — every need above is satisfied on Postgres; a consumer who wants Kafka/NATS implements forge's interface over `watermill` in their own repo.

<sub>2 core · 11 recommended · 4 optional · messaging consolidated per note above</sub>

- **`backoff`** — **core**  
  Backoff strategy primitive: compute exponential/constant/jittered delays. The shared math under retry, circuitbreaker half-open, lock retry, and reconnect loops.  
  `Backoff interface{ Next(attempt int) time.Duration; Reset() }; constructors Exponential(base,max), Constant(d), Jitter(b,fraction). Pure value strategies, no I/O.`  
  <sub>deps: stdlib-only: time, math, math/rand/v2. ~120 LOC; avoids cenkalti/backoff.</sub>

- **`retry`** — **core**  
  Retry an operation with a Backoff strategy, max attempts, RetryIf predicate, Permanent() stop sentinel, and context cancellation. Clock-injectable for deterministic tests.  
  `Do(ctx, fn func() error, opts...) error WithMaxAttempts/WithBackoff/WithRetryIf/WithClock; Permanent(err) error to stop early.`  
  <sub>deps: stdlib-only: context, time, errors. Takes clock.Clock + backoff.Backoff. ~200 LOC. · depends on: `backoff`, `clock`</sub>

- **`singleflight`** — recommended  
  Generic request coalescing: collapse concurrent identical calls per key into one execution and share the result (cache-stampede protection). Local generic impl preferred over x/sync.  
  `Group[T any]: New[T]() *Group[T]; Do(ctx,key,fn func(ctx)(T,error))(T, shared bool, error); Forget(key).`  
  <sub>deps: stdlib-only: context, sync. ~120 LOC; generic improvement over golang.org/x/sync/singleflight.</sub>

- **`parallel`** — recommended  
  Bounded concurrent execution with error aggregation and context cancellation: an errgroup-with-limit plus generic Map/ForEach that cancel on first error.  
  `Group with Go(fn)/Wait()/SetLimit(n); generic ForEach[T](ctx,items,limit,fn) error and Map[T,U](ctx,items,limit,fn)([]U,error).`  
  <sub>deps: stdlib-only: context, sync. ~200 LOC; local instead of golang.org/x/sync/errgroup.</sub>

- **`ttlcache`** — recommended  
  In-memory generic TTL cache with optional max-size LRU eviction and GetOrLoad (pairs with singleflight against stampedes). Clock-injectable. The single-instance cache; Redis is a separate concern.  
  `New[K comparable,V any](opts...) *Cache[K,V]: Get(k)(V,bool); Set(k,v)/SetTTL(k,v,d); Delete; Len; GetOrLoad(k,fn). WithDefaultTTL/WithMaxEntries/WithClock.`  
  <sub>deps: stdlib-only: sync, time, container/list. Takes clock.Clock. ~300 LOC. · depends on: `clock`, `singleflight`</sub>

- **`circuitbreaker`** — recommended  
  Circuit breaker (closed/open/half-open) to fail fast against a failing dependency, with ErrOpen sentinel and OnStateChange. Pairs with retry around flaky upstreams.  
  `New(opts...) *Breaker; Do(ctx, fn func(ctx) error) error returns ErrOpen when tripped; State() State (exported enum). WithFailureThreshold/WithOpenTimeout/WithOnStateChange/WithClock.`  
  <sub>deps: stdlib-only: sync, time, errors. Takes clock.Clock. ~250 LOC; avoids sony/gobreaker. · depends on: `clock`</sub>

- **`loadshed`** — recommended  
  Adaptive load shedding / backpressure: reject the marginal request early (429/503 + Retry-After) based on in-flight concurrency or observed latency, before the system saturates. Distinct from ratelimit (request RATE) and timeout (per-request deadline); covers the cascading-failure overload mode.  
  `New(opts...)(*Shedder,error); (*Shedder).Middleware() middleware.Middleware; (*Shedder).Acquire(ctx)(release func(), ok bool) for non-HTTP call sites (jobqueue/parallel admission). Config{MaxInFlight,TargetLatency,Strategy('concurrency'|'latency')}+Validate. Sentinels ErrShed/ErrInvalidConfig.`  
  <sub>deps: stdlib-only: net/http, sync, time. Emits slog attrs (inflight, shed_total). · depends on: `middleware`, `problem`, `clock`</sub>

- **`ratelimit`** — recommended  
  CANONICAL keyed rate limiter (token-bucket/sliding-window) over a pluggable Store, with in-memory default and an http.Handler middleware emitting RateLimit-* headers. Distributed Redis store is an isolated subpackage. Distinct from quota (cumulative billing-window caps) and throttle (failure-counting+lockout).  
  `New(opts...) *Limiter: Allow(ctx,key)(Result,error) Result{Allowed,Remaining,RetryAfter}; Reserve/Wait. Store interface (in-mem MemoryStore built-in). Middleware(keyFn) middleware.Middleware. Sentinel ErrLimited.` 
<sub>deps: stdlib-only core: sync, time. Takes clock.Clock. ratelimit/redisstore subpackage isolates the Redis driver behind the Store interface (logger/sentry pattern). · depends on:`clock`, `middleware`, `problem`</sub>

- **`quota`** — recommended  
  Enforce cumulative usage limits per subject over a calendar/rolling window (requests/month, seats, storage bytes, AI tokens) with reset boundaries — the plan-entitlement counterpart to ratelimit. Limit resolver is caller-owned so plan/pricing logic stays in the consumer (no billing coupling).  
  `New(store Store, opts...) *Quota; Allow(ctx, subject string, n int64)(Decision,error) Decision{Allowed,Used,Limit,Remaining,ResetAt}; WithLimits(func(subject) Limit); Window helpers Daily/Monthly/Rolling(d). Optional Middleware emitting X-Quota-* headers. Sentinels ErrOverQuota/ErrNoLimit.`  
  <sub>deps: stdlib-only core: time. Store interface (atomic counters with windows); in-memory ships; Redis/SQL adapters via subpackage. Takes clock.Clock. · depends on: `clock`, `tenant`, `problem`, `middleware`</sub>

- **`lock`** — recommended  
  CANONICAL distributed mutual exclusion with TTL leases, fencing tokens, and auto-refresh, PLUS leader election as a supervisor.Service (RunAsLeader/Elect) so cron/outbox/reconcilers run on exactly one replica with failover. Merges the dlock/leader/lock proposals. Not a consensus library; leans on the backend's atomic primitive.  
  `New(store Store, opts...)(*Locker,error); Acquire(ctx,key)(*Lease,error); (Lease) Token() uint64 (fencing)/Release(ctx)/Done() <-chan struct{} (auto-refresh until ctx/loss). Leader: RunAsLeader(ctx,key, fn func(ctx)) implements supervisor.Service; IsLeader() bool. Store{TrySet/Refresh/Delete with fencing}. Sentinels ErrNotAcquired/ErrLeaseLost.`  
  <sub>deps: stdlib-only core + pluggable Store interface (in-process store ships for single-node/tests). Postgres-advisory-lock and Redis-SETNX backends in lock/pglock and lock/redislock subpackages (logger/sentry pattern). · depends on: `supervisor`, `backoff`, `clock`, `logger`</sub>

- **`eventbus`** — recommended — *events facade; see Messaging model note*  
  Type-safe generic events. **Sync in-process mode** (observer) for same-process listeners, **and durable mode** where each `Subscribe` declares a named subscription (its own SKIP LOCKED queue): publish fans out one row per subscription, competing consumers per subscription, at-least-once. Transactional publish via the business `pgx.Tx` (folds in `outbox`).  
  `Bus: eventbus.New(broker?, opts...); Subscribe[E](bus,"subscription",fn func(ctx,E) error); Publish(ctx, tx, E). No broker = sync observer; with broker = durable subscriptions (supervisor.Service workers).`  
  <sub>deps: stdlib (sync mode) / pgx via the jobqueue engine (durable mode). Generics, no reflection. · depends on: `jobqueue`, `supervisor`, `logger`</sub>

- **`jobqueue`** — recommended — **THE durable engine** (one-time jobs facade + substrate under scheduler/eventbus/commandbus)  
  Supervised worker pool (bounded concurrency, per-job retry/backoff, graceful drain) over a pluggable Broker with claim-with-lease (at-least-once), max-attempts → dead-letter. Producer Client separate from worker Service. In-memory broker in core; **`jobqueue/sqlbroker` is the SKIP LOCKED pgx impl** (the only durable broker forge ships). Typed handlers over JSON payloads.  
  `Worker: NewWorker(broker,opts...) supervisor.Service; Register[T](w,"kind",fn func(ctx,T) error); WithConcurrency/WithBackoff/WithDeadLetter. Client.Enqueue(ctx,"kind",payload, WithRunAt/WithMaxAttempts). Broker interface{ Push/Claim/Ack/Nack }.`  
  <sub>deps: stdlib core + in-memory broker; **`jobqueue/sqlbroker` uses pgx** (caller's `pgxpool.Pool`) — `LISTEN/NOTIFY` wakeups + poll fallback, SKIP LOCKED claim, SQLSTATE retry classification. · depends on: `supervisor`, `backoff`, `drain`, `logger`</sub>

- **`scheduler`** — recommended — *periodic-jobs facade*  
  Cron/interval scheduler as a supervisor.Service that **enqueues into the jobqueue engine when due**. Fires **once per fleet** via a `unique(name, scheduled_for)` constraint — every instance races the insert, one wins, the rest no-op (no leader election needed).  
  `New(client, opts...) supervisor.Service; WithCron(name,spec,fn)/WithInterval(name,d,fn)/WithJitter. Local ~150-LOC 5-field cron parser (no robfig/cron).`  
  <sub>deps: stdlib (time/context) + pgx via the engine for the dedup insert. · depends on: `jobqueue`, `supervisor`, `logger`, `clock`</sub>

- **`commandbus`** — recommended — *commands facade (NEW)*  
  Point-to-point command dispatch with **exactly one handler**, enforced at registration (a second handler for the same command type fails). Returns a result (unlike events). Sync/in-process by default; optional async-durable via the jobqueue engine (single logical consumer).  
  `Bus: commandbus.New(); Handle[C](bus, fn func(ctx,C)(R,error)); Send[C,R](ctx,bus,cmd)(R,error). Async: Enqueue[C](ctx,bus,cmd) onto the engine. Sentinels ErrNoHandler/ErrDuplicateHandler.`  
  <sub>deps: stdlib (sync) / pgx via the engine (async). Generics, no reflection. · depends on: `jobqueue` (async mode only), `logger`</sub>

- **`watchdog`** — optional  
  Heartbeat/deadman watchdog as a supervisor.Service: components emit periodic Kick()s; a missed deadline fires a callback (alert, flip readiness, controlled crash). Detects a hung-but-not-returned background loop that liveness probes miss; distinct from supervisor (observes Run returning) and health (external probe).  
  `New(opts...)(*Watchdog,error) implementing supervisor.Service; Register(name string, timeout time.Duration)(kick func()); WithOnMiss(func(name string, since time.Duration)). Config{CheckInterval}+Validate. Often wired to readiness.SetReady(false) on miss.`  
  <sub>deps: stdlib-only: context, sync, time. Composes supervisor. · depends on: `supervisor`, `clock`, `logger`</sub>

- **`pubsub`** — ~~optional~~ **DROPPED** (cross-broker not in scope; needs satisfied on Postgres — a consumer wanting Kafka/NATS implements forge's interface over `watermill` in their own repo)  
  Distributed pub/sub abstraction: Publisher/Subscriber interfaces and a supervised Consumer loop, with concrete brokers (Redis/NATS/Postgres LISTEN-NOTIFY) in isolated subpackages. The cross-process sibling of eventbus.  
  `Publisher{ Publish(ctx,topic,[]byte) error }; Subscriber{ Subscribe(ctx,topic)(<-chan Message,error) }. New(broker,opts...) *Consumer implementing supervisor.Service; WithBackoff(reconnect)/WithLogger.`  
  <sub>deps: stdlib-only core (interfaces + consumer loop). pubsub/redisbroker, pubsub/natsbroker, pubsub/pgbroker each isolate their client. Topic/payload are []byte. · depends on: `supervisor`, `backoff`, `logger`</sub>

- **`outbox`** — ~~optional~~ **FOLDED into `eventbus` durable mode** (transactional publish = insert in the business `pgx.Tx`; `Seen(id)` inbox dedup stays a helper)  
  Transactional outbox/inbox: Stage(ctx, tx, msg) enqueues in the caller's DB tx; a supervised Relay drains staged rows to a Publisher with retry; Seen(ctx, tx, id) dedupes inbound. Avoids dual-write inconsistency. Gate the relay to one replica via lock.  
  `free-func Stage(ctx,*sql.Tx,Message) error + Seen(ctx,tx,id)(bool,error); Relay: New(db,pub,opts...) *Relay implementing supervisor.Service. WithBatchSize/WithPollInterval/WithBackoff. Consumer owns schema/migration.`  
  <sub>deps: stdlib-only at the seam: database/sql (*sql.Tx/*sql.DB supplied by caller, no driver). Publisher is the pubsub.Publisher interface. SELECT...FOR UPDATE SKIP LOCKED dialect option. · depends on: `supervisor`, `sqltx`, `txcontext`, `pubsub`, `backoff`, `lock`</sub>

- **`drain`** — optional  
  Fine-grained graceful-drain helper: track in-flight units and block (up to a deadline) until they finish during shutdown. Complements supervisor's service-level ShutdownTimeout; shared by jobqueue/pubsub consumers and the readiness drain sequence.  
  `New(opts...) *Drainer: Begin() (done func()); Wait(ctx) error (ErrDrainTimeout); Closed() reject-new gate. WithTimeout/WithLogger.`  
  <sub>deps: stdlib-only: context, sync, time. ~80 LOC. Composes supervisor. · depends on: `supervisor`</sub>

### P3 · Web / HTTP transport · 19 packages

_net/http transport layer: the shared Middleware seam plus stateless free-func helpers and middleware factories for the request pipeline, content handling, idempotency, maintenance gating, and static/fingerprinted assets. Composes with existing httpserver/hostrouter/render/htmx._

<sub>5 core · 11 recommended · 3 optional</sub>

- **`middleware`** — **core**  
  Defines the shared Middleware alias type and Chain/Wrap composition the entire web layer reuses, so each cross-cutting concern is a standalone package returning Middleware. Also the documented stdlib-ServeMux routing stance (Group/Handle sugar) — absorbs the 'router' proposal.  
  `type Middleware func(http.Handler) http.Handler; Chain(mw...) Middleware; Wrap(h, mw...) http.Handler; WrapFunc(h, mw...). Plus Group(mux,prefix,mw...)/Handle(mux,pattern,h,mw...) ServeMux sugar.`  
  <sub>deps: stdlib-only: net/http. The universal seam; no external router now that ServeMux has method+wildcard patterns.</sub>

- **`recover`** — **core**  
  Recover from handler panics, log a single-line slog error, and emit a clean 500 (delegating to problem by default via an injectable responder). Per-request counterpart to supervisor's process recovery.  
  `New(opts...) middleware.Middleware; WithLogger(*slog.Logger)/WithResponder(func(w,r,any))/WithStackTrace(bool).`  
  <sub>deps: stdlib-only: net/http, log/slog, runtime. Opt-in stack capture to honor single-line-error rule. · depends on: `middleware`, `logger`, `problem`</sub>

- **`requestid`** — **core**  
  Assign/propagate a per-request correlation ID via header and context, and expose a logger.ContextExtractor so it flows into logs. IDs route through pkg/id; context via ctxkey.  
  `New(opts...) middleware.Middleware; FromContext(ctx)(string,bool); Extractor() logger.ContextExtractor; WithHeader/WithGenerator/WithTrustInbound.`  
  <sub>deps: stdlib-only: net/http. ID generation via id; typed context via ctxkey. · depends on: `middleware`, `logger`, `id`, `ctxkey`</sub>

- **`bind`** — ~~core~~ **SUPERSEDED by the existing `request` package** (see Reconciliation note above; drop this entry)  
  Decode query/path/header/form/JSON request data into typed values WITHOUT reflection-tag magic: generic JSON[T], explicit scalar helpers, and an opt-in DecodeRequest structural interface. Struct-tag decode (when used) goes through the shared structfields + typeconv primitives.  
  `free-funcs+generics: JSON[T](r)(T,error); Query(r) url.Values + QueryInt/QueryBool(r,key,def); PathValue(r,key); optional Decoder interface{ DecodeRequest(*http.Request) error }.`  
  <sub>deps: stdlib-only: encoding/json, net/http, net/url. Uses structfields (reflection confined there) + typeconv for scalar coercion; avoids gorilla/schema/mapstructure. Validation is separate (validate). · depends on: `validate`, `problem`, `structfields`, `typeconv`</sub>

- **`problem`** — **core**  
  RFC 9457 problem+json structured API errors with a canonical exported Problem value and Write helpers. The shared default responder for recover/bind/bodylimit/timeout/ratelimit/loadshed/quota/idempotency. Owns application/problem+json; render stays generic.  
  `Problem struct (Type/Title/Status/Detail/Instance/Extensions, JSON-tagged); Write(w, Problem) error; New(status,detail) Problem; From(err) Problem; WriteError(w,status,err).`  
  <sub>deps: stdlib-only: encoding/json, net/http. Composes render's transactional-buffer technique. · depends on: `render`</sub>

- **`realip`** — recommended — **already covered by `request.ClientIP` + `WithTrustedProxies`** (only a context-middleware wrapper would be new)  
  Derive the true client IP from XFF/X-Real-IP/Forwarded ONLY behind configured trusted proxy CIDRs (falls back to RemoteAddr otherwise), storing netip.Addr in context. (iplist allow/deny lives in security-web; geoip resolves meaning.)  
  `New(opts...) middleware.Middleware; FromContext(ctx)(netip.Addr,bool); Config{TrustedProxies []string}+Validate; WithTrustedProxies(...netip.Prefix)/WithHeader.`  
  <sub>deps: stdlib-only: net/netip, net/http. Trusted-CIDR Config with env tags; context via ctxkey. · depends on: `middleware`, `logger`, `ctxkey`</sub>

- **`reqlog`** — recommended  
  Structured access-logging middleware: method/path/status/bytes/duration as slog attrs, with a wrapped ResponseWriter that preserves Flusher/Hijacker/Pusher (so SSE/streaming survive).  
  `New(opts...) middleware.Middleware; WithLogger/WithSkip(func(*http.Request) bool)/WithLevel/WithAttrs(func(*http.Request,Record) []slog.Attr). Record is exported.`  
  <sub>deps: stdlib-only: net/http, log/slog, time. Single-line slog records. · depends on: `middleware`, `logger`, `requestid`</sub>

- **`timeout`** — recommended  
  Per-request deadline middleware that cancels the request context and writes 503/504 on expiry. Complements httpserver connection timeouts; must not be applied to SSE/streaming routes.  
  `New(d time.Duration, opts...) middleware.Middleware; WithResponder/WithSkip. Duration is the required positional arg.`  
  <sub>deps: stdlib-only: context, net/http, time. · depends on: `middleware`, `problem`</sub>

- **`bodylimit`** — recommended  
  Cap request body size to guard against memory-exhaustion, applied before bind/decode, using iox's over-limit-erroring reader for correct 413 semantics. 413 responder delegates to problem.  
  `New(maxBytes int64, opts...) middleware.Middleware; WithResponder; thin Limit(r,n) helper over iox.LimitReader.`  
  <sub>deps: stdlib-only: net/http. Composes bytesize for human limits and iox for the erroring LimitReader. · depends on: `middleware`, `bytesize`, `iox`, `problem`</sub>

- **`compress`** — recommended  
  Negotiate and apply gzip/deflate response compression by Accept-Encoding, skipping already-compressed types and preserving Flusher (SSE). Brotli excluded from core (isolate as compress/brotli if ever needed).  
  `New(opts...) middleware.Middleware; WithLevel/WithMinSize/WithContentTypes/WithSkip.`  
  <sub>deps: stdlib-only: compress/gzip, compress/flate. Composes negotiate. · depends on: `middleware`, `negotiate`</sub>

- **`cors`** — recommended  
  CORS preflight + Access-Control-\* headers per an env-loadable allow-policy, with Validate rejecting the wildcard-origin + credentials vuln. Distinct from hostrouter (Host) — this is Origin policy.  
  `New(opts...) middleware.Middleware; Config{AllowedOrigins,AllowedMethods,AllowCredentials,MaxAge}+Validate; WithAllowedOrigins/WithOriginFunc.`  
  <sub>deps: stdlib-only: net/http, strings, time. Env-loadable allowlist Config + code-only WithOriginFunc. · depends on: `middleware`</sub>

- **`idempotency`** — recommended  
  CANONICAL middleware that dedupes mutating requests by Idempotency-Key, replaying the stored first response (status+headers+body) on client/gateway retries and rejecting key-reuse with a different payload fingerprint. The at-most-once side-effect counterpart to retry/webhook senders; distinct from webhookverify (signatures) and ttlcache (generic cache).  
  `New(store Store, opts...)(*Idempotency,error); Middleware() middleware.Middleware (sets Idempotent-Replayed on replay). Store{ Reserve(ctx,key,ttl)(claimed bool, prior *Record, err error); Save(ctx,key,Record) error }; Record{Status,Header,Body,FingerprintHash}. Config{HeaderName,KeyTTL,Methods,MaxBodyBytes}+Validate; WithScope(func(*http.Request) string) for per-tenant namespacing. Sentinels ErrKeyConflict(409)/ErrInProgress/ErrNoStore.`  
  <sub>deps: stdlib-only core (Store is a forge interface; in-memory store ships, default backable by ttlcache). SQL/Redis adapters in idempotency/<driver> subpackages. Reserve must be atomic (mutex / INSERT ON CONFLICT / SETNX). · depends on: `middleware`, `problem`, `ttlcache`, `requestid`</sub>

- **`maintenance`** — recommended  
  Maintenance-mode middleware kill-switch: when enabled, serves 503 + Retry-After to all traffic except an allowlist (health/admin paths, admin CIDRs) so risky migrations/incidents take the app offline while keeping health reachable and crawlers backing off correctly. Toggled at runtime via flag/configwatch/featureflag.  
  `New(opts...)(*Mode,error); (*Mode).Wrap(http.Handler) http.Handler; Enable()/Disable()/Enabled() bool; SetMessage(string). Config{RetryAfter,BypassPaths,BypassCIDRs,StatusCode}+Validate; WithRenderer(func(w,r)). Sentinel ErrInvalidConfig.`  
  <sub>deps: stdlib-only: net/http. Reuses iplist for the CIDR bypass; optional WithRenderer for a custom 503 page. · depends on: `middleware`, `iplist`, `problem`, `render`</sub>

- **`static`** — recommended  
  Serve embedded/disk static assets (fs.FS) with correct content types, range support, and cache headers; implements http.Handler. Raw byte serving — fingerprinted cache-busting URLs live in the assets package. No build pipeline (esbuild is out of scope).  
  `New(fsys fs.FS, opts...)(*Handler,error); ServeHTTP; WithPrefix/WithCacheControl/WithETag.`  
  <sub>deps: stdlib-only: io/fs, net/http, embed-compatible. Composes conditional for 304. · depends on: `conditional`</sub>

- **`assets`** — recommended  
  CANONICAL fingerprinted asset URL resolver: build a content-hash manifest over an fs.FS (or read a bundler's manifest.json), serve hashed files with immutable far-future cache headers, and expose URL(name)+Integrity(name) helpers for templ templates. Merges the web/i18n-ui assets proposals. Complements static (raw serving); not a bundler.  
  `New(fsys fs.FS, opts...)(*Manifest,error); URL(name string) string ('/static/app.4f3a1b.css'); Integrity(name) string (SRI); Handler() http.Handler (Cache-Control: public, max-age=31536000, immutable); Func() for viewhelper/templ. WithPrefix/WithManifestPath/WithDev(bool). Sentinels ErrUnknownAsset/ErrEmptyFS.`  
  <sub>deps: stdlib-only: crypto/sha256, io/fs, net/http, encoding/json. embed.FS friendly; manifest built at New or parsed from bundler output. · depends on: `conditional`, `viewhelper`</sub>

- **`upload`** — recommended — **re-scoped to a validator:** `request.File/Files` already parses multipart; this adds MIME-allowlist + `filetype` magic-byte sniff on top  
  Parse/validate multipart uploads: size caps, MIME/extension allowlist, real content sniffing via filetype (not trusting Content-Type). Stateless free-funcs; pairs with objectstore for persistence.  
  `free-funcs+Validator value: Parse(r, field string, v Validator)(*File,error); File{Filename,ContentType,Size,Open}; Validator{MaxSize,AllowedTypes,AllowedExt}; SniffContentType(r). Sentinels ErrTooLarge/ErrDisallowedType.`  
  <sub>deps: stdlib-only: net/http, mime/multipart, path/filepath. Composes bytesize (limits) and filetype (content sniffing). · depends on: `bytesize`, `filetype`</sub>

- **`negotiate`** — optional  
  Content negotiation: parse Accept/Accept-Language/Accept-Encoding q-values and pick the best offer. Powers compress and JSON-vs-HTML branching. Stateless free-funcs.  
  `free-funcs: Accept(r, offers...) string; Language(r, offers...) string; AcceptsJSON(r) bool; ParseAccept(header) []Spec (Spec{Value,Quality} exported).`  
  <sub>deps: stdlib-only: net/http, strings, strconv. Local q-value parsing.</sub>

- **`conditional`** — optional  
  ETag generation and If-None-Match/If-Modified-Since handling to emit 304. Stateless free-funcs used by static/assets and cacheable handlers; document ordering vs compress.  
  `free-funcs: ETag(w, body []byte)(matched bool); Check(w,r,etag,modtime)(notModified bool); SetLastModified(w,t).`  
  <sub>deps: stdlib-only: net/http, crypto/sha256, time.</sub>

- **`proxy`** — optional  
  Thin reverse-proxy over httputil.ReverseProxy with X-Forwarded hygiene, slog, timeouts, and error handling; returns http.Handler so it composes with hostrouter (Host->upstream gateway). Keep thin (no LB pools/retries).  
  `New(target *url.URL, opts...)(http.Handler,error); WithLogger/WithRewrite/WithErrorHandler/WithTransport/WithFlushInterval.`  
  <sub>deps: stdlib-only: net/http/httputil, net/url, log/slog. · depends on: `logger`, `realip`</sub>

### P4 · Security (web boundary) · 6 packages

_Security-focused HTTP middleware and request-boundary protections: headers/CSP, CSRF, cookies, IP lists, CAPTCHA verification, inbound webhook verification. Build on crypto primitives and the web Middleware seam._

<sub>0 core · 6 recommended · 0 optional</sub>

- **`secheaders`** — recommended  
  Baseline security response headers + typed CSP builder with per-request nonce, as middleware with secure defaults. Nonce flows via context (cspnonce) for templ.  
  `New(opts...) middleware.Middleware; Config{HSTSMaxAge,FrameOptions,CSP}+DefaultConfig(secure)+Validate; WithHSTS/WithCSP(Policy)/WithReferrerPolicy; Nonce(r) string. Policy is exported with String().`  
  <sub>deps: stdlib-only: net/http, strings, context, crypto/rand. · depends on: `middleware`</sub>

- **`cookie`** — recommended  
  Sign+encrypt cookies with secure defaults (HttpOnly, SameSite, \_\_Host- prefix) and key rotation, returning an exported *Codec. Foundational for csrf, flash, and the cookie session store.  
  `New(keys keyset.Keyset, opts...)(*Codec,error); Write(w,name,value) error; Read(r,name)(string,error); Encode/Decode bytes. WithMaxAge/WithSameSite/WithSecure/WithPath/WithDomain.` 
<sub>deps: stdlib-first: net/http + composes secret (AEAD) and sign (HMAC). Keys are a required New arg supporting rotation via keyset. · depends on:`secret`, `sign`, `keyset`</sub>

- **`csrf`** — recommended  
  CANONICAL stateless double-submit / signed-token CSRF protection with a Token(r) template helper, htmx-compatible header, and unsafe-method middleware. Builds on sign + cookie.  
  `New(key []byte, opts...)(*Protector,error); Middleware() middleware.Middleware; Token(r) string; Verify(r) error. WithTrustedOrigins/WithCookieName/WithHeaderName. Sentinels ErrTokenMissing/ErrTokenInvalid.`  
  <sub>deps: stdlib-only: net/http + composes sign/randx/subtlex/cookie. No session store dependency. · depends on: `middleware`, `sign`, `randx`, `subtlex`, `cookie`</sub>

- **`iplist`** — recommended  
  IP allow/deny lists with CIDR matching as middleware (admin-panel restrictions, blocklists). Client-IP extraction itself lives in realip; iplist composes it. Reused by maintenance for the CIDR bypass.  
  `New(opts...)(*List,error); Allowed(ip netip.Addr) bool; Middleware() middleware.Middleware; WithAllow(cidrs...)/WithDeny(cidrs...). Sentinels ErrForbiddenIP/ErrInvalidCIDR.`  
  <sub>deps: stdlib-only: net/netip, net/http. · depends on: `middleware`, `realip`</sub>

- **`captcha`** — recommended  
  Server-side verification of CAPTCHA challenge tokens (Cloudflare Turnstile, hCaptcha, reCAPTCHA) via a provider Verifier interface, for bot protection on public sign-up/contact/login forms. Mirrors webhookverify's verify-a-token shape; providers isolated in subpackages, core provider-agnostic.  
  `New(verifier Verifier, opts...) *Guard; Verify(ctx, token, remoteIP string)(Result,error) Result{Success bool, Score float64, Action string}; Middleware() middleware.Middleware. Verifier{ Verify(ctx, token, ip)(Result,error) }.`  
  <sub>deps: stdlib-only core (Verifier interface + types). Provider HTTP calls go through forge httpclient; captcha/turnstile, captcha/hcaptcha, captcha/recaptcha subpackages hold the thin POST+JSON adapters, no SDK deps. · depends on: `httpclient`, `middleware`, `problem`</sub>

- **`webhookverify`** — recommended  
  Verify inbound webhook signatures (Stripe/GitHub/Slack HMAC schemes) with constant-time compare and timestamp tolerance; reads+restores r.Body. Security-critical, stateless free-funcs.  
  `free-funcs: Verify(payload, header string, secret []byte, opts...) error; VerifyRequest(r, secret, opts...)(body []byte, err error); VerifyStripe/VerifyGitHub/VerifySlack; WithTolerance/WithNow. Sentinels ErrSignatureMismatch/ErrTimestampExpired.`  
  <sub>deps: stdlib-only: crypto/hmac, crypto/sha256, crypto/subtle, net/http. Provider scheme parsing local. · depends on: `subtlex`</sub>

### P5 · Auth & identity · 14 packages

_Authentication and authorization utilities assembled from crypto primitives: sessions with pluggable stores, multi-factor, numeric OTP, OAuth/OIDC client, API keys, magic links, invitations, and policy evaluation. Account lifecycle stays in consumer repos._

<sub>1 core · 11 recommended · 2 optional</sub>

- **`session`** — **core**  
  Server-side session lifecycle (Start/Load/Save/Destroy/Rotate) over a pluggable Store, with cookie wiring and rotate-on-privilege-change to prevent fixation. The foundational stateful-auth package.  
  `New(store Store, opts...)(*Manager,error); Start/Load/Save/Destroy/Rotate; Config{CookieName,TTL,IdleTimeout,SameSite,Secure}+Validate; WithCookieCodec/WithIDGenerator. Store{Get/Set/Delete}.`  
  <sub>deps: stdlib-only core: net/http, crypto/rand, encoding. Store is a small interface; backends are the sessionstore package. Composes cookie; context via ctxkey. · depends on: `cookie`, `id`, `clock`, `ctxkey`</sub>

- **`sessionstore`** — recommended  
  session.Store implementations: in-memory (dev/single-node, TTL eviction) in core; SQL, Redis (injected client interface), and stateless encrypted-cookie stores. Subpackages isolate drivers; may back onto the shared kv seam.  
  `NewMemory(opts...) *Memory; NewCookie(secret *secret.Box, opts...) *Cookie; sql.New(db *sql.DB, opts...)(Schema() DDL); redis.New(client, opts...). All implement session.Store.`  
  <sub>deps: stdlib-only core (memory + cookie store via secret). sessionstore/sql uses database/sql; sessionstore/redis isolates the client behind a tiny interface. · depends on: `session`, `secret`, `ttlcache`, `sqldb`</sub>

- **`authmw`** — recommended  
  Middleware authenticating requests (Bearer/API-key/session) via a Verifier interface (satisfied by token/apikey/session) and injecting the identity into context for request-scoped reads, with an exported IdentityFromContext accessor.  
  `New(verifier Verifier, opts...); RequireAuth() middleware.Middleware; Optional(); IdentityFromContext(ctx)(Identity,bool); WithErrorHandler.`  
  <sub>deps: stdlib-only: net/http, context. Composes token/apikey/session via Verifier; context via ctxkey. · depends on: `middleware`, `session`, `apikey`, `token`, `ctxkey`</sub>

- **`totp`** — recommended  
  RFC 6238/4226 TOTP/HOTP secret generation, skew-window verification, and otpauth:// URI for QR provisioning (authenticator apps). Self-implemented (~150 LOC), no pquerna/otp. Distinct from otp (numeric codes by email/SMS) and magiclink (signed URLs).  
  `free-funcs+options: GenerateSecret(opts...)(Secret,error); Validate(secret,code, opts...) bool WithSkew/WithPeriod/WithDigits; ProvisioningURI(secret,account,issuer) string.`  
  <sub>deps: stdlib-only: crypto/hmac, crypto/sha1+sha256, encoding/base32, encoding/binary, net/url. QR image generation out of scope. · depends on: `randx`, `subtlex`</sub>

- **`otp`** — recommended  
  Generate, store, hash, and verify short numeric one-time codes for email/SMS verification (sign-up, email-2FA, step-up): attempt-limited, single-channel, TTL'd. Fills the gap between totp (app-based) and magiclink (URL-based); delivery channel is the caller's (email/sms).  
  `New(store Store, opts...) *Service; Issue(ctx, identity string)(code string, err error); Verify(ctx, identity, code string) error; WithLength/WithTTL/WithMaxAttempts. Sentinels ErrCodeExpired/ErrInvalidCode/ErrTooManyAttempts.`  
  <sub>deps: stdlib-only core + pluggable Store interface (in-memory default over ttlcache). Composes randx (generation) + hashx (stored-code hashing). · depends on: `randx`, `hashx`, `ttlcache`, `throttle`</sub>

- **`recoverycodes`** — recommended  
  Generate one-time 2FA backup codes, hash for storage, and verify-and-consume returning the matched index (caller deletes). Constant-time matching. Stateless; persistence is consumer DB.  
  `free-funcs+options: Generate(opts...)(plaintext []string, hashes []string, err error); Verify(plaintext, hashes []string)(matchedIndex int, ok bool); WithCount/WithGroupSize.`  
  <sub>deps: stdlib-only: crypto/rand, crypto/sha256, crypto/subtle. · depends on: `randx`, `subtlex`</sub>

- **`apikey`** — recommended  
  Generate/hash/verify Stripe-style prefixed API keys (sk*live*...) with embedded checksum for cheap rejection and constant-time verify; plaintext shown once, hash stored. High-entropy keys use fast SHA-256 (no Argon2).  
  `Generate(opts...)(plaintext, hash string, err error); Verify(plaintext, hash) bool; Parse(plaintext)(prefix string, err error); WithPrefix("sk_live")/WithEntropyBytes. Sentinel ErrMalformedKey.`  
  <sub>deps: stdlib-only: composes randx/hashx/subtlex; crypto/sha256, encoding, hash/crc32. · depends on: `randx`, `hashx`, `subtlex`, `redact`</sub>

- **`magiclink`** — recommended  
  Passwordless email login: issue signed single-use login links (via token) and verify/consume them; single-use enforced through an injected store interface. Does NOT send email (caller's mailer). Distinct from invite (membership grant with role/tenant claims).  
  `New(signer token.Codec, opts...) *Issuer; Issue(ctx, identifier string)(url string, err error) WithBaseURL/WithTTL; Verify(ctx, rawToken)(identifier string, err error).`  
  <sub>deps: stdlib-only: net/url, time. Composes token; consumed-store is a small injected interface. · depends on: `token`</sub>

- **`invite`** — recommended  
  Issue and verify time-boxed invitation tokens for team/org membership (email invites, seat grants) carrying embedded role/tenant claims; stateless HMAC-signed by default, opt-in store for single-use redemption. Distinct from magiclink (login of an existing identity, no role/tenant claims).  
  `New(signer sign.Signer, opts...) *Inviter; Create(claims Claims)(token string, err error) Claims{Email,TenantID,Role string; ExpiresAt time.Time; Extra map}; Verify(token)(Claims,error); Redeem(ctx, jti) error via WithStore. Sentinels ErrExpired/ErrInvalidToken/ErrAlreadyRedeemed.`  
  <sub>deps: stdlib-only: composes sign/token. Optional WithStore for single-use revocation/redemption tracking. · depends on: `sign`, `token`</sub>

- **`oauthclient`** — recommended  
  OAuth2/OIDC client (login-with-Google/GitHub): auth-code + PKCE, state, token exchange, id_token/userinfo verification, provider presets. Implemented on net/http rather than x/oauth2; id_token signature delegates to a JWKS+jwt-style verifier.  
  `New(cfg ProviderConfig, opts...)(*Client,error); AuthCodeURL(state, opts...)(url); Exchange(ctx,code,verifier)(*Token,error); UserInfo(ctx,*Token)(*Profile,error); GenVerifier()/Challenge(); WithProvider(Google|GitHub).`  
  <sub>deps: stdlib-first: net/http, encoding/json, crypto/ed25519+ecdsa for id_token verify; JWKS fetcher local. No x/oauth2. · depends on: `token`, `csrf`, `httpclient`</sub>

- **`rbac`** — recommended  
  In-memory role/permission matcher with wildcard + hierarchy (admin:* implies admin:read), hot-reloadable. Explicit, no policy-engine dependency (no casbin). Subject->roles mapping is the consumer's DB.  
  `New(policy Policy, opts...)(*Enforcer,error); Can(roles []string, perm string) bool; CanResource(roles,action,resourceType) bool; WithRoleHierarchy. Policy is exported map[Role][]Permission.` 
<sub>deps: stdlib-only: strings, sync.RWMutex. ~200 LOC. · depends on:`set`</sub>

- **`throttle`** — recommended  
  Login throttling and account lockout: failure-counting with exponential backoff and lockout windows over an injected AttemptStore. Distinct from async/ratelimit (request-rate shaping) and quota (cumulative caps).  
  `New(store AttemptStore, opts...)(*Limiter,error); Check(ctx,key)(allowed bool, retryAfter time.Duration, err error); Fail(ctx,key); Reset(ctx,key). Config{MaxAttempts,Window,LockoutDuration,Backoff}.`  
  <sub>deps: stdlib-only core: time. AttemptStore interface (Incr/Get/Reset with TTL) satisfied by sessionstore/kv backends. Takes clock.Clock. · depends on: `clock`, `backoff`, `sessionstore`</sub>

- **`webauthn`** — optional  
  WebAuthn/passkey registration and assertion (challenge create + attestation/assertion verify), with credential storage left to the caller. The ONE place a heavier external dep (CBOR/COSE) is justified — isolated to this package.  
  `New(cfg Config, opts...)(*Server,error); BeginRegistration(user)(opts,state,err); FinishRegistration(state,resp)(Credential,error); BeginLogin/FinishLogin. Config{RPID,RPName,Origin}+Validate.`  
  <sub>deps: Justified isolated external: github.com/go-webauthn/webauthn (CBOR/COSE/attestation are not in stdlib and unsafe to hand-roll). Confined here; core forge stays clean. · depends on: `session`, `recoverycodes`</sub>

- **`abac`** — optional  
  Attribute/condition-based authorization for row-level/ownership/tenancy checks RBAC can't express, using explicit Go predicate funcs (no DSL, no OPA/rego). Optional — most apps only need rbac.  
  `New(rules []Rule, opts...)(*Evaluator,error); Allow(ctx, req Request)(Decision,error) where Request carries Subject/Action/Resource/Env attribute maps.`  
  <sub>deps: stdlib-only. Rules are func(Request) bool registered by name. Avoids opa/rego. · depends on: `rbac`</sub>

### P5 · Realtime & streaming · 8 packages

_Server-push primitives: SSE framing, an in-process fan-out broker, a WebSocket connection helper + hub, long-poll fallback, presence, and a multi-instance backplane interface with isolated driver subpackages._

<sub>2 core · 3 recommended · 3 optional</sub>

- **`sse`** — **core**  
  CANONICAL Server-Sent Events writer: text/event-stream framing (event/data/id/retry), correct headers, per-event flush via http.ResponseController, keep-alive comments, ctx-cancellation. Owns framing render deliberately omits. Document httpserver WriteTimeout=0.  
  `free-funcs + exported Writer: Upgrade(w,r)(*Writer,error); (*Writer).Send(Event)/Comment(string)/Flush(); Event{ID,Name,Data string; Retry time.Duration}; stateless SendEvent(w,Event); SendComponent helper for the htmx-html bridge.`  
  <sub>deps: stdlib-only: net/http, bufio, time, io, strings. Requires http.Flusher. · depends on: `render`, `htmx`</sub>

- **`broker`** — **core**  
  In-process pub/sub fan-out hub: per-topic subscriber channels with bounded buffers and a slow-consumer policy (drop-oldest default). The local fan-out point every realtime feature needs.  
  `New(opts...) *Broker; Subscribe(topic)(*Subscription,error) with C() <-chan Message + Close(); Publish(topic, []byte) error; Close(). Config{BufferSize,SlowConsumerPolicy}+Validate; WithBufferSize/WithLogger.`  
  <sub>deps: stdlib-only: sync, context, log/slog. Channels + mutex. · depends on: `logger`</sub>

- **`ws`** — recommended  
  WebSocket connection helper: accept/upgrade an HTTP request to a typed read/write Conn with ping/pong keepalive and graceful close. The ONE justified realtime external dep (RFC6455 is large/bug-prone), isolated behind exported forge Conn/Message.  
  `Accept(w,r,opts...)(*Conn,error); (*Conn) Read(ctx)(Message,error)/Write(ctx,Message)/Ping(ctx)/Close(code,reason); Message{Type,Data}; Config{ReadLimit,PingInterval,Origins}+Validate; WithOriginPatterns.`  
  <sub>deps: Justified isolated external: github.com/coder/websocket (stdlib has no WS server; x/net/websocket is frozen). Dep confined to this package. · depends on: `logger`</sub>

- **`wshub`** — recommended  
  WebSocket registry + broadcast: track live ws.Conn by id/room, fan out to all/subset, run read/write pumps under supervisor, drain on shutdown. Splits lifecycle/fan-out from ws's per-connection primitive.  
  `New(opts...) *Hub implementing supervisor.Service; Add(conn *ws.Conn, opts...)(*Client,error); Broadcast(ws.Message); Send(clientID,msg) error; Join(clientID,room). WithBroker/WithLogger.`  
  <sub>deps: stdlib-only: sync, context, log/slog + ws + broker. Multi-instance via realtimebus injection. · depends on: `ws`, `broker`, `supervisor`, `realtimebus`</sub>

- **`realtimebus`** — recommended  
  Multi-instance fan-out backplane: a tiny Bus interface with a stdlib Local(broker) adapter as the zero-dep default and isolated driver subpackages (redisbus/natsbus), so cross-instance events reach SSE/WS on every node.  
  `Bus interface{ Publish(ctx,topic,[]byte) error; Subscribe(ctx,topic)(<-chan Message,error); Close() error }; Local(b *broker.Broker) Bus; redisbus.New(client,opts...) Bus.`  
  <sub>deps: stdlib-only core + broker (Local adapter). realtimebus/redisbus isolates the Redis client; realtimebus/natsbus isolates nats.go. · depends on: `broker`, `logger`</sub>

- **`longpoll`** — optional  
  HTTP long-poll fallback for proxies/old browsers: hold a request open until a topic event, timeout, or client disconnect, then write via render. Thin wrapper over broker + ctx.  
  `Wait(ctx,w,r, sub *broker.Subscription, timeout)(Result,error); Handler(broker, opts) http.Handler; Result{Events []broker.Message; TimedOut bool}; Config{DefaultTimeout,MaxTimeout}.`  
  <sub>deps: stdlib-only: context, net/http, time + broker + render. · depends on: `broker`, `render`</sub>

- **`presence`** — optional  
  Presence tracking ('3 viewing'): record online members per channel with TTL heartbeats, emit join/leave events via broker, answer who-is-here. Supervised sweep reaps stale leases; multi-instance is eventually-consistent over realtimebus.  
  `New(opts...) *Tracker implementing supervisor.Service; Join(channel,memberID,meta)(*Lease,error) with Heartbeat()/Leave(); Members(channel) []Member; Events(channel)(<-chan Event,error). Config{TTL,SweepInterval}.`  
  <sub>deps: stdlib-only: sync, time, context, log/slog + broker. realtimebus via option for multi-instance. · depends on: `broker`, `supervisor`, `realtimebus`</sub>

- **`streamhandler`** — optional  
  Glue that exposes a broker/bus topic as an SSE endpoint: an http.Handler that subscribes per request, streams events with Last-Event-ID resume + heartbeat, and handles disconnect. The 90%-case convenience over sse+broker+bus.  
  `Handler(bus realtimebus.Bus, topicFn func(*http.Request) string, opts...) http.Handler; WithHeartbeat/WithEncoder(func(broker.Message) sse.Event). Returns http.Handler.`  
  <sub>deps: stdlib-only: net/http, time, context + sse + broker + realtimebus. · depends on: `sse`, `broker`, `realtimebus`</sub>

### P6 · Integrations & comms · 15 packages

_Outbound communication and third-party I/O behind small Sender/Pusher interfaces with stdlib defaults; provider SDKs live in subpackages or consumer repos. Includes a resilient HTTP client, geo lookup, and AI/LLM seams._

<sub>1 core · 5 recommended · 9 optional</sub>

- **`email`** — **core**  
  Send transactional email via a Sender interface with a built-in stdlib net/smtp implementation; provider adapters (SES/Mailgun/Postmark/Resend) live in subpackages or consumer repos. Flagship of the integrations layer.  
  `Sender{ Send(ctx, Message) error }; SMTP: New(opts...)(*SMTP,error) Config{Host,Port,Username,Password,STARTTLS,Timeout}; WithDialer/WithLogger. Message{From,To,Subject,Text,HTML,Attachments}. Sentinels ErrNoRecipient/ErrSendFailed.`  
  <sub>deps: stdlib-only core: net/smtp, crypto/tls, mime/multipart, net/mail. Sender is 1-method; SDKs isolated in subpackages. · depends on: `logger`</sub>

- **`httpclient`** — recommended  
  Resilient outbound *http.Client (returns stdlib type): per-attempt timeout, bounded jittered retry, and a circuit breaker via a custom RoundTripper. The transport under email/sms/oauth/webhook/captcha/geoip adapters.  
  `New(opts...)(*http.Client,error); Config{Timeout,MaxRetries,BackoffBase,CircuitThreshold,RetryOnStatus []int}; WithTransport/WithLogger/WithRetryPolicy. Sentinels ErrCircuitOpen/ErrMaxRetries.` 
<sub>deps: stdlib-only: net/http, time + composes retry/backoff/circuitbreaker. · depends on:`retry`, `backoff`, `circuitbreaker`, `logger`</sub>

- **`emailtemplate`** — recommended  
  Render named subject + HTML + text email bodies from html/text templates (or a render.Component) into an email.Message. Distinct from sending and from render (targets email bodies, dual HTML/text + subject).  
  `New(fsys fs.FS, opts...)(*Set,error); Render(ctx, name string, data any)(subject string, html, text []byte, err error); WithFuncMap/WithLayout/WithTextFromHTML. Sentinels ErrTemplateNotFound/ErrRender.`  
  <sub>deps: stdlib-only: html/template, text/template, io/fs. Auto text-from-HTML is a small local function. · depends on: `email`</sub>

- **`webhook`** — recommended  
  Outbound webhook delivery: HMAC-signed (Stripe-style t=,v1=) callbacks with timeout, bounded retry/backoff, and per-attempt result. In-process delivery only (durable queue/state is jobqueue/consumer DB). Pairs with webhookverify (inbound).  
  `New(opts...)(*Sender,error); Deliver(ctx, Delivery)(Result,error); Delivery{URL,Event string; Payload []byte; Headers}; Result{StatusCode,Attempts int; Duration}. Config{Secret,SignatureHeader,Timeout,MaxRetries}. Sentinels ErrDeliveryFailed/ErrExhausted.`  
  <sub>deps: stdlib-only: net/http, crypto/hmac, crypto/sha256, time + composes httpclient/sign. · depends on: `httpclient`, `sign`</sub>

- **`llm`** — recommended  
  Provider-agnostic chat/completion + streaming behind Completer/Streamer structural interfaces with plain DTOs; provider SDKs (openai/anthropic) live in subpackages so core imports zero SDKs. The most reused AI primitive.  
  `Completer{ Complete(ctx,Request)(Response,error) }; Streamer{ Stream(ctx,Request)(iter.Seq2[Chunk,error],error) }; New(provider Completer, opts...) *Client (default model/timeout/logger). DTOs Request/Message/Response/Chunk/Usage.`  
  <sub>deps: stdlib-only core: context, iter, net/http, encoding/json. llm/openai, llm/anthropic isolate SDKs; an OpenAI-compatible HTTP adapter can stay stdlib-only. · depends on: `logger`, `httpclient`</sub>

- **`prompt`** — recommended  
  Type-safe prompt templating from an fs.FS registry over text/template with strict missing-key errors. Mechanical only — no chains/agents (business logic). NOT html/template (escaping corrupts prompts).  
  `New(fsys fs.FS, opts...)(*Set,error); Render(name string, data any)(string,error); free Render(tmpl,data)(string,error); WithFuncMap/WithMissingKeyError(default true)/WithDelims.`  
  <sub>deps: stdlib-only: text/template, io/fs, embed.</sub>

- **`sms`** — optional  
  Send SMS behind a Sender interface (no stdlib transport exists, so core is interface + value types); Twilio/Vonage/SNS adapters are isolated subpackages. The canonical seam for OTP/alert delivery.  
  `Sender{ Send(ctx, Message)(MessageID,error) }; Message{To,From,Body string; Unicode bool}; MessageID string. Sentinels ErrInvalidNumber/ErrSendFailed/ErrNoSender.`  
  <sub>deps: stdlib-only core (interfaces + types). sms/twilio etc. isolate provider SDKs. E.164 normalization is a tiny local helper, no libphonenumber.</sub>

- **`push`** — optional  
  Send web/mobile push behind a Pusher interface; WebPush (VAPID/ECDH/AES-GCM) is fully stdlib-doable and ships as push/webpush, while native APNs/FCM are isolated subpackages.  
  `Pusher{ Push(ctx, Notification) error }; Notification{Token,Title,Body string; Data map[string]string; Badge *int}; WebPushSubscription{Endpoint,P256dh,Auth}. Sentinels ErrInvalidToken/ErrPushFailed.`  
  <sub>deps: stdlib-only core + push/webpush (crypto/ecdsa+aes). push/fcm isolates the FCM SDK; APNs uses net/http h2 + JWT (stdlib).</sub>

- **`notify`** — optional  
  Fan one logical notification across channels (email/sms/push) via a Channel interface, joining errors. Pure orchestration, no preference storage (consumer DB). Optional — single-channel apps skip it.  
  `Channel{ Name() string; Deliver(ctx, Event) error }; New(opts...) *Notifier WithChannel(Channel); Notify(ctx, Event) error; Event{Recipient,Template string; Data map; Channels []string}. Sentinels ErrNoChannel/ErrAllChannelsFailed.`  
  <sub>deps: stdlib-only: errors.Join. Channels wrap email.Sender/sms.Sender/push.Pusher. · depends on: `email`, `sms`, `push`</sub>

- **`geoip`** — optional  
  Resolve client IP to country/region/city/ASN via a pluggable Source for geo-routing, data-residency, tax/VAT, fraud throttling, and content localization. Header-based source (CF-IPCountry/X-Geo-*) ships stdlib-only in core; MaxMind .mmdb reader isolated in geoip/maxmind. realip gives the address, geoip gives its meaning.  
  `New(source Source, opts...) *Resolver; Lookup(ctx, ip netip.Addr)(Location,error); Location{Country,Region,City string; ASN uint; Lat,Lng float64}; Source{ Lookup(netip.Addr)(Location,error) }. Header source in core; maxmind.New(path) Source.` 
<sub>deps: stdlib-only core (header Source over net/netip). Optional external .mmdb reader confined to geoip/maxmind subpackage. · depends on:`realip`, `httpclient`</sub>

- **`aistream`** — optional  
  Bridge an llm token stream to an SSE response: write incremental chunks as events with flushing and disconnect handling, ending with [DONE]. Thin adapter mapping llm.Chunk onto the sse writer (does not duplicate sse).  
  `SSE(w,r, chunks iter.Seq2[llm.Chunk,error]) error; Pipe(w,r, src io.Reader); EventSSE(w,r, named iter.Seq[Event]).`  
  <sub>deps: stdlib-only: net/http, context, iter + sse + llm. · depends on: `sse`, `llm`</sub>

- **`embeddings`** — optional  
  Embedder interface + stdlib vector math (cosine/dot/normalize/top-k) for in-memory RAG over small corpora. Deliberately NO vector index/ANN/persistence — pgvector/qdrant are the consumer's data layer.  
  `Embedder{ Embed(ctx, texts []string)([][]float32,error) }; free-funcs Cosine/Dot/Normalize([]float32); TopK(query, corpus [][]float32, k)([]Match).`  
  <sub>deps: stdlib-only core math (math). Concrete embedders reuse llm adapters or isolated subpackages. · depends on: `llm`</sub>

- **`structured`** — optional  
  Coerce noisy LLM text into a typed Go value: strip code fences, extract the JSON object, strict-decode into T (DisallowUnknownFields), and build a repair-prompt on failure. Generic, dependency-free glue for structured extraction.  
  `generic free-funcs: Decode[T](raw string)(T,error); Extract(raw)(json.RawMessage,error); RepairPrompt(raw, err) string; Schema[T]()(json.RawMessage,error).`  
  <sub>deps: stdlib-only: encoding/json, strings, reflect (only for optional Schema hint). No jsonschema lib. · depends on: `llm`</sub>

- **`aiusage`** — optional  
  Token/cost accounting: tally prompt/completion tokens into per-model cost via a configurable price table, emitting structured slog attrs. Computes+logs only; aggregation/storage is the app's DB. Exact cost via money/decimal (no float).  
  `New(opts...) *Meter; Cost(model string, u Usage)(Money,error); Record(ctx, model, u); WithPrices(map[string]Price). Usage{InputTokens,OutputTokens}.`  
  <sub>deps: stdlib-only. Reuses llm.Usage and money (exact decimal). Prices loaded from Config by consumer. · depends on: `llm`, `money`, `logger`</sub>

- **`tokenizer`** — optional  
  Token counting + budget-aware message truncation to stay under context limits: a Counter interface with a stdlib chars/4 Approx impl in core; exact BPE (tiktoken) is an isolated subpackage. Optional (many apps trust max_tokens).  
  `Counter{ Count(text string)(int,error) }; Approx{} ships in-package; free Fit(c, msgs, budget)([]Message,error); Estimate(c, text) int.`  
  <sub>deps: stdlib-only core (Approx). tokenizer/tiktoken isolates the vocab/dep. Reuses llm.Message (one-way dep to avoid cycle). · depends on: `llm`</sub>

### P6 · i18n & server-side UI · 14 packages

_Localization engine and server-rendered UI view-model helpers: message catalog, locale negotiation/context, plurals, number/date formatting, flash, form decode+errors, and pagination/breadcrumb/nonce view helpers. Compose with render/htmx._

<sub>4 core · 6 recommended · 4 optional</sub>

- **`i18n`** — **core**  
  In-memory message catalog: locale lookup with {name} interpolation and fallback chains, immutable after New (concurrent-safe). Does not load files (i18nload) or pluralize (i18nplural). Locale identity is a plain BCP-47 string; no x/text.  
  `New(opts...)(*Catalog,error); T(loc, key string, args...) string; Has(loc,key) bool; Locales() []string; Config{DefaultLocale,FallbackLocale}+Validate; WithMessages(map[string]map[string]string)/WithFallback/WithMissingHandler.`  
  <sub>deps: stdlib-only: strings, fmt, errors. Local locale canonicalizer.</sub>

- **`localematch`** — **core**  
  Negotiate the best supported locale from Accept-Language (or a candidate list) against offered locales with q-value parsing and region fallback (en-US->en). Stateless free-funcs over strings; no x/text/language.  
  `free-funcs: Parse(acceptLanguage) []Tag (Tag{Value string; Q float64}); Match(accept, offered []string, def) string; Best(candidates, offered []string, def) string.`  
  <sub>deps: stdlib-only: strings, strconv, sort. ~80 LOC.</sub>

- **`flash`** — **core**  
  One-shot flash messages surviving a redirect (read-and-cleared next request) over a pluggable Store, with a built-in signed-cookie store so it works zero-extra-deps; server-side stores implement the same iface. For PRG and htmx flows.  
  `Store{ Save(ctx,w,msgs) error; Load(ctx,w,r)([]Message,error) }; New(store, opts...)(*Flasher,error); Add(ctx,w,r, kind Kind, text); Drain(ctx,w,r)([]Message,error); Kind consts Success/Error/Info/Warning; WithCookieStore.`  
  <sub>deps: stdlib-first: net/http, encoding/json + composes cookie for the built-in store. Session-backed store satisfies the same iface. · depends on: `cookie`</sub>

- **`form`** — **core**  
  Decode url.Values/multipart into structs and collect per-field errors as a render-friendly Errors type, with small local validators. Struct field-binding goes through the shared structfields primitive (reflection confined there); validation itself is reflection-free. Backbone of server-rendered CRUD.  
  `Decode(r, dst any)(Values, error); Values exported (Get/Has for sticky fields); Errors map[field][]string with Add/Has/Any/First; free validators Required/Email(v,field,errs).`  
  <sub>deps: stdlib-only: net/http, net/url, strconv, strings. Uses structfields (decode) + typeconv (scalar parse) + validate. No go-playground/validator. · depends on: `validate`, `structfields`, `typeconv`</sub>

- **`i18nload`** — recommended  
  Parse translation files (JSON + flat key=value) from a dir or fs.FS into the map[locale]map[key]string shape i18n.WithMessages consumes; nested JSON flattened to dotted keys. Pairs with go:embed.  
  `free-funcs: Dir(path)(map[string]map[string]string,error); FS(fsys, root)(...); JSON(r, locale)(map[string]string,error). Filename en.json -> locale en.`  
  <sub>deps: stdlib-only: io/fs, encoding/json, path/filepath, strings. Avoids YAML to dodge a dep. · depends on: `i18n`</sub>

- **`i18nctx`** — recommended  
  Carry the request's negotiated locale (and a bound translator func) through context for read-only access in templ trees, plus a middleware that sets it and a LocaleAttr logger.ContextExtractor. The legitimate request-scoped-read context use.  
  `Locale(ctx) string; WithLocale(ctx, loc) context.Context; Translator(ctx) func(key, args...) string; Middleware(next, resolve func(*http.Request) string) http.Handler; LocaleAttr(ctx)(slog.Attr,bool).`  
  <sub>deps: stdlib-only: context, net/http, log/slog. Context keys via ctxkey. · depends on: `i18n`, `localematch`, `logger`, `ctxkey`</sub>

- **`numfmt`** — recommended  
  Locale-aware number/currency/percent formatting (grouping, decimal mark, currency placement) bound once per locale. Steers callers off float money via the money type. Small built-in symbol table; no x/text.  
  `New(locale string, opts...)(*Formatter,error); Decimal(f) string; Currency(m money.Money) string; Percent(f) string; Config{Locale,CurrencyCode}+Validate; WithSymbols(NumberSymbols).`  
  <sub>deps: stdlib-only: strconv, strings, math. Composes money. · depends on: `money`</sub>

- **`datefmt`** — recommended  
  Locale + timezone date/time and relative-time ('3 hours ago') formatting bound once per locale, with named presets. Timezone validated at construction. Gregorian only; full CLDR calendars out of scope.  
  `New(locale string, opts...)(*Formatter,error); Date(t)/DateTime(t)/Time(t)/Relative(t,now) string; Config{Locale,TimeZone,Style}+Validate; WithLayouts/WithLocation.`  
  <sub>deps: stdlib-only: time (IANA tz via LoadLocation), strconv, fmt. Local month/weekday tables; no x/text. · depends on: `clock`</sub>

- **`pageview`** — recommended  
  Pagination VIEW-model: compute page-number window with ellipses and build page links preserving query params, from data/pagination's Page. Split from data pagination (math/cursors) so view concerns don't pull net/url into the data layer.  
  `free-funcs: Window(p pagination.Page, around int) []int (0 = ellipsis sentinel); Links(p, base *url.URL, param string) []Link (Link{Number,Href,Current}).`  
  <sub>deps: stdlib-only: net/url, strconv. Consumes pagination.Page. · depends on: `pagination`</sub>

- **`cspnonce`** — recommended  
  Per-request CSP nonce generation exposed via context for inline script/style tags in templ, plus the CSP-header middleware. The templ-side read seam that composes with secheaders. Context carries only the request-scoped nonce.  
  `New(opts...) middleware.Middleware; Nonce(ctx) string; Config{Policy string; ReportOnly bool}+Validate; WithPolicy/WithReportOnly/WithDirective.`  
  <sub>deps: stdlib-only: crypto/rand, encoding/base64, net/http, context. Context keys via ctxkey. · depends on: `middleware`, `secheaders`, `ctxkey`</sub>

- **`i18nplural`** — optional  
  Select CLDR plural forms (zero/one/two/few/many/other) for a count+locale via a curated local rules table (English fallback), so catalogs store per-form variants. Honest non-goal: full CLDR coverage.  
  `free-funcs: Form(locale string, n int) string; Cardinal(locale, n) string; Ordinal(locale, n) string. i18n.T can call Form to pick key.one/key.other.`  
  <sub>deps: stdlib-only: hand-maintained rules table in Go, no CLDR data files. · depends on: `i18n`</sub>

- **`formui`** — optional  
  View-glue free-funcs turning form.Errors + submitted values into render-ready primitives (error class toggles, aria-invalid, first-error text, sticky values) for templ. Keeps form focused on decode/validate.  
  `free-funcs: FieldError(errs, field) string; HasError(errs, field) bool; InvalidAttr(errs, field) string; ErrorClass(errs, field, cls) string; Value(vals, field) string.`  
  <sub>deps: stdlib-only: strings, html. One-way dep on form. · depends on: `form`</sub>

- **`viewhelper`** — optional  
  Curated pure presentation helpers for templ trees: conditional class joining, generic If ternary, default-if-empty, truncate, URL query mutation. Bounded scope (no business logic) to avoid a junk drawer.  
  `free-funcs+generics: Classes(parts...) string (dedup non-empty); If[T](cond, a, b) T; Default(v, fallback) string; Truncate(s,n) string; QuerySet(u *url.URL, key, val) string.`  
  <sub>deps: stdlib-only: strings, net/url, unicode/utf8. Pluralize delegated to i18nplural. · depends on: `i18nplural`</sub>

- **`breadcrumb`** — optional  
  Breadcrumb trail value type (append returns a copy, not a builder) with current-page marking and optional schema.org JSON-LD output for SEO. Labels pre-translated via i18n by the caller.  
  `New(crumbs...) Trail (exported slice); Crumb{Label,Href string; Current bool}; Trail.Append(label,href) Trail; JSONLD(Trail)(string,error).`  
  <sub>deps: stdlib-only: encoding/json, strings.</sub>

### P7 · Ops & observability · 17 packages

_Operational glue for deployable services: env-tag config loader, build/version info, composable health and readiness probes, container-aware runtime tuning, profiling, metrics facade, thin tracing, feature flags, hot-reloadable secrets, log sampling, and structured audit logging._

<sub>3 core · 7 recommended · 7 optional</sub>

- **`envconfig`** — **core**  
  Populate env-tagged Config structs from environment + .env, closing the loop on forge's inert env tags so consumers don't pull caarlos0/env or viper. Reflection confined to the shared structfields primitive; scalar coercion via typeconv. Boot-time only, no init-time auto-load.  
  `Parse[T any](dst *T, opts...) error; MustParse[T any](dst *T) T; reads env/envDefault/required tags; WithPrefix/WithLookup/WithDotenv. Sentinels ErrMissingRequired/ErrInvalidValue.`  
  <sub>deps: stdlib-only: os, encoding (TextUnmarshaler). Uses structfields (the one sanctioned reflection helper) + typeconv. No external config loader. · depends on: `dotenv`, `bytesize`, `structfields`, `typeconv`</sub>

- **`health`** — **core**  
  Composable LIVENESS probes: register named Checks, aggregate status, serve /livez with cached, timed checks as http.Handler. Driver-free core; concrete checks (SQL/Redis/HTTP) in health/checks subpackage. Readiness (with drain gating) is the separate readiness package.  
  `New(opts...) *Checker (http.Handler); Register(name string, c Check, opts...) where Check func(ctx) error; WithTimeout/WithCacheTTL. JSON report with per-check status/latency.`  
  <sub>deps: stdlib-only: context, net/http, encoding/json, sync, time. health/checks uses database/sql / a Pinger interface, no drivers. · depends on: `logger`, `buildinfo`</sub>

- **`readiness`** — **core**  
  READINESS/startup gate distinct from liveness: aggregates async dependency probes into a single Ready bool served as http.Handler, with a Shutdown() that flips ready=false during graceful drain so the load balancer deregisters BEFORE connections close — the missing half of zero-downtime rollouts. Conflating with liveness causes restart loops.  
  `New(opts...)(*Gate,error); Handler() http.Handler (200 ready / 503 + failing-checks JSON); Register(name string, check func(ctx) error); SetReady(bool); Shutdown() (fail readiness, keep liveness green). Config{Path,CheckTimeout,CacheTTL}. Sentinels ErrNotReady/ErrInvalidConfig. Drain sequence: readiness.Shutdown() first, then httpserver.Shutdown.`  
  <sub>deps: stdlib-only: context, net/http, encoding/json, sync, time. Composes drain in the shutdown sequence. · depends on: `health`, `drain`</sub>

- **`buildinfo`** — recommended  
  Expose build/version metadata (version/commit/build-time/Go/dirty) from ldflags + runtime/debug.ReadBuildInfo as a struct, slog.LogValuer, and a /version http.Handler.  
  `Read() Info{Version,Commit,BuildTime,GoVersion string; Dirty bool}; (Info).String(); (Info).LogValue() slog.Value; Handler() http.Handler. Package vars set via -ldflags -X.`  
  <sub>deps: stdlib-only: runtime/debug, encoding/json, net/http, log/slog. · depends on: `logger`</sub>

- **`automaxprocs`** — recommended  
  Container-aware runtime tuning called once at startup: set GOMAXPROCS from the cgroup CPU quota and GOMEMLIMIT from the cgroup memory limit, fixing the silent host-core-count and OOM-kill footguns. Read-only of /sys; no-op (logged) when not containerized. Distinct from runtimeinfo (reports state — this MUTATES it).  
  `free-funcs: Configure(opts...)(Result,error) Result{GOMAXPROCS int; GOMEMLIMIT int64; Source string}; WithReservePercent/WithMinProcs/WithLogger. ErrNoCgroup is informational, not fatal.`  
  <sub>deps: stdlib-only: parses cgroup v2 then v1 files directly; replaces uber-go/automaxprocs to honor the dep policy (the local cgroup parser justifies avoiding the well-known external dep). · depends on: `logger`</sub>

- **`pprof`** — recommended  
  Wire net/http/pprof safely: a dedicated diagnostics handler/port with optional auth guard so profiling is never silently exposed on the public listener. Thin convenience, not a wrapper.  
  `Handler() http.Handler (fresh mux with /debug/pprof/*); Register(mux, opts...); Service(addr, opts...) supervisor.Service for a dedicated port; WithPrefix/WithAuth(func(*http.Request) bool).`  
  <sub>deps: stdlib-only: net/http, net/http/pprof, runtime. · depends on: `supervisor`</sub>

- **`metrics`** — recommended  
  Metrics facade: a Recorder interface (counter/gauge/histogram) with a stdlib expvar-backed default + JSON handler + request Middleware, and a metrics/prometheus adapter isolated so core stays driver-free. App code records without committing to a backend.  
  `Recorder{ Counter(name, labels...) Counter; Gauge(...); Histogram(...) }; New() *Registry (expvar Recorder) + Handler() http.Handler; Middleware() middleware.Middleware; prometheus.New(reg) Recorder + promhttp handler.`  
  <sub>deps: stdlib-only core: expvar, net/http, sync, time. metrics/prometheus isolates client_golang. · depends on: `middleware`</sub>

- **`featureflag`** — recommended  
  Evaluate boolean/variant flags against request/user context via a Provider interface, with static + env providers in core; vendor SDKs (LaunchDarkly/Unleash) stay opt-in subpackages. No targeting DSL in core. Backs runtime toggles like maintenance mode.  
  `New(p Provider, opts...) *Flags; Provider{ Bool(ctx,key,def) bool; String(ctx,key,def) string }; StaticProvider(map)/EnvProvider; Flags.Enabled(ctx,key)/Variant(ctx,key,def); WithDefault.`  
  <sub>deps: stdlib-only: context, sync, strings. Uses typeconv for env value coercion. Provider is the seam; vendor SDKs not imported. · depends on: `configwatch`, `typeconv`</sub>

- **`auditlog`** — recommended  
  Append-only structured audit events (actor/action/resource/outcome, stable schema, compliance intent) to a pluggable Sink, with a slog-backed default and a JSONL file sink. Distinct from app logging and from data/audit (DB columns).  
  `New(sink Sink, opts...) *Auditor; Event{Actor,Action,Resource,Outcome string; At time.Time; Meta map[string]any}; Record(ctx, Event) error; Sink{ Write(ctx, Event) error }; SlogSink/FileSink; WithExtractor.`  
  <sub>deps: stdlib-only: context, log/slog, encoding/json, time, io. DB/Kafka/S3 sinks via the Sink interface, not in core. Single-line slog attrs. · depends on: `logger`, `buildinfo`</sub>

- **`autocert`** — recommended  
  ACME/Let's Encrypt automatic TLS certificate management wired as a tls.Config + HTTP-01 challenge handler for httpserver, so a self-hosted micro-SaaS terminates its own auto-renewing TLS without a reverse proxy. Isolated so httpserver core stays stdlib-only.  
  `New(opts...)(*Manager,error); TLSConfig() *tls.Config (passed into httpserver via an option); HTTPChallengeHandler(fallback http.Handler) http.Handler; WithHosts/WithCacheDir/WithEmail/WithStaging. Sentinels ErrNoHosts/ErrCacheUnwritable.`  
  <sub>deps: Justified isolated external: golang.org/x/crypto/acme/autocert (ACME is a 1000+ line crypto protocol; x/crypto is the canonical Go-team-maintained impl, reimplementing is unjustifiable). Dep confined to this package.</sub>

- **`dotenv`** — optional  
  .env file parsing into map[string]string (quotes, export prefix, comments, multiline) feeding envconfig.WithLookup. Avoids godotenv. Kept separate for CLI reuse.  
  `free-funcs: Parse(r io.Reader)(map[string]string,error); Load(paths...)(map[string]string,error); Setenv(m) error (skips already-set).`  
  <sub>deps: stdlib-only: io, os, bufio, strings. ~120 LOC.</sub>

- **`profile`** — optional  
  Typed environment/profile enum (dev/test/staging/prod) with parsing and predicate helpers and TextUnmarshaler so envconfig decodes it directly. Removes stringly-typed APP_ENV bugs.  
  `Profile string; consts Development/Test/Staging/Production; Parse(string)(Profile,error); IsProduction()/IsDevelopment(); FromEnv(key) Profile; TextUnmarshaler.`  
  <sub>deps: stdlib-only: strings, encoding. ~80 LOC.</sub>

- **`tracing`** — optional  
  Thin tracing: a Tracer interface, W3C traceparent context propagation + middleware, a no-op default, and a trace_id logger.ContextExtractor — covering correlated logs without forcing OTel. The OTel exporter is the isolated tracing/otel subpackage.  
  `Tracer{ Start(ctx, name)(context.Context, Span) }; Span{ End(); SetError(err); SetAttr(k, v) }; FromContext(ctx)(TraceID,bool); Middleware(t Tracer) middleware.Middleware; NoopTracer; trace_id extractor.`  
  <sub>deps: stdlib-only core: context, net/http, crypto/rand, encoding/hex. tracing/otel isolates the OTel SDK. Context keys via ctxkey. · depends on: `middleware`, `logger`, `ctxkey`</sub>

- **`secretsource`** — optional  
  Hot-reloadable secret/credential source: load secrets from a backend, expose typed redaction-aware atomic getters (returns the existing secret.Secret type), and refresh on TTL/SIGHUP so credential rotation needs no restart. File/env Providers in core; cloud (Vault/AWS SM) Providers are user-supplied. Distinct from configwatch (general config) and keyset (key material storage).  
  `New(provider Provider, opts...)(*Store,error) implementing supervisor.Service; Get(name string)(redact.Secret[string],bool); Watch(name string, func(redact.Secret[string])); Run(ctx) error (polls/refreshes). Provider{ Load(ctx)(map[string]string,error) }. Config{RefreshInterval}+Validate.`  
  <sub>deps: stdlib-only core + pluggable Provider interface; cloud SDKs never imported by core. Runs as supervisor.Service. · depends on: `supervisor`, `secret`, `keyset`, `redact`, `configwatch`</sub>

- **`logsample`** — optional  
  slog.Handler wrapper that rate-samples high-volume log records (first-N-per-interval per call site) to cap log cost while always passing warn/error. Plugs straight into the existing logger.WithHandler seam — it IS just an slog.Handler (same shape as logger/sentry).  
  `NewHandler(next slog.Handler, opts...)(slog.Handler,error); Config{PerInterval,Interval,MinLevelAlways slog.Level (>= bypasses sampling)}+Validate; keyed by (level,message) or WithKeyFunc(func(slog.Record) string). Sentinel ErrInvalidConfig.`  
  <sub>deps: stdlib-only: log/slog, sync, time. · depends on: `logger`</sub>

- **`configwatch`** — optional  
  Watch config source (poll-based mtime, no fsnotify dep) and atomically swap a validated config snapshot at runtime, keeping last-good on validation failure. Runs as supervisor.Service; hands you the new value (you decide what to rebuild).  
  `New[T any](load func()(T,error), opts...)(*Watcher[T],error) implementing supervisor.Service; Current() T (atomic); WithInterval/WithValidate/WithOnReload.`  
  <sub>deps: stdlib-only: os.Stat polling, time. Optional WithWatcher(Notifier) seam lets a consumer plug fsnotify. Generics keep value type exported. · depends on: `supervisor`, `envconfig`, `logger`</sub>

- **`runtimeinfo`** — optional  
  Point-in-time runtime stats (goroutines, memstats, GC, GOMAXPROCS, uptime) as a struct, slog.LogValuer, and JSON handler; optional metrics bridge. Read-only; complements pprof (profiles), automaxprocs (sets the knobs), and buildinfo.  
  `Read() Stats; (Stats).LogValue() slog.Value; Handler() http.Handler; optional Gauges(r metrics.Recorder).`  
  <sub>deps: stdlib-only: runtime, runtime/metrics, time, net/http, encoding/json, log/slog. ~120 LOC. · depends on: `logger`</sub>

### P7 · Application & dev tooling · 11 packages

_Application bootstrap and developer/test tooling: command tree, interactive terminal I/O, main() wiring, signal context, and black-box test harnesses (httptest, golden, fixtures, fakes, dbtest, factory, seed)._

<sub>0 core · 4 recommended · 7 optional</sub>

- **`cli`** — recommended  
  Minimal struct-described command TREE with stdlib flag.FlagSet per command, ctx-aware Run, and auto help — no cobra/urfave (no global init-registry, no builder). Covers serve/migrate/worker/seed/version.  
  `Command{Name,Usage,Short,Long string; Flags func(*flag.FlagSet); Run func(ctx, args []string) error; Sub []*Command}; New(root, opts...) *App; (*App).Execute(ctx, args) error; WithOutput/WithVersion.`  
  <sub>deps: stdlib-only: flag, context, io, text/tabwriter, os. ~300 LOC; rejects cobra/pflag/viper. · depends on: `signalctx`</sub>

- **`term`** — recommended  
  Interactive terminal I/O for CLI tools: prompts (confirm/input/password/select), spinners, progress bars, and tabular output, all writer-injectable for black-box tests and TTY-detecting (degrade to plain output for pipes/CI). Scope is I/O primitives only — command parsing stays in cli.  
  `writer-injected free-funcs + small types: Confirm(in,out,prompt)(bool,error); Input/Password/Select(out,items)(int,error); NewProgress(out,total) with Add(n)/Done(); NewSpinner(out,label) with Stop(); WriteTable(out,headers,rows).`  
  <sub>deps: stdlib-first: bufio, io, os. No-echo password input is the one tricky bit — copy a tiny termios helper or justify golang.org/x/term (single, stdlib-adjacent). TTY detection (isatty) local. · depends on: `cli`</sub>

- **`appmain`** — recommended  
  Bootstrap glue collapsing the main() boilerplate: wire signal context + logger + supervisor (and call automaxprocs) and map errors to an exit code, while the caller's build callback explicitly constructs and wires its own deps (NOT a DI container).  
  `Run(ctx, services []supervisor.Service, opts...) error; Main(build func(ctx)([]supervisor.Service,error), opts...) int; WithLogger/WithShutdownTimeout/WithExitCode. Build func returns explicitly-wired services.`  
  <sub>deps: stdlib-only: context, os, os/signal, time, log/slog + supervisor/logger/signalctx/automaxprocs. · depends on: `supervisor`, `logger`, `signalctx`, `automaxprocs`</sub>

- **`httptest`** — recommended  
  Black-box HTTP test harness over stdlib httptest: spin a real :0 server from an http.Handler, a fluent request builder, and testing.TB-based response asserts (no testify forced on consumers). Pairs with render/htmx/hostrouter.  
  `Serve(t testing.TB, h http.Handler) *Client (auto-Close via t.Cleanup); Client.GET/POST(path, opts...) *Response; ReqOption WithJSON/WithHeader/WithForm/WithCookie; Response.AssertStatus/JSON(&v)/BodyString.`  
  <sub>deps: stdlib-only: net/http, net/http/httptest, testing, encoding/json, io. testify optional in caller tests only.</sub>

- **`signalctx`** — optional  
  Context cancelled on SIGINT/SIGTERM with a second-signal force-quit variant, over signal.NotifyContext. NOTE: supervisor already ships a NewContext() signal helper; this only adds the force-quit pattern and may fold into supervisor instead — verify before building.  
  `Notify(parent, sigs...)(context.Context, context.CancelFunc); Default(parent)(...) SIGINT+SIGTERM; WithForceQuit(parent, code)(...).`  
  <sub>deps: stdlib-only: context, os/signal, syscall. · depends on: `supervisor`</sub>

- **`golden`** — optional  
  Golden-file snapshot testing with a -update flag, for render HTML/CSV/templ output, htmx headers, and CLI help. Minimal stdlib diff (no go-cmp in production graph).  
  `free-funcs: Assert(t testing.TB, got []byte, name...); AssertString(t, got); AssertJSON(t, v) (canonicalized). Writes testdata/<TestName>.golden on -update.`  
  <sub>deps: stdlib-only: testing, os, path/filepath, flag, encoding/json. Local line-diff.</sub>

- **`fixtures`** — optional  
  Load testdata fixture files (bytes/JSON) into types and resolve the testdata dir robustly via runtime.Caller, plus auto-cleanup temp files. Complements golden/httptest.  
  `free-funcs+generics: Bytes(t, name) []byte; JSON[T](t, name) T; Path(t, name) string; TempFile(t, content) string.`  
  <sub>deps: stdlib-only: testing, os, encoding/json, runtime, path/filepath.</sub>

- **`fake`** — optional  
  In-memory test doubles for forge seams: a recording slog.Handler now, growing per-seam (mailer/queue/kv) as those interfaces solidify. Fakes structural interfaces, never vendor clients, so no external deps leak into tests.  
  `LogHandler struct implementing slog.Handler with Records() []slog.Record; Mailer{Sent []email.Message} implementing email.Sender; thread-safe via sync.Mutex.`  
  <sub>deps: stdlib-only: sync, log/slog, context. Imports the faked interfaces' owning packages only. · depends on: `logger`, `email`</sub>

- **`dbtest`** — optional  
  DB test helpers over stdlib database/sql: per-test transaction-rollback isolation, ephemeral schema setup/teardown, Postgres template-DB. Driver-agnostic (caller brings *sql.DB); no testcontainers.  
  `free-funcs: TxRollback(t testing.TB, db *sql.DB) *sql.Tx (t.Cleanup ROLLBACK); WithSchema(t, db, ddl); Template(t, db, name) *sql.DB.` 
<sub>deps: stdlib-only against database/sql; no driver dep. Some helpers Postgres-specific (documented). · depends on:`migrate`, `fixtures`</sub>

- **`factory`** — optional  
  Generic closure-override test-data factory (no struct-tag reflection à la factory_boy): define a base for T, apply func(*T) overrides, BuildN, and persist via a pluggable func(T) error. Storage-agnostic.  
  `Factory[T any]: New[T](base func() T) *Factory[T]; Build(overrides ...func(\*T)) T; BuildN(n, ...) []T; Create(t, persist func(T) error) T; Seq() func() int.` 
<sub>deps: stdlib-only: generics + closures. No faker dep; offers a tiny Seq(). · depends on:`fixtures`</sub>

- **`seed`** — optional  
  Idempotent seeding runner: register named Seeders (explicit WithSeeder, no init magic), run all/by-name, expose an `app seed` cli.Command. Mirrors migrate's shape; the data is consumer business logic.  
  `Seeder{ Name() string; Seed(ctx) error }; New(opts...) *Runner; WithSeeder(Seeder); (*Runner).Run(ctx, names...) error; cli.Command helper.`  
  <sub>deps: stdlib-only: context. Storage-agnostic (seeders close over their deps). · depends on: `cli`, `factory`</sub>

---

## Build order (dependency phases)

Each phase depends only on the ones below it — a clean DAG rooted at `pkg/*`.

### P0 — pkg/\* primitives base

`clock`, `randx`, `id`, `ctxkey`, `encoding`, `typeconv`, `structfields`, `iox`, `bufpool`, `slicex`, `mapx`, `set`, `enum`, `ptr`, `nullx`, `validate`, `sanitize`, `slugx`, `bytesize`, `decimal`, `money`, `filetype`, `errorsx`, `stringsx`

> Zero-forge-dependency leaves the entire framework builds on. clock+randx+id are the mandated seams (testable time, secure entropy, the exclusive ID source); ctxkey is the typed-context seam every request-scoped package adopts; structfields is the single sanctioned reflection helper that envconfig/bind/form depend on; typeconv is the scalar-coercion substrate beneath it. Internal edges: id->clock, nullx->ptr, slugx->sanitize, money->decimal, filetype->iox. Otherwise independent and parallelizable.

### P1 — crypto primitives

`subtlex`, `hashx`, `keyset`, `sign`, `secret`, `kdf`, `password`, `token`, `redact`

> Security building blocks layered on randx/clock. subtlex (constant-time) underpins sign/password/keyset; keyset feeds secret; sign+secret+randx+clock feed token; password is independent (x/crypto); redact is standalone. These precede cookie/csrf/session, secretsource, and all auth packages.

### P2 — async + data foundations

`backoff`, `retry`, `singleflight`, `parallel`, `ttlcache`, `circuitbreaker`, `drain`, `sqldb`, `dbreplica`, `sqltx`, `txcontext`, `scan`, `migrate`, `pagination`, `tenant`, `audit`, `optimistic`, `dataloader`, `kv`, `objectstore`

> Resilience primitives (backoff->retry, singleflight->ttlcache) and stdlib database/sql utilities, depending only on P0/P1. sqltx->txcontext->{tenant,dataloader,dbreplica}; sqldb->{dbreplica,migrate}; ttlcache->kv. Supervised async services and the lock package come later once they can also build on supervisor.

### P3 — web transport core

`middleware`, `negotiate`, `problem`, `conditional`, `requestid`, `realip`, `recover`, `reqlog`, `timeout`, `bodylimit`, `compress`, `cors`, `bind`, `static`, `upload`, `proxy`

> middleware defines the shared seam first; problem (the default responder) needs render (existing); requestid needs id+ctxkey+logger; bind needs structfields+typeconv+validate+problem; bodylimit needs iox; upload needs filetype. Everything composes with existing httpserver/hostrouter/render/htmx. Built before security-web, assets, idempotency, maintenance, and the async services that return Middleware/use problem.

### P4 — security-web + web extensions + async services/locks

`cookie`, `secheaders`, `csrf`, `iplist`, `webhookverify`, `idempotency`, `maintenance`, `assets`, `loadshed`, `ratelimit`, `quota`, `lock`, `watchdog`, `eventbus`, `jobqueue`, `scheduler`, `pubsub`, `outbox`

> cookie needs secret/sign/keyset; csrf needs cookie+middleware; idempotency needs middleware+problem+ttlcache+requestid; maintenance needs iplist; assets needs conditional+viewhelper; loadshed/ratelimit/quota need middleware+problem(+clock/tenant). Supervised async runtimes now have supervisor + P2 resilience + sqldb: lock (mutex+leader election), watchdog, eventbus, jobqueue, scheduler, pubsub; outbox needs lock for single-replica relay. captcha is deferred to P6 since it needs httpclient.

### P5 — auth + realtime

`session`, `sessionstore`, `authmw`, `totp`, `otp`, `recoverycodes`, `apikey`, `magiclink`, `invite`, `oauthclient`, `webauthn`, `rbac`, `abac`, `throttle`, `sse`, `broker`, `realtimebus`, `ws`, `wshub`, `longpoll`, `presence`, `streamhandler`

> Auth assembles crypto + cookie + session into login/MFA/policy; sessionstore backs session; throttle backs otp; authmw composes token/apikey/session; invite composes sign/token; oauthclient needs httpclient (built ahead in P6 transport-sibling — oauthclient is scheduled here but its only forward edge is httpclient which is foundational and can be pulled into P5 or oauthclient deferred a half-step). Realtime: sse needs render/htmx; broker is the local hub; realtimebus the backplane; ws/wshub/presence/streamhandler build on those + supervisor.

### P6 — integrations + i18n-ui

`httpclient`, `email`, `emailtemplate`, `sms`, `push`, `notify`, `webhook`, `geoip`, `captcha`, `llm`, `prompt`, `aistream`, `embeddings`, `structured`, `aiusage`, `tokenizer`, `i18n`, `i18nload`, `localematch`, `i18nctx`, `i18nplural`, `numfmt`, `datefmt`, `flash`, `form`, `formui`, `viewhelper`, `pageview`, `breadcrumb`, `cspnonce`

> httpclient (retry/backoff/circuitbreaker) is the transport under email->emailtemplate, webhook, geoip, captcha, oauthclient, and llm. notify fans over email/sms/push; aiusage needs money; the localization+UI view layer (i18n->i18nload/i18nplural/i18nctx; numfmt->money; form->structfields+typeconv+validate; pagination->pageview; secheaders->cspnonce; cookie->flash; viewhelper is consumed by assets). This is where most consumer-facing surface lives.

### P7 — ops + app/cli/test tooling

`dotenv`, `envconfig`, `profile`, `buildinfo`, `automaxprocs`, `health`, `readiness`, `pprof`, `metrics`, `tracing`, `featureflag`, `secretsource`, `logsample`, `auditlog`, `autocert`, `configwatch`, `runtimeinfo`, `cli`, `term`, `appmain`, `signalctx`, `httptest`, `golden`, `fixtures`, `fake`, `dbtest`, `factory`, `seed`

> Operational glue and bootstrap sit on top of nearly everything: envconfig reads env-tagged Configs via structfields/typeconv; health/readiness/metrics/tracing return Handlers/Middleware; readiness composes health+drain; secretsource composes secret/keyset/redact; logsample plugs into logger.WithHandler; appmain wires supervisor+logger+signalctx+automaxprocs; cli->signalctx, term->cli, seed->cli+factory. Test harnesses (httptest/golden/fixtures/fake/dbtest/factory/seed) depend on the concrete packages they exercise (email/migrate/cli), so they come last.

---

## Cut-lines

**Minimal "Core" forge (29 packages, all `core`-tier)** — enough to build a real API or htmx app end-to-end:

`clock`, `randx`, `id`, `ctxkey`, `typeconv`, `slicex`, `ptr`, `validate`, `sqldb`, `sqltx`, `migrate`, `backoff`, `retry`, `middleware`, `recover`, `requestid`, `bind`, `problem`, `session`, `sse`, `broker`, `email`, `i18n`, `localematch`, `flash`, `form`, `envconfig`, `health`, `readiness`

**Maximal (167 packages)** — covers B2B multi-tenant SaaS, public REST APIs, htmx web apps, realtime dashboards, background data pipelines, webhook hubs, and CLI tools.

---

## Anti-scope — what stays in consumer repos (and why)

The completeness critics were deliberately ruthless about scope. These are **non-goals**:

- **result (generic Result[T]/Option[T] error-monad)** — Go's idiom is multi-return (T, error) and the rubric demands no-magic explicit code. A Result monad imposes a non-idiomatic control style the whole framework would have to adopt to benefit. The real needs are already covered: ptr.Optional/nullx (absent values) and errorsx (error helpers). Deliberate non-goal.

- **shutdownhook (ordered LIFO cleanup-hook registry)** — supervisor already coordinates graceful lifecycle of Services and drain handles in-flight request draining. A separate hook registry duplicates supervisor's Run/shutdown ordering and splits one concern across two packages. Fold any LIFO-close need into supervisor.

- **deploy-orchestration (blue/green, canary, rolling-update)** — Blue/green, canary weighting, and rolling updates belong to the deployment platform (k8s/ECS/Swarm/Traefik/mesh), not the app binary. Forge provides the in-process hooks that make platform-driven deploys safe — readiness gating, drain, maintenance, idempotency — but the orchestration itself is infra/IaC.

- **billing / payments gateway abstraction** — Billing is provider-coupled business logic, not a reusable utility: a generic Gateway interface either leaks Stripe-specific objects (price IDs, proration, tax, entitlements) or hides them uselessly. Consumers use stripe-go directly and reuse forge webhookverify (Stripe webhook signatures) + httpclient (API calls) + quota (plan enforcement) + money. Plan/entitlement logic is consumer code.

- **usermanager (full account lifecycle: signup/login/verify/reset/profile)** — The consumer's domain layer assembled FROM forge primitives (password/session/token/magiclink/totp/otp/invite). Bundling it forces schema and mailer opinions, violating utility-only. Ship a recipe in examples/, not a package.

- **oidcprovider (being an OAuth2/OIDC authorization server)** — Issuing tokens to third-party clients entangles client registration, consent UI, scope governance, and JWKS rotation — a product, not a utility. Forge ships the client side (oauthclient) and primitives (token); run Ory Hydra/Keycloak for the IdP.

- **full JWT/JWS/JWE/JWKS library** — Spec-complete JWT (alg negotiation, JWE, JWKS) is large and historically a source of severe vulns (alg:none, key confusion). The opaque token package covers internal needs; oauthclient does the minimal id_token verify it requires. Third-party JWT interop justifies golang-jwt in the consumer repo.

- **vault / secretsmanager (live secrets-backend client)** — Requires heavy provider SDKs (Vault/AWS) and provider-specific auth. Forge consumes key material as plain Config (base64 env) via keyset, handles in-process hygiene via redact, and provides the hot-reload lifecycle via secretsource's Provider interface — the concrete backend client is consumer infrastructure supplied behind that Provider.

- **htmlsanitize (rich-HTML/UGC XSS sanitization)** — Correct HTML sanitization is hard and needs a substantial external dep (bluemonday). templ/render auto-escape covers output; sanitize handles plain-text escaping. If ever shipped, isolate as its own top-level package with the dep contained, never in pkg/\* primitives.

- **search / full-text search abstraction** — A useful FTS either reduces to dialect-specific tsvector/FTS5 SQL (belongs in the app's migrate/scan code) or wraps a heavy engine (bleve/meili/elastic). The leaky abstraction would not look native; document a Postgres tsvector example instead.

- **vectorstore / ANN index (pgvector/qdrant drivers)** — A real vector store means an external DB driver and index lifecycle — the consumer's data layer. The embeddings package covers brute-force in-memory similarity for small corpora; persistence beyond that belongs in the app.

- **agent runtime (multi-step tool-calling loop, planner, chains)** — Stateful, opinionated, fast-evolving orchestration — application logic, not a thin utility. Forge provides the primitives (llm tool-call types, structured decode, prompt templating); consumers or a dedicated agent framework build the loop.

- **promptregistry (versioned/remote prompt management with A/B + analytics + UI)** — Versioning, experimentation, analytics, and a control-plane UI are a product with storage and dashboards, far beyond a utility. The prompt package's embed.FS registry is the right-sized local need.

- **errtrack (standalone error/panic reporting facade)** — Redundant with the existing logger/sentry adapter (capture via logger.WithHandler) plus the recover middleware. A parallel reporting facade duplicates the established seam; route reports through logger.WithHandler -> logger/sentry.

- **generator (code/file scaffolding from templates)** — Generators are project/convention-specific: the value is entirely in consumer-owned templates. A generic text/template-over-fs.FS wrapper adds little over stdlib and would smuggle app conventions into the framework. Leave scaffolding to the consumer's /cmd/gen or a starter-template repo.

- **webtransport (HTTP/3 WebTransport over QUIC)** — Niche adoption, unsettled spec/browser support, and the only viable Go impl drags in a heavy fast-moving QUIC dependency (quic-go). Fails the unavoidable-and-heavily-used dependency bar; SSE+WebSocket cover current micro-SaaS realtime needs.

- **socketio (Socket.IO/Engine.IO protocol server)** — Heavyweight protocol tied to a non-stdlib JS client ecosystem. The ws+broker+wshub+realtimebus stack already gives rooms/fan-out with raw WebSockets and far less surface. Teams needing socket.io client compat use a dedicated library in the consumer repo.

- **shared service base package** — Per the v2 direction memory, a shared supervisor.Service base is deliberately deferred until 2–3 ports exist and real duplication appears — do NOT build it preemptively. Each new service (httpserver, jobqueue, scheduler, lock leader, etc.) mirrors the New+Config+Validate+WithConfig shape independently for now.

---

## Two decisions worth an explicit call

1. **`pkg/id` is mandated but does not exist yet.** README + CONTRIBUTING both require "all IDs via `pkg/id`", but there is no `pkg/` directory and no `id` package. That is the strongest signal for what to build first — the entire P0 base (`clock`, `randx`, `id`, `ctxkey`, `validate`) is the true foundation everything else needs.

2. **"Service base" tension with the roadmap.** The current direction is "supervisor done, service base next." The reconciliation pass argues the opposite — **defer** a shared `supervisor.Service` base until 2–3 services (`jobqueue`, `scheduler`, `lock`-leader) exist and real duplication appears, rather than abstracting preemptively. Each new service mirrors the `New`+`Config`+`Validate` shape independently until then.
