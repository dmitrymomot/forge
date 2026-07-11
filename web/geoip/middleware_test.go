package geoip_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/web/geoip"
)

func TestMiddlewareCachesLocation(t *testing.T) {
	src := fakeSource{loc: geoip.Location{CountryCode: "CA", City: "Toronto"}}
	var got geoip.Location
	var ran bool
	h := geoip.Middleware(src)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = geoip.Get(r)
		_, ran = geoip.From(r.Context())
	}))
	h.ServeHTTP(httptest.NewRecorder(), req(nil))
	if !ran {
		t.Fatal("From should report the middleware ran")
	}
	if got.CountryCode != "CA" || got.City != "Toronto" {
		t.Fatalf("cached %+v, want CA/Toronto", got)
	}
}

func TestFromWithoutMiddleware(t *testing.T) {
	if _, ok := geoip.From(context.Background()); ok {
		t.Fatal("From should be false when middleware did not run")
	}
	if !geoip.Get(req(nil)).Empty() {
		t.Fatal("Get should be empty without middleware")
	}
}

func TestMiddlewareErrorCachesEmptyButRuns(t *testing.T) {
	src := fakeSource{err: context.DeadlineExceeded}
	var ran bool
	var loc geoip.Location
	h := geoip.Middleware(src)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		loc, ran = func() (geoip.Location, bool) { l, ok := geoip.From(r.Context()); return l, ok }()
	}))
	h.ServeHTTP(httptest.NewRecorder(), req(nil))
	if !ran || !loc.Empty() {
		t.Fatalf("want ran=true empty loc, got ran=%v loc=%+v", ran, loc)
	}
}

func TestLogExtractor(t *testing.T) {
	src := fakeSource{loc: geoip.Location{CountryCode: "US", ASN: 13335}}
	var attr string
	var present bool
	h := geoip.Middleware(src)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		a, ok := geoip.LogExtractor(r.Context())
		present = ok
		attr = a.Key
	}))
	h.ServeHTTP(httptest.NewRecorder(), req(nil))
	if !present || attr != "geo" {
		t.Fatalf("LogExtractor present=%v key=%q, want true/geo", present, attr)
	}

	if _, ok := geoip.LogExtractor(context.Background()); ok {
		t.Fatal("LogExtractor should be absent without a cached Location")
	}
}
