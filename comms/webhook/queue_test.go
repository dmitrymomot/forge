package webhook_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/async/queue"
	"github.com/dmitrymomot/forge/comms/webhook"
	"github.com/dmitrymomot/forge/resilience/backoff"
)

// startWorker runs a fast-polling queue worker with the delivery handler
// registered; it stops when the test ends.
func startWorker(t *testing.T, broker queue.Broker, sender *webhook.Sender, resolve webhook.Resolver) {
	t.Helper()
	svc, err := queue.NewService(broker,
		queue.WithConfig(queue.Config{Concurrency: 2, PollInterval: 10 * time.Millisecond, Lease: 30 * time.Second, MaxAttempts: 25, HandlerTimeout: time.Minute}),
		queue.WithBackoff(backoff.Constant(time.Millisecond)),
	)
	require.NoError(t, err)
	webhook.RegisterDeliverer(svc, sender, resolve)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = svc.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
}

func staticResolver(ep webhook.Endpoint) webhook.Resolver {
	return func(context.Context, string) (webhook.Endpoint, error) { return ep, nil }
}

func drained(t *testing.T, c *queue.Client) func() bool {
	t.Helper()
	return func() bool {
		stats, err := c.Stats(t.Context())
		require.NoError(t, err)
		return stats["default"].Pending == 0
	}
}

func TestEnqueueValidation(t *testing.T) {
	t.Parallel()
	c := queue.NewClient(queue.NewMemoryBroker())

	err := webhook.Enqueue(t.Context(), c, webhook.Delivery{Payload: testPayload})
	assert.ErrorIs(t, err, webhook.ErrInvalidDelivery)

	err = webhook.Enqueue(t.Context(), c, webhook.Delivery{Endpoint: "ep_1", Payload: json.RawMessage(`{broken`)})
	assert.ErrorIs(t, err, webhook.ErrInvalidDelivery)

	err = webhook.Enqueue(t.Context(), c, webhook.Delivery{Endpoint: "ep_1", Payload: testPayload, Key: "evt\r\n1"})
	assert.ErrorIs(t, err, webhook.ErrInvalidDelivery, "a header-illegal key would fail every attempt")
}

func TestDeliveryPayloadSurvivesQueueEncoding(t *testing.T) {
	t.Parallel()
	bodies := make(chan []byte, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodies <- body
	}))
	defer srv.Close()

	broker := queue.NewMemoryBroker()
	c := queue.NewClient(broker)
	sender := webhook.New(webhook.WithHTTPClient(srv.Client()))
	startWorker(t, broker, sender, staticResolver(webhook.Endpoint{URL: srv.URL, Secret: testSecret}))

	payload := json.RawMessage(`{"html": "<b>tags & ampersands</b>"}`)
	require.NoError(t, webhook.Enqueue(t.Context(), c, webhook.Delivery{Endpoint: "ep_1", Payload: payload}))

	select {
	case body := <-bodies:
		// The queue's JSON round trip may compact and HTML-escape the body;
		// what arrives is semantically the same document.
		assert.JSONEq(t, string(payload), string(body))
	case <-time.After(5 * time.Second):
		t.Fatal("delivery never arrived")
	}
}

func TestRegisterDelivererNilWiringPanics(t *testing.T) {
	t.Parallel()
	svc, err := queue.NewService(queue.NewMemoryBroker())
	require.NoError(t, err)
	resolve := staticResolver(webhook.Endpoint{})
	assert.Panics(t, func() { webhook.RegisterDeliverer(svc, nil, resolve) })
	assert.Panics(t, func() { webhook.RegisterDeliverer(svc, webhook.New(), nil) })
}

func TestDurableDeliverySuccess(t *testing.T) {
	t.Parallel()
	type hit struct {
		key  string
		body []byte
	}
	hits := make(chan hit, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		hits <- hit{key: r.Header.Get("Webhook-Id"), body: body}
	}))
	defer srv.Close()

	broker := queue.NewMemoryBroker()
	c := queue.NewClient(broker)
	sender := webhook.New(webhook.WithHTTPClient(srv.Client()))
	startWorker(t, broker, sender, staticResolver(webhook.Endpoint{URL: srv.URL, Secret: testSecret}))

	require.NoError(t, webhook.Enqueue(t.Context(), c, webhook.Delivery{Endpoint: "ep_1", Payload: testPayload}))

	select {
	case h := <-hits:
		assert.NotEmpty(t, h.key, "Enqueue generated an idempotency key")
		assert.Equal(t, testPayload, h.body, "an escape-free payload survives byte-for-byte")
	case <-time.After(5 * time.Second):
		t.Fatal("delivery never arrived")
	}
	require.Eventually(t, drained(t, c), 5*time.Second, 10*time.Millisecond)
}

