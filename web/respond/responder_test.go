package respond_test

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/web/problem"
	"github.com/dmitrymomot/forge/web/request"
	"github.com/dmitrymomot/forge/web/respond"
)

var errBoom = errors.New("boom")

// statusOf resolves this package's sentinels first, then falls back to the decode
// mapping — the wiring the Responder doc recommends.
func statusOf(err error) int {
	if code := respond.StatusOf(err); code != 0 {
		return code
	}
	return request.StatusCode(err)
}

func newResponder(t *testing.T, opts ...respond.ResponderOption) *respond.Responder {
	t.Helper()
	base := []respond.ResponderOption{
		respond.WithProblem(problem.JSON(problem.WithStatusOf(statusOf))),
	}
	return respond.New(append(base, opts...)...)
}

func call(t *testing.T, h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestWrapWritesTheResponse(t *testing.T) {
	rs := newResponder(t)
	h := rs.Wrap(func(*http.Request) (respond.Response, error) {
		return respond.Text("ok"), nil
	})

	rec := call(t, h, get())
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "ok", rec.Body.String())
}

func TestWrapSendsErrorsToTheProblemResponder(t *testing.T) {
	rs := newResponder(t)
	h := rs.Wrap(func(*http.Request) (respond.Response, error) {
		return nil, respond.ErrNotFound
	})

	rec := call(t, h, get())
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/problem+json")
}

// TestWrapNeverLeaksTheCauseOn5xx pins the rule that makes returning a bare error
// safe: the problem responder renders the status, never the message.
func TestWrapNeverLeaksTheCauseOn5xx(t *testing.T) {
	rs := respond.New(respond.WithProblem(problem.JSON(problem.WithStatus(http.StatusInternalServerError))))
	h := rs.Wrap(func(*http.Request) (respond.Response, error) {
		return nil, errors.New("connection string postgres://user:secret@host")
	})

	rec := call(t, h, get())
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.NotContains(t, rec.Body.String(), "secret")
}

func TestWrapRejectsANilResponse(t *testing.T) {
	rs := newResponder(t)
	h := rs.Wrap(func(*http.Request) (respond.Response, error) { return nil, nil })

	assert.Equal(t, http.StatusInternalServerError, call(t, h, get()).Code)
}

func TestNotFoundAndMethodNotAllowed(t *testing.T) {
	rs := newResponder(t)
	assert.Equal(t, http.StatusNotFound, call(t, rs.NotFound(), get()).Code)
	assert.Equal(t, http.StatusMethodNotAllowed, call(t, rs.MethodNotAllowed(), get()).Code)
}

// TestDialectIsAWiringDecision is the point of one Responder per router tree: the
// same handler answers problem+json on the API and HTML on the page tree.
func TestDialectIsAWiringDecision(t *testing.T) {
	handler := func(*http.Request) (respond.Response, error) { return nil, respond.ErrNotFound }

	api := respond.New(respond.WithProblem(problem.JSON(problem.WithStatusOf(statusOf))))
	pages := respond.New(respond.WithProblem(problem.Text(problem.WithStatusOf(statusOf))))

	apiRec := call(t, api.Wrap(handler), get())
	pageRec := call(t, pages.Wrap(handler), get())

	assert.Equal(t, http.StatusNotFound, apiRec.Code)
	assert.Equal(t, http.StatusNotFound, pageRec.Code)
	assert.Contains(t, apiRec.Header().Get("Content-Type"), "application/problem+json")
	assert.Contains(t, pageRec.Header().Get("Content-Type"), "text/plain")
}

func TestWithBeforeRunsAheadOfTheBody(t *testing.T) {
	rs := newResponder(t)
	h := rs.Wrap(func(*http.Request) (respond.Response, error) {
		return respond.SeeOther("/invoices", respond.WithBefore(func(w http.ResponseWriter) error {
			http.SetCookie(w, &http.Cookie{Name: "flash", Value: "sent"})
			return nil
		})), nil
	})

	rec := call(t, h, get())
	assert.Equal(t, http.StatusSeeOther, rec.Code)
	cookies := rec.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.Equal(t, "sent", cookies[0].Value)
}

func TestWithBeforeRunsInOrder(t *testing.T) {
	var order []string
	rs := newResponder(t)
	h := rs.Wrap(func(*http.Request) (respond.Response, error) {
		return respond.Text("ok",
			respond.WithBefore(func(http.ResponseWriter) error { order = append(order, "first"); return nil }),
			respond.WithBefore(func(http.ResponseWriter) error { order = append(order, "second"); return nil }),
		), nil
	})

	call(t, h, get())
	assert.Equal(t, []string{"first", "second"}, order)
}

// TestFailingBeforeFailsTheWholeResponse pins the choice that keeps a flash and its
// redirect in step: a lost side effect is an error, not a silently plain redirect.
func TestFailingBeforeFailsTheWholeResponse(t *testing.T) {
	rs := respond.New(respond.WithProblem(problem.JSON(problem.WithStatus(http.StatusInternalServerError))))
	ran := false
	h := rs.Wrap(func(*http.Request) (respond.Response, error) {
		return respond.Text("body",
			respond.WithBefore(func(http.ResponseWriter) error { return errBoom }),
			respond.WithBefore(func(http.ResponseWriter) error { ran = true; return nil }),
		), nil
	})

	rec := call(t, h, get())
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.NotContains(t, rec.Body.String(), "body")
	assert.False(t, ran, "a hook after the failing one must not run")
}

func TestWrapLogsAFailureAfterTheStatusIsSent(t *testing.T) {
	var buf bytes.Buffer
	rs := newResponder(t, respond.WithLogger(slog.New(slog.NewTextHandler(&buf, nil))))
	h := rs.Wrap(func(*http.Request) (respond.Response, error) {
		return respond.Raw(func(w http.ResponseWriter, _ *http.Request) error {
			w.WriteHeader(http.StatusOK)
			return errBoom
		}), nil
	})

	rec := call(t, h, get())
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, buf.String(), "response failed after it started")
	assert.Contains(t, buf.String(), "boom")
}

func TestWrapWithoutLoggerStaysSilent(t *testing.T) {
	rs := respond.New()
	h := rs.Wrap(func(*http.Request) (respond.Response, error) {
		return respond.Raw(func(w http.ResponseWriter, _ *http.Request) error {
			w.WriteHeader(http.StatusOK)
			return errBoom
		}), nil
	})

	assert.NotPanics(t, func() { call(t, h, get()) })
}

func TestFailIsUsableFromMiddleware(t *testing.T) {
	rs := newResponder(t)
	guard := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rs.Fail(w, r, respond.ErrNotFound)
		})
	}

	rec := call(t, guard(rs.Wrap(func(*http.Request) (respond.Response, error) {
		return respond.Text("never"), nil
	})), get())

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.NotContains(t, rec.Body.String(), "never")
}

func TestDefaultResponderAnswersProblemJSON(t *testing.T) {
	rs := respond.New()
	rec := call(t, rs.NotFound(), get())
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/problem+json")
}

func TestNilOptionsAreIgnored(t *testing.T) {
	rs := respond.New(respond.WithProblem(nil), respond.WithLogger(nil))
	assert.NotPanics(t, func() { call(t, rs.NotFound(), get()) })
}
