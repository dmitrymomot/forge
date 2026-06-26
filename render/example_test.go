package render_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"

	"github.com/dmitrymomot/forge/render"
)

func ExampleJSON() {
	rec := httptest.NewRecorder()
	_ = render.JSON(rec, http.StatusOK, map[string]string{"status": "ok"})

	fmt.Println(rec.Code)
	fmt.Print(rec.Body.String())
	// Output:
	// 200
	// {"status":"ok"}
}

// greeting is a minimal render.Component. Any github.com/a-h/templ component
// satisfies this same interface, so render.Templ accepts templ output without
// the framework importing templ.
type greeting struct{ name string }

func (g greeting) Render(_ context.Context, w io.Writer) error {
	_, err := io.WriteString(w, "<p>Hello, "+g.name+"</p>")
	return err
}

func ExampleTempl() {
	rec := httptest.NewRecorder()
	_ = render.Templ(context.Background(), rec, http.StatusOK, greeting{name: "Ada"})

	fmt.Println(rec.Code)
	fmt.Print(rec.Body.String())
	// Output:
	// 200
	// <p>Hello, Ada</p>
}
