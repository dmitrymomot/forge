// Package clientip resolves and caches the originating client IP.
//
// Install Middleware once with your topology, then call Get/From everywhere:
//
//	h := clientip.Middleware(clientip.TrustPrivateProxies())(mux)
//	// in a handler:
//	ip := clientip.Get(r)
//
// Wire LogExtractor into the logger so every log line during a request carries
// client_ip:
//
//	logger.New(logger.WithContextExtractors(clientip.LogExtractor))
//
// Strategies: RemoteAddrOnly (default, safe), SingleHeader, TrustedRanges,
// TrustedHopCount, LeftmostNonPrivate. Presets: Cloudflare, Fastly, CloudFront,
// Akamai, AzureFrontDoor, Envoy, XRealIP, TrustPrivateProxies.
package clientip
