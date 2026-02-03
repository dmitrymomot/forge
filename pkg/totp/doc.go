// Package totp provides RFC 6238 compliant Time-based One-Time Password (TOTP) authentication
// with AES-256-GCM secret encryption and backup recovery codes.
//
// This package implements secure TOTP generation and validation with built-in secret encryption
// for database storage, recovery code generation, and full compatibility with authenticator apps.
// It supports all standard TOTP operations: secret generation, URI creation for QR codes,
// code validation with clock drift tolerance, and recovery code management for account recovery.
//
// # Basic Usage
//
// Generate and validate TOTP codes:
//
//	import "github.com/dmitrymomot/forge/pkg/totp"
//
//	// Generate a new secret
//	secret, err := totp.GenerateSecretKey()
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	// Create TOTP URI for QR codes
//	params := totp.TOTPParams{
//		Secret:      secret,
//		AccountName: "user@example.com",
//		Issuer:      "MyApp",
//	}
//
//	uri, err := totp.GetTOTPURI(params)
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	// Generate current TOTP code
//	code, err := totp.GenerateTOTP(secret)
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	// Validate user-provided code
//	valid, err := totp.ValidateTOTP(secret, "123456")
//	if err != nil {
//		log.Fatal(err)
//	}
//	fmt.Printf("Code valid: %t\n", valid)
//
// # Secret Encryption
//
// Encrypt secrets before storing in database:
//
//	// Load configuration
//	cfg := totp.Config{
//		EncryptionKey: "base64-encoded-32-byte-key",
//	}
//
//	// Get encryption key
//	key, err := totp.GetEncryptionKey(cfg)
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	// Encrypt secret for storage
//	encrypted, err := totp.EncryptSecret(secret, key)
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	// Decrypt when validating
//	decrypted, err := totp.DecryptSecret(encrypted, key)
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	valid, err := totp.ValidateTOTP(decrypted, userCode)
//
// # Recovery Codes
//
// Generate backup codes for account recovery:
//
//	// Generate 10 recovery codes
//	codes, err := totp.GenerateRecoveryCodes(10)
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	// Hash codes for secure storage
//	var hashedCodes []string
//	for _, code := range codes {
//		hashedCodes = append(hashedCodes, totp.HashRecoveryCode(code))
//	}
//
//	// Verify recovery code during authentication
//	userCode := "A1B2C3D4E5F6G7H8" // user input
//	if totp.VerifyRecoveryCode(userCode, hashedCodes[0]) {
//		fmt.Println("Recovery code valid")
//		// Remove used code from database
//	}
//
// # Time-based Testing
//
// Generate codes for specific times in tests:
//
//	testTime := time.Unix(1609459200, 0) // 2021-01-01 00:00:00 UTC
//	code, err := totp.GenerateTOTPWithTime(secret, testTime)
//	if err != nil {
//		log.Fatal(err)
//	}
//
// # Encryption Key Management
//
// Generate new encryption keys:
//
//	// Generate raw key bytes
//	key, err := totp.GenerateEncryptionKey()
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	// Generate base64-encoded key for config
//	encodedKey, err := totp.GenerateEncodedEncryptionKey()
//	if err != nil {
//		log.Fatal(err)
//	}
//	fmt.Printf("TOTP_ENCRYPTION_KEY=%s\n", encodedKey)
package totp
