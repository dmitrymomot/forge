package request_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/dmitrymomot/forge/request"
	"github.com/stretchr/testify/assert"
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
