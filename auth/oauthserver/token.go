package oauthserver

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dmitrymomot/forge/auth/jwt"
	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/crypto/consttime"
	"github.com/dmitrymomot/forge/crypto/digest"
	"github.com/dmitrymomot/forge/resilience/cache"
)

// accessClaims is the access-token claim set. sub is the client for M2M
// tokens and the end user for auth-code tokens; client_id always names
// the requesting client; tenant carries the client's tenant when set.
type accessClaims struct {
	Scope    string `json:"scope,omitempty"`
	ClientID string `json:"client_id,omitempty"`
	Tenant   string `json:"tenant,omitempty"`
	jwt.Claims
}

// TokenHandler serves the RFC 6749 token endpoint (POST). Mount the
// signer's JWKS() next to it so resource servers can verify the JWTs.
// Brute-force throttling composes from resilience/ratelimit middleware.
func (s *Server) TokenHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeTokenError(w, http.StatusMethodNotAllowed, "invalid_request", "POST required")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if err := r.ParseForm(); err != nil {
			writeTokenError(w, http.StatusBadRequest, "invalid_request", "malformed form body")
			return
		}
		cl, ok := s.authenticateClient(w, r)
		if !ok {
			return
		}
		switch r.PostForm.Get("grant_type") {
		case GrantClientCredentials:
			s.handleClientCredentials(w, r, cl)
		case GrantAuthorizationCode:
			s.handleAuthorizationCode(w, r, cl)
		default:
			writeTokenError(w, http.StatusBadRequest, "unsupported_grant_type", "")
		}
	})
}

// authenticateClient supports client_secret_basic and client_secret_post.
// On failure it writes invalid_client and returns ok=false. Unknown ids
// burn a dummy digest compare so they are timing-indistinguishable from
// bad secrets.
func (s *Server) authenticateClient(w http.ResponseWriter, r *http.Request) (Client, bool) {
	clientID, secret, ok := r.BasicAuth()
	if ok {
		// RFC 6749 §2.3.1: basic-auth credentials are form-urlencoded.
		if u, err := url.QueryUnescape(clientID); err == nil {
			clientID = u
		}
		if u, err := url.QueryUnescape(secret); err == nil {
			secret = u
		}
	} else {
		clientID, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	}
	if clientID == "" || secret == "" {
		writeTokenError(w, http.StatusUnauthorized, "invalid_client", "client authentication required")
		return Client{}, false
	}
	presented := digest.SHA256([]byte(secret))
	cl, err := s.store.Get(r.Context(), clientID)
	if err != nil {
		consttime.BytesEqual(presented, s.dummyHash)
		writeTokenError(w, http.StatusUnauthorized, "invalid_client", "")
		return Client{}, false
	}
	if !consttime.BytesEqual(presented, cl.SecretHash) || cl.Revoked() {
		writeTokenError(w, http.StatusUnauthorized, "invalid_client", "")
		return Client{}, false
	}
	return cl, true
}

func (s *Server) handleClientCredentials(w http.ResponseWriter, r *http.Request, cl Client) {
	if !cl.AllowsGrant(GrantClientCredentials) {
		writeTokenError(w, http.StatusBadRequest, "unauthorized_client", "")
		return
	}
	scopes := strings.Fields(r.PostForm.Get("scope"))
	if len(scopes) == 0 {
		scopes = cl.Scopes
	} else if !cl.AllowsScopes(scopes) {
		writeTokenError(w, http.StatusBadRequest, "invalid_scope", "")
		return
	}
	scope := strings.Join(scopes, " ")
	tok, ttl, err := s.signAccessToken(cl.ID, cl, scope)
	if err != nil {
		writeTokenError(w, http.StatusInternalServerError, "server_error", "")
		return
	}
	writeTokenResponse(w, tokenResponse{
		AccessToken: tok, TokenType: "Bearer",
		ExpiresIn: int64(ttl.Seconds()), Scope: scope,
	})
}

// signAccessToken mints the access JWT for sub on behalf of cl.
func (s *Server) signAccessToken(sub string, cl Client, scope string) (string, time.Duration, error) {
	ttl := cl.TokenTTL
	if ttl <= 0 {
		ttl = s.cfg.TokenTTL
	}
	now := s.clk.Now()
	claims := accessClaims{
		Issuer:    s.cfg.Issuer,
		Subject:   sub,
		ID:        id.NewULID().String(),
		ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		IssuedAt:  jwt.NewNumericDate(now),
		Scope:     scope,
		ClientID:  cl.ID,
		Tenant:    cl.TenantID,
	}
	if s.cfg.Audience != "" {
		claims.Audience = jwt.Audience{s.cfg.Audience}
	}
	tok, err := s.signer.Sign(claims)
	return tok, ttl, err
}

