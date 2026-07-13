# web/fingerprint Collector & Signal Completeness Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the fingerprinting coverage gaps in `web/fingerprint` — Client Hints, self-terminated-TLS, a deeper JS probe, two new cross-layer signals, and a usable `tls-ua-mismatch` — as one PR.

**Architecture:** Each addition is a new opt-in `Collector` or `Signal` following the package's existing `Name/Value` component idiom; nothing is wired by default except two preset edits. The `tlsprint` subpackage (which imports the parent) gains one collector; the parent gains everything else. Output stays identity + facts, never a score.

**Tech Stack:** Go (stdlib `crypto/tls`, `net/http`), embedded `probe.js` (vanilla ES5 + Promises), HMAC component hashing, signed cookies.

## Global Constraints

- Go 1.26; use `new(expr)` not a `ptr.To` wrapper; every counting loop is `for i := range N`, never C-style.
- Work only on the current branch `dm/web-fingerprint-collectors-665c19`; do not switch branches.
- Run `just fmt ./web/fingerprint/...` after editing Go files (package-path form — single-file form trips a spurious betteralign "undefined").
- Run `just lint` after the final task (runs vet, build, golangci-lint, nilaway, betteralign, modernize).
- Full test run: `just test ./web/fingerprint/...` (→ `go test -race -cover`).
- Tests are black-box (`package fingerprint_test` / `package tlsprint_test`) unless asserting unexported state.
- No Claude attribution in any commit message.
- The parent `fingerprint` package must NOT import `tlsprint` (tlsprint imports the parent — importing back is a cycle).
- Output is facts, never a score; a collector emits nothing when its input is absent, and never aborts the collector chain.

## Deviations from the spec (all reconciled in the committed spec)

1. `Antifraud()` does **not** auto-fold `RequestTLS()` (import cycle — see Global Constraints). Consumers compose it into the `tls Collector` argument; documented on `RequestTLS` and in `Antifraud`'s doc.
2. `js-tz-offset` dropped: its only consumer signal (`tz-offset-mismatch`) was cut, and the IANA `js-timezone` string already encodes the offset — pure redundant entropy.
3. `js-devicememory` and `js-dpr` are collected as **strings** (their JS values are floats), clamped like the existing string fields.

---

### Task 1: `ClientHints()` collector

**Files:**
- Create: `web/fingerprint/clienthints.go`
- Test: `web/fingerprint/clienthints_test.go`

**Interfaces:**
- Consumes: `headerPair` struct + `Component` type (from `headers.go`/`fingerprint.go`), `Collector` interface.
- Produces: `func ClientHints() Collector` — emits components `ch-ua`, `ch-ua-platform`, `ch-ua-mobile`, `ch-ua-arch`, `ch-ua-bitness`, `ch-ua-model`, `device-memory`, `dpr`.

- [ ] **Step 1: Write the failing test**

Create `web/fingerprint/clienthints_test.go`:

```go
package fingerprint_test

import (
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/web/fingerprint"
)

func TestClientHintsCollector(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Sec-CH-UA", `"Chromium";v="126", "Not.A/Brand";v="24"`)
	r.Header.Set("Sec-CH-UA-Platform", `"Windows"`)
	r.Header.Set("Sec-CH-UA-Mobile", "?0")
	r.Header.Set("Device-Memory", "8")
	// Sec-CH-UA-Arch, -Bitness, -Model, DPR absent.
	comps, err := fingerprint.ClientHints().Collect(r)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, c := range comps {
		got[c.Name] = c.Value
	}
	if got["ch-ua-platform"] != `"Windows"` || got["ch-ua-mobile"] != "?0" || got["device-memory"] != "8" {
		t.Fatalf("unexpected components: %v", got)
	}
	if _, ok := got["ch-ua-arch"]; ok {
		t.Fatalf("absent hint must not emit a component: %v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -race -run TestClientHintsCollector ./web/fingerprint/`
Expected: FAIL — `undefined: fingerprint.ClientHints`.

- [ ] **Step 3: Write minimal implementation**

Create `web/fingerprint/clienthints.go`:

```go
package fingerprint

import (
	"net/http"
	"strings"
)

// clientHintHeaders maps a User-Agent Client-Hint request header to a component
// name. Only stable device/browser identity hints are included. Deliberately
// excluded: Sec-CH-UA-Platform-Version and -Full-Version-List churn on every
// browser update and their entropy is already in the "ua" component; Sec-Fetch-*
// are per-request context (not device identity), so they feed signals raw
// instead of being hashed as components.
var clientHintHeaders = []headerPair{
	{"Sec-CH-UA", "ch-ua"},
	{"Sec-CH-UA-Platform", "ch-ua-platform"},
	{"Sec-CH-UA-Mobile", "ch-ua-mobile"},
	{"Sec-CH-UA-Arch", "ch-ua-arch"},
	{"Sec-CH-UA-Bitness", "ch-ua-bitness"},
	{"Sec-CH-UA-Model", "ch-ua-model"},
	{"Device-Memory", "device-memory"},
	{"DPR", "dpr"},
}

type clientHintsCollector struct{}

// ClientHints returns a Collector contributing the request's stable User-Agent
// Client Hints (Sec-CH-UA-*, Device-Memory, DPR) as components. Absent or blank
// hints contribute nothing. Modern Chromium browsers send these; others omit
// them, so this layer adds entropy only where available.
func ClientHints() Collector { return clientHintsCollector{} }

func (clientHintsCollector) Collect(r *http.Request) ([]Component, error) {
	comps := make([]Component, 0, len(clientHintHeaders))
	for _, h := range clientHintHeaders {
		if v := strings.TrimSpace(r.Header.Get(h.header)); v != "" {
			comps = append(comps, Component{Name: h.name, Value: v})
		}
	}
	return comps, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -race -run TestClientHintsCollector ./web/fingerprint/`
Expected: PASS.

- [ ] **Step 5: Format, then commit**

