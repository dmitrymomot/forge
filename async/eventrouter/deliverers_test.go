package eventrouter_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/async/eventrouter"
	"github.com/dmitrymomot/forge/comms/postback"
	"github.com/dmitrymomot/forge/comms/webhook"
)

func testEvents(n int) []eventrouter.Event {
	events := make([]eventrouter.Event, 0, n)
	for i := range n {
		events = append(events, eventrouter.Event{
			OccurredAt: time.Date(2026, 7, 20, 12, 0, i, 0, time.UTC),
			Payload:    json.RawMessage(`{"v":"` + string(rune('a'+i)) + `"}`),
			ID:         "evt_" + string(rune('a'+i)),
			Name:       "order.placed",
		})
	}
	return events
}

// capture records the last request a test server saw.
type capture struct {
	header http.Header
	body   []byte
	method string
	mu     sync.Mutex
}

func captureServer(t *testing.T, status int) (*httptest.Server, *capture) {
	t.Helper()
	c := &capture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		c.mu.Lock()
		c.method = r.Method
		c.header = r.Header.Clone()
		c.body = body
		c.mu.Unlock()
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv, c
}

func TestHTTPDeliverer_Deliver(t *testing.T) {
	t.Parallel()

	t.Run("batch body and headers", func(t *testing.T) {
		t.Parallel()
		srv, c := captureServer(t, http.StatusOK)
		d, err := eventrouter.NewHTTPDeliverer(srv.URL+"/collect",
			eventrouter.WithHTTPHeader("Authorization", "Bearer tok"))
		require.NoError(t, err)

		require.NoError(t, d.Deliver(context.Background(), testEvents(2)))

		c.mu.Lock()
		defer c.mu.Unlock()
		assert.Equal(t, http.MethodPost, c.method)
		assert.Equal(t, "application/json", c.header.Get("Content-Type"))
		assert.Equal(t, "Bearer tok", c.header.Get("Authorization"))
		assert.Empty(t, c.header.Get("Idempotency-Key"), "batches carry ids in the payload, not the header")

		var got []map[string]any
		require.NoError(t, json.Unmarshal(c.body, &got))
		require.Len(t, got, 2)
		assert.Equal(t, "evt_a", got[0]["id"])
		assert.Equal(t, "order.placed", got[0]["name"])
		assert.NotEmpty(t, got[0]["occurred_at"])
		assert.Equal(t, map[string]any{"v": "a"}, got[0]["payload"])
	})

	t.Run("single event carries Idempotency-Key", func(t *testing.T) {
		t.Parallel()
		srv, c := captureServer(t, http.StatusOK)
		d, err := eventrouter.NewHTTPDeliverer(srv.URL)
		require.NoError(t, err)

		require.NoError(t, d.Deliver(context.Background(), testEvents(1)))
		c.mu.Lock()
		defer c.mu.Unlock()
		assert.Equal(t, "evt_a", c.header.Get("Idempotency-Key"))
	})

	t.Run("status classes", func(t *testing.T) {
		t.Parallel()
		for status, wantPermanent := range map[int]bool{
			http.StatusNoContent:           false, // 2xx → nil error, checked below
			http.StatusBadRequest:          true,
			http.StatusNotFound:            true,
			http.StatusGone:                true,
			http.StatusRequestTimeout:      false,
			http.StatusTooManyRequests:     false,
			http.StatusInternalServerError: false,
			http.StatusServiceUnavailable:  false,
		} {
			srv, _ := captureServer(t, status)
			d, err := eventrouter.NewHTTPDeliverer(srv.URL)
			require.NoError(t, err)
			err = d.Deliver(context.Background(), testEvents(1))
			if status < 300 {
				assert.NoError(t, err, "status %d", status)
				continue
			}
			require.Error(t, err, "status %d", status)
			assert.Equal(t, wantPermanent, errors.Is(err, eventrouter.ErrPermanent), "status %d", status)
		}
	})

	t.Run("transport failure is transient", func(t *testing.T) {
		t.Parallel()
		srv, _ := captureServer(t, http.StatusOK)
		d, err := eventrouter.NewHTTPDeliverer(srv.URL)
		require.NoError(t, err)
		srv.Close()
		err = d.Deliver(context.Background(), testEvents(1))
		require.Error(t, err)
		assert.False(t, errors.Is(err, eventrouter.ErrPermanent))
	})

	t.Run("invalid URL", func(t *testing.T) {
		t.Parallel()
		for _, u := range []string{"", "not a url", "ftp://host/x", "/relative", "https://"} {
			_, err := eventrouter.NewHTTPDeliverer(u)
			assert.ErrorIs(t, err, eventrouter.ErrInvalidURL, "url %q", u)
		}
	})

	t.Run("zero deliverer errors instead of panicking", func(t *testing.T) {
		t.Parallel()
		var d eventrouter.HTTPDeliverer
		assert.Error(t, d.Deliver(context.Background(), testEvents(1)))
	})
}

