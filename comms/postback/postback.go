package postback

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// Sender fires rendered postbacks through one reused *http.Client.
type Sender struct {
	client *http.Client
}

// New returns a Sender. Without WithHTTPClient it builds a default
// httpclient.New client with a 10s overall timeout — which retries transient
// failures on GET but not POST (non-idempotent); bring your own client to
// change either.
func New(opts ...Option) *Sender {
	c := newConfig(opts...)
	return &Sender{client: c.client}
}

// Result reports a fired postback for audit logging: the fully rendered URL
// and the tracker's status code (0 when no response arrived).
type Result struct {
	URL        string
	StatusCode int
}

// Send renders dest against the per-event values map and fires it. The
// outcome is reported by status class: nil error for 2xx, ErrServerStatus for
// 5xx (transient — worth retrying), ErrClientStatus for any other status
// (permanent — the destination or event is wrong). Transport failures return
// the underlying error with a zero StatusCode.
func (s *Sender) Send(ctx context.Context, dest Destination, values map[string]string) (Result, error) {
	if s == nil || s.client == nil { // zero Sender bypassed New
		return Result{}, errors.New("postback: sender not constructed with New")
	}
	if dest.method == "" { // zero Destination bypassed NewDestination
		return Result{}, fmt.Errorf("%w: zero destination", ErrInvalidTemplate)
	}
	rendered := dest.Render(values)
	req, err := http.NewRequestWithContext(ctx, dest.method, rendered, nil)
	if err != nil {
		return Result{URL: rendered}, fmt.Errorf("postback: build request: %w", err)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return Result{URL: rendered}, fmt.Errorf("postback: send: %w", err)
	}
	// Bounded drain so the connection is reusable; trackers answer tiny bodies.
	//nolint:nilaway // resp is non-nil whenever err is nil, per http.Client.Do's contract
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	_ = resp.Body.Close()

	res := Result{URL: rendered, StatusCode: resp.StatusCode}
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return res, nil
	case resp.StatusCode >= 500:
		return res, fmt.Errorf("%w: %d", ErrServerStatus, resp.StatusCode)
	default:
		return res, fmt.Errorf("%w: %d", ErrClientStatus, resp.StatusCode)
	}
}