```bash
just fmt ./web/fingerprint/...
git add web/fingerprint/clienthints.go web/fingerprint/clienthints_test.go
git commit -m "feat(fingerprint): ClientHints collector for Sec-CH-UA device hints"
```

---

### Task 2: `tlsprint.RequestTLS()` collector

**Files:**
- Create: `web/fingerprint/tlsprint/request.go`
- Test: `web/fingerprint/tlsprint/request_test.go`

**Interfaces:**
- Consumes: `fingerprint.Collector`, `fingerprint.Component`.
- Produces: `func RequestTLS() fingerprint.Collector` — emits one component `tlsconn` = `"<version>|<cipher-hex>|<alpn>"`; nothing when `r.TLS == nil`.

- [ ] **Step 1: Write the failing test**

Create `web/fingerprint/tlsprint/request_test.go`:

```go
package tlsprint_test

import (
	"crypto/tls"
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/web/fingerprint/tlsprint"
)

func TestRequestTLSCollect(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.TLS = &tls.ConnectionState{
		Version:            tls.VersionTLS13,
		CipherSuite:        tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256, // 0xc02b
		NegotiatedProtocol: "h2",
	}
	comps, err := tlsprint.RequestTLS().Collect(r)
	if err != nil {
		t.Fatal(err)
	}
	if len(comps) != 1 || comps[0].Name != "tlsconn" || comps[0].Value != "1.3|c02b|h2" {
		t.Fatalf("unexpected component: %+v", comps)
	}
}

func TestRequestTLSPlaintextEmitsNothing(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil) // r.TLS == nil
	comps, err := tlsprint.RequestTLS().Collect(r)
	if err != nil || comps != nil {
		t.Fatalf("plaintext request must emit nothing: %+v, %v", comps, err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -race -run TestRequestTLS ./web/fingerprint/tlsprint/`
Expected: FAIL — `undefined: tlsprint.RequestTLS`.

- [ ] **Step 3: Write minimal implementation**

Create `web/fingerprint/tlsprint/request.go`:

```go
package tlsprint

import (
	"crypto/tls"
	"net/http"
	"strconv"

	"github.com/dmitrymomot/forge/web/fingerprint"
)

type requestTLSCollector struct{}

// RequestTLS returns a Collector that emits a coarse "tlsconn" component —
// "<version>|<cipher-hex>|<alpn>" (e.g. "1.3|c02b|h2") — from the request's own
// TLS handshake (http.Request.TLS). Use it when the Go server terminates
// crypto/tls directly, with no CDN JA header and no wrapped Listener. It is NOT
// a JA3/JA4 hash, so it uses a distinct component name and does not feed
// tls-ua-mismatch. A plaintext request (r.TLS == nil) contributes nothing.
//
// The parent fingerprint package cannot wire this into the Antifraud preset
// (that would import this subpackage back — a cycle), so compose it yourself:
//
//	tlsprint.Chain(tlsprint.CloudFrontJA4(trust), tlsprint.RequestTLS())
func RequestTLS() fingerprint.Collector { return requestTLSCollector{} }

func (requestTLSCollector) Collect(r *http.Request) ([]fingerprint.Component, error) {
	if r.TLS == nil {
		return nil, nil
	}
	v := tlsVersionString(r.TLS.Version) + "|" +
		strconv.FormatUint(uint64(r.TLS.CipherSuite), 16) + "|" +
		r.TLS.NegotiatedProtocol
	return []fingerprint.Component{{Name: "tlsconn", Value: v}}, nil
}

// tlsVersionString renders a TLS version constant compactly ("1.2", "1.3"),
// falling back to a hex code for unknown values.
func tlsVersionString(v uint16) string {
	switch v {
	case tls.VersionTLS13:
		return "1.3"
	case tls.VersionTLS12:
		return "1.2"
	case tls.VersionTLS11:
		return "1.1"
	case tls.VersionTLS10:
		return "1.0"
	default:
		return "0x" + strconv.FormatUint(uint64(v), 16)
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -race -run TestRequestTLS ./web/fingerprint/tlsprint/`
Expected: PASS.

- [ ] **Step 5: Format, then commit**

```bash
just fmt ./web/fingerprint/...
git add web/fingerprint/tlsprint/request.go web/fingerprint/tlsprint/request_test.go
git commit -m "feat(tlsprint): RequestTLS collector for self-terminated crypto/tls"
```

---

### Task 3: JS probe expansion — Go side (payload, clamp, collect, fix `hardwareConcurrency`)

**Files:**
- Modify: `web/fingerprint/jsprobe.go` (`probePayload`, `normalizeProbe`, `jsCollector.Collect`)
- Test: `web/fingerprint/jsprobe_test.go` (add), `web/fingerprint/jsprobe_fuzz_test.go` (extend seed)

**Interfaces:**
- Consumes: existing `IngestHandler`, `IssueToken`, `JSCollector`, `normalizeProbe`, `clampStr`.
- Produces: new components on collect — `js-hardware`, `js-screen`, `js-dpr`, `js-touch`, `js-devicememory`, `js-webgl-vendor`, `js-uadata`, `js-audio`, `js-fonts` (each emitted only when its value is non-empty / > 0).

- [ ] **Step 1: Write the failing test**

Add to `web/fingerprint/jsprobe_test.go`:

