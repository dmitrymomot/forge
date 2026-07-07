// Package bootstrap is the thin runtime integrator for a forge process's
// main(). It wires together the sibling ops packages — logger, automaxprocs,
// buildinfo, logredact, config, and supervisor — into a one-line entry point,
// so a typical main() reduces to:
//
//	func main() {
//		os.Exit(bootstrap.Run(context.Background(), "svc", run))
//	}
//
// bootstrap owns only the runtime edges, in order: build the logger from
// LOG_* env (or an injected *slog.Logger via WithLogger), wrap it in
// logredact when WithRedactKeys is set, tune GOMAXPROCS/GOMEMLIMIT via
// automaxprocs (unless WithAutoMaxProcs(false)), log build info when
// WithBuildInfo is set, derive a SIGINT/SIGTERM-aware context via
// supervisor.NewContext, then call the caller's function body.
//
// It does NOT own the service lifecycle: the callback (Func or ConfigFunc[T])
// wraps the whole app body, so the caller invokes supervisor.Run itself and
// uses plain defer for cleanup — teardown runs after the callback returns,
// whether that's after a graceful drain or an early setup error. A nil
// result, or an error satisfying errors.Is against context.Canceled or
// context.DeadlineExceeded (a cancelled or timed-out parent context), is a
// clean stop (exit code 0); any other error logs and exits 1.
//
// RunWithConfig[T] additionally autoloads a typed application config before
// invoking the callback: config.LoadEnv[T] by default, or the layered
// YAML+env loader (config.Load[T]) when WithConfigDir is set. T is inferred
// from the callback's signature. A load failure is logged and yields exit
// code 1 without calling the callback. A fuller shape, wiring a database and
// an HTTP server behind supervisor.Run, looks like:
//
//	/*
//	type AppConfig struct {
//		HTTP httpserver.Config
//		DB   dbConfig
//	}
//
//	func main() {
//		os.Exit(bootstrap.RunWithConfig(context.Background(), "svc",
//			func(ctx context.Context, log *slog.Logger, cfg AppConfig) error {
//				db, err := openDB(cfg.DB)
//				if err != nil {
//					return err
//				}
//				defer db.Close()
//
//				srv := newServer(db, cfg.HTTP)
//				return supervisor.Run(ctx, log, srv)
//			},
//			bootstrap.WithBuildInfo(buildinfo.Read()),
//		))
//	}
//	*/
package bootstrap
