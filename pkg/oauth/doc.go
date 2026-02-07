// Package oauth provides OAuth2 authorization code flow implementations for common providers.
//
// This package includes a Provider interface and concrete implementations for Google and GitHub.
// Each provider handles the full OAuth2 flow: generating authorization URLs, exchanging codes
// for tokens, and fetching verified user information.
//
// # Features
//
//   - Provider interface for pluggable OAuth2 implementations
//   - Google OAuth2 with email verification
//   - GitHub OAuth2 with primary verified email resolution
//   - Functional options for custom HTTP clients (testing, custom transports)
//   - Configuration structs with env tags for environment-based setup
//   - Sentinel errors with "oauth:" prefix for consistent error handling
//
// # Usage
//
// Google provider setup:
//
//	provider, err := oauth.NewGoogleProvider(oauth.GoogleConfig{
//		ClientID:     os.Getenv("GOOGLE_OAUTH_CLIENT_ID"),
//		ClientSecret: os.Getenv("GOOGLE_OAUTH_CLIENT_SECRET"),
//		RedirectURL:  "https://example.com/auth/google/callback",
//	})
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	// Generate authorization URL
//	url := provider.AuthCodeURL("random-state-string")
//
//	// Exchange code for token (in callback handler)
//	token, err := provider.Exchange(ctx, code, "")
//	if err != nil {
//		// handle error
//	}
//
//	// Fetch user info
//	user, err := provider.FetchUserInfo(ctx, token)
//	if err != nil {
//		// handle error
//	}
//
// GitHub provider setup:
//
//	provider, err := oauth.NewGitHubProvider(oauth.GitHubConfig{
//		ClientID:     os.Getenv("GITHUB_OAUTH_CLIENT_ID"),
//		ClientSecret: os.Getenv("GITHUB_OAUTH_CLIENT_SECRET"),
//		RedirectURL:  "https://example.com/auth/github/callback",
//	})
//	if err != nil {
//		log.Fatal(err)
//	}
//
// # Custom Providers
//
// Implement the Provider interface to add support for other OAuth2 providers:
//
//	type MyProvider struct { /* ... */ }
//
//	func (p *MyProvider) Name() string { return "my-provider" }
//	func (p *MyProvider) AuthCodeURL(state string, opts ...oauth2.AuthCodeOption) string { /* ... */ }
//	func (p *MyProvider) Exchange(ctx context.Context, code, redirectURI string) (*oauth2.Token, error) { /* ... */ }
//	func (p *MyProvider) FetchUserInfo(ctx context.Context, token *oauth2.Token) (*oauth.UserInfo, error) { /* ... */ }
//
// # Testing
//
// Use WithHTTPClient to inject a test server for unit testing:
//
//	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
//		// mock responses
//	}))
//	defer ts.Close()
//
//	provider, err := oauth.NewGoogleProvider(cfg, oauth.WithHTTPClient(ts.Client()))
//
// # Error Handling
//
// The package provides sentinel errors for specific failure modes:
//
//   - ErrMissingClientID: Constructor called without client ID
//   - ErrMissingClientSecret: Constructor called without client secret
//   - ErrEmailNotVerified: Provider reports unverified email
//   - ErrFetchFailed: HTTP request to provider failed
//   - ErrNilResponse: Provider returned nil HTTP response
//   - ErrRequestFailed: Provider returned non-OK HTTP status
//   - ErrDecodeFailed: Failed to decode provider JSON response
//
// Use errors.Is for checking:
//
//	if errors.Is(err, oauth.ErrEmailNotVerified) {
//		// ask user to verify email
//	}
//
// # Security
//
//   - Always validate the state parameter to prevent CSRF attacks
//   - Use HTTPS redirect URIs in production
//   - Store tokens securely (encrypted at rest, never in URLs)
//   - Both providers enforce email verification before returning user info
//   - Keep client secrets out of source control (use environment variables)
package oauth