```go
func TestIngestCollectsExpandedProbe(t *testing.T) {
	cfg := fingerprint.Config{Secret: "s", Version: 1, TokenTTL: time.Minute}
	fp, err := fingerprint.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "203.0.113.7:5555"
	tok, err := fp.IssueToken(r)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{
		"token": tok,
		"data": map[string]any{
			"hardwareConcurrency": 8,
			"maxTouchPoints":      5,
			"deviceMemory":        "8",
			"screen":              "1920x1080x24",
			"devicePixelRatio":    "2",
			"webglVendor":         "Google Inc. (NVIDIA)",
			"uadata":              "Windows|15.0.0|||64",
			"audio":              "1a2b3c4d",
			"fonts":               "deadbeef:17",
		},
	})
	ingReq := httptest.NewRequest("POST", "/_fp/ingest", bytes.NewReader(body))
	ingReq.RemoteAddr = "203.0.113.7:5555"
	ingRec := httptest.NewRecorder()
	fp.IngestHandler().ServeHTTP(ingRec, ingReq)
	if ingRec.Code != http.StatusNoContent {
		t.Fatalf("ingest failed: %d", ingRec.Code)
	}
	next := httptest.NewRequest("GET", "/", nil)
	for _, c := range ingRec.Result().Cookies() {
		next.AddCookie(c)
	}
	comps, err := fp.JSCollector().Collect(next)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, c := range comps {
		got[c.Name] = c.Value
	}
	for name, want := range map[string]string{
		"js-hardware":     "8",
		"js-touch":        "5",
		"js-devicememory": "8",
		"js-screen":       "1920x1080x24",
		"js-dpr":          "2",
		"js-webgl-vendor": "Google Inc. (NVIDIA)",
		"js-uadata":       "Windows|15.0.0|||64",
		"js-audio":        "1a2b3c4d",
		"js-fonts":        "deadbeef:17",
	} {
		if got[name] != want {
			t.Fatalf("component %q = %q, want %q (all: %v)", name, got[name], want, got)
		}
	}
}

func TestExpandedProbeCookieFitsBudget(t *testing.T) {
	cfg := fingerprint.Config{Secret: "s", Version: 1, TokenTTL: time.Minute}
	fp, err := fingerprint.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "203.0.113.7:5555"
	tok, err := fp.IssueToken(r)
	if err != nil {
		t.Fatal(err)
	}
	big := strings.Repeat("Z", 200) // each string field over-length; clamping must bound it
	body, _ := json.Marshal(map[string]any{
		"token": tok,
		"data": map[string]any{
			"timezone": big, "platform": big, "canvas": big, "webgl": big,
			"webglVendor": big, "uadata": big, "audio": big, "fonts": big,
			"screen": big, "devicePixelRatio": big, "deviceMemory": big,
			"languages":           []string{big, big, big, big, big, big, big, big, big, big},
			"hardwareConcurrency": 64, "maxTouchPoints": 10,
		},
	})
	ingReq := httptest.NewRequest("POST", "/_fp/ingest", bytes.NewReader(body))
	ingReq.RemoteAddr = "203.0.113.7:5555"
	ingRec := httptest.NewRecorder()
	fp.IngestHandler().ServeHTTP(ingRec, ingReq)
	if ingRec.Code != http.StatusNoContent {
		t.Fatalf("ingest of max-fill payload failed: %d", ingRec.Code)
	}
	total := 0
	for _, c := range ingRec.Result().Cookies() {
		total += len(c.Value)
	}
	if total == 0 || total >= 4096 {
		t.Fatalf("clamped probe cookie payload = %d bytes, want in (0,4096)", total)
	}
}
```

Add `"strings"` to the `jsprobe_test.go` import block if not already present (it is present per the existing file).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -race -run 'TestIngestCollectsExpandedProbe|TestExpandedProbeCookieFitsBudget' ./web/fingerprint/`
Expected: FAIL — new components missing (`js-hardware` etc. empty).

- [ ] **Step 3: Write the implementation**

In `web/fingerprint/jsprobe.go`, replace the `probePayload` struct with:

```go
// probePayload is the whitelisted, clamped shape accepted from the browser.
type probePayload struct {
	Timezone            string   `json:"timezone"`
	Platform            string   `json:"platform"`
	Canvas              string   `json:"canvas"`
	WebGL               string   `json:"webgl"`
	WebGLVendor         string   `json:"webglVendor"`
	UAData              string   `json:"uadata"`
	Audio               string   `json:"audio"`
	Fonts               string   `json:"fonts"`
	Screen              string   `json:"screen"`
	DevicePixelRatio    string   `json:"devicePixelRatio"`
	DeviceMemory        string   `json:"deviceMemory"`
	Languages           []string `json:"languages"`
	HardwareConcurrency int      `json:"hardwareConcurrency"`
	MaxTouchPoints      int      `json:"maxTouchPoints"`
	WebDriver           bool     `json:"webdriver"`
}
```

Replace `normalizeProbe` with (keeps the existing clamps, adds the new fields):

```go
func normalizeProbe(p probePayload) probePayload {
	p.Timezone = clampStr(p.Timezone, 64)
	p.Platform = clampStr(p.Platform, 40)
	p.Canvas = clampStr(p.Canvas, 64)
	p.WebGL = clampStr(p.WebGL, 64)
	p.WebGLVendor = clampStr(p.WebGLVendor, 64)
	p.UAData = clampStr(p.UAData, 128)
	p.Audio = clampStr(p.Audio, 64)
	p.Fonts = clampStr(p.Fonts, 64)
	p.Screen = clampStr(p.Screen, 20)
	p.DevicePixelRatio = clampStr(p.DevicePixelRatio, 12)
	p.DeviceMemory = clampStr(p.DeviceMemory, 8)
	if p.HardwareConcurrency < 0 || p.HardwareConcurrency > 1024 {
		p.HardwareConcurrency = 0
	}
	if p.MaxTouchPoints < 0 || p.MaxTouchPoints > 256 {
		p.MaxTouchPoints = 0
	}
	if len(p.Languages) > 10 {
		p.Languages = p.Languages[:10]
	}
	for i := range p.Languages {
		p.Languages[i] = clampStr(p.Languages[i], 20)
	}
	return p
}
```

Replace the component-assembly block in `jsCollector.Collect` (the `comps := []Component{...}` through the final `return comps, nil`) with:

```go
	comps := []Component{
		{Name: "js-webdriver", Value: strconv.FormatBool(p.WebDriver)},
	}
	if p.Timezone != "" {
		comps = append(comps, Component{Name: "js-timezone", Value: p.Timezone})
	}
	if len(p.Languages) > 0 {
		comps = append(comps, Component{Name: "js-languages", Value: strings.Join(p.Languages, ",")})
	}
	if p.Platform != "" {
		comps = append(comps, Component{Name: "js-platform", Value: p.Platform})
	}
	if p.Canvas != "" {
		comps = append(comps, Component{Name: "js-canvas", Value: p.Canvas})
	}
	if p.WebGL != "" {
		comps = append(comps, Component{Name: "js-webgl", Value: p.WebGL})
	}
	if p.WebGLVendor != "" {
		comps = append(comps, Component{Name: "js-webgl-vendor", Value: p.WebGLVendor})
	}
	if p.UAData != "" {
		comps = append(comps, Component{Name: "js-uadata", Value: p.UAData})
	}
	if p.Audio != "" {
		comps = append(comps, Component{Name: "js-audio", Value: p.Audio})
	}
	if p.Fonts != "" {
		comps = append(comps, Component{Name: "js-fonts", Value: p.Fonts})
	}
	if p.Screen != "" {
		comps = append(comps, Component{Name: "js-screen", Value: p.Screen})
	}
	if p.DevicePixelRatio != "" {
		comps = append(comps, Component{Name: "js-dpr", Value: p.DevicePixelRatio})
	}
	if p.DeviceMemory != "" {
		comps = append(comps, Component{Name: "js-devicememory", Value: p.DeviceMemory})
	}
	if p.HardwareConcurrency > 0 {
		comps = append(comps, Component{Name: "js-hardware", Value: strconv.Itoa(p.HardwareConcurrency)})
	}
	if p.MaxTouchPoints > 0 {
		comps = append(comps, Component{Name: "js-touch", Value: strconv.Itoa(p.MaxTouchPoints)})
	}
	return comps, nil
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race -run 'TestIngestCollectsExpandedProbe|TestExpandedProbeCookieFitsBudget|TestIngestThenCollect' ./web/fingerprint/`
Expected: PASS (including the pre-existing `TestIngestThenCollect`).

- [ ] **Step 5: Extend the fuzz seed corpus**

In `web/fingerprint/jsprobe_fuzz_test.go`, add a second seed inside `FuzzIngest` right after the existing `f.Add(...)` line:

```go
	f.Add([]byte(`{"token":"x.y","data":{"screen":"9999x9999x99","uadata":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","fonts":"x","audio":"y","hardwareConcurrency":-5,"maxTouchPoints":99999,"deviceMemory":"0.5"}}`))
