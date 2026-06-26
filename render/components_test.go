package render_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/render"
)

func TestComponents_Success(t *testing.T) {
	rec := httptest.NewRecorder()
	err := render.Components(context.Background(), rec, http.StatusCreated,
		&fakeComponent{out: "<p>a</p>"},
		&fakeComponent{out: "<p>b</p>"},
	)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, "text/html; charset=utf-8", rec.Header().Get("Content-Type"))
	assert.Equal(t, "<p>a</p><p>b</p>", rec.Body.String()) // order preserved, concatenated
}

func TestComponents_NoComponents(t *testing.T) {
	rec := httptest.NewRecorder()
	err := render.Components(context.Background(), rec, http.StatusOK)
	require.ErrorIs(t, err, render.ErrNoComponents)
	assert.Empty(t, rec.Body.String())
}

func TestComponents_NilComponentWritesNothing(t *testing.T) {
	rec := httptest.NewRecorder()
	err := render.Components(context.Background(), rec, http.StatusOK,
		&fakeComponent{out: "<p>a</p>"}, nil)
	require.ErrorIs(t, err, render.ErrNilComponent)
	assert.Empty(t, rec.Body.String())
}

func TestComponents_RenderErrorWritesNothing(t *testing.T) {
	rec := httptest.NewRecorder()
	err := render.Components(context.Background(), rec, http.StatusAccepted,
		&fakeComponent{out: "<p>a</p>"},
		&fakeComponent{err: errors.New("boom")},
	)
	require.Error(t, err)
	assert.Equal(t, http.StatusOK, rec.Code) // recorder default; nothing committed
	assert.Empty(t, rec.Body.String())
}

func TestComponents_PreservesPresetContentType(t *testing.T) {
	rec := httptest.NewRecorder()
	rec.Header().Set("Content-Type", "application/xml")
	err := render.Components(context.Background(), rec, http.StatusOK,
		&fakeComponent{out: "<x/>"})
	require.NoError(t, err)
	assert.Equal(t, "application/xml", rec.Header().Get("Content-Type"))
}