func TestWebhookDeliverer_Deliver(t *testing.T) {
	t.Parallel()
	endpoint := func(url string) webhook.Endpoint {
		return webhook.Endpoint{URL: url, Secret: []byte("shhh")}
	}

	t.Run("signs and keys single deliveries", func(t *testing.T) {
		t.Parallel()
		srv, c := captureServer(t, http.StatusOK)
		d := eventrouter.NewWebhookDeliverer(webhook.New(), endpoint(srv.URL))

		require.NoError(t, d.Deliver(context.Background(), testEvents(1)))
		c.mu.Lock()
		defer c.mu.Unlock()
		assert.NotEmpty(t, c.header.Get("Webhook-Signature"), "deliveries are signed")
		assert.Equal(t, "evt_a", c.header.Get("Webhook-Id"))

		var got []map[string]any
		require.NoError(t, json.Unmarshal(c.body, &got))
		require.Len(t, got, 1)
		assert.Equal(t, "evt_a", got[0]["id"])
	})

	t.Run("batches are signed without a batch key", func(t *testing.T) {
		t.Parallel()
		srv, c := captureServer(t, http.StatusOK)
		d := eventrouter.NewWebhookDeliverer(webhook.New(), endpoint(srv.URL))

		require.NoError(t, d.Deliver(context.Background(), testEvents(3)))
		c.mu.Lock()
		defer c.mu.Unlock()
		assert.NotEmpty(t, c.header.Get("Webhook-Signature"))
		assert.Empty(t, c.header.Get("Webhook-Id"), "a re-formed batch has no stable identity")
	})

	t.Run("status mapping", func(t *testing.T) {
		t.Parallel()
		srv400, _ := captureServer(t, http.StatusBadRequest)
		d := eventrouter.NewWebhookDeliverer(webhook.New(), endpoint(srv400.URL))
		err := d.Deliver(context.Background(), testEvents(1))
		assert.ErrorIs(t, err, eventrouter.ErrPermanent)
		assert.ErrorIs(t, err, webhook.ErrPermanentStatus)

		srv500, _ := captureServer(t, http.StatusInternalServerError)
		d = eventrouter.NewWebhookDeliverer(webhook.New(), endpoint(srv500.URL))
		err = d.Deliver(context.Background(), testEvents(1))
		assert.ErrorIs(t, err, webhook.ErrTransientStatus)
		assert.False(t, errors.Is(err, eventrouter.ErrPermanent))
	})

	t.Run("invalid endpoint is permanent", func(t *testing.T) {
		t.Parallel()
		d := eventrouter.NewWebhookDeliverer(webhook.New(), webhook.Endpoint{URL: "https://example.com"})
		err := d.Deliver(context.Background(), testEvents(1))
		assert.ErrorIs(t, err, eventrouter.ErrPermanent)
	})

	t.Run("validation", func(t *testing.T) {
		t.Parallel()
		assert.Panics(t, func() { eventrouter.NewWebhookDeliverer(nil, webhook.Endpoint{}) })
		var d eventrouter.WebhookDeliverer
		assert.Error(t, d.Deliver(context.Background(), testEvents(1)))
	})
}

