package hashx

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// SHA256 returns the SHA-256 digest of b.
func SHA256(b []byte) []byte { s := sha256.Sum256(b); return s[:] }

// SHA256Hex returns the lowercase-hex SHA-256 digest of b.
func SHA256Hex(b []byte) string { return hex.EncodeToString(SHA256(b)) }

// SHA256Base64 returns the unpadded standard-base64 SHA-256 digest of b.
func SHA256Base64(b []byte) string { return base64.RawStdEncoding.EncodeToString(SHA256(b)) }

// SHA512 returns the SHA-512 digest of b.
func SHA512(b []byte) []byte { s := sha512.Sum512(b); return s[:] }

// SHA512Hex returns the lowercase-hex SHA-512 digest of b.
func SHA512Hex(b []byte) string { return hex.EncodeToString(SHA512(b)) }

// HMACSHA256 returns the HMAC-SHA256 of msg under key.
func HMACSHA256(key, msg []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write(msg)
	return m.Sum(nil)
}

// HMACSHA256Hex returns the lowercase-hex HMAC-SHA256 of msg under key.
func HMACSHA256Hex(key, msg []byte) string { return hex.EncodeToString(HMACSHA256(key, msg)) }

// FileSHA256 streams the file at path and returns its lowercase-hex SHA-256 digest.
func FileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("hashx: open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hashx: read %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
