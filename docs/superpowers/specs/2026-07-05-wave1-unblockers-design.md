# Wave 1 Unblocker Bundle — Design Spec

> Date: 2026-07-05 · Status: awaiting approval · Ships as one PR.
> Unblocks the core-tier cut-line: `web/httpclient`, `auth/{session,apikey,otp}`,
> `ops/{envconfig,health}`, and the fleet error contract that `httpclient`
> surfaces.

Eight small API additions across seven already-shipped packages, plus one
doc-only change and `docs/packages.md` housekeeping. No new packages, no new
third-party dependencies. Every item is independent (touches a different
package) and follows forge's DNA: options never builders, `errors.Is`-matchable
sentinels, black-box tests only, crypto-secure by default.

This is the first spec of the "finish core packages" effort. The target is the
13 unshipped **core-tier** packages named in the packages.md "minimal-core
cut-line"; this bundle clears the Wave 1 unblocker work-items that gate the
rest. One spec per wave.

---

## Locked decisions (from brainstorming)

1. **`core/random`** stays uniformly **crypto-secure**. We adopt the good ideas
   from the old `github.com/dmitrymomot/random` — named charset constants and
   variadic charset composition — but reject its `math/rand` fast-path (a
   token-generation footgun), its silent length clamp, and its error-returning
   OTP. `String`/`DigitCode` use crypto/rand + rejection sampling and **panic**
   on RNG failure / misuse, consistent with the existing `Bytes`/`Int`.
2. **Weighted random selection is dropped** — not shipped in any form. Consumers
   who need lootbox/A-B weighting write it themselves; it does not belong in a
   crypto-secure primitive.
3. **`core/id.Prefixed`** is a **bound `Prefix` codec**
   (`NewPrefix(prefix, ...opts)` → `New`/`Parse`/`Is`/`Prefix`) with a
   **pluggable body generator** (`WithGenerator`, default `Short`; options are
   optional) plus **package-level static aliases**
   (`NewPrefixed`/`ParsePrefixed`/`IsPrefixed`) for the zero-config default.
   `Parse` returns the body **string** — a pluggable generator precludes a typed
   return; decode a default-Short body with `ParseShort`. Not a phantom-generic
   type (too much ceremony for forge's "no magic" lean). Reuses the existing
   `id.ErrMalformed`; adds `id.ErrWrongPrefix`.
4. **`web/htmx.SendComponent` is deferred** to the `realtime/sse` wave so it can
   reuse that writer. It stays as a re-tagged row in the packages.md table; it
   is **not** in this bundle.
5. **`web/problem`**: `*Problem` becomes an `error` (`Error` + `Is`); `Is`
   matches on the target's non-zero `Status`/`Code`. `Decode` is lenient
   (accepts `application/problem+json` or any JSON that looks like a problem),
   caps the body at 1 MiB, fills `Status` from the response, and does **not**
   close the body (the caller — `httpclient` — owns it).
6. **`ops/supervisor.NewContext` gains variadic options**; the no-option call is
   byte-for-byte the current behavior. `WithForceQuit` makes the **second**
   signal `os.Exit(130)`.
7. **`ops/logger` ships a recording test handler** (`Recorder`) as the seam
   owner's test double — there is no central fakes package.

## Design invariants (every symbol here)

- **No global mutable state**: no `init`, no package singletons, no registries.
  Every stateful object (`Prefix`, `Recorder`) is constructed and owned by the
  caller. `random`'s functions are pure over crypto/rand.
- **Options, never builders**: `ValidateFile`, `NewContext` take
  `...Option`-style variadics with a private `config`.
- **`errors.Is`-matchable single-line sentinels**: `id.ErrWrongPrefix`,
  `problem.ErrNotProblem`. Reuse existing sentinels where present
  (`id.ErrMalformed`, `request.Kind*`).
- **Public methods never return unexported types**: `Prefix.Parse` returns a
  `string` body; `Recorder.Records` returns `[]Record`.
- **Black-box tests only** (`package x_test`). White-box only where asserting
  unexported state is unavoidable (none expected here).

---

## 1. `core/random` — bias-free string & digit generators

### API

```go
// Charset constants for String. Each is an ASCII byte string.
const (
	Lowercase    = "abcdefghijklmnopqrstuvwxyz"
	Uppercase    = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	Digits       = "0123456789"
	Alphabetic   = Lowercase + Uppercase
	Alphanumeric = Alphabetic + Digits
	Symbols      = "~!@#$%^&*()-_+={}[]|\\;:\"<>,./?`"
)

