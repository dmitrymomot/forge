package frankfurter_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/core/decimal"
	"github.com/dmitrymomot/forge/finance/fxrate"
	"github.com/dmitrymomot/forge/finance/fxrate/frankfurter"
)

func newServer(t *testing.T, handler http.HandlerFunc) *frankfurter.Source {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return frankfurter.New(
		frankfurter.WithBaseURL(srv.URL+"/"), // trailing slash must be tolerated
		frankfurter.WithHTTPClient(srv.Client()),
	)
}

func TestFetch(t *testing.T) {
	t.Parallel()

	src := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/latest" {
			t.Errorf("path = %q, want /latest", r.URL.Path)
		}
		if got := r.URL.Query().Get("base"); got != "EUR" {
			t.Errorf("base = %q, want EUR", got)
		}
		if got := r.URL.Query().Get("symbols"); got != "USD,GBP" {
			t.Errorf("symbols = %q, want USD,GBP", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"base":"EUR","date":"2026-07-18","rates":{"USD":1.0850,"GBP":0.8425}}`))
	})

	snap, err := src.Fetch(t.Context(), "eur", []string{"usd", "gbp"})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if snap.Base() != "EUR" || snap.Provider() != frankfurter.Provider {
		t.Fatalf("snapshot metadata: base %q provider %q", snap.Base(), snap.Provider())
	}
	if want := time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC); !snap.AsOf().Equal(want) {
		t.Fatalf("AsOf = %v, want %v", snap.AsOf(), want)
	}

	// Rates decode exactly — the bare JSON number never touches float64.
	r, err := snap.Rate("EUR", "USD")
	if err != nil {
		t.Fatalf("Rate: %v", err)
	}
	if !r.Value.Equal(decimal.MustParse("1.0850")) {
		t.Fatalf("USD rate = %s, want 1.0850", r.Value)
	}
}

func TestFetchAllQuotesOmitsSymbols(t *testing.T) {
	t.Parallel()

	src := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("symbols") {
			t.Errorf("symbols sent for a fetch-all: %q", r.URL.Query().Get("symbols"))
		}
		_, _ = w.Write([]byte(`{"base":"EUR","date":"2026-07-18","rates":{"USD":1.0850}}`))
	})

	if _, err := src.Fetch(t.Context(), "EUR", nil); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
}

func TestFetchErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantIs  error
	}{
		{"non-200 status", func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "nope", http.StatusNotFound)
		}, nil},
		{"malformed json", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"base":"EUR","date":`))
		}, nil},
		{"bad date", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"base":"EUR","date":"18-07-2026","rates":{"USD":1.0850}}`))
		}, nil},
		{"negative rate fails closed", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"base":"EUR","date":"2026-07-18","rates":{"USD":-1}}`))
		}, fxrate.ErrInvalidRate},
		{"empty rates", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"base":"EUR","date":"2026-07-18","rates":{}}`))
		}, fxrate.ErrInvalidSnapshot},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			src := newServer(t, tt.handler)
			_, err := src.Fetch(t.Context(), "EUR", nil)
			if err == nil {
				t.Fatal("Fetch succeeded, want error")
			}
			if tt.wantIs != nil && !errors.Is(err, tt.wantIs) {
				t.Fatalf("got %v, want %v", err, tt.wantIs)
			}
		})
	}
}

func TestFetchEmptyBase(t *testing.T) {
	t.Parallel()

	src := frankfurter.New()
	if _, err := src.Fetch(t.Context(), "  ", nil); err == nil {
		t.Fatal("Fetch succeeded with empty base, want error")
	}
}
