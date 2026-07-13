package guard_test

import (
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/auth/guard"
)

func basicGet(t *testing.T, h http.Handler, setAuth func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if setAuth != nil {
		setAuth(r)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestBasicAuth_Success(t *testing.T) {
	t.Parallel()
	h := guard.BasicAuth(map[string]string{"ops": "s3cret"})(echoHandler(t))
	w := basicGet(t, h, func(r *http.Request) { r.SetBasicAuth("ops", "s3cret") })
	if w.Code != http.StatusOK || w.Body.String() != "ops" {
		t.Fatalf("got %d %q, want 200 ops (Identity in context)", w.Code, w.Body.String())
	}
	if w.Header().Get("WWW-Authenticate") != "" {
		t.Fatal("WWW-Authenticate set on success")
	}
}

func TestBasicAuth_MethodIsBasic(t *testing.T) {
	t.Parallel()
	var got guard.Identity
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = guard.MustFrom(r.Context())
	})
	h := guard.BasicAuth(map[string]string{"ops": "s3cret"})(inner)
	basicGet(t, h, func(r *http.Request) { r.SetBasicAuth("ops", "s3cret") })
	if got.Subject != "ops" || got.Method != "basic" {
		t.Fatalf("Identity = %+v, want Subject=ops Method=basic", got)
	}
}

func TestBasicAuth_Failures(t *testing.T) {
	t.Parallel()
	const wantChallenge = `Basic realm="restricted", charset="UTF-8"`
	tests := []struct {
		name    string
		setAuth func(*http.Request)
		wantErr error
	}{
		{"missing header", nil, guard.ErrNoCredential},
		{"malformed header", func(r *http.Request) { r.Header.Set("Authorization", "Basic !!!not-base64!!!") }, guard.ErrNoCredential},
		{"wrong password", func(r *http.Request) { r.SetBasicAuth("ops", "wrong") }, guard.ErrInvalidCredential},
		{"unknown user", func(r *http.Request) { r.SetBasicAuth("nobody", "s3cret") }, guard.ErrInvalidCredential},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var captured error
			h := guard.BasicAuth(map[string]string{"ops": "s3cret"},
				guard.WithResponder(func(w http.ResponseWriter, r *http.Request, err error) {
					captured = err
					w.WriteHeader(http.StatusUnauthorized)
				}),
			)(echoHandler(t))
			w := basicGet(t, h, tt.setAuth)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", w.Code)
			}
			if got := w.Header().Get("WWW-Authenticate"); got != wantChallenge {
				t.Fatalf("WWW-Authenticate = %q, want %q", got, wantChallenge)
			}
			if !errors.Is(captured, tt.wantErr) {
				t.Fatalf("err = %v, want Is(%v)", captured, tt.wantErr)
			}
		})
	}
}

func TestBasicAuth_Realm(t *testing.T) {
	t.Parallel()
	h := guard.BasicAuth(map[string]string{"ops": "s3cret"}, guard.WithRealm("staging"))(echoHandler(t))
	w := basicGet(t, h, nil)
	if got := w.Header().Get("WWW-Authenticate"); got != `Basic realm="staging", charset="UTF-8"` {
		t.Fatalf("WWW-Authenticate = %q", got)
	}
}

func TestBasicAuth_DefaultProblemResponse(t *testing.T) {
	t.Parallel()
	h := guard.BasicAuth(map[string]string{"ops": "s3cret"})(echoHandler(t))
	w := basicGet(t, h, nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
		t.Fatalf("Content-Type = %q, want application/problem+json", ct)
	}
}

func TestParseUsers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		in    string
		want  map[string]string
		isErr bool
	}{
		{"single", "ops:s3cret", map[string]string{"ops": "s3cret"}, false},
		{"multiple with space", "a:1, b:2", map[string]string{"a": "1", "b": "2"}, false},
		{"password with colon", "ops:pa:ss", map[string]string{"ops": "pa:ss"}, false},
		{"empty input", "", nil, true},
		{"no colon", "opspass", nil, true},
		{"empty user", ":pass", nil, true},
		{"empty password", "ops:", nil, true},
		{"duplicate user", "ops:1,ops:2", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := guard.ParseUsers(tt.in)
			if tt.isErr {
				if !errors.Is(err, guard.ErrInvalidUsers) {
					t.Fatalf("err = %v, want Is(ErrInvalidUsers)", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseUsers(%q): %v", tt.in, err)
			}
			if !maps.Equal(got, tt.want) {
				t.Fatalf("ParseUsers(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
