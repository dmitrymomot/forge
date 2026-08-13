// Package logger builds configured *slog.Logger values over the standard library.
//
// New returns a logger with a single primary destination — stdout by default, or a
// local file (created with parent dirs) when Config.File is set; the two are mutually
// exclusive. Optional context extractors inject request-scoped attributes at the
// record's top level on every call. WithHandler attaches extra parallel destinations
// beneath that extraction, and WithLeveledHandler does the same with a per-destination
// minimum level (e.g. stdout at info while a file handler receives only error+).
//
//	log, err := logger.New(
//		logger.WithFormat(logger.FormatJSON),
//		logger.WithContextExtractors(
//			logger.ContextValue[string](requestIDKey, "request_id"),
//		),
//	)
//
// ContextValue covers the common "read one typed value under one key" extractor; write
// a ContextExtractor by hand when the value needs shaping first.
//
// NewAsync is New with a buffered async core: log calls never block on the sink — they
// extract context attributes, clone the record, and enqueue; a single worker goroutine
// formats and writes to every destination. When the buffer (default 8192 records,
// WithAsyncBufferSize) is full, new records are dropped, counted, and later reported as
// a Warn record ("logger: dropped log records", dropped=N); WithDropHook delivers the
// same tally to a func, which is how a metrics counter sees drops. The single worker goroutine
// runs until the returned CloseFunc is called; CloseFunc drains the buffer and must run on
// shutdown, before flushing downstream sinks, or the goroutine leaks. Records logged after
// Close are silently dropped, and records buffered at crash/os.Exit are lost. Keep the
// sync New wherever those trade-offs are unacceptable.
//
//	log, closeLog, err := logger.NewAsync(logger.WithFormat(logger.FormatJSON))
//	defer closeLog(ctx)
//
// Serializable settings live in an env-loadable Config (Level, Format, File, AddSource)
// with a DefaultConfig and Validate; writers, handlers, and extractor funcs are
// functional options. New and NewAsync return ErrInvalidConfig for bad option or Config
// values (multiple invalid options are joined together) and ErrOpenFile if the log file
// cannot be opened — these two errors are on separate paths and are never joined to each
// other. A file opened via Config.File is held open for the lifetime of the process and
// never closed (like os.Stdout); no closer is returned for it, so call New once at
// startup rather than per request. NewNope returns a discard logger. The package imports
// only the standard library.
package logger