func TestPostbackDeliverer_Deliver(t *testing.T) {
	t.Parallel()
	vocab, err := postback.NewVocabulary("event_id", "value")
	require.NoError(t, err)
	values := func(e eventrouter.Event) (map[string]string, error) {
		var p payload
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return nil, err
		}
		return map[string]string{"event_id": e.ID, "value": p.V}, nil
	}

	t.Run("renders macros per event", func(t *testing.T) {
		t.Parallel()
		var mu sync.Mutex
		var urls []string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			urls = append(urls, r.URL.String())
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(srv.Close)
		dest, err := postback.NewDestination(srv.URL+"/pb?eid={event_id}&v={value}", vocab)
		require.NoError(t, err)
		d := eventrouter.NewPostbackDeliverer(postback.New(), dest, values)

		require.NoError(t, d.Deliver(context.Background(), testEvents(2)))
		mu.Lock()
		defer mu.Unlock()
		assert.ElementsMatch(t, []string{"/pb?eid=evt_a&v=a", "/pb?eid=evt_b&v=b"}, urls)
	})

	t.Run("status mapping", func(t *testing.T) {
		t.Parallel()
		srv500, _ := captureServer(t, http.StatusInternalServerError)
		dest, err := postback.NewDestination(srv500.URL+"/pb?eid={event_id}", vocab)
		require.NoError(t, err)
		d := eventrouter.NewPostbackDeliverer(postback.New(), dest, values)
		err = d.Deliver(context.Background(), testEvents(1))
		require.Error(t, err)
		assert.ErrorIs(t, err, postback.ErrServerStatus)
		assert.False(t, errors.Is(err, eventrouter.ErrPermanent), "5xx retries")

		srv404, _ := captureServer(t, http.StatusNotFound)
		dest404, err := postback.NewDestination(srv404.URL+"/pb?eid={event_id}", vocab)
		require.NoError(t, err)
		d = eventrouter.NewPostbackDeliverer(postback.New(), dest404, values)
		err = d.Deliver(context.Background(), testEvents(1))
		assert.ErrorIs(t, err, eventrouter.ErrPermanent)
		assert.ErrorIs(t, err, postback.ErrClientStatus)
	})

	t.Run("values error is permanent and skips the ping", func(t *testing.T) {
		t.Parallel()
		srv, c := captureServer(t, http.StatusOK)
		dest, err := postback.NewDestination(srv.URL+"/pb?eid={event_id}", vocab)
		require.NoError(t, err)
		d := eventrouter.NewPostbackDeliverer(postback.New(), dest,
			func(eventrouter.Event) (map[string]string, error) { return nil, context.Canceled })
		err = d.Deliver(context.Background(), testEvents(1))
		assert.ErrorIs(t, err, eventrouter.ErrPermanent)
		c.mu.Lock()
		defer c.mu.Unlock()
		assert.Nil(t, c.body, "no request fired for an unmappable event")
	})

	t.Run("permanent outranks transient in mixed batches", func(t *testing.T) {
		t.Parallel()
		srv, _ := captureServer(t, http.StatusOK)
		dest, err := postback.NewDestination(srv.URL+"/pb?eid={event_id}", vocab)
		require.NoError(t, err)
		d := eventrouter.NewPostbackDeliverer(postback.New(), dest,
			func(e eventrouter.Event) (map[string]string, error) {
				if e.ID == "evt_a" {
					return nil, context.Canceled
				}
				return map[string]string{"event_id": e.ID}, nil
			})
		err = d.Deliver(context.Background(), testEvents(2))
		assert.ErrorIs(t, err, eventrouter.ErrPermanent, "isolation splits the batch into per-event verdicts")
	})

	t.Run("validation", func(t *testing.T) {
		t.Parallel()
		assert.Panics(t, func() { eventrouter.NewPostbackDeliverer(nil, postback.Destination{}, values) })
		assert.Panics(t, func() { eventrouter.NewPostbackDeliverer(postback.New(), postback.Destination{}, nil) })
		var d eventrouter.PostbackDeliverer
		assert.Error(t, d.Deliver(context.Background(), testEvents(1)))
	})
}
