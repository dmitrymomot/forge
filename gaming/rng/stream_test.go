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
		_, _ = fmt.Fprintf(mac, "%s:%d:%d", client, nonce, i)
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
		goldenUint64  = uint64(9179226351331017642)
		goldenIntN100 = 63
		goldenFloat64 = float64(0.836028352292791)
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
	a, err := rng.New(testSeed(), "alice", 3)
	require.NoError(t, err)
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
