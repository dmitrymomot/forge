package fingerprint

import (
	"net/http"
	"net/netip"
	"strings"
)

// componentSignals derives the component-driven signals: headless,
// tls-ua-mismatch, lang-mismatch, ch-ua-mismatch, geo-tz-mismatch,
// header-anomaly, and fetch-metadata-anomaly. Each emits only when its
// required inputs (components, seams, or raw request headers) are present.
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
	if s, ok := chUAMismatch(comp); ok {
		out = append(out, s)
	}
	if s, ok := fp.geoTZMismatch(comp); ok {
		out = append(out, s)
	}
	if s, ok := fp.headerAnomaly(r, comp); ok {
		out = append(out, s)
	}
	if s, ok := fetchMetadataAnomaly(r); ok {
		out = append(out, s)
	}
	return out
}

// tlsUAMismatch requires a JA4-format "tls" component — tlsprint.Local() or a
// JA4 header source (e.g. tlsprint.CloudFrontJA4), NOT the JA3 hash from
// tlsprint.CloudflareJA3, which never matches the pinned JA4 keys. The
// signal is inert (always Value:false) until WithAutomationJA4 is set with
// pinned automation fingerprints.
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
	label, flagged := fp.automationJA4[tls]
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

// chUAMismatch compares the Client-Hint platform (ch-ua-platform, e.g. "Windows")
// against navigator.platform (js-platform, e.g. "Win32") through a coarse OS
// normalization; disagreement is a spoofing tell. It fires only when both
// normalize to a known, differing OS. Ambiguous values — bare "Linux arm*",
// shared by Android and desktop Linux — yield no signal, avoiding false positives.
func chUAMismatch(comp map[string]string) (Signal, bool) {
	chPlat, hasCH := comp["ch-ua-platform"]
	jsPlat, hasJS := comp["js-platform"]
	if !hasCH || !hasJS {
		return Signal{}, false
	}
	chOS := osFromClientHint(chPlat)
	jsOS := osFromJSPlatform(jsPlat)
	if chOS == "" || jsOS == "" {
		return Signal{}, false
	}
	return Signal{Name: "ch-ua-mismatch", Value: chOS != jsOS, Detail: chPlat + " vs " + jsPlat}, true
}

// osFromClientHint maps a Sec-CH-UA-Platform value (a quoted token) to a coarse
// OS key, or "" when unknown. ChromeOS ("Chrome OS"/"Chromium OS") is treated
// as ambiguous (no signal) because navigator.platform can't distinguish it
// from desktop Linux — see osFromJSPlatform.
func osFromClientHint(v string) string {
	switch strings.Trim(v, `"`) {
	case "Windows":
		return "windows"
	case "macOS":
		return "macos"
	case "iOS":
		return "ios"
	case "Android":
		return "android"
	case "Chrome OS", "Chromium OS":
		// ChromeOS reports navigator.platform "Linux x86_64" — indistinguishable
		// from desktop Linux — so treat it as ambiguous and emit no signal,
		// mirroring the bare-"Linux arm*" case in osFromJSPlatform.
		return ""
	case "Linux":
		return "linux"
	default:
		return ""
	}
}

// osFromJSPlatform maps a navigator.platform value to a coarse OS key, or ""
// when unknown or ambiguous (bare "Linux arm*" is shared by Android and desktop
// Linux, so it is treated as unknown). ChromeOS reports "Linux x86_64", which
// deliberately maps to "linux" here — there is no separate chromeos key,
// since navigator.platform cannot distinguish ChromeOS from desktop Linux.
func osFromJSPlatform(v string) string {
	switch {
	case strings.HasPrefix(v, "Win"):
		return "windows"
	case strings.HasPrefix(v, "Mac"):
		return "macos"
	case strings.HasPrefix(v, "iPhone"), strings.HasPrefix(v, "iPad"), strings.HasPrefix(v, "iPod"):
		return "ios"
	case strings.HasPrefix(v, "Linux x86"), strings.HasPrefix(v, "Linux i"):
		return "linux"
	default:
		return ""
	}
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

// fetchMetadataAnomaly flags Sec-Fetch-* header combinations a conforming
// browser never emits: a navigation with an "empty" destination, or a
// "document" destination outside navigate mode. It reads the headers raw (they
// are per-request context, never hashed) and emits only when at least one
// Sec-Fetch-* header is present.
func fetchMetadataAnomaly(r *http.Request) (Signal, bool) {
	site := r.Header.Get("Sec-Fetch-Site")
	mode := r.Header.Get("Sec-Fetch-Mode")
	dest := r.Header.Get("Sec-Fetch-Dest")
	if site == "" && mode == "" && dest == "" {
		return Signal{}, false
	}
	anomaly := (mode == "navigate" && dest == "empty") ||
		(dest == "document" && mode != "" && mode != "navigate")
	return Signal{Name: "fetch-metadata-anomaly", Value: anomaly, Detail: "mode=" + mode + " dest=" + dest}, true
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
