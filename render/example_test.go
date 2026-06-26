package render_test

import (
	"fmt"
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
