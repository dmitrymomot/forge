package supervisor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
)

// Run starts every registered service, supervises them, and blocks until
// shutdown completes. The first service to return (nil or error), or
// cancellation of ctx, begins a coordinated shutdown: the shared context is
// cancelled so every service drains, and Run waits for them all to return.
// Run returns the joined non-context.Canceled service errors, or nil on a
// clean stop.
func Run(ctx context.Context, opts ...Option) error {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	log := resolveLogger(cfg.logger)

	if len(cfg.services) == 0 {
		return ErrNoServices
	}
	for _, svc := range cfg.services {
		if svc.Name() == "" {
			return ErrUnnamedService
		}
	}
	warnDuplicateNames(log, cfg.services)

	if err := ctx.Err(); err != nil {
		log.Info("context already cancelled, nothing to run")
		return nil
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	type result struct {
		idx  int
		name string
		err  error
	}
	results := make(chan result, len(cfg.services))
	remaining := make(map[int]string, len(cfg.services))

	for i, svc := range cfg.services {
		remaining[i] = svc.Name()
		go func() {
			log.Info("service started", slog.String("service", svc.Name()))
			err := runService(runCtx, svc)
			results <- result{idx: i, name: svc.Name(), err: err}
		}()
	}

	var (
		errs         []error
		done         = runCtx.Done()
		shuttingDown bool
	)

	beginShutdown := func(reason string) {
		if shuttingDown {
			return
		}
		shuttingDown = true
		log.Info("shutdown started", slog.String("reason", reason))
		cancel()
		done = nil
	}

	for len(remaining) > 0 {
		select {
		case res := <-results:
			delete(remaining, res.idx)
			log.Info("service stopped",
				slog.String("service", res.name), slog.Any("err", res.err))
			if res.err != nil && !errors.Is(res.err, context.Canceled) {
				errs = append(errs, fmt.Errorf("service %q: %w", res.name, res.err))
			}
			beginShutdown(fmt.Sprintf("service %q exited", res.name))
		case <-done:
			beginShutdown("context cancelled")
		}
	}

	log.Info("shutdown complete")
	return errors.Join(errs...)
}

// runService runs a single service. Panic recovery is added in a later task.
func runService(ctx context.Context, svc Service) error {
	return svc.Run(ctx)
}

// resolveLogger returns l, or a discard logger when l is nil.
func resolveLogger(l *slog.Logger) *slog.Logger {
	if l == nil {
		return slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return l
}

// warnDuplicateNames logs a warning for each service name that appears more
// than once. Duplicates are permitted; they only hurt log readability.
func warnDuplicateNames(log *slog.Logger, services []Service) {
	seen := make(map[string]struct{}, len(services))
	for _, svc := range services {
		name := svc.Name()
		if _, dup := seen[name]; dup {
			log.Warn("duplicate service name", slog.String("service", name))
			continue
		}
		seen[name] = struct{}{}
	}
}
