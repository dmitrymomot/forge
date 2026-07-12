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
