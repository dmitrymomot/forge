# auth/oauthclient + auth/oauthserver Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship `auth/oauthclient` (OAuth2/OIDC login client: auth-code + PKCE, Google/GitHub presets, OIDC discovery, cookie + token flow transports) and `auth/oauthserver` (OAuth2 provider: M2M client-credentials + first-party authorization-code for trusted apps/mirrors, Store-backed client registry, pgstore driver) as one bundle.

**Architecture:** oauthclient is a stateless `Client` whose flow state (state/PKCE/nonce/binding) rides a sealed `crypto/token` blob — cookie and raw-token transports share one code path; id_tokens verify through per-provider cached `auth/jwt` Verifiers. oauthserver is a `Server` over an injected `*jwt.Signer` + `Store` registry; the token endpoint speaks RFC 6749 JSON, auth codes are sealed `crypto/token` blobs made single-use by an atomic SetNX claim on `resilience/cache.Store`.

**Tech Stack:** Go stdlib + forge bricks only: `auth/jwt`, `web/httpclient`, `crypto/token`, `crypto/keyset`, `crypto/digest`, `crypto/consttime`, `core/random`, `core/id`, `core/clock`, `resilience/cache`; `pgx` in `oauthserver/pgstore` only. Tests: testify, httptest.

**Spec:** `docs/superpowers/specs/2026-07-13-oauth-client-server-design.md` (read it before starting a task if anything here seems ambiguous).

## Global Constraints

- Module path: `github.com/dmitrymomot/forge`. Branch: stay on the current branch; never switch.
- After changing Go files in a package run `just fmt ./auth/oauthclient/...` (or the package you touched). After finishing a task run `just lint` and `just test ./auth/...` — both must pass before commit.
- Black-box tests only: test files use `package oauthclient_test` / `package oauthserver_test` / `package pgstore_test`.
- Errors: single-line `errors.Is`-matchable sentinels in `errors.go`. No error text from internals may reach HTTP response bodies in oauthserver.
- No new external dependencies. No `golang.org/x/oauth2`.
- `for i := range N` loops (never C-style); `httptest.NewRequest` (never `http.NewRequest` with `_`); nilaway/betteralign/modernize run in `just lint`.
- NEVER add Claude/AI attribution to commits, PRs, or comments.
- Env prefixes baked into tags: `OAUTHCLIENT_*`, `OAUTHSERVER_*`.
- Both packages must ship `doc.go` (package comment with runnable example), `config.go`, `options.go`, `errors.go`, `bench_test.go`.
- Postgres integration tests: skip unless `FORGE_TEST_POSTGRES_DSN` is set (repo convention).

## File Structure

```
auth/oauthclient/
  doc.go            — package comment + wiring example
  errors.go         — sentinels + ProviderError
  config.go         — Config / DefaultConfig / Validate
  options.go        — Option funcs for New
  provider.go       — Provider, ProviderConfig, Google preset, reserved params
  github.go         — GitHub preset + Identity hook (user + emails API)
  discover.go       — OIDC discovery
  client.go         — Client, New, FromConfig, resolve, verifier cache
  flow.go           — flowState, AuthURL, BeginOption, PKCE helpers
  exchange.go       — Exchange, token endpoint POST, id_token verify, Result
  cookie.go         — Begin / Complete cookie transport
  bench_test.go
auth/oauthserver/
  doc.go            — package comment + wiring example (incl. mirror recipe)
  errors.go         — sentinels
  config.go         — Config / DefaultConfig / Validate
  options.go        — Option funcs for New
  client.go         — Client record, grant consts, helper methods
  store.go          — Store interface
  memstore.go       — in-memory Store
  server.go         — Server, New
  manage.go         — CreateClient/RotateSecret/RevokeClient/GetClient/ListClients
  rfc.go            — RFC 6749 JSON error/response writers
  token.go          — TokenHandler: client auth + client_credentials + authorization_code
  authorize.go      — AuthorizeHandler + authCode blob
  bench_test.go
auth/oauthserver/pgstore/
  doc.go
  pgstore.go        — pgx Store impl
  migrations/00001_oauth_clients.sql
```

Task order: 1–6 oauthclient, 7–12 oauthserver, 13 pgstore, 14 integration + catalog cleanup. Tasks 1–6 and 7–12 are two independent chains; 14 needs both.

---

### Task 1: oauthclient provider model (errors, Provider, Google + GitHub presets)

**Files:**
- Create: `auth/oauthclient/errors.go`
- Create: `auth/oauthclient/provider.go`
- Create: `auth/oauthclient/github.go`
- Create: `auth/oauthclient/types.go` (Identity, TokenResponse — shared by everything)
- Test: `auth/oauthclient/provider_test.go`, `auth/oauthclient/github_test.go`

**Interfaces:**
- Consumes: nothing (first task).
- Produces (later tasks rely on exact shapes):
  - `type Identity struct { Provider, Subject, Email string; EmailVerified bool; Name, Picture string; Raw map[string]any }`
  - `type TokenResponse struct { AccessToken, TokenType, RefreshToken, IDToken, Scope string; ExpiresAt time.Time }`
  - `type Provider struct { ClientID, ClientSecret, AuthURL, TokenURL, JWKSURL, Issuer, RedirectURL string; Scopes []string; AuthParams map[string]string; Identity func(ctx context.Context, hc *http.Client, token TokenResponse) (Identity, error) }` with method `validate() error` (unexported)
  - `type ProviderConfig struct { ClientID string \`env:"CLIENT_ID,required"\`; ClientSecret string \`env:"CLIENT_SECRET,required"\`; RedirectURL string \`env:"REDIRECT_URL"\`; Scopes []string \`env:"SCOPES"\`; AuthParams map[string]string }`
  - `func Google(cfg ProviderConfig) Provider`, `func GitHub(cfg ProviderConfig) Provider`
  - `var reservedParams map[string]bool` (unexported; keys: client_id, redirect_uri, response_type, scope, state, nonce, code_challenge, code_challenge_method)
  - Sentinels: `ErrUnknownProvider, ErrFlowExpired, ErrStateMismatch, ErrNonceMismatch, ErrScopeBinding, ErrNoIdentity, ErrInvalidConfig, ErrReservedParam, ErrDiscovery` + `type ProviderError struct { Code, Description string }` with `Error() string`

- [ ] **Step 1: Write the failing tests**

`auth/oauthclient/provider_test.go`:

```go
package oauthclient_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/auth/oauthclient"
)

func TestGooglePreset(t *testing.T) {
	p := oauthclient.Google(oauthclient.ProviderConfig{ClientID: "cid", ClientSecret: "sec"})
	assert.Equal(t, "cid", p.ClientID)
	assert.Equal(t, "https://accounts.google.com/o/oauth2/v2/auth", p.AuthURL)
	assert.Equal(t, "https://oauth2.googleapis.com/token", p.TokenURL)
	assert.Equal(t, "https://www.googleapis.com/oauth2/v3/certs", p.JWKSURL)
	assert.Equal(t, "https://accounts.google.com", p.Issuer)
	assert.Equal(t, []string{"openid", "email", "profile"}, p.Scopes)
	assert.Nil(t, p.Identity, "google is OIDC — no identity hook")
}

func TestGooglePresetScopeOverride(t *testing.T) {
	p := oauthclient.Google(oauthclient.ProviderConfig{ClientID: "cid", ClientSecret: "s", Scopes: []string{"openid"}})
	assert.Equal(t, []string{"openid"}, p.Scopes)
}

func TestGitHubPreset(t *testing.T) {
	p := oauthclient.GitHub(oauthclient.ProviderConfig{ClientID: "cid", ClientSecret: "sec"})
	assert.Equal(t, "https://github.com/login/oauth/authorize", p.AuthURL)
	assert.Equal(t, "https://github.com/login/oauth/access_token", p.TokenURL)
	assert.Equal(t, []string{"read:user", "user:email"}, p.Scopes)
	assert.NotNil(t, p.Identity, "github is not OIDC — identity hook required")
	assert.Empty(t, p.Issuer)
}

func TestProviderErrorMessage(t *testing.T) {
	e := &oauthclient.ProviderError{Code: "access_denied", Description: "user said no"}
	assert.Equal(t, "oauthclient: provider error: access_denied: user said no", e.Error())
	e2 := &oauthclient.ProviderError{Code: "access_denied"}
	assert.Equal(t, "oauthclient: provider error: access_denied", e2.Error())
}
```

`auth/oauthclient/github_test.go` (tests the Identity hook against a fake GitHub API):

```go
package oauthclient_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/oauthclient"
)

// fakeGitHub serves /user and /user/emails like api.github.com.
func fakeGitHub(t *testing.T, emailsStatus int) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /user", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer gho_token", r.Header.Get("Authorization"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": 12345, "login": "octocat", "name": "Octo Cat",
			"avatar_url": "https://example.com/a.png", "email": "public@example.com",
		})
	})
	mux.HandleFunc("GET /user/emails", func(w http.ResponseWriter, r *http.Request) {
		if emailsStatus != http.StatusOK {
			w.WriteHeader(emailsStatus)
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"email": "old@example.com", "primary": false, "verified": true},
			{"email": "primary@example.com", "primary": true, "verified": true},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestGitHubIdentityHook(t *testing.T) {
	srv := fakeGitHub(t, http.StatusOK)
	p := oauthclient.GitHub(oauthclient.ProviderConfig{ClientID: "c", ClientSecret: "s"})
	// Redirect the hook at the fake API (test-only export, see Step 3).
	p.Identity = oauthclient.GitHubIdentity(srv.URL)

	id, err := p.Identity(context.Background(), srv.Client(), oauthclient.TokenResponse{AccessToken: "gho_token"})
	require.NoError(t, err)
	assert.Equal(t, "12345", id.Subject)
	assert.Equal(t, "primary@example.com", id.Email)
	assert.True(t, id.EmailVerified)
	assert.Equal(t, "Octo Cat", id.Name)
	assert.Equal(t, "https://example.com/a.png", id.Picture)
	assert.Equal(t, "octocat", id.Raw["login"])
}

func TestGitHubIdentityHookEmailsForbiddenFallsBack(t *testing.T) {
	srv := fakeGitHub(t, http.StatusForbidden)
	hook := oauthclient.GitHubIdentity(srv.URL)
	id, err := hook(context.Background(), srv.Client(), oauthclient.TokenResponse{AccessToken: "gho_token"})
	require.NoError(t, err)
	assert.Equal(t, "public@example.com", id.Email)
	assert.False(t, id.EmailVerified)
}

func TestGitHubIdentityHookUserEndpointFails(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /user", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	hook := oauthclient.GitHubIdentity(srv.URL)
	_, err := hook(context.Background(), srv.Client(), oauthclient.TokenResponse{AccessToken: "bad"})
	var perr *oauthclient.ProviderError
	require.ErrorAs(t, err, &perr)
	assert.Equal(t, "userinfo_failed", perr.Code)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./auth/oauthclient/...`
