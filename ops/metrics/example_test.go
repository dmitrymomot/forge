package metrics_test

import (
	"expvar"
	"fmt"
	"net/http"

	"github.com/dmitrymomot/forge/ops/metrics"
	"github.com/dmitrymomot/forge/web/middleware"
)

func Example() {
	// The default recorder aggregates in-process and publishes one expvar
	// var, so everything below shows up on /debug/vars with zero deps.
	rec := metrics.New(metrics.WithName("example_metrics"))

	signups := rec.Counter("signups_total", "Completed signups.", "plan")
	signups.Inc("pro")
	signups.Inc("pro")
	signups.Inc("free")

	depth := rec.Gauge("queue_depth", "Jobs waiting.")
	depth.Set(17)

	root, _ := expvar.Get("example_metrics").(*expvar.Map)
	fmt.Println(root.Get("signups_total"))
	fmt.Println(root.Get("queue_depth"))
	// Output:
	// {"plan=\"free\"":1,"plan=\"pro\"":2}
	// 17
}

// ExampleNewMiddleware wires request instrumentation around a mux; the path
// label is the matched route pattern, so cardinality stays bounded.
func ExampleNewMiddleware() {
	rec := metrics.New(metrics.WithName("example_middleware_metrics"))

	mux := http.NewServeMux()
	mux.HandleFunc("GET /users/{id}", func(w http.ResponseWriter, r *http.Request) {})

	handler := middleware.Wrap(mux,
		metrics.NewMiddleware(rec, metrics.WithSkip(func(r *http.Request) bool {
			return r.URL.Path == "/livez"
		})),
	)
	_ = handler // pass to httpserver
	// Output:
}
