package webhook_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/comms/webhook"
)

var (
	testSecret  = []byte("s3cr3t-signing-key")
	testPayload = []byte(`{"type":"invoice.paid","id":"evt_1"}`)
)

func schemes() map[string]webhook.Scheme {
	return map[string]webhook.Scheme{
		"stripe": webhook.Stripe(),
		"github": webhook.GitHub(),
		"slack":  webhook.Slack(),
	}
}

func TestSchemeRoundTrip(t *testing.T) {
	t.Parallel()
	now := time.Now()
	for name, s := range schemes() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h, err := s.Sign(testSecret, testPayload, now)
			require.NoError(t, err)
			require.NoError(t, s.Verify(testSecret, testPayload, h, now, 5*time.Minute))

			assert.ErrorIs(t, s.Verify(testSecret, []byte(`{"tampered":true}`), h, now, 5*time.Minute), webhook.ErrInvalidSignature)
			assert.ErrorIs(t, s.Verify([]byte("wrong-secret"), testPayload, h, now, 5*time.Minute), webhook.ErrInvalidSignature)
			assert.ErrorIs(t, s.Verify(testSecret, testPayload, http.Header{}, now, 5*time.Minute), webhook.ErrMissingSignature)
		})
	}
}

func TestSchemeEmptySecret(t *testing.T) {
	t.Parallel()
	now := time.Now()
	for name, s := range schemes() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := s.Sign(nil, testPayload, now)
			require.Error(t, err)

			h, err := s.Sign(testSecret, testPayload, now)
			require.NoError(t, err)
			require.Error(t, s.Verify(nil, testPayload, h, now, 0))
		})
	}
}

func TestStripeTolerance(t *testing.T) {
	t.Parallel()
	now := time.Now()
	s := webhook.Stripe()
	h, err := s.Sign(testSecret, testPayload, now.Add(-10*time.Minute))
	require.NoError(t, err)

	assert.ErrorIs(t, s.Verify(testSecret, testPayload, h, now, 5*time.Minute), webhook.ErrInvalidTimestamp)
	assert.NoError(t, s.Verify(testSecret, testPayload, h, now, 15*time.Minute), "within a wider window")
	assert.NoError(t, s.Verify(testSecret, testPayload, h, now, 0), "zero tolerance disables the check")

	// A signature from the future is just as rejected as a stale one.
	h, err = s.Sign(testSecret, testPayload, now.Add(10*time.Minute))
	require.NoError(t, err)
	assert.ErrorIs(t, s.Verify(testSecret, testPayload, h, now, 5*time.Minute), webhook.ErrInvalidTimestamp)
}

func TestStripeHeaderParsing(t *testing.T) {
	t.Parallel()
	now := time.Now()
	s := webhook.Stripe()
	signed, err := s.Sign(testSecret, testPayload, now)
	require.NoError(t, err)
	good := signed.Get("Stripe-Signature")

	set := func(v string) http.Header {
		h := http.Header{}
		h.Set("Stripe-Signature", v)
		return h
	}

	// Extra v1 elements (sender-side rotation) and unknown elements are tolerated.
	assert.NoError(t, s.Verify(testSecret, testPayload, set(good+",v1=deadbeef,v0=ignored"), now, time.Minute))
	assert.NoError(t, s.Verify(testSecret, testPayload, set("v1=deadbeef, "+good), now, time.Minute), "whitespace and a bad v1 first")

	assert.ErrorIs(t, s.Verify(testSecret, testPayload, set("v1=deadbeef"), now, time.Minute), webhook.ErrInvalidTimestamp, "missing t")
	assert.ErrorIs(t, s.Verify(testSecret, testPayload, set("t=notanumber,v1=deadbeef"), now, time.Minute), webhook.ErrInvalidTimestamp)
	assert.ErrorIs(t, s.Verify(testSecret, testPayload, set("t="+strconv.FormatInt(now.Unix(), 10)), now, time.Minute), webhook.ErrInvalidSignature, "no v1 at all")
}

func TestStripeCandidateCap(t *testing.T) {
	t.Parallel()
	now := time.Now()
	s := webhook.Stripe()
	signed, err := s.Sign(testSecret, testPayload, now)
	require.NoError(t, err)
	// Sign produces "t=<ts>,v1=<good>"; ts element first, good sig second.
	good := signed.Get("Stripe-Signature")

	set := func(v string) http.Header {
		h := http.Header{}
		h.Set("Stripe-Signature", v)
		return h
	}
	junk := strings.Repeat("v1="+strings.Repeat("ab", 32)+",", 8)

	assert.ErrorIs(t, s.Verify(testSecret, testPayload, set(junk+good), now, time.Minute), webhook.ErrInvalidSignature,
		"the genuine signature past the candidate cap is not considered")
	assert.NoError(t, s.Verify(testSecret, testPayload, set(good+","+junk), now, time.Minute),
		"the genuine signature within the cap verifies")
}

