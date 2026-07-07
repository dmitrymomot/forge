package logredact_test

import (
	"log/slog"
	"os"

	"github.com/dmitrymomot/forge/ops/logredact"
)

func Example() {
	opts := &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey && len(groups) == 0 {
				return slog.Attr{}
			}
			return a
		},
	}
	h := logredact.New(slog.NewJSONHandler(os.Stdout, opts), logredact.WithKeys("password"))
	log := slog.New(h)

	log.Info("login", "password", "hunter2")
	log.WithGroup("auth").Info("login", "password", "hunter2")

	// Output:
	// {"level":"INFO","msg":"login","password":"[REDACTED]"}
	// {"level":"INFO","msg":"login","auth":{"password":"[REDACTED]"}}
}