// String returns a cryptographically secure random string of n characters drawn
// uniformly (bias-free, via crypto/rand rejection sampling) from the concatenation
// of charsets. With no charsets it defaults to Alphanumeric. Overlapping charsets
// are de-duplicated (first occurrence wins) so the distribution stays uniform over
// the distinct characters. The charset is treated as bytes; multi-byte UTF-8
// alphabets are not supported. Panics if n < 0 or the combined charset is empty.
// n == 0 returns "".
func String(n int, charsets ...string) string

// DigitCode returns an n-digit decimal string with leading zeros preserved
// (suitable for OTP / email verification codes). Equivalent to String(n, Digits).
// Panics if n <= 0.
func DigitCode(n int) string
```

### Semantics & rationale

- **Bias-free.** Let `k = len(dedup(charset))`. For `k <= 256`, read random bytes
  and reject any byte `>= 256 - (256 % k)` before taking `b % k` — the standard
  modulo-bias rejection. For `k > 256` (only reachable with a caller-supplied
  oversized alphabet) read the minimal number of bytes and apply the same
  rejection against `k`. Bytes are drawn from a refillable buffer, not one
  `crypto/rand.Int` per character.
- **Panic, not error.** A crypto/rand failure is an unrecoverable broken-OS-RNG
  condition (see existing `Bytes`); misuse (`n<0`, empty charset, `DigitCode`
  `n<=0`) is a programming bug. `random.Read` remains the error-returning escape
  hatch.
- **`DigitCode` panics on `n<=0`** (stricter than `String(0)` returning `""`)
  because a zero-length verification code is always a bug.

### Reference: rejection sampler (illustrative)

```go
func String(n int, charsets ...string) string {
	if n < 0 {
		panic("random: String n must be >= 0")
	}
	set := dedup(charsets) // join + first-occurrence dedup; default Alphanumeric
	k := len(set)
	if k == 0 {
		panic("random: String charset is empty")
	}
	out := make([]byte, n)
	// max is the largest multiple of k that fits the sample width; bytes at or
	// above it are rejected to remove modulo bias.
	if k <= 256 {
		max := 256 - (256 % k)
		buf := make([]byte, 0, n+n/4+8)
		for i := 0; i < n; {
			if len(buf) == 0 {
				buf = Bytes(cap(buf))
			}
			b := int(buf[len(buf)-1])
			buf = buf[:len(buf)-1]
			if b < max {
				out[i] = set[b%k]
				i++
			}
		}
		return string(out)
	}
	// k > 256: 2-byte sampling with the same rejection rule against k.
	// ... (uint16 draw, reject >= (65536 - 65536%k))
}
```

### Tests (black-box, `random_test`)

- Length: `len(String(n, …)) == n`; `String(0)==""`; `DigitCode(6)` is 6 chars,
  all `[0-9]`, leading zeros possible (assert over many draws that a `0`-leading
  code appears).
- Alphabet membership: every rune of `String(n, Uppercase, Digits)` is in the
  set; dedup verified by passing `Alphanumeric, Digits` and asserting the digit
  frequency is ~1/62, not ~2/72.
- Uniformity smoke test: chi-square-ish frequency check over a large sample for a
  small alphabet (e.g. `Digits`) within a loose tolerance.
- Panics: `String(-1)`, `String(3, "")`, `DigitCode(0)`, `DigitCode(-1)`.

---

## 2. `core/id` — bound `Prefix` codec (Stripe-style IDs)

### API

```go
// Prefix is an immutable, concurrency-safe Stripe-style ID codec: a human prefix
// joined to a generated body by "_". The zero value is unusable; construct with
// NewPrefix. Options are optional; the default body generator is Short.
type Prefix struct { /* prefix, joined ("prefix_"), gen func() string */ }

// PrefixOption configures NewPrefix.
type PrefixOption func(*Prefix)

// WithGenerator sets the body generator — the part after "<prefix>_". The default
// is Short (NewShort().String()); pass any func to mint ULID/UUID/random bodies,
// e.g. WithGenerator(func() string { return NewULID().String() }). A nil gen
// panics via NewPrefix.
func WithGenerator(gen func() string) PrefixOption

// NewPrefix returns a codec emitting IDs of the form "<prefix>_<body>". prefix
// must be non-empty and match [a-z0-9]+ (Stripe convention; "_" is the
// separator). Options are optional. Panics on an invalid prefix or a nil
// generator — both are boot-time programming errors.
func NewPrefix(prefix string, opts ...PrefixOption) Prefix

