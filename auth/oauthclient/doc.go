// Package oauthclient implements the OAuth2 authorization-code flow with
// PKCE (RFC 7636, always S256) plus the OIDC layer (id_token + nonce
// verification via auth/jwt), as a login-only identity broker: the product
// of a flow is a verified Identity; the raw TokenResponse is exposed once
// and persisting it is the caller's concern.
//
// Flow state (state, PKCE verifier, nonce, tenancy binding, return-to)
// rides a sealed crypto/token blob, so the client is stateless. Begin and
// Complete carry the blob in an HttpOnly cookie for server-rendered apps;
// AuthURL and Exchange expose the same blob as a caller-held token for
// SPA/BFF/mobile transports.
//
//	ks, _ := keyset.New(keyset.WithBase64Keys(os.Getenv("OAUTHCLIENT_KEYS")))
//	client, err := oauthclient.New(ks,
//	    oauthclient.WithRedirectURL("https://app.example.com/auth/callback"),
//	    oauthclient.WithProvider("google", oauthclient.Google(oauthclient.ProviderConfig{
//	        ClientID:     os.Getenv("OAUTH_GOOGLE_CLIENT_ID"),
//	        ClientSecret: os.Getenv("OAUTH_GOOGLE_CLIENT_SECRET"),
//	    })),
//	    oauthclient.WithProvider("github", oauthclient.GitHub(oauthclient.ProviderConfig{
//	        ClientID:     os.Getenv("OAUTH_GITHUB_CLIENT_ID"),
//	        ClientSecret: os.Getenv("OAUTH_GITHUB_CLIENT_SECRET"),
//	    })),
//	)
//	if err != nil { ... }
//
//	mux.HandleFunc("GET /auth/{provider}", func(w http.ResponseWriter, r *http.Request) {
//	    if err := client.Begin(w, r, r.PathValue("provider")); err != nil {
//	        http.Error(w, "unknown provider", http.StatusNotFound)
//	    }
//	})
//	mux.HandleFunc("GET /auth/callback", func(w http.ResponseWriter, r *http.Request) {
//	    res, err := client.Complete(w, r)
//	    if err != nil { http.Error(w, "login failed", http.StatusBadRequest); return }
//	    // create the app session from res.Identity, then:
//	    http.Redirect(w, r, cmp.Or(res.ReturnTo, "/"), http.StatusSeeOther)
//	})
//
// Separate JS frontend (SvelteKit/Next) or mobile: call AuthURL, return
// {url, flow_token} as JSON, have the frontend hold flow_token (its own
// cookie/session) and POST it back with the callback query; finish with
// Exchange. Same sealed blob — the cookie is just one transport.
//
// Enterprise/per-tenant IdPs: Discover(ctx, issuer, cfg) fills a Provider
// from OIDC discovery at onboarding time; serve per-tenant providers via
// WithProviderSource, and pin flows to a tenant with WithScope (the value
// is sealed at Begin and must match at Complete — fail-closed). The
// id_token verifier cache is keyed per (issuer, jwks, clientID) and retained
// for the process lifetime, so a WithProviderSource fleet accumulates one
// entry per distinct tenant provider seen.
//
// Forge's own oauthserver is just another provider: hand-build
// Provider{AuthURL, TokenURL, JWKSURL, Issuer, ClientID, ClientSecret,
// Scopes} pointing at its endpoints; see auth/oauthserver.
package oauthclient
