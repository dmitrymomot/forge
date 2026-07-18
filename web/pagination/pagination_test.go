package pagination_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/web/pagination"
	"github.com/dmitrymomot/forge/web/request"
)

func TestParse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		target string
		opts   []pagination.Option
		want   pagination.Params
	}{
		{
			name: "defaults",
			want: pagination.Params{Limit: 20},
		},
		{
			name:   "values",
			target: "/?page=3&per_page=15",
			want:   pagination.Params{Limit: 15, Offset: 30},
		},
		{
			name:   "numeric values below minimum clamp",
			target: "/?page=-4&per_page=0",
			want:   pagination.Params{Limit: 1},
		},
		{
			name:   "limit clamps at maximum",
			target: "/?page=2&per_page=999",
			want:   pagination.Params{Limit: 100, Offset: 100},
		},
		{
			name:   "empty values use defaults",
			target: "/?page=&per_page=",
			want:   pagination.Params{Limit: 20},
		},
		{
			name:   "first repeated value wins",
			target: "/?page=2&page=4&per_page=10&per_page=50",
			want:   pagination.Params{Limit: 10, Offset: 10},
		},
		{
			name:   "custom parameter names",
			target: "/?p=4&size=12",
			opts:   []pagination.Option{pagination.WithPageParams("p", "size")},
			want:   pagination.Params{Limit: 12, Offset: 36},
		},
		{
			name: "default normalizes to configured maximum",
			opts: []pagination.Option{
				pagination.WithDefaultLimit(75),
				pagination.WithMaxLimit(25),
			},
			want: pagination.Params{Limit: 25},
		},
		{
			name: "invalid options keep defaults",
			opts: []pagination.Option{
				pagination.WithPageParams("", ""),
				pagination.WithDefaultLimit(0),
				pagination.WithMaxLimit(0),
			},
			want: pagination.Params{Limit: 20},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			target := tt.target
			if target == "" {
				target = "/"
			}
			got, err := pagination.Parse(httptest.NewRequest(http.MethodGet, target, nil), tt.opts...)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseMalformed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		target string
		key    string
	}{
		{name: "page", target: "/?page=wrong", key: "page"},
		{name: "per page", target: "/?per_page=wrong", key: "per_page"},
		{name: "page exceeds int32", target: "/?page=2147483648", key: "page"},
		{name: "per page exceeds int32", target: "/?per_page=2147483648", key: "per_page"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := pagination.Parse(httptest.NewRequest(http.MethodGet, tt.target, nil))
			require.Error(t, err)

			var requestErr *request.Error
			require.ErrorAs(t, err, &requestErr)
			assert.Equal(t, request.SourceQuery, requestErr.Source)
			assert.Equal(t, tt.key, requestErr.Key)
			assert.Equal(t, request.KindMalformed, requestErr.Kind)
			assert.Equal(t, http.StatusBadRequest, request.StatusCode(err))
		})
	}
}

func TestParseOffsetOverflow(t *testing.T) {
	t.Parallel()

	_, err := pagination.Parse(httptest.NewRequest(http.MethodGet, "/?page=2147483647&per_page=100", nil))
	require.Error(t, err)
	assert.ErrorIs(t, err, pagination.ErrOffsetOverflow)
	assert.Equal(t, http.StatusBadRequest, request.StatusCode(err))

	var requestErr *request.Error
	require.True(t, errors.As(err, &requestErr))
	assert.Equal(t, "page", requestErr.Key)
}

func TestParseCursor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		target string
		opts   []pagination.Option
		want   pagination.CursorParams
	}{
		{
			name: "defaults",
			want: pagination.CursorParams{Limit: 20},
		},
		{
			name:   "values",
			target: "/?cursor=next%2Fpage&limit=15",
			want:   pagination.CursorParams{Cursor: "next/page", Limit: 15},
		},
		{
			name:   "numeric limit below minimum clamps",
			target: "/?cursor=next&limit=0",
			want:   pagination.CursorParams{Cursor: "next", Limit: 1},
		},
		{
			name:   "limit clamps at maximum",
			target: "/?cursor=next&limit=999",
			want:   pagination.CursorParams{Cursor: "next", Limit: 100},
		},
		{
			name:   "first repeated value wins",
			target: "/?cursor=first&cursor=second&limit=10&limit=50",
			want:   pagination.CursorParams{Cursor: "first", Limit: 10},
		},
		{
			name:   "custom parameter names",
			target: "/?after=next&n=12",
			opts:   []pagination.Option{pagination.WithCursorParams("after", "n")},
			want:   pagination.CursorParams{Cursor: "next", Limit: 12},
		},
		{
			name: "default normalizes to configured maximum",
			opts: []pagination.Option{
				pagination.WithDefaultLimit(75),
				pagination.WithMaxLimit(25),
			},
			want: pagination.CursorParams{Limit: 25},
		},
		{
			name: "empty parameter names keep defaults",
			opts: []pagination.Option{
				pagination.WithCursorParams("", ""),
			},
			want: pagination.CursorParams{Limit: 20},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			target := tt.target
			if target == "" {
				target = "/"
			}
			got, err := pagination.ParseCursor(httptest.NewRequest(http.MethodGet, target, nil), tt.opts...)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseCursorMalformedLimit(t *testing.T) {
	t.Parallel()

	_, err := pagination.ParseCursor(httptest.NewRequest(http.MethodGet, "/?limit=wrong", nil))
	require.Error(t, err)
	assert.Equal(t, http.StatusBadRequest, request.StatusCode(err))

	var requestErr *request.Error
	require.ErrorAs(t, err, &requestErr)
	if requestErr == nil {
		t.Fatal("expected request error")
	}
	assert.Equal(t, request.SourceQuery, requestErr.Source)
	assert.Equal(t, "limit", requestErr.Key)
	assert.Equal(t, request.KindMalformed, requestErr.Kind)
}