```

- [ ] **Step 6: Run the fuzzer briefly to confirm no panic**

Run: `go test -race -run FuzzIngest ./web/fingerprint/` (replays the seed corpus)
Expected: PASS (no panic on the oversized/negative fields).

- [ ] **Step 7: Format, then commit**

```bash
just fmt ./web/fingerprint/...
git add web/fingerprint/jsprobe.go web/fingerprint/jsprobe_test.go web/fingerprint/jsprobe_fuzz_test.go
git commit -m "feat(fingerprint): expand JS probe payload (device entropy) + fix dropped hardwareConcurrency"
```

---

### Task 4: JS probe expansion — `probe.js` (browser collection + async send)

**Files:**
- Modify: `web/fingerprint/assets/probe.js` (full rewrite)
- Test: `web/fingerprint/jsprobe_test.go` (add a served-content assertion)

**Interfaces:**
- Consumes: `window.__fp = {token, url, canvas, webgl, audio, fonts}` set by the page.
- Produces: a POST body `{token, data}` where `data` carries the fields Task 3 decodes. New `window.__fp` gates: `audio`, `fonts` (mirroring the existing `canvas`, `webgl` gates).

- [ ] **Step 1: Write the failing test**

Add to `web/fingerprint/jsprobe_test.go`:

```go
func TestScriptHandlerServesExpandedProbe(t *testing.T) {
	cfg := fingerprint.Config{Secret: "s", Version: 1, TokenTTL: time.Minute}
	fp, err := fingerprint.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	fp.ScriptHandler().ServeHTTP(rec, httptest.NewRequest("GET", "/_fp/probe.js", nil))
	body := rec.Body.String()
	for _, marker := range []string{"getHighEntropyValues", "OfflineAudioContext", "detectFonts", "webglVendor", "maxTouchPoints"} {
		if !strings.Contains(body, marker) {
			t.Fatalf("probe.js missing %q", marker)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -race -run TestScriptHandlerServesExpandedProbe ./web/fingerprint/`
Expected: FAIL — markers absent from the current probe.js.

- [ ] **Step 3: Rewrite `web/fingerprint/assets/probe.js`**

Replace the entire file with:

```js
// forge web/fingerprint browser probe. Reads window.__fp = {token, url, canvas,
// webgl, audio, fonts} set by the page, collects a device signal payload, and
// POSTs it once after any async collectors resolve.
(function () {
  var cfg = window.__fp || {};
  if (!cfg.token || !cfg.url) return;
  var s = window.screen || {};
  var d = {
    timezone: (Intl.DateTimeFormat().resolvedOptions().timeZone) || "",
    languages: (navigator.languages || []).slice(0, 10),
    platform: navigator.platform || "",
    hardwareConcurrency: navigator.hardwareConcurrency || 0,
    maxTouchPoints: navigator.maxTouchPoints || 0,
    deviceMemory: navigator.deviceMemory ? String(navigator.deviceMemory) : "",
    screen: (s.width || 0) + "x" + (s.height || 0) + "x" + (s.colorDepth || 0),
    devicePixelRatio: window.devicePixelRatio ? String(window.devicePixelRatio) : "",
    webdriver: navigator.webdriver === true
  };
  if (cfg.canvas) {
    try {
      var c = document.createElement("canvas");
      var g = c.getContext("2d");
      g.textBaseline = "top";
      g.font = "14px 'Arial'";
      g.fillText("forge-fp", 2, 2);
      d.canvas = c.toDataURL().slice(-64);
    } catch (e) {}
  }
  if (cfg.webgl) {
    try {
      var gl = document.createElement("canvas").getContext("webgl");
      var ext = gl.getExtension("WEBGL_debug_renderer_info");
      if (ext) {
        d.webgl = String(gl.getParameter(ext.UNMASKED_RENDERER_WEBGL)).slice(0, 64);
        d.webglVendor = String(gl.getParameter(ext.UNMASKED_VENDOR_WEBGL)).slice(0, 64);
      }
    } catch (e) {}
  }
  if (cfg.fonts) {
    try { d.fonts = detectFonts(); } catch (e) {}
  }

  var tasks = [];
  if (cfg.audio) {
    tasks.push(audioHash().then(function (h) { d.audio = h; }).catch(function () {}));
  }
  if (navigator.userAgentData && navigator.userAgentData.getHighEntropyValues) {
    tasks.push(navigator.userAgentData
      .getHighEntropyValues(["platform", "platformVersion", "model", "architecture", "bitness"])
      .then(function (h) {
        d.uadata = [h.platform, h.platformVersion, h.model, h.architecture, h.bitness].join("|").slice(0, 128);
      }).catch(function () {}));
  }

  function send() {
    try {
      fetch(cfg.url, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ token: cfg.token, data: d }),
        keepalive: true
      });
    } catch (e) {}
  }

  // hash32 is a tiny FNV-1a string hash rendered as 8 hex chars. NOT
  // cryptographic — only a compact, stable identifier for a collected value.
  function hash32(str) {
    var h = 0x811c9dc5;
    for (var i = 0; i < str.length; i++) {
      h ^= str.charCodeAt(i);
      h = (h * 0x01000193) >>> 0;
    }
    return ("0000000" + h.toString(16)).slice(-8);
  }

  // detectFonts returns "hash:count" where the hash encodes which probe fonts
  // are installed, detected via measureText width/height deltas against generic
  // baseline families. Never returns the raw font list.
  function detectFonts() {
    var probes = ["Arial", "Helvetica", "Times New Roman", "Courier New", "Georgia",
      "Verdana", "Tahoma", "Trebuchet MS", "Impact", "Comic Sans MS", "Segoe UI",
      "Roboto", "Ubuntu", "Cantarell", "Menlo", "Monaco", "Consolas", "Calibri",
      "Cambria", "Garamond", "Palatino", "Franklin Gothic", "Century Gothic",
      "Lucida Console", "MS Gothic", "Meiryo", "SimSun", "Noto Sans", "Open Sans",
      "Liberation Sans", "DejaVu Sans", "Droid Sans", "PT Sans", "Source Sans Pro",
      "Fira Sans", "Inter", "Helvetica Neue", "Andale Mono", "Courier", "Roboto Mono"];
    var baseFonts = ["monospace", "sans-serif", "serif"];
    var span = document.createElement("span");
    span.style.position = "absolute";
    span.style.left = "-9999px";
    span.style.fontSize = "72px";
    span.textContent = "mmmmmmmmmmlli";
    document.body.appendChild(span);
    var base = {};
    for (var i = 0; i < baseFonts.length; i++) {
      span.style.fontFamily = baseFonts[i];
      base[baseFonts[i]] = { w: span.offsetWidth, h: span.offsetHeight };
    }
    var bits = "", count = 0;
    for (var j = 0; j < probes.length; j++) {
      var found = false;
      for (var k = 0; k < baseFonts.length; k++) {
        span.style.fontFamily = "'" + probes[j] + "'," + baseFonts[k];
        if (span.offsetWidth !== base[baseFonts[k]].w || span.offsetHeight !== base[baseFonts[k]].h) {
          found = true;
          break;
        }
      }
      bits += found ? "1" : "0";
      if (found) count++;
    }
    document.body.removeChild(span);
    return hash32(bits) + ":" + count;
  }

  // audioHash renders a short OfflineAudioContext buffer and hashes the summed
  // output magnitude — a stable per-device/-browser value.
  function audioHash() {
    var AC = window.OfflineAudioContext || window.webkitOfflineAudioContext;
    if (!AC) return Promise.reject();
    var ctx = new AC(1, 5000, 44100);
    var osc = ctx.createOscillator();
    osc.type = "triangle";
    osc.frequency.value = 10000;
    var comp = ctx.createDynamicsCompressor();
    osc.connect(comp);
    comp.connect(ctx.destination);
    osc.start(0);
    return ctx.startRendering().then(function (buf) {
      var data = buf.getChannelData(0);
      var acc = 0;
      for (var i = 0; i < data.length; i++) acc += Math.abs(data[i]);
      return hash32(acc.toString());
    });
  }

  if (tasks.length) {
    // Send after async collectors resolve, but never block past 800ms.
    var timeout = new Promise(function (res) { setTimeout(res, 800); });
    Promise.race([Promise.all(tasks), timeout]).then(send);
  } else {
    send();
  }
})();
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race -run 'TestScriptHandlerServesExpandedProbe|TestScriptHandlerServesJS' ./web/fingerprint/`
Expected: PASS (the SRI/`window.__fp` assertion in the pre-existing test still holds; the embedded bytes changed but its ETag is recomputed from them).

- [ ] **Step 5: Commit** (no Go formatting needed — only an asset + a test file already covered by Step 4)

```bash
git add web/fingerprint/assets/probe.js web/fingerprint/jsprobe_test.go
git commit -m "feat(fingerprint): probe.js collects screen/audio/fonts/uadata, async send"
```

---

### Task 5: `ch-ua-mismatch` signal

**Files:**
- Modify: `web/fingerprint/signals_component.go` (add call in `componentSignals`, add `chUAMismatch` + two helpers)
- Test: `web/fingerprint/signals_component_test.go`

**Interfaces:**
- Consumes: components `ch-ua-platform` (Task 1) and `js-platform` (Task 3).
- Produces: signal `ch-ua-mismatch`, emitted only when both components are present and both normalize to a known OS.

- [ ] **Step 1: Write the failing test**

Add to `web/fingerprint/signals_component_test.go`:

```go
func TestCHUAMismatchSignal(t *testing.T) {
	newFP := func(chPlatform, jsPlatform string) (*fingerprint.Fingerprinter, *http.Request) {
		cfg := fingerprint.Config{Secret: "s", Version: 1, TokenTTL: time.Minute}
		fp, err := fingerprint.New(cfg, fingerprint.WithCollectors(
			fingerprint.CollectorFunc(func(_ *http.Request) ([]fingerprint.Component, error) {
				return []fingerprint.Component{
					{Name: "ch-ua-platform", Value: chPlatform},
					{Name: "js-platform", Value: jsPlatform},
				}, nil
			}),
		))
		if err != nil {
			t.Fatal(err)
		}
		return fp, httptest.NewRequest("GET", "/", nil)
	}

	// Contradiction: CH says Windows, JS says a Mac.
	fp, r := newFP(`"Windows"`, "MacIntel")
	f, _ := fp.FromRequest(r)
	if s, ok := signalByName(fp.Signals(r, f), "ch-ua-mismatch"); !ok || !s.Value {
		t.Fatalf("expected ch-ua-mismatch=true: %+v", fp.Signals(r, f))
	}

	// Agreement: CH Windows, JS Win32.
	fp, r = newFP(`"Windows"`, "Win32")
	f, _ = fp.FromRequest(r)
	if s, ok := signalByName(fp.Signals(r, f), "ch-ua-mismatch"); !ok || s.Value {
		t.Fatalf("expected ch-ua-mismatch=false: %+v", fp.Signals(r, f))
	}

	// Ambiguous JS platform (Android/desktop Linux share "Linux armv8l") → not emitted.
	fp, r = newFP(`"Android"`, "Linux armv8l")
	f, _ = fp.FromRequest(r)
	if _, ok := signalByName(fp.Signals(r, f), "ch-ua-mismatch"); ok {
		t.Fatal("ch-ua-mismatch must not emit on an ambiguous js-platform")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -race -run TestCHUAMismatchSignal ./web/fingerprint/`
Expected: FAIL — signal never emitted.

- [ ] **Step 3: Write the implementation**

In `web/fingerprint/signals_component.go`, add to `componentSignals` (after the `langMismatch` block, before `geoTZMismatch`):

```go
	if s, ok := chUAMismatch(comp); ok {
		out = append(out, s)
	}
```

Then add these functions to the same file:

```go
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
// OS key, or "" when unknown.
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
		return "chromeos"
	case "Linux":
		return "linux"
	default:
		return ""
	}
}

