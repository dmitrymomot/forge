package render_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/render"
)

type testCtxKey struct{}

// fakeComponent satisfies render.Component (and mirrors templ.Component's method).
type fakeComponent struct {
	out      string
	err      error
	ctxValue any
}

func (f *fakeComponent) Render(ctx context.Context, w io.Writer) error {
	f.ctxValue = ctx.Value(testCtxKey{})
	if f.err != nil {
		return f.err
	}
	_, err := io.WriteString(w, f.out)
	return err
}

func TestTempl_Success(t *testing.T) {
	rec := httptest.NewRecorder()
	c := &fakeComponent{out: "<p>hi</p>"}
	ctx := context.WithValue(context.Background(), testCtxKey{}, "v")
	err := render.Templ(ctx, rec, http.StatusOK, c)
	require.NoError(t, err)
	assert.Equal(t, "text/html; charset=utf-8", rec.Header().Get("Content-Type"))
	assert.Equal(t, "<p>hi</p>", rec.Body.String())
	assert.Equal(t, "v", c.ctxValue) // ctx propagated to the component
}

func TestTempl_NilComponent(t *testing.T) {
	rec := httptest.NewRecorder()
	err := render.Templ(context.Background(), rec, http.StatusOK, nil)
	require.ErrorIs(t, err, render.ErrNilComponent)
	assert.Empty(t, rec.Body.String())
}

func TestTempl_RenderErrorWritesNothing(t *testing.T) {
	rec := httptest.NewRecorder()
	c := &fakeComponent{err: errors.New("boom")}
	err := render.Templ(context.Background(), rec, http.StatusAccepted, c)
	require.Error(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, rec.Body.String())
}
