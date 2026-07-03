// Package secret provides authenticated symmetric encryption (an AEAD "secret box") of
// bytes or strings, with versioned ciphertext for key rotation. The default is
// AES-256-GCM (stdlib); WithChaCha selects XChaCha20-Poly1305 for high-volume
// random-nonce use. It underpins cookie/session encryption, encrypted tokens, and field
// crypto.
//
//	box, _ := secret.New(key)            // 32-byte key
//	ct, _  := box.EncryptString(pii)
//	pt, _  := box.DecryptString(ct)
//
//	// rotation:
//	box, _ := secret.FromKeyset(ks)      // encrypt under primary, decrypt under any version
//
// Ciphertext is version-byte || nonce || ciphertext+tag. Any decryption failure returns
// ErrDecryptFailed without revealing the cause (no padding/key oracle).
package secret
