# auth/oauthclient + auth/oauthserver — Design Spec

Date: 2026-07-13
Status: approved design, pre-plan

Two packages, one bundle branch/PR:

- **`auth/oauthclient`** — OAuth2/OIDC *login client*: authorization-code +
  PKCE against external IdPs (Google, GitHub, any OIDC issuer) and against
  forge's own `oauthserver`. Login-only: the product is a verified identity;
  ongoing provider-API access (refresh rotation, token persistence) is
  consumer domain.
- **`auth/oauthserver`** — OAuth2 provider for two audiences only:
  machine-to-machine partners (`client_credentials`) and **first-party
  trusted apps / mirrors** (`authorization_code` + PKCE). Explicitly NOT a
  third-party IdP: no consent screens, no external/dynamic client
  registration, no discovery metadata, no userinfo endpoint, no JWE, no
  refresh tokens.

Both packages work single- and multi-tenant per the standing forge rule:
zero ceremony single-tenant, optional fail-closed scope hook multi-tenant.

---

## 1. auth/oauthclient

### Product decision

Login-only identity broker (decision A). `Result` exposes the raw token
response once so a consumer *can* store tokens and build API access in
their own repo; the package ships no refresh rotation and no token Store.

### Construction

```go
client, err := oauthclient.New(
    oauthclient.WithKeyset(ks),                          // flow-token signing (crypto/token)
    oauthclient.WithRedirectURL("https://app.io/auth/callback"), // client-wide default
    oauthclient.WithProvider("google", oauthclient.Google(googleCfg)),
    oauthclient.WithProvider("github", oauthclient.GitHub(githubCfg)),
    oauthclient.WithProviderSource(loadTenantIdP),       // optional per-tenant lookup
)
```

Env-loadable `Config{RedirectURL, CookieName, FlowTTL}` with
`OAUTHCLIENT_*` tags + `DefaultConfig` + `Validate`. Flow TTL default
10 min; cookie name default `oauth_flow`.

### Providers

```go
type ProviderConfig struct {
    ClientID     string   `env:"CLIENT_ID,required"`
    ClientSecret string   `env:"CLIENT_SECRET,required"`
    RedirectURL  string   `env:"REDIRECT_URL"`  // overrides client-wide default
    Scopes       []string `env:"SCOPES"`        // empty → preset defaults
    AuthParams   map[string]string              // extra authorize params (prompt, hd, allow_signup…)
}

func Google(cfg ProviderConfig) Provider   // pinned endpoints; default scopes: openid email profile
func GitHub(cfg ProviderConfig) Provider   // pinned endpoints; default scopes: read:user user:email
func Discover(ctx context.Context, issuer string, cfg ProviderConfig) (Provider, error)
```

- `Provider` is a plain exported struct: ClientID, ClientSecret, AuthURL,
  TokenURL, JWKSURL, Issuer, Scopes, RedirectURL, AuthParams, and an
  optional `Identity func(ctx, *http.Client, TokenResponse) (Identity, error)`
  hook for non-OIDC providers — presets set it (GitHub: `GET /user` +
  `GET /user/emails`); when nil the OIDC id_token path is required.
  Hand-building a Provider is the canonical recipe for odd providers and
  for pointing at forge's own `oauthserver`.
- `Discover` fetches `/.well-known/openid-configuration` once and returns a
  filled Provider. Called at config/tenant-onboarding time, never per
  request; consumers cache the result (e.g. their tenant-IdP DB row).
