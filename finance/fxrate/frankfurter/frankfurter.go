// Package frankfurter is a thin JSON adapter over the Frankfurter API
// (https://frankfurter.dev — ECB reference rates, no API key) implementing
// fxrate.RateSource. It is deliberately minimal: one GET, a body-capped JSON
// decode straight into core/decimal (never through float64), and
// fxrate.NewSnapshot validation on the way out.
//
// # Usage
//
//	src := frankfurter.New()
//	snap, err := src.Fetch(ctx, "EUR", []string{"USD", "GBP"})
package frankfurter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dmitrymomot/forge/core/decimal"
	"github.com/dmitrymomot/forge/finance/fxrate"
	"github.com/dmitrymomot/forge/web/httpclient"
)

// DefaultBaseURL is the public Frankfurter API root.
const DefaultBaseURL = "https://api.frankfurter.dev/v1"

// Provider is the source name recorded in snapshots.
const Provider = "frankfurter"

// maxBody caps the response body read; the full-table payload is ~1 KB.
const maxBody = 1 << 20

// Source implements fxrate.RateSource over the Frankfurter JSON API.
type Source struct {
	client  *http.Client
	baseURL string
}

// Option configures a Source.
type Option func(*Source)

// WithBaseURL overrides the API root, e.g. for a self-hosted instance or a
// test server. A trailing slash is trimmed.
func WithBaseURL(u string) Option {
	return func(s *Source) { s.baseURL = strings.TrimRight(u, "/") }
}

// WithHTTPClient overrides the HTTP client. The default is an httpclient with
// a 15s overall timeout and 5s per attempt; GET retries are on by default.
func WithHTTPClient(c *http.Client) Option {
	return func(s *Source) { s.client = c }
}

// New builds a Source talking to the public Frankfurter API.
func New(opts ...Option) *Source {
	s := &Source{baseURL: DefaultBaseURL}
	for _, opt := range opts {
		opt(s)
	}
	if s.client == nil {
		s.client = httpclient.New(
			httpclient.WithTimeout(15*time.Second),
			httpclient.WithPerAttemptTimeout(5*time.Second),
		)
	}
	return s
}

// payload is Frankfurter's /latest response shape. Rates arrive as bare JSON
// numbers; decimal.UnmarshalJSON parses the token text exactly.
type payload struct {
	Rates map[string]decimal.Decimal `json:"rates"`
	Base  string                     `json:"base"`
	Date  string                     `json:"date"`
}

// Fetch implements fxrate.RateSource, requesting the latest rates for base.
// Empty quotes fetches every currency Frankfurter publishes.
func (s *Source) Fetch(ctx context.Context, base string, quotes []string) (fxrate.Snapshot, error) {
	base = strings.ToUpper(strings.TrimSpace(base))
	if base == "" {
		return fxrate.Snapshot{}, errors.New("frankfurter: empty base currency")
	}

	q := url.Values{"base": []string{base}}
	if len(quotes) > 0 {
		symbols := make([]string, 0, len(quotes))
		for _, code := range quotes {
			if code = strings.ToUpper(strings.TrimSpace(code)); code != "" {
				symbols = append(symbols, code)
			}
		}
		if len(symbols) > 0 {
			q.Set("symbols", strings.Join(symbols, ","))
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+"/latest?"+q.Encode(), nil)
	if err != nil {
		return fxrate.Snapshot{}, fmt.Errorf("frankfurter: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return fxrate.Snapshot{}, fmt.Errorf("frankfurter: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	//nolint:nilaway // resp is non-nil whenever err is nil, per http.Client.Do's contract
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return fxrate.Snapshot{}, fmt.Errorf("frankfurter: unexpected status %s", resp.Status)
	}

	var p payload
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&p); err != nil {
		return fxrate.Snapshot{}, fmt.Errorf("frankfurter: decode response: %w", err)
	}
	asOf, err := time.ParseInLocation(time.DateOnly, p.Date, time.UTC)
	if err != nil {
		return fxrate.Snapshot{}, fmt.Errorf("frankfurter: parse date %q: %w", p.Date, err)
	}

	snap, err := fxrate.NewSnapshot(p.Base, Provider, asOf, p.Rates)
	if err != nil {
		return fxrate.Snapshot{}, fmt.Errorf("frankfurter: %w", err)
	}
	return snap, nil
}
