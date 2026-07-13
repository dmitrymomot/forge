package totp

import (
	"encoding/base32"
	"fmt"

	"github.com/dmitrymomot/forge/core/random"
)

// GenerateSecret returns a new shared secret: cryptographically random
// bytes sized to the configured algorithm's hash length (RFC 4226 §4),
// encoded as unpadded uppercase base32 for authenticator-app entry.
func (t *TOTP) GenerateSecret() (string, error) {
	buf := make([]byte, t.cfg.algorithm.secretSize())
	if err := random.Read(buf); err != nil {
		return "", fmt.Errorf("totp: generate secret: %w", err)
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf), nil
}
