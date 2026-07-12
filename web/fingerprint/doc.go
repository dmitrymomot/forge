// Package fingerprint turns an HTTP request into a versioned identity plus
// structured anti-fraud signals, from headers alone up to a full TLS + JS device
// probe. Layers are opt-in Collectors; heavy lookups (geoip, useragent) enter
// through wired function seams so the core stays stdlib-light. Output is facts,
// never a score — weighting signals into a decision is the consumer's policy.
package fingerprint
