// Package logsample provides an slog.Handler that caps log volume by sampling
// high-frequency records while always passing important ones.
//
// It decorates any slog.Handler: records at or above a threshold level
// (default slog.LevelWarn) always pass; records below it are sampled "keep 1 of
// N" (default 1 of 10) via a shared atomic counter, so a chatty Info/Debug path
// cannot flood the logs or the log bill. Handlers derived via Logger.With share
// the counter and sample the stream as one.
//
//	base := slog.NewJSONHandler(os.Stdout, nil)
//	logger := slog.New(logsample.New(base,
//		logsample.WithRate(100),                // keep 1% of sub-threshold records
//		logsample.WithMinLevel(slog.LevelError), // only Error+ bypasses sampling (default is Warn)
//	))
//	logger.Info("cache miss")  // sampled
//	logger.Error("db down")    // always logged
package logsample
