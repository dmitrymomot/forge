package clientip

// Provider presets exist ONLY for edges that set a dedicated, reliable header
// they overwrite and strip on ingress. Generic XFF proxies (nginx, Caddy,
// Traefik, HAProxy, k8s ingress, cloud LBs) have no guaranteed dedicated header —
// use TrustPrivateProxies, TrustedRanges, TrustedHopCount, or XRealIP instead.

// Cloudflare trusts the CF-Connecting-IP header.
func Cloudflare() Option { return SingleHeader("CF-Connecting-IP") }

// Fastly trusts the Fastly-Client-IP header.
func Fastly() Option { return SingleHeader("Fastly-Client-IP") }

// CloudFront trusts the CloudFront-Viewer-Address header (its port is stripped).
func CloudFront() Option { return SingleHeader("CloudFront-Viewer-Address") }

// Akamai trusts the True-Client-IP header.
func Akamai() Option { return SingleHeader("True-Client-IP") }

// AzureFrontDoor trusts the X-Azure-ClientIP header.
func AzureFrontDoor() Option { return SingleHeader("X-Azure-ClientIP") }

// Envoy trusts the x-envoy-external-address header (Envoy's computed trusted
// client address).
func Envoy() Option { return SingleHeader("x-envoy-external-address") }

// XRealIP trusts the X-Real-IP header — the de-facto header nginx/Traefik/ingress
// set when configured. Only safe when your proxy always overwrites it.
func XRealIP() Option { return SingleHeader("X-Real-IP") }

// TrustPrivateProxies trusts all private/loopback/link-local/CGNAT/ULA ranges as
// proxies and returns the rightmost untrusted address — the standard setup for an
// app behind a reverse proxy on a private network.
func TrustPrivateProxies() Option {
	return func(c *config) { c.mode = modeTrustedRanges; c.trusted = PrivateRanges() }
}
