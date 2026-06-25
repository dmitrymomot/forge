// Command helloworld demonstrates serving a plain-HTTP (no TLS) "hello world"
// endpoint with the forge httpserver. The server runs under the supervisor so
// it drains in-flight requests and stops gracefully on SIGINT or SIGTERM.
package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/dmitrymomot/forge/httpserver"
	"github.com/dmitrymomot/forge/supervisor"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("Hello, World!\n"))
	})

	// No TLS option is set, so the server speaks plain HTTP. Secure timeouts
	// and limits come from httpserver.DefaultConfig().
	srv := httpserver.New(mux,
		httpserver.WithAddr(":8080"),
		httpserver.WithName("hello"),
		httpserver.WithLogger(log),
	)

	ctx, stop := supervisor.NewContext()
	defer stop()

	err := supervisor.Run(ctx,
		supervisor.WithLogger(log),
		supervisor.WithService(srv),
	)
	if err != nil {
		log.Error("supervisor stopped with error", slog.Any("err", err))
		os.Exit(1)
	}
}
