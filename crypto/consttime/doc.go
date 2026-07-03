// Package consttime provides constant-time, length-safe equality for secrets — MACs,
// tokens, API keys, password hashes — removing the timing-attack footgun of == and
// bytes.Equal. It is the comparison primitive the rest of the crypto layer builds on.
//
//	if consttime.StringEqual(presentedAPIKey, storedAPIKey) {
//		// authenticated
//	}
package consttime
