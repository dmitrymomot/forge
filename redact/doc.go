// Package redact keeps secrets out of logs, error strings, and JSON. The Secret[T]
// wrapper renders as "REDACTED" through fmt, encoding/json, and log/slog, and reveals
// its value only via Expose, so logging a whole config or request cannot leak a wrapped
// field. The free functions String and Map scrub data you do not control.
//
//	type Config struct {
//		StripeKey redact.Secret[string]
//	}
//	slog.Info("config", "cfg", cfg)            // StripeKey logs as REDACTED
//	client := stripe.New(cfg.StripeKey.Expose())
//
//	safe := redact.Map(payload, "password", "token")
//	slog.Info("webhook", "body", safe)
//
// redact does not fetch from a vault; it is purely a wrapper plus scrub helpers.
package redact
