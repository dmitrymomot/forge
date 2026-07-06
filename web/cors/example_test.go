package cors_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/dmitrymomot/forge/web/cors"
	"github.com/dmitrymomot/forge/web/middleware"
)

func ExampleNew() {
	mw, err := cors.New(cors.WithConfig(cors.Config{
		AllowedOrigins: []string{"https://app.example.com"},
	}))
	if err != nil {
		panic(err)
	}

	mux := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	h := middleware.Wrap(mux, mw)

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Origin", "https://app.example.com")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	fmt.Println("Access-Control-Allow-Origin:", rec.Header().Get("Access-Control-Allow-Origin"))
	// Output:
	// Access-Control-Allow-Origin: https://app.example.com
}