// osFromJSPlatform maps a navigator.platform value to a coarse OS key, or ""
// when unknown or ambiguous (bare "Linux arm*" is shared by Android and desktop
// Linux, so it is treated as unknown).
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
	case strings.Contains(v, "CrOS"):
		return "chromeos"
	default:
		return ""
	}
}
```

`strings` is already imported in this file.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -race -run TestCHUAMismatchSignal ./web/fingerprint/`
Expected: PASS.

- [ ] **Step 5: Format, then commit**

```bash
just fmt ./web/fingerprint/...
git add web/fingerprint/signals_component.go web/fingerprint/signals_component_test.go
git commit -m "feat(fingerprint): ch-ua-mismatch signal (Client-Hint platform vs navigator.platform)"
```

---

### Task 6: `fetch-metadata-anomaly` signal

**Files:**
- Modify: `web/fingerprint/signals_component.go` (add call in `componentSignals`, add `fetchMetadataAnomaly`)
- Test: `web/fingerprint/signals_component_test.go`

**Interfaces:**
- Consumes: raw `Sec-Fetch-Site/Mode/Dest` request headers (read from `r`, never hashed).
- Produces: signal `fetch-metadata-anomaly`, emitted only when at least one `Sec-Fetch-*` header is present.

- [ ] **Step 1: Write the failing test**

