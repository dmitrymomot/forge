package webhook

import (
	"net/http"
	"time"

	"github.com/dmitrymomot/forge/web/httpclient"
)

type config struct {
	client      *http.Client
	scheme      Scheme
	keyHeader   string
	contentType string
}

func newConfig(opts ...Option) config {
	c := config{keyHeader: "Webhook-Id", contentType: "application/json"}
	for _, o := range opts {
		o(&c)
	}
	if c.scheme == nil {
		c.scheme = Stripe(WithSignatureHeader("Webhook-Signature"))
	}
	if c.client == nil {
		client := httpclient.New(httpclient.WithTimeout(10 * time.Second))
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		c.client = client
	}
	return c
}

// Option configures the Sender.
type Option func(*config)

// WithHTTPClient replaces the default client — the seam for custom timeouts,
// breakers, static headers, or a test server's client. A custom client keeps
// its own redirect policy (the default client never follows). Nil is ignored.
func WithHTTPClient(client *http.Client) Option {
	return func(c *config) {
		if client != nil {
			c.client = client
		}
	}
}

// WithScheme replaces the default outbound scheme (Stripe-style under
// "Webhook-Signature"). Nil is ignored.
func WithScheme(s Scheme) Option {
	return func(c *config) {
		if s != nil {
			c.scheme = s
		}
	}
}

// WithIdempotencyHeader renames the header carrying the delivery's
// idempotency key (default "Webhook-Id"). An empty name is ignored.
func WithIdempotencyHeader(name string) Option {
	return func(c *config) {
		if name != "" {
			c.keyHeader = name
		}
	}
}

// WithContentType overrides the outbound Content-Type (default
// "application/json"). An empty value is ignored.
func WithContentType(ct string) Option {
	return func(c *config) {
		if ct != "" {
			c.contentType = ct
		}
	}
}
