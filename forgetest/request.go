package forgetest

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/dmitrymomot/forge"
	"github.com/dmitrymomot/forge/pkg/id"
)

// Request builds an HTTP request for testing forge handlers.
type Request struct {
	t   testing.TB
	app *App

	headers     http.Header
	formValues  url.Values
	sessionData map[string]any
	method      string
	path        string

	userID   string
	cookies  []*http.Cookie
	jsonBody []byte
	hasUser  bool
}

// Get creates a GET request builder.
func Get(t testing.TB, app *App, path string) *Request {
	t.Helper()
	return newRequest(t, app, http.MethodGet, path)
}

// Post creates a POST request builder.
func Post(t testing.TB, app *App, path string) *Request {
	t.Helper()
	return newRequest(t, app, http.MethodPost, path)
}

// Put creates a PUT request builder.
func Put(t testing.TB, app *App, path string) *Request {
	t.Helper()
	return newRequest(t, app, http.MethodPut, path)
}

// Delete creates a DELETE request builder.
func Delete(t testing.TB, app *App, path string) *Request {
	t.Helper()
	return newRequest(t, app, http.MethodDelete, path)
}

// Patch creates a PATCH request builder.
func Patch(t testing.TB, app *App, path string) *Request {
	t.Helper()
	return newRequest(t, app, http.MethodPatch, path)
}

func newRequest(t testing.TB, app *App, method, path string) *Request {
	t.Helper()
	return &Request{
		t:           t,
		app:         app,
		method:      method,
		path:        path,
		headers:     make(http.Header),
		sessionData: make(map[string]any),
	}
}

// AsUser creates a session for the given user ID.
// The session is created in the store and a cookie is attached to the request.
func (r *Request) AsUser(userID string) *Request {
	r.userID = userID
	r.hasUser = true
	return r
}

// WithRole stores the given role in session data.
// Only effective when used with AsUser (no session = no role).
func (r *Request) WithRole(role string) *Request {
	r.sessionData[roleDataKey] = role
	return r
}

// WithSessionData sets arbitrary session data.
func (r *Request) WithSessionData(key string, value any) *Request {
	r.sessionData[key] = value
	return r
}

// WithHTMX sets the HX-Request header to mark this as an HTMX request.
func (r *Request) WithHTMX() *Request {
	r.headers.Set("HX-Request", "true")
	return r
}

// WithForm adds a form key-value pair. Sets Content-Type to application/x-www-form-urlencoded.
func (r *Request) WithForm(key, value string) *Request {
	if r.formValues == nil {
		r.formValues = make(url.Values)
	}
	r.formValues.Add(key, value)
	return r
}

// WithJSON marshals the value as JSON body and sets Content-Type.
func (r *Request) WithJSON(v any) *Request {
	r.t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		r.t.Fatalf("forgetest: marshal JSON: %v", err)
	}
	r.jsonBody = data
	return r
}

// WithHeader sets a custom request header.
func (r *Request) WithHeader(key, value string) *Request {
	r.headers.Set(key, value)
	return r
}

// WithCookie adds a cookie to the request.
func (r *Request) WithCookie(name, value string) *Request {
	r.cookies = append(r.cookies, &http.Cookie{Name: name, Value: value})
	return r
}

// Do executes the request and returns the response.
func (r *Request) Do() *Response {
	r.t.Helper()

	// Build session if user is specified.
	if r.hasUser {
		r.buildSession()
	}

	// Build HTTP request.
	req := r.buildHTTPRequest()

	// Execute via the forge app's router.
	rec := httptest.NewRecorder()
	r.app.app.Router().ServeHTTP(rec, req)

	return &Response{rec: rec, t: r.t}
}

// buildSession creates a session in the store and attaches the cookie.
func (r *Request) buildSession() {
	r.t.Helper()

	// Generate token matching internal/session.go:276-282.
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		r.t.Fatalf("forgetest: generate token: %v", err)
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes)

	// Hash matching internal/session.go:302-305.
	h := sha256.Sum256([]byte(token))
	tokenHash := base64.URLEncoding.EncodeToString(h[:])

	now := time.Now()
	uid := r.userID

	sess := &forge.Session{
		ID:           id.NewULID(),
		TokenHash:    tokenHash,
		UserID:       &uid,
		Data:         make(map[string]any),
		CreatedAt:    now,
		LastActiveAt: now,
		ExpiresAt:    now.Add(30 * 24 * time.Hour),
	}

	// Copy session data.
	maps.Copy(sess.Data, r.sessionData)

	if err := r.app.store.Create(context.Background(), sess); err != nil {
		r.t.Fatalf("forgetest: create session: %v", err)
	}

	r.cookies = append(r.cookies, &http.Cookie{Name: "__sid", Value: token})
}

// buildHTTPRequest constructs the *http.Request from the builder state.
func (r *Request) buildHTTPRequest() *http.Request {
	r.t.Helper()

	var req *http.Request

	switch {
	case r.jsonBody != nil:
		req = httptest.NewRequest(r.method, r.path, bytes.NewReader(r.jsonBody))
		req.Header.Set("Content-Type", "application/json")
	case r.formValues != nil:
		body := r.formValues.Encode()
		req = httptest.NewRequest(r.method, r.path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	default:
		req = httptest.NewRequest(r.method, r.path, nil)
	}

	// Apply headers.
	for k, vals := range r.headers {
		for _, v := range vals {
			req.Header.Set(k, v)
		}
	}

	// Apply cookies.
	for _, c := range r.cookies {
		req.AddCookie(c)
	}

	return req
}
