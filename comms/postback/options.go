package postback

import (
	"net/http"
	"time"

	"github.com/dmitrymomot/forge/web/httpclient"
)

type config struct {
	client *http.Client
}

func newConfig(opts ...Option) config {
	var c config
	for _, o := range opts {
		o(&c)
	}
	if c.client == nil {
		c.client = httpclient.New(httpclient.WithTimeout(10 * time.Second))
	}
	return c
}

// Option configures the Sender.
type Option func(*config)

// WithHTTPClient replaces the default client — the seam for custom retry
// policy (e.g. retrying POST destinations), per-attempt timeouts, breakers,
// or a test server's client. Nil is ignored.
func WithHTTPClient(client *http.Client) Option {
	return func(c *config) {
		if client != nil {
			c.client = client
		}
	}
}
