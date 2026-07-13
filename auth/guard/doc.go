// Package guard authenticates HTTP requests. A chain of credential
// extractors finds the credential (Authorization header by default;
// cookie and query opt-in), a Verifier resolves it to an Identity stored
// in request context, and rejections are problem+json 401s. Authorization
// (403, permissions) is out of scope — guard answers "who is this request
// from"; session lifecycle (rotation, TTLs) belongs to auth/session and
// scope checks to the future authorization seam.
//
// A Verifier adapter is a small closure — over auth/jwt:
//
//	type appClaims struct {
//		jwt.Claims
//		TenantID string   `json:"tid"`
//		Scopes   []string `json:"scopes"`
//	}
//
//	verifier := guard.VerifierFunc(func(ctx context.Context, token string) (guard.Identity, error) {
//		c, err := jwt.Verify[appClaims](ctx, jwtVerifier, token)
//		if err != nil {
//			return guard.Identity{}, err // client sees a generic 401
//		}
//		return guard.Identity{Subject: c.Subject, Tenant: c.TenantID, Scopes: c.Scopes, Method: "bearer"}, nil
//	})
//
//	authn := guard.New(verifier, guard.WithChallenge(`Bearer realm="api"`))
//	mux.Handle("GET /api/me", authn(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
//		id := guard.MustFrom(r.Context())
//		_ = id.Subject
//	})))
//
// Session-cookie flows swap the extractor and redirect instead of 401:
//
//	authn := guard.New(sessionVerifier,
//		guard.WithExtractors(guard.Cookie("sid")),
//		guard.WithResponder(func(w http.ResponseWriter, r *http.Request, err error) {
//			http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
//		}),
//	)
//
// Public-but-personalized routes use WithOptional: missing credential
// passes anonymously (From reports ok=false), invalid credential still
// gets 401.
//
// BasicAuth gates internal surfaces (pprof, metrics, staging) with static
// env-sourced credentials, constant-time:
//
//	users, err := guard.ParseUsers(os.Getenv("ADMIN_BASIC_USERS")) // "ops:s3cret"
//	if err != nil {
//		log.Fatal(err)
//	}
//	mux.Handle("/debug/pprof/", guard.BasicAuth(users, guard.WithRealm("staging"))(pprofHandler))
package guard
