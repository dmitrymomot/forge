// Package geoip resolves a client IP to geographic and network facts (country,
// region, city, timezone, ASN) behind a pluggable Source seam, caching a
// Location in the request context the way web/clientip caches the IP.
//
// Two seams: the IP-based [Locator] primitive (call it directly from non-HTTP
// code) and the request-based [Source] (header sources and the middleware).
// Header sources ([Cloudflare], [CloudFront], [Vercel], [Headers]) read a CDN's
// geo headers; [FromLocator] bridges an IP [Locator] (e.g. web/geoip/mmdb) into
// a Source; [Chain] tries the CDN header first and falls back to the database:
//
//	reader, _ := mmdb.New(mmdb.WithCity(cityPath), mmdb.WithASN(asnPath))
//	src := geoip.Chain(geoip.Cloudflare(), geoip.FromLocator(reader))
//	mux2 := geoip.Middleware(src)(mux)
//	// in a handler: loc := geoip.Get(r)
//
// Wire LogExtractor into the logger so every log line during a request carries
// the geo group:
//
//	logger.New(logger.WithContextExtractors(geoip.LogExtractor))
package geoip
