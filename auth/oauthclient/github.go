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
	//nolint:nilaway // resp is non-nil whenever err is nil, per http.Client.Do's contract
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
