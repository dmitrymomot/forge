package eventrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"time"

	"github.com/dmitrymomot/forge/web/httpclient"
)

// HTTPDeliverer is the generic JSON-batch adapter: it POSTs each batch as a
// JSON array of Event objects. Single-event deliveries additionally carry the
// event ID in an Idempotency-Key header; in batches the per-event "id" field
// is the dedup key.
type HTTPDeliverer struct {
	client *http.Client
	header http.Header
	url    string
}

// HTTPOption configures NewHTTPDeliverer.
type HTTPOption func(*HTTPDeliverer)

// WithHTTPClient replaces the default client (an httpclient.New with a 10s
// overall timeout; POSTs are not transport-retried — the router owns retry).
func WithHTTPClient(client *http.Client) HTTPOption {
	return func(h *HTTPDeliverer) { h.client = client }
}

// WithHTTPHeader adds a static header to every delivery (auth tokens,
// content-type overrides).
func WithHTTPHeader(key, value string) HTTPOption {
	return func(h *HTTPDeliverer) { h.header.Set(key, value) }
}

// NewHTTPDeliverer builds an HTTPDeliverer POSTing to rawURL. The URL is
// consumer config, so a non-absolute or non-http(s) URL is an ErrInvalidURL
// error, not a panic.
func NewHTTPDeliverer(rawURL string, opts ...HTTPOption) (*HTTPDeliverer, error) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("%w: %q is not an absolute http(s) URL", ErrInvalidURL, rawURL)
	}
	h := &HTTPDeliverer{url: rawURL, header: http.Header{}}
	for _, opt := range opts {
		opt(h)
	}
	if h.client == nil {
		h.client = httpclient.New(httpclient.WithTimeout(10 * time.Second))
	}
	return h, nil
}

// Deliver POSTs the batch and classifies the response by status class: nil
// for 2xx, transient (retryable) for 408/429/5xx and transport failures,
// Permanent for everything else.
func (h *HTTPDeliverer) Deliver(ctx context.Context, events []Event) error {
	if h == nil || h.client == nil { // zero deliverer bypassed NewHTTPDeliverer
		return errors.New("eventrouter: http deliverer not constructed with NewHTTPDeliverer")
	}
	body, err := json.Marshal(events)
	if err != nil {
		return Permanent(fmt.Errorf("eventrouter: encode batch: %w", err))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.url, bytes.NewReader(body))
	if err != nil {
		return Permanent(fmt.Errorf("eventrouter: build request: %w", err))
	}
	req.Header.Set("Content-Type", "application/json")
	maps.Copy(req.Header, h.header)
	if len(events) == 1 {
		req.Header.Set("Idempotency-Key", events[0].ID)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return fmt.Errorf("eventrouter: deliver: %w", err)
	}
	// Bounded drain so the connection is reusable; receivers answer tiny bodies.
	//nolint:nilaway // resp is non-nil whenever err is nil, per http.Client.Do's contract
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	_ = resp.Body.Close()

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	case resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return fmt.Errorf("eventrouter: transient status %d", resp.StatusCode)
	default:
		return Permanent(fmt.Errorf("eventrouter: status %d", resp.StatusCode))
	}
}