// New returns a fresh ID: "<prefix>_" + gen().
func (p Prefix) New() string

// Parse validates that s carries p's prefix and a non-empty body, returning the
// body (the part after "<prefix>_"). Because the body generator is pluggable, the
// body is returned opaque — for the default Short generator, decode it with
// ParseShort. A wrong/absent prefix returns ErrWrongPrefix; an empty body returns
// ErrMalformed (both errors.Is-matchable).
func (p Prefix) Parse(s string) (string, error)

// Is reports whether s carries p's prefix and a non-empty body. It validates the
// prefix and body presence only, not the body's internal format.
func (p Prefix) Is(s string) bool

// Prefix returns the bound prefix (for logging / diagnostics).
func (p Prefix) Prefix() string
```

### Static aliases (default Short generator, no codec to hold)

```go
// NewPrefixed returns a fresh "<prefix>_<short>" ID. Equivalent to
// NewPrefix(prefix).New(); panics on an invalid prefix.
func NewPrefixed(prefix string) string

// ParsePrefixed validates prefix on s and returns the body. Equivalent to
// NewPrefix(prefix).Parse(s).
func ParsePrefixed(prefix, s string) (string, error)

// IsPrefixed reports whether s carries prefix with a non-empty body. Equivalent
// to NewPrefix(prefix).Is(s).
func IsPrefixed(prefix, s string) bool
```

### Semantics & rationale

- **Pluggable body, opaque parse.** The body generator is `func() string`
  (default `NewShort().String()`), so `New` composes `prefix + "_" + gen()`.
  Because the generator is pluggable, `Parse` returns the body **string** rather
  than a typed `Short` — the honest signature. Default-Short consumers recover the
  typed value with `id.ParseShort(body)`; ULID/UUID/random-bodied codecs decode
  with their own parser. (This supersedes the earlier "Parse returns Short"
  decision, which pluggable generators make impossible.)
- **Separator `_`** fixed. `Parse` requires the exact `prefix + "_"` head; the
  remainder is the body. Empty body → `ErrMalformed`; wrong/absent prefix (no
  matching head) → `ErrWrongPrefix`. Add
  `id.ErrWrongPrefix = errors.New("id: wrong prefix")`; reuse `id.ErrMalformed`.
- **Prefix validation** is a small `[a-z0-9]` byte-loop (no `regexp` dependency).
  An invalid prefix or nil generator **panics** in `NewPrefix` (constants, caught
  at boot). The static aliases delegate to `NewPrefix`, so they validate
  identically.
- **Static aliases** cover the zero-config default (Short body); reach for the
  `Prefix` codec when you want a custom generator or a hot path that binds the
  prefix once. Options are deliberately **not** exposed on the static aliases —
  custom generation implies you hold a reusable codec.
- **Immutable value**, safe to share across goroutines; the default generator uses
  the package `defaultGen` like `NewShort()`.

### Tests (`id_test`)

- Round-trip (default): `p := NewPrefix("user"); s := p.New()`;
  `strings.HasPrefix(s, "user_")`; `body, err := p.Parse(s)` with `err==nil`;
  `ParseShort(body)` succeeds.
- Custom generator: `NewPrefix("tok", WithGenerator(func() string { return
  NewULID().String() }))` → `New()` has a `tok_` head; `Parse` returns the ULID
  body; `ParseULID(body)` succeeds.
- `Is(p.New()) == true`; `Is("org_"+body) == false`.
- Wrong prefix → `errors.Is(err, ErrWrongPrefix)`; empty body (`"user_"`) →
  `errors.Is(err, ErrMalformed)`; no-separator / empty input → `ErrWrongPrefix`
  (not a panic).
- Static aliases mirror the codec on the default generator
  (`NewPrefixed`/`ParsePrefixed`/`IsPrefixed`).
- Panics: `NewPrefix("")`, `NewPrefix("User")`, `NewPrefix("a_b")`,
  `NewPrefix("x", WithGenerator(nil))`, `NewPrefixed("")`.

---

## 3. `web/problem` — `Decode` + `*Problem` as an error

### API

```go
// Error implements error with a single-line summary. The response body written by
// the responders is unaffected — this is for logs and errors.Is chains.
func (p *Problem) Error() string // "problem: 429 Too Many Requests [rate_limited]"

