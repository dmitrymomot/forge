package logger

import (
	"io"
	"log/slog"
)

// NewNope creates a no-op logger that discards all output.
// Use this as a default when logging is not configured.
func NewNope() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