Expected: FAIL — `no required module provides package` / `undefined: oauthclient.Google` (package doesn't exist yet).

- [ ] **Step 3: Write the implementation**

`auth/oauthclient/errors.go`:

```go
package oauthclient

import "errors"

var (
	// ErrUnknownProvider is returned when a provider name matches neither the
	// static registry nor the provider source.
	ErrUnknownProvider = errors.New("oauthclient: unknown provider")
	// ErrFlowExpired is returned when the flow token/cookie is missing,
	// expired, or tampered with.
	ErrFlowExpired = errors.New("oauthclient: flow expired or missing")
	// ErrStateMismatch is returned when the callback state does not match the flow.
	ErrStateMismatch = errors.New("oauthclient: state mismatch")
	// ErrNonceMismatch is returned when the id_token nonce does not match the flow.
	ErrNonceMismatch = errors.New("oauthclient: nonce mismatch")
	// ErrScopeBinding is returned when the tenancy scope at Exchange differs
	// from the one sealed at Begin.
	ErrScopeBinding = errors.New("oauthclient: scope binding mismatch")
	// ErrNoIdentity is returned when a provider yields neither an id_token
	// nor an Identity hook result with a subject.
	ErrNoIdentity = errors.New("oauthclient: provider returned no identity")
	// ErrInvalidConfig is returned by New/FromConfig/AuthURL for invalid setup.
	ErrInvalidConfig = errors.New("oauthclient: invalid config")
	// ErrReservedParam is returned when Provider.AuthParams collides with a
	// protocol-owned authorize parameter.
	ErrReservedParam = errors.New("oauthclient: reserved auth param")
	// ErrDiscovery is returned when OIDC discovery fails or the returned
	// issuer does not match the requested one.
	ErrDiscovery = errors.New("oauthclient: discovery failed")
)

// ProviderError is an OAuth 2.0 error returned by the provider, either via
// the error= callback parameters or an RFC 6749 §5.2 token-endpoint response.
type ProviderError struct {
	Code        string
	Description string
}

func (e *ProviderError) Error() string {
	if e.Description == "" {
		return "oauthclient: provider error: " + e.Code
	}
	return "oauthclient: provider error: " + e.Code + ": " + e.Description
}
```

`auth/oauthclient/types.go`:

```go
package oauthclient

import "time"

// Identity is the normalized user identity produced by Complete/Exchange.
type Identity struct {
	Provider      string
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
	Picture       string
	// Raw holds the full id_token claims (OIDC path) or the provider profile
	// payload (Identity-hook path) for fields the normalized set omits.
	Raw map[string]any
}

// TokenResponse is the provider's raw token-endpoint response. It is exposed
// once in Result; storing it (for later provider-API calls) is the caller's
// concern — this package is login-only.
type TokenResponse struct {
	AccessToken  string
	TokenType    string
	RefreshToken string
	IDToken      string
	Scope        string
	// ExpiresAt is Now+expires_in at exchange time; zero when the provider
	// omitted expires_in.
	ExpiresAt time.Time
}
```

`auth/oauthclient/provider.go`:

```go
package oauthclient

import (
	"context"
	"fmt"
	"net/http"
)

// Provider describes one OAuth2/OIDC provider. Google/GitHub presets and
// Discover fill it; hand-building one is the recipe for odd providers and
// for forge's own oauthserver.
type Provider struct {
	ClientID     string
	ClientSecret string
	AuthURL      string
	TokenURL     string
	JWKSURL      string
	Issuer       string
	// RedirectURL overrides the client-wide default for this provider.
	RedirectURL string
	Scopes      []string
	// AuthParams are extra authorize-URL parameters (prompt, hd, allow_signup…).
	// Protocol-owned parameters are reserved; a collision fails AuthURL/Begin.
	AuthParams map[string]string
	// Identity fetches the user identity for providers without OIDC
	// id_tokens (GitHub). When nil the OIDC id_token path is used and
	// Issuer + JWKSURL are required.
	Identity func(ctx context.Context, hc *http.Client, token TokenResponse) (Identity, error)
}

// ProviderConfig is the env-loadable per-provider config shared by all
// presets and Discover. Nest it with a tagged prefix to separate providers:
//
//	type Config struct {
//	    Google oauthclient.ProviderConfig `env:"OAUTH_GOOGLE"`
//	    GitHub oauthclient.ProviderConfig `env:"OAUTH_GITHUB"`
//	}
type ProviderConfig struct {
	ClientID     string   `env:"CLIENT_ID,required"`
	ClientSecret string   `env:"CLIENT_SECRET,required"`
	RedirectURL  string   `env:"REDIRECT_URL"`
	Scopes       []string `env:"SCOPES"`
	AuthParams   map[string]string
}

// reservedParams are authorize-URL parameters owned by the flow; AuthParams
// may not override them.
var reservedParams = map[string]bool{
	"client_id": true, "redirect_uri": true, "response_type": true,
	"scope": true, "state": true, "nonce": true,
	"code_challenge": true, "code_challenge_method": true,
}

func (p Provider) validate() error {
	if p.ClientID == "" || p.AuthURL == "" || p.TokenURL == "" {
		return fmt.Errorf("%w: provider needs ClientID, AuthURL and TokenURL", ErrInvalidConfig)
	}
	if p.Identity == nil && (p.Issuer == "" || p.JWKSURL == "") {
		return fmt.Errorf("%w: OIDC provider needs Issuer and JWKSURL (or set an Identity hook)", ErrInvalidConfig)
	}
	for k := range p.AuthParams {
		if reservedParams[k] {
			return fmt.Errorf("%w: %q", ErrReservedParam, k)
		}
	}
	return nil
}

// Google returns the Google OIDC preset. Default scopes: openid email profile.
func Google(cfg ProviderConfig) Provider {
	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = []string{"openid", "email", "profile"}
	}
	return Provider{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		AuthURL:      "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL:     "https://oauth2.googleapis.com/token",
		JWKSURL:      "https://www.googleapis.com/oauth2/v3/certs",
		Issuer:       "https://accounts.google.com",
		RedirectURL:  cfg.RedirectURL,
		Scopes:       scopes,
		AuthParams:   cfg.AuthParams,
	}
}
```

`auth/oauthclient/github.go`:

```go
package oauthclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
)

const githubAPIBase = "https://api.github.com"

// GitHub returns the GitHub OAuth2 preset. GitHub does not implement OIDC,
// so identity comes from its user API via the Identity hook. Default
// scopes: read:user user:email.
func GitHub(cfg ProviderConfig) Provider {
	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = []string{"read:user", "user:email"}
	}
	return Provider{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		AuthURL:      "https://github.com/login/oauth/authorize",
		TokenURL:     "https://github.com/login/oauth/access_token",
		RedirectURL:  cfg.RedirectURL,
		Scopes:       scopes,
		AuthParams:   cfg.AuthParams,
		Identity:     GitHubIdentity(githubAPIBase),
	}
}

// GitHubIdentity returns the GitHub identity hook against apiBase. Exported
// so tests (and GitHub-Enterprise consumers) can point it at another host.
func GitHubIdentity(apiBase string) func(context.Context, *http.Client, TokenResponse) (Identity, error) {
	return func(ctx context.Context, hc *http.Client, tok TokenResponse) (Identity, error) {
		var raw map[string]any
		if err := getJSON(ctx, hc, apiBase+"/user", tok.AccessToken, &raw); err != nil {
			return Identity{}, err
		}
		id := Identity{
			Subject: strconv.FormatInt(int64(num(raw["id"])), 10),
			Email:   str(raw["email"]),
			Name:    str(raw["name"]),
			Picture: str(raw["avatar_url"]),
			Raw:     raw,
		}
		if id.Name == "" {
			id.Name = str(raw["login"])
		}
		if id.Subject == "0" {
			return Identity{}, ErrNoIdentity
		}
		// Secondary call; missing user:email scope (403/404) falls back to
		// the public profile email already set above.
		var emails []struct {
			Email    string `json:"email"`
			Primary  bool   `json:"primary"`
			Verified bool   `json:"verified"`
		}
		if err := getJSON(ctx, hc, apiBase+"/user/emails", tok.AccessToken, &emails); err == nil {
			for _, e := range emails {
				if e.Primary {
					id.Email, id.EmailVerified = e.Email, e.Verified
					break
				}
			}
		}
		return id, nil
	}
}

// getJSON GETs url with a bearer token and decodes the JSON body (capped at
// 1 MiB). Non-200 responses become *ProviderError{Code: "userinfo_failed"}.
func getJSON(ctx context.Context, hc *http.Client, url, bearer string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("oauthclient: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Accept", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("oauthclient: userinfo: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only body
	if resp.StatusCode != http.StatusOK {
		return &ProviderError{Code: "userinfo_failed", Description: resp.Status}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("oauthclient: userinfo: %w", err)
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return &ProviderError{Code: "userinfo_failed", Description: "malformed JSON"}
	}
	return nil
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func num(v any) float64 {
	f, _ := v.(float64)
	return f
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./auth/oauthclient/...`
Expected: PASS (all provider + github tests).

- [ ] **Step 5: Format, lint, commit**

```bash
just fmt ./auth/oauthclient/...
just lint
git add auth/oauthclient
git commit -m "feat(oauthclient): provider model with Google and GitHub presets"
```

---

### Task 2: oauthclient OIDC discovery

**Files:**
- Create: `auth/oauthclient/discover.go`
- Test: `auth/oauthclient/discover_test.go`

**Interfaces:**
- Consumes: `Provider`, `ProviderConfig`, `ErrDiscovery` from Task 1.
- Produces: `func Discover(ctx context.Context, issuer string, cfg ProviderConfig, opts ...DiscoverOption) (Provider, error)`, `type DiscoverOption`, `func WithDiscoverClient(hc *http.Client) DiscoverOption`.

- [ ] **Step 1: Write the failing tests**

`auth/oauthclient/discover_test.go`:

```go
package oauthclient_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/oauthclient"
)

func fakeIssuer(t *testing.T, mutate func(doc map[string]string, issuer string)) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		doc := map[string]string{
			"issuer":                 srv.URL,
			"authorization_endpoint": srv.URL + "/authorize",
			"token_endpoint":         srv.URL + "/token",
			"jwks_uri":               srv.URL + "/jwks",
		}
		if mutate != nil {
			mutate(doc, srv.URL)
		}
		_ = json.NewEncoder(w).Encode(doc)
	})
	return srv
}

func TestDiscover(t *testing.T) {
	srv := fakeIssuer(t, nil)
	p, err := oauthclient.Discover(context.Background(), srv.URL,
		oauthclient.ProviderConfig{ClientID: "cid", ClientSecret: "sec"},
		oauthclient.WithDiscoverClient(srv.Client()))
	require.NoError(t, err)
	assert.Equal(t, srv.URL+"/authorize", p.AuthURL)
	assert.Equal(t, srv.URL+"/token", p.TokenURL)
	assert.Equal(t, srv.URL+"/jwks", p.JWKSURL)
	assert.Equal(t, srv.URL, p.Issuer)
	assert.Equal(t, []string{"openid", "email", "profile"}, p.Scopes)
	assert.Nil(t, p.Identity)
}

func TestDiscoverTrailingSlashAndScopeOverride(t *testing.T) {
	srv := fakeIssuer(t, nil)
	p, err := oauthclient.Discover(context.Background(), srv.URL+"/",
		oauthclient.ProviderConfig{ClientID: "c", ClientSecret: "s", Scopes: []string{"openid", "groups"}},
		oauthclient.WithDiscoverClient(srv.Client()))
	require.NoError(t, err)
	assert.Equal(t, []string{"openid", "groups"}, p.Scopes)
}

func TestDiscoverIssuerMismatch(t *testing.T) {
	srv := fakeIssuer(t, func(doc map[string]string, _ string) { doc["issuer"] = "https://evil.example.com" })
	_, err := oauthclient.Discover(context.Background(), srv.URL,
		oauthclient.ProviderConfig{ClientID: "c", ClientSecret: "s"},
		oauthclient.WithDiscoverClient(srv.Client()))
	require.ErrorIs(t, err, oauthclient.ErrDiscovery)
}

func TestDiscoverHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	_, err := oauthclient.Discover(context.Background(), srv.URL,
		oauthclient.ProviderConfig{ClientID: "c", ClientSecret: "s"},
		oauthclient.WithDiscoverClient(srv.Client()))
	require.ErrorIs(t, err, oauthclient.ErrDiscovery)
}

func TestDiscoverIncompleteDocument(t *testing.T) {
	srv := fakeIssuer(t, func(doc map[string]string, _ string) { delete(doc, "token_endpoint") })
	_, err := oauthclient.Discover(context.Background(), srv.URL,
		oauthclient.ProviderConfig{ClientID: "c", ClientSecret: "s"},
		oauthclient.WithDiscoverClient(srv.Client()))
	require.ErrorIs(t, err, oauthclient.ErrDiscovery)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./auth/oauthclient/ -run TestDiscover -v`
Expected: FAIL with `undefined: oauthclient.Discover`.

- [ ] **Step 3: Write the implementation**

`auth/oauthclient/discover.go`:

```go
package oauthclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/dmitrymomot/forge/web/httpclient"
)

// DiscoverOption configures Discover.
type DiscoverOption func(*discoverConfig)

type discoverConfig struct {
	hc *http.Client
}

// WithDiscoverClient sets the HTTP client used for the discovery fetch.
// Default: httpclient.New with a 15s timeout.
func WithDiscoverClient(hc *http.Client) DiscoverOption {
	return func(c *discoverConfig) { c.hc = hc }
}

// Discover builds a Provider from an issuer's RFC 8414 / OIDC discovery
// document. Call it at config or tenant-onboarding time — never per
// request — and cache the result (e.g. in the tenant's IdP row).
func Discover(ctx context.Context, issuer string, cfg ProviderConfig, opts ...DiscoverOption) (Provider, error) {
	var dc discoverConfig
	for _, o := range opts {
		o(&dc)
	}
	hc := dc.hc
	if hc == nil {
		hc = httpclient.New(httpclient.WithTimeout(15 * time.Second))
	}
	issuer = strings.TrimSuffix(issuer, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, issuer+"/.well-known/openid-configuration", nil)
	if err != nil {
		return Provider{}, fmt.Errorf("%w: %v", ErrDiscovery, err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return Provider{}, fmt.Errorf("%w: %v", ErrDiscovery, err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only body
	if resp.StatusCode != http.StatusOK {
		return Provider{}, fmt.Errorf("%w: %s", ErrDiscovery, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Provider{}, fmt.Errorf("%w: %v", ErrDiscovery, err)
	}
	var doc struct {
		Issuer   string `json:"issuer"`
		AuthURL  string `json:"authorization_endpoint"`
		TokenURL string `json:"token_endpoint"`
		JWKSURL  string `json:"jwks_uri"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return Provider{}, fmt.Errorf("%w: malformed document", ErrDiscovery)
	}
	// OIDC Discovery §4.3: the returned issuer MUST match the requested one.
	if strings.TrimSuffix(doc.Issuer, "/") != issuer {
		return Provider{}, fmt.Errorf("%w: issuer mismatch: got %q want %q", ErrDiscovery, doc.Issuer, issuer)
	}
	if doc.AuthURL == "" || doc.TokenURL == "" || doc.JWKSURL == "" {
		return Provider{}, fmt.Errorf("%w: document missing endpoints", ErrDiscovery)
	}
	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = []string{"openid", "email", "profile"}
	}
	return Provider{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		AuthURL:      doc.AuthURL,
		TokenURL:     doc.TokenURL,
		JWKSURL:      doc.JWKSURL,
		Issuer:       doc.Issuer,
		RedirectURL:  cfg.RedirectURL,
		Scopes:       scopes,
		AuthParams:   cfg.AuthParams,
	}, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./auth/oauthclient/ -run TestDiscover -v`
Expected: PASS (5 tests).

- [ ] **Step 5: Format, lint, commit**

```bash
just fmt ./auth/oauthclient/...
just lint
git add auth/oauthclient
git commit -m "feat(oauthclient): OIDC discovery"
```

---

### Task 3: oauthclient Client core + AuthURL

**Files:**
- Create: `auth/oauthclient/config.go`
- Create: `auth/oauthclient/options.go`
- Create: `auth/oauthclient/client.go`
- Create: `auth/oauthclient/flow.go`
- Test: `auth/oauthclient/flow_test.go`

**Interfaces:**
- Consumes: `Provider`, `validate()`, `reservedParams`, sentinels (Task 1).
- Produces:
  - `type Client struct` (opaque) with `func New(ks *keyset.Keyset, opts ...Option) (*Client, error)` and `func FromConfig(cfg Config, opts ...Option) (*Client, error)`
  - `type Config struct { Keys string \`env:"OAUTHCLIENT_KEYS"\`; RedirectURL string \`env:"OAUTHCLIENT_REDIRECT_URL"\`; CookieName string \`env:"OAUTHCLIENT_COOKIE_NAME"\`; FlowTTL time.Duration \`env:"OAUTHCLIENT_FLOW_TTL"\` }` + `DefaultConfig() Config` (CookieName "oauth_flow", FlowTTL 10m) + `Validate() error`
  - Options: `WithProvider(name string, p Provider)`, `WithProviderSource(fn func(ctx context.Context, name string) (Provider, error))`, `WithScope(fn func(ctx context.Context) (string, error))`, `WithHTTPClient(hc *http.Client)`, `WithRedirectURL(u string)`, `WithCookieName(n string)`, `WithFlowTTL(d time.Duration)`, `WithClock(clk clock.Clock)`
  - `type Flow struct { URL, FlowToken string }`, `func (c *Client) AuthURL(ctx context.Context, provider string, opts ...BeginOption) (*Flow, error)`
  - `type BeginOption`, `func WithReturnTo(path string) BeginOption`
  - unexported: `type flowState struct { Provider, State, Verifier, Nonce, Binding, ReturnTo string }` (json tags p/s/v/n/b/r), `func pkceChallenge(verifier string) string`, `func (c *Client) resolve(ctx, name) (Provider, error)`, fields `c.hc *http.Client`, `c.codec *token.Codec[flowState]`, `c.cookieName string`, `c.flowTTL time.Duration`, `c.clk clock.Clock`, `c.binding func(...)`, `c.redirect string` — Tasks 4–5 use these.

- [ ] **Step 1: Write the failing tests**

`auth/oauthclient/flow_test.go`:

```go
package oauthclient_test

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/oauthclient"
	"github.com/dmitrymomot/forge/crypto/keyset"
)

// testKeyset takes testing.TB so Task 6's benchmarks can reuse it.
func testKeyset(tb testing.TB) *keyset.Keyset {
	tb.Helper()
	ks, err := keyset.New(keyset.WithPrimary(1, []byte("0123456789abcdef0123456789abcdef")))
	require.NoError(tb, err)
	return ks
}

func oidcProvider() oauthclient.Provider {
	return oauthclient.Provider{
		ClientID: "cid", ClientSecret: "sec",
		AuthURL:  "https://idp.example.com/authorize",
		TokenURL: "https://idp.example.com/token",
		JWKSURL:  "https://idp.example.com/jwks",
		Issuer:   "https://idp.example.com",
		Scopes:   []string{"openid", "email"},
	}
}

func TestAuthURLContents(t *testing.T) {
	c, err := oauthclient.New(testKeyset(t),
		oauthclient.WithRedirectURL("https://app.example.com/cb"),
		oauthclient.WithProvider("idp", oidcProvider()))
	require.NoError(t, err)

	flow, err := c.AuthURL(context.Background(), "idp", oauthclient.WithReturnTo("/dash"))
	require.NoError(t, err)
	require.NotEmpty(t, flow.FlowToken)

	u, err := url.Parse(flow.URL)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(flow.URL, "https://idp.example.com/authorize?"))
	q := u.Query()
	assert.Equal(t, "cid", q.Get("client_id"))
	assert.Equal(t, "https://app.example.com/cb", q.Get("redirect_uri"))
	assert.Equal(t, "code", q.Get("response_type"))
	assert.Equal(t, "openid email", q.Get("scope"))
	assert.NotEmpty(t, q.Get("state"))
	assert.NotEmpty(t, q.Get("nonce"), "OIDC provider gets a nonce")
	assert.NotEmpty(t, q.Get("code_challenge"))
	assert.Equal(t, "S256", q.Get("code_challenge_method"))
}

func TestAuthURLProviderRedirectOverridesAndAuthParams(t *testing.T) {
	p := oidcProvider()
	p.RedirectURL = "https://tenant.example.com/cb"
	p.AuthParams = map[string]string{"prompt": "select_account"}
	c, err := oauthclient.New(testKeyset(t),
		oauthclient.WithRedirectURL("https://app.example.com/cb"),
		oauthclient.WithProvider("idp", p))
	require.NoError(t, err)
	flow, err := c.AuthURL(context.Background(), "idp")
	require.NoError(t, err)
	u, _ := url.Parse(flow.URL)
	assert.Equal(t, "https://tenant.example.com/cb", u.Query().Get("redirect_uri"))
	assert.Equal(t, "select_account", u.Query().Get("prompt"))
}

func TestAuthURLNoNonceForIdentityHookProvider(t *testing.T) {
	p := oauthclient.GitHub(oauthclient.ProviderConfig{ClientID: "c", ClientSecret: "s"})
	c, err := oauthclient.New(testKeyset(t),
		oauthclient.WithRedirectURL("https://app.example.com/cb"),
		oauthclient.WithProvider("github", p))
	require.NoError(t, err)
	flow, err := c.AuthURL(context.Background(), "github")
	require.NoError(t, err)
	u, _ := url.Parse(flow.URL)
	assert.Empty(t, u.Query().Get("nonce"))
}

func TestNewRejectsReservedAuthParams(t *testing.T) {
	p := oidcProvider()
	p.AuthParams = map[string]string{"state": "evil"}
	_, err := oauthclient.New(testKeyset(t),
		oauthclient.WithRedirectURL("https://a/cb"),
		oauthclient.WithProvider("idp", p))
	require.ErrorIs(t, err, oauthclient.ErrReservedParam)
}

func TestAuthURLUnknownProvider(t *testing.T) {
	c, err := oauthclient.New(testKeyset(t), oauthclient.WithRedirectURL("https://a/cb"),
		oauthclient.WithProvider("idp", oidcProvider()))
	require.NoError(t, err)
	_, err = c.AuthURL(context.Background(), "nope")
	require.ErrorIs(t, err, oauthclient.ErrUnknownProvider)
}

func TestAuthURLProviderSource(t *testing.T) {
	c, err := oauthclient.New(testKeyset(t),
		oauthclient.WithRedirectURL("https://a/cb"),
		oauthclient.WithProviderSource(func(ctx context.Context, name string) (oauthclient.Provider, error) {
			if name == "tenant-okta" {
				return oidcProvider(), nil
			}
			return oauthclient.Provider{}, errors.New("no such tenant idp")
		}))
	require.NoError(t, err)

	_, err = c.AuthURL(context.Background(), "tenant-okta")
	require.NoError(t, err)
	_, err = c.AuthURL(context.Background(), "missing")
	require.Error(t, err, "source errors propagate (fail-closed)")
}

func TestAuthURLScopeHookFailClosed(t *testing.T) {
	c, err := oauthclient.New(testKeyset(t),
		oauthclient.WithRedirectURL("https://a/cb"),
		oauthclient.WithProvider("idp", oidcProvider()),
		oauthclient.WithScope(func(ctx context.Context) (string, error) { return "", errors.New("no tenant") }))
	require.NoError(t, err)
	_, err = c.AuthURL(context.Background(), "idp")
	require.Error(t, err)
}

func TestAuthURLNoRedirectAnywhere(t *testing.T) {
	c, err := oauthclient.New(testKeyset(t), oauthclient.WithProvider("idp", oidcProvider()))
	require.NoError(t, err)
	_, err = c.AuthURL(context.Background(), "idp")
	require.ErrorIs(t, err, oauthclient.ErrInvalidConfig)
}

func TestConfigValidate(t *testing.T) {
	cfg := oauthclient.DefaultConfig()
	require.Error(t, cfg.Validate(), "Keys required")
	cfg.Keys = "1:MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY"
	require.NoError(t, cfg.Validate())
	assert.Equal(t, "oauth_flow", cfg.CookieName)
}

func TestFromConfig(t *testing.T) {
	cfg := oauthclient.DefaultConfig()
	cfg.Keys = "1:MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY"
	cfg.RedirectURL = "https://app.example.com/cb"
	c, err := oauthclient.FromConfig(cfg, oauthclient.WithProvider("idp", oidcProvider()))
	require.NoError(t, err)
	_, err = c.AuthURL(context.Background(), "idp")
	require.NoError(t, err)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./auth/oauthclient/ -run 'TestAuthURL|TestNewRejects|TestConfig|TestFromConfig' -v`
Expected: FAIL with `undefined: oauthclient.New`.

- [ ] **Step 3: Write the implementation**

`auth/oauthclient/config.go`:

```go
package oauthclient

import (
	"fmt"
	"time"
)

// Config is the env-loadable client configuration. Key material rides Keys
// in keyset.WithBase64Keys format ("version:base64,..."). The canonical
// loading flow preserves defaults:
//
//	cfg := oauthclient.DefaultConfig()
//	err := appconfig.Populate(&cfg)
type Config struct {
	Keys        string        `env:"OAUTHCLIENT_KEYS"`
	RedirectURL string        `env:"OAUTHCLIENT_REDIRECT_URL"`
	CookieName  string        `env:"OAUTHCLIENT_COOKIE_NAME"`
	FlowTTL     time.Duration `env:"OAUTHCLIENT_FLOW_TTL"`
}

// DefaultConfig returns the default flow policy: 10-minute flows in an
// "oauth_flow" cookie.
func DefaultConfig() Config {
	return Config{CookieName: "oauth_flow", FlowTTL: 10 * time.Minute}
}

// Validate checks required fields.
func (c Config) Validate() error {
	if c.Keys == "" {
		return fmt.Errorf("%w: Keys required", ErrInvalidConfig)
	}
	if c.CookieName == "" {
		return fmt.Errorf("%w: CookieName required", ErrInvalidConfig)
	}
	if c.FlowTTL <= 0 {
		return fmt.Errorf("%w: FlowTTL must be positive", ErrInvalidConfig)
	}
	return nil
}
```

`auth/oauthclient/options.go`:

```go
package oauthclient

import (
	"context"
	"net/http"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
)

// Option configures New.
type Option func(*clientConfig)

type clientConfig struct {
	providers   map[string]Provider
	source      func(ctx context.Context, name string) (Provider, error)
	binding     func(ctx context.Context) (string, error)
	hc          *http.Client
	clk         clock.Clock
	redirectURL string
	cookieName  string
	flowTTL     time.Duration
}

// WithProvider registers a static provider under name.
func WithProvider(name string, p Provider) Option {
	return func(c *clientConfig) { c.providers[name] = p }
}

// WithProviderSource sets the dynamic provider lookup consulted when a name
// is not in the static registry — the multi-tenant seam (resolve the tenant
// from ctx inside fn). Errors propagate: resolution fails closed.
func WithProviderSource(fn func(ctx context.Context, name string) (Provider, error)) Option {
	return func(c *clientConfig) { c.source = fn }
}

// WithScope sets the tenancy binding hook. Its value is sealed into the
// flow at Begin/AuthURL and must match at Complete/Exchange
// (ErrScopeBinding otherwise); hook errors fail closed.
func WithScope(fn func(ctx context.Context) (string, error)) Option {
	return func(c *clientConfig) { c.binding = fn }
}

// WithHTTPClient sets the HTTP client used for token exchange, identity
// hooks, and JWKS fetches. Default: httpclient.New with a 15s timeout.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *clientConfig) { c.hc = hc }
}

// WithRedirectURL sets the client-wide default redirect URL; a provider's
// RedirectURL overrides it.
func WithRedirectURL(u string) Option {
	return func(c *clientConfig) { c.redirectURL = u }
}

// WithCookieName sets the flow cookie name. Default "oauth_flow".
func WithCookieName(n string) Option {
	return func(c *clientConfig) { c.cookieName = n }
}

// WithFlowTTL bounds how long a started flow stays completable. Default 10m.
func WithFlowTTL(d time.Duration) Option {
	return func(c *clientConfig) { c.flowTTL = d }
}

// WithClock overrides the time source (tests).
func WithClock(clk clock.Clock) Option {
	return func(c *clientConfig) { c.clk = clk }
}
```

`auth/oauthclient/client.go`:

```go
package oauthclient

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/dmitrymomot/forge/auth/jwt"
	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/crypto/keyset"
	"github.com/dmitrymomot/forge/crypto/token"
	"github.com/dmitrymomot/forge/web/httpclient"
)

// Client drives authorization-code + PKCE login flows against registered
// providers. It is stateless: flow state rides a sealed crypto/token blob
// (cookie or caller-held), so any instance can complete any flow.
type Client struct {
	codec      *token.Codec[flowState]
	providers  map[string]Provider
	source     func(ctx context.Context, name string) (Provider, error)
	binding    func(ctx context.Context) (string, error)
	hc         *http.Client
	clk        clock.Clock
	redirect   string
	cookieName string
	flowTTL    time.Duration
	verifiers  sync.Map // issuer\x00jwks\x00clientID -> *jwt.Verifier
}

// New builds a Client. ks signs flow tokens (rotation-aware).
func New(ks *keyset.Keyset, opts ...Option) (*Client, error) {
	cfg := clientConfig{
		providers:  map[string]Provider{},
		clk:        clock.System(),
		cookieName: "oauth_flow",
		flowTTL:    10 * time.Minute,
	}
	for _, o := range opts {
		o(&cfg)
	}
	for name, p := range cfg.providers {
		if name == "" {
			return nil, fmt.Errorf("%w: empty provider name", ErrInvalidConfig)
		}
		if err := p.validate(); err != nil {
			return nil, fmt.Errorf("provider %q: %w", name, err)
		}
	}
	codec, err := token.FromKeyset[flowState](ks,
		token.WithTTL(cfg.flowTTL),
		token.WithPurpose("oauthclient:flow"),
		token.WithClock(cfg.clk),
	)
	if err != nil {
		return nil, err
	}
	hc := cfg.hc
	if hc == nil {
		hc = httpclient.New(httpclient.WithTimeout(15 * time.Second))
	}
	return &Client{
		codec:      codec,
		providers:  cfg.providers,
		source:     cfg.source,
		binding:    cfg.binding,
		hc:         hc,
		clk:        cfg.clk,
		redirect:   cfg.redirectURL,
		cookieName: cfg.cookieName,
		flowTTL:    cfg.flowTTL,
	}, nil
}

// FromConfig builds a Client from an env-loaded Config.
func FromConfig(cfg Config, opts ...Option) (*Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	ks, err := keyset.New(keyset.WithBase64Keys(cfg.Keys))
	if err != nil {
		return nil, fmt.Errorf("%w: keys: %v", ErrInvalidConfig, err)
	}
	base := []Option{
		WithRedirectURL(cfg.RedirectURL),
		WithCookieName(cfg.CookieName),
		WithFlowTTL(cfg.FlowTTL),
	}
	return New(ks, append(base, opts...)...)
}

// resolve finds a provider: static registry first, then the source.
func (c *Client) resolve(ctx context.Context, name string) (Provider, error) {
	if p, ok := c.providers[name]; ok {
		return p, nil
	}
	if c.source != nil {
		p, err := c.source(ctx, name)
		if err != nil {
			return Provider{}, fmt.Errorf("oauthclient: provider source: %w", err)
		}
		if err := p.validate(); err != nil {
			return Provider{}, err
		}
		return p, nil
	}
	return Provider{}, fmt.Errorf("%w: %q", ErrUnknownProvider, name)
}

// verifierFor returns a cached alg-pinned verifier for p's id_tokens.
func (c *Client) verifierFor(p Provider) (*jwt.Verifier, error) {
	key := p.Issuer + "\x00" + p.JWKSURL + "\x00" + p.ClientID
	if v, ok := c.verifiers.Load(key); ok {
		return v.(*jwt.Verifier), nil
	}
	v, err := jwt.NewVerifier(
		jwt.WithJWKSURL(p.JWKSURL, jwt.WithHTTPClient(c.hc)),
		jwt.WithIssuer(p.Issuer),
		jwt.WithAudience(p.ClientID),
		jwt.WithClock(c.clk),
	)
	if err != nil {
		return nil, err
	}
	actual, _ := c.verifiers.LoadOrStore(key, v)
	return actual.(*jwt.Verifier), nil
}
```

`auth/oauthclient/flow.go`:

```go
package oauthclient

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"

	"github.com/dmitrymomot/forge/core/random"
	"github.com/dmitrymomot/forge/crypto/digest"
)

// flowState is the sealed per-flow blob: everything Complete/Exchange needs
// to finish a flow started by Begin/AuthURL.
type flowState struct {
	Provider string `json:"p"`
	State    string `json:"s"`
	Verifier string `json:"v"`
	Nonce    string `json:"n,omitempty"`
	Binding  string `json:"b,omitempty"`
	ReturnTo string `json:"r,omitempty"`
}

// Flow is a started login flow: the provider URL to send the user to and
// the sealed flow token the caller must present back to Exchange. Begin
// handles both via a cookie; SPA/mobile callers carry FlowToken themselves.
type Flow struct {
	URL       string
	FlowToken string
}

// BeginOption configures one Begin/AuthURL call.
type BeginOption func(*beginConfig)

type beginConfig struct {
	returnTo string
}

// WithReturnTo round-trips a post-login destination through the flow; it
// comes back verbatim in Result.ReturnTo.
func WithReturnTo(path string) BeginOption {
	return func(c *beginConfig) { c.returnTo = path }
}

// AuthURL starts a flow: resolves the provider, generates state + PKCE
// verifier (+ OIDC nonce), seals them into a flow token, and returns the
// provider authorize URL. Transport-neutral core under Begin.
func (c *Client) AuthURL(ctx context.Context, provider string, opts ...BeginOption) (*Flow, error) {
	p, err := c.resolve(ctx, provider)
	if err != nil {
		return nil, err
	}
	var bc beginConfig
	for _, o := range opts {
		o(&bc)
	}
	redirect := p.RedirectURL
	if redirect == "" {
		redirect = c.redirect
	}
	if redirect == "" {
		return nil, fmt.Errorf("%w: no redirect URL for provider %q", ErrInvalidConfig, provider)
	}
	fs := flowState{
		Provider: provider,
		State:    random.URLSafe(32),
		Verifier: random.URLSafe(32), // 43 chars encoded — within RFC 7636's 43..128
		ReturnTo: bc.returnTo,
	}
	if p.Identity == nil {
		fs.Nonce = random.URLSafe(16)
	}
	if c.binding != nil {
		b, err := c.binding(ctx)
		if err != nil {
			return nil, fmt.Errorf("oauthclient: scope hook: %w", err)
		}
		fs.Binding = b
	}
	tok, err := c.codec.Issue(fs)
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	for k, v := range p.AuthParams {
		if reservedParams[k] {
			return nil, fmt.Errorf("%w: %q", ErrReservedParam, k)
		}
		q.Set(k, v)
	}
	q.Set("client_id", p.ClientID)
	q.Set("redirect_uri", redirect)
	q.Set("response_type", "code")
	q.Set("scope", strings.Join(p.Scopes, " "))
	q.Set("state", fs.State)
	if fs.Nonce != "" {
		q.Set("nonce", fs.Nonce)
	}
	q.Set("code_challenge", pkceChallenge(fs.Verifier))
	q.Set("code_challenge_method", "S256")
	return &Flow{URL: p.AuthURL + "?" + q.Encode(), FlowToken: tok}, nil
}

// pkceChallenge is the RFC 7636 S256 transform.
func pkceChallenge(verifier string) string {
	return base64.RawURLEncoding.EncodeToString(digest.SHA256([]byte(verifier)))
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./auth/oauthclient/...`
Expected: PASS (Tasks 1–3 tests).

- [ ] **Step 5: Format, lint, commit**

```bash
just fmt ./auth/oauthclient/...
just lint
git add auth/oauthclient
git commit -m "feat(oauthclient): client core, sealed flow state, AuthURL"
```

---

### Task 4: oauthclient Exchange (token exchange + identity verification)

**Files:**
- Create: `auth/oauthclient/exchange.go`
- Test: `auth/oauthclient/exchange_test.go`, `auth/oauthclient/helpers_test.go`

**Interfaces:**
- Consumes: everything from Tasks 1–3 (`flowState`, `pkceChallenge`, `resolve`, `verifierFor`, `c.codec`, `c.hc`, `c.binding`, `c.redirect`, sentinels, `Identity`, `TokenResponse`, `ProviderError`).
- Produces:
  - `type Result struct { Identity Identity; Token TokenResponse; Provider, ReturnTo string }`
  - `func (c *Client) Exchange(ctx context.Context, flowToken string, callback url.Values) (*Result, error)`
  - Test helper `signIDToken(t, signerKS, claims map[string]any) string` and `fakeOIDC` server used again in Task 5.

- [ ] **Step 1: Write the failing tests**

`auth/oauthclient/helpers_test.go` (shared by Tasks 4–6 tests):

```go
package oauthclient_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/jwt"
	"github.com/dmitrymomot/forge/auth/oauthclient"
	"github.com/dmitrymomot/forge/crypto/keyset"
)

// idpSigner builds an Ed25519 jwt.Signer for minting fake id_tokens.
func idpSigner(t *testing.T) *jwt.Signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	require.NoError(t, err)
	ks, err := keyset.New(keyset.WithPrimary(1, der))
	require.NoError(t, err)
	s, err := jwt.NewSigner(jwt.WithKeyset(ks))
	require.NoError(t, err)
	return s
}

// fakeOIDC is an httptest IdP: /token returns tokenResp (with a freshly
// signed id_token when mintIDToken is set), /jwks serves the signer's keys.
type fakeOIDC struct {
	Server    *httptest.Server
	Signer    *jwt.Signer
	TokenForm url.Values // captured form of the last /token POST
	// IDTokenClaims lets a test override claims minted into the id_token.
	// Keys iss/aud/exp are filled with valid values unless already set.
	IDTokenClaims map[string]any
	TokenStatus   int
	TokenBody     map[string]any // non-nil overrides the default token response
}

func newFakeOIDC(t *testing.T) *fakeOIDC {
	t.Helper()
	f := &fakeOIDC{Signer: idpSigner(t), TokenStatus: http.StatusOK, IDTokenClaims: map[string]any{}}
	mux := http.NewServeMux()
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Server.Close)
	mux.Handle("GET /jwks", f.Signer.JWKS())
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		f.TokenForm = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		if f.TokenStatus != http.StatusOK {
			w.WriteHeader(f.TokenStatus)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid_grant", "error_description": "bad code"})
			return
		}
		body := f.TokenBody
		if body == nil {
			claims := map[string]any{
				"iss": f.Server.URL, "aud": "cid", "sub": "user-1",
				"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
				"email": "u@example.com", "email_verified": true,
				"name": "User One", "picture": "https://img.example.com/u.png",
			}
			for k, v := range f.IDTokenClaims {
				claims[k] = v
			}
			idt, err := f.Signer.Sign(claims)
			require.NoError(t, err)
			body = map[string]any{
				"access_token": "at-123", "token_type": "Bearer",
				"expires_in": 3600, "id_token": idt, "scope": "openid email",
			}
		}
		_ = json.NewEncoder(w).Encode(body)
	})
	return f
}

// provider returns a Provider pointed at the fake IdP.
func (f *fakeOIDC) provider() oauthclient.Provider {
	return oauthclient.Provider{
		ClientID: "cid", ClientSecret: "sec",
		AuthURL:  f.Server.URL + "/authorize",
		TokenURL: f.Server.URL + "/token",
		JWKSURL:  f.Server.URL + "/jwks",
		Issuer:   f.Server.URL,
		Scopes:   []string{"openid", "email"},
	}
}

// newClient builds an oauthclient against the fake IdP.
func (f *fakeOIDC) newClient(t *testing.T, opts ...oauthclient.Option) *oauthclient.Client {
	t.Helper()
	base := []oauthclient.Option{
		oauthclient.WithRedirectURL("https://app.example.com/cb"),
		oauthclient.WithProvider("idp", f.provider()),
		oauthclient.WithHTTPClient(f.Server.Client()),
	}
	c, err := oauthclient.New(testKeyset(t), append(base, opts...)...)
	require.NoError(t, err)
	return c
}

// startFlow runs AuthURL and returns the flow plus the authorize-URL query
// (which carries state/nonce the callback must echo).
func startFlow(t *testing.T, c *oauthclient.Client, opts ...oauthclient.BeginOption) (*oauthclient.Flow, url.Values) {
	t.Helper()
	flow, err := c.AuthURL(t.Context(), "idp", opts...)
	require.NoError(t, err)
	u, err := url.Parse(flow.URL)
	require.NoError(t, err)
	return flow, u.Query()
}

// callbackQuery fabricates the provider redirect query for a started flow.
func callbackQuery(authQ url.Values, code string) url.Values {
	return url.Values{"code": {code}, "state": {authQ.Get("state")}}
}

// withNonce makes the fake mint the nonce the started flow expects: the
// authorize query carries it, and f.IDTokenClaims overrides flow into the
// minted id_token. Tests that skip it exercise the nonce-mismatch path.
func withNonce(f *fakeOIDC, authQ url.Values) {
	f.IDTokenClaims["nonce"] = authQ.Get("nonce")
}
```

`auth/oauthclient/exchange_test.go`:

```go
package oauthclient_test

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/oauthclient"
	"github.com/dmitrymomot/forge/core/clock"
)

func TestExchangeOIDCHappyPath(t *testing.T) {
	f := newFakeOIDC(t)
	c := f.newClient(t)
	flow, authQ := startFlow(t, c, oauthclient.WithReturnTo("/dash"))
	withNonce(f, authQ)

	res, err := c.Exchange(context.Background(), flow.FlowToken, callbackQuery(authQ, "code-1"))
	require.NoError(t, err)
	assert.Equal(t, "idp", res.Provider)
	assert.Equal(t, "/dash", res.ReturnTo)
	assert.Equal(t, "user-1", res.Identity.Subject)
	assert.Equal(t, "u@example.com", res.Identity.Email)
	assert.True(t, res.Identity.EmailVerified)
	assert.Equal(t, "User One", res.Identity.Name)
	assert.Equal(t, "idp", res.Identity.Provider)
	assert.Equal(t, "at-123", res.Token.AccessToken)
	assert.False(t, res.Token.ExpiresAt.IsZero())
	assert.NotEmpty(t, res.Identity.Raw["iss"], "raw claims exposed")

	// the exchange POST carried the PKCE verifier and the code
	assert.Equal(t, "authorization_code", f.TokenForm.Get("grant_type"))
	assert.Equal(t, "code-1", f.TokenForm.Get("code"))
	assert.NotEmpty(t, f.TokenForm.Get("code_verifier"))
	assert.Equal(t, "https://app.example.com/cb", f.TokenForm.Get("redirect_uri"))
	assert.Equal(t, "cid", f.TokenForm.Get("client_id"))
	assert.Equal(t, "sec", f.TokenForm.Get("client_secret"))
}

func TestExchangeStateMismatch(t *testing.T) {
	f := newFakeOIDC(t)
	c := f.newClient(t)
	flow, authQ := startFlow(t, c)
	withNonce(f, authQ)
	cb := callbackQuery(authQ, "code-1")
	cb.Set("state", "forged")
	_, err := c.Exchange(context.Background(), flow.FlowToken, cb)
	require.ErrorIs(t, err, oauthclient.ErrStateMismatch)
}

func TestExchangeProviderErrorCallback(t *testing.T) {
	f := newFakeOIDC(t)
	c := f.newClient(t)
	flow, _ := startFlow(t, c)
	cb := url.Values{"error": {"access_denied"}, "error_description": {"user cancelled"}}
	_, err := c.Exchange(context.Background(), flow.FlowToken, cb)
	var perr *oauthclient.ProviderError
	require.ErrorAs(t, err, &perr)
	assert.Equal(t, "access_denied", perr.Code)
}

func TestExchangeExpiredFlow(t *testing.T) {
	f := newFakeOIDC(t)
	mock := clock.NewMock(time.Now())
	c := f.newClient(t, oauthclient.WithClock(mock))
	flow, authQ := startFlow(t, c)
	mock.Advance(11 * time.Minute)
	_, err := c.Exchange(context.Background(), flow.FlowToken, callbackQuery(authQ, "code-1"))
	require.ErrorIs(t, err, oauthclient.ErrFlowExpired)
}

func TestExchangeTamperedFlowToken(t *testing.T) {
	f := newFakeOIDC(t)
	c := f.newClient(t)
	_, authQ := startFlow(t, c)
	_, err := c.Exchange(context.Background(), "garbage.token", callbackQuery(authQ, "code-1"))
	require.ErrorIs(t, err, oauthclient.ErrFlowExpired)
}

func TestExchangeNonceMismatch(t *testing.T) {
	f := newFakeOIDC(t)
	c := f.newClient(t)
	flow, authQ := startFlow(t, c)
	f.IDTokenClaims["nonce"] = "wrong-nonce"
	_, err := c.Exchange(context.Background(), flow.FlowToken, callbackQuery(authQ, "code-1"))
	require.ErrorIs(t, err, oauthclient.ErrNonceMismatch)
}

func TestExchangeTokenEndpointRFCError(t *testing.T) {
	f := newFakeOIDC(t)
	c := f.newClient(t)
	flow, authQ := startFlow(t, c)
	f.TokenStatus = 400
	_, err := c.Exchange(context.Background(), flow.FlowToken, callbackQuery(authQ, "bad"))
	var perr *oauthclient.ProviderError
	require.ErrorAs(t, err, &perr)
	assert.Equal(t, "invalid_grant", perr.Code)
}

func TestExchangeMissingCode(t *testing.T) {
	f := newFakeOIDC(t)
	c := f.newClient(t)
	flow, authQ := startFlow(t, c)
	cb := url.Values{"state": {authQ.Get("state")}}
	_, err := c.Exchange(context.Background(), flow.FlowToken, cb)
	var perr *oauthclient.ProviderError
	require.ErrorAs(t, err, &perr)
	assert.Equal(t, "invalid_response", perr.Code)
}

func TestExchangeScopeBinding(t *testing.T) {
	f := newFakeOIDC(t)
	tenant := "tenant-a"
	hook := func(ctx context.Context) (string, error) { return tenant, nil }
	c := f.newClient(t, oauthclient.WithScope(hook))
	flow, authQ := startFlow(t, c)
	withNonce(f, authQ)

	tenant = "tenant-b" // flow finishes under a different tenant
	_, err := c.Exchange(context.Background(), flow.FlowToken, callbackQuery(authQ, "code-1"))
	require.ErrorIs(t, err, oauthclient.ErrScopeBinding)

	tenant = "tenant-a"
	_, err = c.Exchange(context.Background(), flow.FlowToken, callbackQuery(authQ, "code-1"))
	require.NoError(t, err)
}

func TestExchangeScopeHookErrorFailsClosed(t *testing.T) {
	f := newFakeOIDC(t)
	fail := false
	c := f.newClient(t, oauthclient.WithScope(func(ctx context.Context) (string, error) {
		if fail {
			return "", errors.New("boom")
		}
		return "t", nil
	}))
	flow, authQ := startFlow(t, c)
	fail = true
	_, err := c.Exchange(context.Background(), flow.FlowToken, callbackQuery(authQ, "code-1"))
	require.Error(t, err)
	require.NotErrorIs(t, err, oauthclient.ErrScopeBinding, "hook error is not a binding mismatch")
}

func TestExchangeIdentityHookPath(t *testing.T) {
	// GitHub-shaped: no id_token; identity from the hook.
	gh := fakeGitHub(t, 200)
	f := newFakeOIDC(t)
	f.TokenBody = map[string]any{"access_token": "gho_token", "token_type": "bearer", "scope": "read:user"}
	p := f.provider()
	p.Issuer, p.JWKSURL = "", "" // not OIDC
	p.Identity = oauthclient.GitHubIdentity(gh.URL)
	c, err := oauthclient.New(testKeyset(t),
		oauthclient.WithRedirectURL("https://app.example.com/cb"),
		oauthclient.WithProvider("idp", p),
		oauthclient.WithHTTPClient(f.Server.Client()))
	require.NoError(t, err)

	flow, authQ := startFlow(t, c)
	res, err := c.Exchange(context.Background(), flow.FlowToken, callbackQuery(authQ, "code-1"))
	require.NoError(t, err)
	assert.Equal(t, "12345", res.Identity.Subject)
	assert.Equal(t, "idp", res.Identity.Provider, "provider name comes from the registry key")
	assert.Empty(t, res.Token.IDToken)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./auth/oauthclient/ -run TestExchange -v`
Expected: FAIL with `undefined: c.Exchange` / `undefined: oauthclient.Result`.

- [ ] **Step 3: Write the implementation**

`auth/oauthclient/exchange.go`:

```go
package oauthclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dmitrymomot/forge/auth/jwt"
	"github.com/dmitrymomot/forge/crypto/consttime"
	"github.com/dmitrymomot/forge/crypto/token"
)

// Result is a completed login.
type Result struct {
	Identity Identity
	// Token is the raw provider token response, exposed once. Persisting it
	// for later provider-API access is consumer domain.
	Token    TokenResponse
	Provider string
	ReturnTo string
}

// Exchange completes a flow: validates state (and tenancy binding) against
// the sealed flow token, exchanges the code with PKCE, and verifies the
// identity (id_token or Identity hook). callback is the full query the
// provider redirected back with.
func (c *Client) Exchange(ctx context.Context, flowToken string, callback url.Values) (*Result, error) {
	fs, err := c.codec.Parse(flowToken)
	if err != nil {
		if errors.Is(err, token.ErrExpired) {
			return nil, ErrFlowExpired
		}
		return nil, fmt.Errorf("%w: %v", ErrFlowExpired, err)
	}
	if ec := callback.Get("error"); ec != "" {
		return nil, &ProviderError{Code: ec, Description: callback.Get("error_description")}
	}
	if st := callback.Get("state"); st == "" || !consttime.StringEqual(st, fs.State) {
		return nil, ErrStateMismatch
	}
	if c.binding != nil {
		b, err := c.binding(ctx)
		if err != nil {
			return nil, fmt.Errorf("oauthclient: scope hook: %w", err)
		}
		if b != fs.Binding {
			return nil, ErrScopeBinding
		}
	} else if fs.Binding != "" {
		return nil, ErrScopeBinding
	}
	p, err := c.resolve(ctx, fs.Provider)
	if err != nil {
		return nil, err
	}
	code := callback.Get("code")
	if code == "" {
		return nil, &ProviderError{Code: "invalid_response", Description: "callback missing code"}
	}
	redirect := p.RedirectURL
	if redirect == "" {
		redirect = c.redirect
	}
	tok, err := c.exchangeCode(ctx, p, code, fs.Verifier, redirect)
	if err != nil {
		return nil, err
	}
	var ident Identity
	if p.Identity != nil {
		ident, err = p.Identity(ctx, c.hc, tok)
	} else {
		ident, err = c.verifyIDToken(ctx, p, tok, fs.Nonce)
	}
	if err != nil {
		return nil, err
	}
	ident.Provider = fs.Provider
	return &Result{Identity: ident, Token: tok, Provider: fs.Provider, ReturnTo: fs.ReturnTo}, nil
}

// exchangeCode POSTs the authorization code to the provider token endpoint.
func (c *Client) exchangeCode(ctx context.Context, p Provider, code, verifier, redirect string) (TokenResponse, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirect},
		"client_id":     {p.ClientID},
		"client_secret": {p.ClientSecret},
		"code_verifier": {verifier},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return TokenResponse{}, fmt.Errorf("oauthclient: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json") // GitHub answers urlencoded without it
	resp, err := c.hc.Do(req)
	if err != nil {
		return TokenResponse{}, fmt.Errorf("oauthclient: token exchange: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only body
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return TokenResponse{}, fmt.Errorf("oauthclient: token exchange: %w", err)
	}
	var raw struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int64  `json:"expires_in"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
		Scope        string `json:"scope"`
		Error        string `json:"error"`
		ErrorDesc    string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return TokenResponse{}, &ProviderError{Code: "invalid_response", Description: resp.Status}
	}
	if raw.Error != "" {
		return TokenResponse{}, &ProviderError{Code: raw.Error, Description: raw.ErrorDesc}
	}
	if resp.StatusCode != http.StatusOK || raw.AccessToken == "" {
		return TokenResponse{}, &ProviderError{Code: "invalid_response", Description: resp.Status}
	}
	tr := TokenResponse{
		AccessToken:  raw.AccessToken,
		TokenType:    raw.TokenType,
		RefreshToken: raw.RefreshToken,
		IDToken:      raw.IDToken,
		Scope:        raw.Scope,
	}
	if raw.ExpiresIn > 0 {
		tr.ExpiresAt = c.clk.Now().Add(time.Duration(raw.ExpiresIn) * time.Second)
	}
	return tr, nil
}