// Is matches by target's non-zero fields: a *Problem target matches p when
// (target.Status == 0 || target.Status == p.Status) &&
// (target.Code   == "" || target.Code   == p.Code).
// So errors.Is(err, &Problem{Code:"rate_limited"}) matches by code,
// errors.Is(err, &Problem{Status:429}) matches by status, and &Problem{} matches
// any Problem. A non-*Problem target never matches.
func (p *Problem) Is(target error) bool

// Decode reads an RFC 9457 problem+json response body into a *Problem. It caps the
// body at 1 MiB (io.LimitReader), fills Status from resp.StatusCode when the body
// omits it, and DOES NOT close resp.Body (the caller owns it). A body that is not
// a problem document returns ErrNotProblem.
func Decode(resp *http.Response) (*Problem, error)

var ErrNotProblem = errors.New("problem: not a problem+json response")
```

### Semantics & rationale

- **`Error()`**: `fmt.Sprintf("problem: %d %s", p.Status, p.Title)`, appending
  `" ["+p.Code+"]"` when `Code != ""`. Single line, no field dump.
- **`Is`**: enables the fleet error contract — `httpclient` returns a decoded
  `*Problem` and callers match with `errors.Is` on status and/or machine code
  without string comparison. Both-zero target matching "any Problem" is
  intentional and documented.
- **`Decode` lenience**: treat the body as a problem when the content type is
  `application/problem+json` **or** the JSON unmarshals with a non-zero `Status`
  or a non-empty `Code`/`Title`/`Type`. Otherwise `ErrNotProblem`. This tolerates
  servers that send `application/json` for errors.
- **Body ownership**: `Decode` drains up to the cap but never closes — parity with
  stdlib decoders and required because `httpclient` may inspect/close the body
  itself. A `nil` `resp` returns `ErrNotProblem` (defensive, not a panic).
- **Fixed 1 MiB cap** (no option in Wave 1); a `WithMaxBytes` is a plausible later
  addition, deferred.

### Tests (`problem_test`)

- `errors.Is`: build `&Problem{Status:429, Code:"rate_limited"}`; assert matches
  for `{Code:"rate_limited"}`, `{Status:429}`, `{}`; non-match for
  `{Status:400}`, `{Code:"other"}`, and a non-Problem error.
- `Error()` format with and without `Code`.
- `Decode` via `httptest`: a real `application/problem+json` body round-trips
  (fields + status); an `application/json` problem-shaped body decodes; a plain
  `{"ok":true}` / HTML body → `ErrNotProblem`; a body omitting `status` inherits
  `resp.StatusCode`; an over-cap body is truncated safely (still decodes or errors
  without OOM); body is left open (assert readable/closeable by the test after).

---

## 4. `web/request` — `ValidateFile` + `Accept`

### API

```go
// FileOption configures ValidateFile.
type FileOption func(*fileConfig)

// WithAllowedMIME restricts uploads to these sniffed MIME types (e.g.
// "image/png", "application/pdf"). With no allowlist, the MIME is not checked.
func WithAllowedMIME(mimes ...string) FileOption

// WithMaxFileSize rejects uploads whose declared size exceeds n bytes. With no
// limit, size is not checked.
func WithMaxFileSize(n int64) FileOption

// ValidateFile validates an uploaded file by MAGIC BYTES (core/filetype sniff),
// deliberately ignoring the client-declared Content-Type, plus an optional size
// cap. Returns a *request.Error: KindTooLarge (413) for oversize,
// KindUnsupportedMediaType (415) for a disallowed/undetectable type, KindMissing
// for a nil header. nil on success. Callers with multiple files loop over Files().
func ValidateFile(fh *multipart.FileHeader, opts ...FileOption) error

// Accept reports whether the request's Accept header admits mediaType, honoring
// "*/*", "type/*", and explicit "q=0" rejections. An absent/empty Accept header
// admits everything (returns true), per RFC 9110.
func Accept(r *http.Request, mediaType string) bool

