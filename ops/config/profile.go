package config

import (
	"os"
	"slices"
	"strings"
)

// Profile returns the active deployment profile from APP_ENV, then ENV,
// defaulting to "development". The raw string is also the {profile}.yaml stem.
func Profile() string {
	for _, k := range []string{"APP_ENV", "ENV"} {
		if v, ok := os.LookupEnv(k); ok && v != "" {
			return v
		}
	}
	return "development"
}

func matches(p string, names ...string) bool {
	return slices.Contains(names, strings.ToLower(strings.TrimSpace(p)))
}

// IsDev reports whether the profile is a development profile.
func IsDev() bool { return matches(Profile(), "dev", "development", "local") }

// IsProd reports whether the profile is a production profile.
func IsProd() bool { return matches(Profile(), "prod", "production") }

// IsTest reports whether the profile is a test profile.
func IsTest() bool { return matches(Profile(), "test", "testing") }

// IsStaging reports whether the profile is a staging profile.
func IsStaging() bool { return matches(Profile(), "staging", "stage") }
