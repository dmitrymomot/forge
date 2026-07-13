package oauthserver

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/dmitrymomot/forge/core/id"
)

// AuthorizeHandler serves the first-party authorization endpoint (GET).
// It requires WithAuthenticator (who is logged in), WithCodeStore
// (single-use enforcement), and WithCodeKeyset (code sealing) — it fails
// closed without them. There is no consent screen: every registered
// auth-code client is first-party and trusted by definition.
func (s *Server) AuthorizeHandler() (http.Handler, error) {
	if s.authenticator == nil || s.codeStore == nil || s.codes == nil {
		return nil, fmt.Errorf("%w: AuthorizeHandler requires WithAuthenticator, WithCodeStore and WithCodeKeyset", ErrInvalidConfig)
	}
	return http.HandlerFunc(s.authorize), nil
}

func (s *Server) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	redirect := q.Get("redirect_uri")
	cl, err := s.store.Get(r.Context(), q.Get("client_id"))
	if err != nil || cl.Revoked() || !cl.AllowsGrant(GrantAuthorizationCode) || !cl.AllowsRedirect(redirect) {
		// RFC 6749 §4.1.2.1: never redirect to an unvalidated URI.
		http.Error(w, "invalid client_id or redirect_uri", http.StatusBadRequest)
		return
	}
	state := q.Get("state")
	if q.Get("response_type") != "code" {
		s.redirectError(w, r, redirect, state, "unsupported_response_type", "")
		return
	}
	scopes := strings.Fields(q.Get("scope"))
	if len(scopes) == 0 {
		scopes = cl.Scopes
	} else if !cl.AllowsScopes(scopes) {
		s.redirectError(w, r, redirect, state, "invalid_scope", "")
		return
	}
	challenge := q.Get("code_challenge")
	if challenge == "" || q.Get("code_challenge_method") != "S256" {
		s.redirectError(w, r, redirect, state, "invalid_request", "PKCE with S256 is required")
		return
	}
	subject, ok := s.authenticator(w, r)
	if !ok {
		return // the authenticator wrote the response (e.g. login redirect)
	}
	if subject == "" {
		s.redirectError(w, r, redirect, state, "server_error", "")
		return
	}
	code, err := s.codes.Issue(authCode{
		JTI:         id.NewULID().String(),
		ClientID:    cl.ID,
		RedirectURI: redirect,
		Subject:     subject,
		Scope:       strings.Join(scopes, " "),
		Nonce:       q.Get("nonce"),
		Challenge:   challenge,
	})
	if err != nil {
		s.redirectError(w, r, redirect, state, "server_error", "")
		return
	}
	u, err := url.Parse(redirect)
	if err != nil {
		http.Error(w, "invalid redirect_uri", http.StatusBadRequest)
		return
	}
	qq := u.Query()
	qq.Set("code", code)
	if state != "" {
		qq.Set("state", state)
	}
	u.RawQuery = qq.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

// redirectError sends an RFC 6749 §4.1.2.1 error back to the (already
// validated) redirect URI.
func (s *Server) redirectError(w http.ResponseWriter, r *http.Request, redirect, state, code, desc string) {
	u, err := url.Parse(redirect)
	if err != nil {
		http.Error(w, "invalid redirect_uri", http.StatusBadRequest)
		return
	}
	q := u.Query()
	q.Set("error", code)
	if desc != "" {
		q.Set("error_description", desc)
	}
	if state != "" {
		q.Set("state", state)
	}
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}
