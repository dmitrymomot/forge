package keyset

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
)

// config accumulates keys and option errors before New validates them.
type config struct {
	keys       map[int][]byte
	errs       []error
	primary    int
	hasPrimary bool
}

// Option configures New. Invalid values accumulate and are returned by New.
type Option func(*config)

// WithPrimary registers key as the primary (encrypting/signing) key at version.
// Version must be in 0..255: the secret/token wire format encodes it as a single byte.
func WithPrimary(version int, key []byte) Option {
	return func(c *config) {
		if version < 0 || version > 255 || len(key) == 0 {
			c.errs = append(c.errs, fmt.Errorf("%w: primary version %d (must be 0..255)", ErrBadKeyMaterial, version))
			return
		}
		c.keys[version] = key
		c.primary = version
		c.hasPrimary = true
	}
}

// WithRetired registers a retired (decrypt/verify-only) key at version.
// Version must be in 0..255: the secret/token wire format encodes it as a single byte.
func WithRetired(version int, key []byte) Option {
	return func(c *config) {
		if version < 0 || version > 255 || len(key) == 0 {
			c.errs = append(c.errs, fmt.Errorf("%w: retired version %d (must be 0..255)", ErrBadKeyMaterial, version))
			return
		}
		c.keys[version] = key
	}
}

// WithBase64Keys parses comma-separated "version:base64" pairs (typically one env var).
// The highest version becomes primary; the rest are retired.
func WithBase64Keys(s string) Option {
	return func(c *config) {
		if strings.TrimSpace(s) == "" {
			c.errs = append(c.errs, fmt.Errorf("%w: empty key material", ErrBadKeyMaterial))
			return
		}
		// Parsing fails fast on the first malformed pair: one ErrBadKeyMaterial is enough
		// to reject the whole env value, and continuing would report noise.
		for pair := range strings.SplitSeq(s, ",") {
			verStr, b64, ok := strings.Cut(strings.TrimSpace(pair), ":")
			if !ok {
				c.errs = append(c.errs, fmt.Errorf("%w: %q missing version", ErrBadKeyMaterial, pair))
				return
			}
			v, err := strconv.Atoi(strings.TrimSpace(verStr))
			if err != nil || v < 0 || v > 255 {
				c.errs = append(c.errs, fmt.Errorf("%w: bad version %q (must be 0..255)", ErrBadKeyMaterial, verStr))
				return
			}
			key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
			if err != nil || len(key) == 0 {
				c.errs = append(c.errs, fmt.Errorf("%w: bad base64 for version %d", ErrBadKeyMaterial, v))
				return
			}
			c.keys[v] = key
			if !c.hasPrimary || v > c.primary {
				c.primary = v
				c.hasPrimary = true
			}
		}
	}
}
