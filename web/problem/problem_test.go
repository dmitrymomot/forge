package problem_test

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/errorsx"
	"github.com/dmitrymomot/forge/core/validate"
	"github.com/dmitrymomot/forge/web/problem"
)

func TestFromPlainErrorIs400(t *testing.T) {
	p := problem.From(errors.New("boom"))
	assert.Equal(t, http.StatusBadRequest, p.Status)
	assert.Equal(t, "Bad Request", p.Title)
	assert.Equal(t, "about:blank", p.Type)
	assert.Equal(t, "boom", p.Detail)
}

func TestFromNilIs200(t *testing.T) {
	assert.Equal(t, http.StatusOK, problem.From(nil).Status)
}

func TestFromCodedError(t *testing.T) {
	err := errorsx.New("user_not_found", "no such user")
	p := problem.From(err)
	assert.Equal(t, "user_not_found", p.Code)
}

func TestFromValidateErrorsPopulatesFields(t *testing.T) {
	verr := validate.Check(validate.Result{
		{Field: "email", Key: "required", Message: "is required"},
	})
	p := problem.From(verr)
	assert.Equal(t, "is required", p.Fields["email"])
}

func TestFromValidateErrorsJoinsMultipleViolations(t *testing.T) {
	verr := validate.Check(validate.Result{
		{Field: "password", Key: "min", Message: "too short"},
		{Field: "password", Key: "complexity", Message: "needs a digit"},
	})
	p := problem.From(verr)
	assert.Contains(t, p.Fields["password"], "too short")
	assert.Contains(t, p.Fields["password"], "needs a digit")
	assert.Contains(t, p.Fields["password"], "; ")
}

func TestForceStatusAndTypeBaseURI(t *testing.T) {
	err := errorsx.New("rate_limited", "slow down")
	p := problem.From(err,
		problem.WithStatus(http.StatusTooManyRequests),
		problem.WithTypeBaseURI("https://errors.example/"),
	)
	assert.Equal(t, http.StatusTooManyRequests, p.Status)
	assert.Equal(t, "https://errors.example/rate_limited", p.Type)
}

func TestFrom5xxHasNoDetail(t *testing.T) {
	p := problem.From(errors.New("db exploded"), problem.WithStatus(http.StatusInternalServerError))
	assert.Empty(t, p.Detail) // never leak internals on 5xx
}

func TestProblem_ErrorString(t *testing.T) {
	p := &problem.Problem{Status: 429, Title: "Too Many Requests", Code: "rate_limited"}
	assert.Equal(t, "problem: 429 Too Many Requests [rate_limited]", p.Error())
	p2 := &problem.Problem{Status: 400, Title: "Bad Request"}
	assert.Equal(t, "problem: 400 Bad Request", p2.Error())
}

func TestProblem_Is(t *testing.T) {
	p := &problem.Problem{Status: 429, Code: "rate_limited"}
	assert.True(t, errors.Is(p, &problem.Problem{Code: "rate_limited"}))
	assert.True(t, errors.Is(p, &problem.Problem{Status: 429}))
	assert.True(t, errors.Is(p, &problem.Problem{}))
	assert.False(t, errors.Is(p, &problem.Problem{Status: 400}))
	assert.False(t, errors.Is(p, &problem.Problem{Code: "other"}))
	assert.False(t, errors.Is(p, errors.New("nope")))
}

func TestDecode_ProblemJSON(t *testing.T) {
	body := `{"type":"about:blank","title":"Too Many Requests","status":429,"code":"rate_limited"}`
	resp := &http.Response{
		StatusCode: 429,
		Header:     http.Header{"Content-Type": []string{"application/problem+json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	p, err := problem.Decode(resp)
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Equal(t, 429, p.Status)
	assert.Equal(t, "rate_limited", p.Code)
	assert.Equal(t, "Too Many Requests", p.Title)
}

func TestDecode_FillsStatusFromResponse(t *testing.T) {
	resp := &http.Response{
		StatusCode: 503,
		Header:     http.Header{"Content-Type": []string{"application/problem+json"}},
		Body:       io.NopCloser(strings.NewReader(`{"title":"Service Unavailable"}`)),
	}
	p, err := problem.Decode(resp)
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Equal(t, 503, p.Status)
}

func TestDecode_NotAProblem(t *testing.T) {
	resp := &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": []string{"text/html"}},
		Body:       io.NopCloser(strings.NewReader("<html></html>")),
	}
	_, err := problem.Decode(resp)
	assert.ErrorIs(t, err, problem.ErrNotProblem)
}

func TestDecode_LeavesBodyOpen(t *testing.T) {
	rc := &trackCloser{Reader: strings.NewReader(`{"status":400,"code":"x"}`)}
	resp := &http.Response{
		StatusCode: 400,
		Header:     http.Header{"Content-Type": []string{"application/problem+json"}},
		Body:       rc,
	}
	_, err := problem.Decode(resp)
	require.NoError(t, err)
	assert.False(t, rc.closed, "Decode must not close the response body")
}

// trackCloser records whether Close was called.
type trackCloser struct {
	io.Reader
	closed bool
}

func (t *trackCloser) Close() error { t.closed = true; return nil }
