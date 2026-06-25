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
// functional options. New returns ErrInvalidConfig for bad option or Config values
// (multiple invalid options are joined together); it returns ErrOpenFile if the log
// file cannot be opened — these two errors are on separate paths and are never joined
// to each other. A file opened via Config.File is held open for the lifetime of the
// process and never closed (like os.Stdout); no closer is returned, so call New once at
// startup rather than per request. NewNope returns a discard logger. The package imports
// only the standard library.
package logger
