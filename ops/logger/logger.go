package logger

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
)

// New builds an *slog.Logger from the options. With no options it returns a text-format,
// info-level logger writing to os.Stdout. If Config.File is set the primary destination
// becomes that file instead of stdout; the file is opened once and held for the lifetime
// of the process (never closed, like os.Stdout), so call New once at startup rather than
// per request. Handlers added via WithHandler run as parallel destinations beneath
// context extraction. Returns ErrInvalidConfig for bad values and ErrOpenFile if the file
// cannot be opened.
func New(opts ...Option) (*slog.Logger, error) {
	c := defaultConfig()
	for _, opt := range opts {
		opt(&c)
	}
	if len(c.errs) > 0 {
		return nil, errors.Join(c.errs...)
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if c.asyncBufferSize != 0 {
		return nil, fmt.Errorf("%w: WithAsyncBufferSize is only valid with NewAsync", ErrInvalidConfig)
	}

	base, err := buildBase(c)
	if err != nil {
		return nil, err
	}
	if len(c.extractors) > 0 {
		return slog.New(newContextHandler(base, c.extractors...)), nil
	}
	return slog.New(base), nil
}

// buildBase resolves the writer and builds the handler stack beneath context extraction:
// the primary destination plus any extra parallel handlers. Shared by New and NewAsync.
func buildBase(c config) (slog.Handler, error) {
	level := parseLevel(c.Level)
	if c.levelOverride != nil {
		level = *c.levelOverride
	}
	format := parseFormat(c.Format)
	if c.formatOverride != nil {
		format = *c.formatOverride
	}

	w, err := c.resolveWriter()
	if err != nil {
		return nil, err
	}

	primary := newHandler(format, w, level, c.AddSource)
	if len(c.extraHandlers) > 0 {
		return slog.NewMultiHandler(append([]slog.Handler{primary}, c.extraHandlers...)...), nil
	}
	return primary, nil
}

// resolveWriter picks the single primary writer: WithOutput override, else the file,
// else stdout.
func (c config) resolveWriter() (io.Writer, error) {
	switch {
	case c.outputOverride != nil:
		return c.outputOverride, nil
	case c.File != "":
		f, err := openFile(c.File)
		if err != nil {
			return nil, err
		}
		return f, nil
	default:
		return os.Stdout, nil
	}
}

// newHandler builds the primary slog.Handler for the chosen format and writer.
func newHandler(format Format, w io.Writer, level slog.Level, addSource bool) slog.Handler {
	opts := &slog.HandlerOptions{Level: level, AddSource: addSource}
	if format == FormatJSON {
		return slog.NewJSONHandler(w, opts)
	}
	return slog.NewTextHandler(w, opts)
}

// NewNope returns a logger that discards all records. Use as a safe default when logging
// is not configured, and in tests.
func NewNope() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}