// verifyIDToken checks the id_token (signature via provider JWKS, iss, aud,
// exp inside jwt.Verify; nonce here) and maps claims to Identity.
func (c *Client) verifyIDToken(ctx context.Context, p Provider, tok TokenResponse, nonce string) (Identity, error) {
	if tok.IDToken == "" {
		return Identity{}, ErrNoIdentity
	}
	v, err := c.verifierFor(p)
	if err != nil {
		return Identity{}, err
	}
	claims, err := jwt.Verify[map[string]any](ctx, v, tok.IDToken)
	if err != nil {
		return Identity{}, err
	}
	raw := *claims
	got := str(raw["nonce"])
	if nonce == "" || !consttime.StringEqual(got, nonce) {
		return Identity{}, ErrNonceMismatch
	}
	ident := Identity{
		Subject:       str(raw["sub"]),
		Email:         str(raw["email"]),
		EmailVerified: boolClaim(raw["email_verified"]),
		Name:          str(raw["name"]),
		Picture:       str(raw["picture"]),
		Raw:           raw,
	}
	if ident.Subject == "" {
		return Identity{}, ErrNoIdentity
	}
	return ident, nil
}

// boolClaim tolerates IdPs that encode email_verified as a string.
func boolClaim(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return t == "true"
	}
	return false
}
```

Note: `helpers_test.go` imports `assert` only for `fakeGitHub` assertions living in `github_test.go` — if goimports flags an unused `assert` import in helpers_test.go, drop it there (each test file imports what it uses).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race ./auth/oauthclient/...`
Expected: PASS.

