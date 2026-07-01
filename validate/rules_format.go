package validate

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/mail"
	"net/url"
	"regexp"
)

var (
	reUUID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	reSlug = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
)

// Email validates address structure via net/mail (no DNS).
func Email(s string) Violation {
	if _, err := mail.ParseAddress(s); err != nil {
		return Violation{Key: "validation.email"}
	}
	return Violation{}
}

// URL requires an absolute http/https URL with a host.
func URL(s string) Violation {
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return Violation{Key: "validation.url"}
	}
	return Violation{}
}

// UUID validates the canonical 8-4-4-4-12 hex form (any version).
func UUID(s string) Violation {
	if !reUUID.MatchString(s) {
		return Violation{Key: "validation.uuid"}
	}
	return Violation{}
}

// Slug validates a lowercase [a-z0-9] slug with single-hyphen separators.
func Slug(s string) Violation {
	if !reSlug.MatchString(s) {
		return Violation{Key: "validation.slug"}
	}
	return Violation{}
}

// Hex validates an even-length hex string.
func Hex(s string) Violation {
	if _, err := hex.DecodeString(s); err != nil {
		return Violation{Key: "validation.hex"}
	}
	return Violation{}
}

// Base64 validates standard base64 (with padding).
func Base64(s string) Violation {
	if _, err := base64.StdEncoding.DecodeString(s); err != nil {
		return Violation{Key: "validation.base64"}
	}
	return Violation{}
}

// JSON validates that s is well-formed JSON.
func JSON(s string) Violation {
	if !json.Valid([]byte(s)) {
		return Violation{Key: "validation.json"}
	}
	return Violation{}
}
