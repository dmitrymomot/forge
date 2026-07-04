// Package password hashes and verifies user passwords. The default is Argon2id with a
// self-describing PHC string; bcrypt is available as a fallback and migration source.
//
// Verify detects the algorithm from the encoded prefix, compares in constant time, and
// reports needsRehash when the stored parameters or algorithm differ from the current
// defaults — bcrypt-stored hashes always request a rehash to Argon2id. A wrong password
// returns ok=false with a nil error; only a malformed encoding returns ErrInvalidHash.
// Argon2 parameters are shared with package kdf.
//
// # Usage
//
//	enc, _ := password.Hash(plaintext)
//	ok, needsRehash, err := password.Verify(plaintext, enc)
//	if err == nil && ok && needsRehash {
//		newEnc, _ := password.Hash(plaintext) // upgrade stored hash transparently
//	}
package password
