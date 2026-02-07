// Package secrets provides AES-256-GCM encryption with compound key derivation
// for tenant-isolated data encryption in multi-tenant SaaS applications.
//
// A compound key model combines an application key and a workspace key via
// HKDF (RFC 5869) to derive unique encryption keys per tenant. Identical
// plaintext encrypted for different workspaces produces entirely different
// ciphertext, providing cryptographic tenant isolation.
//
// # Usage
//
// Generate keys and encrypt data:
//
//	appKey, err := secrets.GenerateKey()
//	if err != nil {
//		log.Fatal(err)
//	}
//	workspaceKey, err := secrets.GenerateKey()
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	encrypted, err := secrets.EncryptString("sensitive data", appKey, workspaceKey)
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	decrypted, err := secrets.DecryptString(encrypted, appKey, workspaceKey)
//	if err != nil {
//		log.Fatal(err)
//	}
//
// Binary data:
//
//	sealed, err := secrets.EncryptBytes(data, appKey, workspaceKey)
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	plain, err := secrets.DecryptBytes(sealed, appKey, workspaceKey)
//	if err != nil {
//		log.Fatal(err)
//	}
//
// # Tenant Isolation
//
// Different workspace keys produce different ciphertext for the same plaintext:
//
//	enc1, _ := secrets.EncryptString("hello", appKey, workspaceKey1)
//	enc2, _ := secrets.EncryptString("hello", appKey, workspaceKey2)
//	// enc1 != enc2 — different derived keys
//	// Decrypting enc1 with workspaceKey2 fails
//
// # Security Properties
//
// Key requirements:
//   - Both appKey and workspaceKey must be exactly 32 bytes (256 bits)
//   - Generate keys using [GenerateKey] (crypto/rand backed)
//   - Store keys securely (environment variables, key management service)
//
// Encryption:
//   - AES-256-GCM provides authenticated encryption (confidentiality + integrity)
//   - Unique random nonce generated per encryption operation
//   - HKDF-SHA256 derives per-tenant keys from compound key material
//   - Derived key material is zeroed after use
//   - Ciphertext tampering is detected during decryption
//
// # Error Handling
//
// All errors can be checked with [errors.Is]:
//   - [ErrInvalidAppKey]: app key is not 32 bytes
//   - [ErrInvalidWorkspaceKey]: workspace key is not 32 bytes
//   - [ErrEncryptionFailed]: encryption operation failed (wraps cause)
//   - [ErrDecryptionFailed]: decryption operation failed (wraps cause)
//   - [ErrInvalidCiphertext]: ciphertext is malformed or too short
//   - [ErrKeyDerivationFailed]: HKDF key derivation failed
//
// Errors are joined so both the operation and cause can be matched:
//
//	_, err := secrets.EncryptString("data", shortKey, workspaceKey)
//	errors.Is(err, secrets.ErrEncryptionFailed)  // true
//	errors.Is(err, secrets.ErrInvalidAppKey)      // true
package secrets
