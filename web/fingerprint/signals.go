package fingerprint

import (
	"net/http"
	"net/netip"
)

// Signal is a structured anti-fraud fact derived from a Fingerprint. Value is
// the finding; Detail is a short human-readable explanation. Scoring is the
// consumer's job — this package only reports facts.
type Signal struct {
	Name   string
	Detail string
	Value  bool
}

func componentIndex(comps []Component) map[string]string {
	m := make(map[string]string, len(comps))
	for _, c := range comps {
		m[c.Name] = c.Value
	}
	return m
}

// Signals derives every signal whose required inputs (components and wired
// seams) are present. An unwired seam or absent layer simply omits its signals.
func (fp *Fingerprinter) Signals(r *http.Request, f Fingerprint) []Signal {
	comp := componentIndex(f.Components)
	var out []Signal
	if s, ok := fp.datacenterASN(comp); ok {
		out = append(out, s)
	}
	if s, ok := fp.botUA(comp); ok {
		out = append(out, s)
	}
	out = append(out, fp.componentSignals(r, comp)...) // Task 6
	return out
}

func (fp *Fingerprinter) datacenterASN(comp map[string]string) (Signal, bool) {
	if fp.geo == nil {
		return Signal{}, false
	}
	ip, err := netip.ParseAddr(comp["ip"])
	if err != nil {
		return Signal{}, false
	}
	info, ok := fp.geo(ip)
	if !ok {
		return Signal{}, false
	}
	detail := ""
	if info.Hosting {
		detail = "client IP is on a hosting/datacenter ASN"
	}
	return Signal{Name: "datacenter-asn", Value: info.Hosting, Detail: detail}, true
}

func (fp *Fingerprinter) botUA(comp map[string]string) (Signal, bool) {
	ua, has := comp["ua"]
	if !has || fp.ua == nil {
		return Signal{}, false
	}
	fam, ok := fp.ua(ua)
	if !ok {
		return Signal{}, false
	}
	return Signal{Name: "bot-ua", Value: fam == FamilyBot, Detail: ua}, true
}

// componentSignals is a temporary stub; Task 6 moves this method (with a real
// body) into signals_component.go and deletes this stub.
func (fp *Fingerprinter) componentSignals(_ *http.Request, _ map[string]string) []Signal {
	return nil
}
