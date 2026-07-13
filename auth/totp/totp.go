package totp

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"hash"
	"strings"
	"time"

	"github.com/dmitrymomot/forge/crypto/consttime"
)

// Algorithm selects the HMAC hash for code derivation.
type Algorithm int

// Supported HMAC algorithms. SHA1 is the authenticator-app default.
const (
	SHA1 Algorithm = iota
	SHA256
	SHA512
)

// String returns the otpauth URI parameter form: "SHA1", "SHA256", "SHA512".
func (a Algorithm) String() string {
	switch a {
	case SHA1:
		return "SHA1"
	case SHA256:
		return "SHA256"
	case SHA512:
		return "SHA512"
	default:
		return "UNKNOWN"
	}
}

// hashFunc returns the hash constructor for HMAC.
func (a Algorithm) hashFunc() func() hash.Hash {
	switch a {
	case SHA256:
		return sha256.New
	case SHA512:
		return sha512.New
	default:
		return sha1.New
	}
}

// secretSize is the RFC 4226 §4 recommendation: secret length = hash length.
func (a Algorithm) secretSize() int {
	switch a {
	case SHA256:
		return 32
	case SHA512:
		return 64
	default:
		return 20
	}
}

func (a Algorithm) valid() bool { return a == SHA1 || a == SHA256 || a == SHA512 }

// TOTP holds validated code-generation parameters. One instance serves both
// enrollment (ProvisioningURI) and verification, so the parameters an
// authenticator app was enrolled with can never drift from the ones Verify
// checks. Safe for concurrent use.
type TOTP struct {
	cfg config
}

// New validates opts and builds a TOTP. Defaults: 6 digits, 30s period,
// SHA-1, skew ±1.
func New(opts ...Option) (*TOTP, error) {
	cfg := defaultConfig()
	for _, o := range opts {
		o(&cfg)
	}
	if err := cfg.validateCore(); err != nil {
		return nil, err
	}
	return &TOTP{cfg: cfg}, nil
}

func (c *config) validateCore() error {
	if c.digits != 6 && c.digits != 8 {
		return fmt.Errorf("totp: digits must be 6 or 8, got %d", c.digits)
	}
	if c.period < time.Second || c.period%time.Second != 0 {
		return fmt.Errorf("totp: period must be whole seconds >= 1s, got %s", c.period)
	}
	if c.skew < 0 {
		return fmt.Errorf("totp: skew must be >= 0, got %d", c.skew)
	}
	if !c.algorithm.valid() {
		return fmt.Errorf("totp: unknown algorithm %d", int(c.algorithm))
	}
	return nil
}

// decodeSecret parses a base32 shared secret leniently: users retype
// secrets by hand, so lowercase, interior spaces, and trailing padding are
// all accepted. Never panics on malformed input.
func decodeSecret(s string) ([]byte, error) {
	s = strings.ToUpper(strings.ReplaceAll(s, " ", ""))
	s = strings.TrimRight(s, "=")
	b, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("totp: decode secret: %w", err)
	}
	return b, nil
}

// hotp computes the RFC 4226 code: HMAC(key, counter) → dynamic truncation
// → digits decimal digits, zero-padded.
func hotp(h func() hash.Hash, key []byte, counter uint64, digits int) string {
	mac := hmac.New(h, key)
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)
	mac.Write(msg[:])
	sum := mac.Sum(nil)

	off := sum[len(sum)-1] & 0x0f
	v := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff

	mod := uint32(1_000_000)
	if digits == 8 {
		mod = 100_000_000
	}
	code := v % mod

	out := make([]byte, digits)
	for i := digits - 1; i >= 0; i-- {
		out[i] = '0' + byte(code%10)
		code /= 10
	}
	return string(out)
}

// HOTPCode computes the RFC 4226 code for counter. Counter state is the
// caller's; most consumers want time-based Code/Verify instead.
func (t *TOTP) HOTPCode(secret string, counter uint64) (string, error) {
	key, err := decodeSecret(secret)
	if err != nil {
		return "", err
	}
	return hotp(t.cfg.algorithm.hashFunc(), key, counter, t.cfg.digits), nil
}

// VerifyHOTP checks code against counters counter..counter+lookahead
// (constant-time, no early exit) and returns the next counter value to
// persist (matched+1). ErrInvalidCode when nothing matches.
func (t *TOTP) VerifyHOTP(secret, code string, counter uint64, lookahead int) (uint64, error) {
	if lookahead < 0 {
		return 0, fmt.Errorf("totp: negative lookahead %d", lookahead)
	}
	key, err := decodeSecret(secret)
	if err != nil {
		return 0, err
	}
	var next uint64
	found := false
	for i := range lookahead + 1 {
		c := counter + uint64(i)
		if consttime.StringEqual(hotp(t.cfg.algorithm.hashFunc(), key, c, t.cfg.digits), code) && !found {
			next, found = c+1, true
		}
	}
	if !found {
		return 0, ErrInvalidCode
	}
	return next, nil
}

func (t *TOTP) stepSeconds() int64 { return int64(t.cfg.period / time.Second) }

// Code computes the TOTP code for an explicit instant — client side, CLIs,
// tests. Server-side verification should use Verify.
func (t *TOTP) Code(secret string, at time.Time) (string, error) {
	if at.Unix() < 0 {
		return "", fmt.Errorf("totp: time %s is before the unix epoch", at)
	}
	key, err := decodeSecret(secret)
	if err != nil {
		return "", err
	}
	counter := uint64(at.Unix() / t.stepSeconds())
	return hotp(t.cfg.algorithm.hashFunc(), key, counter, t.cfg.digits), nil
}

// Verify checks code against every step in [now-skew, now+skew] — all
// windows evaluated without early exit, compared in constant time — then
// rejects any match at or before lastUsed's step (ErrReplayed). On success
// it returns the matched step-start time (UTC, whole seconds): persist it
// and pass it back as lastUsed on the next call. Zero lastUsed = never
// verified; callers pass back only step-times from prior Verify results,
// which are always after the Unix epoch. ErrInvalidCode when no window
// matches.
func (t *TOTP) Verify(secret, code string, lastUsed time.Time) (time.Time, error) {
	key, err := decodeSecret(secret)
	if err != nil {
		return time.Time{}, err
	}
	step := t.stepSeconds()
	cur := t.cfg.clk.Now().Unix() / step
	last := int64(-1)
	if !lastUsed.IsZero() {
		last = lastUsed.Unix() / step
	}

	matched, replayed := int64(-1), false
	for off := -t.cfg.skew; off <= t.cfg.skew; off++ {
		c := cur + int64(off)
		if c < 0 {
			continue
		}
		if !consttime.StringEqual(hotp(t.cfg.algorithm.hashFunc(), key, uint64(c), t.cfg.digits), code) {
			continue
		}
		if c <= last {
			replayed = true
		} else if c > matched {
			matched = c
		}
	}
	switch {
	case matched >= 0:
		return time.Unix(matched*step, 0).UTC(), nil
	case replayed:
		return time.Time{}, ErrReplayed
	default:
		return time.Time{}, ErrInvalidCode
	}
}
