package webhook_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/comms/webhook"
)

func TestSendSignedDelivery(t *testing.T) {
	t.Parallel()
	type received struct {
		header http.Header
		body   []byte
	}
	got := make(chan received, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got <- received{header: r.Header.Clone(), body: body}
	}))
	defer srv.Close()

	sender := webhook.New(webhook.WithHTTPClient(srv.Client()))
	res, err := sender.Send(t.Context(), webhook.Endpoint{URL: srv.URL, Secret: testSecret}, testPayload, "evt_1")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode)

	rec := <-got
	assert.Equal(t, "application/json", rec.header.Get("Content-Type"))
	assert.Equal(t, "evt_1", rec.header.Get("Webhook-Id"))
	assert.Equal(t, testPayload, rec.body)

	// What arrived over the wire verifies under the Sender's default scheme.
	scheme := webhook.Stripe(webhook.WithSignatureHeader("Webhook-Signature"))
	require.NoError(t, scheme.Verify(testSecret, rec.body, rec.header, time.Now(), 5*time.Minute))
}

func TestSendStatusClasses(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		status int
		want   error
	}{
		{"created", http.StatusCreated, nil},
		{"server error", http.StatusInternalServerError, webhook.ErrTransientStatus},
		{"bad gateway", http.StatusBadGateway, webhook.ErrTransientStatus},
		{"too many requests", http.StatusTooManyRequests, webhook.ErrTransientStatus},
		{"request timeout", http.StatusRequestTimeout, webhook.ErrTransientStatus},
		{"not found", http.StatusNotFound, webhook.ErrPermanentStatus},
		{"bad request", http.StatusBadRequest, webhook.ErrPermanentStatus},
		{"gone", http.StatusGone, webhook.ErrPermanentStatus},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
			}))
			defer srv.Close()

			sender := webhook.New(webhook.WithHTTPClient(srv.Client()))
			res, err := sender.Send(t.Context(), webhook.Endpoint{URL: srv.URL, Secret: testSecret}, testPayload, "")
			assert.Equal(t, tc.status, res.StatusCode)
			if tc.want == nil {
				assert.NoError(t, err)
			} else {
				assert.ErrorIs(t, err, tc.want)
			}
		})
	}
}

func TestSendDoesNotFollowRedirects(t *testing.T) {
	t.Parallel()
	followed := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/moved" {
			followed = true
			return
		}
		http.Redirect(w, r, "/moved", http.StatusTemporaryRedirect)
	}))
	defer srv.Close()

	// Default client (not srv.Client()) so the no-follow redirect policy is exercised;
	// httptest URLs are plain http so no TLS config is needed.
	sender := webhook.New()
	res, err := sender.Send(t.Context(), webhook.Endpoint{URL: srv.URL, Secret: testSecret}, testPayload, "")
	assert.ErrorIs(t, err, webhook.ErrPermanentStatus)
	assert.Equal(t, http.StatusTemporaryRedirect, res.StatusCode)
	assert.False(t, followed, "a signed POST must land on the registered URL only")
}

func TestSendInvalidEndpoint(t *testing.T) {
	t.Parallel()
	sender := webhook.New()
	for name, ep := range map[string]webhook.Endpoint{
		"empty secret": {URL: "https://example.com/hook"},
		"empty url":    {Secret: testSecret},
		"relative url": {URL: "/hook", Secret: testSecret},
		"non-http url": {URL: "ftp://example.com/hook", Secret: testSecret},
		"schemeless":   {URL: "example.com/hook", Secret: testSecret},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := sender.Send(t.Context(), ep, testPayload, "")
			assert.ErrorIs(t, err, webhook.ErrInvalidEndpoint)
		})
	}
}

func TestSendTransportError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close() // guaranteed connection refused

	sender := webhook.New()
	res, err := sender.Send(t.Context(), webhook.Endpoint{URL: url, Secret: testSecret}, testPayload, "")
	require.Error(t, err)
	assert.NotErrorIs(t, err, webhook.ErrTransientStatus)
	assert.NotErrorIs(t, err, webhook.ErrPermanentStatus)
	assert.Zero(t, res.StatusCode)
}

func TestSendZeroSender(t *testing.T) {
	t.Parallel()
	var zero webhook.Sender
	_, err := zero.Send(t.Context(), webhook.Endpoint{URL: "https://example.com", Secret: testSecret}, testPayload, "")
	require.Error(t, err)
}

func TestSendOptions(t *testing.T) {
	t.Parallel()
	got := make(chan http.Header, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got <- r.Header.Clone()
	}))
	defer srv.Close()

	sender := webhook.New(
		webhook.WithHTTPClient(srv.Client()),
		webhook.WithScheme(webhook.GitHub()),
		webhook.WithIdempotencyHeader("Idempotency-Key"),
		webhook.WithContentType("application/cloudevents+json"),
	)
	_, err := sender.Send(t.Context(), webhook.Endpoint{URL: srv.URL, Secret: testSecret}, testPayload, "evt_9")
	require.NoError(t, err)

	h := <-got
	assert.Equal(t, "application/cloudevents+json", h.Get("Content-Type"))
	assert.Equal(t, "evt_9", h.Get("Idempotency-Key"))
	assert.NotEmpty(t, h.Get("X-Hub-Signature-256"))
	assert.Empty(t, h.Get("Webhook-Signature"))
}

func TestSendOmitsEmptyKey(t *testing.T) {
	t.Parallel()
	got := make(chan http.Header, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got <- r.Header.Clone()
	}))
	defer srv.Close()

	sender := webhook.New(webhook.WithHTTPClient(srv.Client()))
	_, err := sender.Send(t.Context(), webhook.Endpoint{URL: srv.URL, Secret: testSecret}, testPayload, "")
	require.NoError(t, err)

	h := <-got
	_, present := h["Webhook-Id"]
	assert.False(t, present)
}
