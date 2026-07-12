package fingerprint_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/web/fingerprint"
)

func TestScriptHandlerServesJS(t *testing.T) {
	cfg := fingerprint.Config{Secret: "s", Version: 1, TokenTTL: time.Minute}
	fp, err := fingerprint.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	fp.ScriptHandler().ServeHTTP(rec, httptest.NewRequest("GET", "/_fp/probe.js", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "window.__fp") {
		t.Fatalf("script not served: %d", rec.Code)
	}
	if !strings.HasPrefix(fp.ProbeSRI(), "sha384-") {
		t.Fatalf("bad SRI: %s", fp.ProbeSRI())
	}
}

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

func TestIngestThenCollect(t *testing.T) {
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
		"data":  map[string]any{"timezone": "Asia/Tokyo", "languages": []string{"ja-JP", "ja"}, "webdriver": true},
	})
	ingReq := httptest.NewRequest("POST", "/_fp/ingest", bytes.NewReader(body))
	ingReq.RemoteAddr = "203.0.113.7:5555"
	ingRec := httptest.NewRecorder()
	fp.IngestHandler().ServeHTTP(ingRec, ingReq)
	if ingRec.Code != http.StatusNoContent {
		t.Fatalf("ingest failed: %d", ingRec.Code)
	}

	// Replay the Set-Cookie onto a new request and collect.
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
	if got["js-timezone"] != "Asia/Tokyo" || got["js-webdriver"] != "true" {
		t.Fatalf("collected wrong payload: %v", got)
	}
}

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
			"audio":               "1a2b3c4d",
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

func TestIngestRejectsBadToken(t *testing.T) {
	cfg := fingerprint.Config{Secret: "s", Version: 1, TokenTTL: time.Minute}
	fp, err := fingerprint.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"token": "bogus.sig", "data": map[string]any{}})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/_fp/ingest", bytes.NewReader(body))
	fp.IngestHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}
