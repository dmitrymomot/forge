# gaming/rng Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship `gaming/rng` — deterministic game-mechanics randomness (lootbox tables, dice, cards, slot reels) over a frozen `rng/v1` derivation spec, with a casual CSPRNG entry point and a provably-fair seed-chain Manager — plus the `gaming/rng/pgstore` pgx driver.

**Architecture:** Three layers in one package: a pure `Stream` (HMAC-SHA256 block expansion of serverSeed/clientSeed/nonce, rejection-sampled ints, 53-bit floats, spec'd Fisher–Yates), pure mechanics on top (`Table[T]` with audit `Version()` and pity, `Deal`, cards), and a stateful `Manager` (commit–reveal lifecycle, atomic nonce consumption) behind a 5-method `Store` seam with an in-package memory store and an isolated Postgres driver.

**Tech Stack:** Go stdlib (`crypto/hmac`, `crypto/sha256`), forge `core/random`, `core/clock`, `core/id`; driver: `pgx` + `data/migration`; tests: testify, fuzzing, `FORGE_TEST_POSTGRES_DSN`-gated Postgres integration.

**Spec:** `docs/superpowers/specs/2026-07-13-gaming-rng-design.md` — the derivation algorithm section is NORMATIVE; implementation must match it byte-for-byte.

## Global Constraints

- Module `github.com/dmitrymomot/forge`; new package `gaming/rng`, driver `gaming/rng/pgstore`. Work ONLY in the current branch.
- Black-box tests only: `package rng_test` / `package pgstore_test`; testify `require`/`assert`.
- After changing files run `just fmt ./gaming/...`; at the end of every task run `just lint` (must pass clean) and `just test ./gaming/...`.
- `rng/v1` is frozen: once Task 1's golden vectors are committed, any change to derivation output is a bug — never "fix" a golden vector.
- Errors are single-line `errors.Is`-matchable sentinels in `errors.go`.
- No manual line-wrapping in commit bodies. No Claude attribution anywhere (no "Generated with", no Co-Authored-By trailers).
- Hot-path rules (docs/design.md §Performance): zero allocs per Stream draw after construction (verified in Task 9); no `fmt` on hot paths (`strconv.AppendUint`).
- Go 1.26 idioms: `for range n` loops (not C-style), `b.Loop()` in benchmarks — `just lint` runs modernize and will fail otherwise.

---

### Task 1: Stream derivation core (`rng/v1`)

**Files:**
- Create: `gaming/rng/doc.go` (minimal; expanded in Task 8)
- Create: `gaming/rng/errors.go`
- Create: `gaming/rng/stream.go`
- Test: `gaming/rng/stream_test.go`, `gaming/rng/fuzz_test.go`

**Interfaces:**
- Consumes: `core/random` (`random.Bytes(n int) []byte`, `random.String(n int, charsets ...string) string`).
- Produces (later tasks rely on these exact names): `const Algorithm = "rng/v1"`, `New(serverSeed []byte, clientSeed string, nonce uint64) (*Stream, error)`, `Casual() *Stream`, `(*Stream).Uint64() uint64`, `IntN(n int) int`, `Ints(count, n int) []int`, `Float64() float64`, `Roll(sides int) int`, `Perm(n int) []int`, `Shuffle(n int, swap func(i, j int))`, `Commitment(serverSeed []byte) string`, `VerifyCommitment(serverSeed []byte, commitment string) bool`, sentinels `ErrInvalidSeed`, `ErrInvalidClientSeed`, unexported helpers `validClientSeed(string) bool`, consts `serverSeedLen = 32`, `clientSeedAlphabet`, `defaultClientSeedLen = 16`.

- [ ] **Step 1: Write the failing tests**

`gaming/rng/stream_test.go`:

```go
package rng_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/gaming/rng"
)

// testSeed is the fixed golden server seed: bytes 0x00..0x1f.
func testSeed() []byte {
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = byte(i)
	}
	return seed
}

// refBytes reimplements the rng/v1 block expansion independently of the
// package: block_i = HMAC-SHA256(serverSeed, clientSeed:nonce:i),
// concatenated until total bytes are produced.
func refBytes(t *testing.T, server []byte, client string, nonce uint64, total int) []byte {
	t.Helper()
	out := make([]byte, 0, total+sha256.Size)
	for i := uint64(0); len(out) < total; i++ {
		mac := hmac.New(sha256.New, server)
		fmt.Fprintf(mac, "%s:%d:%d", client, nonce, i)
		out = mac.Sum(out)
	}
	return out[:total]
}

func TestNew_Validation(t *testing.T) {
	t.Parallel()
	_, err := rng.New(make([]byte, 31), "alice", 0)
	assert.ErrorIs(t, err, rng.ErrInvalidSeed)
	_, err = rng.New(make([]byte, 33), "alice", 0)
	assert.ErrorIs(t, err, rng.ErrInvalidSeed)
	_, err = rng.New(testSeed(), "", 0)
	assert.ErrorIs(t, err, rng.ErrInvalidClientSeed)
	_, err = rng.New(testSeed(), strings.Repeat("a", 65), 0)
	assert.ErrorIs(t, err, rng.ErrInvalidClientSeed)
	_, err = rng.New(testSeed(), "has space", 0)
	assert.ErrorIs(t, err, rng.ErrInvalidClientSeed)
	_, err = rng.New(testSeed(), "has:colon", 0)
	assert.ErrorIs(t, err, rng.ErrInvalidClientSeed)

	_, err = rng.New(testSeed(), "a", 0)
	assert.NoError(t, err)
	_, err = rng.New(testSeed(), strings.Repeat("Z", 64), 0)
	assert.NoError(t, err)
	_, err = rng.New(testSeed(), "Ab9_-", 42)
	assert.NoError(t, err)
}

func TestStream_MatchesReference(t *testing.T) {
	t.Parallel()
	s, err := rng.New(testSeed(), "alice", 7)
	require.NoError(t, err)
	// 10 Uint64s = 80 bytes = 2.5 blocks: crosses block boundaries.
	want := refBytes(t, testSeed(), "alice", 7, 80)
	for i := range 10 {
		got := s.Uint64()
		exp := binary.BigEndian.Uint64(want[i*8:])
		assert.Equal(t, exp, got, "uint64 #%d", i)
	}
}

func TestStream_Deterministic(t *testing.T) {
	t.Parallel()
	a, err := rng.New(testSeed(), "bob", 3)
	require.NoError(t, err)
	b, err := rng.New(testSeed(), "bob", 3)
	require.NoError(t, err)
	for range 100 {
		assert.Equal(t, a.IntN(1000), b.IntN(1000))
	}
	// Different nonce diverges.
	c, err := rng.New(testSeed(), "bob", 4)
	require.NoError(t, err)
	d, err := rng.New(testSeed(), "bob", 3)
	require.NoError(t, err)
	diverged := false
	for range 10 {
		if c.Uint64() != d.Uint64() {
			diverged = true
		}
	}
	assert.True(t, diverged)
}

// Golden vectors freeze rng/v1 forever. Generated once from the reference
// implementation (see plan Task 1 Step 4); NEVER regenerate — a mismatch
// after a refactor means the refactor broke the spec.
func TestGoldenVectors(t *testing.T) {
	t.Parallel()
	s, err := rng.New(testSeed(), "alice", 7)
	require.NoError(t, err)
	const (
		goldenUint64  = uint64(0)  // PASTE-FROM-GENERATOR
		goldenIntN100 = 0          // PASTE-FROM-GENERATOR
		goldenFloat64 = float64(0) // PASTE-FROM-GENERATOR
	)
	assert.Equal(t, goldenUint64, s.Uint64())
	assert.Equal(t, goldenIntN100, s.IntN(100))
	assert.Equal(t, goldenFloat64, s.Float64()) //nolint:testifylint // exact bit equality is the point
}

func TestIntN_RangeAndPanic(t *testing.T) {
	t.Parallel()
	s, err := rng.New(testSeed(), "alice", 0)
	require.NoError(t, err)
	for range 1000 {
		v := s.IntN(7)
		assert.GreaterOrEqual(t, v, 0)
		assert.Less(t, v, 7)
	}
	assert.Panics(t, func() { s.IntN(0) })
	assert.Panics(t, func() { s.IntN(-1) })
	// n == 1 always 0 and terminates.
	assert.Equal(t, 0, s.IntN(1))
}

func TestFloat64_Range(t *testing.T) {
	t.Parallel()
	s, err := rng.New(testSeed(), "alice", 1)
	require.NoError(t, err)
	for range 1000 {
		f := s.Float64()
		assert.GreaterOrEqual(t, f, 0.0)
		assert.Less(t, f, 1.0)
	}
}

func TestRoll(t *testing.T) {
	t.Parallel()
	s, err := rng.New(testSeed(), "alice", 2)
	require.NoError(t, err)
	for range 100 {
		v := s.Roll(6)
		assert.GreaterOrEqual(t, v, 1)
		assert.LessOrEqual(t, v, 6)
	}
}

func TestInts(t *testing.T) {
	t.Parallel()
	s, err := rng.New(testSeed(), "alice", 3)
	require.NoError(t, err)
	vals := s.Ints(5, 50)
	require.Len(t, vals, 5)
	for _, v := range vals {
		assert.GreaterOrEqual(t, v, 0)
		assert.Less(t, v, 50)
	}
	// Ints is exactly count sequential IntN draws.
	a, _ := rng.New(testSeed(), "alice", 3)
	for i := range 5 {
		assert.Equal(t, vals[i], a.IntN(50))
	}
	assert.Panics(t, func() { s.Ints(-1, 5) })
	assert.Empty(t, s.Ints(0, 5))
}

func TestShuffle_IsPermutationAndSpecOrder(t *testing.T) {
	t.Parallel()
	s, err := rng.New(testSeed(), "alice", 4)
	require.NoError(t, err)
	vals := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}
	s.Shuffle(len(vals), func(i, j int) { vals[i], vals[j] = vals[j], vals[i] })
	seen := make(map[int]bool)
	for _, v := range vals {
		seen[v] = true
	}
	assert.Len(t, seen, 10)

	// Reference Fisher–Yates in the spec'd order over an equal stream.
	ref, err := rng.New(testSeed(), "alice", 4)
	require.NoError(t, err)
	want := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}
	for i := len(want) - 1; i > 0; i-- {
		j := ref.IntN(i + 1)
		want[i], want[j] = want[j], want[i]
	}
	assert.Equal(t, want, vals)
}

func TestPerm_MatchesShuffle(t *testing.T) {
	t.Parallel()
	s, err := rng.New(testSeed(), "alice", 5)
	require.NoError(t, err)
	p := s.Perm(8)
	ref, err := rng.New(testSeed(), "alice", 5)
	require.NoError(t, err)
	want := []int{0, 1, 2, 3, 4, 5, 6, 7}
	ref.Shuffle(8, func(i, j int) { want[i], want[j] = want[j], want[i] })
	assert.Equal(t, want, p)
}

func TestCasual_IndependentStreams(t *testing.T) {
	t.Parallel()
	a, b := rng.Casual(), rng.Casual()
	assert.NotEqual(t, a.Uint64(), b.Uint64()) // 2^-64 false-positive odds
}

func TestIntN_Uniform(t *testing.T) {
	t.Parallel()
	s, err := rng.New(testSeed(), "stats", 0)
	require.NoError(t, err)
	const buckets, draws = 6, 60000
	var counts [buckets]int
	for range draws {
		counts[s.IntN(buckets)]++
	}
	for i, c := range counts {
		assert.InDelta(t, draws/buckets, c, 500, "bucket %d", i) // ±5%
	}
}

func TestCommitment(t *testing.T) {
	t.Parallel()
	seed := testSeed()
	c := rng.Commitment(seed)
	assert.Len(t, c, 64) // hex SHA-256
	assert.Equal(t, strings.ToLower(c), c)
	assert.True(t, rng.VerifyCommitment(seed, c))
	assert.False(t, rng.VerifyCommitment(seed, "deadbeef"))
	other := testSeed()
	other[0] ^= 1
	assert.False(t, rng.VerifyCommitment(other, c))
}
```

`gaming/rng/fuzz_test.go`:

```go
package rng_test

import (
	"testing"

	"github.com/dmitrymomot/forge/gaming/rng"
)

func FuzzStream(f *testing.F) {
	f.Add([]byte("0123456789abcdef0123456789abcdef"), "alice", uint64(0), 6)
	f.Add([]byte("0123456789abcdef0123456789abcdef"), "Z_-9", uint64(1<<63), 1)
	f.Fuzz(func(t *testing.T, seed []byte, client string, nonce uint64, n int) {
		s, err := rng.New(seed, client, nonce)
		if err != nil {
			return // invalid inputs must error, never panic
		}
		if n <= 0 {
			n = 1
		}
		if v := s.IntN(n); v < 0 || v >= n {
			t.Fatalf("IntN(%d) = %d out of range", n, v)
		}
		if f64 := s.Float64(); f64 < 0 || f64 >= 1 {
			t.Fatalf("Float64() = %v out of range", f64)
		}
		s.Shuffle(8, func(i, j int) {})
	})
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./gaming/rng/`
Expected: FAIL — `no Go files in .../gaming/rng` (or undefined symbols once files exist).

- [ ] **Step 3: Write the implementation**

`gaming/rng/doc.go` (minimal now; Task 8 replaces it):

```go
// Package rng provides deterministic random outcomes for game mechanics
// over the frozen rng/v1 derivation spec: a casual CSPRNG entry point and
// a provably-fair commit-reveal seed chain. See doc comments on New,
// Casual, Table, and Manager.
package rng
```

`gaming/rng/errors.go`:

```go
package rng

import "errors"

var (
	// ErrInvalidSeed reports a server seed that is not exactly 32 bytes.
	ErrInvalidSeed = errors.New("rng: server seed must be exactly 32 bytes")

	// ErrInvalidClientSeed reports a client seed outside 1-64 chars of [A-Za-z0-9_-].
	ErrInvalidClientSeed = errors.New("rng: client seed must be 1-64 chars of [A-Za-z0-9_-]")
)
```

`gaming/rng/stream.go`:

```go
package rng

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"hash"
	"math"
	"strconv"

	"github.com/dmitrymomot/forge/core/random"
)

// Algorithm identifies the frozen rng/v1 derivation spec. It is stamped
// into every Proof and stored on every seed record; a future algorithm
// change ships as rng/v2 alongside so old outcomes stay verifiable.
const Algorithm = "rng/v1"

const (
	serverSeedLen        = 32
	clientSeedMaxLen     = 64
	clientSeedAlphabet   = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_-"
	defaultClientSeedLen = 16
)

// Stream is a deterministic random stream derived from (serverSeed,
// clientSeed, nonce) per rng/v1: 32-byte blocks
// HMAC-SHA256(serverSeed, clientSeed:nonce:blockIndex) consumed
// sequentially. Draws are ordered — a verifier replays them in the same
// order. Not safe for concurrent use.
type Stream struct {
	mac     hash.Hash // HMAC-SHA256 keyed with the server seed
	prefix  []byte    // clientSeed ':' nonce ':' — block index appended per refill
	scratch []byte    // block-index formatting buffer
	next    uint64    // next block index
	off     int       // bytes of block already consumed
	block   [sha256.Size]byte
}

// New returns the deterministic Stream for (serverSeed, clientSeed,
// nonce). The server seed must be exactly 32 bytes; the client seed must
// be 1-64 chars of [A-Za-z0-9_-].
func New(serverSeed []byte, clientSeed string, nonce uint64) (*Stream, error) {
	if len(serverSeed) != serverSeedLen {
		return nil, ErrInvalidSeed
	}
	if !validClientSeed(clientSeed) {
		return nil, ErrInvalidClientSeed
	}
	prefix := make([]byte, 0, len(clientSeed)+22)
	prefix = append(prefix, clientSeed...)
	prefix = append(prefix, ':')
	prefix = strconv.AppendUint(prefix, nonce, 10)
	prefix = append(prefix, ':')
	return &Stream{
		mac:     hmac.New(sha256.New, serverSeed),
		prefix:  prefix,
		scratch: make([]byte, 0, 20),
		off:     sha256.Size, // force a refill on first read
	}, nil
}

// Casual returns a Stream seeded from crypto/rand — the same math as New
// with no verifiability ceremony. Each call yields an independent stream.
func Casual() *Stream {
	s, err := New(random.Bytes(serverSeedLen), random.String(defaultClientSeedLen, clientSeedAlphabet), 0)
	if err != nil {
		panic("rng: unreachable: generated inputs are always valid: " + err.Error())
	}
	return s
}

func validClientSeed(s string) bool {
	if len(s) < 1 || len(s) > clientSeedMaxLen {
		return false
	}
	for i := range len(s) {
		c := s[i]
		switch {
		case 'A' <= c && c <= 'Z', 'a' <= c && c <= 'z', '0' <= c && c <= '9', c == '_', c == '-':
		default:
			return false
		}
	}
	return true
}

func (s *Stream) refill() {
	s.mac.Reset()
	s.mac.Write(s.prefix)
	s.scratch = strconv.AppendUint(s.scratch[:0], s.next, 10)
	s.mac.Write(s.scratch)
	s.mac.Sum(s.block[:0])
	s.next++
	s.off = 0
}

func (s *Stream) read(p []byte) {
	for len(p) > 0 {
		if s.off == sha256.Size {
			s.refill()
		}
		n := copy(p, s.block[s.off:])
		s.off += n
		p = p[n:]
	}
}

// Uint64 returns the next 8 stream bytes as a big-endian unsigned integer.
func (s *Stream) Uint64() uint64 {
	var b [8]byte
	s.read(b[:])
	return binary.BigEndian.Uint64(b[:])
}

// IntN returns a uniform int in [0, n) via rejection sampling (rng/v1:
// draw Uint64 until v < 2^64 - (2^64 mod n)). It panics if n <= 0.
func (s *Stream) IntN(n int) int {
	if n <= 0 {
		panic("rng: IntN n must be > 0")
	}
	n64 := uint64(n)
	r := (math.MaxUint64%n64 + 1) % n64 // 2^64 mod n
	for {
		v := s.Uint64()
		if v <= math.MaxUint64-r { // v < 2^64 - r
			return int(v % n64)
		}
	}
}

// Ints returns count sequential IntN(n) draws — e.g. slot reel stops. It
// panics if count < 0 or n <= 0.
func (s *Stream) Ints(count, n int) []int {
	if count < 0 {
		panic("rng: Ints count must be >= 0")
	}
	out := make([]int, count)
	for i := range out {
		out[i] = s.IntN(n)
	}
	return out
}

// Float64 returns a uniform float64 in [0, 1): the top 53 bits of one
// Uint64 divided by 2^53 (rng/v1) — exact in IEEE 754 in every language.
func (s *Stream) Float64() float64 {
	return float64(s.Uint64()>>11) / (1 << 53)
}

// Roll returns a die roll in [1, sides]. It panics if sides <= 0.
func (s *Stream) Roll(sides int) int { return s.IntN(sides) + 1 }

// Shuffle pseudo-randomizes the order of n elements in the rng/v1 spec'd
// Fisher-Yates order. It panics if n < 0.
func (s *Stream) Shuffle(n int, swap func(i, j int)) {
	if n < 0 {
		panic("rng: Shuffle n must be >= 0")
	}
	for i := n - 1; i > 0; i-- {
		j := s.IntN(i + 1)
		swap(i, j)
	}
}

// Perm returns a permutation of [0, n): the identity slice reordered by
// Shuffle. It panics if n < 0.
func (s *Stream) Perm(n int) []int {
	p := make([]int, n)
	for i := range p {
		p[i] = i
	}
	s.Shuffle(n, func(i, j int) { p[i], p[j] = p[j], p[i] })
	return p
}

// Commitment returns the lowercase-hex SHA-256 of serverSeed — the value
// published before play.
func Commitment(serverSeed []byte) string {
	sum := sha256.Sum256(serverSeed)
	return hex.EncodeToString(sum[:])
}

// VerifyCommitment reports whether commitment matches serverSeed.
func VerifyCommitment(serverSeed []byte, commitment string) bool {
	return subtle.ConstantTimeCompare([]byte(Commitment(serverSeed)), []byte(commitment)) == 1
}
```

Note: `hash.Hash.Write` never returns an error (stdlib contract); if `just lint` errcheck flags the bare `s.mac.Write(...)` calls, change them to `_, _ = s.mac.Write(...)`.

- [ ] **Step 4: Generate the golden vector constants**

The three `PASTE-FROM-GENERATOR` constants in `TestGoldenVectors` must come from the reference implementation, not the package. Add this TEMPORARY test to `stream_test.go`:

```go
func TestPrintGolden(t *testing.T) {
	want := refBytes(t, testSeed(), "alice", 7, 24)
	u := binary.BigEndian.Uint64(want[0:8])
	v := binary.BigEndian.Uint64(want[8:16]) // IntN(100) uses the 2nd uint64 (no rejection possible for tiny r)
	f := binary.BigEndian.Uint64(want[16:24])
	t.Logf("goldenUint64  = uint64(%d)", u)
	t.Logf("goldenIntN100 = %d", v%100)
	t.Logf("goldenFloat64 = float64(%v)", float64(f>>11)/(1<<53))
}
```

Run: `go test ./gaming/rng/ -run TestPrintGolden -v`
Copy the three printed values into `TestGoldenVectors`, then DELETE `TestPrintGolden`. (Rejection note: for n=100, 2^64 mod 100 = 16, so the acceptance threshold rejects only 16 of 2^64 values — if `goldenIntN100` disagrees with the package, first check whether the drawn uint64 landed above `2^64 - 16`; it almost certainly did not.)

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test -race ./gaming/rng/`
Expected: PASS (all tests, including golden vectors matching the independent reference).

- [ ] **Step 6: Run the fuzzer briefly**

Run: `go test ./gaming/rng/ -fuzz FuzzStream -fuzztime 30s`
Expected: no crashers.

- [ ] **Step 7: Format, lint, commit**

```bash
just fmt ./gaming/...
just lint
git add gaming/rng docs/superpowers/plans/2026-07-13-gaming-rng.md
git commit -m "feat(rng): add gaming/rng Stream derivation core (rng/v1)"
```

---

### Task 2: Weighted Table (lootbox/wheel) with audit Version

**Files:**
- Create: `gaming/rng/table.go`
- Modify: `gaming/rng/errors.go` (add `ErrInvalidTable`)
- Test: `gaming/rng/table_test.go`

**Interfaces:**
- Consumes: `(*Stream).IntN` from Task 1.
- Produces: `Entry[T any]{Key string; Weight uint64; Item T}`, `TableOption func(*tableConfig)`, `NewTable[T any](entries []Entry[T], opts ...TableOption) (*Table[T], error)`, `(*Table[T]).Pick(s *Stream) Entry[T]`, `(*Table[T]).Version() string`, sentinel `ErrInvalidTable`. Task 3 extends `tableConfig` and `Table` with pity fields.

- [ ] **Step 1: Write the failing tests**

`gaming/rng/table_test.go`:

```go
package rng_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/gaming/rng"
)

