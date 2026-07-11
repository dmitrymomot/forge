package assets_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing/fstest"

	"github.com/dmitrymomot/forge/web/assets"
)

func ExampleNew() {
	// In a real app: //go:embed static; embed.FS. Here, an in-memory fs.FS.
	fsys := fstest.MapFS{"app.css": {Data: []byte("body{color:red}")}}

	a, err := assets.New(fsys, assets.WithSPA("index.html"))
	if err != nil {
		panic(err)
	}

	mux := http.NewServeMux()
	mux.Handle(a.Prefix(), a) // serve the fingerprinted tree at /static/

	// Templates resolve logical names to fingerprinted URLs:
	req := httptest.NewRequest(http.MethodGet, a.URL("app.css"), nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	fmt.Println("Cache-Control:", rec.Header().Get("Cache-Control"))
	fmt.Println("served:", rec.Code == http.StatusOK)
	// Output:
	// Cache-Control: public, max-age=31536000, immutable
	// served: true
}
