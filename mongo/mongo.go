package mongo

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

// maxRetryBackoff caps the exponential wait between connect attempts.
const maxRetryBackoff = 30 * time.Second

// Open turns a Config (typically env-loaded) into a live, pooled, health-checkable
// *mongo.Database: it applies options over DefaultConfig(), validates, builds the
// driver client options from Config (URI, pool limits, timeouts, read/write
// concerns), runs the WithClientOptions escape hatch last, connects, pings with
// bounded exponential-backoff retry, then returns client.Database(cfg.Database). On
// failure it disconnects any partially opened client and returns a sentinel-wrapped,
// single-line error. The caller owns the returned database and closes it with
// Close(db, logger) in main; use db.Client() to reach client-level ops.
func Open(ctx context.Context, opts ...Option) (*mongodriver.Database, error) {
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
		logger = slog.Default()
	}

	clientOpts, err := buildClientOptions(c.Config)
	if err != nil {
		return nil, err // already ErrInvalidConfig-wrapped
	}
	if c.clientOptions != nil {
		c.clientOptions(clientOpts) // escape hatch runs LAST
	}

	client, err := connectWithRetry(ctx, c.Config, clientOpts, logger)
	if err != nil {
		return nil, err
	}
	return client.Database(c.Database), nil
}

// buildClientOptions assembles *options.ClientOptions from Config. The connection
// string is the base (ApplyURI runs first); non-zero Config fields override any
// overlapping URI query-string parameters, matching the convention used by
// postgres.Open. Empty concern strings leave the driver defaults untouched.
func buildClientOptions(cfg Config) (*options.ClientOptions, error) {
	// Connection string is the base; Config fields overlay it below.
	o := options.Client().ApplyURI(cfg.URI)
	o.SetMaxPoolSize(cfg.MaxPoolSize)
	o.SetMinPoolSize(cfg.MinPoolSize)
	if cfg.ConnectTimeout > 0 {
		o.SetConnectTimeout(cfg.ConnectTimeout)
	}
	if cfg.ServerSelectionTimeout > 0 {
		o.SetServerSelectionTimeout(cfg.ServerSelectionTimeout)
	}
	if cfg.MaxConnIdleTime > 0 {
		o.SetMaxConnIdleTime(cfg.MaxConnIdleTime)
	}

	rp, err := parseReadPreference(cfg.ReadPreference)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidConfig, err)
	}
	if rp != nil {
		o.SetReadPreference(rp)
	}
	rc, err := parseReadConcern(cfg.ReadConcern)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidConfig, err)
	}
	if rc != nil {
		o.SetReadConcern(rc)
	}
	wc, err := parseWriteConcern(cfg.WriteConcern)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidConfig, err)
	}
	if wc != nil {
		o.SetWriteConcern(wc)
	}
	return o, nil
}

// connectWithRetry connects and pings with bounded exponential backoff. Each
// attempt builds a fresh client (Connect does not dial; the Ping confirms a live
// server). On a failed ping the client is disconnected before retrying so nothing
// leaks. The wait is RetryInterval·2^attempt capped at maxRetryBackoff and honors
// ctx cancellation. After RetryAttempts it returns ErrConnect joined with the last
// driver error. RetryAttempts <= 1 means a single attempt with no wait.
func connectWithRetry(ctx context.Context, cfg Config, clientOpts *options.ClientOptions, logger *slog.Logger) (*mongodriver.Client, error) {
	attempts := max(cfg.RetryAttempts, 1)

	var lastErr error
	for attempt := range attempts {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrConnect, err)
		}

		client, err := mongodriver.Connect(clientOpts)
		if err != nil {
			lastErr = err
		} else if err = client.Ping(ctx, readpref.Primary()); err != nil {
			lastErr = err
			// Disconnect the partially opened client under a short bounded context.
			_ = disconnect(client)
		} else {
			return client, nil
		}

		// No wait after the final attempt.
		if attempt == attempts-1 {
			break
		}
		wait := backoff(cfg.RetryInterval, attempt)
		logger.Warn("mongo connect attempt failed; retrying",
			slog.Int("attempt", attempt+1),
			slog.Int("attempts", attempts),
			slog.Duration("wait", wait),
			slog.String("err", lastErr.Error()),
		)
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, fmt.Errorf("%w: %v", ErrConnect, ctx.Err())
		case <-timer.C:
		}
	}
	return nil, fmt.Errorf("%w: %v", ErrConnect, lastErr)
}

// backoff returns interval·2^attempt capped at maxRetryBackoff (and never below 0).
func backoff(interval time.Duration, attempt int) time.Duration {
	if interval <= 0 {
		return 0
	}
	wait := interval << attempt // interval * 2^attempt
	if wait <= 0 || wait > maxRetryBackoff {
		return maxRetryBackoff
	}
	return wait
}

// disconnect tears down a client under a short bounded context and returns any
// error from Disconnect.
func disconnect(client *mongodriver.Client) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return client.Disconnect(ctx)
}

// Close logs a single "closing mongo client" line and disconnects the underlying
// client (via db.Client().Disconnect) under a short internal bounded context (5s),
// keeping the uniform no-ctx signature. Used as `defer Close(db, logger)` in main,
// so it runs after supervisor.Run returns — i.e. after every service has drained,
// the only point at which disconnecting is guaranteed not to race in-flight work. A
// nil logger is tolerated (the client still closes; the log line is skipped); a nil
// db is a no-op.
func Close(db *mongodriver.Database, log *slog.Logger) {
	if db == nil {
		return
	}
	if log != nil {
		log.Info("closing mongo client")
	}
	if err := disconnect(db.Client()); err != nil && log != nil {
		log.Warn("mongo client disconnect failed", "err", err)
	}
}

// Healthcheck returns a stateless closure that pings the primary via
// db.Client().Ping, wrapping any failure in ErrHealthcheck. Its
// func(context.Context) error shape is exactly what a readiness/liveness probe
// wants; hand it to the app's /readyz handler. Safe to call on every probe.
func Healthcheck(db *mongodriver.Database) func(context.Context) error {
	return func(ctx context.Context) error {
		if err := db.Client().Ping(ctx, readpref.Primary()); err != nil {
			return fmt.Errorf("%w: %v", ErrHealthcheck, err)
		}
		return nil
	}
}