- [ ] **Step 5: Format, lint, commit**

```bash
just fmt ./auth/oauthclient/...
just lint
git add auth/oauthclient
git commit -m "feat(oauthclient): code exchange with PKCE and id_token/hook identity verification"
```

---

### Task 5: oauthclient cookie transport (Begin / Complete)

**Files:**
- Create: `auth/oauthclient/cookie.go`
- Test: `auth/oauthclient/cookie_test.go`

**Interfaces:**
- Consumes: `AuthURL`, `Exchange`, `c.cookieName`, `c.flowTTL` (Tasks 3–4); `fakeOIDC` helpers (Task 4).
- Produces:
  - `func (c *Client) Begin(w http.ResponseWriter, r *http.Request, provider string, opts ...BeginOption) error`
  - `func (c *Client) Complete(w http.ResponseWriter, r *http.Request) (*Result, error)`

- [ ] **Step 1: Write the failing tests**

`auth/oauthclient/cookie_test.go`:

```go
package oauthclient_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/oauthclient"
)

func TestBeginSetsCookieAndRedirects(t *testing.T) {
	f := newFakeOIDC(t)
	c := f.newClient(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "https://app.example.com/auth/idp", nil)
	require.NoError(t, c.Begin(rec, req, "idp", oauthclient.WithReturnTo("/dash")))

	assert.Equal(t, http.StatusSeeOther, rec.Code)
	loc, err := url.Parse(rec.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "/authorize", loc.Path)

	cookies := rec.Result().Cookies()
	require.Len(t, cookies, 1)
	ck := cookies[0]
	assert.Equal(t, "oauth_flow", ck.Name)
	assert.NotEmpty(t, ck.Value)
	assert.True(t, ck.HttpOnly)
	assert.True(t, ck.Secure)
	assert.Equal(t, http.SameSiteLaxMode, ck.SameSite)
	assert.Equal(t, "/", ck.Path)
	assert.Equal(t, 600, ck.MaxAge)
}

func TestCompleteHappyPath(t *testing.T) {
	f := newFakeOIDC(t)
	c := f.newClient(t)

	// Begin
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "https://app.example.com/auth/idp", nil)
	require.NoError(t, c.Begin(rec, req, "idp"))
	flowCookie := rec.Result().Cookies()[0]
	loc, _ := url.Parse(rec.Header().Get("Location"))
	withNonce(f, loc.Query())

	// Callback
	cbURL := "https://app.example.com/cb?code=code-1&state=" + url.QueryEscape(loc.Query().Get("state"))
	cbReq := httptest.NewRequest(http.MethodGet, cbURL, nil)
	cbReq.AddCookie(flowCookie)
	cbRec := httptest.NewRecorder()

	res, err := c.Complete(cbRec, cbReq)
	require.NoError(t, err)
	assert.Equal(t, "user-1", res.Identity.Subject)

	// flow cookie is cleared
	cleared := cbRec.Result().Cookies()
	require.Len(t, cleared, 1)
	assert.Equal(t, "oauth_flow", cleared[0].Name)
	assert.Equal(t, -1, cleared[0].MaxAge)
}

func TestCompleteWithoutCookie(t *testing.T) {
	f := newFakeOIDC(t)
	c := f.newClient(t)
	req := httptest.NewRequest(http.MethodGet, "https://app.example.com/cb?code=x&state=y", nil)
	_, err := c.Complete(httptest.NewRecorder(), req)
	require.ErrorIs(t, err, oauthclient.ErrFlowExpired)
}

func TestBeginCustomCookieName(t *testing.T) {
	f := newFakeOIDC(t)
	c := f.newClient(t, oauthclient.WithCookieName("my_flow"))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "https://app.example.com/auth/idp", nil)
	require.NoError(t, c.Begin(rec, req, "idp"))
	assert.Equal(t, "my_flow", rec.Result().Cookies()[0].Name)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./auth/oauthclient/ -run 'TestBegin|TestComplete' -v`
Expected: FAIL with `undefined: c.Begin`.

- [ ] **Step 3: Write the implementation**

`auth/oauthclient/cookie.go`:

```go
package oauthclient

import (
	"net/http"
	"time"
)

// Begin starts a login flow and sends the user to the provider: AuthURL +
// flow cookie + 303 redirect. The cookie is HttpOnly, Secure, SameSite=Lax
// (Lax survives the top-level redirect back from the provider).
func (c *Client) Begin(w http.ResponseWriter, r *http.Request, provider string, opts ...BeginOption) error {
	flow, err := c.AuthURL(r.Context(), provider, opts...)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     c.cookieName,
		Value:    flow.FlowToken,
		Path:     "/",
		MaxAge:   int(c.flowTTL / time.Second),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, flow.URL, http.StatusSeeOther)
	return nil
}

// Complete finishes the flow on the callback request: reads and clears the
// flow cookie, then runs Exchange with the callback query.
func (c *Client) Complete(w http.ResponseWriter, r *http.Request) (*Result, error) {
	ck, err := r.Cookie(c.cookieName)
	if err != nil {
		return nil, ErrFlowExpired
	}
	http.SetCookie(w, &http.Cookie{
		Name:     c.cookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
	return c.Exchange(r.Context(), ck.Value, r.URL.Query())
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race ./auth/oauthclient/...`
Expected: PASS.

- [ ] **Step 5: Format, lint, commit**

```bash
just fmt ./auth/oauthclient/...
just lint
git add auth/oauthclient
git commit -m "feat(oauthclient): Begin/Complete cookie transport"
```

---

### Task 6: oauthclient doc.go + benchmarks

**Files:**
- Create: `auth/oauthclient/doc.go`
- Create: `auth/oauthclient/bench_test.go`

**Interfaces:**
- Consumes: the whole public API (Tasks 1–5).
- Produces: nothing new — documentation and benchmark baseline.

- [ ] **Step 1: Write doc.go**

```go
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
// is sealed at Begin and must match at Complete — fail-closed).
//
// Forge's own oauthserver is just another provider: hand-build
// Provider{AuthURL, TokenURL, JWKSURL, Issuer, ClientID, ClientSecret,
// Scopes} pointing at its endpoints; see auth/oauthserver.
package oauthclient
```

- [ ] **Step 2: Write benchmarks**

`auth/oauthclient/bench_test.go`:

```go
package oauthclient_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/oauthclient"
)

func BenchmarkAuthURL(b *testing.B) {
	c, err := oauthclient.New(testKeyset(b),
		oauthclient.WithRedirectURL("https://app.example.com/cb"),
		oauthclient.WithProvider("idp", oidcProvider()))
	require.NoError(b, err)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := c.AuthURL(ctx, "idp"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFlowRoundTrip(b *testing.B) {
	// Seal + parse cost of the flow blob: AuthURL then Exchange up to the
	// state check (the exchange fails on the fabricated callback state —
	// that's fine; the sealed-blob parse is the hot part being measured).
	c, err := oauthclient.New(testKeyset(b),
		oauthclient.WithRedirectURL("https://app.example.com/cb"),
		oauthclient.WithProvider("idp", oidcProvider()))
	require.NoError(b, err)
	ctx := context.Background()
	flow, err := c.AuthURL(ctx, "idp")
	require.NoError(b, err)
	cb := url.Values{"code": {"c"}, "state": {"wrong"}}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = c.Exchange(ctx, flow.FlowToken, cb)
	}
}
```

(`testKeyset` already takes `testing.TB` — flow_test.go, Task 3 — so both benchmarks call it with `b` directly; add `net/url` to the bench imports.)

- [ ] **Step 3: Run benchmarks and record the baseline**

Run: `just bench ./auth/oauthclient/`
Expected: both benchmarks run without failures. Record ns/op and allocs/op in the commit message. If AuthURL allocates egregiously (> ~40 allocs/op), inspect with `-benchmem` and apply the obvious fixes (preallocate `url.Values`, `strings.Builder` for the query) — but only with the benchmark numbers proving the win.

- [ ] **Step 4: Full package test + lint**

Run: `just test ./auth/oauthclient/ && just lint`
Expected: PASS, no lint findings.

- [ ] **Step 5: Commit**

```bash
just fmt ./auth/oauthclient/...
git add auth/oauthclient
git commit -m "docs(oauthclient): package docs and benchmarks"
```

---

### Task 7: oauthserver registry (Client record, Store, memory store)

**Files:**
- Create: `auth/oauthserver/errors.go`
- Create: `auth/oauthserver/client.go`
- Create: `auth/oauthserver/store.go`
- Create: `auth/oauthserver/memstore.go`
- Test: `auth/oauthserver/memstore_test.go`, `auth/oauthserver/client_test.go`

**Interfaces:**
- Consumes: nothing in-repo yet.
- Produces (later tasks and pgstore rely on exact shapes):
  - `const GrantClientCredentials = "client_credentials"`, `const GrantAuthorizationCode = "authorization_code"`
  - `type Client struct { ID, Name string; SecretHash []byte; Scopes, Grants, RedirectURIs []string; TenantID string; TokenTTL time.Duration; RevokedAt, CreatedAt time.Time }` with methods `Revoked() bool`, `AllowsGrant(g string) bool`, `AllowsRedirect(uri string) bool`, `AllowsScopes(requested []string) bool`
  - `type Store interface { Create(ctx context.Context, c Client) error; Get(ctx context.Context, id string) (Client, error); Update(ctx context.Context, c Client) error; List(ctx context.Context, tenantID string) ([]Client, error); Delete(ctx context.Context, id string) error }` — Create returns ErrDuplicateClient on ID collision; Get/Update return ErrClientNotFound; List("") returns all clients.
  - `func NewMemoryStore() Store`
  - Sentinels: `ErrClientNotFound, ErrClientRevoked, ErrDuplicateClient, ErrInvalidConfig, ErrInvalidInput`

- [ ] **Step 1: Write the failing tests**

`auth/oauthserver/client_test.go`:

```go
package oauthserver_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/auth/oauthserver"
)

func TestClientHelpers(t *testing.T) {
	c := oauthserver.Client{
		Grants:       []string{oauthserver.GrantClientCredentials},
		Scopes:       []string{"read:odds", "write:bets"},
		RedirectURIs: []string{"https://m1.example.com/cb"},
	}
	assert.True(t, c.AllowsGrant("client_credentials"))
	assert.False(t, c.AllowsGrant("authorization_code"))
	assert.True(t, c.AllowsRedirect("https://m1.example.com/cb"))
	assert.False(t, c.AllowsRedirect("https://m1.example.com/cb/"), "exact match only")
	assert.True(t, c.AllowsScopes([]string{"read:odds"}))
	assert.True(t, c.AllowsScopes(nil), "empty request is always a subset")
	assert.False(t, c.AllowsScopes([]string{"read:odds", "admin"}))
	assert.False(t, c.Revoked())
}
```

`auth/oauthserver/memstore_test.go`:

