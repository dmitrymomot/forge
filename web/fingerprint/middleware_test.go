package fingerprint_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/web/fingerprint"
)

func TestMiddlewareStashesResult(t *testing.T) {
	cfg := fingerprint.Config{Secret: "s", Version: 1, TokenTTL: time.Minute}
	fp, _ := fingerprint.New(cfg, fingerprint.WithCollectors(fingerprint.Headers()))
	var got fingerprint.Result
	var ok bool
	h := fp.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok = fingerprint.FromContext(r.Context())
	}))
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("User-Agent", "Mozilla/5.0")
	h.ServeHTTP(httptest.NewRecorder(), r)
	if !ok || got.Fingerprint.Hash == "" {
		t.Fatalf("result not stashed: ok=%v %+v", ok, got)
	}
}

func TestMiddlewareCollectorErrorNeverFailsRequest(t *testing.T) {
	cfg := fingerprint.Config{Secret: "s", Version: 1, TokenTTL: time.Minute}
	erroring := fingerprint.CollectorFunc(func(r *http.Request) ([]fingerprint.Component, error) {
		return nil, errors.New("boom")
	})
	fp, err := fingerprint.New(cfg, fingerprint.WithCollectors(erroring, fingerprint.Headers()))
	if err != nil {
		t.Fatal(err)
	}

	var nextCalled bool
	var got fingerprint.Result
	var ok bool
	h := fp.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		got, ok = fingerprint.FromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("User-Agent", "Mozilla/5.0")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if !nextCalled {
		t.Fatal("next handler did not run after collector error")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("response code = %d, want %d", w.Code, http.StatusOK)
	}
	if !ok || got.Fingerprint.Hash == "" {
		t.Fatalf("result not stashed despite a working collector: ok=%v %+v", ok, got)
	}
}

func TestLogExtractorNoResult(t *testing.T) {
	_, ok := fingerprint.LogExtractor(context.Background())
	if ok {
		t.Fatal("LogExtractor reported ok=true with no cached Result")
	}
}

func groupAttr(t *testing.T, attr slog.Attr, key string) (slog.Attr, bool) {
	t.Helper()
	if attr.Value.Kind() != slog.KindGroup {
		t.Fatalf("attr.Value.Kind() = %v, want %v", attr.Value.Kind(), slog.KindGroup)
	}
	for _, a := range attr.Value.Group() {
		if a.Key == key {
			return a, true
		}
	}
	return slog.Attr{}, false
}

func TestLogExtractorPopulatedNoSignals(t *testing.T) {
	cfg := fingerprint.Config{Secret: "s", Version: 1, TokenTTL: time.Minute}
	fp, err := fingerprint.New(cfg, fingerprint.WithCollectors(fingerprint.Headers()))
	if err != nil {
		t.Fatal(err)
	}

	var ctx context.Context
	h := fp.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx = r.Context()
	}))
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("User-Agent", "Mozilla/5.0")
	h.ServeHTTP(httptest.NewRecorder(), r)

	attr, ok := fingerprint.LogExtractor(ctx)
	if !ok {
		t.Fatal("LogExtractor reported ok=false with a cached Result")
	}
	if attr.Key != "fingerprint" {
		t.Fatalf("attr.Key = %q, want %q", attr.Key, "fingerprint")
	}
	if _, ok := groupAttr(t, attr, "hash"); !ok {
		t.Fatal(`"hash" key missing from fingerprint group`)
	}
	if _, ok := groupAttr(t, attr, "signals"); ok {
		t.Fatal(`"signals" key present despite no true signals`)
	}
}

func TestLogExtractorPopulatedWithSignal(t *testing.T) {
	cfg := fingerprint.Config{Secret: "s", Version: 1, TokenTTL: time.Minute}
	alwaysBot := func(ua string) (fingerprint.Family, bool) { return fingerprint.FamilyBot, true }
	fp, err := fingerprint.New(cfg,
		fingerprint.WithCollectors(fingerprint.Headers()),
		fingerprint.WithUAFamily(alwaysBot),
	)
	if err != nil {
		t.Fatal(err)
	}

	var ctx context.Context
	h := fp.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx = r.Context()
	}))
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("User-Agent", "Mozilla/5.0")
	h.ServeHTTP(httptest.NewRecorder(), r)

	attr, ok := fingerprint.LogExtractor(ctx)
	if !ok {
		t.Fatal("LogExtractor reported ok=false with a cached Result")
	}
	sig, ok := groupAttr(t, attr, "signals")
	if !ok {
		t.Fatal(`"signals" key missing despite a true signal`)
	}
	if sig.Value.String() != "bot-ua" {
		t.Fatalf("signals = %q, want %q", sig.Value.String(), "bot-ua")
	}
}