// AcceptsJSON is Accept(r, "application/json").
func AcceptsJSON(r *http.Request) bool
```

### Semantics & rationale

- **`ValidateFile` sniffs, never trusts the header.** Size check first (cheap,
  from `fh.Size`), then `f, _ := fh.Open()` / `defer f.Close()` /
  `filetype.DetectReader(f)`; compare the detected `Type.MIME` against the
  allowlist. This defeats a spoofed `Content-Type` on the multipart part.
- **Error mapping reuses the existing `Kind` enum** — no new Kind. `Source` is
  `SourceForm`, `Key` is `fh.Filename` for a useful message.
- **`Accept` parsing**: split on `,`, parse each media range with an optional
  `;q=` (default `1.0`); a range matches `mediaType` by exact, `type/*`, or `*/*`.
  If the most specific matching range has `q > 0` → true; `q == 0` → false; no
  match → false; no header → true.

### Tests (`request_test`)

- `ValidateFile`: multipart fixtures with real PNG/PDF magic bytes; a `.png`
  filename whose bytes are actually a script → `KindUnsupportedMediaType`
  (spoof defeated); oversize vs `WithMaxFileSize` → `KindTooLarge`; empty
  allowlist skips MIME check; nil header → `KindMissing`; `StatusCode(err)` maps
  to 413/415/400 correctly.
- `Accept`: table over `Accept: application/json`, `*/*`, `text/*`,
  `application/json;q=0`, absent header, multi-range with q-values; `AcceptsJSON`
  parallels.

---

## 5. `web/middleware` — `When` / `Skip` combinators

### API

```go
// When returns a Middleware that applies mw only to requests for which pred
// returns true; other requests pass to the next handler untouched.
func When(pred func(*http.Request) bool, mw Middleware) Middleware

// Skip is the inverse of When: mw applies unless pred returns true.
func Skip(pred func(*http.Request) bool, mw Middleware) Middleware
```

### Semantics

- Build the wrapped handler **once** per `next` (so a stateful `mw` is
  constructed a single time), then branch per request:

```go
func When(pred func(*http.Request) bool, mw Middleware) Middleware {
	return func(next http.Handler) http.Handler {
		wrapped := mw(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if pred(r) {
				wrapped.ServeHTTP(w, r)
			} else {
				next.ServeHTTP(w, r)
			}
		})
	}
}

func Skip(pred func(*http.Request) bool, mw Middleware) Middleware {
	return When(func(r *http.Request) bool { return !pred(r) }, mw)
}
```

- Compose with the existing `Chain`. A `nil` `pred` or `mw` is a programming
  error; document that both are required (guard-panic acceptable, or let a nil
  `mw` panic naturally — decide in TDD; prefer an explicit panic message).

### Tests (`middleware_test`)

- `When` applies mw for matching paths, skips otherwise (assert a header the mw
  sets is present/absent).
- `Skip` is the exact inverse over the same predicate.
- `mw` is built once (a construction counter increments a single time across many
  requests).

---

## 6. `ops/supervisor` — `WithForceQuit`

### API

```go
// ContextOption configures NewContext.
type ContextOption func(*contextConfig)

// WithForceQuit makes the SECOND SIGINT/SIGTERM force an immediate os.Exit(130)
// instead of being ignored. The first signal still cancels the returned context
// for graceful drain; the second is the impatient-operator escape hatch. os.Exit
// bypasses deferred cleanup by design.
func WithForceQuit() ContextOption

// NewContext returns a context cancelled on the first SIGINT or SIGTERM. Call the
// returned CancelFunc (typically deferred in main) to release the signal handler.
func NewContext(opts ...ContextOption) (context.Context, context.CancelFunc)
```

### Semantics & rationale

- **Back-compat**: with no options, `NewContext()` returns exactly today's
  `signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)` —
  no behavior change.
- **With `WithForceQuit`**: use an explicit handler instead —
  `context.WithCancel(Background)`, a buffered `signal.Notify` channel, and a
  goroutine that cancels on the first signal and `os.Exit(130)` on the second. The
  returned `CancelFunc` calls `signal.Stop` + stops the goroutine so nothing
  leaks; guard against a double-exit / double-cancel.
- Exit code **130** (128 + SIGINT), the conventional forced-interrupt code.

### Reference (force-quit path)

```go
func NewContext(opts ...ContextOption) (context.Context, context.CancelFunc) {
	var cfg contextConfig
	for _, o := range opts {
		o(&cfg)
	}
	if !cfg.forceQuit {
		return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	}
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan os.Signal, 2)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-ch          // first signal → graceful
		cancel()
		<-ch          // second signal → impatient
		os.Exit(130)
	}()
	stop := func() {
		signal.Stop(ch)
		cancel()
	}
	return ctx, stop
}
```

### Tests (`supervisor_test`)

- No-option `NewContext()` cancels on a delivered SIGTERM (existing behavior
  preserved).
- `WithForceQuit`: first signal cancels the context (deterministic).
- Second-signal `os.Exit(130)`: the standard re-exec subprocess pattern — the
  test binary re-invokes itself with an env flag, sends two signals, and asserts
  exit code 130. Documented as the one os.Exit path.

---

## 7. `ops/logger` — recording test handler

### API

```go
// Record is one captured log record with attributes flattened to dotted keys
// (a WithGroup("http") + Int("status",…) attr becomes "http.status").
type Record struct {
	Time    time.Time
	Level   slog.Level
	Message string
	Attrs   map[string]any
}

// Recorder is a concurrency-safe slog.Handler sink capturing records for test
// assertions. It is the seam owner's test double — there is no central fakes
// package.
type Recorder struct { /* mu sync.Mutex; records []Record */ }

