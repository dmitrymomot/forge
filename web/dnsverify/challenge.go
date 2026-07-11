package dnsverify

import (
	"net/netip"

	"github.com/dmitrymomot/forge/core/random"
)

// RecordType is the DNS record kind a Challenge verifies.
type RecordType uint8

const (
	TXT RecordType = iota
	CNAME
	A
	AAAA
)

// String returns the stable uppercase DNS type token ("TXT", "CNAME", "A",
// "AAAA"). It is safe as an i18n key fragment and matches the record type a
// user types into a DNS panel. Unknown values render "UNKNOWN".
func (t RecordType) String() string {
	switch t {
	case TXT:
		return "TXT"
	case CNAME:
		return "CNAME"
	case A:
		return "A"
	case AAAA:
		return "AAAA"
	default:
		return "UNKNOWN"
	}
}

// Challenge describes one DNS record to verify: look up Record at Host and
// check the observed value(s) against Expect (any match verifies). It is plain
// and serializable — persist it (e.g. a Postgres row/JSONB) between issuing
// setup instructions and verifying later.
type Challenge struct {
	Host   string
	Expect []string
	Record RecordType
}

// txtValuePrefix namespaces the ownership token inside the TXT value so it
// never collides with SPF or other TXT records at the same host.
const txtValuePrefix = "forge-verification="

// TXTChallenge mints a fresh ownership token and returns a TXT Challenge to
// publish at Label.domain (e.g. "_forge-verify.example.com"). Persist the
// returned Challenge; verify it later with Verify. The token comes from
// random.URLSafe (unpadded base64url — safe in a TXT value).
func (v *Verifier) TXTChallenge(domain string) Challenge {
	token := random.URLSafe(v.cfg.TokenBytes)
	return Challenge{
		Record: TXT,
		Host:   v.cfg.Label + "." + domain,
		Expect: []string{txtValuePrefix + token},
	}
}

// CNAMEChallenge builds a routing Challenge: host must CNAME to target.
func (v *Verifier) CNAMEChallenge(host, target string) Challenge {
	return Challenge{Record: CNAME, Host: host, Expect: []string{target}}
}

// AChallenge builds a routing Challenge: host must resolve (A) to at least one
// of ips.
func (v *Verifier) AChallenge(host string, ips ...netip.Addr) Challenge {
	return Challenge{Record: A, Host: host, Expect: addrsToStrings(ips)}
}

// AAAAChallenge builds a routing Challenge: host must resolve (AAAA) to at
// least one of ips.
func (v *Verifier) AAAAChallenge(host string, ips ...netip.Addr) Challenge {
	return Challenge{Record: AAAA, Host: host, Expect: addrsToStrings(ips)}
}

func addrsToStrings(ips []netip.Addr) []string {
	out := make([]string, len(ips))
	for i, ip := range ips {
		out[i] = ip.String()
	}
	return out
}