```go
package oauthserver_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/oauthserver"
)

func TestMemoryStoreCRUD(t *testing.T) {
	s := oauthserver.NewMemoryStore()
	ctx := context.Background()
	c := oauthserver.Client{
		ID: "client_1", Name: "partner", SecretHash: []byte{1, 2},
		Scopes: []string{"read"}, Grants: []string{oauthserver.GrantClientCredentials},
		TenantID: "t1", CreatedAt: time.Now().UTC(),
	}
	require.NoError(t, s.Create(ctx, c))
	require.ErrorIs(t, s.Create(ctx, c), oauthserver.ErrDuplicateClient)

	got, err := s.Get(ctx, "client_1")
	require.NoError(t, err)
	assert.Equal(t, "partner", got.Name)

	_, err = s.Get(ctx, "nope")
	require.ErrorIs(t, err, oauthserver.ErrClientNotFound)

	got.Name = "renamed"
	require.NoError(t, s.Update(ctx, got))
	got2, _ := s.Get(ctx, "client_1")
	assert.Equal(t, "renamed", got2.Name)

	require.ErrorIs(t, s.Update(ctx, oauthserver.Client{ID: "nope"}), oauthserver.ErrClientNotFound)

	require.NoError(t, s.Delete(ctx, "client_1"))
	_, err = s.Get(ctx, "client_1")
	require.ErrorIs(t, err, oauthserver.ErrClientNotFound)
}

func TestMemoryStoreListTenantFilter(t *testing.T) {
	s := oauthserver.NewMemoryStore()
	ctx := context.Background()
	require.NoError(t, s.Create(ctx, oauthserver.Client{ID: "a", TenantID: "t1"}))
	require.NoError(t, s.Create(ctx, oauthserver.Client{ID: "b", TenantID: "t2"}))
	require.NoError(t, s.Create(ctx, oauthserver.Client{ID: "c"}))

	all, err := s.List(ctx, "")
	require.NoError(t, err)
	assert.Len(t, all, 3)

	t1, err := s.List(ctx, "t1")
	require.NoError(t, err)
	require.Len(t, t1, 1)
	assert.Equal(t, "a", t1[0].ID)
}

func TestMemoryStoreReturnsCopies(t *testing.T) {
	s := oauthserver.NewMemoryStore()
	ctx := context.Background()
	require.NoError(t, s.Create(ctx, oauthserver.Client{ID: "a", Scopes: []string{"read"}}))
	got, _ := s.Get(ctx, "a")
	got.Scopes[0] = "mutated"
	fresh, _ := s.Get(ctx, "a")
	assert.Equal(t, "read", fresh.Scopes[0], "stored record must not alias returned slices")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./auth/oauthserver/...`
Expected: FAIL — package does not exist.

- [ ] **Step 3: Write the implementation**

`auth/oauthserver/errors.go`:

```go
package oauthserver

import "errors"

var (
	// ErrClientNotFound is returned when a client id is unknown — including
	// when a tenancy-scoped call targets another tenant's client.
	ErrClientNotFound = errors.New("oauthserver: client not found")
	// ErrClientRevoked is returned by management calls on a revoked client.
	ErrClientRevoked = errors.New("oauthserver: client revoked")
	// ErrDuplicateClient is returned by Store.Create on an ID collision.
	ErrDuplicateClient = errors.New("oauthserver: duplicate client")
	// ErrInvalidConfig is returned by New/AuthorizeHandler for invalid setup.
	ErrInvalidConfig = errors.New("oauthserver: invalid config")
	// ErrInvalidInput is returned by CreateClient for invalid input.
	ErrInvalidInput = errors.New("oauthserver: invalid input")
)
```

`auth/oauthserver/client.go`:

```go
package oauthserver

import (
	"slices"
	"time"
)

// Grant types supported by the token endpoint.
const (
	GrantClientCredentials = "client_credentials"
	GrantAuthorizationCode = "authorization_code"
)

// Client is a registered OAuth client: an M2M partner
// (client_credentials), a first-party app/mirror (authorization_code), or
// both. Secrets are stored as SHA-256 digests; plaintext exists only in
// the CreateClient/RotateSecret return value.
type Client struct {
	ID         string
	Name       string
	SecretHash []byte
	// Scopes is the allowlist; requests must be a subset.
	Scopes []string
	// Grants lists the grant types this client may use.
	Grants []string
	// RedirectURIs is the exact-match callback allowlist (authorization_code).
	RedirectURIs []string
	// TenantID scopes the client to a tenant; empty in single-tenant apps.
	TenantID string
	// TokenTTL overrides the server default when positive.
	TokenTTL  time.Duration
	RevokedAt time.Time
	CreatedAt time.Time
}

// Revoked reports whether the client has been revoked.
func (c Client) Revoked() bool { return !c.RevokedAt.IsZero() }

// AllowsGrant reports whether g is in the client's grant allowlist.
func (c Client) AllowsGrant(g string) bool { return slices.Contains(c.Grants, g) }

// AllowsRedirect reports whether uri exactly matches a registered redirect URI.
func (c Client) AllowsRedirect(uri string) bool {
	return uri != "" && slices.Contains(c.RedirectURIs, uri)
}

// AllowsScopes reports whether every requested scope is in the allowlist.
func (c Client) AllowsScopes(requested []string) bool {
	for _, s := range requested {
		if !slices.Contains(c.Scopes, s) {
			return false
		}
	}
	return true
}

// clone deep-copies c so store internals never alias caller slices.
func (c Client) clone() Client {
	c.SecretHash = slices.Clone(c.SecretHash)
	c.Scopes = slices.Clone(c.Scopes)
	c.Grants = slices.Clone(c.Grants)
	c.RedirectURIs = slices.Clone(c.RedirectURIs)
	return c
}
```

`auth/oauthserver/store.go`:

```go
package oauthserver

import "context"

// Store persists the client registry. Implementations must return
// ErrDuplicateClient from Create on an existing ID and ErrClientNotFound
// from Get/Update on a missing one. List("") returns every client;
// List(tenantID) filters by tenant.
type Store interface {
	Create(ctx context.Context, c Client) error
	Get(ctx context.Context, id string) (Client, error)
	Update(ctx context.Context, c Client) error
	List(ctx context.Context, tenantID string) ([]Client, error)
	Delete(ctx context.Context, id string) error
}
```

`auth/oauthserver/memstore.go`:

```go
package oauthserver

import (
	"context"
	"sync"
)

// memoryStore is the in-process Store for tests and single-node dev.
type memoryStore struct {
	mu sync.Mutex
	m  map[string]Client
}

// NewMemoryStore returns an in-memory Store. Data is lost on restart; use
// oauthserver/pgstore in production.
func NewMemoryStore() Store {
	return &memoryStore{m: map[string]Client{}}
}

func (s *memoryStore) Create(_ context.Context, c Client) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[c.ID]; ok {
		return ErrDuplicateClient
	}
	s.m[c.ID] = c.clone()
	return nil
}

func (s *memoryStore) Get(_ context.Context, id string) (Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.m[id]
	if !ok {
		return Client{}, ErrClientNotFound
	}
	return c.clone(), nil
}

func (s *memoryStore) Update(_ context.Context, c Client) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[c.ID]; !ok {
		return ErrClientNotFound
	}
	s.m[c.ID] = c.clone()
	return nil
}

func (s *memoryStore) List(_ context.Context, tenantID string) ([]Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Client, 0, len(s.m))
	for _, c := range s.m {
		if tenantID == "" || c.TenantID == tenantID {
			out = append(out, c.clone())
		}
	}
	return out, nil
}

func (s *memoryStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, id)
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race ./auth/oauthserver/...`
Expected: PASS.

- [ ] **Step 5: Format, lint, commit**

```bash
just fmt ./auth/oauthserver/...
just lint
git add auth/oauthserver
git commit -m "feat(oauthserver): client registry model, Store seam, memory store"
```

---

### Task 8: oauthserver Server core + client management

**Files:**
- Create: `auth/oauthserver/config.go`
- Create: `auth/oauthserver/options.go`
- Create: `auth/oauthserver/server.go`
- Create: `auth/oauthserver/manage.go`
- Test: `auth/oauthserver/manage_test.go`, `auth/oauthserver/helpers_test.go`

**Interfaces:**
- Consumes: `Client`, `Store`, `NewMemoryStore`, sentinels (Task 7); `jwt.Signer`, `token.Codec`, `cache.Store`, `keyset.Keyset`, `clock.Clock`, `id.NewPrefix`, `random.URLSafe`, `digest.SHA256`.
- Produces:
  - `type Config struct { Issuer string \`env:"OAUTHSERVER_ISSUER"\`; Audience string \`env:"OAUTHSERVER_AUDIENCE"\`; TokenTTL time.Duration \`env:"OAUTHSERVER_TOKEN_TTL"\` }` + `DefaultConfig()` (TokenTTL 15m) + `Validate()`
  - `func New(signer *jwt.Signer, store Store, opts ...Option) (*Server, error)`
  - Options: `WithConfig(cfg Config)`, `WithClock(clk clock.Clock)`, `WithScope(fn func(ctx context.Context) (string, error))`, `WithAuthenticator(fn func(w http.ResponseWriter, r *http.Request) (string, bool))`, `WithCodeStore(cs cache.Store)`, `WithCodeKeyset(ks *keyset.Keyset)`, `WithUserClaims(fn func(ctx context.Context, subject string) (map[string]any, error))`, `WithCodeTTL(d time.Duration)`
  - `type CreateClientInput struct { Name string; Scopes, Grants, RedirectURIs []string; TokenTTL time.Duration }`
  - `type ClientCredentials struct { ClientID, ClientSecret string }`
  - Methods: `CreateClient(ctx, in) (*ClientCredentials, error)`, `RotateSecret(ctx, id) (*ClientCredentials, error)`, `RevokeClient(ctx, id) error`, `GetClient(ctx, id) (Client, error)`, `ListClients(ctx) ([]Client, error)`
  - Unexported struct fields Tasks 9–11 use: `s.signer *jwt.Signer`, `s.store Store`, `s.cfg Config`, `s.clk clock.Clock`, `s.scope`, `s.authenticator`, `s.codeStore cache.Store`, `s.codes *token.Codec[authCode]`, `s.userClaims`, `s.codeTTL time.Duration`, `s.dummyHash []byte`; plus `type authCode struct { JTI, ClientID, RedirectURI, Subject, Scope, Nonce, Challenge string }` (json tags jti/cid/ru/sub/scp/n/cc).
  - Test helper `testSigner(tb testing.TB) *jwt.Signer` reused by Tasks 9–12 and 14.

- [ ] **Step 1: Write the failing tests**

`auth/oauthserver/helpers_test.go`:

```go
package oauthserver_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/jwt"
	"github.com/dmitrymomot/forge/auth/oauthserver"
	"github.com/dmitrymomot/forge/crypto/keyset"
)

// testSigner builds an Ed25519 jwt.Signer.
func testSigner(tb testing.TB) *jwt.Signer {
	tb.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(tb, err)
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	require.NoError(tb, err)
	ks, err := keyset.New(keyset.WithPrimary(1, der))
	require.NoError(tb, err)
	s, err := jwt.NewSigner(jwt.WithKeyset(ks))
	require.NoError(tb, err)
	return s
}

// testKeyset builds an HMAC keyset for sealing auth codes.
func testKeyset(tb testing.TB) *keyset.Keyset {
	tb.Helper()
	ks, err := keyset.New(keyset.WithPrimary(1, []byte("0123456789abcdef0123456789abcdef")))
	require.NoError(tb, err)
	return ks
}

// newServer builds a Server over a fresh memory store with a valid Config.
func newServer(tb testing.TB, opts ...oauthserver.Option) (*oauthserver.Server, oauthserver.Store) {
	tb.Helper()
	store := oauthserver.NewMemoryStore()
	cfg := oauthserver.DefaultConfig()
	cfg.Issuer = "https://auth.example.com"
	cfg.Audience = "https://api.example.com"
	base := []oauthserver.Option{oauthserver.WithConfig(cfg)}
	srv, err := oauthserver.New(testSigner(tb), store, append(base, opts...)...)
	require.NoError(tb, err)
	return srv, store
}
```

`auth/oauthserver/manage_test.go`:

```go
package oauthserver_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/oauthserver"
)

func TestCreateClient(t *testing.T) {
	srv, store := newServer(t)
	creds, err := srv.CreateClient(context.Background(), oauthserver.CreateClientInput{
		Name:   "partner",
		Grants: []string{oauthserver.GrantClientCredentials},
		Scopes: []string{"read:odds"},
	})
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(creds.ClientID, "client_"))
	assert.True(t, strings.HasPrefix(creds.ClientSecret, "osk_"))

	stored, err := store.Get(context.Background(), creds.ClientID)
	require.NoError(t, err)
	assert.NotEmpty(t, stored.SecretHash)
	assert.NotContains(t, string(stored.SecretHash), creds.ClientSecret, "plaintext never stored")
	assert.False(t, stored.CreatedAt.IsZero())
}

func TestCreateClientValidation(t *testing.T) {
	srv, _ := newServer(t)
	ctx := context.Background()
	_, err := srv.CreateClient(ctx, oauthserver.CreateClientInput{Grants: []string{"client_credentials"}})
	require.ErrorIs(t, err, oauthserver.ErrInvalidInput, "name required")
	_, err = srv.CreateClient(ctx, oauthserver.CreateClientInput{Name: "x"})
	require.ErrorIs(t, err, oauthserver.ErrInvalidInput, "grants required")
	_, err = srv.CreateClient(ctx, oauthserver.CreateClientInput{Name: "x", Grants: []string{"password"}})
	require.ErrorIs(t, err, oauthserver.ErrInvalidInput, "unknown grant")
	_, err = srv.CreateClient(ctx, oauthserver.CreateClientInput{Name: "x", Grants: []string{oauthserver.GrantAuthorizationCode}})
	require.ErrorIs(t, err, oauthserver.ErrInvalidInput, "auth-code needs redirect URIs")
	_, err = srv.CreateClient(ctx, oauthserver.CreateClientInput{
		Name: "x", Grants: []string{oauthserver.GrantAuthorizationCode}, RedirectURIs: []string{"not a url"},
	})
	require.ErrorIs(t, err, oauthserver.ErrInvalidInput, "redirect URIs must be absolute URLs")
}

func TestRotateSecret(t *testing.T) {
	srv, store := newServer(t)
	ctx := context.Background()
	creds, err := srv.CreateClient(ctx, oauthserver.CreateClientInput{
		Name: "p", Grants: []string{oauthserver.GrantClientCredentials},
	})
	require.NoError(t, err)
	before, _ := store.Get(ctx, creds.ClientID)

	rotated, err := srv.RotateSecret(ctx, creds.ClientID)
	require.NoError(t, err)
	assert.Equal(t, creds.ClientID, rotated.ClientID)
	assert.NotEqual(t, creds.ClientSecret, rotated.ClientSecret)
	after, _ := store.Get(ctx, creds.ClientID)
	assert.NotEqual(t, before.SecretHash, after.SecretHash)
}

func TestRevokeClient(t *testing.T) {
	srv, store := newServer(t)
	ctx := context.Background()
	creds, err := srv.CreateClient(ctx, oauthserver.CreateClientInput{
		Name: "p", Grants: []string{oauthserver.GrantClientCredentials},
	})
	require.NoError(t, err)
	require.NoError(t, srv.RevokeClient(ctx, creds.ClientID))
	got, _ := store.Get(ctx, creds.ClientID)
	assert.True(t, got.Revoked())
	require.NoError(t, srv.RevokeClient(ctx, creds.ClientID), "revoke is idempotent")
	_, err = srv.RotateSecret(ctx, creds.ClientID)
	require.ErrorIs(t, err, oauthserver.ErrClientRevoked)
}

func TestManagementTenancyScoping(t *testing.T) {
	tenant := "t1"
	srv, _ := newServer(t, oauthserver.WithScope(func(ctx context.Context) (string, error) {
		if tenant == "" {
			return "", errors.New("no tenant in ctx")
		}
		return tenant, nil
	}))
	ctx := context.Background()

	creds, err := srv.CreateClient(ctx, oauthserver.CreateClientInput{
		Name: "m1", Grants: []string{oauthserver.GrantClientCredentials},
	})
	require.NoError(t, err)

	got, err := srv.GetClient(ctx, creds.ClientID)
	require.NoError(t, err)
	assert.Equal(t, "t1", got.TenantID, "create stamps the tenant")

	tenant = "t2" // same call, different tenant scope
	_, err = srv.GetClient(ctx, creds.ClientID)
	require.ErrorIs(t, err, oauthserver.ErrClientNotFound, "cross-tenant access is a not-found")
	require.ErrorIs(t, srv.RevokeClient(ctx, creds.ClientID), oauthserver.ErrClientNotFound)

	list, err := srv.ListClients(ctx)
	require.NoError(t, err)
	assert.Empty(t, list)

	tenant = "" // hook error → fail closed
	_, err = srv.ListClients(ctx)
	require.Error(t, err)
}

func TestNewValidation(t *testing.T) {
	_, err := oauthserver.New(nil, oauthserver.NewMemoryStore())
	require.ErrorIs(t, err, oauthserver.ErrInvalidConfig)
	_, err = oauthserver.New(testSigner(t), nil)
	require.ErrorIs(t, err, oauthserver.ErrInvalidConfig)
	_, err = oauthserver.New(testSigner(t), oauthserver.NewMemoryStore()) // no issuer
	require.ErrorIs(t, err, oauthserver.ErrInvalidConfig)
}

func TestClientTokenTTLOverrideStored(t *testing.T) {
	srv, store := newServer(t)
	ctx := context.Background()
	creds, err := srv.CreateClient(ctx, oauthserver.CreateClientInput{
		Name: "p", Grants: []string{oauthserver.GrantClientCredentials}, TokenTTL: 5 * time.Minute,
	})
	require.NoError(t, err)
	got, _ := store.Get(ctx, creds.ClientID)
	assert.Equal(t, 5*time.Minute, got.TokenTTL)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./auth/oauthserver/ -run 'TestCreate|TestRotate|TestRevoke|TestManagement|TestNewValidation|TestClientTokenTTL' -v`
Expected: FAIL with `undefined: oauthserver.New`.

- [ ] **Step 3: Write the implementation**

`auth/oauthserver/config.go`:

```go
package oauthserver

import (
	"fmt"
	"time"
)

// Config is the env-loadable server configuration.
type Config struct {
	// Issuer is the iss claim on every issued token (the server's public URL).
	Issuer string `env:"OAUTHSERVER_ISSUER"`
	// Audience is the aud claim on access tokens; empty omits the claim.
	Audience string `env:"OAUTHSERVER_AUDIENCE"`
	// TokenTTL is the default access-token lifetime; per-client TokenTTL
	// overrides it.
	TokenTTL time.Duration `env:"OAUTHSERVER_TOKEN_TTL"`
}

// DefaultConfig returns the default policy: 15-minute tokens.
func DefaultConfig() Config {
	return Config{TokenTTL: 15 * time.Minute}
}

// Validate checks required fields.
func (c Config) Validate() error {
	if c.Issuer == "" {
		return fmt.Errorf("%w: Issuer required", ErrInvalidConfig)
	}
	if c.TokenTTL <= 0 {
		return fmt.Errorf("%w: TokenTTL must be positive", ErrInvalidConfig)
	}
	return nil
}
```

`auth/oauthserver/options.go`:

