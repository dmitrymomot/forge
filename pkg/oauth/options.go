package oauth

import "net/http"

// Option configures an OAuth provider.
type Option func(*options)

type options struct {
	httpClient *http.Client
}

// WithHTTPClient sets a custom HTTP client for OAuth requests.
// This is useful for testing with httptest servers or injecting
// custom transports (e.g., logging, retries).
func WithHTTPClient(client *http.Client) Option {
	return func(o *options) {
		o.httpClient = client
	}
}