Add to `web/fingerprint/signals_component_test.go`:

```go
func TestFetchMetadataAnomalySignal(t *testing.T) {
	cfg := fingerprint.Config{Secret: "s", Version: 1, TokenTTL: time.Minute}
	fp, err := fingerprint.New(cfg, fingerprint.WithCollectors(fingerprint.Headers()))
	if err != nil {
		t.Fatal(err)
	}

	// Contradiction a real browser never sends: navigate mode with empty dest.
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Sec-Fetch-Mode", "navigate")
	r.Header.Set("Sec-Fetch-Dest", "empty")
	f, _ := fp.FromRequest(r)
	if s, ok := signalByName(fp.Signals(r, f), "fetch-metadata-anomaly"); !ok || !s.Value {
		t.Fatalf("expected fetch-metadata-anomaly=true: %+v", fp.Signals(r, f))
	}

	// Normal top-level navigation.
	r2 := httptest.NewRequest("GET", "/", nil)
	r2.Header.Set("Sec-Fetch-Site", "none")
	r2.Header.Set("Sec-Fetch-Mode", "navigate")
	r2.Header.Set("Sec-Fetch-Dest", "document")
	f2, _ := fp.FromRequest(r2)
	if s, ok := signalByName(fp.Signals(r2, f2), "fetch-metadata-anomaly"); !ok || s.Value {
		t.Fatalf("expected fetch-metadata-anomaly=false: %+v", fp.Signals(r2, f2))
	}

	// No Sec-Fetch-* headers at all → not emitted.
	r3 := httptest.NewRequest("GET", "/", nil)
	f3, _ := fp.FromRequest(r3)
	if _, ok := signalByName(fp.Signals(r3, f3), "fetch-metadata-anomaly"); ok {
		t.Fatal("fetch-metadata-anomaly must not emit without any Sec-Fetch-* header")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -race -run TestFetchMetadataAnomalySignal ./web/fingerprint/`
Expected: FAIL — signal never emitted.

- [ ] **Step 3: Write the implementation**

