package webhook_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/comms/webhook"
)

// signedRequest builds a POST with payload signed by scheme at time at.
func signedRequest(t *testing.T, scheme webhook.Scheme, secret, payload []byte, at time.Time) *http.Request {
	t.Helper()
	h, err := scheme.Sign(secret, payload, at)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/hook", bytes.NewReader(payload))
	maps.Copy(req.Header, h)
	return req
}

// echoBody records that it ran and echoes the request body it could read.
func echoBody(t *testing.T, called *bool) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*called = true
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		_, _ = w.Write(body)
	})
}

func TestVerifyPassesAndRestoresBody(t *testing.T) {
	t.Parallel()
	for name, scheme := range schemes() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			called := false
			h := webhook.Verify(scheme, webhook.StaticSecrets(testSecret))(echoBody(t, &called))

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, signedRequest(t, scheme, testSecret, testPayload, time.Now()))

			require.True(t, called)
			assert.Equal(t, http.StatusOK, rec.Code)
			assert.Equal(t, testPayload, rec.Body.Bytes(), "handler saw the exact original body")
		})
	}
}

func TestVerifyRejects(t *testing.T) {
	t.Parallel()
	scheme := webhook.Stripe()
	cases := []struct {
		name       string
		req        func(t *testing.T) *http.Request
		wantStatus int
		wantDetail string
	}{
		{
			name: "wrong secret",
			req: func(t *testing.T) *http.Request {
				return signedRequest(t, scheme, []byte("wrong"), testPayload, time.Now())
			},
			wantStatus: http.StatusUnauthorized,
			wantDetail: webhook.ErrInvalidSignature.Error(),
		},
		{
			name: "no signature header",
			req: func(*testing.T) *http.Request {
				return httptest.NewRequest(http.MethodPost, "/hook", bytes.NewReader(testPayload))
			},
			wantStatus: http.StatusUnauthorized,
			wantDetail: webhook.ErrMissingSignature.Error(),
		},
		{
			name: "stale timestamp",
			req: func(t *testing.T) *http.Request {
				return signedRequest(t, scheme, testSecret, testPayload, time.Now().Add(-time.Hour))
			},
			wantStatus: http.StatusUnauthorized,
			wantDetail: webhook.ErrInvalidTimestamp.Error(),
		},
		{
			name: "tampered payload",
			req: func(t *testing.T) *http.Request {
				r := signedRequest(t, scheme, testSecret, testPayload, time.Now())
				r.Body = io.NopCloser(strings.NewReader(`{"tampered":true}`))
				return r
			},
			wantStatus: http.StatusUnauthorized,
			wantDetail: webhook.ErrInvalidSignature.Error(),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			called := false
			h := webhook.Verify(scheme, webhook.StaticSecrets(testSecret))(echoBody(t, &called))
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, tc.req(t))

			assert.False(t, called)
			assert.Equal(t, tc.wantStatus, rec.Code)
			assert.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))

			var p struct {
				Detail string `json:"detail"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &p))
			assert.Equal(t, tc.wantDetail, p.Detail, "bare sentinel only, no internals")
		})
	}
}

func TestVerifyBodyTooLarge(t *testing.T) {
	t.Parallel()
	called := false
	h := webhook.Verify(webhook.GitHub(), webhook.StaticSecrets(testSecret), webhook.WithMaxBody(8))(echoBody(t, &called))

	req := httptest.NewRequest(http.MethodPost, "/hook", strings.NewReader("way more than eight bytes"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.False(t, called)
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	assert.Contains(t, rec.Body.String(), webhook.ErrBodyTooLarge.Error())
	assert.NotContains(t, rec.Body.String(), "cap 8", "cap detail stays server-side")
}

func TestVerifySecretsFailClosed(t *testing.T) {
	t.Parallel()
	scheme := webhook.GitHub()
	cases := map[string]webhook.Secrets{
		"lookup error": func(*http.Request) ([][]byte, error) { return nil, errors.New("tenant db down") },
		"no secrets":   webhook.StaticSecrets(),
		"only empties": webhook.StaticSecrets(nil, []byte{}),
	}
	for name, secrets := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			called := false
			h := webhook.Verify(scheme, secrets)(echoBody(t, &called))
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, signedRequest(t, scheme, testSecret, testPayload, time.Now()))

			assert.False(t, called)
			assert.Equal(t, http.StatusUnauthorized, rec.Code)
			assert.NotContains(t, rec.Body.String(), "tenant db down", "lookup detail never leaks")
		})
	}
}

func TestVerifySecretRotation(t *testing.T) {
	t.Parallel()
	scheme := webhook.Stripe()
	old, current := []byte("old-secret"), []byte("new-secret")
	called := false
	h := webhook.Verify(scheme, webhook.StaticSecrets(current, old))(echoBody(t, &called))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, signedRequest(t, scheme, old, testPayload, time.Now()))
	assert.True(t, called, "a signature under the retired secret still verifies")
}

func TestVerifyToleranceOptions(t *testing.T) {
	t.Parallel()
	scheme := webhook.Stripe()
	stale := func(t *testing.T) *http.Request {
		return signedRequest(t, scheme, testSecret, testPayload, time.Now().Add(-time.Hour))
	}

	called := false
	h := webhook.Verify(scheme, webhook.StaticSecrets(testSecret), webhook.WithTolerance(0))(echoBody(t, &called))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, stale(t))
	assert.True(t, called, "zero tolerance disables the timestamp check")

	called = false
	h = webhook.Verify(scheme, webhook.StaticSecrets(testSecret), webhook.WithTolerance(2*time.Hour))(echoBody(t, &called))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, stale(t))
	assert.True(t, called, "stale but within a widened window")
}

func TestVerifyCustomResponder(t *testing.T) {
	t.Parallel()
	scheme := webhook.GitHub()
	var got error
	h := webhook.Verify(scheme, webhook.StaticSecrets(testSecret), webhook.WithResponder(func(w http.ResponseWriter, _ *http.Request, err error) {
		got = err
		w.WriteHeader(http.StatusForbidden)
	}))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/hook", bytes.NewReader(testPayload)))

	assert.Equal(t, http.StatusForbidden, rec.Code)
	require.ErrorIs(t, got, webhook.ErrMissingSignature, "custom responder sees the full chain")
	assert.ErrorContains(t, got, "X-Hub-Signature-256")
}

func TestVerifyNilWiringPanics(t *testing.T) {
	t.Parallel()
	assert.Panics(t, func() { webhook.Verify(nil, webhook.StaticSecrets(testSecret)) })
	assert.Panics(t, func() { webhook.Verify(webhook.GitHub(), nil) })
}

func TestStaticSecretsClones(t *testing.T) {
	t.Parallel()
	scheme := webhook.GitHub()
	secret := []byte("rotate-me")
	secrets := webhook.StaticSecrets(secret)
	copy(secret, []byte("MUTATED!!"))

	called := false
	h := webhook.Verify(scheme, secrets)(echoBody(t, &called))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, signedRequest(t, scheme, []byte("rotate-me"), testPayload, time.Now()))
	assert.True(t, called, "the middleware holds its own copy of the secret")
}
