package dnsverify

import (
	"fmt"
	"strings"
	"time"
)

// Config is the env-loadable deployment config. The resolver is a code-shaped
// seam and lives in options (WithResolver), not env.
type Config struct {
	Label      string        `env:"DNSVERIFY_LABEL"`       // TXT ownership host prefix
	Timeout    time.Duration `env:"DNSVERIFY_TIMEOUT"`     // per-lookup deadline
	TokenBytes int           `env:"DNSVERIFY_TOKEN_BYTES"` // entropy (bytes) of minted tokens
}

// DefaultConfig returns a 5s per-lookup timeout, the "_forge-verify" TXT label,
// and 16-byte tokens.
func DefaultConfig() Config {
	return Config{
		Timeout:    5 * time.Second,
		Label:      "_forge-verify",
		TokenBytes: 16,
	}
}

// Validate rejects a non-positive Timeout, a Label that is not a syntactically
// valid DNS host prefix, and TokenBytes < 8. Label is validated because it is
// concatenated as Label + "." + domain to form the record host; a malformed
// Label (spaces, illegal punctuation, stray dots) would otherwise silently
// yield an unresolvable Host at Verify time.
func (c Config) Validate() error {
	if c.Timeout <= 0 {
		return fmt.Errorf("%w: non-positive Timeout", ErrInvalidConfig)
	}
	if c.Label == "" {
		return fmt.Errorf("%w: empty Label", ErrInvalidConfig)
	}
	if !validLabelPrefix(c.Label) {
		return fmt.Errorf("%w: Label %q is not a valid DNS label", ErrInvalidConfig, c.Label)
	}
	if c.TokenBytes < 8 {
		return fmt.Errorf("%w: TokenBytes %d (want >= 8)", ErrInvalidConfig, c.TokenBytes)
	}
	return nil
}

// validLabelPrefix reports whether s is a syntactically valid DNS host prefix:
// one or more dot-separated labels, each 1-63 ASCII characters of letters,
// digits, hyphen, or underscore, with no leading or trailing hyphen. Underscore
// is permitted because service labels (e.g. the default "_forge-verify",
// "_dmarc", "_acme-challenge") begin with one. The empty string yields a single
// empty label and is rejected.
func validLabelPrefix(s string) bool {
	for label := range strings.SplitSeq(s, ".") {
		if !validLabel(label) {
			return false
		}
	}
	return true
}

func validLabel(label string) bool {
	if len(label) == 0 || len(label) > 63 {
		return false
	}
	for i := 0; i < len(label); i++ {
		ch := label[i]
		switch {
		case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9', ch == '_':
			// allowed anywhere
		case ch == '-':
			if i == 0 || i == len(label)-1 {
				return false // no leading or trailing hyphen
			}
		default:
			return false
		}
	}
	return true
}
