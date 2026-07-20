package postback_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/dmitrymomot/forge/comms/postback"
)

// newServer returns an httptest server plus a Sender wired to its plain
// client (no retries), so status-class tests stay single-attempt and fast.
func newServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *postback.Sender) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv, postback.New(postback.WithHTTPClient(srv.Client()))
}

func destFor(t *testing.T, raw string, opts ...postback.DestinationOption) postback.Destination {
	t.Helper()
	vocab := mustVocab(t, "click_id", "payout", "sub1")
	d, err := postback.NewDestination(raw, vocab, opts...)
	if err != nil {
		t.Fatalf("NewDestination(%q): %v", raw, err)
	}
	return d
}

func TestSenderSend(t *testing.T) {
	t.Parallel()

	t.Run("success reports rendered URL and status", func(t *testing.T) {
		t.Parallel()
		var gotQuery atomic.Value
		srv, sender := newServer(t, func(w http.ResponseWriter, r *http.Request) {
			gotQuery.Store(r.URL.RawQuery)
			w.WriteHeader(http.StatusOK)
		})
		dest := destFor(t, srv.URL+"/pb?cid={click_id}&sum={payout}")
		res, err := sender.Send(t.Context(), dest, map[string]string{"click_id": "a b", "payout": "12.50"})
		if err != nil {
			t.Fatalf("Send: %v", err)
		}
		if res.StatusCode != http.StatusOK {
			t.Errorf("StatusCode = %d, want 200", res.StatusCode)
		}
		if want := srv.URL + "/pb?cid=a+b&sum=12.50"; res.URL != want {
			t.Errorf("URL = %q, want %q", res.URL, want)
		}
		if q := gotQuery.Load(); q != "cid=a+b&sum=12.50" {
			t.Errorf("server saw query %q", q)
		}
	})

	t.Run("4xx is ErrClientStatus", func(t *testing.T) {
		t.Parallel()
		srv, sender := newServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		})
		res, err := sender.Send(t.Context(), destFor(t, srv.URL+"/pb?cid={click_id}"), nil)
		if !errors.Is(err, postback.ErrClientStatus) {
			t.Fatalf("err = %v, want ErrClientStatus", err)
		}
		if res.StatusCode != http.StatusNotFound {
			t.Errorf("StatusCode = %d, want 404", res.StatusCode)
		}
	})

	t.Run("5xx is ErrServerStatus", func(t *testing.T) {
		t.Parallel()
		srv, sender := newServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
		})
		res, err := sender.Send(t.Context(), destFor(t, srv.URL+"/pb?cid={click_id}"), nil)
		if !errors.Is(err, postback.ErrServerStatus) {
			t.Fatalf("err = %v, want ErrServerStatus", err)
		}
		if res.StatusCode != http.StatusBadGateway {
			t.Errorf("StatusCode = %d, want 502", res.StatusCode)
		}
	})

	t.Run("POST destination fires POST", func(t *testing.T) {
		t.Parallel()
		var gotMethod atomic.Value
		srv, sender := newServer(t, func(w http.ResponseWriter, r *http.Request) {
			gotMethod.Store(r.Method)
			w.WriteHeader(http.StatusOK)
		})
		dest := destFor(t, srv.URL+"/pb?cid={click_id}", postback.WithMethod(http.MethodPost))
		if _, err := sender.Send(t.Context(), dest, map[string]string{"click_id": "abc"}); err != nil {
			t.Fatalf("Send: %v", err)
		}
		if m := gotMethod.Load(); m != http.MethodPost {
			t.Errorf("server saw method %v, want POST", m)
		}
	})

	t.Run("transport failure returns error with zero status", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.NotFoundHandler())
		srv.Close() // dead server: connection refused
		sender := postback.New()
		res, err := sender.Send(t.Context(), destFor(t, srv.URL+"/pb?cid={click_id}"), nil)
		if err == nil {
			t.Fatal("want transport error")
		}
		if errors.Is(err, postback.ErrClientStatus) || errors.Is(err, postback.ErrServerStatus) {
			t.Errorf("transport error must not match a status class: %v", err)
		}
		if res.StatusCode != 0 {
			t.Errorf("StatusCode = %d, want 0", res.StatusCode)
		}
	})

	t.Run("canceled context aborts", func(t *testing.T) {
		t.Parallel()
		srv, sender := newServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if _, err := sender.Send(ctx, destFor(t, srv.URL+"/pb?cid={click_id}"), nil); !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	})

	t.Run("nil WithHTTPClient falls back to the default client", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(srv.Close)
		sender := postback.New(postback.WithHTTPClient(nil))
		if _, err := sender.Send(t.Context(), destFor(t, srv.URL+"/pb?cid={click_id}"), nil); err != nil {
			t.Fatalf("Send with defaulted client: %v", err)
		}
	})

	t.Run("zero sender fails closed", func(t *testing.T) {
		t.Parallel()
		var sender postback.Sender
		if _, err := sender.Send(t.Context(), destFor(t, "https://t.example.com/pb?cid={click_id}"), nil); err == nil {
			t.Fatal("want error from zero Sender, got nil")
		}
	})

	t.Run("zero destination fails closed", func(t *testing.T) {
		t.Parallel()
		sender := postback.New()
		if _, err := sender.Send(t.Context(), postback.Destination{}, nil); !errors.Is(err, postback.ErrInvalidTemplate) {
			t.Fatalf("err = %v, want ErrInvalidTemplate", err)
		}
	})
}
