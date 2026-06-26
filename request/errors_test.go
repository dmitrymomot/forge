package request_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/request"
)

func TestStatusCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want int
	}{
		{"nil is 200", nil, http.StatusOK},
		{"malformed is 400", &request.Error{Source: request.SourceQuery, Key: "p", Kind: request.KindMalformed}, http.StatusBadRequest},
		{"too large is 413", &request.Error{Source: request.SourceBody, Kind: request.KindTooLarge}, http.StatusRequestEntityTooLarge},
		{"unsupported media is 415", &request.Error{Source: request.SourceBody, Kind: request.KindUnsupportedMediaType}, http.StatusUnsupportedMediaType},
		{"invalid body is 400", &request.Error{Source: request.SourceBody, Kind: request.KindInvalidBody}, http.StatusBadRequest},
		{"missing is 400", &request.Error{Source: request.SourceForm, Key: "f", Kind: request.KindMissing}, http.StatusBadRequest},
		{"plain error is 400", errors.New("other"), http.StatusBadRequest},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, request.StatusCode(tc.err))
		})
	}
}

func TestErrorStringAndUnwrap(t *testing.T) {
	t.Parallel()

	cause := errors.New("boom")
	e := &request.Error{Source: request.SourceQuery, Key: "page", Kind: request.KindMalformed, Err: cause}

	assert.ErrorIs(t, e, cause)
	assert.Contains(t, e.Error(), "request:")
	assert.Contains(t, e.Error(), "query")
	assert.Contains(t, e.Error(), "page")
	assert.Contains(t, e.Error(), "malformed")
}

func TestErrorStringAllKinds(t *testing.T) {
	t.Parallel()
	cases := []struct {
		kind request.Kind
		want string
	}{
		{request.KindMalformed, "malformed"},
		{request.KindTooLarge, "too large"},
		{request.KindUnsupportedMediaType, "unsupported media type"},
		{request.KindInvalidBody, "invalid body"},
		{request.KindMissing, "missing"},
	}
	for _, tc := range cases {
		e := &request.Error{Source: request.SourceBody, Kind: tc.kind, Err: errors.New("x")}
		assert.Contains(t, e.Error(), tc.want)
	}
}

func TestErrorStringNoCause(t *testing.T) {
	t.Parallel()
	e := &request.Error{Source: request.SourcePath, Key: "id", Kind: request.KindMalformed}
	assert.Equal(t, `request: path "id": malformed`, e.Error())
}
