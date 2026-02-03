// Package clientip extracts real client IP addresses from HTTP requests.
//
// This package handles various proxy headers in priority order to determine the
// actual client IP address, which is essential for rate limiting, geolocation,
// and security logging in web applications behind proxies, load balancers, or CDNs.
//
// The package checks headers in this specific order:
//  1. CF-Connecting-IP (Cloudflare)
//  2. DO-Connecting-IP (DigitalOcean App Platform)
//  3. X-Forwarded-For (most common proxy header)
//  4. X-Real-IP (nginx and other proxies)
//  5. RemoteAddr (direct connection)
//
// Basic usage:
//
//	import "github.com/dmitrymomot/forge/pkg/clientip"
//
//	func handleRequest(w http.ResponseWriter, r *http.Request) {
//		clientIP := clientip.GetIP(r)
//		log.Printf("Request from IP: %s", clientIP)
//
//		// Use for rate limiting, geolocation, security logging, etc.
//	}
//
// The GetIP function validates all IP addresses and handles edge cases:
//   - Invalid IP strings are rejected and fallback headers are checked
//   - IPv6 addresses are properly parsed and normalized
//   - X-Forwarded-For with multiple IPs uses the leftmost (original client) IP
//   - The special address 0.0.0.0 is rejected as invalid
//   - If no valid IP is found, returns the raw RemoteAddr as fallback
//
// The function never panics and always returns a string, making it safe for
// production use in high-traffic applications.
package clientip