- Nested env tags give `OAUTH_GOOGLE_CLIENT_ID` etc. for free.
- Presets: Google + GitHub only. Microsoft and Apple are explicitly out
  (Apple's ES256-JWT client secret is its own project); both are addable
  later as presets without API change.

**Resolution order** (used by `Begin`/`AuthURL` and `Complete`/`Exchange`):
static registry first, then `ProviderSource(ctx, name)` — the multi-tenant
seam (tenant read from ctx inside the consumer's func). Unknown provider →
`ErrUnknownProvider`; source errors propagate (fail-closed).

### Flow API — transport-neutral core + cookie convenience

```go
// Core (SPA / separate JS frontend / mobile / custom transports):
func (c *Client) AuthURL(ctx context.Context, provider string, opts ...BeginOption) (*Flow, error)
type Flow struct{ URL, FlowToken string }
func (c *Client) Exchange(ctx context.Context, flowToken string, callback url.Values) (*Result, error)

// Cookie wrappers (server-rendered / same-site BFF), thin over the core:
func (c *Client) Begin(w http.ResponseWriter, r *http.Request, provider string, opts ...BeginOption) error
func (c *Client) Complete(w http.ResponseWriter, r *http.Request) (*Result, error)
```

- Flow state = sealed `crypto/token` blob (signed, TTL'd, purpose-bound):
  `{provider, state, pkceVerifier, nonce, scopeBinding, returnTo}`.
  `Begin` puts it in an HttpOnly/Secure/SameSite=Lax cookie (Lax survives
  the top-level redirect back); `AuthURL` hands it to the caller as
  `FlowToken` — same blob, cookie is just one transport.
- Separate-frontend recipe (SvelteKit/Next): API returns `{url, flow_token}`
  JSON → frontend server route keeps flow_token in its own cookie → IdP
  redirects to the frontend callback → frontend POSTs `{flow_token + query}`
  to the API → `Exchange`.
- `Begin` = AuthURL + cookie + 303 redirect. `Complete` = read+delete cookie
  + `Exchange`. One code path for the security-critical parts.
- PKCE S256 on every flow. OIDC nonce on every OIDC flow.
- `BeginOption`: `WithReturnTo(path)` (round-tripped in the flow blob,
  returned in `Result`). Provider `AuthParams` merge into the authorize URL;
  reserved params (state, nonce, code_challenge*, client_id, redirect_uri,
  response_type, scope) collide → construction-time/Begin error, never
  silent override.

`Exchange` steps: parse+verify flow token (TTL, purpose) → constant-time
state check → surface `error=` callback params as `*ProviderError` →
resolve provider again (registry/source with ctx) → scope-binding check →
code exchange over injected `web/httpclient` (PKCE verifier attached) →
identity:

- **OIDC path** (Identity hook nil): id_token verified via `auth/jwt`
  (alg pinned per provider, JWKS from provider config), iss/aud checked,
  nonce must match the flow; profile fields from id_token claims.
- **Hook path** (GitHub preset, odd providers, minimal-id_token IdPs):
  `Provider.Identity` fetches identity — GitHub: `GET /user` +
  `GET /user/emails` (primary verified email).

```go
type Result struct {
    Identity Identity      // Provider, Subject, Email, EmailVerified, Name, Picture, Raw map[string]any
    Token    TokenResponse // AccessToken, TokenType, RefreshToken, ExpiresAt, IDToken, Scope — raw, exposed once
    Provider string
    ReturnTo string
}
```

### Multi-tenancy

`WithScope(func(ctx) (string, error))` — when set, the value is computed at
`Begin`/`AuthURL`, sealed into the flow blob, recomputed at
`Complete`/`Exchange`, and must match (`ErrScopeBinding`), fail-closed on
hook error. A flow started in tenant A cannot finish in tenant B.
White-label custom domains additionally get natural binding from
host-scoped cookies. Per-tenant IdP configs go through `ProviderSource`.

### Errors

`ErrUnknownProvider`, `ErrFlowExpired`, `ErrStateMismatch`,
`ErrNonceMismatch`, `ErrScopeBinding`, plus typed
`*ProviderError{Code, Description}` for `error=` callbacks and RFC-shaped
token-endpoint rejections. Single-line, `errors.Is`-matchable.

### Dependencies

`auth/jwt`, `web/httpclient`, `crypto/token` (+`crypto/keyset`),
`core/random`. Stateless — no Store. No x/oauth2, no new external deps.

---

## 2. auth/oauthserver

### Construction

```go
srv, err := oauthserver.New(signer, store, opts...) // signer *jwt.Signer, store oauthserver.Store
```

Env-loadable `Config{Issuer, Audience, TokenTTL}` (`OAUTHSERVER_ISSUER`,
`OAUTHSERVER_AUDIENCE`, `OAUTHSERVER_TOKEN_TTL`, TTL default 15m) +
`DefaultConfig` + `Validate`. Keys live in the injected `jwt.Signer`; the
package never touches key material. JWKS is `signer.JWKS()` mounted by the
consumer — documented recipe, no wrapper.

### Client registry

```go
type Client struct {
    ID           string        // prefixed, e.g. "client_…" (core/id)
    Name         string
    SecretHash   []byte        // SHA-256 digest of high-entropy secret
    Scopes       []string      // allowlist for client_credentials + authorize
    Grants       []string      // {"client_credentials"} | {"authorization_code"} | both
    RedirectURIs []string      // required for authorization_code; exact match only
    TenantID     string        // empty in single-tenant
    TokenTTL     time.Duration // 0 → server default
    RevokedAt    time.Time
    CreatedAt    time.Time
}
```

Management methods on `Server`:

- `CreateClient(ctx, CreateClientInput{Name, Scopes, Grants, RedirectURIs, TokenTTL})
  → *ClientCredentials` — secret generated (`core/random`), plaintext
  returned exactly once, hash stored.
- `RotateSecret(ctx, id) → *ClientCredentials`
- `RevokeClient(ctx, id)` — soft (RevokedAt). New tokens stop immediately;
  **outstanding JWTs remain valid ≤ TTL** — documented loudly. No
  introspection/revocation endpoints (pointless at 15m TTLs) — documented.
- `GetClient(ctx, id)`, `ListClients(ctx)`

Store seam: `Create/Get/Update/List/Delete` + in-memory impl in-package.
Driver: `oauthserver/pgstore` (pgx, `migration.Group`-compatible migration,
`oauth_clients` table).

### Token endpoint — `srv.TokenHandler()`, POST only

Client authentication (both grants): `client_secret_basic` and
`client_secret_post`; digest compare constant-time (`crypto/consttime`);
unknown client_id → dummy compare (no enumeration timing); revoked →
`invalid_client`.

Responses per RFC 6749 (`Cache-Control: no-store`). **Errors use RFC §5.2
JSON, not problem+json** — partners' OAuth libraries expect it;
`invalid_client` → 401 + `WWW-Authenticate`. Documented exception to the
fleet error contract. Internal Go error text never reaches response bodies.

**grant_type=client_credentials** (M2M partners):

1. Grant allowed for this client (`Grants`), else `unauthorized_client`.
2. Scopes: requested ⊆ client allowlist else `invalid_scope` (explicit
   reject, no silent narrowing); omitted → full allowed set.
3. JWT via signer: `iss`, `sub`=client_id, `aud` (config), `exp`
   (client TTL override or default), `iat`, `jti` (`core/id`), `scope`,
   `tenant` when the client record carries one.
4. `{access_token, token_type: "Bearer", expires_in, scope}`.

**grant_type=authorization_code** (first-party apps/mirrors):

1. Client auth as above; grant allowed.
2. Unseal code (`crypto/token`): expiry, client_id match, redirect_uri
   match.
3. **Single-use:** atomic SetNX claim on the code's `jti` in
   `resilience/cache.Store` (`WithCodeStore`) — already-claimed →
   `invalid_grant`. Memory store OK single-instance; redis for fleets
   (documented).
4. PKCE: S256(code_verifier) must equal the sealed challenge — mandatory.
5. Response: `access_token` (JWT: `sub`=user subject, `scope`, `tenant`
   from client record) + `id_token` (`aud`=client_id, `nonce` echoed,
   claims from `WithUserClaims` hook) + `expires_in`. **No refresh token** —
   the app creates its own local session from the result.

### Authorize endpoint — `srv.AuthorizeHandler()`, GET

First-party user flow (mirrors/trusted apps). The three auth-code inputs —
`WithAuthenticator`, `WithCodeStore`, and `WithCodeKeyset` (the
`crypto/token` keyset that seals codes; the jwt.Signer's key material is
not accessible for HMAC sealing) — are optional at `New` (an M2M-only
server needs none); fail-closed at use instead:
`AuthorizeHandler() (http.Handler, error)` errors unless all three are
set, and the token endpoint rejects `authorization_code` with
`unsupported_grant_type` when the code store or code keyset is missing.

1. Validate `response_type=code`, client exists + grant allowed,
   `redirect_uri` exactly ∈ allowlist, `code_challenge` present with
   `method=S256`. Invalid client or redirect_uri → 400 rendered locally
   (**never** redirect to an unvalidated URI); other errors → RFC error
   redirect (`error=…&state=…`).
2. `WithAuthenticator(func(w, r) (subject string, ok bool))` — the
   consumer's seam. `ok=false` ⇒ the authenticator already wrote the
   response (redirect to the consumer's login page, which returns to the
   full /authorize URL). An existing central session returns immediately ⇒
   SSO across mirrors.
3. Issue code: sealed `crypto/token` blob
   `{jti, client_id, redirect_uri, subject, scope, nonce, code_challenge}`,
   TTL ≈ 60s → `302 redirect_uri?code=…&state=…`.

`WithUserClaims(func(ctx, subject string) (map[string]any, error))` —
optional id_token enrichment (email/name/roles) so apps skip a post-login
lookup.

Trusted apps configure `oauthclient` with a hand-built
`Provider{AuthURL, TokenURL, JWKSURL, Issuer}` — canonical recipe in both
doc.go files.

### Multi-tenancy

`WithScope(func(ctx) (string, error))` scopes the **management** methods
(create stamps TenantID; get/list/revoke/rotate filter by it; fail-closed
on hook error). **Issuance derives tenant from the client record**, not
ctx: client_ids are globally unique and the security boundary is the
`tenant` claim verified by resource APIs. Rationale: ctx-scoping the token
endpoint would force per-tenant token URLs and break the global-endpoint
layout; the claim check is what actually isolates tenants. White-label
mirrors are first-party clients carrying their tenant's ID.

### Errors

Management sentinels: `ErrClientNotFound`, `ErrClientRevoked`,
`ErrDuplicateClient`. Wire errors are RFC JSON only. Brute-force
throttling composes from `resilience/ratelimit` middleware — documented,
not built in.

### Dependencies

`auth/jwt`, `core/id`, `core/random`, `crypto/digest`, `crypto/consttime`,
`crypto/token`, `resilience/cache` (auth-code path only); `pgstore` → pgx.
No new external deps.

### Size note

Two grants sharing one registry, one token endpoint, and JWKS put the
package at ~900 LOC — top of the band; splitting would duplicate the
registry and endpoint plumbing for no isolation gain.

---

## 3. Cross-cutting

### Testing (black-box, `package X_test`)

- **oauthclient:** httptest fake IdP (authorize/token/userinfo/JWKS,
  id_tokens signed with a test key). Cases: OIDC happy path (nonce/iss/aud
  verified), GitHub-shaped non-OIDC path (user + emails), `Discover`,
  state mismatch, expired flow, `error=` callback, PKCE verifier present in
  exchange, scope-binding mismatch, cookie + flow-token transports,
  AuthParams merge + reserved-param collision.
- **oauthserver:** handler-level tests verifying issued JWTs with a real
  `jwt.Verifier` against the signer's JWKS. Cases: both client-auth
  methods, bad/rotated/revoked secrets, scope subset/superset/omitted,
  grant not allowed, RFC error shapes, management lifecycle
  (create → token → rotate → old secret fails → revoke → invalid_client),
  tenancy fail-closed; authorize flow: bad redirect_uri (local 400),
  missing PKCE, unauthenticated → seam handles, code replay (SetNX),
  wrong verifier, nonce echo.
- **Cross-package integration:** real `oauthclient` completes a login
  against a real `oauthserver` (the mirror recipe), end-to-end.
- **pgstore:** live-Postgres integration test (ephemeral docker pg16,
  DSN threaded, per repo precedent).
- **Benchmarks** (repo rule): flow-token seal/parse + authorize-URL build;
  issuance path end-to-end + secret verify. Post-bench optimization pass.

### Anatomy & layout

Standard forge anatomy both packages (`doc.go` runnable example,
`config.go`, `options.go`, `errors.go`). oauthclient ≈ provider.go,
discover.go, flow.go, cookie transport, identity.go. oauthserver ≈
token endpoint, authorize endpoint, registry, store.go, memstore.go,
`pgstore/` driver.

### Deferred (documented, not built)

Apple + Microsoft presets · refresh/token-source client support (option B)
· public (secretless) clients · discovery/metadata endpoint · userinfo
endpoint · token introspection/revocation · consent screens / third-party
clients (anti-scope, run Hydra/Keycloak).

### Delivery

One branch, one bundle PR (both packages + pgstore + integration test).
On merge: delete both entries from `docs/packages.md` (roadmap rule).
