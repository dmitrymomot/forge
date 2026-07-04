package subroute_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/dmitrymomot/forge/web/request"
	"github.com/dmitrymomot/forge/web/subroute"
)

func ExampleMount() {
	adminMux := http.NewServeMux()
	adminMux.HandleFunc("GET /users/{id}", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, "user %s at %s", r.PathValue("id"), r.URL.Path)
	})

	mux := http.NewServeMux()
	subroute.Mount(mux, "/admin", adminMux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/users/42", nil))
	fmt.Println(rec.Body.String())
	// Output: user 42 at /users/42
}

func ExampleMount_wildcardPrefix() {
	dashboardMux := http.NewServeMux()
	dashboardMux.HandleFunc("GET /reports/{id}", func(w http.ResponseWriter, r *http.Request) {
		tenant, _ := request.Path[string](r, "tenant")
		_, _ = fmt.Fprintf(w, "tenant %s, report %s", tenant, r.PathValue("id"))
	})

	mux := http.NewServeMux()
	subroute.Mount(mux, "/app/{tenant}/dashboard", dashboardMux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/app/acme/dashboard/reports/7", nil))
	fmt.Println(rec.Body.String())
	// Output: tenant acme, report 7
}
