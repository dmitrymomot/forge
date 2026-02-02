package main

import (
	"os"

	"github.com/dmitrymomot/forge"
	"github.com/dmitrymomot/forge/pkg/logger"
)

func main() {
	slog := logger.New().With("app", "multi-domain-example")

	landing := forge.New(
		forge.WithCustomLogger(slog.With("service", "landing")),
		forge.WithHandlers(&landingHandler{}),
	)

	api := forge.New(
		forge.WithCustomLogger(slog.With("service", "api")),
		forge.WithHandlers(&apiHandler{}),
	)

	tenant := forge.New(
		forge.WithCustomLogger(slog.With("service", "tenant")),
		forge.WithMiddleware(tenantMiddleware),
		forge.WithHandlers(&tenantHandler{}),
	)

	if err := forge.Run(
		forge.Domain("api.lvh.me", api),
		forge.Domain("*.lvh.me", tenant),
		forge.Fallback(landing),
		forge.Address(":8081"),
		forge.Logger(slog.With("service", "forge")),
	); err != nil {
		slog.Error("for example app running is failed", "err", err)
		os.Exit(1)
	}
}
