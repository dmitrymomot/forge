// Package digest provides convenience digest helpers — SHA-256/512 in raw, hex, and
// base64 form, HMAC-SHA256, and a streaming file hash — removing per-call boilerplate
// for ETags, cache keys, content addressing, and dedup. It deliberately excludes the
// insecure MD5 and SHA-1 digests.
//
// # Usage
//
//	etag := digest.SHA256Hex(body)
//	sum, err := digest.FileSHA256("/path/to/upload")
package digest