func testEntries() []rng.Entry[string] {
	return []rng.Entry[string]{
		{Key: "common", Weight: 700, Item: "coins"},
		{Key: "rare", Weight: 250, Item: "gem"},
		{Key: "legendary", Weight: 50, Item: "dragon"},
	}
}

func TestNewTable_Validation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		entries []rng.Entry[string]
	}{
		{"empty", nil},
		{"empty key", []rng.Entry[string]{{Key: "", Weight: 1}}},
		{"zero weight", []rng.Entry[string]{{Key: "a", Weight: 0}}},
		{"duplicate key", []rng.Entry[string]{{Key: "a", Weight: 1}, {Key: "a", Weight: 2}}},
		{"overflow", []rng.Entry[string]{{Key: "a", Weight: math.MaxUint64}, {Key: "b", Weight: 1}}},
		{"exceeds int64", []rng.Entry[string]{{Key: "a", Weight: math.MaxInt64}, {Key: "b", Weight: 1}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := rng.NewTable(tc.entries)
			assert.ErrorIs(t, err, rng.ErrInvalidTable)
		})
	}
}

func TestTable_PickDeterministicAndSpecFaithful(t *testing.T) {
	t.Parallel()
	table, err := rng.NewTable(testEntries())
	require.NoError(t, err)

	s, err := rng.New(testSeed(), "loot", 0)
	require.NoError(t, err)
	got := make([]string, 20)
	for i := range got {
		got[i] = table.Pick(s).Key
	}

	// Reference: manual cumulative walk over an equal stream (rng/v1
	// weighted pick: IntN(total), first cumulative bucket wins).
	ref, err := rng.New(testSeed(), "loot", 0)
	require.NoError(t, err)
	entries := testEntries()
	for i := range got {
		draw := uint64(ref.IntN(1000))
		var cum uint64
		want := ""
		for _, e := range entries {
			cum += e.Weight
			if draw < cum {
				want = e.Key
				break
			}
		}
		assert.Equal(t, want, got[i], "pick #%d", i)
	}
}

func TestTable_PickReturnsFullEntry(t *testing.T) {
	t.Parallel()
	table, err := rng.NewTable(testEntries())
	require.NoError(t, err)
	e := table.Pick(rng.Casual())
	assert.NotEmpty(t, e.Key)
	assert.NotZero(t, e.Weight)
	assert.NotEmpty(t, e.Item)
}

func TestTable_Version(t *testing.T) {
	t.Parallel()
	a, err := rng.NewTable(testEntries())
	require.NoError(t, err)
	b, err := rng.NewTable(testEntries())
	require.NoError(t, err)
	assert.Equal(t, a.Version(), b.Version())
	assert.Len(t, a.Version(), 64) // hex SHA-256

	weightChanged := testEntries()
	weightChanged[2].Weight = 51
	c, err := rng.NewTable(weightChanged)
	require.NoError(t, err)
	assert.NotEqual(t, a.Version(), c.Version())

	keyRenamed := testEntries()
	keyRenamed[2].Key = "mythic"
	d, err := rng.NewTable(keyRenamed)
	require.NoError(t, err)
	assert.NotEqual(t, a.Version(), d.Version())

	// Item payload does NOT affect the version (identity is key+weight).
	itemChanged := testEntries()
	itemChanged[2].Item = "phoenix"
	e, err := rng.NewTable(itemChanged)
	require.NoError(t, err)
	assert.Equal(t, a.Version(), e.Version())
}

func TestTable_Distribution(t *testing.T) {
	t.Parallel()
	table, err := rng.NewTable(testEntries())
	require.NoError(t, err)
	s, err := rng.New(testSeed(), "dist", 0)
	require.NoError(t, err)
	counts := map[string]int{}
	const draws = 100000
	for range draws {
		counts[table.Pick(s).Key]++
	}
	assert.InDelta(t, 70000, counts["common"], 1500)
	assert.InDelta(t, 25000, counts["rare"], 1500)
	assert.InDelta(t, 5000, counts["legendary"], 700)
}

