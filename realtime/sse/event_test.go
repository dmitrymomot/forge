package sse_test

import (
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/realtime/sse"
)

// frame renders one event through a Writer and returns its wire bytes.
func frame(t *testing.T, e sse.Event) (string, error) {
	t.Helper()
	rec := httptest.NewRecorder()
	w, err := sse.NewWriter(rec)
	require.NoError(t, err)
	before := rec.Body.Len()
	if err := w.Send(e); err != nil {
		return "", err
	}
	return rec.Body.String()[before:], nil
}

func TestEventFraming(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		event sse.Event
		want  string
	}{
		"text":            {sse.Text("update", "hi"), "event: update\ndata: hi\n\n"},
		"unnamed text":    {sse.Text("", "hi"), "data: hi\n\n"},
		"multiline LF":    {sse.Text("", "a\nb"), "data: a\ndata: b\n\n"},
		"multiline CRLF":  {sse.Text("", "a\r\nb"), "data: a\ndata: b\n\n"},
		"multiline CR":    {sse.Text("", "a\rb"), "data: a\ndata: b\n\n"},
		"trailing LF":     {sse.Text("", "a\n"), "data: a\ndata: \n\n"},
		"empty data":      {sse.Text("", ""), "data: \n\n"},
		"nil data":        {sse.Event{ID: "7"}, "id: 7\n\n"},
		"zero event":      {sse.Event{}, "\n"},
		"comment":         {sse.Comment("ping"), ": ping\n\n"},
		"empty comment":   {sse.Comment(""), ": \n\n"},
		"comment lines":   {sse.Comment("a\nb"), ": a\n: b\n\n"},
		"retry":           {sse.Retry(3 * time.Second), "retry: 3000\n\n"},
		"retry rounds up": {sse.Retry(1500 * time.Microsecond), "retry: 2\n\n"},
		"retry sub-ms":    {sse.Retry(time.Microsecond), "retry: 1\n\n"},
		"all fields": {
			sse.Event{ID: "9", Name: "tick", Data: []byte("x"), Retry: time.Second},
			"id: 9\nevent: tick\nretry: 1000\ndata: x\n\n",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := frame(t, tc.event)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestEventValidation(t *testing.T) {
	t.Parallel()

	cases := map[string]sse.Event{
		"name newline": {Name: "a\nb"},
		"name CR":      {Name: "a\rb"},
		"id newline":   {ID: "1\n2"},
		"id NUL":       {ID: "1\x002"},
		"neg retry":    {Retry: -time.Second},
	}
	for name, e := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := frame(t, e)
			require.ErrorIs(t, err, sse.ErrInvalidEvent)
			assert.Empty(t, got, "nothing must be written on validation failure")
		})
	}
}

// componentFunc adapts a render func to sse.Component, the same way an
// html/template consumer would.
type componentFunc func(ctx context.Context, w io.Writer) error

func (f componentFunc) Render(ctx context.Context, w io.Writer) error { return f(ctx, w) }

func TestTempl(t *testing.T) {
	t.Parallel()

	html := "<tr>\n  <td>paid</td>\n</tr>"
	e, err := sse.Templ(t.Context(), "orders.42", componentFunc(func(_ context.Context, w io.Writer) error {
		_, err := io.WriteString(w, html)
		return err
	}))
	require.NoError(t, err)
	got, err := frame(t, e)
	require.NoError(t, err)
	assert.Equal(t, "event: orders.42\ndata: <tr>\ndata:   <td>paid</td>\ndata: </tr>\n\n", got)

	// What a spec-compliant EventSource reassembles (data lines joined with
	// \n) must be the exact HTML htmx swaps in.
	var lines []string
	for line := range strings.Lines(got) {
		if rest, ok := strings.CutPrefix(line, "data: "); ok {
			lines = append(lines, strings.TrimSuffix(rest, "\n"))
		}
	}
	assert.Equal(t, html, strings.Join(lines, "\n"))
}

func TestTemplErrors(t *testing.T) {
	t.Parallel()

	_, err := sse.Templ(t.Context(), "x", nil)
	require.Error(t, err)

	_, err = sse.Templ(t.Context(), "x", componentFunc(func(context.Context, io.Writer) error {
		return errors.New("render failed")
	}))
	require.Error(t, err)
}

func TestJSON(t *testing.T) {
	t.Parallel()

	e, err := sse.JSON("update", map[string]int{"unread": 3})
	require.NoError(t, err)
	got, err := frame(t, e)
	require.NoError(t, err)
	assert.Equal(t, "event: update\ndata: {\"unread\":3}\n\n", got)

	_, err = sse.JSON("update", make(chan int))
	require.Error(t, err)
}
