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

// Uint64 returns the next 8 stream bytes as a big-endian unsigned integer.
// The block size (32) is a multiple of 8, so a draw never needs to splice
// across a refill boundary — read straight out of s.block.
func (s *Stream) Uint64() uint64 {
	if s.off == sha256.Size {
		s.refill()
	}
	v := binary.BigEndian.Uint64(s.block[s.off:])
	s.off += 8
	return v
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
// panics if count < 0, or if count > 0 and n <= 0.
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