// pkceChallenge is the RFC 7636 S256 transform.
func pkceChallenge(verifier string) string {
	return base64.RawURLEncoding.EncodeToString(digest.SHA256([]byte(verifier)))
}

// handleAuthorizationCode redeems a sealed single-use code for an access
// token + id_token. No refresh token is issued: the first-party app builds
// its own session from the result.
func (s *Server) handleAuthorizationCode(w http.ResponseWriter, r *http.Request, cl Client) {
	if s.codes == nil || s.codeStore == nil {
		writeTokenError(w, http.StatusBadRequest, "unsupported_grant_type", "authorization_code not configured")
		return
	}
	if !cl.AllowsGrant(GrantAuthorizationCode) {
		writeTokenError(w, http.StatusBadRequest, "unauthorized_client", "")
		return
	}
	ac, err := s.codes.Parse(r.PostForm.Get("code"))
	if err != nil {
		writeTokenError(w, http.StatusBadRequest, "invalid_grant", "")
		return
	}
	// Single-use: claim the jti before any further validation so every
	// structurally-valid redemption attempt burns the code (RFC 6749
	// leans toward revoking on suspicious replay).
	err = s.codeStore.Set(r.Context(), "oauthserver:code:"+ac.JTI, []byte{1},
		cache.WithTTL(s.codeTTL+time.Minute), cache.WithSetNonExist())
	switch {
	case errors.Is(err, cache.ErrExists):
		writeTokenError(w, http.StatusBadRequest, "invalid_grant", "code already redeemed")
		return
	case err != nil:
		// Store outage: fail closed rather than risk double redemption.
		writeTokenError(w, http.StatusInternalServerError, "server_error", "")
		return
	}
	if ac.ClientID != cl.ID || r.PostForm.Get("redirect_uri") != ac.RedirectURI {
		writeTokenError(w, http.StatusBadRequest, "invalid_grant", "")
		return
	}
	verifier := r.PostForm.Get("code_verifier")
	if verifier == "" || !consttime.StringEqual(pkceChallenge(verifier), ac.Challenge) {
		writeTokenError(w, http.StatusBadRequest, "invalid_grant", "")
		return
	}
	access, ttl, err := s.signAccessToken(ac.Subject, cl, ac.Scope)
	if err != nil {
		writeTokenError(w, http.StatusInternalServerError, "server_error", "")
		return
	}
	idt, err := s.signIDToken(r.Context(), cl, ac)
	if err != nil {
		writeTokenError(w, http.StatusInternalServerError, "server_error", "")
		return
	}
	writeTokenResponse(w, tokenResponse{
		AccessToken: access, TokenType: "Bearer",
		ExpiresIn: int64(ttl.Seconds()), Scope: ac.Scope, IDToken: idt,
	})
}

// reservedIDClaims are id_token claims the WithUserClaims hook may not override.
var reservedIDClaims = map[string]bool{
	"iss": true, "sub": true, "aud": true, "exp": true, "iat": true, "nonce": true,
}

// signIDToken mints the OIDC id_token for the code's subject, audience'd
// to the redeeming client.
func (s *Server) signIDToken(ctx context.Context, cl Client, ac authCode) (string, error) {
	ttl := cl.TokenTTL
	if ttl <= 0 {
		ttl = s.cfg.TokenTTL
	}
	now := s.clk.Now()
	claims := map[string]any{
		"iss": s.cfg.Issuer,
		"sub": ac.Subject,
		"aud": cl.ID,
		"exp": now.Add(ttl).Unix(),
		"iat": now.Unix(),
	}
	if ac.Nonce != "" {
		claims["nonce"] = ac.Nonce
	}
	if s.userClaims != nil {
		extra, err := s.userClaims(ctx, ac.Subject)
		if err != nil {
			return "", fmt.Errorf("oauthserver: user claims hook: %w", err)
		}
		for k, v := range extra {
			if !reservedIDClaims[k] {
				claims[k] = v
			}
		}
	}
	return s.signer.Sign(claims)
}