func TestTable_ImmutableAfterConstruction(t *testing.T) {
	t.Parallel()
	entries := testEntries()
	table, err := rng.NewTable(entries)
	require.NoError(t, err)
	v := table.Version()
	entries[0].Weight = 1 // mutating the input slice must not affect the table
	assert.Equal(t, v, table.Version())
	s, err := rng.New(testSeed(), "immut", 0)
	require.NoError(t, err)
	assert.NotPanics(t, func() { table.Pick(s) })
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./gaming/rng/ -run 'TestNewTable|TestTable'`
Expected: FAIL — `undefined: rng.NewTable`, `undefined: rng.Entry`.

- [ ] **Step 3: Write the implementation**

Append to `gaming/rng/errors.go` inside the `var (...)` block:

```go
	// ErrInvalidTable reports invalid drop-table construction: no entries,
	// empty or duplicate keys, zero weights, weight-sum overflow, or bad
	// pity configuration.
	ErrInvalidTable = errors.New("rng: invalid table")
```

`gaming/rng/table.go`:

```go
package rng

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
)

// Entry is one weighted outcome in a Table. Key is the stable identity
// used in the audit version hash and pity hit sets; Item is the payload.
type Entry[T any] struct {
	Item   T
	Key    string
	Weight uint64
}

// TableOption configures NewTable.
type TableOption func(*tableConfig)

type tableConfig struct{}

// Table is an immutable weighted outcome table (lootbox drop table,
// wheel segments). Safe for concurrent use.
type Table[T any] struct {
	entries []Entry[T]
	cum     []uint64 // cumulative weights; cum[len-1] is the total
	version string
}

// NewTable validates entries and builds an immutable table. Entries need
// non-empty unique keys and weights > 0; the weight sum must fit int64.
func NewTable[T any](entries []Entry[T], opts ...TableOption) (*Table[T], error) {
	var cfg tableConfig
	for _, o := range opts {
		o(&cfg)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("%w: no entries", ErrInvalidTable)
	}
	seen := make(map[string]struct{}, len(entries))
	cum := make([]uint64, len(entries))
	var total uint64
	h := sha256.New()
	var buf [8]byte
	for i, e := range entries {
		if e.Key == "" {
			return nil, fmt.Errorf("%w: empty key at index %d", ErrInvalidTable, i)
		}
		if _, dup := seen[e.Key]; dup {
			return nil, fmt.Errorf("%w: duplicate key %q", ErrInvalidTable, e.Key)
		}
		seen[e.Key] = struct{}{}
		if e.Weight == 0 {
			return nil, fmt.Errorf("%w: zero weight for key %q", ErrInvalidTable, e.Key)
		}
		if total > math.MaxUint64-e.Weight {
			return nil, fmt.Errorf("%w: weight sum overflows uint64", ErrInvalidTable)
		}
		total += e.Weight
		cum[i] = total
		// Version hash: length-prefixed key then weight, both big-endian —
		// unambiguous for any key content.
		binary.BigEndian.PutUint64(buf[:], uint64(len(e.Key)))
		h.Write(buf[:])
		h.Write([]byte(e.Key))
		binary.BigEndian.PutUint64(buf[:], e.Weight)
		h.Write(buf[:])
	}
	if total > math.MaxInt64 {
		return nil, fmt.Errorf("%w: weight sum must fit int64", ErrInvalidTable)
	}
	return &Table[T]{
		entries: append([]Entry[T](nil), entries...),
		cum:     cum,
		version: hex.EncodeToString(h.Sum(nil)),
	}, nil
}

// Pick draws one entry (rng/v1 weighted pick: IntN(totalWeight), first
// cumulative bucket in entry order wins).
func (t *Table[T]) Pick(s *Stream) Entry[T] {
	draw := uint64(s.IntN(int(t.cum[len(t.cum)-1])))
	i := sort.Search(len(t.cum), func(i int) bool { return draw < t.cum[i] })
	return t.entries[i]
}

// Version is the audit anchor: lowercase-hex SHA-256 over the ordered
// (key, weight) pairs. Store it on the game round to prove which drop
// configuration was live; the Item payload does not affect it.
func (t *Table[T]) Version() string { return t.version }
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race ./gaming/rng/`
Expected: PASS.

- [ ] **Step 5: Format, lint, commit**

```bash
just fmt ./gaming/...
just lint
git add gaming/rng
git commit -m "feat(rng): add weighted Table with audit Version"
```

---

### Task 3: Pity (guaranteed drop)

**Files:**
- Modify: `gaming/rng/table.go` (extend `tableConfig`, `Table`, `NewTable`; add `WithPity`, `PickWithPity`)
- Test: `gaming/rng/pity_test.go`

**Interfaces:**
- Consumes: `Table`/`Entry`/`NewTable`/`Pick` from Task 2; `Stream` from Task 1.
- Produces: `WithPity(threshold uint64, hitKeys ...string) TableOption`, `(*Table[T]).PickWithPity(s *Stream, misses uint64) (Entry[T], uint64)`.

- [ ] **Step 1: Write the failing tests**

`gaming/rng/pity_test.go`:

```go
package rng_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/gaming/rng"
)

func pityTable(t *testing.T, threshold uint64) *rng.Table[string] {
	t.Helper()
	table, err := rng.NewTable(testEntries(), rng.WithPity(threshold, "legendary"))
	require.NoError(t, err)
	return table
}

func TestWithPity_Validation(t *testing.T) {
	t.Parallel()
	_, err := rng.NewTable(testEntries(), rng.WithPity(0, "legendary"))
	assert.ErrorIs(t, err, rng.ErrInvalidTable, "zero threshold")
	_, err = rng.NewTable(testEntries(), rng.WithPity(10))
	assert.ErrorIs(t, err, rng.ErrInvalidTable, "no hit keys")
	_, err = rng.NewTable(testEntries(), rng.WithPity(10, "nosuch"))
	assert.ErrorIs(t, err, rng.ErrInvalidTable, "unknown hit key")
}

func TestPickWithPity_ForcedAtThreshold(t *testing.T) {
	t.Parallel()
	table := pityTable(t, 5)
	// misses = 4, threshold = 5 → misses+1 >= threshold → forced hit.
	for range 50 { // any stream state forces a hit
		e, next := table.PickWithPity(rng.Casual(), 4)
		assert.Equal(t, "legendary", e.Key)
		assert.Zero(t, next)
	}
}

func TestPickWithPity_CounterSemantics(t *testing.T) {
	t.Parallel()
	table := pityTable(t, 1000000) // threshold high enough to never force here
	s, err := rng.New(testSeed(), "pity", 0)
	require.NoError(t, err)
	misses := uint64(0)
	hits := 0
	for range 2000 {
		e, next := table.PickWithPity(s, misses)
		if e.Key == "legendary" {
			assert.Zero(t, next, "natural hit resets")
			hits++
		} else {
			assert.Equal(t, misses+1, next, "miss increments")
		}
		misses = next
	}
	assert.Positive(t, hits) // 5% over 2000 draws: expected ~100
}

func TestPickWithPity_DeterministicReplay(t *testing.T) {
	t.Parallel()
	table := pityTable(t, 20)
	run := func() ([]string, []uint64) {
		s, err := rng.New(testSeed(), "replay", 3)
		require.NoError(t, err)
		keys := make([]string, 100)
		counters := make([]uint64, 100)
		misses := uint64(0)
		for i := range keys {
			e, next := table.PickWithPity(s, misses)
			keys[i], counters[i], misses = e.Key, next, next
		}
		return keys, counters
	}
	k1, c1 := run()
	k2, c2 := run()
	assert.Equal(t, k1, k2)
	assert.Equal(t, c1, c2)
}

func TestPickWithPity_ForcedPickIsWeightedAmongHits(t *testing.T) {
	t.Parallel()
	table, err := rng.NewTable(testEntries(), rng.WithPity(1, "rare", "legendary"))
	require.NoError(t, err)
	s, err := rng.New(testSeed(), "forced", 0)
	require.NoError(t, err)
	counts := map[string]int{}
	for range 30000 { // threshold 1 → every pick forced into {rare, legendary}
		e, next := table.PickWithPity(s, 0)
		assert.Zero(t, next)
		counts[e.Key]++
	}
	assert.Zero(t, counts["common"])
	// rare:legendary = 250:50 = 5:1 → of 30000: 25000 vs 5000.
	assert.InDelta(t, 25000, counts["rare"], 800)
	assert.InDelta(t, 5000, counts["legendary"], 800)
}

