package supervisor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"runtime/debug"
	"sort"
	"time"
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

	allErrs := cfg.errs
	if e := cfg.Validate(); e != nil {
		allErrs = append(allErrs, e)
	}
	if len(allErrs) > 0 {
		return errors.Join(allErrs...)
	}

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
		err  error
		name string
		idx  int
	}
	results := make(chan result, len(cfg.services))
	remaining := make(map[int]string, len(cfg.services))

	for i, svc := range cfg.services {
		remaining[i] = svc.Name()
		go func() {
			log.Info("service started", slog.String("service", svc.Name()))
			err := runService(runCtx, svc, log, cfg.Recover)
			results <- result{idx: i, name: svc.Name(), err: err}
		}()
	}

	var (
		errs         []error
		done         = runCtx.Done()
		graceCh      <-chan time.Time // nil until shutdown begins; never armed when timeout == 0
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
		if cfg.ShutdownTimeout > 0 {
			graceCh = time.After(cfg.ShutdownTimeout)
		}
	}

	for len(remaining) > 0 {
		select {
		case res := <-results:
			delete(remaining, res.idx)
			log.Info("service stopped",
				slog.String("service", res.name), slog.Any("err", res.err))
			// context.Canceled is the expected result of our own cancellation and is dropped;
			// a service's own context.DeadlineExceeded is a real failure and is surfaced.
			if res.err != nil && !errors.Is(res.err, context.Canceled) {
				errs = append(errs, fmt.Errorf("service %q: %w", res.name, res.err))
			}
			beginShutdown(fmt.Sprintf("service %q exited", res.name))
		case <-done:
			beginShutdown("context cancelled")
		case <-graceCh:
			errs = append(errs, fmt.Errorf("%w: %d service(s) did not stop within %s",
				ErrShutdownTimeout, len(remaining), cfg.ShutdownTimeout))
			log.Error("graceful shutdown timed out", slog.Any("stuck", remainingNames(remaining)))
			return errors.Join(errs...)
		}
	}

	log.Info("shutdown complete")
	return errors.Join(errs...)
}

// runService runs a single service. When recoverPanics is true, a panic
// escaping svc.Run is logged with structured attributes (service, panic,
// stack) and converted to an ErrPanic-wrapped, single-line error. The stack is
// never embedded in the returned error string.
func runService(ctx context.Context, svc Service, log *slog.Logger, recoverPanics bool) (err error) {
	if !recoverPanics {
		return svc.Run(ctx)
	}
	defer func() {
		if p := recover(); p != nil {
			log.Error("service panicked",
				slog.String("service", svc.Name()),
				slog.Any("panic", p),
				slog.String("stack", string(debug.Stack())),
			)
			err = fmt.Errorf("%w: %v", ErrPanic, p)
		}
	}()
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

// remainingNames returns the names of services still running, sorted for
// deterministic logging.
func remainingNames(remaining map[int]string) []string {
	names := make([]string, 0, len(remaining))
	for _, name := range remaining {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
