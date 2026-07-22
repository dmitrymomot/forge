package objectstore

import (
	"fmt"
	"unicode/utf8"
)

// maxKeyLen caps key length in bytes — the S3 limit, adopted for every
// backend so keys stay portable across drivers.
const maxKeyLen = 1024

// ValidateKey enforces the portable key grammar: 1–1024 bytes of UTF-8,
// slash-separated non-empty segments, no "." or ".." segments, no control
// characters, DEL, or backslashes. The grammar is safe on every backend —
// no traversal on disk, no surprises in S3 listings or presigned URLs.
func ValidateKey(key string) error {
	if key == "" {
		return fmt.Errorf("%w: empty", ErrInvalidKey)
	}
	if len(key) > maxKeyLen {
		return fmt.Errorf("%w: longer than %d bytes", ErrInvalidKey, maxKeyLen)
	}
	if !utf8.ValidString(key) {
		return fmt.Errorf("%w: not valid UTF-8", ErrInvalidKey)
	}
	segStart := 0
	for i := range len(key) + 1 {
		if i == len(key) || key[i] == '/' {
			switch seg := key[segStart:i]; seg {
			case "":
				return fmt.Errorf("%w: empty path segment in %q", ErrInvalidKey, key)
			case ".", "..":
				return fmt.Errorf("%w: %q segment in %q", ErrInvalidKey, seg, key)
			}
			segStart = i + 1
			continue
		}
		if c := key[i]; c < 0x20 || c == 0x7f || c == '\\' {
			return fmt.Errorf("%w: forbidden byte %#x in %q", ErrInvalidKey, key[i], key)
		}
	}
	return nil
}

// ValidatePrefix enforces the key grammar minus the segment-shape rules a
// partial key legitimately breaks: empty is allowed (list everything), and a
// trailing "/" or in-progress last segment is fine. Traversal segments and
// forbidden bytes are still rejected.
func ValidatePrefix(prefix string) error {
	if prefix == "" {
		return nil
	}
	if len(prefix) > maxKeyLen {
		return fmt.Errorf("%w: prefix longer than %d bytes", ErrInvalidKey, maxKeyLen)
	}
	if !utf8.ValidString(prefix) {
		return fmt.Errorf("%w: prefix not valid UTF-8", ErrInvalidKey)
	}
	segStart := 0
	for i := range len(prefix) + 1 {
		if i == len(prefix) || prefix[i] == '/' {
			if seg := prefix[segStart:i]; seg == "." || seg == ".." {
				return fmt.Errorf("%w: %q segment in prefix %q", ErrInvalidKey, seg, prefix)
			}
			segStart = i + 1
			continue
		}
		if c := prefix[i]; c < 0x20 || c == 0x7f || c == '\\' {
			return fmt.Errorf("%w: forbidden byte %#x in prefix %q", ErrInvalidKey, prefix[i], prefix)
		}
	}
	return nil
}
