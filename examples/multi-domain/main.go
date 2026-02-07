package main

import (
	"log"
	"os"

	"github.com/dmitrymomot/forge"
	"github.com/dmitrymomot/forge/pkg/logger"
)

type config struct {
	Server forge.RunConfig `envPrefix:"SERVER_"`
}

func main() {
	var cfg config
	if err := forge.LoadConfig(&cfg); err != nil {
		log.Fatal(err)
	}

	slog := logger.New().With("app", "multi-domain-example")

	landing := forge.New(
		forge.AppConfig{},
		forge.WithCustomLogger(slog.With("service", "landing")),
		forge.WithHandlers(&landingHandler{}),
	)

	api := forge.New(
		forge.AppConfig{},
		forge.WithCustomLogger(slog.With("service", "api")),
		forge.WithHandlers(&apiHandler{}),
	)

	tenant := forge.New(
		forge.AppConfig{},
		forge.WithCustomLogger(slog.With("service", "tenant")),
		forge.WithMiddleware(tenantMiddleware),
		forge.WithHandlers(&tenantHandler{}),
	)

	if err := forge.Run(
		cfg.Server,
		forge.WithDomain("api.lvh.me", api),
		forge.WithDomain("*.lvh.me", tenant),
		forge.WithFallback(landing),
		forge.WithRunLogger(slog.With("service", "forge")),
	); err != nil {
		slog.Error("for example app running is failed", "err", err)
		os.Exit(1)
	}
}