```go
package oauthserver

import (
	"context"
	"net/http"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/crypto/keyset"
	"github.com/dmitrymomot/forge/resilience/cache"
)

// Option configures New.
type Option func(*serverConfig)

type serverConfig struct {
	cfg           Config
	clk           clock.Clock
	scope         func(ctx context.Context) (string, error)
	authenticator func(w http.ResponseWriter, r *http.Request) (string, bool)
	codeStore     cache.Store
	codeKeyset    *keyset.Keyset
	userClaims    func(ctx context.Context, subject string) (map[string]any, error)
	codeTTL       time.Duration
}

// WithConfig replaces the default Config (typically env-loaded). A zero
// TokenTTL falls back to the DefaultConfig value.
func WithConfig(cfg Config) Option {
	return func(c *serverConfig) { c.cfg = cfg }
}

// WithClock overrides the time source (tests).
func WithClock(clk clock.Clock) Option {
	return func(c *serverConfig) { c.clk = clk }
}

// WithScope sets the tenancy hook for the management methods: CreateClient
// stamps the returned value as TenantID; Get/Rotate/Revoke/List are
// filtered by it; hook errors fail closed. Token issuance is NOT
// ctx-scoped — the tenant claim comes from the client record.
func WithScope(fn func(ctx context.Context) (string, error)) Option {
	return func(c *serverConfig) { c.scope = fn }
}

// WithAuthenticator sets the authorize-endpoint user seam: return the
// logged-in subject, or write your own response (e.g. redirect to the
// login page, returning here afterwards) and return ok=false.
func WithAuthenticator(fn func(w http.ResponseWriter, r *http.Request) (string, bool)) Option {
	return func(c *serverConfig) { c.authenticator = fn }
}

// WithCodeStore sets the TTL-KV store that makes authorization codes
// single-use (atomic SetNX claim on the code's jti). Memory store works
// for a single instance; use cache/redis for a fleet.
func WithCodeStore(cs cache.Store) Option {
	return func(c *serverConfig) { c.codeStore = cs }
}

// WithCodeKeyset sets the keyset sealing authorization codes
// (crypto/token; independent from the jwt.Signer's keys).
func WithCodeKeyset(ks *keyset.Keyset) Option {
	return func(c *serverConfig) { c.codeKeyset = ks }
}

// WithUserClaims enriches id_tokens with per-user claims (email, name,
// roles) so first-party apps skip a post-login lookup. Reserved claims
// (iss, sub, aud, exp, iat, nonce) cannot be overridden. Hook errors fail
// the token request.
func WithUserClaims(fn func(ctx context.Context, subject string) (map[string]any, error)) Option {
	return func(c *serverConfig) { c.userClaims = fn }
}

// WithCodeTTL bounds authorization-code lifetime. Default 60s.
func WithCodeTTL(d time.Duration) Option {
	return func(c *serverConfig) { c.codeTTL = d }
}
```

`auth/oauthserver/server.go`:

```go
package oauthserver

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/dmitrymomot/forge/auth/jwt"
	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/crypto/digest"
	"github.com/dmitrymomot/forge/crypto/token"
	"github.com/dmitrymomot/forge/resilience/cache"
)

// authCode is the sealed authorization-code payload. Codes are
// crypto/token blobs — deliberately not JWTs — so they can never be
// mistaken for access tokens; single-use is enforced by a SetNX claim on
// JTI at redemption.
type authCode struct {
	JTI         string `json:"jti"`
	ClientID    string `json:"cid"`
	RedirectURI string `json:"ru"`
	Subject     string `json:"sub"`
	Scope       string `json:"scp,omitempty"`
	Nonce       string `json:"n,omitempty"`
	Challenge   string `json:"cc"`
}

// Server issues OAuth2 tokens to registered clients: client_credentials
// for M2M partners and authorization_code (+PKCE) for first-party trusted
// apps. It is NOT a third-party IdP: no consent, no dynamic registration,
// no discovery metadata.
type Server struct {
	signer        *jwt.Signer
	store         Store
	cfg           Config
	clk           clock.Clock
	scope         func(ctx context.Context) (string, error)
	authenticator func(w http.ResponseWriter, r *http.Request) (string, bool)
	codeStore     cache.Store
	codes         *token.Codec[authCode]
	userClaims    func(ctx context.Context, subject string) (map[string]any, error)
	codeTTL       time.Duration
	idgen         id.Prefix
	dummyHash     []byte
}

// New builds a Server. signer provides the JWT keys (serve its JWKS() next
// to the token endpoint); store is the client registry.
func New(signer *jwt.Signer, store Store, opts ...Option) (*Server, error) {
	if signer == nil {
		return nil, fmt.Errorf("%w: signer required", ErrInvalidConfig)
	}
	if store == nil {
		return nil, fmt.Errorf("%w: store required", ErrInvalidConfig)
	}
	sc := serverConfig{cfg: DefaultConfig(), clk: clock.System(), codeTTL: time.Minute}
	for _, o := range opts {
		o(&sc)
	}
	if sc.cfg.TokenTTL <= 0 {
		sc.cfg.TokenTTL = DefaultConfig().TokenTTL
	}
	if err := sc.cfg.Validate(); err != nil {
		return nil, err
	}
	s := &Server{
		signer:        signer,
		store:         store,
		cfg:           sc.cfg,
		clk:           sc.clk,
		scope:         sc.scope,
		authenticator: sc.authenticator,
		codeStore:     sc.codeStore,
		userClaims:    sc.userClaims,
		codeTTL:       sc.codeTTL,
		idgen:         id.NewPrefix("client"),
		// dummyHash burns the same digest-compare time for unknown client
		// ids so they are indistinguishable from bad secrets.
		dummyHash: digest.SHA256([]byte("oauthserver:no-such-client")),
	}
	if sc.codeKeyset != nil {
		codes, err := token.FromKeyset[authCode](sc.codeKeyset,
			token.WithTTL(sc.codeTTL),
			token.WithPurpose("oauthserver:code"),
			token.WithClock(sc.clk),
		)
		if err != nil {
			return nil, err
		}
		s.codes = codes
	}
	return s, nil
}
```

`auth/oauthserver/manage.go`:

```go
package oauthserver

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"time"

	"github.com/dmitrymomot/forge/core/random"
	"github.com/dmitrymomot/forge/crypto/digest"
)

// secretPrefix makes leaked client secrets recognizable to scanners.
const secretPrefix = "osk_"

// CreateClientInput describes a new registry client.
type CreateClientInput struct {
	Name         string
	Scopes       []string
	Grants       []string
	RedirectURIs []string
	// TokenTTL overrides the server default when positive.
	TokenTTL time.Duration
}

// ClientCredentials carries the plaintext secret — returned exactly once
// by CreateClient/RotateSecret, never stored.
type ClientCredentials struct {
	ClientID     string
	ClientSecret string
}

var knownGrants = []string{GrantClientCredentials, GrantAuthorizationCode}

// CreateClient registers a client and returns its credentials. The secret
// is shown once; only its SHA-256 digest is stored.
func (s *Server) CreateClient(ctx context.Context, in CreateClientInput) (*ClientCredentials, error) {
	if in.Name == "" {
		return nil, fmt.Errorf("%w: name required", ErrInvalidInput)
	}
	if len(in.Grants) == 0 {
		return nil, fmt.Errorf("%w: at least one grant required", ErrInvalidInput)
	}
	for _, g := range in.Grants {
		if !slices.Contains(knownGrants, g) {
			return nil, fmt.Errorf("%w: unknown grant %q", ErrInvalidInput, g)
		}
	}
	if slices.Contains(in.Grants, GrantAuthorizationCode) {
		if len(in.RedirectURIs) == 0 {
			return nil, fmt.Errorf("%w: authorization_code requires redirect URIs", ErrInvalidInput)
		}
		for _, u := range in.RedirectURIs {
			parsed, err := url.Parse(u)
			if err != nil || !parsed.IsAbs() || parsed.Fragment != "" {
				return nil, fmt.Errorf("%w: redirect URI %q must be an absolute URL without fragment", ErrInvalidInput, u)
			}
		}
	}
	var tenant string
	if s.scope != nil {
		t, err := s.scope(ctx)
		if err != nil {
			return nil, fmt.Errorf("oauthserver: scope hook: %w", err)
		}
		tenant = t
	}
	secret := secretPrefix + random.URLSafe(32)
	cl := Client{
		ID:           s.idgen.New(),
		Name:         in.Name,
		SecretHash:   digest.SHA256([]byte(secret)),
		Scopes:       in.Scopes,
		Grants:       in.Grants,
		RedirectURIs: in.RedirectURIs,
		TenantID:     tenant,
		TokenTTL:     in.TokenTTL,
		CreatedAt:    s.clk.Now().UTC(),
	}
	if err := s.store.Create(ctx, cl); err != nil {
		return nil, err
	}
	return &ClientCredentials{ClientID: cl.ID, ClientSecret: secret}, nil
}

// RotateSecret replaces the client's secret, returning the new plaintext
// once. Tokens already issued stay valid until their exp.
func (s *Server) RotateSecret(ctx context.Context, clientID string) (*ClientCredentials, error) {
	cl, err := s.getScoped(ctx, clientID)
	if err != nil {
		return nil, err
	}
	if cl.Revoked() {
		return nil, ErrClientRevoked
	}
	secret := secretPrefix + random.URLSafe(32)
	cl.SecretHash = digest.SHA256([]byte(secret))
	if err := s.store.Update(ctx, cl); err != nil {
		return nil, err
	}
	return &ClientCredentials{ClientID: cl.ID, ClientSecret: secret}, nil
}

// RevokeClient disables the client. New tokens stop immediately;
// outstanding JWTs remain valid until their exp (≤ the token TTL) — there
// is no introspection endpoint by design.
func (s *Server) RevokeClient(ctx context.Context, clientID string) error {
	cl, err := s.getScoped(ctx, clientID)
	if err != nil {
		return err
	}
	if cl.Revoked() {
		return nil
	}
	cl.RevokedAt = s.clk.Now().UTC()
	return s.store.Update(ctx, cl)
}

// GetClient returns one client (tenancy-scoped when WithScope is set).
func (s *Server) GetClient(ctx context.Context, clientID string) (Client, error) {
	return s.getScoped(ctx, clientID)
}

// ListClients returns the tenant's clients (all clients without WithScope).
func (s *Server) ListClients(ctx context.Context) ([]Client, error) {
	if s.scope == nil {
		return s.store.List(ctx, "")
	}
	t, err := s.scope(ctx)
	if err != nil {
		return nil, fmt.Errorf("oauthserver: scope hook: %w", err)
	}
	return s.store.List(ctx, t)
}

// getScoped fetches a client and enforces the tenancy scope; a
// cross-tenant hit reads as not-found so existence never leaks.
func (s *Server) getScoped(ctx context.Context, clientID string) (Client, error) {
	cl, err := s.store.Get(ctx, clientID)
	if err != nil {
		return Client{}, err
	}
	if s.scope != nil {
		t, err := s.scope(ctx)
		if err != nil {
			return Client{}, fmt.Errorf("oauthserver: scope hook: %w", err)
		}
		if cl.TenantID != t {
			return Client{}, ErrClientNotFound
		}
	}
	return cl, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race ./auth/oauthserver/...`
Expected: PASS.

- [ ] **Step 5: Format, lint, commit**

```bash
just fmt ./auth/oauthserver/...
just lint
git add auth/oauthserver
git commit -m "feat(oauthserver): server core and client management"
```

---

### Task 9: oauthserver token endpoint — client_credentials

**Files:**
- Create: `auth/oauthserver/rfc.go`
- Create: `auth/oauthserver/token.go`
- Test: `auth/oauthserver/token_test.go`

**Interfaces:**
- Consumes: `Server` fields, `Client` helpers, `newServer`/`testSigner` helpers (Tasks 7–8).
- Produces:
  - `func (s *Server) TokenHandler() http.Handler`
  - unexported: `writeTokenError(w, status, code, desc)`, `writeTokenResponse(w, tokenResponse)`, `type tokenResponse struct { AccessToken string \`json:"access_token"\`; TokenType string \`json:"token_type"\`; ExpiresIn int64 \`json:"expires_in"\`; Scope string \`json:"scope,omitempty"\`; IDToken string \`json:"id_token,omitempty"\` }`, `type accessClaims struct { jwt.Claims; Scope string \`json:"scope,omitempty"\`; ClientID string \`json:"client_id,omitempty"\`; Tenant string \`json:"tenant,omitempty"\` }`, `(s *Server) authenticateClient(w, r) (Client, bool)`, `(s *Server) signAccessToken(sub string, cl Client, scope string) (string, time.Duration, error)`, `(s *Server) handleClientCredentials(w, r, cl)`.
  - Task 11 replaces the `GrantAuthorizationCode` switch arm; Task 9 stubs it with `writeTokenError(w, http.StatusBadRequest, "unsupported_grant_type", "authorization_code not configured")`.

- [ ] **Step 1: Write the failing tests**

`auth/oauthserver/token_test.go`:

```go
package oauthserver_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/jwt"
	"github.com/dmitrymomot/forge/auth/oauthserver"
)

// ccClient registers a client_credentials client and returns its creds.
func ccClient(t *testing.T, srv *oauthserver.Server, scopes ...string) *oauthserver.ClientCredentials {
	t.Helper()
	creds, err := srv.CreateClient(context.Background(), oauthserver.CreateClientInput{
		Name: "partner", Grants: []string{oauthserver.GrantClientCredentials}, Scopes: scopes,
	})
	require.NoError(t, err)
	return creds
}

// postToken POSTs form to the token handler; basic creds attach when set.
func postToken(t *testing.T, h http.Handler, form url.Values, basicID, basicSecret string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if basicID != "" {
		req.SetBasicAuth(basicID, basicSecret)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &m))
	return m
}

type ccClaims struct {
	jwt.Claims
	Scope    string `json:"scope"`
	ClientID string `json:"client_id"`
	Tenant   string `json:"tenant"`
}

func TestClientCredentialsBasicAuth(t *testing.T) {
	signer := testSigner(t)
	store := oauthserver.NewMemoryStore()
	cfg := oauthserver.DefaultConfig()
	cfg.Issuer = "https://auth.example.com"
	cfg.Audience = "https://api.example.com"
	srv, err := oauthserver.New(signer, store, oauthserver.WithConfig(cfg))
	require.NoError(t, err)
	creds := ccClient(t, srv, "read:odds", "write:bets")

	rec := postToken(t, srv.TokenHandler(), url.Values{
		"grant_type": {"client_credentials"}, "scope": {"read:odds"},
	}, creds.ClientID, creds.ClientSecret)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	body := decodeJSON(t, rec)
	assert.Equal(t, "Bearer", body["token_type"])
	assert.Equal(t, "read:odds", body["scope"])
	assert.InDelta(t, 900, body["expires_in"], 1)

	// verify the JWT like a resource server would
	v, err := jwt.NewVerifier(
		jwt.WithKeys(signer.PublicKeys()...),
		jwt.WithIssuer("https://auth.example.com"),
		jwt.WithAudience("https://api.example.com"),
	)
	require.NoError(t, err)
	claims, err := jwt.Verify[ccClaims](context.Background(), v, body["access_token"].(string))
	require.NoError(t, err)
	assert.Equal(t, creds.ClientID, claims.Subject)
	assert.Equal(t, creds.ClientID, claims.ClientID)
	assert.Equal(t, "read:odds", claims.Scope)
	assert.NotEmpty(t, claims.ID, "jti present")
}

func TestClientCredentialsPostAuth(t *testing.T) {
	srv, _ := newServer(t)
	creds := ccClient(t, srv, "read")
	rec := postToken(t, srv.TokenHandler(), url.Values{
		"grant_type": {"client_credentials"},
		"client_id":  {creds.ClientID}, "client_secret": {creds.ClientSecret},
	}, "", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

func TestClientCredentialsOmittedScopeGrantsFullSet(t *testing.T) {
	srv, _ := newServer(t)
	creds := ccClient(t, srv, "a", "b")
	rec := postToken(t, srv.TokenHandler(), url.Values{"grant_type": {"client_credentials"}},
		creds.ClientID, creds.ClientSecret)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "a b", decodeJSON(t, rec)["scope"])
}

func TestClientCredentialsScopeSupersetRejected(t *testing.T) {
	srv, _ := newServer(t)
	creds := ccClient(t, srv, "read")
	rec := postToken(t, srv.TokenHandler(), url.Values{
		"grant_type": {"client_credentials"}, "scope": {"read admin"},
	}, creds.ClientID, creds.ClientSecret)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "invalid_scope", decodeJSON(t, rec)["error"])
}

func TestTokenEndpointAuthFailures(t *testing.T) {
	srv, _ := newServer(t)
	creds := ccClient(t, srv, "read")
	h := srv.TokenHandler()

	for name, tc := range map[string]struct{ id, secret string }{
		"wrong secret":   {creds.ClientID, "osk_wrong"},
		"unknown client": {"client_nope", "osk_whatever"},
		"empty":          {"", ""},
	} {
		t.Run(name, func(t *testing.T) {
			rec := postToken(t, h, url.Values{"grant_type": {"client_credentials"}}, tc.id, tc.secret)
			require.Equal(t, http.StatusUnauthorized, rec.Code)
			assert.Equal(t, "invalid_client", decodeJSON(t, rec)["error"])
			assert.Contains(t, rec.Header().Get("WWW-Authenticate"), "Basic")
			assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
		})
	}
}

func TestTokenEndpointRevokedClient(t *testing.T) {
	srv, _ := newServer(t)
	creds := ccClient(t, srv, "read")
	require.NoError(t, srv.RevokeClient(context.Background(), creds.ClientID))
	rec := postToken(t, srv.TokenHandler(), url.Values{"grant_type": {"client_credentials"}},
		creds.ClientID, creds.ClientSecret)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Equal(t, "invalid_client", decodeJSON(t, rec)["error"])
}

func TestTokenEndpointGrantNotAllowed(t *testing.T) {
	srv, _ := newServer(t)
	// auth-code-only client trying client_credentials
	creds, err := srv.CreateClient(context.Background(), oauthserver.CreateClientInput{
		Name: "app", Grants: []string{oauthserver.GrantAuthorizationCode},
		RedirectURIs: []string{"https://m1.example.com/cb"},
	})
	require.NoError(t, err)
	rec := postToken(t, srv.TokenHandler(), url.Values{"grant_type": {"client_credentials"}},
		creds.ClientID, creds.ClientSecret)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "unauthorized_client", decodeJSON(t, rec)["error"])
}

func TestTokenEndpointUnsupportedGrantAndMethod(t *testing.T) {
	srv, _ := newServer(t)
	creds := ccClient(t, srv, "read")
	rec := postToken(t, srv.TokenHandler(), url.Values{"grant_type": {"password"}},
		creds.ClientID, creds.ClientSecret)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "unsupported_grant_type", decodeJSON(t, rec)["error"])

	req := httptest.NewRequest(http.MethodGet, "/token", nil)
	rec2 := httptest.NewRecorder()
	srv.TokenHandler().ServeHTTP(rec2, req)
	require.Equal(t, http.StatusMethodNotAllowed, rec2.Code)
}

func TestTokenClientTTLOverride(t *testing.T) {
	srv, _ := newServer(t)
	creds, err := srv.CreateClient(context.Background(), oauthserver.CreateClientInput{
		Name: "p", Grants: []string{oauthserver.GrantClientCredentials}, TokenTTL: 5 * time.Minute,
	})
	require.NoError(t, err)
	rec := postToken(t, srv.TokenHandler(), url.Values{"grant_type": {"client_credentials"}},
		creds.ClientID, creds.ClientSecret)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.InDelta(t, 300, decodeJSON(t, rec)["expires_in"], 1)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./auth/oauthserver/ -run 'TestClientCredentials|TestTokenEndpoint|TestTokenClient' -v`
Expected: FAIL with `undefined: srv.TokenHandler`.

- [ ] **Step 3: Write the implementation**

`auth/oauthserver/rfc.go`:

