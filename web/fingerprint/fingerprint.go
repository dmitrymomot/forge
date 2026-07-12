package fingerprint

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// Component is one named contribution to a fingerprint. Value is the in-request
// normalized raw value and may be PII; persist a Digest, not Components.
type Component struct{ Name, Value string }

// Fingerprint is a versioned, component-tagged identity for one request.
type Fingerprint struct {
	parts      map[string]string // name -> hex HMAC, filled at build time
	Hash       string
	Components []Component
	Version    int
}

// Digest is the persistable form: per-component HMACs and the combined hash, no
// raw PII. Store this and compare with Drift.
type Digest struct {
	Parts   map[string]string
	Hash    string
	Version int
}

// Digest returns the persistable digest of f.
func (f Fingerprint) Digest() Digest {
	return Digest{Version: f.Version, Parts: maps.Clone(f.parts), Hash: f.Hash}
}

// Drift returns the sorted names of components whose per-component hash differs
// between old and next (including components present in only one). Consumers
// weight the result: a "ua" bump is benign; a simultaneous "tls"+"ip" flip is not.
func Drift(old, next Digest) []string {
	changed := map[string]struct{}{}
	for name, h := range next.Parts {
		if old.Parts[name] != h {
			changed[name] = struct{}{}
		}
	}
	for name := range old.Parts {
		if _, ok := next.Parts[name]; !ok {
			changed[name] = struct{}{}
		}
	}
	out := slices.Collect(maps.Keys(changed))
	slices.Sort(out)
	return out
}

// combineHash computes per-component HMACs and a combined HMAC over the
// version + name/parthash pairs, with components sorted by name for stability.
//
//nolint:unused // wired up by (*Fingerprinter).Build in a later task of this plan
func combineHash(secret []byte, version int, comps []Component) (string, map[string]string) {
	sorted := slices.Clone(comps)
	slices.SortFunc(sorted, func(a, b Component) int { return strings.Compare(a.Name, b.Name) })

	parts := make(map[string]string, len(sorted))
	for _, c := range sorted {
		m := hmac.New(sha256.New, secret)
		m.Write([]byte(c.Name))
		m.Write([]byte{0})
		m.Write([]byte(c.Value))
		parts[c.Name] = hex.EncodeToString(m.Sum(nil))
	}

	m := hmac.New(sha256.New, secret)
	m.Write([]byte(strconv.Itoa(version)))
	m.Write([]byte{0x1e})
	for _, c := range sorted {
		m.Write([]byte(c.Name))
		m.Write([]byte{0x1f})
		m.Write([]byte(parts[c.Name]))
		m.Write([]byte{0x1e})
	}
	return hex.EncodeToString(m.Sum(nil)), parts
}