// NewRecorder returns a *slog.Logger writing into the returned *Recorder.
func NewRecorder() (*slog.Logger, *Recorder)

// Records returns a snapshot copy of the captured records.
func (r *Recorder) Records() []Record

// Len returns the number of captured records.
func (r *Recorder) Len() int

// Reset discards all captured records.
func (r *Recorder) Reset()

// Contains reports whether any captured record has the given level and message.
func (r *Recorder) Contains(level slog.Level, msg string) bool
```

### Semantics & rationale

- The internal handler implements `slog.Handler`; `WithAttrs`/`WithGroup` return
  child handlers carrying an accumulated dotted group prefix + pre-bound attrs,
  all appending to the one shared `*Recorder` under its mutex. `Enabled` is always
  true (record every level; asserts filter).
- Attribute values are resolved (`attr.Value.Resolve()`) and stored as `any` under
  dotted keys, so `LogValuer`s and groups are assertable by key.
- `Records()` returns a copy so tests can't mutate internal state; all reads/writes
  are mutex-guarded for logging from goroutines.

### Tests (`logger_test`)

- Emitting at multiple levels captures each with correct `Level`/`Message`.
- `With(slog.Group("http", "status", 200))` produces `Attrs["http.status"] == 200`.
- `Contains(slog.LevelError, "boom")` true/false cases.
- `Reset()` clears; `Len()` tracks.
- Race test: concurrent logging from N goroutines under `-race` with a final
  count assertion.

---

## 8. `web/hostrouter` — DNS-rebinding doc note

Doc-only. Add to `doc.go` (package overview) and the `New` / `WithFallback`
comments:

> Unmatched hosts fall through to the fallback (`http.NotFoundHandler()`, 404, by
> default). This default-deny is a DNS-rebinding defense: a handler is reachable
> only for explicitly registered `Host` values, so an attacker who points their
> own domain at your IP reaches the fallback, not a real handler. Do not install a
> catch-all fallback that serves sensitive handlers.

No code change; no test change (an existing unmatched-host → 404 test already
covers the behavior).

---

## Housekeeping — `docs/packages.md` (same PR)

Edit the **Shipped-package work items** table:

- Remove the three already-shipped resilience rows (`circuitbreaker`, `retry`,
  `cache`) — this clears the pending post-merge cleanup from the prior bundle.
- Remove each row landed by this bundle: `core/random`, `core/id`, `web/request`,
  `web/middleware`, `web/problem`, `ops/supervisor`, `ops/logger`,
  `web/hostrouter`.
- Re-tag the `web/htmx` row to record the deferral, e.g.
  `web/htmx | SSE SendComponent bridge — deferred to the realtime/sse wave`.

The table should end containing only the deferred `htmx` row.

---

## Verification (per item, then bundle)

- Black-box `X_test` packages only.
- `just fmt ./<changed-pkg>/...` (package-path form — the single-file form trips a
  spurious betteralign "undefined").
- `just lint` clean.
- `go test ./... -race` green (includes the uniformity, spoof-file, force-quit
  subprocess, and concurrent-logger tests).
- Run `modernize` (Go 1.26 idioms: `new(expr)`, `errors.AsType`).

## Implementation notes

- All eight items are **independent** (distinct packages, no cross-item deps), so
  implementation parallelizes cleanly — one focused TDD change per item. The
  packages.md housekeeping is done last as a single writer.
- TDD each item (red → green → refactor); no shared state to sequence.

## Out of scope / explicitly deferred

- `web/htmx.SendComponent` → `realtime/sse` wave.
- Weighted random selection → dropped entirely, not shipped.
- `core/random` math/rand fast-path, silent length clamp, error-returning
  generators → rejected (crypto-secure, panic-on-misuse instead).
- `problem.Decode` `WithMaxBytes` option → deferred to first concrete demand
  (fixed 1 MiB cap for now). (`id.Prefix.WithGenerator` is now **in** scope.)
- The four Wave 1 **packages** (`httpclient`, `ratelimit`, `envconfig`,
  `health`) → their own specs, after these unblockers land.
