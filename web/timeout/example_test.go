package timeout_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/dmitrymomot/forge/web/middleware"
	"github.com/dmitrymomot/forge/web/timeout"
)

// ExampleNew wraps a fast handler that completes well within the deadline,
// so the middleware never intervenes and the handler's own response is
// returned unchanged.
func ExampleNew() {
	mw, err := timeout.New(timeout.WithConfig(timeout.Config{Timeout: time.Second}))
	if err != nil {
		panic(err)
	}

	mux := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	})
	h := middleware.Wrap(mux, mw)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	fmt.Println(rec.Code, rec.Body.String())
	// Output:
	// 200 ok
}
