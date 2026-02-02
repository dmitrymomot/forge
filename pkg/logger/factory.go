package logger

import (
	"log/slog"
	"os"
)

// New creates a JSON-formatted logger with optional context extractors.
func New(extractors ...ContextExtractor) *slog.Logger {
	log := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	return slog.New(NewLogHandlerDecorator(log, extractors...))
}
