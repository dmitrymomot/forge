package id

import (
	"crypto/rand"
	"fmt"
	"sync"

	"github.com/dmitrymomot/forge/clock"
)

// Generator produces IDs from an injectable clock. The zero-configuration free
// functions (NewUUID, NewULID, NewShort) use a shared non-monotonic default.
// Construct a Generator when you need a test clock or strictly-increasing
// same-millisecond ordering (WithMonotonic).
//
// Always construct a Generator with NewGenerator. The zero value is not usable:
// its clock is nil and the first generation call panics.
type Generator struct {
	clk       clock.Clock
	uuidMS    uint64
	ulidMS    uint64
	shortMS   uint64
	mu        sync.Mutex
	uuidVal   UUID
	ulidVal   ULID
	shortVal  Short
	monotonic bool
}

// Option configures a Generator.
type Option func(*Generator)

// WithClock sets the time source; a nil clock is ignored. Defaults to clock.System().
func WithClock(c clock.Clock) Option {
	return func(g *Generator) {
		if c != nil {
			g.clk = c
		}
	}
}

// WithMonotonic enables strictly-increasing, collision-free IDs within a single
// millisecond by incrementing the random component instead of redrawing it. It
// adds a mutex, so it is opt-in; the default free functions stay lock-free.
func WithMonotonic() Option {
	return func(g *Generator) { g.monotonic = true }
}

// NewGenerator returns a Generator configured by opts.
func NewGenerator(opts ...Option) *Generator {
	g := &Generator{clk: clock.System()}
	for _, o := range opts {
		o(g)
	}
	return g
}

func randRead(p []byte) {
	if _, err := rand.Read(p); err != nil {
		panic(fmt.Errorf("id: crypto/rand failed: %w", err))
	}
}

func (g *Generator) nowMS() uint64 { return uint64(g.clk.Now().UnixMilli()) }

func setUUIDBits(u *UUID) {
	u[6] = (u[6] & 0x0f) | 0x70 // version 7
	u[8] = (u[8] & 0x3f) | 0x80 // variant 0b10
}

// UUID returns a new version-7 UUID.
func (g *Generator) UUID() UUID {
	if !g.monotonic {
		var u UUID
		putMillis(u[:], g.nowMS())
		randRead(u[6:])
		setUUIDBits(&u)
		return u
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	ms := g.nowMS()
	if ms > g.uuidMS {
		g.uuidMS = ms
		putMillis(g.uuidVal[:], ms)
		randRead(g.uuidVal[6:])
		setUUIDBits(&g.uuidVal)
	} else if incrUUIDRand(&g.uuidVal) { // random space exhausted within one ms
		g.uuidMS++
		putMillis(g.uuidVal[:], g.uuidMS)
		randRead(g.uuidVal[6:])
		setUUIDBits(&g.uuidVal)
	}
	return g.uuidVal
}

// ULID returns a new ULID.
func (g *Generator) ULID() ULID {
	if !g.monotonic {
		var u ULID
		putMillis(u[:], g.nowMS())
		randRead(u[6:])
		return u
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	ms := g.nowMS()
	if ms > g.ulidMS {
		g.ulidMS = ms
		putMillis(g.ulidVal[:], ms)
		randRead(g.ulidVal[6:])
	} else if incrBytes(g.ulidVal[6:]) {
		g.ulidMS++
		putMillis(g.ulidVal[:], g.ulidMS)
		randRead(g.ulidVal[6:])
	}
	return g.ulidVal
}

// Short returns a new Short.
func (g *Generator) Short() Short {
	if !g.monotonic {
		var s Short
		putMillis(s[:], g.nowMS())
		randRead(s[6:])
		return s
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	ms := g.nowMS()
	if ms > g.shortMS {
		g.shortMS = ms
		putMillis(g.shortVal[:], ms)
		randRead(g.shortVal[6:])
	} else if incrBytes(g.shortVal[6:]) {
		g.shortMS++
		putMillis(g.shortVal[:], g.shortMS)
		randRead(g.shortVal[6:])
	}
	return g.shortVal
}

// incrBytes adds 1 to p interpreted as a big-endian integer, returning true on
// overflow (all bytes wrapped to zero). Used for ULID (10 bytes) and Short (4).
func incrBytes(p []byte) bool {
	for i := len(p) - 1; i >= 0; i-- {
		p[i]++
		if p[i] != 0 {
			return false
		}
	}
	return true
}

// incrUUIDRand adds 1 to the 74 random bits of a version-7 UUID (rand_b in
// bytes 9..15 and the low 6 bits of byte 8, then rand_a in byte 7 and the low
// nibble of byte 6), preserving the version and variant bits. It returns true on
// full overflow.
func incrUUIDRand(u *UUID) bool {
	for i := 15; i >= 9; i-- { // rand_b tail
		u[i]++
		if u[i] != 0 {
			return false
		}
	}
	if low := (u[8] & 0x3f) + 1; low <= 0x3f { // low 6 bits of byte 8
		u[8] = 0x80 | low
		return false
	}
	u[8] = 0x80            // wrapped; variant preserved
	if u[7]++; u[7] != 0 { // rand_a high byte
		return false
	}
	if low := (u[6] & 0x0f) + 1; low <= 0x0f { // low nibble of byte 6
		u[6] = (u[6] & 0xf0) | low
		return false
	}
	u[6] &= 0xf0 // wrapped; version nibble preserved
	return true
}

// defaultGen backs the package-level free functions: non-monotonic and lock-free.
var defaultGen = &Generator{clk: clock.System()}

// NewUUID returns a new version-7 UUID from the default generator.
func NewUUID() UUID { return defaultGen.UUID() }

// NewULID returns a new ULID from the default generator.
func NewULID() ULID { return defaultGen.ULID() }

// NewShort returns a new Short from the default generator.
func NewShort() Short { return defaultGen.Short() }
