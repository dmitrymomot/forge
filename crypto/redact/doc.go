// Package redact keeps secrets out of logs, error strings, and JSON. The Secret[T]
// wrapper renders as "REDACTED" through fmt, encoding/json, and log/slog, and reveals
// its value only via Expose, so logging a whole config or request cannot leak a wrapped
// field. The free functions String and Map scrub data you do not control.
//
// redact does not fetch from a vault; it is purely a wrapper plus scrub helpers.
//
// # Usage
//
//	type Config struct {
//		StripeKey redact.Secret[string]
//	}
//	cfg := Config{StripeKey: redact.New("sk_live_123")}
//	slog.Info("config", "cfg", cfg) // StripeKey logs as REDACTED
//	key := cfg.StripeKey.Expose()   // only way to read the real value
//
//	safe := redact.Map(payload, "password", "token")
//	slog.Info("webhook", "body", safe)
package redact
