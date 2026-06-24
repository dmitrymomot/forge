package main

import (
	"fmt"
	"log"

	"github.com/dmitrymomot/forge/pkg/totp"
)

func main() {
	// Generate a base64-encoded encryption key for environment variables
	encodedKey, err := totp.GenerateEncodedEncryptionKey()
	if err != nil {
		log.Fatalf("Failed to generate encoded encryption key: %v", err)
	}

	// The Config struct env tag is ENCRYPTION_KEY; if you mount totp.Config under
	// a prefix (e.g. "TOTP_") it is read from TOTP_ENCRYPTION_KEY.
	fmt.Printf("Generated Encoded Encryption Key (for ENCRYPTION_KEY env var, or <PREFIX>_ENCRYPTION_KEY): \n———\n%s\n———\n", encodedKey)
}