In `web/fingerprint/signals_component.go`, add to `componentSignals` (after the `headerAnomaly` block):

```go
	if s, ok := fetchMetadataAnomaly(r); ok {
		out = append(out, s)
	}
```

Then add this function to the same file:

```go
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
```

`net/http` is already imported in this file.

Then update the `componentSignals` doc comment (top of the file) so it stays honest — it currently reads "derives the component-driven signals: headless, tls-ua-mismatch, lang-mismatch, geo-tz-mismatch, and header-anomaly." Change it to list all seven: "headless, tls-ua-mismatch, lang-mismatch, ch-ua-mismatch, geo-tz-mismatch, header-anomaly, and fetch-metadata-anomaly."

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -race -run TestFetchMetadataAnomalySignal ./web/fingerprint/`
Expected: PASS.

- [ ] **Step 5: Format, then commit**

```bash
just fmt ./web/fingerprint/...
git add web/fingerprint/signals_component.go web/fingerprint/signals_component_test.go
git commit -m "feat(fingerprint): fetch-metadata-anomaly signal from Sec-Fetch-* headers"
```

---

### Task 7: `WithAutomationJA4` — make `tls-ua-mismatch` usable

**Files:**
- Modify: `web/fingerprint/collector.go` (add `automationJA4` field to `Fingerprinter`)
- Modify: `web/fingerprint/signals_component.go` (drop the package var; read `fp.automationJA4`)
- Modify: `web/fingerprint/options.go` (add `WithAutomationJA4`)
- Test: `web/fingerprint/signals_component_test.go`

**Interfaces:**
- Consumes: existing `tlsUAMismatch` inspector, `tls` + `ua` components, the UA seam.
- Produces: `func WithAutomationJA4(m map[string]string) Option`; `tls-ua-mismatch` fires `Value:true` when the `tls` component matches a pinned key under a browser-family UA.

- [ ] **Step 1: Write the failing test**

Add to `web/fingerprint/signals_component_test.go`:

```go
func TestTLSUAMismatchFiresWithPinnedJA4(t *testing.T) {
	cfg := fingerprint.Config{Secret: "s", Version: 1, TokenTTL: time.Minute}
	fp, err := fingerprint.New(cfg,
		fingerprint.WithCollectors(
			fingerprint.CollectorFunc(func(_ *http.Request) ([]fingerprint.Component, error) {
				return []fingerprint.Component{
					{Name: "tls", Value: "t13d1516h2_8daaf6152771_02713d6af862"},
					{Name: "ua", Value: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/126.0"},
				}, nil
			}),
		),
		fingerprint.WithUAFamily(func(_ string) (fingerprint.Family, bool) {
			return fingerprint.FamilyBrowser, true
		}),
		fingerprint.WithAutomationJA4(map[string]string{
			"t13d1516h2_8daaf6152771_02713d6af862": "python-requests",
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/", nil)
	f, _ := fp.FromRequest(r)
	s, ok := signalByName(fp.Signals(r, f), "tls-ua-mismatch")
	if !ok || !s.Value || s.Detail != "python-requests" {
		t.Fatalf("expected tls-ua-mismatch=true detail=python-requests: %+v", s)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -race -run TestTLSUAMismatchFiresWithPinnedJA4 ./web/fingerprint/`
Expected: FAIL — `undefined: fingerprint.WithAutomationJA4`.

- [ ] **Step 3: Add the field, the option, and switch the lookup**

In `web/fingerprint/collector.go`, add a field to the `Fingerprinter` struct (place it among the reference-type fields; `just fmt` will realign):

```go
	automationJA4 map[string]string
```

In `web/fingerprint/signals_component.go`, delete the package-level `var automationJA4 = map[string]string{ ... }` block entirely (the whole `var` with its comment), and change the lookup in `tlsUAMismatch` from:

```go
	label, flagged := automationJA4[tls]
```

to:

```go
	label, flagged := fp.automationJA4[tls]
```

(reading a nil map is safe — it yields `"", false`, preserving the ships-empty behavior). Update the two doc comments in that file that say "until automationJA4 is populated" to "until WithAutomationJA4 is set".

In `web/fingerprint/options.go`, add `"maps"` to the import block and append:

```go
// WithAutomationJA4 pins non-browser JA4 client fingerprints to labels so the
// tls-ua-mismatch signal fires (Value:true) when the "tls" component matches a
// pinned fingerprint under a browser-family UA. Ships empty by design — pinned
// TLS fingerprints drift as tools update, so populate this from fingerprints you
// capture from your own traffic (see the tlsprint.Listener / Conn.JA4() capture
// recipe in the package doc). The map is cloned.
func WithAutomationJA4(m map[string]string) Option {
	return func(fp *Fingerprinter) { fp.automationJA4 = maps.Clone(m) }
}
```

- [ ] **Step 4: Run tests to verify pass (including the pre-existing empty-map case)**

Run: `go test -race -run 'TestTLSUAMismatch' ./web/fingerprint/`
Expected: PASS — the new pinned test fires true, and the existing `TestTLSUAMismatchSignal` still gets `Value:false` (default nil map).

- [ ] **Step 5: Format, then commit**

```bash
just fmt ./web/fingerprint/...
git add web/fingerprint/collector.go web/fingerprint/signals_component.go web/fingerprint/options.go
git commit -m "feat(fingerprint): WithAutomationJA4 option makes tls-ua-mismatch usable"
```

---

### Task 8: Presets + docs

**Files:**
- Modify: `web/fingerprint/presets.go` (wire `ClientHints()` into both presets)
- Modify: `web/fingerprint/doc.go` (expand package doc: new collectors, capture recipe, icebox)
- Test: `web/fingerprint/presets_test.go`

**Interfaces:**
- Consumes: `ClientHints()` (Task 1).
- Produces: `Session`/`Antifraud` now emit Client-Hint components when the request carries them.

- [ ] **Step 1: Write the failing test**

Add to `web/fingerprint/presets_test.go`:

```go
func TestSessionPresetEmitsClientHints(t *testing.T) {
	cfg := fingerprint.Config{Secret: "s", Version: 1, TokenTTL: time.Minute}
	fp, err := fingerprint.Session(cfg)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("User-Agent", "Mozilla/5.0")
	r.Header.Set("Sec-CH-UA-Platform", `"Windows"`)
	f, _ := fp.FromRequest(r)
	found := false
	for _, c := range f.Components {
		if c.Name == "ch-ua-platform" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected ch-ua-platform component from Session preset, got %+v", f.Components)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -race -run TestSessionPresetEmitsClientHints ./web/fingerprint/`
Expected: FAIL — no `ch-ua-platform` component (preset doesn't wire `ClientHints()` yet).

- [ ] **Step 3: Wire the presets**

In `web/fingerprint/presets.go`, change `Session`'s base to:

```go
	base := []Option{WithCollectors(Headers(), ClientHints())}
```

and `Antifraud`'s `cols` to:

```go
	cols := []Collector{Headers(), ClientIP(), ClientHints()}
```

Update `Session`'s doc comment to note it now also collects Client Hints, and `Antifraud`'s doc comment to add: "Client Hints are included; compose `tlsprint.RequestTLS()` into the `tls` argument yourself for self-terminated TLS (this package cannot import `tlsprint`)."

- [ ] **Step 4: Run tests to verify pass**

Run: `go test -race -run 'TestSessionPreset|TestAntifraudPreset' ./web/fingerprint/`
Expected: PASS (new test + both pre-existing preset tests).

- [ ] **Step 5: Expand `web/fingerprint/doc.go`**

Replace the package comment in `web/fingerprint/doc.go` with:

```go
// Package fingerprint turns an HTTP request into a versioned identity plus
// structured anti-fraud signals, from headers alone up to a full TLS + JS device
// probe. Layers are opt-in Collectors; heavy lookups (geoip, useragent) enter
// through wired function seams so the core stays stdlib-light. Output is facts,
// never a score — weighting signals into a decision is the consumer's policy.
//
// Collectors: Headers (UA + Accept*), ClientHints (Sec-CH-UA-* + Device-Memory
// + DPR), ClientIP, and the JS probe (JSCollector, served by ScriptHandler and
// fed by IngestHandler). TLS fingerprints come from the tlsprint subpackage:
// trusted-proxy header sources (Cloudflare/CloudFront/generic), a local
// raw-ClientHello JA4 computation, and RequestTLS for self-terminated crypto/tls.
//
// Making tls-ua-mismatch fire: it stays inert until WithAutomationJA4 pins the
// JA4 fingerprints of non-browser clients you observe. Pinned TLS fingerprints
// drift as tools update, so harvest them from your own traffic rather than
// shipping a static list — wrap your listener with tlsprint.Listener, read
// tlsprint.Conn.JA4() per connection (via ConnContext), record the fingerprints
// arriving under automation User-Agents, and pass that map to WithAutomationJA4.
//
// Out of scope: JA4H (the HTTP-layer fingerprint) and the HTTP/2 SETTINGS
// fingerprint both need raw header/frame order, which net/http discards and
// HTTP/2 normalizes — they cannot be computed faithfully from a handler.
package fingerprint
```

- [ ] **Step 6: Commit**

```bash
just fmt ./web/fingerprint/...
git add web/fingerprint/presets.go web/fingerprint/doc.go web/fingerprint/presets_test.go
git commit -m "feat(fingerprint): wire ClientHints into presets; document collectors, capture recipe, icebox"
```

---

### Task 9: Full verification

- [ ] **Step 1: Run the full package test suite with race + coverage**

Run: `just test ./web/fingerprint/...`
Expected: PASS, no data races. Includes the tlsprint subpackage and the fuzz seed corpora.

- [ ] **Step 2: Run the full linter**

Run: `just lint`
Expected: clean — no vet, golangci-lint, nilaway, betteralign, or modernize findings. If betteralign reports the `Fingerprinter` struct, it was already auto-applied by `just fmt`; re-run `just fmt ./web/fingerprint/...` and re-lint.

- [ ] **Step 3: Confirm godoc examples still build**

Run: `go test -race -run Example ./web/fingerprint/`
Expected: PASS (`ExampleAntifraud` output unchanged — it only checks `bot-ua`).

- [ ] **Step 4: Final nothing-to-commit check**

Run: `git status --short`
Expected: clean working tree (all task commits landed).

---

## Self-Review

**Spec coverage:**
- Unit 1 ClientHints → Task 1 ✓
- Unit 2 RequestTLS → Task 2 ✓
- Unit 3 JS probe (hardwareConcurrency fix + entropy fields, cookie guard, fuzz) → Tasks 3 (Go) + 4 (probe.js) ✓
- Unit 4 signals (ch-ua-mismatch, fetch-metadata-anomaly) → Tasks 5 + 6 ✓
- Unit 5 WithAutomationJA4 + capture recipe → Task 7 (option) + Task 8 (recipe in doc.go) ✓
- Unit 6 presets + docs → Task 8 ✓
- Icebox (JA4H, h2 SETTINGS) → documented in doc.go (Task 8) ✓
- Cross-cutting hash-change note → covered by doc.go prose + no Version gate (spec-consistent) ✓

**Type consistency:**
- `Component{Name, Value}`, `Signal{Name, Detail, Value}`, `Collector`, `Option` — all match existing signatures.
- `probePayload` JSON tags (`webglVendor`, `uadata`, `deviceMemory`, …) match the keys the Task-3 test and Task-4 `probe.js` send.
- `fp.automationJA4` field (Task 7) read in `signals_component.go`, written by `WithAutomationJA4` — names consistent.
- Component names emitted by `ClientHints()` (`ch-ua-platform`) match the name read by `chUAMismatch` (Task 5) and asserted in Task 8's preset test.

**Placeholder scan:** none — every step carries full code and exact commands.
