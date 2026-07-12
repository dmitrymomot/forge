package fingerprint

import (
	"net/http"
	"net/netip"
	"strings"
)

// automationJA4 maps well-known non-browser JA4 client fingerprints to a label.
// Pin values from the FoxIO JA4 reference vectors (github.com/FoxIO-LLC/ja4);
// bounded on purpose — an unmatched TLS hash simply does not fire the signal.
// It ships empty: no verified automation JA4 fingerprints have been pinned yet,
// so tls-ua-mismatch always reports Value:false while still emitting the
// signal (inputs present, nothing flagged) rather than silently omitting it.
var automationJA4 = map[string]string{
	// Example placeholders to be pinned during implementation from captured
	// handshakes; keep only verified entries:
	// "t13d1516h2_8daaf6152771_02713d6af862": "chrome-like (allowed)",
}

// componentSignals derives the component-driven signals: headless,
// tls-ua-mismatch, lang-mismatch, geo-tz-mismatch, and header-anomaly. Each
// emits only when its required components and seams are present.
func (fp *Fingerprinter) componentSignals(r *http.Request, comp map[string]string) []Signal {
	var out []Signal
	if v, ok := comp["js-webdriver"]; ok {
		out = append(out, Signal{Name: "headless", Value: v == "true", Detail: "navigator.webdriver"})
	}
	if s, ok := fp.tlsUAMismatch(comp); ok {
		out = append(out, s)
	}
	if s, ok := langMismatch(comp); ok {
		out = append(out, s)
	}
	if s, ok := fp.geoTZMismatch(comp); ok {
		out = append(out, s)
	}
	if s, ok := fp.headerAnomaly(r, comp); ok {
		out = append(out, s)
	}
	return out
}

func (fp *Fingerprinter) tlsUAMismatch(comp map[string]string) (Signal, bool) {
	tls, hasTLS := comp["tls"]
	ua, hasUA := comp["ua"]
	if !hasTLS || !hasUA || fp.ua == nil {
		return Signal{}, false
	}
	fam, ok := fp.ua(ua)
	if !ok || fam != FamilyBrowser {
		return Signal{}, false
	}
	label, flagged := automationJA4[tls]
	return Signal{Name: "tls-ua-mismatch", Value: flagged, Detail: label}, true
}

func langMismatch(comp map[string]string) (Signal, bool) {
	hdr, hasHdr := comp["accept-language"]
	js, hasJS := comp["js-languages"]
	if !hasHdr || !hasJS {
		return Signal{}, false
	}
	mismatch := primaryLang(hdr) != "" && primaryLang(js) != "" && primaryLang(hdr) != primaryLang(js)
	return Signal{Name: "lang-mismatch", Value: mismatch, Detail: hdr + " vs " + js}, true
}

func (fp *Fingerprinter) geoTZMismatch(comp map[string]string) (Signal, bool) {
	if fp.geo == nil {
		return Signal{}, false
	}
	tz, hasTZ := comp["js-timezone"]
	ip, err := netip.ParseAddr(comp["ip"])
	if !hasTZ || err != nil {
		return Signal{}, false
	}
	info, ok := fp.geo(ip)
	if !ok || info.Continent == "" {
		return Signal{}, false
	}
	jsCont := continentOfTZ(tz)
	if jsCont == "" {
		return Signal{}, false
	}
	return Signal{Name: "geo-tz-mismatch", Value: jsCont != info.Continent, Detail: tz + " vs " + info.Continent}, true
}

// headerAnomaly fires when the UA claims a modern Chromium browser but the
// Client-Hints / Fetch-Metadata headers that browser always sends are absent.
func (fp *Fingerprinter) headerAnomaly(r *http.Request, comp map[string]string) (Signal, bool) {
	ua, hasUA := comp["ua"]
	if !hasUA || fp.ua == nil {
		return Signal{}, false
	}
	fam, ok := fp.ua(ua)
	if !ok || fam != FamilyBrowser {
		return Signal{}, false
	}
	if !strings.Contains(ua, "Chrome/") { // Client Hints are Chromium-specific
		return Signal{}, false
	}
	missing := r.Header.Get("Sec-Ch-Ua") == "" || r.Header.Get("Sec-Fetch-Site") == ""
	return Signal{Name: "header-anomaly", Value: missing, Detail: "Chrome UA without Sec-Ch-Ua/Sec-Fetch-*"}, true
}

// primaryLang returns the base language subtag of the first entry ("en-US,..." -> "en").
func primaryLang(v string) string {
	first, _, _ := strings.Cut(v, ",")
	first = strings.TrimSpace(first)
	if i := strings.IndexByte(first, ';'); i >= 0 {
		first = first[:i]
	}
	base, _, _ := strings.Cut(first, "-")
	return strings.ToLower(strings.TrimSpace(base))
}

// continentOfTZ maps an IANA zone's region prefix to a two-letter continent
// code, or "" when unknown or ambiguous (the America/ prefix spans NA and SA,
// so it is treated as unknown to avoid false positives).
func continentOfTZ(tz string) string {
	region, _, ok := strings.Cut(tz, "/")
	if !ok {
		return ""
	}
	switch region {
	case "Europe":
		return "EU"
	case "Africa":
		return "AF"
	case "Asia":
		return "AS"
	case "Australia", "Pacific":
		return "OC"
	case "Antarctica":
		return "AN"
	default: // America (ambiguous NA/SA), Atlantic, Indian, Etc, UTC...
		return ""
	}
}
