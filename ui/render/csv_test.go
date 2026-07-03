package render_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/ui/render"
)

func TestCSV_Success(t *testing.T) {
	rec := httptest.NewRecorder()
	err := render.CSV(rec, http.StatusOK, "", [][]string{{"a", "b"}, {"1", "2"}})
	require.NoError(t, err)
	assert.Equal(t, "text/csv; charset=utf-8", rec.Header().Get("Content-Type"))
	assert.Equal(t, "a,b\n1,2\n", rec.Body.String())
	assert.Empty(t, rec.Header().Get("Content-Disposition"))
}

func TestCSV_WithFilenameSetsDisposition(t *testing.T) {
	rec := httptest.NewRecorder()
	err := render.CSV(rec, http.StatusOK, "users.csv", [][]string{{"x"}})
	require.NoError(t, err)
	assert.Equal(t,
		`attachment; filename="users.csv"; filename*=UTF-8''users.csv`,
		rec.Header().Get("Content-Disposition"))
}

func TestCSV_EmptyRecords(t *testing.T) {
	rec := httptest.NewRecorder()
	err := render.CSV(rec, http.StatusOK, "", nil)
	require.NoError(t, err)
	assert.Empty(t, rec.Body.String())
}