func TestEnqueuePreservesKey(t *testing.T) {
	t.Parallel()
	keys := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		keys <- r.Header.Get("Webhook-Id")
	}))
	defer srv.Close()

	broker := queue.NewMemoryBroker()
	c := queue.NewClient(broker)
	sender := webhook.New(webhook.WithHTTPClient(srv.Client()))
	startWorker(t, broker, sender, staticResolver(webhook.Endpoint{URL: srv.URL, Secret: testSecret}))

	require.NoError(t, webhook.Enqueue(t.Context(), c, webhook.Delivery{Endpoint: "ep_1", Payload: testPayload, Key: "evt_42"}))

	select {
	case key := <-keys:
		assert.Equal(t, "evt_42", key)
	case <-time.After(5 * time.Second):
		t.Fatal("delivery never arrived")
	}
}

func TestTransientStatusRetries(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if hits.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer srv.Close()

	broker := queue.NewMemoryBroker()
	c := queue.NewClient(broker)
	sender := webhook.New(webhook.WithHTTPClient(srv.Client()))
	startWorker(t, broker, sender, staticResolver(webhook.Endpoint{URL: srv.URL, Secret: testSecret}))

	require.NoError(t, webhook.Enqueue(t.Context(), c, webhook.Delivery{Endpoint: "ep_1", Payload: testPayload}))

	require.Eventually(t, func() bool { return hits.Load() >= 3 }, 10*time.Second, 10*time.Millisecond, "two 503s then success")
	require.Eventually(t, drained(t, c), 5*time.Second, 10*time.Millisecond)

	dead, err := c.ListDead(t.Context(), "default", 10)
	require.NoError(t, err)
	assert.Empty(t, dead, "the delivery succeeded on the third attempt")
}

func TestPermanentStatusDeadLetters(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	broker := queue.NewMemoryBroker()
	c := queue.NewClient(broker)
	sender := webhook.New(webhook.WithHTTPClient(srv.Client()))
	startWorker(t, broker, sender, staticResolver(webhook.Endpoint{URL: srv.URL, Secret: testSecret}))

	require.NoError(t, webhook.Enqueue(t.Context(), c, webhook.Delivery{Endpoint: "ep_1", Payload: testPayload}))

	require.Eventually(t, func() bool {
		dead, err := c.ListDead(t.Context(), "default", 10)
		require.NoError(t, err)
		return len(dead) == 1
	}, 5*time.Second, 10*time.Millisecond, "a 404 dead-letters without burning attempts")
	assert.Equal(t, int32(1), hits.Load(), "no retries on a permanent status")
}

func TestEndpointNotFoundCancels(t *testing.T) {
	t.Parallel()
	var resolved atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("a cancelled delivery must never reach the endpoint")
	}))
	defer srv.Close()

	broker := queue.NewMemoryBroker()
	c := queue.NewClient(broker)
	sender := webhook.New(webhook.WithHTTPClient(srv.Client()))
	startWorker(t, broker, sender, func(context.Context, string) (webhook.Endpoint, error) {
		resolved.Add(1)
		return webhook.Endpoint{}, webhook.ErrEndpointNotFound
	})

	require.NoError(t, webhook.Enqueue(t.Context(), c, webhook.Delivery{Endpoint: "ep_gone", Payload: testPayload}))

	require.Eventually(t, func() bool { return resolved.Load() == 1 }, 5*time.Second, 10*time.Millisecond)
	require.Eventually(t, drained(t, c), 5*time.Second, 10*time.Millisecond)

	dead, err := c.ListDead(t.Context(), "default", 10)
	require.NoError(t, err)
	assert.Empty(t, dead, "cancelled, not dead-lettered")
}

func TestResolverErrorRetries(t *testing.T) {
	t.Parallel()
	hits := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		hits <- struct{}{}
	}))
	defer srv.Close()

	var calls atomic.Int32
	broker := queue.NewMemoryBroker()
	c := queue.NewClient(broker)
	sender := webhook.New(webhook.WithHTTPClient(srv.Client()))
	startWorker(t, broker, sender, func(context.Context, string) (webhook.Endpoint, error) {
		if calls.Add(1) == 1 {
			return webhook.Endpoint{}, assert.AnError // transient store hiccup
		}
		return webhook.Endpoint{URL: srv.URL, Secret: testSecret}, nil
	})

	require.NoError(t, webhook.Enqueue(t.Context(), c, webhook.Delivery{Endpoint: "ep_1", Payload: testPayload}))

	select {
	case <-hits:
		assert.GreaterOrEqual(t, calls.Load(), int32(2), "first resolve failed, retry delivered")
	case <-time.After(10 * time.Second):
		t.Fatal("delivery never arrived after resolver retry")
	}
}
