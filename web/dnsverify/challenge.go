package dnsverify

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