```go
package oauthserver

import (
	"encoding/json"
	"net/http"
)

// writeTokenError writes an RFC 6749 §5.2 error. The token endpoint
// deliberately does NOT speak problem+json: partners' OAuth libraries
// expect the RFC shape. Descriptions are static strings — internal error
// text never reaches the wire.
func writeTokenError(w http.ResponseWriter, status int, code, desc string) {
	w.Header().Set("Content-Type", "application/json;charset=UTF-8")
	w.Header().Set("Cache-Control", "no-store")
	if status == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", `Basic realm="oauth", charset="UTF-8"`)
	}
	w.WriteHeader(status)
	body := map[string]string{"error": code}
	if desc != "" {
		body["error_description"] = desc
	}
	_ = json.NewEncoder(w).Encode(body)
}

// tokenResponse is the RFC 6749 §5.1 success body.
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
	Scope       string `json:"scope,omitempty"`
	IDToken     string `json:"id_token,omitempty"`
}

func writeTokenResponse(w http.ResponseWriter, resp tokenResponse) {
	w.Header().Set("Content-Type", "application/json;charset=UTF-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(resp)
}
```

`auth/oauthserver/token.go`:

```go
package oauthserver

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dmitrymomot/forge/auth/jwt"
	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/crypto/consttime"
	"github.com/dmitrymomot/forge/crypto/digest"
)

// accessClaims is the access-token claim set. sub is the client for M2M
// tokens and the end user for auth-code tokens; client_id always names
// the requesting client; tenant carries the client's tenant when set.
type accessClaims struct {
	jwt.Claims
	Scope    string `json:"scope,omitempty"`
	ClientID string `json:"client_id,omitempty"`
	Tenant   string `json:"tenant,omitempty"`
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
			// Replaced with the real implementation in the auth-code task.
			writeTokenError(w, http.StatusBadRequest, "unsupported_grant_type", "authorization_code not configured")
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
		Claims: jwt.Claims{
			Issuer:    s.cfg.Issuer,
			Subject:   sub,
			ID:        id.NewULID().String(),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
		Scope:    scope,
		ClientID: cl.ID,
		Tenant:   cl.TenantID,
	}
	if s.cfg.Audience != "" {
		claims.Audience = jwt.Audience{s.cfg.Audience}
	}
	tok, err := s.signer.Sign(claims)
	return tok, ttl, err
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race ./auth/oauthserver/...`
Expected: PASS.

- [ ] **Step 5: Format, lint, commit**

```bash
just fmt ./auth/oauthserver/...
just lint
git add auth/oauthserver
git commit -m "feat(oauthserver): RFC 6749 token endpoint with client_credentials grant"
```

---

### Task 10: oauthserver authorize endpoint

**Files:**
- Create: `auth/oauthserver/authorize.go`
- Test: `auth/oauthserver/authorize_test.go`

**Interfaces:**
- Consumes: `Server` fields incl. `s.codes`, `s.codeStore`, `s.authenticator` (Task 8); `authCode` (Task 8); helpers (Tasks 8–9).
- Produces: `func (s *Server) AuthorizeHandler() (http.Handler, error)` (the PKCE S256 transform `pkceChallenge` lands in Task 11 with its only caller, so no task leaves an unused func behind for the linter).

- [ ] **Step 1: Write the failing tests**

`auth/oauthserver/authorize_test.go`:

```go
package oauthserver_test

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/oauthserver"
	"github.com/dmitrymomot/forge/crypto/digest"
	"github.com/dmitrymomot/forge/resilience/cache"
)

// acServer builds a Server wired for the auth-code flow with a static
// logged-in subject.
func acServer(t *testing.T, subject string, ok bool, opts ...oauthserver.Option) *oauthserver.Server {
	t.Helper()
	store := cache.NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })
	base := []oauthserver.Option{
		oauthserver.WithCodeStore(store),
		oauthserver.WithCodeKeyset(testKeyset(t)),
		oauthserver.WithAuthenticator(func(w http.ResponseWriter, r *http.Request) (string, bool) {
			if !ok {
				http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.String()), http.StatusSeeOther)
				return "", false
			}
			return subject, true
		}),
	}
	srv, _ := newServer(t, append(base, opts...)...)
	return srv
}

// acClient registers an auth-code client.
func acClient(t *testing.T, srv *oauthserver.Server, redirects ...string) *oauthserver.ClientCredentials {
	t.Helper()
	if len(redirects) == 0 {
		redirects = []string{"https://mirror1.example.com/cb"}
	}
	creds, err := srv.CreateClient(context.Background(), oauthserver.CreateClientInput{
		Name: "mirror", Grants: []string{oauthserver.GrantAuthorizationCode},
		Scopes: []string{"profile", "email"}, RedirectURIs: redirects,
	})
	require.NoError(t, err)
	return creds
}

func s256(verifier string) string {
	return base64.RawURLEncoding.EncodeToString(digest.SHA256([]byte(verifier)))
}

// authorizeQuery builds a valid authorize request query.
func authorizeQuery(clientID string) url.Values {
	return url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {"https://mirror1.example.com/cb"},
		"scope":                 {"profile"},
		"state":                 {"st-1"},
		"nonce":                 {"n-1"},
		"code_challenge":        {s256("verifier-123")},
		"code_challenge_method": {"S256"},
	}
}

func getAuthorize(t *testing.T, h http.Handler, q url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/authorize?"+q.Encode(), nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAuthorizeHappyPath(t *testing.T) {
	srv := acServer(t, "user-1", true)
	creds := acClient(t, srv)
	h, err := srv.AuthorizeHandler()
	require.NoError(t, err)

	rec := getAuthorize(t, h, authorizeQuery(creds.ClientID))
	require.Equal(t, http.StatusFound, rec.Code)
	loc, err := url.Parse(rec.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "mirror1.example.com", loc.Host)
	assert.NotEmpty(t, loc.Query().Get("code"))
	assert.Equal(t, "st-1", loc.Query().Get("state"))
	assert.Empty(t, loc.Query().Get("error"))
}

func TestAuthorizeInvalidClientOrRedirectIsLocal400(t *testing.T) {
	srv := acServer(t, "user-1", true)
	creds := acClient(t, srv)
	h, err := srv.AuthorizeHandler()
	require.NoError(t, err)

	q := authorizeQuery(creds.ClientID)
	q.Set("redirect_uri", "https://evil.example.com/cb")
	rec := getAuthorize(t, h, q)
	require.Equal(t, http.StatusBadRequest, rec.Code, "unregistered redirect_uri never gets a redirect")

	q2 := authorizeQuery("client_unknown")
	rec2 := getAuthorize(t, h, q2)
	require.Equal(t, http.StatusBadRequest, rec2.Code)
}

func TestAuthorizeProtocolErrorsRedirectBack(t *testing.T) {
	srv := acServer(t, "user-1", true)
	creds := acClient(t, srv)
	h, err := srv.AuthorizeHandler()
	require.NoError(t, err)

	for name, mutate := range map[string]struct {
		key, val, wantErr string
	}{
		"bad response_type": {"response_type", "token", "unsupported_response_type"},
		"missing pkce":      {"code_challenge", "", "invalid_request"},
		"plain pkce":        {"code_challenge_method", "plain", "invalid_request"},
		"scope superset":    {"scope", "profile admin", "invalid_scope"},
	} {
		t.Run(name, func(t *testing.T) {
			q := authorizeQuery(creds.ClientID)
			if mutate.val == "" {
				q.Del(mutate.key)
			} else {
				q.Set(mutate.key, mutate.val)
			}
			rec := getAuthorize(t, h, q)
			require.Equal(t, http.StatusFound, rec.Code)
			loc, _ := url.Parse(rec.Header().Get("Location"))
			assert.Equal(t, mutate.wantErr, loc.Query().Get("error"))
			assert.Equal(t, "st-1", loc.Query().Get("state"))
			assert.Empty(t, loc.Query().Get("code"))
		})
	}
}

func TestAuthorizeUnauthenticatedDelegatesToSeam(t *testing.T) {
	srv := acServer(t, "", false)
	creds := acClient(t, srv)
	h, err := srv.AuthorizeHandler()
	require.NoError(t, err)

	rec := getAuthorize(t, h, authorizeQuery(creds.ClientID))
	require.Equal(t, http.StatusSeeOther, rec.Code, "authenticator wrote its login redirect")
	assert.Contains(t, rec.Header().Get("Location"), "/login?next=")
}

func TestAuthorizeHandlerRequiresSeams(t *testing.T) {
	srv, _ := newServer(t) // no authenticator/code store/keyset
	_, err := srv.AuthorizeHandler()
	require.ErrorIs(t, err, oauthserver.ErrInvalidConfig)
}

func TestAuthorizeRevokedClient(t *testing.T) {
	srv := acServer(t, "user-1", true)
	creds := acClient(t, srv)
	require.NoError(t, srv.RevokeClient(context.Background(), creds.ClientID))
	h, err := srv.AuthorizeHandler()
	require.NoError(t, err)
	rec := getAuthorize(t, h, authorizeQuery(creds.ClientID))
	require.Equal(t, http.StatusBadRequest, rec.Code)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./auth/oauthserver/ -run TestAuthorize -v`
Expected: FAIL with `undefined: srv.AuthorizeHandler`.

- [ ] **Step 3: Write the implementation**

`auth/oauthserver/authorize.go`:

```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race ./auth/oauthserver/...`
Expected: PASS.

- [ ] **Step 5: Format, lint, commit**

```bash
just fmt ./auth/oauthserver/...
just lint
git add auth/oauthserver
git commit -m "feat(oauthserver): first-party authorize endpoint with mandatory PKCE"
```

---

### Task 11: oauthserver authorization_code grant on the token endpoint

**Files:**
- Modify: `auth/oauthserver/token.go` (replace the `GrantAuthorizationCode` stub arm; add `handleAuthorizationCode` + `signIDToken`)
- Test: `auth/oauthserver/authcode_test.go`

**Interfaces:**
- Consumes: `authCode`, `s.codes`, `s.codeStore`, `s.codeTTL`, `s.userClaims`, `signAccessToken`, `pkceChallenge`, `writeTokenError`, `writeTokenResponse` (Tasks 8–10); `cache.WithTTL`, `cache.WithSetNonExist`, `cache.ErrExists`.
- Produces: complete `TokenHandler` (both grants); unexported `handleAuthorizationCode`, `signIDToken`, `reservedIDClaims`.

- [ ] **Step 1: Write the failing tests**

`auth/oauthserver/authcode_test.go`:

```go
package oauthserver_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/jwt"
	"github.com/dmitrymomot/forge/auth/oauthserver"
)

// obtainCode drives the authorize endpoint and returns the issued code.
func obtainCode(t *testing.T, srv *oauthserver.Server, clientID string) string {
	t.Helper()
	h, err := srv.AuthorizeHandler()
	require.NoError(t, err)
	rec := getAuthorize(t, h, authorizeQuery(clientID))
	require.Equal(t, http.StatusFound, rec.Code)
	loc, err := url.Parse(rec.Header().Get("Location"))
	require.NoError(t, err)
	code := loc.Query().Get("code")
	require.NotEmpty(t, code)
	return code
}

// redeemForm builds a valid auth-code redemption form.
func redeemForm(code string) url.Values {
	return url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {"https://mirror1.example.com/cb"},
		"code_verifier": {"verifier-123"},
	}
}

type idClaims struct {
	jwt.Claims
	Nonce string `json:"nonce"`
	Email string `json:"email"`
}

func TestAuthCodeRedemptionHappyPath(t *testing.T) {
	signer := testSigner(t)
	store := oauthserver.NewMemoryStore()
	cfg := oauthserver.DefaultConfig()
	cfg.Issuer = "https://auth.example.com"
	srv, err := oauthserver.New(signer, store,
		oauthserver.WithConfig(cfg),
		oauthserver.WithCodeStore(cacheStore(t)),
		oauthserver.WithCodeKeyset(testKeyset(t)),
		oauthserver.WithAuthenticator(staticUser("user-1")),
		oauthserver.WithUserClaims(func(ctx context.Context, subject string) (map[string]any, error) {
			return map[string]any{"email": "u1@example.com", "sub": "OVERRIDE-IGNORED"}, nil
		}),
	)
	require.NoError(t, err)
	creds := acClient(t, srv)

	code := obtainCode(t, srv, creds.ClientID)
	rec := postToken(t, srv.TokenHandler(), redeemForm(code), creds.ClientID, creds.ClientSecret)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body := decodeJSON(t, rec)
	assert.Equal(t, "profile", body["scope"])
	require.NotEmpty(t, body["id_token"])
	require.NotEmpty(t, body["access_token"])

	// access token: sub = user, client_id = client, verified via JWKS keys
	av, err := jwt.NewVerifier(jwt.WithKeys(signer.PublicKeys()...), jwt.WithIssuer("https://auth.example.com"))
	require.NoError(t, err)
	access, err := jwt.Verify[ccClaims](context.Background(), av, body["access_token"].(string))
	require.NoError(t, err)
	assert.Equal(t, "user-1", access.Subject)
	assert.Equal(t, creds.ClientID, access.ClientID)

	// id_token: aud = client_id, nonce echoed, user claims merged, sub protected
	iv, err := jwt.NewVerifier(jwt.WithKeys(signer.PublicKeys()...),
		jwt.WithIssuer("https://auth.example.com"), jwt.WithAudience(creds.ClientID))
	require.NoError(t, err)
	idt, err := jwt.Verify[idClaims](context.Background(), iv, body["id_token"].(string))
	require.NoError(t, err)
	assert.Equal(t, "user-1", idt.Subject, "reserved sub claim survives the hook")
	assert.Equal(t, "n-1", idt.Nonce)
	assert.Equal(t, "u1@example.com", idt.Email)
}

func TestAuthCodeReplayRejected(t *testing.T) {
	srv := acServer(t, "user-1", true)
	creds := acClient(t, srv)
	code := obtainCode(t, srv, creds.ClientID)
	h := srv.TokenHandler()

	rec := postToken(t, h, redeemForm(code), creds.ClientID, creds.ClientSecret)
	require.Equal(t, http.StatusOK, rec.Code)

	rec2 := postToken(t, h, redeemForm(code), creds.ClientID, creds.ClientSecret)
	require.Equal(t, http.StatusBadRequest, rec2.Code)
	assert.Equal(t, "invalid_grant", decodeJSON(t, rec2)["error"])
}

func TestAuthCodeWrongVerifier(t *testing.T) {
	srv := acServer(t, "user-1", true)
	creds := acClient(t, srv)
	code := obtainCode(t, srv, creds.ClientID)
	form := redeemForm(code)
	form.Set("code_verifier", "wrong-verifier")
	rec := postToken(t, srv.TokenHandler(), form, creds.ClientID, creds.ClientSecret)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "invalid_grant", decodeJSON(t, rec)["error"])
}

func TestAuthCodeWrongRedirectURI(t *testing.T) {
	srv := acServer(t, "user-1", true)
	creds := acClient(t, srv)
	code := obtainCode(t, srv, creds.ClientID)
	form := redeemForm(code)
	form.Set("redirect_uri", "https://mirror1.example.com/cb2")
	rec := postToken(t, srv.TokenHandler(), form, creds.ClientID, creds.ClientSecret)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "invalid_grant", decodeJSON(t, rec)["error"])
}

func TestAuthCodeOtherClientCannotRedeem(t *testing.T) {
	srv := acServer(t, "user-1", true)
	creds := acClient(t, srv)
	other, err := srv.CreateClient(context.Background(), oauthserver.CreateClientInput{
		Name: "other", Grants: []string{oauthserver.GrantAuthorizationCode},
		RedirectURIs: []string{"https://mirror1.example.com/cb"},
	})
	require.NoError(t, err)
	code := obtainCode(t, srv, creds.ClientID)
	rec := postToken(t, srv.TokenHandler(), redeemForm(code), other.ClientID, other.ClientSecret)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "invalid_grant", decodeJSON(t, rec)["error"])
}

func TestAuthCodeGrantWithoutCodeStore(t *testing.T) {
	srv, _ := newServer(t) // no code store / keyset
	creds, err := srv.CreateClient(context.Background(), oauthserver.CreateClientInput{
		Name: "app", Grants: []string{oauthserver.GrantAuthorizationCode},
		RedirectURIs: []string{"https://m/cb"},
	})
	require.NoError(t, err)
	rec := postToken(t, srv.TokenHandler(), redeemForm("some-code"), creds.ClientID, creds.ClientSecret)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "unsupported_grant_type", decodeJSON(t, rec)["error"])
}

func TestAuthCodeUserClaimsHookErrorFailsClosed(t *testing.T) {
	srv := acServer(t, "user-1", true,
		oauthserver.WithUserClaims(func(ctx context.Context, subject string) (map[string]any, error) {
			return nil, errors.New("directory down")
		}))
	creds := acClient(t, srv)
	code := obtainCode(t, srv, creds.ClientID)
	rec := postToken(t, srv.TokenHandler(), redeemForm(code), creds.ClientID, creds.ClientSecret)
	require.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, "server_error", decodeJSON(t, rec)["error"])
}
```

Add to `auth/oauthserver/helpers_test.go`:

```go
// staticUser is an Authenticator that always returns subject.
func staticUser(subject string) func(http.ResponseWriter, *http.Request) (string, bool) {
	return func(w http.ResponseWriter, r *http.Request) (string, bool) { return subject, true }
}

// cacheStore returns a memory cache.Store cleaned up with the test.
func cacheStore(tb testing.TB) cache.Store {
	tb.Helper()
	s := cache.NewMemoryStore()
	tb.Cleanup(func() { _ = s.Close() })
	return s
}
```

(with imports `net/http` and `github.com/dmitrymomot/forge/resilience/cache` added to helpers_test.go; `acServer` from Task 10 already closes its own store via t.Cleanup.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./auth/oauthserver/ -run TestAuthCode -v`
Expected: FAIL — redemption returns `unsupported_grant_type` (the Task 9 stub), so the happy-path test fails.

- [ ] **Step 3: Write the implementation**

In `auth/oauthserver/token.go`, replace the stub arm:

```go
		case GrantAuthorizationCode:
			s.handleAuthorizationCode(w, r, cl)
```

and append (new imports: `context`, `encoding/base64`, `errors`, `fmt`, `github.com/dmitrymomot/forge/resilience/cache`):

```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race ./auth/oauthserver/...`
Expected: PASS (all Task 7–11 tests).

- [ ] **Step 5: Format, lint, commit**

```bash
just fmt ./auth/oauthserver/...
just lint
git add auth/oauthserver
git commit -m "feat(oauthserver): authorization_code grant with single-use sealed codes and id_token"
```

---

### Task 12: oauthserver doc.go + benchmarks

**Files:**
- Create: `auth/oauthserver/doc.go`
- Create: `auth/oauthserver/bench_test.go`

**Interfaces:**
- Consumes: the whole public API (Tasks 7–11).
- Produces: nothing new — documentation and benchmark baseline.

- [ ] **Step 1: Write doc.go**

```go
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
//	srv, err := oauthserver.New(signer, pgstore.New(pool),
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
```

- [ ] **Step 2: Write benchmarks**

`auth/oauthserver/bench_test.go`:

```go
package oauthserver_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/oauthserver"
)

func BenchmarkTokenClientCredentials(b *testing.B) {
	srv, _ := newServer(b)
	creds, err := srv.CreateClient(context.Background(), oauthserver.CreateClientInput{
		Name: "p", Grants: []string{oauthserver.GrantClientCredentials}, Scopes: []string{"read"},
	})
	require.NoError(b, err)
	h := srv.TokenHandler()
	form := url.Values{"grant_type": {"client_credentials"}}.Encode()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		req := httptest.NewRequest(http.MethodPost, "/token", strings.NewReader(form))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.SetBasicAuth(creds.ClientID, creds.ClientSecret)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			b.Fatalf("status %d", rec.Code)
		}
	}
}

