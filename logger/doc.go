// Package logger builds configured *slog.Logger values over the standard library.
//
// New returns a logger with a single primary destination — stdout by default, or a
// local file (created with parent dirs) when Config.File is set; the two are mutually
// exclusive. Optional context extractors inject request-scoped attributes at the
// record's top level on every call, and WithHandler attaches extra parallel
// destinations (used by logger/sentry) beneath that extraction.
//
//	log, err := logger.New(
//		logger.WithFormat(logger.FormatJSON),
//		logger.WithContextExtractors(reqIDExtractor),
//	)
//
// Serializable settings live in an env-loadable Config (Level, Format, File, AddSource)
// with a DefaultConfig and Validate; writers, handlers, and extractor funcs are
// functional options. Invalid values are reported by New as a joined ErrInvalidConfig
// and ErrOpenFile; no closer is returned because file writes are unbuffered syscalls.
// NewNope returns a discard logger. The package imports only the standard library.
package logger