func TestGitHubKnownVector(t *testing.T) {
	t.Parallel()
	// The exact example from GitHub's webhook docs.
	secret := []byte("It's a Secret to Everybody")
	payload := []byte("Hello, World!")
	const want = "sha256=757107ea0eb2509fc211221cce984b8a37570b6d7586c22c46f4379c8b043e17"

	s := webhook.GitHub()
	h, err := s.Sign(secret, payload, time.Now())
	require.NoError(t, err)
	assert.Equal(t, want, h.Get("X-Hub-Signature-256"))

	require.NoError(t, s.Verify(secret, payload, h, time.Now(), 0))
}

func TestGitHubIgnoresTolerance(t *testing.T) {
	t.Parallel()
	s := webhook.GitHub()
	h, err := s.Sign(testSecret, testPayload, time.Now().Add(-24*time.Hour))
	require.NoError(t, err)
	assert.NoError(t, s.Verify(testSecret, testPayload, h, time.Now(), time.Minute), "no timestamp in the format")
}

func TestGitHubMalformedSignature(t *testing.T) {
	t.Parallel()
	s := webhook.GitHub()
	set := func(v string) http.Header {
		h := http.Header{}
		h.Set("X-Hub-Signature-256", v)
		return h
	}
	assert.ErrorIs(t, s.Verify(testSecret, testPayload, set("sha1=deadbeef"), time.Now(), 0), webhook.ErrInvalidSignature)
	assert.ErrorIs(t, s.Verify(testSecret, testPayload, set("sha256=nothex"), time.Now(), 0), webhook.ErrInvalidSignature)
}

func TestSlackKnownVector(t *testing.T) {
	t.Parallel()
	// The exact example from Slack's request-verification docs.
	secret := []byte("8f742231b10e8888abcd99yyyzzz85a5")
	payload := []byte("token=xyzz0WbapA4vBCDEFasx0q6G&team_id=T1DC2JH3J&team_domain=testteamnow&channel_id=G8PSS9T3V&channel_name=foobar&user_id=U2CERLKJA&user_name=roadrunner&command=%2Fwebhook-collect&text=&response_url=https%3A%2F%2Fhooks.slack.com%2Fcommands%2FT1DC2JH3J%2F397700885554%2F96rGlfmibIGlgcZRskXaIFfN&trigger_id=398738663015.47445629121.803a0bc887a14d10d2c447fce8b6703c")
	at := time.Unix(1531420618, 0)

	s := webhook.Slack()
	h, err := s.Sign(secret, payload, at)
	require.NoError(t, err)
	assert.Equal(t, "v0=a2114d57b48eac39b9ad189dd8316235a7b4a8d21a10bd27519666489c69b503", h.Get("X-Slack-Signature"))
	assert.Equal(t, "1531420618", h.Get("X-Slack-Request-Timestamp"))

	require.NoError(t, s.Verify(secret, payload, h, at, 5*time.Minute))
}

func TestSlackToleranceAndMalformed(t *testing.T) {
	t.Parallel()
	now := time.Now()
	s := webhook.Slack()
	h, err := s.Sign(testSecret, testPayload, now.Add(-10*time.Minute))
	require.NoError(t, err)
	assert.ErrorIs(t, s.Verify(testSecret, testPayload, h, now, 5*time.Minute), webhook.ErrInvalidTimestamp)

	h, err = s.Sign(testSecret, testPayload, now)
	require.NoError(t, err)

	noTS := h.Clone()
	noTS.Del("X-Slack-Request-Timestamp")
	assert.ErrorIs(t, s.Verify(testSecret, testPayload, noTS, now, time.Minute), webhook.ErrMissingSignature)

	badTS := h.Clone()
	badTS.Set("X-Slack-Request-Timestamp", "notanumber")
	assert.ErrorIs(t, s.Verify(testSecret, testPayload, badTS, now, time.Minute), webhook.ErrInvalidTimestamp)

	badPrefix := h.Clone()
	badPrefix.Set("X-Slack-Signature", "v1="+h.Get("X-Slack-Signature")[3:])
	assert.ErrorIs(t, s.Verify(testSecret, testPayload, badPrefix, now, time.Minute), webhook.ErrInvalidSignature)
}

func TestSchemeCustomHeaders(t *testing.T) {
	t.Parallel()
	now := time.Now()

	s := webhook.Stripe(webhook.WithSignatureHeader("Webhook-Signature"))
	h, err := s.Sign(testSecret, testPayload, now)
	require.NoError(t, err)
	assert.NotEmpty(t, h.Get("Webhook-Signature"))
	assert.Empty(t, h.Get("Stripe-Signature"))
	require.NoError(t, s.Verify(testSecret, testPayload, h, now, time.Minute))

	sl := webhook.Slack(webhook.WithSignatureHeader("X-Partner-Sig"), webhook.WithTimestampHeader("X-Partner-Ts"))
	h, err = sl.Sign(testSecret, testPayload, now)
	require.NoError(t, err)
	assert.NotEmpty(t, h.Get("X-Partner-Sig"))
	assert.NotEmpty(t, h.Get("X-Partner-Ts"))
	require.NoError(t, sl.Verify(testSecret, testPayload, h, now, time.Minute))
}
