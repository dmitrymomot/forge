package compress_test

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/dmitrymomot/forge/web/compress"
	"github.com/dmitrymomot/forge/web/middleware"
)

func ExampleNew() {
	mw, err := compress.New()
	if err != nil {
		panic(err)
	}

	body := strings.Repeat("forge compresses text. ", 200)
	mux := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, body)
	})
	h := middleware.Wrap(mux, mw)

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	zr, err := gzip.NewReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		panic(err)
	}
	out, err := io.ReadAll(zr)
	if err != nil {
		panic(err)
	}

	fmt.Println("Content-Encoding:", rec.Header().Get("Content-Encoding"))
	fmt.Println("round-trip matches:", string(out) == body)
	// Output:
	// Content-Encoding: gzip
	// round-trip matches: true
}
