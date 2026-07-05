package circuitbreaker_test

import (
	"net/http"

	"github.com/dmitrymomot/forge/resilience/circuitbreaker"
)

func ExampleGroupKey() {
	g := circuitbreaker.NewGroup()
	mux := http.NewServeMux()
	checkout := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {})
	mux.Handle("/checkout", circuitbreaker.GroupKey(g, "checkout")(checkout))
	// Output:
}
