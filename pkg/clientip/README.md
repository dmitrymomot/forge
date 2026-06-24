# clientip

Extract the real client IP address from an HTTP request behind proxies, load balancers, and CDNs.

```go
import "github.com/dmitrymomot/forge/pkg/clientip"
```

## Purpose

`GetIP` resolves the originating client IP by checking common proxy headers in
priority order, then falling back to `r.RemoteAddr`. Every returned value is
validated with `net.ParseIP` and normalized via `ip.String()`. The function
never panics and always returns a string, making it safe for high-traffic
request paths (rate limiting, geolocation, security logging).

Header priority order:

1. `CF-Connecting-IP` (Cloudflare)
2. `DO-Connecting-IP` (DigitalOcean App Platform)
3. `X-Forwarded-For` (leftmost valid entry — the original client)
4. `X-Real-IP` (nginx and other proxies)
5. `r.RemoteAddr` (direct connection)

Invalid IPs are skipped and the next source is tried. The special address
`0.0.0.0` is rejected. IPv6 addresses are parsed and normalized (compressed).

## Usage

```go
func handleRequest(w http.ResponseWriter, r *http.Request) {
	clientIP := clientip.GetIP(r)
	log.Printf("Request from IP: %s", clientIP)
}
```

## Trust boundary (IMPORTANT)

`GetIP` **unconditionally trusts** the proxy headers above. They are fully
attacker-controlled: any client able to reach the application directly can set
`CF-Connecting-IP`, `X-Forwarded-For`, `X-Real-IP`, etc. to arbitrary values, so
the returned IP is **spoofable** unless every request is guaranteed to traverse
a trusted reverse proxy / load balancer / CDN that strips and rewrites these
headers.

- Use this package **only** when the application is deployed strictly behind such
  trusted infrastructure.
- If clients can connect directly, do **not** use the header-derived IP for any
  security decision (authentication, authorization, rate limiting, audit
  logging, IP allow/deny lists) — use `r.RemoteAddr` directly instead.
- There is no in-package allowlist of trusted proxies; enforcing the trust
  boundary is the deployer's responsibility.
