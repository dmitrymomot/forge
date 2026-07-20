package webhook

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"time"
)

// Endpoint is one partner-registered delivery target: where to POST and which
// secret signs the payload. Endpoints (with their event subscriptions and
// state) are consumer data — a Resolver fetches them at fire time.
type Endpoint struct {
	URL    string
	Secret []byte
}

func (ep Endpoint) validate() error {
	if len(ep.Secret) == 0 {
		return fmt.Errorf("%w: empty secret", ErrInvalidEndpoint)
	}
	u, err := url.Parse(ep.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("%w: %q is not an absolute http(s) URL", ErrInvalidEndpoint, ep.URL)
	}
	return nil
}

// Result reports a fired delivery for audit logging: the receiver's status
// code (0 when no response arrived).
type Result struct {
	StatusCode int
}

// Sender fires signed webhook deliveries through one reused *http.Client.
type Sender struct {
	client      *http.Client
	scheme      Scheme
	keyHeader   string
	contentType string
}

// New returns a Sender. Defaults: the Stripe-style scheme under a neutral
// "Webhook-Signature" header, idempotency keys in "Webhook-Id",
// "application/json" bodies, and an httpclient with a 10s overall timeout
// that does not follow redirects (a redirect is a permanent delivery
// failure, never a re-send elsewhere).
func New(opts ...Option) *Sender {
	c := newConfig(opts...)
	return &Sender{client: c.client, scheme: c.scheme, keyHeader: c.keyHeader, contentType: c.contentType}
}

// Send signs payload for ep and POSTs it once. key, when non-empty, rides in
// the idempotency header so receivers can dedup across retries — pass the
// same key on every attempt (Enqueue does). The outcome maps onto a queue's
// retry decision: nil for 2xx, ErrTransientStatus for 408/429/5xx (retry),
// ErrPermanentStatus for any other status (the endpoint or event is wrong).
// Transport failures return the underlying error with a zero StatusCode.
func (s *Sender) Send(ctx context.Context, ep Endpoint, payload []byte, key string) (Result, error) {
	if s == nil || s.client == nil { // zero Sender bypassed New
		return Result{}, errors.New("webhook: sender not constructed with New")
	}
	if err := ep.validate(); err != nil {
		return Result{}, err
	}
	sig, err := s.scheme.Sign(ep.Secret, payload, time.Now())
	if err != nil {
		return Result{}, fmt.Errorf("webhook: sign: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ep.URL, bytes.NewReader(payload))
	if err != nil {
		return Result{}, fmt.Errorf("webhook: build request: %w", err)
	}
	req.Header.Set("Content-Type", s.contentType)
	if key != "" {
		req.Header.Set(s.keyHeader, key)
	}
	maps.Copy(req.Header, sig)

	resp, err := s.client.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("webhook: send: %w", err)
	}
	// Bounded drain so the connection is reusable; receivers answer tiny bodies.
	//nolint:nilaway // resp is non-nil whenever err is nil, per http.Client.Do's contract
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	_ = resp.Body.Close()

	res := Result{StatusCode: resp.StatusCode}
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return res, nil
	case resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return res, fmt.Errorf("%w: %d", ErrTransientStatus, resp.StatusCode)
	default:
		return res, fmt.Errorf("%w: %d", ErrPermanentStatus, resp.StatusCode)
	}
}
