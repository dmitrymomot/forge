package problem_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/errorsx"
	"github.com/dmitrymomot/forge/problem"
	"github.com/dmitrymomot/forge/validate"
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
