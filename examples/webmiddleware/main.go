// Command webmiddleware demonstrates the full web-transport middleware chain:
// recoverer -> requestid -> clientip -> requestlog, with a problem responder and a
// logger wired to the request_id and client_ip extractors.
package main

import (
	"context"
	"errors"
	"html/template"
	"net/http"
	"os"
	"os/signal"

	"github.com/dmitrymomot/forge/core/errorsx"
	"github.com/dmitrymomot/forge/ops/logger"
	"github.com/dmitrymomot/forge/web/clientip"
	"github.com/dmitrymomot/forge/web/httpserver"
	"github.com/dmitrymomot/forge/web/middleware"
	"github.com/dmitrymomot/forge/web/problem"
	"github.com/dmitrymomot/forge/web/recoverer"
	"github.com/dmitrymomot/forge/web/render"
	"github.com/dmitrymomot/forge/web/requestid"
	"github.com/dmitrymomot/forge/web/requestlog"
)

// errPage is the browser-facing error template (markup lives here, not in forge).
var errPage = template.Must(template.New("err").Parse(
	`<!doctype html><title>{{.Status}}</title><h1>{{.Status}} {{.Title}}</h1>`))

func main() {
	log, err := logger.New(logger.WithContextExtractors(requestid.LogExtractor, clientip.LogExtractor))
	if err != nil {
		panic(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /ok", func(w http.ResponseWriter, r *http.Request) {
		_ = render.JSON(w, http.StatusOK, map[string]string{"ip": clientip.Get(r)})
	})
	mux.HandleFunc("GET /fail", func(w http.ResponseWriter, r *http.Request) {
		problem.JSON(problem.WithLogger(log))(w, r, errorsx.New("teapot", "I refuse"))
	})
	mux.HandleFunc("GET /panic", func(http.ResponseWriter, *http.Request) {
		panic(errors.New("unexpected"))
	})

	h := middleware.Wrap(mux,
		recoverer.New(
			recoverer.WithLogger(log),
			recoverer.WithResponder(problem.Negotiate(
				problem.JSON(problem.WithStatus(http.StatusInternalServerError)),
				map[string]problem.Responder{
					"text/html": problem.HTML(errPage, "", problem.WithStatus(http.StatusInternalServerError)),
				})),
		),
		requestid.New(),
		clientip.Middleware(clientip.TrustPrivateProxies()),
		requestlog.New(log, requestlog.WithSkip(func(r *http.Request) bool { return r.URL.Path == "/healthz" })),
	)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	srv := httpserver.New(h, httpserver.WithAddr(":8080"), httpserver.WithLogger(log))
	if err := srv.Run(ctx); err != nil {
		log.Error("server exited", "error", err)
		os.Exit(1)
	}
}