func TestPickWithPity_PanicsWithoutWithPity(t *testing.T) {
	t.Parallel()
	table, err := rng.NewTable(testEntries())
	require.NoError(t, err)
	assert.Panics(t, func() { table.PickWithPity(rng.Casual(), 0) })
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./gaming/rng/ -run 'TestWithPity|TestPickWithPity'`
Expected: FAIL — `undefined: rng.WithPity`.

- [ ] **Step 3: Write the implementation**

In `gaming/rng/table.go`, replace `type tableConfig struct{}` with:

```go
type tableConfig struct {
	pityHitKeys   []string
	pityThreshold uint64
}

// WithPity guarantees a hit within threshold picks: when the caller's
// miss counter reaches threshold-1, the next pick is forced to the hit
// set (weighted among hit entries only, still drawn from the stream). A
// natural or forced hit resets the counter. Hit keys must exist in the
// table.
func WithPity(threshold uint64, hitKeys ...string) TableOption {
	return func(c *tableConfig) {
		c.pityThreshold = threshold
		c.pityHitKeys = hitKeys
	}
}
```

Extend the `Table` struct with pity fields:

```go
type Table[T any] struct {
	entries       []Entry[T]
	cum           []uint64 // cumulative weights; cum[len-1] is the total
	version       string
	hitSet        map[string]bool
	hitTable      *Table[T] // sub-table over hit entries; nil without WithPity
	pityThreshold uint64
}
```

In `NewTable`, after the existing validation loop and total check, add pity validation and construction (before the final `return`):

```go
	t := &Table[T]{
		entries: append([]Entry[T](nil), entries...),
		cum:     cum,
		version: hex.EncodeToString(h.Sum(nil)),
	}
	if cfg.pityThreshold > 0 || len(cfg.pityHitKeys) > 0 {
		if cfg.pityThreshold == 0 {
			return nil, fmt.Errorf("%w: pity threshold must be > 0", ErrInvalidTable)
		}
		if len(cfg.pityHitKeys) == 0 {
			return nil, fmt.Errorf("%w: pity requires at least one hit key", ErrInvalidTable)
		}
		t.hitSet = make(map[string]bool, len(cfg.pityHitKeys))
		var hits []Entry[T]
		for _, k := range cfg.pityHitKeys {
			if _, ok := seen[k]; !ok {
				return nil, fmt.Errorf("%w: pity hit key %q not in table", ErrInvalidTable, k)
			}
			if t.hitSet[k] {
				return nil, fmt.Errorf("%w: duplicate pity hit key %q", ErrInvalidTable, k)
			}
			t.hitSet[k] = true
		}
		for _, e := range t.entries {
			if t.hitSet[e.Key] {
				hits = append(hits, e)
			}
		}
		hitTable, err := NewTable(hits) // no options: plain weighted sub-table
		if err != nil {
			return nil, err // unreachable: hits are validated entries
		}
		t.hitTable = hitTable
		t.pityThreshold = cfg.pityThreshold
	}
	return t, nil
```

Add `PickWithPity`:

```go
// PickWithPity draws one entry under the pity rule and returns the
// updated miss counter for the caller to persist (next to the player row
// — the package never stores it). Requires WithPity; panics otherwise.
func (t *Table[T]) PickWithPity(s *Stream, misses uint64) (Entry[T], uint64) {
	if t.hitTable == nil {
		panic("rng: PickWithPity requires a table built with WithPity")
	}
	if misses+1 >= t.pityThreshold {
		return t.hitTable.Pick(s), 0
	}
	e := t.Pick(s)
	if t.hitSet[e.Key] {
		return e, 0
	}
	return e, misses + 1
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race ./gaming/rng/`
Expected: PASS.

- [ ] **Step 5: Format, lint, commit**

```bash
just fmt ./gaming/...
just lint
git add gaming/rng
git commit -m "feat(rng): add pity (guaranteed drop) to Table"
```

---

### Task 4: Cards and Deal

**Files:**
- Create: `gaming/rng/cards.go`
- Test: `gaming/rng/cards_test.go`

**Interfaces:**
- Consumes: `(*Stream).IntN` from Task 1.
- Produces: `type Suit uint8` (`Spades`, `Hearts`, `Diamonds`, `Clubs` consts), `type Card uint8` with `Suit() Suit`, `Rank() int`, `String() string`; `NewDeck(decks int) []Card`; `Deal[T any](s *Stream, items []T, n int) []T`.

- [ ] **Step 1: Write the failing tests**

`gaming/rng/cards_test.go`:

```go
package rng_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/gaming/rng"
)

func TestCard_StringRankSuit(t *testing.T) {
	t.Parallel()
	deck := rng.NewDeck(1)
	require.Len(t, deck, 52)
	assert.Equal(t, "2♠", deck[0].String())
	assert.Equal(t, "10♠", deck[8].String())
	assert.Equal(t, "J♠", deck[9].String())
	assert.Equal(t, "A♠", deck[12].String())
	assert.Equal(t, "2♥", deck[13].String())
	assert.Equal(t, "A♣", deck[51].String())

	assert.Equal(t, 2, deck[0].Rank())
	assert.Equal(t, 14, deck[12].Rank()) // Ace
	assert.Equal(t, rng.Spades, deck[0].Suit())
	assert.Equal(t, rng.Clubs, deck[51].Suit())
}

func TestNewDeck_MultiDeck(t *testing.T) {
	t.Parallel()
	deck := rng.NewDeck(2)
	require.Len(t, deck, 104)
	counts := map[rng.Card]int{}
	for _, c := range deck {
		counts[c]++
	}
	require.Len(t, counts, 52)
	for c, n := range counts {
		assert.Equal(t, 2, n, "card %s", c)
	}
	assert.Panics(t, func() { rng.NewDeck(0) })
}

func TestDeal_Deterministic(t *testing.T) {
	t.Parallel()
	deck := rng.NewDeck(1)
	s1, err := rng.New(testSeed(), "cards", 0)
	require.NoError(t, err)
	s2, err := rng.New(testSeed(), "cards", 0)
	require.NoError(t, err)
	h1 := rng.Deal(s1, deck, 5)
	h2 := rng.Deal(s2, deck, 5)
	assert.Equal(t, h1, h2)
	require.Len(t, h1, 5)
}

func TestDeal_WithoutReplacementAndNoMutation(t *testing.T) {
	t.Parallel()
	deck := rng.NewDeck(1)
	orig := append([]rng.Card(nil), deck...)
	hand := rng.Deal(rng.Casual(), deck, 52)
	assert.Equal(t, orig, deck) // input never mutated
	seen := map[rng.Card]bool{}
	for _, c := range hand {
		assert.False(t, seen[c], "duplicate card %s", c)
		seen[c] = true
	}
	assert.Len(t, seen, 52)
}

func TestDeal_Panics(t *testing.T) {
	t.Parallel()
	deck := rng.NewDeck(1)
	assert.Panics(t, func() { rng.Deal(rng.Casual(), deck, -1) })
	assert.Panics(t, func() { rng.Deal(rng.Casual(), deck, 53) })
	assert.Empty(t, rng.Deal(rng.Casual(), deck, 0))
}

func TestDeal_GenericRaffle(t *testing.T) {
	t.Parallel()
	users := []string{"ann", "bob", "cee", "dan", "eve"}
	winners := rng.Deal(rng.Casual(), users, 2)
	require.Len(t, winners, 2)
	assert.NotEqual(t, winners[0], winners[1])
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./gaming/rng/ -run 'TestCard|TestNewDeck|TestDeal'`
Expected: FAIL — `undefined: rng.NewDeck`.

- [ ] **Step 3: Write the implementation**

`gaming/rng/cards.go`:

```go
package rng

import "strconv"

// Suit is a playing-card suit.
type Suit uint8

// Suits in canonical deck order.
const (
	Spades Suit = iota
	Hearts
	Diamonds
	Clubs
)

var suitSymbols = [...]string{"♠", "♥", "♦", "♣"}

// String returns the suit symbol.
func (s Suit) String() string {
	if int(s) >= len(suitSymbols) {
		return "?"
	}
	return suitSymbols[s]
}

// Card is one playing card: value = suit*13 + rankIndex, where rankIndex
// 0 is Two and 12 is Ace. Values repeat across decks from NewDeck.
type Card uint8

// Suit returns the card's suit.
func (c Card) Suit() Suit { return Suit(c / 13 % 4) }

// Rank returns the rank as 2-14 (11 = Jack, 12 = Queen, 13 = King, 14 = Ace).
func (c Card) Rank() int { return int(c%13) + 2 }

// String renders like "A♠" or "10♥".
func (c Card) String() string {
	var rank string
	switch r := c % 13; {
	case r <= 8:
		rank = strconv.Itoa(int(r) + 2)
	case r == 9:
		rank = "J"
	case r == 10:
		rank = "Q"
	case r == 11:
		rank = "K"
	default:
		rank = "A"
	}
	return rank + c.Suit().String()
}

// NewDeck returns decks standard 52-card decks in canonical order
// (Spades Two..Ace, Hearts, Diamonds, Clubs, repeated per deck). It
// panics if decks <= 0.
func NewDeck(decks int) []Card {
	if decks <= 0 {
		panic("rng: NewDeck decks must be > 0")
	}
	out := make([]Card, 0, decks*52)
	for range decks {
		for c := range 52 {
			out = append(out, Card(c))
		}
	}
	return out
}

// Deal draws n items without replacement from a copy of items (cards,
// raffle winners): n partial Fisher-Yates steps in stream order, dealt
// items returned in draw order; items is never mutated. Deal(s, items,
// len(items)) consumes exactly the draws of Shuffle(len(items)). It
// panics if n < 0 or n > len(items).
func Deal[T any](s *Stream, items []T, n int) []T {
	if n < 0 || n > len(items) {
		panic("rng: Deal n must be in [0, len(items)]")
	}
	cp := append([]T(nil), items...)
	out := make([]T, 0, n)
	for i := len(cp) - 1; i > 0 && len(out) < n; i-- {
		j := s.IntN(i + 1)
		cp[i], cp[j] = cp[j], cp[i]
		out = append(out, cp[i])
	}
	if len(out) < n { // n == len(items): the final untouched element
		out = append(out, cp[0])
	}
	return out
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race ./gaming/rng/`
Expected: PASS.

- [ ] **Step 5: Format, lint, commit**

```bash
just fmt ./gaming/...
just lint
git add gaming/rng
git commit -m "feat(rng): add Card, NewDeck, and generic Deal"
```

---

### Task 5: Store seam, Record, memory store

**Files:**
- Create: `gaming/rng/store.go`, `gaming/rng/memory.go`
- Modify: `gaming/rng/errors.go` (add `ErrNotFound`, `ErrExists`)
- Test: `gaming/rng/store_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks (pure persistence layer).
- Produces: `type Record struct{ID, Scope, PlayerID string; ServerSeed []byte; ClientSeed string; Nonce uint64; Status string; Algorithm string; CreatedAt, RevealedAt time.Time}`, consts `StatusActive = "active"` / `StatusRevealed = "revealed"`, interface `Store{Active(ctx, scope, playerID string) (Record, error); Create(ctx context.Context, r Record) error; ConsumeNonce(ctx, scope, playerID string) (Record, error); Reveal(ctx context.Context, scope, id string, at time.Time) (Record, error); Get(ctx, scope, id string) (Record, error)}`, `NewMemoryStore() Store`, sentinels `ErrNotFound`, `ErrExists`. Task 6's Manager and Task 7's pgstore implement/consume exactly these.

- [ ] **Step 1: Write the failing tests**

`gaming/rng/store_test.go` (these are the CONTRACT tests — Task 7's pgstore tests mirror them):

```go
package rng_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/gaming/rng"
)

func mkRecord(id, scope, player string) rng.Record {
	return rng.Record{
		ID:         id,
		Scope:      scope,
		PlayerID:   player,
		ServerSeed: testSeed(),
		ClientSeed: "alice",
		Status:     rng.StatusActive,
		Algorithm:  rng.Algorithm,
		CreatedAt:  time.Now().UTC(),
	}
}

func TestMemoryStore_CreateActiveRoundTrip(t *testing.T) {
	t.Parallel()
	s := rng.NewMemoryStore()
	ctx := context.Background()
	rec := mkRecord("s1", "", "p1")
	require.NoError(t, s.Create(ctx, rec))

	got, err := s.Active(ctx, "", "p1")
	require.NoError(t, err)
	assert.Equal(t, rec.ID, got.ID)
	assert.Equal(t, rec.ServerSeed, got.ServerSeed)
	assert.Equal(t, rng.Algorithm, got.Algorithm)
	assert.Zero(t, got.Nonce)

	_, err = s.Active(ctx, "", "nobody")
	assert.ErrorIs(t, err, rng.ErrNotFound)
	_, err = s.Active(ctx, "other-scope", "p1")
	assert.ErrorIs(t, err, rng.ErrNotFound)
}

func TestMemoryStore_CreateConflicts(t *testing.T) {
	t.Parallel()
	s := rng.NewMemoryStore()
	ctx := context.Background()
	require.NoError(t, s.Create(ctx, mkRecord("s1", "", "p1")))
	assert.ErrorIs(t, s.Create(ctx, mkRecord("s2", "", "p1")), rng.ErrExists, "second active for same player")
	assert.ErrorIs(t, s.Create(ctx, mkRecord("s1", "", "p9")), rng.ErrExists, "duplicate id")
	assert.NoError(t, s.Create(ctx, mkRecord("s3", "", "p2")), "other player ok")
	assert.NoError(t, s.Create(ctx, mkRecord("s4", "acme", "p1")), "other scope ok")
}

func TestMemoryStore_ConsumeNonce(t *testing.T) {
	t.Parallel()
	s := rng.NewMemoryStore()
	ctx := context.Background()
	require.NoError(t, s.Create(ctx, mkRecord("s1", "", "p1")))

	for want := uint64(0); want < 3; want++ {
		rec, err := s.ConsumeNonce(ctx, "", "p1")
		require.NoError(t, err)
		assert.Equal(t, want, rec.Nonce, "consumed value is pre-increment")
		assert.Equal(t, "s1", rec.ID)
		assert.Equal(t, testSeed(), rec.ServerSeed)
	}
	act, err := s.Active(ctx, "", "p1")
	require.NoError(t, err)
	assert.Equal(t, uint64(3), act.Nonce, "stored nonce is next-unused")

	_, err = s.ConsumeNonce(ctx, "", "nobody")
	assert.ErrorIs(t, err, rng.ErrNotFound)
}

func TestMemoryStore_Reveal(t *testing.T) {
	t.Parallel()
	s := rng.NewMemoryStore()
	ctx := context.Background()
	require.NoError(t, s.Create(ctx, mkRecord("s1", "", "p1")))
	at := time.Now().UTC().Truncate(time.Second)

	rec, err := s.Reveal(ctx, "", "s1", at)
	require.NoError(t, err)
	assert.Equal(t, rng.StatusRevealed, rec.Status)
	assert.Equal(t, at, rec.RevealedAt)

	_, err = s.Active(ctx, "", "p1")
	assert.ErrorIs(t, err, rng.ErrNotFound, "revealed pair is no longer active")
	_, err = s.ConsumeNonce(ctx, "", "p1")
	assert.ErrorIs(t, err, rng.ErrNotFound, "cannot play a revealed seed")

	again, err := s.Reveal(ctx, "", "s1", at.Add(time.Hour))
	require.NoError(t, err)
	assert.Equal(t, at, again.RevealedAt, "reveal is idempotent")

	_, err = s.Reveal(ctx, "", "nosuch", at)
	assert.ErrorIs(t, err, rng.ErrNotFound)
	_, err = s.Reveal(ctx, "wrong-scope", "s1", at)
	assert.ErrorIs(t, err, rng.ErrNotFound)

	// A fresh active pair can be created after reveal.
	assert.NoError(t, s.Create(ctx, mkRecord("s2", "", "p1")))
}

func TestMemoryStore_Get(t *testing.T) {
	t.Parallel()
	s := rng.NewMemoryStore()
	ctx := context.Background()
	require.NoError(t, s.Create(ctx, mkRecord("s1", "acme", "p1")))
	got, err := s.Get(ctx, "acme", "s1")
	require.NoError(t, err)
	assert.Equal(t, "p1", got.PlayerID)
	_, err = s.Get(ctx, "", "s1")
	assert.ErrorIs(t, err, rng.ErrNotFound, "scope mismatch")
	_, err = s.Get(ctx, "acme", "nosuch")
	assert.ErrorIs(t, err, rng.ErrNotFound)
}

func TestMemoryStore_ConcurrentConsumeUniqueNonces(t *testing.T) {
	t.Parallel()
	s := rng.NewMemoryStore()
	ctx := context.Background()
	require.NoError(t, s.Create(ctx, mkRecord("s1", "", "p1")))

	const n = 100
	nonces := make([]uint64, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			var rec rng.Record
			rec, errs[i] = s.ConsumeNonce(ctx, "", "p1")
			nonces[i] = rec.Nonce
		})
	}
	wg.Wait()
	seen := make(map[uint64]bool, n)
	for i, v := range nonces {
		require.NoError(t, errs[i])
		assert.False(t, seen[v], "duplicate nonce %d", v)
		seen[v] = true
	}
}

func TestMemoryStore_ReturnedRecordIsIsolated(t *testing.T) {
	t.Parallel()
	s := rng.NewMemoryStore()
	ctx := context.Background()
	require.NoError(t, s.Create(ctx, mkRecord("s1", "", "p1")))
	got, err := s.Active(ctx, "", "p1")
	require.NoError(t, err)
	got.ServerSeed[0] ^= 0xff // mutating the returned copy...
	again, err := s.Active(ctx, "", "p1")
	require.NoError(t, err)
	assert.Equal(t, testSeed(), again.ServerSeed, "...must not corrupt the store")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./gaming/rng/ -run TestMemoryStore`
Expected: FAIL — `undefined: rng.NewMemoryStore`, `undefined: rng.Record`.

- [ ] **Step 3: Write the implementation**

Append to `gaming/rng/errors.go` inside the `var (...)` block:

```go
	// ErrNotFound reports an unknown seed id or a missing active pair.
	ErrNotFound = errors.New("rng: seed not found")

	// ErrExists reports a conflicting record: an active pair already
	// exists for the (scope, player), or the record id collides. Store
	// implementations return it from Create; the Manager consumes it
	// internally when racing get-or-create.
	ErrExists = errors.New("rng: active seed already exists")
```

`gaming/rng/store.go`:

```go
package rng

import (
	"context"
	"time"
)

// Seed pair statuses.
const (
	StatusActive   = "active"
	StatusRevealed = "revealed"
)

// Record is the storage shape of one seed pair. ServerSeed is always
// present — derivation needs it until reveal — so the backing table must
// be treated as secret material.
type Record struct {
	CreatedAt  time.Time
	RevealedAt time.Time // zero until revealed
	ID         string
	Scope      string
	PlayerID   string
	ClientSeed string
	Status     string // StatusActive or StatusRevealed
	Algorithm  string // Algorithm the pair derives with ("rng/v1")
	ServerSeed []byte
	Nonce      uint64 // next unused; ConsumeNonce returns the consumed value
}

// Store persists seed pairs. Implementations must be safe for concurrent
// use and must enforce at most one active record per (scope, playerID).
type Store interface {
	// Active returns the active record for (scope, playerID), or ErrNotFound.
	Active(ctx context.Context, scope, playerID string) (Record, error)
	// Create inserts r. ErrExists when an active record already exists
	// for (r.Scope, r.PlayerID) or the id collides.
	Create(ctx context.Context, r Record) error
	// ConsumeNonce atomically increments the active record's nonce and
	// returns the record with Nonce set to the consumed (pre-increment)
	// value; ErrNotFound when the player has no active record.
	ConsumeNonce(ctx context.Context, scope, playerID string) (Record, error)
	// Reveal marks the record revealed at the given time and returns it.
	// Idempotent: revealing a revealed record returns it unchanged.
	Reveal(ctx context.Context, scope, id string, at time.Time) (Record, error)
	// Get returns the record by id within scope, or ErrNotFound.
	Get(ctx context.Context, scope, id string) (Record, error)
}
```

`gaming/rng/memory.go`:

```go
package rng

import (
	"context"
	"sync"
	"time"
)

type memoryStore struct {
	byID   map[string]Record
	active map[string]string // activeKey(scope, playerID) → record id
	mu     sync.Mutex
}

// NewMemoryStore returns an in-memory Store for tests and development.
func NewMemoryStore() Store {
	return &memoryStore{
		byID:   make(map[string]Record),
		active: make(map[string]string),
	}
}

func activeKey(scope, playerID string) string { return scope + "\x00" + playerID }

// cloneRecord isolates the caller from the stored ServerSeed slice.
func cloneRecord(r Record) Record {
	r.ServerSeed = append([]byte(nil), r.ServerSeed...)
	return r
}

func (m *memoryStore) Active(_ context.Context, scope, playerID string) (Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.active[activeKey(scope, playerID)]
	if !ok {
		return Record{}, ErrNotFound
	}
	return cloneRecord(m.byID[id]), nil
}

func (m *memoryStore) Create(_ context.Context, r Record) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.byID[r.ID]; exists {
		return ErrExists
	}
	key := activeKey(r.Scope, r.PlayerID)
	if r.Status == StatusActive {
		if _, exists := m.active[key]; exists {
			return ErrExists
		}
		m.active[key] = r.ID
	}
	m.byID[r.ID] = cloneRecord(r)
	return nil
}

func (m *memoryStore) ConsumeNonce(_ context.Context, scope, playerID string) (Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.active[activeKey(scope, playerID)]
	if !ok {
		return Record{}, ErrNotFound
	}
	rec := m.byID[id]
	consumed := rec.Nonce
	rec.Nonce++
	m.byID[id] = rec
	rec.Nonce = consumed
	return cloneRecord(rec), nil
}

func (m *memoryStore) Reveal(_ context.Context, scope, id string, at time.Time) (Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.byID[id]
	if !ok || rec.Scope != scope {
		return Record{}, ErrNotFound
	}
	if rec.Status == StatusRevealed {
		return cloneRecord(rec), nil
	}
	rec.Status = StatusRevealed
	rec.RevealedAt = at
	delete(m.active, activeKey(scope, rec.PlayerID))
	m.byID[id] = rec
	return cloneRecord(rec), nil
}

func (m *memoryStore) Get(_ context.Context, scope, id string) (Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.byID[id]
	if !ok || rec.Scope != scope {
		return Record{}, ErrNotFound
	}
	return cloneRecord(rec), nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race ./gaming/rng/`
Expected: PASS (including the concurrent-consume race test under -race).

- [ ] **Step 5: Format, lint, commit**

```bash
just fmt ./gaming/...
just lint
git add gaming/rng
git commit -m "feat(rng): add Store seam, Record, and memory store"
```

---

### Task 6: Manager — provably-fair lifecycle

**Files:**
- Create: `gaming/rng/manager.go`, `gaming/rng/options.go`
- Modify: `gaming/rng/errors.go` (add `ErrNoScope`, `ErrStore`)
- Test: `gaming/rng/manager_test.go`, `gaming/rng/storeerr_test.go`

**Interfaces:**
- Consumes: `Store`/`Record`/`NewMemoryStore`/status consts (Task 5); `New`, `Commitment`, `Algorithm`, `validClientSeed`, `defaultClientSeedLen`, `clientSeedAlphabet`, `serverSeedLen` (Task 1); `core/clock` (`clock.Clock`, `clock.System()`, `clock.NewMock(t)`), `core/id` (`id.NewUUID().String()`), `core/random`.
- Produces: `Seed`, `Proof`, `NewManager(store Store, opts ...Option) (*Manager, error)`, `Option`, `WithScope(fn func(context.Context) (string, error)) Option`, `WithClock(cl clock.Clock) Option`, methods `ActiveSeed(ctx, playerID string) (Seed, error)`, `Play(ctx, playerID string) (*Stream, Proof, error)`, `SetClientSeed(ctx, playerID, clientSeed string) (Seed, error)`, `Rotate(ctx, playerID string) (Seed, Seed, error)`, `Seed(ctx, seedID string) (Seed, error)`, sentinels `ErrNoScope`, `ErrStore`.

- [ ] **Step 1: Write the failing tests**

`gaming/rng/storeerr_test.go` (an always-failing Store fake for fail-closed checks):

```go
package rng_test

import (
	"context"
	"errors"
	"time"

	"github.com/dmitrymomot/forge/gaming/rng"
)

var errBoom = errors.New("boom")

type failingStore struct{}

func (failingStore) Active(context.Context, string, string) (rng.Record, error) {
	return rng.Record{}, errBoom
}
func (failingStore) Create(context.Context, rng.Record) error { return errBoom }
func (failingStore) ConsumeNonce(context.Context, string, string) (rng.Record, error) {
	return rng.Record{}, errBoom
}
func (failingStore) Reveal(context.Context, string, string, time.Time) (rng.Record, error) {
	return rng.Record{}, errBoom
}
func (failingStore) Get(context.Context, string, string) (rng.Record, error) {
	return rng.Record{}, errBoom
}
```

`gaming/rng/manager_test.go`:

```go
package rng_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/gaming/rng"
)

type ctxKey struct{}

func newManager(t *testing.T, opts ...rng.Option) (*rng.Manager, rng.Store) {
	t.Helper()
	store := rng.NewMemoryStore()
	m, err := rng.NewManager(store, opts...)
	require.NoError(t, err)
	return m, store
}

func TestNewManager_Validation(t *testing.T) {
	t.Parallel()
	_, err := rng.NewManager(nil)
	assert.Error(t, err)
	_, err = rng.NewManager(rng.NewMemoryStore(), rng.WithClock(nil))
	assert.Error(t, err)
	_, err = rng.NewManager(rng.NewMemoryStore(), rng.WithScope(nil))
	assert.Error(t, err)
}

func TestActiveSeed_GetOrCreate(t *testing.T) {
	t.Parallel()
	m, _ := newManager(t)
	ctx := context.Background()

	seed, err := m.ActiveSeed(ctx, "p1")
	require.NoError(t, err)
	assert.NotEmpty(t, seed.ID)
	assert.Len(t, seed.Commitment, 64)
	assert.Nil(t, seed.ServerSeed, "active seed never exposes the server seed")
	assert.NotEmpty(t, seed.ClientSeed)
	assert.Zero(t, seed.Nonce)
	assert.Equal(t, rng.StatusActive, seed.Status)
	assert.Equal(t, rng.Algorithm, seed.Algorithm)

	again, err := m.ActiveSeed(ctx, "p1")
	require.NoError(t, err)
	assert.Equal(t, seed.ID, again.ID, "second call returns the same pair")

	_, err = m.ActiveSeed(ctx, "")
	assert.Error(t, err, "empty player id")
}

func TestPlay_NoncesAndProof(t *testing.T) {
	t.Parallel()
	m, _ := newManager(t)
	ctx := context.Background()

	_, p0, err := m.Play(ctx, "p1") // auto-creates the pair
	require.NoError(t, err)
	assert.Zero(t, p0.Nonce)
	assert.NotEmpty(t, p0.SeedID)
	assert.Len(t, p0.Commitment, 64)
	assert.Equal(t, rng.Algorithm, p0.Algorithm)

	_, p1, err := m.Play(ctx, "p1")
	require.NoError(t, err)
	assert.Equal(t, uint64(1), p1.Nonce)
	assert.Equal(t, p0.SeedID, p1.SeedID)
}

func TestPlay_VerificationRoundTrip(t *testing.T) {
	t.Parallel()
	m, _ := newManager(t)
	ctx := context.Background()

	// Play 3 rounds, recording outcomes.
	type round struct {
		proof rng.Proof
		stops []int
	}
	rounds := make([]round, 3)
	for i := range rounds {
		stream, proof, err := m.Play(ctx, "p1")
		require.NoError(t, err)
		rounds[i] = round{proof: proof, stops: stream.Ints(5, 100)}
	}

	// Rotate to reveal, then verify every round like a player would.
	old, next, err := m.Rotate(ctx, "p1")
	require.NoError(t, err)
	require.NotNil(t, old.ServerSeed)
	assert.Equal(t, rng.StatusRevealed, old.Status)
	assert.NotEqual(t, old.ID, next.ID)
	assert.Equal(t, old.ClientSeed, next.ClientSeed, "rotation inherits the client seed")

	for i, r := range rounds {
		assert.True(t, rng.VerifyCommitment(old.ServerSeed, r.proof.Commitment), "round %d commitment", i)
		s, err := rng.New(old.ServerSeed, r.proof.ClientSeed, r.proof.Nonce)
		require.NoError(t, err)
		assert.Equal(t, r.stops, s.Ints(5, 100), "round %d replay", i)
	}
}

func TestSetClientSeed(t *testing.T) {
	t.Parallel()
	m, _ := newManager(t)
	ctx := context.Background()

	// First call with no pair: creates one with the given seed.
	seed, err := m.SetClientSeed(ctx, "p1", "my-lucky-charm")
	require.NoError(t, err)
	assert.Equal(t, "my-lucky-charm", seed.ClientSeed)
	assert.Zero(t, seed.Nonce)

	_, _, err = m.Play(ctx, "p1")
	require.NoError(t, err)

	// Changing it rotates: old revealed, new pair fresh.
	next, err := m.SetClientSeed(ctx, "p1", "second-charm")
	require.NoError(t, err)
	assert.Equal(t, "second-charm", next.ClientSeed)
	assert.NotEqual(t, seed.ID, next.ID)
	assert.Zero(t, next.Nonce)

	old, err := m.Seed(ctx, seed.ID)
	require.NoError(t, err)
	assert.Equal(t, rng.StatusRevealed, old.Status)
	assert.NotNil(t, old.ServerSeed, "old pair revealed for verification")

	_, err = m.SetClientSeed(ctx, "p1", "no spaces allowed")
	assert.ErrorIs(t, err, rng.ErrInvalidClientSeed)
}

func TestRotate_RequiresActivePair(t *testing.T) {
	t.Parallel()
	m, _ := newManager(t)
	_, _, err := m.Rotate(context.Background(), "nobody")
	assert.ErrorIs(t, err, rng.ErrNotFound)
}

func TestSeed_Lookup(t *testing.T) {
	t.Parallel()
	m, _ := newManager(t)
	ctx := context.Background()
	created, err := m.ActiveSeed(ctx, "p1")
	require.NoError(t, err)

	got, err := m.Seed(ctx, created.ID)
	require.NoError(t, err)
	assert.Nil(t, got.ServerSeed, "unrevealed lookup hides the server seed")
	assert.Equal(t, created.Commitment, got.Commitment)

	_, err = m.Seed(ctx, "nosuch")
	assert.ErrorIs(t, err, rng.ErrNotFound)
}

func TestPlay_HealsAfterCrashedRotate(t *testing.T) {
	t.Parallel()
	m, store := newManager(t)
	ctx := context.Background()
	seed, err := m.ActiveSeed(ctx, "p1")
	require.NoError(t, err)

	// Simulate a rotate that crashed between reveal and create.
	_, err = store.Reveal(ctx, "", seed.ID, time.Now().UTC())
	require.NoError(t, err)

	_, proof, err := m.Play(ctx, "p1")
	require.NoError(t, err)
	assert.NotEqual(t, seed.ID, proof.SeedID, "fresh pair created")
	assert.Zero(t, proof.Nonce)
}

func TestScope_FailClosedAndIsolation(t *testing.T) {
	t.Parallel()
	hook := func(ctx context.Context) (string, error) {
		v, _ := ctx.Value(ctxKey{}).(string)
		return v, nil
	}
	m, _ := newManager(t, rng.WithScope(hook))

	ctxA := context.WithValue(context.Background(), ctxKey{}, "tenant-a")
	ctxB := context.WithValue(context.Background(), ctxKey{}, "tenant-b")

	seedA, err := m.ActiveSeed(ctxA, "p1")
	require.NoError(t, err)
	seedB, err := m.ActiveSeed(ctxB, "p1")
	require.NoError(t, err)
	assert.NotEqual(t, seedA.ID, seedB.ID, "same player id, different tenants, different pairs")

	_, err = m.Seed(ctxB, seedA.ID)
	assert.ErrorIs(t, err, rng.ErrNotFound, "cross-tenant lookup denied")

	// Empty scope from a configured hook fails closed.
	_, err = m.ActiveSeed(context.Background(), "p1")
	assert.ErrorIs(t, err, rng.ErrNoScope)
	_, _, err = m.Play(context.Background(), "p1")
	assert.ErrorIs(t, err, rng.ErrNoScope)

	// Hook errors fail closed too.
	broken, err := rng.NewManager(rng.NewMemoryStore(), rng.WithScope(func(context.Context) (string, error) {
		return "", errors.New("no tenant")
	}))
	require.NoError(t, err)
	_, err = broken.ActiveSeed(context.Background(), "p1")
	assert.ErrorIs(t, err, rng.ErrNoScope)
}

func TestManager_StoreFailuresWrapErrStore(t *testing.T) {
	t.Parallel()
	m, err := rng.NewManager(failingStore{})
	require.NoError(t, err)
	ctx := context.Background()

	_, err = m.ActiveSeed(ctx, "p1")
	assert.ErrorIs(t, err, rng.ErrStore)
	_, _, err = m.Play(ctx, "p1")
	assert.ErrorIs(t, err, rng.ErrStore)
	_, err = m.SetClientSeed(ctx, "p1", "seed")
	assert.ErrorIs(t, err, rng.ErrStore)
	_, _, err = m.Rotate(ctx, "p1")
	assert.ErrorIs(t, err, rng.ErrStore)
	_, err = m.Seed(ctx, "s1")
	assert.ErrorIs(t, err, rng.ErrStore)
}

func TestManager_ClockStampsCreatedAt(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	m, _ := newManager(t, rng.WithClock(clock.NewMock(now)))
	seed, err := m.ActiveSeed(context.Background(), "p1")
	require.NoError(t, err)
	assert.Equal(t, now, seed.CreatedAt)
}

func TestPlay_ConcurrentUniqueNonces(t *testing.T) {
	t.Parallel()
	m, _ := newManager(t)
	ctx := context.Background()

	const n = 50
	proofs := make([]rng.Proof, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			_, proofs[i], errs[i] = m.Play(ctx, "p1")
		})
	}
	wg.Wait()
	seen := make(map[uint64]bool, n)
	for i, p := range proofs {
		require.NoError(t, errs[i])
		assert.False(t, seen[p.Nonce], "duplicate nonce %d", p.Nonce)
		seen[p.Nonce] = true
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./gaming/rng/ -run 'TestNewManager|TestActiveSeed|TestPlay|TestSetClientSeed|TestRotate|TestSeed_|TestScope|TestManager'`
Expected: FAIL — `undefined: rng.NewManager`.

- [ ] **Step 3: Write the implementation**

Append to `gaming/rng/errors.go` inside the `var (...)` block:

```go
	// ErrNoScope reports fail-closed tenancy: the configured scope hook
	// errored or returned an empty scope.
	ErrNoScope = errors.New("rng: scope unavailable")

	// ErrStore wraps store/driver failures surfaced by the Manager.
	ErrStore = errors.New("rng: store failure")
```

`gaming/rng/options.go`:

```go
package rng

import (
	"context"

	"github.com/dmitrymomot/forge/core/clock"
)

type config struct {
	scope    func(context.Context) (string, error)
	clock    clock.Clock
	scopeSet bool
	clockSet bool
}

// Option configures NewManager.
type Option func(*config)

// WithScope derives the tenant scope from context for every operation.
// Fail-closed: a hook error or empty scope fails the call with ErrNoScope.
// Seeds are always player-owned within a tenant — there is no global case.
// A nil fn is a constructor error.
func WithScope(fn func(context.Context) (string, error)) Option {
	return func(c *config) { c.scope, c.scopeSet = fn, true }
}

// WithClock overrides the time source (tests). A nil clock is a
// constructor error.
func WithClock(cl clock.Clock) Option {
	return func(c *config) { c.clock, c.clockSet = cl, true }
}
```

`gaming/rng/manager.go`:

```go
package rng

import (
	"context"
	"errors"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/core/random"
)

// Seed is the public view of a seed pair. ServerSeed is nil until the
// pair is revealed — active pairs expose only the Commitment.
type Seed struct {
	CreatedAt  time.Time
	RevealedAt time.Time
	ID         string
	PlayerID   string
	Commitment string
	ClientSeed string
	Status     string
	Algorithm  string
	ServerSeed []byte
	Nonce      uint64
}

// Proof is what the consumer persists on a game round — everything a
// verify page needs to recompute the outcome after reveal.
type Proof struct {
	SeedID     string
	Commitment string
	ClientSeed string
	Algorithm  string
	Nonce      uint64
}

// Manager owns the provably-fair seed-pair lifecycle over a Store:
// commit-reveal, atomic per-round nonce consumption, and rotation.
type Manager struct {
	store Store
	cfg   config
}

// NewManager builds a Manager over store.
func NewManager(store Store, opts ...Option) (*Manager, error) {
	cfg := config{clock: clock.System()}
	for _, o := range opts {
		o(&cfg)
	}
	var errs []error
	if store == nil {
		errs = append(errs, errors.New("rng: nil store"))
	}
	if cfg.scopeSet && cfg.scope == nil {
		errs = append(errs, errors.New("rng: nil scope hook"))
	}
	if cfg.clockSet && cfg.clock == nil {
		errs = append(errs, errors.New("rng: nil clock"))
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return &Manager{store: store, cfg: cfg}, nil
}

var errEmptyPlayerID = errors.New("rng: player id must not be empty")

func (m *Manager) scopeFrom(ctx context.Context) (string, error) {
	if m.cfg.scope == nil {
		return "", nil
	}
	s, err := m.cfg.scope(ctx)
	if err != nil {
		return "", errors.Join(ErrNoScope, err)
	}
	if s == "" {
		return "", ErrNoScope
	}
	return s, nil
}

// toSeed converts a Record to its public view, exposing the server seed
// only once revealed.
func toSeed(r Record) Seed {
	s := Seed{
		ID:         r.ID,
		PlayerID:   r.PlayerID,
		Commitment: Commitment(r.ServerSeed),
		ClientSeed: r.ClientSeed,
		Nonce:      r.Nonce,
		Status:     r.Status,
		Algorithm:  r.Algorithm,
		CreatedAt:  r.CreatedAt,
		RevealedAt: r.RevealedAt,
	}
	if r.Status == StatusRevealed {
		s.ServerSeed = append([]byte(nil), r.ServerSeed...)
	}
	return s
}

// createPair inserts a fresh committed pair; empty clientSeed means
// generate one. Returns ErrExists unwrapped so callers can retry reads.
func (m *Manager) createPair(ctx context.Context, scope, playerID, clientSeed string) (Record, error) {
	if clientSeed == "" {
		clientSeed = random.String(defaultClientSeedLen, clientSeedAlphabet)
	} else if !validClientSeed(clientSeed) {
		return Record{}, ErrInvalidClientSeed
	}
	rec := Record{
		ID:         id.NewUUID().String(),
		Scope:      scope,
		PlayerID:   playerID,
		ServerSeed: random.Bytes(serverSeedLen),
		ClientSeed: clientSeed,
		Status:     StatusActive,
		Algorithm:  Algorithm,
		CreatedAt:  m.cfg.clock.Now().UTC(),
	}
	if err := m.store.Create(ctx, rec); err != nil {
		if errors.Is(err, ErrExists) {
			return Record{}, ErrExists
		}
		return Record{}, errors.Join(ErrStore, err)
	}
	return rec, nil
}

// ActiveSeed returns the player's active pair, creating one if none
// exists. The returned Seed carries the Commitment for the fairness UI;
// the server seed itself is never exposed while active.
func (m *Manager) ActiveSeed(ctx context.Context, playerID string) (Seed, error) {
	scope, err := m.scopeFrom(ctx)
	if err != nil {
		return Seed{}, err
	}
	if playerID == "" {
		return Seed{}, errEmptyPlayerID
	}
	rec, err := m.store.Active(ctx, scope, playerID)
	switch {
	case err == nil:
		return toSeed(rec), nil
	case errors.Is(err, ErrNotFound):
		created, cerr := m.createPair(ctx, scope, playerID, "")
		if errors.Is(cerr, ErrExists) { // lost a create race — read the winner
			rec, err = m.store.Active(ctx, scope, playerID)
			if err != nil {
				return Seed{}, errors.Join(ErrStore, err)
			}
			return toSeed(rec), nil
		}
		if cerr != nil {
			return Seed{}, cerr
		}
		return toSeed(created), nil
	default:
		return Seed{}, errors.Join(ErrStore, err)
	}
}

// Play atomically consumes the next nonce of the player's active pair and
// returns the derived Stream plus the Proof to persist on the game round.
// The pair is created on first play; a rotate that crashed between reveal
// and create is healed here.
func (m *Manager) Play(ctx context.Context, playerID string) (*Stream, Proof, error) {
	scope, err := m.scopeFrom(ctx)
	if err != nil {
		return nil, Proof{}, err
	}
	if playerID == "" {
		return nil, Proof{}, errEmptyPlayerID
	}
	for range 3 { // bounded retries against concurrent create/rotate races
		rec, err := m.store.ConsumeNonce(ctx, scope, playerID)
		if errors.Is(err, ErrNotFound) {
			if _, cerr := m.createPair(ctx, scope, playerID, ""); cerr != nil && !errors.Is(cerr, ErrExists) {
				return nil, Proof{}, cerr
			}
			continue
		}
		if err != nil {
			return nil, Proof{}, errors.Join(ErrStore, err)
		}
		s, err := New(rec.ServerSeed, rec.ClientSeed, rec.Nonce)
		if err != nil {
			return nil, Proof{}, errors.Join(ErrStore, err) // corrupted record
		}
		return s, Proof{
			SeedID:     rec.ID,
			Commitment: Commitment(rec.ServerSeed),
			ClientSeed: rec.ClientSeed,
			Nonce:      rec.Nonce,
			Algorithm:  rec.Algorithm,
		}, nil
	}
	return nil, Proof{}, errors.Join(ErrStore, errors.New("rng: no active seed after retries"))
}

// SetClientSeed rotates the player's pair onto clientSeed: the current
// pair (if any) is revealed and a fresh committed pair is created with
// the given client seed, so played (pair, nonce) history is never
// mutated. Creates the first pair when none exists.
func (m *Manager) SetClientSeed(ctx context.Context, playerID, clientSeed string) (Seed, error) {
	scope, err := m.scopeFrom(ctx)
	if err != nil {
		return Seed{}, err
	}
	if playerID == "" {
		return Seed{}, errEmptyPlayerID
	}
	if !validClientSeed(clientSeed) {
		return Seed{}, ErrInvalidClientSeed
	}
	cur, err := m.store.Active(ctx, scope, playerID)
	switch {
	case err == nil:
		if _, rerr := m.store.Reveal(ctx, scope, cur.ID, m.cfg.clock.Now().UTC()); rerr != nil {
			return Seed{}, errors.Join(ErrStore, rerr)
		}
	case !errors.Is(err, ErrNotFound):
		return Seed{}, errors.Join(ErrStore, err)
	}
	rec, err := m.createPair(ctx, scope, playerID, clientSeed)
	if errors.Is(err, ErrExists) {
		return Seed{}, errors.Join(ErrStore, err) // concurrent writer; caller retries
	}
	if err != nil {
		return Seed{}, err
	}
	return toSeed(rec), nil
}

// Rotate reveals the player's active pair — the returned old Seed carries
// the ServerSeed for verification — and creates a fresh committed pair
// inheriting the current client seed. ErrNotFound without an active pair.
func (m *Manager) Rotate(ctx context.Context, playerID string) (Seed, Seed, error) {
	scope, err := m.scopeFrom(ctx)
	if err != nil {
		return Seed{}, Seed{}, err
	}
	if playerID == "" {
		return Seed{}, Seed{}, errEmptyPlayerID
	}
	cur, err := m.store.Active(ctx, scope, playerID)
	if errors.Is(err, ErrNotFound) {
		return Seed{}, Seed{}, ErrNotFound
	}
	if err != nil {
		return Seed{}, Seed{}, errors.Join(ErrStore, err)
	}
	revealed, err := m.store.Reveal(ctx, scope, cur.ID, m.cfg.clock.Now().UTC())
	if err != nil {
		return Seed{}, Seed{}, errors.Join(ErrStore, err)
	}
	next, err := m.createPair(ctx, scope, playerID, cur.ClientSeed)
	if errors.Is(err, ErrExists) {
		return Seed{}, Seed{}, errors.Join(ErrStore, err) // concurrent writer; caller retries
	}
	if err != nil {
		return Seed{}, Seed{}, err
	}
	return toSeed(revealed), toSeed(next), nil
}

// Seed returns a pair by id for verify pages. The ServerSeed is included
// only once the pair is revealed.
func (m *Manager) Seed(ctx context.Context, seedID string) (Seed, error) {
	scope, err := m.scopeFrom(ctx)
	if err != nil {
		return Seed{}, err
	}
	rec, err := m.store.Get(ctx, scope, seedID)
	if errors.Is(err, ErrNotFound) {
		return Seed{}, ErrNotFound
	}
	if err != nil {
		return Seed{}, errors.Join(ErrStore, err)
	}
	return toSeed(rec), nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race ./gaming/rng/`
Expected: PASS.

- [ ] **Step 5: Format, lint, commit**

```bash
just fmt ./gaming/...
just lint
git add gaming/rng
git commit -m "feat(rng): add provably-fair seed-chain Manager"
```

---

### Task 7: Postgres driver (`gaming/rng/pgstore`)

**Files:**
- Create: `gaming/rng/pgstore/doc.go`, `gaming/rng/pgstore/pgstore.go`, `gaming/rng/pgstore/migrations/00001_create_rng_seeds.sql`
- Test: `gaming/rng/pgstore/pgstore_test.go`

**Interfaces:**
- Consumes: `rng.Store`, `rng.Record`, `rng.ErrNotFound`, `rng.ErrExists`, `rng.StatusActive`/`StatusRevealed`, `rng.Algorithm` (Task 5/1); `data/postgres` (`postgres.DefaultConfig()`, `postgres.Open`), `data/migration` (`migration.New(fs, migration.WithTable(...)).Up`), `pgx`.
- Produces: `pgstore.New(pool *pgxpool.Pool) *Store` implementing `rng.Store`; `pgstore.Migrations fs.FS`.

- [ ] **Step 1: Write the migration**

`gaming/rng/pgstore/migrations/00001_create_rng_seeds.sql`:

```sql
-- +goose Up
CREATE TABLE forge_rng_seeds (
    id          text PRIMARY KEY,
    scope       text NOT NULL DEFAULT '',
    player_id   text NOT NULL,
    server_seed bytea NOT NULL,
    client_seed text NOT NULL,
    nonce       bigint NOT NULL DEFAULT 0,
    status      text NOT NULL,
    algorithm   text NOT NULL,
    created_at  timestamptz NOT NULL,
    revealed_at timestamptz
);

CREATE UNIQUE INDEX forge_rng_seeds_active_idx ON forge_rng_seeds (scope, player_id) WHERE status = 'active';

-- +goose Down
DROP TABLE forge_rng_seeds;
```

- [ ] **Step 2: Write the failing tests**

`gaming/rng/pgstore/pgstore_test.go` — mirrors the Task 5 contract tests against real Postgres, gated on `FORGE_TEST_POSTGRES_DSN` (apikey precedent). Player and record ids must be unique per run (the table persists across runs):

```go
package pgstore_test

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/data/migration"
	"github.com/dmitrymomot/forge/data/postgres"
	"github.com/dmitrymomot/forge/gaming/rng"
	"github.com/dmitrymomot/forge/gaming/rng/pgstore"
)

var _ rng.Store = (*pgstore.Store)(nil)

func newStore(t *testing.T) *pgstore.Store {
	t.Helper()
	dsn := os.Getenv("FORGE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set FORGE_TEST_POSTGRES_DSN")
	}
	cfg := postgres.DefaultConfig()
	cfg.URL = dsn
	pool, err := postgres.Open(context.Background(), postgres.WithConfig(cfg))
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	db := stdlib.OpenDBFromPool(pool)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, migration.New(pgstore.Migrations, migration.WithTable("forge_rng_schema")).Up(context.Background(), db))
	return pgstore.New(pool)
}

func testSeedBytes() []byte {
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = byte(i)
	}
	return seed
}

// mkRecord builds a record with unique id and player per call: the table
// persists across test runs, so fixed values would collide.
func mkRecord(scope string) rng.Record {
	return rng.Record{
		ID:         id.NewUUID().String(),
		Scope:      scope,
		PlayerID:   "p-" + id.NewUUID().String(),
		ServerSeed: testSeedBytes(),
		ClientSeed: "alice",
		Status:     rng.StatusActive,
		Algorithm:  rng.Algorithm,
		CreatedAt:  time.Now().UTC().Truncate(time.Microsecond),
	}
}

func TestPg_CreateActiveRoundTrip(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	rec := mkRecord("")
	require.NoError(t, s.Create(ctx, rec))

	got, err := s.Active(ctx, "", rec.PlayerID)
	require.NoError(t, err)
	assert.Equal(t, rec.ID, got.ID)
	assert.Equal(t, rec.ServerSeed, got.ServerSeed)
	assert.Equal(t, rec.ClientSeed, got.ClientSeed)
	assert.Equal(t, rng.Algorithm, got.Algorithm)
	assert.Zero(t, got.Nonce)
	assert.True(t, got.RevealedAt.IsZero())

	_, err = s.Active(ctx, "", "p-nobody")
	assert.ErrorIs(t, err, rng.ErrNotFound)
	_, err = s.Active(ctx, "other", rec.PlayerID)
	assert.ErrorIs(t, err, rng.ErrNotFound)
}

func TestPg_CreateConflicts(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	rec := mkRecord("")
	require.NoError(t, s.Create(ctx, rec))

	second := mkRecord("")
	second.PlayerID = rec.PlayerID
	assert.ErrorIs(t, s.Create(ctx, second), rng.ErrExists, "second active for same player")

	dup := mkRecord("")
	dup.ID = rec.ID
	assert.ErrorIs(t, s.Create(ctx, dup), rng.ErrExists, "duplicate id")

	scoped := mkRecord("acme")
	scoped.PlayerID = rec.PlayerID
	assert.NoError(t, s.Create(ctx, scoped), "same player, different scope")
}

func TestPg_ConsumeNonce(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	rec := mkRecord("")
	require.NoError(t, s.Create(ctx, rec))

	for want := uint64(0); want < 3; want++ {
		got, err := s.ConsumeNonce(ctx, "", rec.PlayerID)
		require.NoError(t, err)
		assert.Equal(t, want, got.Nonce, "consumed value is pre-increment")
		assert.Equal(t, rec.ID, got.ID)
		assert.Equal(t, rec.ServerSeed, got.ServerSeed)
	}
	act, err := s.Active(ctx, "", rec.PlayerID)
	require.NoError(t, err)
	assert.Equal(t, uint64(3), act.Nonce)

	_, err = s.ConsumeNonce(ctx, "", "p-nobody")
	assert.ErrorIs(t, err, rng.ErrNotFound)
}

func TestPg_Reveal(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	rec := mkRecord("")
	require.NoError(t, s.Create(ctx, rec))
	at := time.Now().UTC().Truncate(time.Microsecond)

	got, err := s.Reveal(ctx, "", rec.ID, at)
	require.NoError(t, err)
	assert.Equal(t, rng.StatusRevealed, got.Status)
	assert.Equal(t, at, got.RevealedAt.UTC())

	_, err = s.Active(ctx, "", rec.PlayerID)
	assert.ErrorIs(t, err, rng.ErrNotFound)
	_, err = s.ConsumeNonce(ctx, "", rec.PlayerID)
	assert.ErrorIs(t, err, rng.ErrNotFound)

	again, err := s.Reveal(ctx, "", rec.ID, at.Add(time.Hour))
	require.NoError(t, err)
	assert.Equal(t, at, again.RevealedAt.UTC(), "reveal is idempotent")

	_, err = s.Reveal(ctx, "", id.NewUUID().String(), at)
	assert.ErrorIs(t, err, rng.ErrNotFound)
	_, err = s.Reveal(ctx, "wrong-scope", rec.ID, at)
	assert.ErrorIs(t, err, rng.ErrNotFound)

	// A fresh active pair can be created after reveal.
	next := mkRecord("")
	next.PlayerID = rec.PlayerID
	assert.NoError(t, s.Create(ctx, next))
}

func TestPg_Get(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	rec := mkRecord("acme")
	require.NoError(t, s.Create(ctx, rec))
	got, err := s.Get(ctx, "acme", rec.ID)
	require.NoError(t, err)
	assert.Equal(t, rec.PlayerID, got.PlayerID)
	_, err = s.Get(ctx, "", rec.ID)
	assert.ErrorIs(t, err, rng.ErrNotFound, "scope mismatch")
	_, err = s.Get(ctx, "acme", id.NewUUID().String())
	assert.ErrorIs(t, err, rng.ErrNotFound)
}

func TestPg_ConcurrentConsumeUniqueNonces(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	rec := mkRecord("")
	require.NoError(t, s.Create(ctx, rec))

	const n = 20
	nonces := make([]uint64, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			var got rng.Record
			got, errs[i] = s.ConsumeNonce(ctx, "", rec.PlayerID)
			nonces[i] = got.Nonce
		})
	}
	wg.Wait()
	seen := make(map[uint64]bool, n)
	for i, v := range nonces {
		require.NoError(t, errs[i])
		assert.False(t, seen[v], "duplicate nonce %d", v)
		seen[v] = true
	}
}

// TestPg_ManagerEndToEnd runs the full provably-fair lifecycle against
// real Postgres: play, rotate, verify.
func TestPg_ManagerEndToEnd(t *testing.T) {
	s := newStore(t)
	m, err := rng.NewManager(s)
	require.NoError(t, err)
	ctx := context.Background()
	player := "p-" + id.NewUUID().String()

	stream, proof, err := m.Play(ctx, player)
	require.NoError(t, err)
	stops := stream.Ints(5, 100)

	old, next, err := m.Rotate(ctx, player)
	require.NoError(t, err)
	require.NotNil(t, old.ServerSeed)
	assert.NotEqual(t, old.ID, next.ID)

	assert.True(t, rng.VerifyCommitment(old.ServerSeed, proof.Commitment))
	replay, err := rng.New(old.ServerSeed, proof.ClientSeed, proof.Nonce)
	require.NoError(t, err)
	assert.Equal(t, stops, replay.Ints(5, 100))
}
```

- [ ] **Step 3: Start Postgres and run tests to verify they fail**

```bash
docker run -d --name forge-rng-pg -e POSTGRES_PASSWORD=test -e POSTGRES_DB=forge -p 55432:5432 postgres:16-alpine
export FORGE_TEST_POSTGRES_DSN='postgres://postgres:test@localhost:55432/forge?sslmode=disable'
go test ./gaming/rng/pgstore/
```

Expected: FAIL — `undefined: pgstore.New` (compile error). If Docker is unavailable, the tests skip — then rely on CI, but still complete Step 4 and run `go vet`/build.

- [ ] **Step 4: Write the implementation**

`gaming/rng/pgstore/doc.go`:

```go
// Package pgstore provides the Postgres rng.Store over pgx.
//
// Apply Migrations before first use:
//
//	db := stdlib.OpenDBFromPool(pool)
//	_ = migration.New(pgstore.Migrations, migration.WithTable("forge_rng_schema")).Up(ctx, db)
//	store := pgstore.New(pool)
//
// The forge_rng_seeds table holds plaintext server seeds until reveal —
// treat it as secret material; at-rest encryption is a storage concern
// (disk encryption, pgcrypto) outside this package.
package pgstore
```

`gaming/rng/pgstore/pgstore.go`:

```go
package pgstore

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dmitrymomot/forge/gaming/rng"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations holds the goose migration creating forge_rng_seeds, rooted
// so its .sql files sit at fsys root (data/migration.New globs fsys's
// root, not subdirectories). Apply via data/migration under its own
// version table ("forge_rng_schema").
var Migrations fs.FS

func init() {
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		panic(err) // unreachable: migrations/*.sql is embedded at compile time
	}
	Migrations = sub
}

// Store is the Postgres implementation of rng.Store. The pool's lifecycle
// is the caller's.
type Store struct {
	pool *pgxpool.Pool
}

var _ rng.Store = (*Store)(nil)

// New builds a Postgres rng Store. Apply Migrations first.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

const cols = `id, scope, player_id, server_seed, client_seed, nonce, status, algorithm, created_at, revealed_at`

// Active returns the active record for (scope, playerID).
func (s *Store) Active(ctx context.Context, scope, playerID string) (rng.Record, error) {
	return scanRecord(s.pool.QueryRow(ctx,
		`SELECT `+cols+` FROM forge_rng_seeds WHERE scope = $1 AND player_id = $2 AND status = 'active'`,
		scope, playerID))
}

// Create inserts r. The partial unique index on active (scope, player_id)
// and the primary key both surface as rng.ErrExists.
func (s *Store) Create(ctx context.Context, r rng.Record) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO forge_rng_seeds (`+cols+`) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		r.ID, r.Scope, r.PlayerID, r.ServerSeed, r.ClientSeed, int64(r.Nonce), r.Status, r.Algorithm,
		r.CreatedAt, nullTime(r.RevealedAt))
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
		return rng.ErrExists
	}
	return err
}

// ConsumeNonce is the hot path: one conditional UPDATE ... RETURNING on
// the active row. RETURNING sees the post-update row, so the consumed
// (pre-increment) value is nonce - 1.
func (s *Store) ConsumeNonce(ctx context.Context, scope, playerID string) (rng.Record, error) {
	return scanRecord(s.pool.QueryRow(ctx,
		`UPDATE forge_rng_seeds SET nonce = nonce + 1
		 WHERE scope = $1 AND player_id = $2 AND status = 'active'
		 RETURNING id, scope, player_id, server_seed, client_seed, nonce - 1, status, algorithm, created_at, revealed_at`,
		scope, playerID))
}

// Reveal marks the record revealed; COALESCE keeps the first reveal time,
// making repeat reveals idempotent.
func (s *Store) Reveal(ctx context.Context, scope, id string, at time.Time) (rng.Record, error) {
	return scanRecord(s.pool.QueryRow(ctx,
		`UPDATE forge_rng_seeds SET status = 'revealed', revealed_at = COALESCE(revealed_at, $3)
		 WHERE scope = $1 AND id = $2
		 RETURNING `+cols, scope, id, at))
}

// Get returns the record by id within scope.
func (s *Store) Get(ctx context.Context, scope, id string) (rng.Record, error) {
	return scanRecord(s.pool.QueryRow(ctx,
		`SELECT `+cols+` FROM forge_rng_seeds WHERE scope = $1 AND id = $2`, scope, id))
}

func scanRecord(row pgx.Row) (rng.Record, error) {
	var rec rng.Record
	var nonce int64
	var revealed *time.Time
	err := row.Scan(&rec.ID, &rec.Scope, &rec.PlayerID, &rec.ServerSeed, &rec.ClientSeed,
		&nonce, &rec.Status, &rec.Algorithm, &rec.CreatedAt, &revealed)
	if errors.Is(err, pgx.ErrNoRows) {
		return rng.Record{}, rng.ErrNotFound
	}
	if err != nil {
		return rng.Record{}, err
	}
	rec.Nonce = uint64(nonce)
	if revealed != nil {
		rec.RevealedAt = *revealed
	}
	return rec, nil
}

// nullTime maps the zero time to SQL NULL.
func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test -race ./gaming/rng/pgstore/` (with `FORGE_TEST_POSTGRES_DSN` exported)
Expected: PASS. Then stop the container:

```bash
docker rm -f forge-rng-pg
```

- [ ] **Step 6: Format, lint, commit**

```bash
just fmt ./gaming/...
just lint
git add gaming/rng/pgstore
git commit -m "feat(rng): add pgstore Postgres driver with embedded migrations"
```

---

### Task 8: doc.go — normative spec, examples, anti-scope

**Files:**
- Modify: `gaming/rng/doc.go` (replace the Task 1 stub entirely)

**Interfaces:**
- Consumes: every public symbol from Tasks 1–7 (documentation only; no new symbols).
- Produces: nothing new — the package's reference documentation.

- [ ] **Step 1: Write the full doc.go**

Replace `gaming/rng/doc.go` with:

```go
// Package rng provides deterministic random outcomes for game mechanics:
// weighted drop tables (lootbox, wheel), dice, cards, slot-reel values —
// over one explicitly specified derivation algorithm with two entry
// points: Casual (CSPRNG, zero ceremony) and a provably-fair seed-chain
// Manager (commit-reveal, Store seam).
//
// # The rng/v1 derivation spec (normative, frozen)
//
// All randomness flows through a Stream derived from (serverSeed,
// clientSeed, nonce). The algorithm below is frozen forever under the
// identifier "rng/v1" (the Algorithm constant, stamped into every Proof
// and seed record); any change ships as rng/v2 alongside so old outcomes
// stay verifiable. It is reproducible in any language:
//
//   - Server seed: exactly 32 bytes. Commitment: lowercase-hex
//     SHA-256(serverSeed), published before play.
//   - Client seed: 1-64 chars of [A-Za-z0-9_-].
//   - Nonce: uint64 round counter, starting at 0.
//   - Block expansion: block_i = HMAC-SHA256(key = serverSeed,
//     msg = clientSeed || ":" || decimal(nonce) || ":" || decimal(i)) for
//     i = 0, 1, 2, ... The stream serves these 32-byte blocks
//     sequentially; each draw consumes exactly the bytes it needs,
//     crossing block boundaries transparently. A verifier replays draws
//     in order.
//   - Uint64: next 8 bytes, big-endian.
//   - IntN(n): rejection sampling — draw Uint64 until
//     v < 2^64 - (2^64 mod n), return v mod n. No modulo bias.
//   - Float64: (Uint64 >> 11) / 2^53, in [0, 1).
//   - Shuffle(n): Fisher-Yates: for i = n-1 down to 1, j = IntN(i+1),
//     swap(i, j). Perm(n) is the identity slice reordered by Shuffle.
//     Deal performs the same steps, stopping after n draws and returning
//     dealt items in draw order.
//   - Weighted pick (Table): draw IntN(total weight); the first entry, in
//     table order, whose cumulative weight exceeds the draw wins.
//
// # Casual use — lootbox with pity
//
//	table, err := rng.NewTable([]rng.Entry[Reward]{
//		{Key: "common", Weight: 700, Item: rewardCoins},
//		{Key: "rare", Weight: 250, Item: rewardGem},
//		{Key: "legendary", Weight: 50, Item: rewardSkin},
//	}, rng.WithPity(90, "legendary"))
//	if err != nil {
//		panic(err)
//	}
//	entry, misses := table.PickWithPity(rng.Casual(), player.PityMisses)
//	player.PityMisses = misses          // persist next to the player row
//	audit.TableVersion = table.Version() // prove which config was live
//
// The pity counter lives in the consumer's database, never here: it must
// update atomically with granting the reward, and only the consumer's
// transaction can do that.
//
// # Provably fair — slots round
//
//	store := pgstore.New(pool) // or rng.NewMemoryStore() for tests
//	m, err := rng.NewManager(store)
//	if err != nil {
//		panic(err)
//	}
//
//	seed, _ := m.ActiveSeed(ctx, playerID) // seed.Commitment → fairness UI, before any bet
//	stream, proof, err := m.Play(ctx, playerID)
//	if err != nil {
//		return err
//	}
//	stops := stream.Ints(5, len(reelStrip)) // 5 reels from one nonce
//	// Persist the round with proof; settle the bet in the consumer's tx.
//
// Verification after rotation:
//
//	old, _, _ := m.Rotate(ctx, playerID) // old.ServerSeed now revealed
//	ok := rng.VerifyCommitment(old.ServerSeed, proof.Commitment)
//	replay, _ := rng.New(old.ServerSeed, proof.ClientSeed, proof.Nonce)
//	same := replay.Ints(5, len(reelStrip)) // == stops
//
// Multi-tenant apps add rng.WithScope(func(ctx) (string, error)) —
// fail-closed: a hook error or empty scope fails the call with
// ErrNoScope. Single-tenant apps configure nothing.
//
// # Security and operational notes
//
// Seed records hold the plaintext server seed until reveal — inherent to
// commit-reveal. Treat the store as secret material; at-rest encryption
// is the consumer's storage concern. The server necessarily knows future
// outcomes for the active pair: provably fair proves non-manipulation
// after the fact, not server ignorance — which is why players can change
// their client seed (SetClientSeed rotates the pair) at any time.
//
// This package makes no certified-RNG claims: GLI-19 and similar are lab
// certifications of deployed systems, not properties a library can
// grant. Game math — paylines, RTP, payout multipliers — bet handling,
// and wallets are out of scope; compose with finance/ledger for money.
package rng
```

- [ ] **Step 2: Verify the examples compile conceptually and the package builds**

Run: `go build ./gaming/... && go vet ./gaming/...`
Expected: clean. Cross-check every symbol named in doc.go exists (`Algorithm`, `Casual`, `NewTable`, `WithPity`, `PickWithPity`, `Version`, `NewManager`, `ActiveSeed`, `Play`, `Ints`, `Rotate`, `VerifyCommitment`, `New`, `SetClientSeed`, `WithScope`, `ErrNoScope`, `NewMemoryStore`).

- [ ] **Step 3: Format, lint, commit**

```bash
just fmt ./gaming/...
just lint
git add gaming/rng/doc.go
git commit -m "docs(rng): document the rng/v1 spec, usage, and anti-scope"
```

---

### Task 9: Benchmarks, zero-alloc gate, optimization pass

**Files:**
- Create: `gaming/rng/bench_test.go`
- Modify (only if benchmarks prove wins): `gaming/rng/stream.go`, `gaming/rng/table.go`

**Interfaces:**
- Consumes: everything public from Tasks 1–6.
- Produces: benchmark suite; before/after numbers for the PR body.

- [ ] **Step 1: Write the benchmarks**

`gaming/rng/bench_test.go`:

```go
package rng_test

import (
	"context"
	"testing"

	"github.com/dmitrymomot/forge/gaming/rng"
)

func benchStream(b *testing.B) *rng.Stream {
	b.Helper()
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = byte(i)
	}
	s, err := rng.New(seed, "bench", 0)
	if err != nil {
		b.Fatal(err)
	}
	return s
}

func BenchmarkStreamUint64(b *testing.B) {
	s := benchStream(b)
	b.ReportAllocs()
	for b.Loop() {
		_ = s.Uint64()
	}
}

func BenchmarkStreamIntN(b *testing.B) {
	s := benchStream(b)
	b.ReportAllocs()
	for b.Loop() {
		_ = s.IntN(100)
	}
}

func BenchmarkStreamFloat64(b *testing.B) {
	s := benchStream(b)
	b.ReportAllocs()
	for b.Loop() {
		_ = s.Float64()
	}
}

func BenchmarkStreamInts5(b *testing.B) {
	s := benchStream(b)
	b.ReportAllocs()
	for b.Loop() {
		_ = s.Ints(5, 100)
	}
}

func BenchmarkTablePick(b *testing.B) {
	table, err := rng.NewTable(testEntries())
	if err != nil {
		b.Fatal(err)
	}
	s := benchStream(b)
	b.ReportAllocs()
	for b.Loop() {
		_ = table.Pick(s)
	}
}

func BenchmarkTablePickWithPity(b *testing.B) {
	table, err := rng.NewTable(testEntries(), rng.WithPity(90, "legendary"))
	if err != nil {
		b.Fatal(err)
	}
	s := benchStream(b)
	b.ReportAllocs()
	var misses uint64
	for b.Loop() {
		_, misses = table.PickWithPity(s, misses)
	}
}

func BenchmarkManagerPlay(b *testing.B) {
	m, err := rng.NewManager(rng.NewMemoryStore())
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	if _, _, err := m.Play(ctx, "bench"); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := m.Play(ctx, "bench"); err != nil {
			b.Fatal(err)
		}
	}
}
```

Add the zero-alloc gate to `gaming/rng/stream_test.go`:

```go
func TestStreamDraws_ZeroAlloc(t *testing.T) {
	s, err := rng.New(testSeed(), "alloc", 0)
	require.NoError(t, err)
	assert.Zero(t, testing.AllocsPerRun(1000, func() { _ = s.Uint64() }), "Uint64")
	assert.Zero(t, testing.AllocsPerRun(1000, func() { _ = s.IntN(100) }), "IntN")
	assert.Zero(t, testing.AllocsPerRun(1000, func() { _ = s.Float64() }), "Float64")
}
```

- [ ] **Step 2: Run benchmarks and record BEFORE numbers**

Run: `just bench ./gaming/rng/`
Expected: all benchmarks run; save the output — it goes in the PR body. `TestStreamDraws_ZeroAlloc` must pass (0 allocs/op on Uint64/IntN/Float64). If it fails, the usual culprit is `mac.Sum` allocating — fix by ensuring the sum target is the fixed `s.block[:0]` array slice and `s.scratch` has capacity 20 from construction.

- [ ] **Step 3: Optimization pass (measured wins only)**

Profile the two hottest paths (`BenchmarkStreamUint64`, `BenchmarkManagerPlay`). Apply an optimization ONLY if the benchmark shows a win; otherwise change nothing (readable first — docs/design.md). Candidates to evaluate, not mandates: avoiding the `var b [8]byte` copy in `Uint64` by reading directly from `s.block` when 8 bytes are available without crossing a boundary; preallocating the Proof in `Play`. Record AFTER numbers for anything changed; revert anything that doesn't measurably win.

- [ ] **Step 4: Full verification**

```bash
just test ./gaming/...
go test ./gaming/rng/ -fuzz FuzzStream -fuzztime 30s
just fmt ./gaming/...
just lint
```

Expected: all green; coverage from `just test` reported (target: >90% for gaming/rng).

- [ ] **Step 5: Commit**

```bash
git add gaming/rng
git commit -m "perf(rng): add benchmarks and zero-alloc gate for stream draws"
```

---

### Task 10: Final sweep — full suite, roadmap cleanup

**Files:**
- Modify: `docs/packages.md` (delete the `gaming/rng` entry — the catalog lists only unbuilt packages; keep the `## gaming/` header only if other entries remain under it, otherwise remove the whole section)

**Interfaces:** none — verification and bookkeeping.

- [ ] **Step 1: Run the full repo checks**

```bash
just fmt
just lint
just test
```

Expected: everything green repo-wide (not just gaming/).

- [ ] **Step 2: Delete the shipped roadmap entry**

In `docs/packages.md`, remove the `**gaming/rng**` entry added at design time (rule: "the moment a package ships, delete its entry"). Since `gaming/rng` is the only entry under `## gaming/`, remove the whole `## gaming/` section (header through the `Deps:` line, up to but not including `## async/`).

- [ ] **Step 3: Commit**

```bash
git add docs/packages.md
git commit -m "docs(packages): drop shipped gaming/rng roadmap entry"
```

---

## Plan self-review notes (already applied)

- Spec coverage: derivation spec → Task 1; Table/Version → Task 2; pity → Task 3; cards/Deal → Task 4; Store/Record/memory → Task 5; Manager/tenancy/errors → Task 6; pgstore/migrations → Task 7; doc.go spec+examples+anti-scope → Task 8; benchmarks/zero-alloc/optimization pass → Task 9; roadmap-entry deletion → Task 10. Statistical sanity and fuzz live in Tasks 1–3 test files; the store contract suite is Task 5 with Task 7 mirroring it against Postgres; concurrency tests are in Tasks 5, 6, and 7.
- Golden vectors cannot be precomputed in a plan; Task 1 Step 4 defines the exact generation procedure via the independent reference implementation, then deletes the generator.
- Type consistency verified: `Store` methods, `Record` fields (incl. `Algorithm`), `Seed`/`Proof` fields, and sentinel names are identical across Tasks 5, 6, and 7 and match the spec (spec was amended: `Reveal` takes `at time.Time`; pgstore table is `forge_rng_seeds` applied via `migration.New(...WithTable("forge_rng_schema"))`; Rotate inherits the client seed).
