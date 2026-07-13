package lockout

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// keys composes the failure-counter and lock-marker store keys for one caller
// identity. The caller key is SHA-256 hashed (first 16 bytes, hex) to keep
// PII out of store key listings and make arbitrary bytes store-safe —
// hygiene, not secrecy. With a scope hook configured the scope becomes a key
// segment; resolution failure or an empty scope fails closed with ErrScope.
func (l *Locker) keys(ctx context.Context, key string) (fails, lock string, err error) {
	scope := ""
	if l.cfg.scope != nil {
		s, err := l.cfg.scope(ctx)
		if err != nil {
			return "", "", fmt.Errorf("%w: %w", ErrScope, err)
		}
		if s == "" {
			return "", "", ErrScope
		}
		scope = s + ":"
	}
	sum := sha256.Sum256([]byte(key))
	h := hex.EncodeToString(sum[:16])
	return "lockout:" + scope + "f:" + h, "lockout:" + scope + "l:" + h, nil
}
