// Package oauthserver is an OAuth2 provider for exactly two audiences:
// machine-to-machine partners (client_credentials, RFC 6749 §4.4) and
// first-party trusted apps or white-label mirrors (authorization_code with
// mandatory PKCE S256). It is deliberately NOT a third-party identity
// provider: no consent screens, no external or dynamic client
// registration, no discovery metadata, no userinfo endpoint, no JWE, no
// refresh tokens — run Hydra/Keycloak if you need to issue tokens to other
// companies' apps.
//
// Tokens are short-lived JWTs minted by an injected auth/jwt Signer; there
// are no introspection or revocation endpoints because outstanding tokens
// expire within TokenTTL (default 15m). Revoking a client stops NEW tokens
// immediately; already-issued JWTs remain valid until exp.
//
//	signer, _ := jwt.NewSigner(jwt.WithKeyset(jwtKeys))
//	srv, err := oauthserver.New(signer, oauthserver.NewMemoryStore(),
//	    oauthserver.WithConfig(cfg), // OAUTHSERVER_ISSUER / _AUDIENCE / _TOKEN_TTL
//	)
//	if err != nil { ... }
//	mux.Handle("POST /oauth/token", srv.TokenHandler())
//	mux.Handle("GET /.well-known/jwks.json", signer.JWKS())
//
//	// Partner onboarding — the secret is returned exactly once:
//	creds, err := srv.CreateClient(ctx, oauthserver.CreateClientInput{
//	    Name:   "acme-sportsbook",
//	    Grants: []string{oauthserver.GrantClientCredentials},
//	    Scopes: []string{"read:odds", "write:bets"},
//	})
//
// Resource servers verify tokens with a plain jwt.Verifier pointed at the
// JWKS URL, pinning issuer and audience; the scope / tenant / client_id
// claims carry authorization context.
//
// First-party user flow (SSO across your own apps and mirrors): add the
// three auth-code inputs and mount the authorize endpoint. The
// Authenticator seam answers "who is logged in?" — redirect to your login
// page and return ok=false when nobody is.
//
//	srv, err := oauthserver.New(signer, store,
//	    oauthserver.WithConfig(cfg),
//	    oauthserver.WithCodeKeyset(codeKeys),          // seals auth codes
//	    oauthserver.WithCodeStore(redisCache),         // single-use claims
//	    oauthserver.WithAuthenticator(func(w http.ResponseWriter, r *http.Request) (string, bool) {
//	        sess, ok := sessions.FromRequest(r)
//	        if !ok {
//	            http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.String()), http.StatusSeeOther)
//	            return "", false
//	        }
//	        return sess.UserID, true
//	    }),
//	    oauthserver.WithUserClaims(loadProfileClaims), // optional id_token enrichment
//	)
//	authorize, err := srv.AuthorizeHandler()
//	mux.Handle("GET /oauth/authorize", authorize)
//
// Each mirror is then a plain auth/oauthclient consumer with a hand-built
// provider — no discovery needed for first-party apps:
//
//	oauthclient.WithProvider("platform", oauthclient.Provider{
//	    ClientID:     creds.ClientID,
//	    ClientSecret: creds.ClientSecret,
//	    AuthURL:      "https://auth.platform.com/oauth/authorize",
//	    TokenURL:     "https://auth.platform.com/oauth/token",
//	    JWKSURL:      "https://auth.platform.com/.well-known/jwks.json",
//	    Issuer:       "https://auth.platform.com",
//	    Scopes:       []string{"profile"},
//	})
//
// Multi-tenant: WithScope(fn) tenancy-scopes the management methods
// (fail-closed); issuance derives the tenant claim from the client record
// itself, so one global token endpoint serves every tenant and resource
// APIs enforce isolation by verifying the tenant claim.
//
// The token endpoint speaks RFC 6749 wire JSON (including §5.2 errors),
// not problem+json — partners' OAuth libraries expect the RFC shape. Rate
// limiting composes from resilience/ratelimit middleware.
package oauthserver
