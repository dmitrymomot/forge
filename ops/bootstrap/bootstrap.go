package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/dmitrymomot/forge/ops/automaxprocs"
	"github.com/dmitrymomot/forge/ops/buildinfo"
	"github.com/dmitrymomot/forge/ops/config"
	"github.com/dmitrymomot/forge/ops/logger"
	"github.com/dmitrymomot/forge/ops/logredact"
	"github.com/dmitrymomot/forge/ops/supervisor"
)

// Func is a config-less application body. It receives a context cancelled on
// SIGINT/SIGTERM (or when the ctx passed to Run is cancelled) and the configured
// logger, wires the app, and typically ends by returning supervisor.Run(ctx,…).
// A nil result, or a context.Canceled / context.DeadlineExceeded error from a
// cancelled or timed-out parent context, is a clean stop; any other error makes
// Run return exit code 1. Use plain defer inside fn for resource teardown — it
// runs after fn returns (i.e. after supervisor.Run drains), and on any early error.
type Func func(ctx context.Context, log *slog.Logger) error

// ConfigFunc is an application body that also receives a loaded, typed config.
// T is inferred from the function literal at the call site.
type ConfigFunc[T any] func(ctx context.Context, log *slog.Logger, cfg T) error

type options struct {
	logger      *slog.Logger
	build       *buildinfo.Info
	configDir   string
	redactKeys  []string
	configOpts  []config.Option
	autoMaxProc bool
	forceQuit   bool
}

// Option configures Run and RunWithConfig.
type Option func(*options)

// WithLogger supplies a prebuilt logger instead of building one from LOG_* env.
// WithRedactKeys still wraps it.
func WithLogger(l *slog.Logger) Option { return func(o *options) { o.logger = l } }

// WithBuildInfo logs the build identity once at startup and is otherwise inert.
func WithBuildInfo(b buildinfo.Info) Option { return func(o *options) { o.build = &b } }

// WithRedactKeys wraps the logger in logredact, redacting these attribute keys.
func WithRedactKeys(keys ...string) Option { return func(o *options) { o.redactKeys = keys } }

// WithAutoMaxProcs toggles the automaxprocs step (default on).
func WithAutoMaxProcs(on bool) Option { return func(o *options) { o.autoMaxProc = on } }

// WithForceQuit makes a second SIGINT/SIGTERM force os.Exit(130) while the first
// drains gracefully — an escape hatch for a hung shutdown. Forwarded to
// supervisor.NewContext.
func WithForceQuit() Option { return func(o *options) { o.forceQuit = true } }

// WithConfigDir switches RunWithConfig from env-only loading to the layered
// YAML+env loader rooted at dir (config.Load). Ignored by Run.
func WithConfigDir(dir string) Option { return func(o *options) { o.configDir = dir } }

// WithConfigOptions forwards options to the underlying config loader (dotenv,
// profile, lookup). Ignored by Run.
func WithConfigOptions(opts ...config.Option) Option {
	return func(o *options) { o.configOpts = opts }
}

// Run bootstraps the process runtime and executes fn (no app config), returning
// a process exit code to pass to os.Exit. In order it: builds the logger from
// LOG_* env (or WithLogger), wrapping it in logredact when WithRedactKeys is set;
// tunes GOMAXPROCS/GOMEMLIMIT via automaxprocs unless WithAutoMaxProcs(false);
// logs build info if WithBuildInfo; derives a SIGINT/SIGTERM-aware context from
// ctx via supervisor.NewContext; then calls fn. bootstrap owns the runtime edges
// only — fn owns the service lifecycle (supervisor.Run) and defer-based cleanup.
func Run(ctx context.Context, name string, fn Func, opts ...Option) int {
	return run(ctx, name, opts, func(runCtx context.Context, log *slog.Logger, _ options) error {
		return fn(runCtx, log)
	})
}

// RunWithConfig is Run plus generic config autoload: it loads a T (env-only via
// config.LoadEnv by default, or the layered YAML+env loader under WithConfigDir)
// and passes it to fn. A load failure is logged and yields exit code 1 without
// calling fn. T is inferred from fn.
func RunWithConfig[T any](ctx context.Context, name string, fn ConfigFunc[T], opts ...Option) int {
	return run(ctx, name, opts, func(runCtx context.Context, log *slog.Logger, o options) error {
		cfg, err := loadConfig[T](o)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		return fn(runCtx, log, cfg)
	})
}

// run holds the shared bootstrap sequence; body wraps the caller's fn (with or
// without config) and receives the resolved options.
func run(ctx context.Context, name string, opts []Option, body func(context.Context, *slog.Logger, options) error) int {
	o := options{autoMaxProc: true}
	for _, opt := range opts {
		opt(&o)
	}

	log, err := buildLogger(o)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bootstrap: logger init: %v\n", err)
		return 1
	}
	log = log.With(slog.String("app", name))

	if o.autoMaxProc {
		undo := automaxprocs.Set(log)
		defer undo()
	}
	if o.build != nil {
		log.Info("starting", slog.Any("build", *o.build))
	}

	ctxOpts := []supervisor.ContextOption{supervisor.WithContext(ctx)}
	if o.forceQuit {
		ctxOpts = append(ctxOpts, supervisor.WithForceQuit())
	}
	runCtx, stop := supervisor.NewContext(ctxOpts...)
	defer stop()

	if err := body(runCtx, log, o); err != nil &&
		!errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		log.Error("exit", slog.Any("err", err))
		return 1
	}
	log.Info("stopped")
	return 0
}

func buildLogger(o options) (*slog.Logger, error) {
	base := o.logger
	if base == nil {
		cfg, err := config.LoadEnv[logger.Config]()
		if err != nil {
			return nil, err
		}
		base, err = logger.New(logger.WithConfig(cfg))
		if err != nil {
			return nil, err
		}
	}
	if len(o.redactKeys) > 0 {
		return slog.New(logredact.New(base.Handler(), logredact.WithKeys(o.redactKeys...))), nil
	}
	return base, nil
}

func loadConfig[T any](o options) (T, error) {
	if o.configDir != "" {
		return config.Load[T](o.configDir, o.configOpts...)
	}
	return config.LoadEnv[T](o.configOpts...)
}
