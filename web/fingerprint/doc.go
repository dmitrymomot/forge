// Package fingerprint turns an HTTP request into a versioned identity plus
// structured anti-fraud signals, from headers alone up to a full TLS + JS device
// probe. Layers are opt-in Collectors; heavy lookups (geoip, useragent) enter
// through wired function seams so the core stays stdlib-light. Output is facts,
// never a score — weighting signals into a decision is the consumer's policy.
//
// Collectors: Headers (UA + Accept*), ClientHints (Sec-CH-UA-* + Device-Memory
// + DPR — its high-entropy hints need the AcceptCH middleware to arrive),
// ClientIP, and the JS probe (JSCollector, served by ScriptHandler and fed by
// IngestHandler). TLS fingerprints come from the tlsprint subpackage:
// trusted-proxy header sources (Cloudflare/CloudFront/generic), a local
// raw-ClientHello JA4 computation, and RequestTLS for self-terminated crypto/tls.
//
// Hash churn from Client Hints: enabling Client Hints — as the Session and
// Antifraud presets now do by wiring in ClientHints() — adds ch-ua-* components,
// which changes Fingerprint.Hash and adds new keys to Digest.Parts for any
// request carrying Client Hints, with no schema Version bump. On the first
// comparison after upgrading, Drift reports those new component names as
// changed once; consumers who compare Hash for equality instead of using Drift
// will see every Client-Hint-bearing device re-fingerprint one time.
//
// Making tls-ua-mismatch fire: it stays inert until WithAutomationJA4 pins the
// JA4 fingerprints of non-browser clients you observe. Pinned TLS fingerprints
// drift as tools update, so harvest them from your own traffic rather than
// shipping a static list — wrap your listener with tlsprint.Listener, read
// tlsprint.Conn.JA4() per connection (via ConnContext), record the fingerprints
// arriving under automation User-Agents, and pass that map to WithAutomationJA4.
//
// Out of scope: JA4H (the HTTP-layer fingerprint) and the HTTP/2 SETTINGS
// fingerprint both need raw header/frame order, which net/http discards and
// HTTP/2 normalizes — they cannot be computed faithfully from a handler.
package fingerprint