func BenchmarkTokenBadSecret(b *testing.B) {
	// The rejection path must stay cheap: it is the brute-force surface.
	srv, _ := newServer(b)
	creds, err := srv.CreateClient(context.Background(), oauthserver.CreateClientInput{
		Name: "p", Grants: []string{oauthserver.GrantClientCredentials},
	})
	require.NoError(b, err)
	h := srv.TokenHandler()
	form := url.Values{"grant_type": {"client_credentials"}}.Encode()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		req := httptest.NewRequest(http.MethodPost, "/token", strings.NewReader(form))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.SetBasicAuth(creds.ClientID, "osk_wrong")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			b.Fatalf("status %d", rec.Code)
		}
	}
}
```

Note: `newServer` takes `testing.TB` already (helpers_test.go), so benchmarks reuse it directly.

- [ ] **Step 3: Run benchmarks and record the baseline**

Run: `just bench ./auth/oauthserver/`
Expected: both benchmarks complete. Ed25519 signing (~50µs) dominates BenchmarkTokenClientCredentials — that is expected and fine. Record ns/op and allocs/op in the commit message; optimize only obvious waste (e.g. per-call allocations in authenticateClient) if the numbers show it.

- [ ] **Step 4: Full test + lint**

Run: `just test ./auth/oauthserver/ && just lint`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
just fmt ./auth/oauthserver/...
git add auth/oauthserver
git commit -m "docs(oauthserver): package docs and benchmarks"
```

---

### Task 13: oauthserver/pgstore driver

**Files:**
- Create: `auth/oauthserver/pgstore/doc.go`
- Create: `auth/oauthserver/pgstore/migrations/00001_oauth_clients.sql`
- Create: `auth/oauthserver/pgstore/pgstore.go`
- Test: `auth/oauthserver/pgstore/pgstore_test.go`

**Interfaces:**
- Consumes: `oauthserver.Client`, `oauthserver.Store` contract, `oauthserver.ErrDuplicateClient`, `oauthserver.ErrClientNotFound` (Task 7).
- Produces: `pgstore.New(pool *pgxpool.Pool) *Store` implementing `oauthserver.Store`; exported `pgstore.Migrations fs.FS` for `data/migration`.

- [ ] **Step 1: Write the migration**

`auth/oauthserver/pgstore/migrations/00001_oauth_clients.sql`:

```sql
-- +goose Up
CREATE TABLE IF NOT EXISTS oauth_clients (
    id            text PRIMARY KEY,
    name          text NOT NULL,
    secret_hash   bytea NOT NULL,
    scopes        text[] NOT NULL DEFAULT '{}',
    grants        text[] NOT NULL DEFAULT '{}',
    redirect_uris text[] NOT NULL DEFAULT '{}',
    tenant_id     text NOT NULL DEFAULT '',
    token_ttl_ms  bigint NOT NULL DEFAULT 0,
    revoked_at    timestamptz,
    created_at    timestamptz NOT NULL
);

CREATE INDEX IF NOT EXISTS oauth_clients_tenant_idx ON oauth_clients (tenant_id);

-- +goose Down
DROP TABLE IF EXISTS oauth_clients;
```

- [ ] **Step 2: Write the failing test**

`auth/oauthserver/pgstore/pgstore_test.go`:

```go
package pgstore_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/oauthserver"
	"github.com/dmitrymomot/forge/auth/oauthserver/pgstore"
	"github.com/dmitrymomot/forge/data/migration"
	"github.com/dmitrymomot/forge/data/postgres"
)

var _ oauthserver.Store = (*pgstore.Store)(nil)

func newStore(t *testing.T) *pgstore.Store {
	dsn := os.Getenv("FORGE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set FORGE_TEST_POSTGRES_DSN")
	}
	cfg := postgres.DefaultConfig()
	cfg.URL = dsn
	pool, err := postgres.Open(context.Background(), postgres.WithConfig(cfg))
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	db := stdlib.OpenDBFromPool(pool)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, migration.New(pgstore.Migrations, migration.WithTable("forge_oauthserver_schema")).Up(context.Background(), db))
	return pgstore.New(pool)
}

func sample(id, tenant string) oauthserver.Client {
	return oauthserver.Client{
		ID: id, Name: "n-" + id, SecretHash: []byte{1, 2, 3},
		Scopes: []string{"read"}, Grants: []string{oauthserver.GrantClientCredentials},
		RedirectURIs: []string{"https://m.example.com/cb"},
		TenantID:     tenant, TokenTTL: 5 * time.Minute,
		CreatedAt: time.Now().UTC().Truncate(time.Millisecond),
	}
}

func TestPgStoreCRUD(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	id := "client_" + t.Name()
	_ = s.Delete(ctx, id)

	c := sample(id, "t1")
	require.NoError(t, s.Create(ctx, c))
	require.ErrorIs(t, s.Create(ctx, c), oauthserver.ErrDuplicateClient)

	got, err := s.Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, c.Name, got.Name)
	assert.Equal(t, c.SecretHash, got.SecretHash)
	assert.Equal(t, c.Scopes, got.Scopes)
	assert.Equal(t, c.Grants, got.Grants)
	assert.Equal(t, c.RedirectURIs, got.RedirectURIs)
	assert.Equal(t, 5*time.Minute, got.TokenTTL)
	assert.False(t, got.Revoked())
	assert.WithinDuration(t, c.CreatedAt, got.CreatedAt, time.Second)

	got.RevokedAt = time.Now().UTC()
	got.Name = "renamed"
	require.NoError(t, s.Update(ctx, got))
	got2, err := s.Get(ctx, id)
	require.NoError(t, err)
	assert.True(t, got2.Revoked())
	assert.Equal(t, "renamed", got2.Name)

	require.NoError(t, s.Delete(ctx, id))
	_, err = s.Get(ctx, id)
	require.ErrorIs(t, err, oauthserver.ErrClientNotFound)
	require.ErrorIs(t, s.Update(ctx, got), oauthserver.ErrClientNotFound)
}

func TestPgStoreListTenantFilter(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	a, b := "client_"+t.Name()+"_a", "client_"+t.Name()+"_b"
	_ = s.Delete(ctx, a)
	_ = s.Delete(ctx, b)
	require.NoError(t, s.Create(ctx, sample(a, "tenant-list-1")))
	require.NoError(t, s.Create(ctx, sample(b, "tenant-list-2")))

	t1, err := s.List(ctx, "tenant-list-1")
	require.NoError(t, err)
	require.Len(t, t1, 1)
	assert.Equal(t, a, t1[0].ID)

	all, err := s.List(ctx, "")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(all), 2)
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `FORGE_TEST_POSTGRES_DSN="" go test ./auth/oauthserver/pgstore/` — compile error expected (`undefined: pgstore.New`). If you have a local Postgres, run with the DSN set; otherwise the orchestrator threads an ephemeral docker DSN (repo precedent: `docker run --rm -d -p 5432:5432 -e POSTGRES_PASSWORD=test postgres:16-alpine`, DSN `postgres://postgres:test@localhost:5432/postgres?sslmode=disable`).

- [ ] **Step 4: Write the implementation**

`auth/oauthserver/pgstore/doc.go`:

```go
// Package pgstore is the Postgres oauthserver.Store: the oauth_clients
// table behind the client registry. Apply Migrations via data/migration
// (its .sql files sit at the fs root) before use; the pgxpool's lifecycle
// belongs to the caller.
//
//	require.NoError(t, migration.New(pgstore.Migrations,
//	    migration.WithTable("forge_oauthserver_schema")).Up(ctx, db))
//	store := pgstore.New(pool)
//	srv, err := oauthserver.New(signer, store, ...)
package pgstore
```

`auth/oauthserver/pgstore/pgstore.go`:

```go
package pgstore

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dmitrymomot/forge/auth/oauthserver"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations holds the goose migration creating oauth_clients, rooted so
// its .sql files sit at fsys root (data/migration.New globs the root, not
// subdirectories). Apply via data/migration under its own version table.
var Migrations fs.FS

func init() {
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		panic(err) // unreachable: migrations/*.sql is embedded at compile time
	}
	Migrations = sub
}

// Store is the Postgres oauthserver.Store. The pool's lifecycle is the caller's.
type Store struct {
	pool *pgxpool.Pool
}

// New builds a Postgres client registry. Apply Migrations first.
func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

const columns = `id, name, secret_hash, scopes, grants, redirect_uris, tenant_id, token_ttl_ms, revoked_at, created_at`

const createSQL = `
INSERT INTO oauth_clients (` + columns + `)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`

// Create inserts the client; an existing id yields ErrDuplicateClient.
func (s *Store) Create(ctx context.Context, c oauthserver.Client) error {
	_, err := s.pool.Exec(ctx, createSQL,
		c.ID, c.Name, c.SecretHash,
		emptyIfNil(c.Scopes), emptyIfNil(c.Grants), emptyIfNil(c.RedirectURIs),
		c.TenantID, c.TokenTTL.Milliseconds(), nullTime(c.RevokedAt), c.CreatedAt)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return oauthserver.ErrDuplicateClient
	}
	return err
}

const getSQL = `SELECT ` + columns + ` FROM oauth_clients WHERE id = $1`

// Get fetches one client by id.
func (s *Store) Get(ctx context.Context, id string) (oauthserver.Client, error) {
	return scanClient(s.pool.QueryRow(ctx, getSQL, id))
}

const updateSQL = `
UPDATE oauth_clients
SET name = $2, secret_hash = $3, scopes = $4, grants = $5, redirect_uris = $6,
    tenant_id = $7, token_ttl_ms = $8, revoked_at = $9
WHERE id = $1`

// Update rewrites the client row; a missing id yields ErrClientNotFound.
func (s *Store) Update(ctx context.Context, c oauthserver.Client) error {
	tag, err := s.pool.Exec(ctx, updateSQL,
		c.ID, c.Name, c.SecretHash,
		emptyIfNil(c.Scopes), emptyIfNil(c.Grants), emptyIfNil(c.RedirectURIs),
		c.TenantID, c.TokenTTL.Milliseconds(), nullTime(c.RevokedAt))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return oauthserver.ErrClientNotFound
	}
	return nil
}

const listSQL = `
SELECT ` + columns + ` FROM oauth_clients
WHERE ($1 = '' OR tenant_id = $1)
ORDER BY created_at, id`

// List returns the tenant's clients; tenantID "" returns all.
func (s *Store) List(ctx context.Context, tenantID string) ([]oauthserver.Client, error) {
	rows, err := s.pool.Query(ctx, listSQL, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []oauthserver.Client
	for rows.Next() {
		c, err := scanClient(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Delete removes the client row; deleting a missing id is a no-op.
func (s *Store) Delete(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM oauth_clients WHERE id = $1`, id)
	return err
}

type rowScanner interface{ Scan(dest ...any) error }

func scanClient(row rowScanner) (oauthserver.Client, error) {
	var c oauthserver.Client
	var ttlMS int64
	var revoked *time.Time
	err := row.Scan(&c.ID, &c.Name, &c.SecretHash, &c.Scopes, &c.Grants,
		&c.RedirectURIs, &c.TenantID, &ttlMS, &revoked, &c.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return oauthserver.Client{}, oauthserver.ErrClientNotFound
	}
	if err != nil {
		return oauthserver.Client{}, err
	}
	c.TokenTTL = time.Duration(ttlMS) * time.Millisecond
	if revoked != nil {
		c.RevokedAt = *revoked
	}
	return c, nil
}

// nullTime maps the zero time to SQL NULL.
func nullTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// emptyIfNil keeps nil slices out of pgx array encoding.
func emptyIfNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
```

- [ ] **Step 5: Run the integration test live**

Start an ephemeral Postgres if none is running, run the test against it, then stop it:

```bash
docker run --rm -d --name forge-oauth-pg -p 55432:5432 -e POSTGRES_PASSWORD=test postgres:16-alpine
sleep 3
FORGE_TEST_POSTGRES_DSN="postgres://postgres:test@localhost:55432/postgres?sslmode=disable" go test -race ./auth/oauthserver/pgstore/ -v
docker stop forge-oauth-pg
```

Expected: PASS (both tests, running against live Postgres — not skipped).

- [ ] **Step 6: Format, lint, commit**

```bash
just fmt ./auth/oauthserver/...
just lint
git add auth/oauthserver/pgstore
git commit -m "feat(oauthserver): postgres client registry driver"
```

---

### Task 14: cross-package integration test + catalog cleanup

**Files:**
- Create: `auth/oauthserver/integration_test.go`
- Modify: `docs/packages.md` (delete the `auth/oauthclient` and `auth/oauthserver` entries)

**Interfaces:**
- Consumes: the complete public APIs of both packages (everything above).
- Produces: proof the mirror recipe works end-to-end; catalog updated per the roadmap rule ("the moment a package ships, delete its entry").

- [ ] **Step 1: Write the integration test**

`auth/oauthserver/integration_test.go`:

```go
package oauthserver_test

// The mirror recipe, end to end: a real oauthclient logs a user in against
// a real oauthserver over HTTP — the exact wiring a white-label mirror or
// trusted first-party app uses (see both doc.go files).

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/oauthclient"
	"github.com/dmitrymomot/forge/auth/oauthserver"
	"github.com/dmitrymomot/forge/crypto/keyset"
)

func TestMirrorLoginViaOauthclient(t *testing.T) {
	// --- central auth server (auth.platform.com) ---
	signer := testSigner(t)
	mux := http.NewServeMux()
	authSrv := httptest.NewServer(mux)
	t.Cleanup(authSrv.Close)

	cfg := oauthserver.DefaultConfig()
	cfg.Issuer = authSrv.URL

	srv, err := oauthserver.New(signer, oauthserver.NewMemoryStore(),
		oauthserver.WithConfig(cfg),
		oauthserver.WithCodeStore(cacheStore(t)),
		oauthserver.WithCodeKeyset(testKeyset(t)),
		// Central session exists — the authenticator answers instantly,
		// which is exactly the SSO-across-mirrors behavior.
		oauthserver.WithAuthenticator(staticUser("user-42")),
		oauthserver.WithUserClaims(func(ctx context.Context, subject string) (map[string]any, error) {
			return map[string]any{"email": "u42@example.com", "email_verified": true, "name": "User 42"}, nil
		}),
	)
	require.NoError(t, err)

	authorize, err := srv.AuthorizeHandler()
	require.NoError(t, err)
	mux.Handle("GET /oauth/authorize", authorize)
	mux.Handle("POST /oauth/token", srv.TokenHandler())
	mux.Handle("GET /.well-known/jwks.json", signer.JWKS())

	creds, err := srv.CreateClient(context.Background(), oauthserver.CreateClientInput{
		Name:         "mirror-1",
		Grants:       []string{oauthserver.GrantAuthorizationCode},
		Scopes:       []string{"profile"},
		RedirectURIs: []string{"https://mirror1.example.com/auth/callback"},
	})
	require.NoError(t, err)

	// --- mirror app: oauthclient with a hand-built Provider (the recipe) ---
	flowKeys, err := keyset.New(keyset.WithPrimary(1, []byte("fedcba9876543210fedcba9876543210")))
	require.NoError(t, err)
	mirror, err := oauthclient.New(flowKeys,
		oauthclient.WithRedirectURL("https://mirror1.example.com/auth/callback"),
		oauthclient.WithHTTPClient(authSrv.Client()),
		oauthclient.WithProvider("platform", oauthclient.Provider{
			ClientID:     creds.ClientID,
			ClientSecret: creds.ClientSecret,
			AuthURL:      authSrv.URL + "/oauth/authorize",
			TokenURL:     authSrv.URL + "/oauth/token",
			JWKSURL:      authSrv.URL + "/.well-known/jwks.json",
			Issuer:       authSrv.URL,
			Scopes:       []string{"profile"},
		}))
	require.NoError(t, err)

	// 1. mirror starts the flow
	flow, err := mirror.AuthURL(context.Background(), "platform", oauthclient.WithReturnTo("/lobby"))
	require.NoError(t, err)

	// 2. the "browser" follows the authorize URL; the server SSO-redirects
	//    straight back to the mirror callback with a code
	browser := &http.Client{
		Transport:     authSrv.Client().Transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := browser.Get(flow.URL)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusFound, resp.StatusCode)
	loc, err := url.Parse(resp.Header.Get("Location"))
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(loc.String(), "https://mirror1.example.com/auth/callback"),
		"authorize redirected to %s", loc)

	// 3. mirror completes the flow with the callback query
	res, err := mirror.Exchange(context.Background(), flow.FlowToken, loc.Query())
	require.NoError(t, err)
	assert.Equal(t, "user-42", res.Identity.Subject)
	assert.Equal(t, "u42@example.com", res.Identity.Email)
	assert.True(t, res.Identity.EmailVerified)
	assert.Equal(t, "User 42", res.Identity.Name)
	assert.Equal(t, "/lobby", res.ReturnTo)
	assert.Equal(t, "platform", res.Identity.Provider)
	assert.NotEmpty(t, res.Token.AccessToken)

	// 4. a second login reuses nothing from the first (fresh code, fresh state)
	flow2, err := mirror.AuthURL(context.Background(), "platform")
	require.NoError(t, err)
	resp2, err := browser.Get(flow2.URL)
	require.NoError(t, err)
	require.NoError(t, resp2.Body.Close())
	loc2, err := url.Parse(resp2.Header.Get("Location"))
	require.NoError(t, err)
	res2, err := mirror.Exchange(context.Background(), flow2.FlowToken, loc2.Query())
	require.NoError(t, err)
	assert.Equal(t, "user-42", res2.Identity.Subject)
}
```

- [ ] **Step 2: Run the integration test**

Run: `go test -race ./auth/oauthserver/ -run TestMirrorLogin -v`
Expected: PASS. If the id_token verification fails on audience, check that the oauthclient verifier pins `WithAudience(creds.ClientID)` and the id_token sets `aud` to the client id — both are in the specs of Tasks 4 and 11.

- [ ] **Step 3: Delete the shipped entries from the catalog**

In `docs/packages.md`, delete the two entry blocks (each is a `**auth/oauthclient**` / `**auth/oauthserver**` heading through its `Deps:` line and trailing `---` separator, around lines 688–708). Keep mentions of `oauthserver`/`oauthclient` inside OTHER entries (e.g. `auth/scim`'s auth note) untouched. If the file header carries package counts, decrement them by 2.

- [ ] **Step 4: Full sweep**

Run: `just fmt ./auth/... && just lint && just test ./auth/...`
Expected: everything green.

- [ ] **Step 5: Commit**

```bash
git add auth/oauthserver/integration_test.go docs/packages.md
git commit -m "test(auth): oauthclient-against-oauthserver mirror login integration; drop shipped catalog entries"
```

---

## Post-implementation

- Open one PR for the bundle branch. PR body: package summaries, security decisions (PKCE-always, sealed single-use codes, dummy-hash timing defense, RFC-error exception to problem+json), benchmark numbers from Tasks 6 and 12, and the pgstore live-test evidence.
- Follow repo PR flow: wait for CI, fix failures, read Claude's review, fix real findings, resolve threads, repeat until green.
- No AI attribution anywhere in the PR.
