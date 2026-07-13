package guard

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/dmitrymomot/forge/crypto/consttime"
	"github.com/dmitrymomot/forge/web/middleware"
	"github.com/dmitrymomot/forge/web/problem"
)

// dummyPassword is compared for unknown usernames so user existence does
// not leak through response timing.
const dummyPassword = "guard-basicauth-dummy-password-for-unknown-users"

// BasicAuth returns middleware gating requests with HTTP Basic Auth against
// a static username→password map — for pprof/metrics/staging/admin gates,
// never user login (no hashing; credentials come from env, see ParseUsers).
// Password checks are constant-time and unknown users cost the same as
// wrong passwords; unknown-user and wrong-password failures are
// indistinguishable to the client. Every failure gets 401 with a
// WWW-Authenticate Basic challenge through the responder (default
// problem.JSON 401). On success the request carries
// Identity{Subject: username, Method: "basic"} — From/MustFrom work as
// behind New. Panics on an empty users map, or on an empty username or
// password entry — a gate with no valid credentials, or with a credential
// that authenticates unconditionally, is a wiring bug. Accepted options:
// WithRealm, WithResponder; WithExtractors, WithOptional, and WithChallenge
// are ignored (the scheme and challenge are fixed).
func BasicAuth(users map[string]string, opts ...Option) middleware.Middleware {
	if len(users) == 0 {
		panic("guard: BasicAuth requires at least one user")
	}
	for user, pass := range users {
		if user == "" || pass == "" {
			panic("guard: BasicAuth users must have non-empty username and password")
		}
	}
	cfg := config{
		responder: problem.JSON(problem.WithStatus(http.StatusUnauthorized)),
		realm:     "restricted",
	}
	for _, o := range opts {
		o(&cfg)
	}
	challenge := fmt.Sprintf("Basic realm=%q, charset=%q", cfg.realm, "UTF-8")
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reject := func(err error) {
				w.Header().Set("WWW-Authenticate", challenge)
				cfg.responder(w, r, err)
			}
			user, pass, ok := r.BasicAuth()
			if !ok {
				reject(ErrNoCredential)
				return
			}
			want, known := users[user]
			if !known {
				want = dummyPassword
			}
			if !consttime.StringEqual(pass, want) || !known {
				reject(ErrInvalidCredential)
				return
			}
			next.ServeHTTP(w, r.WithContext(identityKey.With(r.Context(), Identity{Subject: user, Method: MethodBasic})))
		})
	}
}

// ParseUsers parses BasicAuth credentials from the "user1:pass1,user2:pass2"
// env-string format. Passwords may contain colons (the split is at the first
// colon) but not commas. Whitespace surrounding each username and password
// is trimmed, so a password cannot carry leading/trailing spaces via this
// helper; internal spaces are preserved. It rejects (wrapping
// ErrInvalidUsers) empty input, entries without a colon, empty usernames or
// passwords, and duplicate usernames.
func ParseUsers(s string) (map[string]string, error) {
	entries := strings.Split(s, ",")
	users := make(map[string]string, len(entries))
	for _, e := range entries {
		user, pass, found := strings.Cut(strings.TrimSpace(e), ":")
		user, pass = strings.TrimSpace(user), strings.TrimSpace(pass)
		if !found || user == "" || pass == "" {
			return nil, fmt.Errorf("%w: entry %q", ErrInvalidUsers, e)
		}
		if _, dup := users[user]; dup {
			return nil, fmt.Errorf("%w: duplicate user %q", ErrInvalidUsers, user)
		}
		users[user] = pass
	}
	return users, nil
}
