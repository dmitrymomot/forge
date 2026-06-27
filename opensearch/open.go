package opensearch

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	osgo "github.com/opensearch-project/opensearch-go/v4"
	osapi "github.com/opensearch-project/opensearch-go/v4/opensearchapi"
)

// maxBackoff caps the exponential connect-retry wait.
const maxBackoff = 30 * time.Second

// Open builds an OpenSearch client from Config plus options, then verifies the
// cluster is reachable with a bounded retry/backoff. It returns the base
// *opensearch.Client (callers wrap it with opensearchapi for typed requests). On
// failure it returns a sentinel-wrapped, single-line error and leaks nothing.
func Open(ctx context.Context, opts ...Option) (*osgo.Client, error) {
	c := config{Config: DefaultConfig()}
	for _, opt := range opts {
		opt(&c)
	}
	if len(c.errs) > 0 {
		return nil, errors.Join(c.errs...)
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}

	logger := c.logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	// Build the driver config. RequestTimeout and InsecureSkipVerify live on the
	// transport, not osgo.Config; MaxRetries is a driver field.
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: c.InsecureSkipVerify}, //nolint:gosec // opt-in via Config for dev/self-signed
		ResponseHeaderTimeout: c.RequestTimeout,
	}
	driverCfg := osgo.Config{
		Addresses:  c.Addresses,
		Username:   c.Username,
		Password:   c.Password,
		MaxRetries: c.MaxRetries,
		Transport:  transport,
	}
	// Escape hatch runs LAST so it can override anything above.
	if c.clientConfig != nil {
		c.clientConfig(&driverCfg)
	}

	base, err := osgo.NewClient(driverCfg)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrConnect, err)
	}

	if err := waitForCluster(ctx, base, c.Config, logger); err != nil {
		return nil, err
	}

	logger.Info("opensearch: connected", slog.Any("addresses", c.Addresses))
	return base, nil
}

// waitForCluster pings the cluster (cluster health) until it responds or the retry
// budget is spent. Backoff is RetryInterval * 2^attempt capped at maxBackoff; ctx
// cancellation is honored between attempts. RetryAttempts <= 1 means a single
// attempt with no wait.
func waitForCluster(ctx context.Context, base *osgo.Client, cfg Config, logger *slog.Logger) error {
	attempts := max(cfg.RetryAttempts, 1)

	var lastErr error
	for attempt := range attempts {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("%w: %v", ErrConnect, errors.Join(err, lastErr))
		}
		if attempt > 0 {
			wait := backoff(cfg.RetryInterval, attempt)
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return fmt.Errorf("%w: %v", ErrConnect, errors.Join(ctx.Err(), lastErr))
			case <-timer.C:
			}
		}

		lastErr = ping(ctx, base, cfg.RequestTimeout)
		if lastErr == nil {
			return nil
		}
		logger.Warn("opensearch: connect attempt failed",
			slog.Int("attempt", attempt+1),
			slog.Int("attempts", attempts),
			slog.Any("err", lastErr),
		)
	}
	return fmt.Errorf("%w: %v", ErrConnect, lastErr)
}

// backoff returns interval * 2^attempt, capped at maxBackoff.
func backoff(interval time.Duration, attempt int) time.Duration {
	if interval <= 0 {
		return 0
	}
	wait := interval << attempt
	if wait <= 0 || wait > maxBackoff { // overflow or over the cap
		return maxBackoff
	}
	return wait
}

// ping issues a cluster-health request under a per-attempt timeout derived from
// RequestTimeout (0 means no extra deadline beyond ctx).
func ping(ctx context.Context, base *osgo.Client, timeout time.Duration) error {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	api := apiFor(base)
	if _, err := api.Cluster.Health(ctx, nil); err != nil {
		return err
	}
	return nil
}

// apiFor wraps an existing base client in an opensearchapi.Client so typed
// requests reuse the base's transport. opensearch-go v4 exposes no public wrap
// function, so a throwaway api client is built and its base swapped out; every
// typed call routes through apiClient.Client.Do.
func apiFor(base *osgo.Client) *osapi.Client {
	// TODO: opensearch-go v4 exposes no public "wrap an existing client" call, so we
	// build a throwaway api client and swap its .Client field. The placeholder address
	// (port 0, never client-connectable) only has to be syntactically valid.
	api, err := osapi.NewClient(osapi.Config{Client: osgo.Config{Addresses: []string{"http://127.0.0.1:0"}}})
	if err != nil {
		return &osapi.Client{Client: base}
	}
	api.Client = base
	return api
}
