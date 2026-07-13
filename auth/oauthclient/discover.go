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
	defer func() { _ = resp.Body.Close() }()
	//nolint:nilaway // resp is non-nil whenever err is nil, per http.Client.Do's contract
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
