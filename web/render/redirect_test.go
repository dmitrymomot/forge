package render_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/web/render"
)

func TestRedirect(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/old", nil)
	render.Redirect(rec, req, http.StatusSeeOther, "/new")
	assert.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, "/new", rec.Header().Get("Location"))
}
