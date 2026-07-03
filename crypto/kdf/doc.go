// Package kdf derives key material. It has two jobs: HKDF turns one high-entropy master
// secret into many purpose-scoped, cryptographically unrelated keys (the info argument
// domain-separates them); DeriveKey turns a low-entropy user passphrase into key bytes
// via Argon2id.
//
//	cookieKey, _ := kdf.HKDF(master, salt, []byte("cookie-encryption"), 32)
//	tokenKey, _  := kdf.HKDF(master, salt, []byte("email-token-hmac"), 32)
//
//	key, _ := kdf.DeriveKey([]byte(passphrase), salt, kdf.DefaultParams())
//
// It is distinct from password (which verifies a login and never exposes the key) and
// keyset (which stores and rotates keys). Params is shared with package password.
package kdf
