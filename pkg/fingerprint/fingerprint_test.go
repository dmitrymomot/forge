package fingerprint_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/pkg/fingerprint"
)

func TestGenerate(t *testing.T) {
	t.Parallel()
	t.Run("generates consistent fingerprint for same request", func(t *testing.T) {
		t.Parallel()
		req := createTestRequest(map[string]string{
			"User-Agent":      "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)",
			"Accept":          "text/html,application/xhtml+xml",
			"Accept-Language": "en-US,en;q=0.9",
			"Accept-Encoding": "gzip, deflate, br",
		}, "192.168.1.100:54321")

		fp1 := fingerprint.Generate(req)
		fp2 := fingerprint.Generate(req)

		assert.Equal(t, fp1, fp2, "fingerprints should be consistent")
		assert.Len(t, fp1, 35, "fingerprint should be 35 characters (v1: + 32 hex)")
		assert.Regexp(t, "^v1:[a-f0-9]{32}$", fp1, "fingerprint should be v1:hash format")
	})

	t.Run("generates different fingerprints for different user agents", func(t *testing.T) {
		t.Parallel()
		req1 := createTestRequest(map[string]string{
			"User-Agent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)",
			"Accept":     "text/html",
		}, "192.168.1.100:54321")

		req2 := createTestRequest(map[string]string{
			"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64)",
			"Accept":     "text/html",
		}, "192.168.1.100:54321")

		fp1 := fingerprint.Generate(req1)
		fp2 := fingerprint.Generate(req2)

		assert.NotEqual(t, fp1, fp2, "different user agents should produce different fingerprints")
	})

	t.Run("generates same fingerprints for different IPs with default options", func(t *testing.T) {
		t.Parallel()
		headers := map[string]string{
			"User-Agent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)",
			"Accept":     "text/html",
		}

		req1 := createTestRequest(headers, "192.168.1.100:54321")
		req2 := createTestRequest(headers, "192.168.1.101:54321")

		fp1 := fingerprint.Generate(req1)
		fp2 := fingerprint.Generate(req2)

		assert.Equal(t, fp1, fp2, "default options exclude IP, so different IPs should produce same fingerprint")
	})

	t.Run("generates different fingerprints for different IPs when WithIP is used", func(t *testing.T) {
		t.Parallel()
		headers := map[string]string{
			"User-Agent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)",
			"Accept":     "text/html",
		}

		req1 := createTestRequest(headers, "192.168.1.100:54321")
		req2 := createTestRequest(headers, "192.168.1.101:54321")

		fp1 := fingerprint.Generate(req1, fingerprint.WithIP())
		fp2 := fingerprint.Generate(req2, fingerprint.WithIP())

		assert.NotEqual(t, fp1, fp2, "with WithIP(), different IPs should produce different fingerprints")
	})

	t.Run("generates different fingerprints for different accept headers", func(t *testing.T) {
		t.Parallel()
		req1 := createTestRequest(map[string]string{
			"User-Agent":      "Mozilla/5.0",
			"Accept":          "text/html",
			"Accept-Language": "en-US",
			"Accept-Encoding": "gzip",
		}, "192.168.1.100:54321")

		req2 := createTestRequest(map[string]string{
			"User-Agent":      "Mozilla/5.0",
			"Accept":          "application/json",
			"Accept-Language": "fr-FR",
			"Accept-Encoding": "deflate",
		}, "192.168.1.100:54321")

		fp1 := fingerprint.Generate(req1)
		fp2 := fingerprint.Generate(req2)

		assert.NotEqual(t, fp1, fp2, "different accept headers should produce different fingerprints")
	})

	t.Run("handles missing headers gracefully", func(t *testing.T) {
		t.Parallel()
		req := createTestRequest(map[string]string{
			"User-Agent": "TestBot/1.0",
		}, "192.168.1.100:54321")

		fp := fingerprint.Generate(req)
		require.NotEmpty(t, fp)
		assert.Len(t, fp, 35)
	})

	t.Run("handles empty request", func(t *testing.T) {
		t.Parallel()
		req := createTestRequest(map[string]string{}, "127.0.0.1:8080")

		fp := fingerprint.Generate(req)
		require.NotEmpty(t, fp)
		assert.Len(t, fp, 35)
	})

	t.Run("includes header set in fingerprint", func(t *testing.T) {
		t.Parallel()
		// Different header sets should produce different fingerprints
		req1 := createTestRequest(map[string]string{
			"User-Agent":                "Mozilla/5.0",
			"Accept":                    "text/html",
			"Connection":                "keep-alive",
			"Upgrade-Insecure-Requests": "1",
		}, "192.168.1.100:54321")

		req2 := createTestRequest(map[string]string{
			"User-Agent":     "Mozilla/5.0",
			"Accept":         "text/html",
			"Cache-Control":  "no-cache",
			"Sec-Fetch-Mode": "navigate",
		}, "192.168.1.100:54321")

		fp1 := fingerprint.Generate(req1)
		fp2 := fingerprint.Generate(req2)

		assert.NotEqual(t, fp1, fp2, "different header sets should produce different fingerprints")
	})

	t.Run("uses client IP from headers when WithIP is used", func(t *testing.T) {
		t.Parallel()
		req := createTestRequest(map[string]string{
			"User-Agent":       "Mozilla/5.0",
			"CF-Connecting-IP": "203.0.113.195",
		}, "192.168.1.100:54321")

		fp := fingerprint.Generate(req, fingerprint.WithIP())
		require.NotEmpty(t, fp)
		assert.Len(t, fp, 35)

		// Same request without CF header should produce different fingerprint
		req2 := createTestRequest(map[string]string{
			"User-Agent": "Mozilla/5.0",
		}, "192.168.1.100:54321")

		fp2 := fingerprint.Generate(req2, fingerprint.WithIP())
		assert.NotEqual(t, fp, fp2, "different client IPs should produce different fingerprints when WithIP is used")
	})
}

func TestValidate(t *testing.T) {
	t.Parallel()
	t.Run("validates matching fingerprints", func(t *testing.T) {
		t.Parallel()
		req := createTestRequest(map[string]string{
			"User-Agent":      "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)",
			"Accept":          "text/html",
			"Accept-Language": "en-US",
		}, "192.168.1.100:54321")

		storedFingerprint := fingerprint.Generate(req)
		err := fingerprint.Validate(req, storedFingerprint)

		assert.NoError(t, err, "should validate matching fingerprints")
	})

	t.Run("rejects non-matching fingerprints", func(t *testing.T) {
		t.Parallel()
		req1 := createTestRequest(map[string]string{
			"User-Agent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)",
		}, "192.168.1.100:54321")

		req2 := createTestRequest(map[string]string{
			"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64)",
		}, "192.168.1.100:54321")

		storedFingerprint := fingerprint.Generate(req1)
		err := fingerprint.Validate(req2, storedFingerprint)

		assert.Error(t, err, "should reject non-matching fingerprints")
		assert.ErrorIs(t, err, fingerprint.ErrMismatch, "should return ErrMismatch")
	})

	t.Run("rejects invalid stored fingerprint", func(t *testing.T) {
		t.Parallel()
		req := createTestRequest(map[string]string{
			"User-Agent": "Mozilla/5.0",
		}, "192.168.1.100:54321")

		err := fingerprint.Validate(req, "invalid-fingerprint")
		assert.Error(t, err, "should reject invalid fingerprint format")
		assert.ErrorIs(t, err, fingerprint.ErrInvalidFingerprint, "should return ErrInvalidFingerprint")
	})

	t.Run("rejects empty stored fingerprint", func(t *testing.T) {
		t.Parallel()
		req := createTestRequest(map[string]string{
			"User-Agent": "Mozilla/5.0",
		}, "192.168.1.100:54321")

		err := fingerprint.Validate(req, "")
		assert.Error(t, err, "should reject empty fingerprint")
		assert.ErrorIs(t, err, fingerprint.ErrInvalidFingerprint, "should return ErrInvalidFingerprint")
	})

	t.Run("detects IP mismatch when stored fingerprint includes IP", func(t *testing.T) {
		t.Parallel()
		req1 := createTestRequest(map[string]string{
			"User-Agent": "Mozilla/5.0",
			"Accept":     "text/html",
		}, "192.168.1.100:54321")

		req2 := createTestRequest(map[string]string{
			"User-Agent": "Mozilla/5.0",
			"Accept":     "text/html",
		}, "192.168.1.101:54321")

		// Generate with IP option
		storedFingerprint := fingerprint.Generate(req1, fingerprint.WithIP())
		// Validate with the same IP option
		err := fingerprint.Validate(req2, storedFingerprint, fingerprint.WithIP())

		assert.Error(t, err, "should detect IP change")
		assert.ErrorIs(t, err, fingerprint.ErrMismatch, "should return ErrMismatch")
	})

	t.Run("ValidateCookie matches Cookie generator", func(t *testing.T) {
		t.Parallel()
		req := createTestRequest(map[string]string{
			"User-Agent":      "Mozilla/5.0",
			"Accept":          "text/html",
			"Accept-Language": "en-US",
		}, "192.168.1.100:54321")

		storedFP := fingerprint.Cookie(req)
		err := fingerprint.ValidateCookie(req, storedFP)

		assert.NoError(t, err, "ValidateCookie should validate Cookie-generated fingerprints")
	})

	t.Run("ValidateJWT matches JWT generator", func(t *testing.T) {
		t.Parallel()
		req := createTestRequest(map[string]string{
			"User-Agent": "Mozilla/5.0",
			"Accept":     "text/html",
		}, "192.168.1.100:54321")

		storedFP := fingerprint.JWT(req)
		err := fingerprint.ValidateJWT(req, storedFP)

		assert.NoError(t, err, "ValidateJWT should validate JWT-generated fingerprints")
	})

	t.Run("ValidateStrict matches Strict generator", func(t *testing.T) {
		t.Parallel()
		req := createTestRequest(map[string]string{
			"User-Agent": "Mozilla/5.0",
			"Accept":     "text/html",
		}, "192.168.1.100:54321")

		storedFP := fingerprint.Strict(req)
		err := fingerprint.ValidateStrict(req, storedFP)

		assert.NoError(t, err, "ValidateStrict should validate Strict-generated fingerprints")
	})

	t.Run("Validate fails when options don't match generation", func(t *testing.T) {
		t.Parallel()
		req := createTestRequest(map[string]string{
			"User-Agent": "Mozilla/5.0",
			"Accept":     "text/html",
		}, "192.168.1.100:54321")

		// Generate with IP option
		storedFP := fingerprint.Generate(req, fingerprint.WithIP())

		// Validate WITHOUT IP option - should fail because fingerprints won't match
		err := fingerprint.Validate(req, storedFP)
		require.Error(t, err)
		assert.ErrorIs(t, err, fingerprint.ErrMismatch, "should return ErrMismatch when options don't match")

		// Validate WITH IP option - should succeed
		err = fingerprint.Validate(req, storedFP, fingerprint.WithIP())
		assert.NoError(t, err, "should succeed when using same options")
	})

	t.Run("validation helpers reject mismatched fingerprints", func(t *testing.T) {
		t.Parallel()
		req := createTestRequest(map[string]string{
			"User-Agent":      "Mozilla/5.0",
			"Accept":          "text/html",
			"Accept-Language": "en-US",
		}, "192.168.1.100:54321")

		// Cookie fingerprint validated with JWT should fail (different Accept header handling)
		cookieFP := fingerprint.Cookie(req)
		err := fingerprint.ValidateJWT(req, cookieFP)
		assert.Error(t, err, "ValidateJWT should reject Cookie fingerprint")
		assert.ErrorIs(t, err, fingerprint.ErrMismatch)

		// JWT fingerprint validated with Cookie should fail
		jwtFP := fingerprint.JWT(req)
		err = fingerprint.ValidateCookie(req, jwtFP)
		assert.Error(t, err, "ValidateCookie should reject JWT fingerprint")
		assert.ErrorIs(t, err, fingerprint.ErrMismatch)

		// Strict fingerprint validated with Cookie should fail (different IP handling)
		strictFP := fingerprint.Strict(req)
		err = fingerprint.ValidateCookie(req, strictFP)
		assert.Error(t, err, "ValidateCookie should reject Strict fingerprint")
		assert.ErrorIs(t, err, fingerprint.ErrMismatch)
	})

	t.Run("handles all components disabled", func(t *testing.T) {
		t.Parallel()
		req1 := createTestRequest(map[string]string{
			"User-Agent":      "Mozilla/5.0",
			"Accept":          "text/html",
			"Accept-Language": "en-US",
		}, "192.168.1.100:54321")

		req2 := createTestRequest(map[string]string{
			"User-Agent":      "Different Browser",
			"Accept":          "application/json",
			"Accept-Language": "fr-FR",
		}, "192.168.1.200:12345")

		// Generate fingerprints with all components disabled
		fp1 := fingerprint.Generate(req1,
			fingerprint.WithoutUserAgent(),
			fingerprint.WithoutAcceptHeaders(),
			fingerprint.WithoutHeaderSet(),
		)
		fp2 := fingerprint.Generate(req2,
			fingerprint.WithoutUserAgent(),
			fingerprint.WithoutAcceptHeaders(),
			fingerprint.WithoutHeaderSet(),
		)

		require.NotEmpty(t, fp1)
		assert.Len(t, fp1, 35, "should still produce valid fingerprint format")
		assert.Equal(t, fp1, fp2, "should produce same fingerprint when all components disabled")

		// Should validate successfully
		err := fingerprint.Validate(req2, fp1,
			fingerprint.WithoutUserAgent(),
			fingerprint.WithoutAcceptHeaders(),
			fingerprint.WithoutHeaderSet(),
		)
		assert.NoError(t, err)
	})

	t.Run("ignores non-whitelisted headers", func(t *testing.T) {
		t.Parallel()
		req1 := createTestRequest(map[string]string{
			"User-Agent":    "Mozilla/5.0",
			"Accept":        "text/html",
			"Cookie":        "session=xyz",
			"Authorization": "Bearer token",
			"X-Custom":      "value1",
		}, "192.168.1.100:54321")

		req2 := createTestRequest(map[string]string{
			"User-Agent":    "Mozilla/5.0",
			"Accept":        "text/html",
			"Cookie":        "session=different",
			"Authorization": "Bearer other_token",
			"X-Custom":      "value2",
		}, "192.168.1.100:54321")

		fp1 := fingerprint.Generate(req1)
		fp2 := fingerprint.Generate(req2)

		assert.Equal(t, fp1, fp2, "non-whitelisted headers (Cookie, Authorization, X-Custom) should not affect fingerprint")
	})
}

func TestFingerprintConsistency(t *testing.T) {
	t.Parallel()
	t.Run("produces consistent fingerprints across multiple calls", func(t *testing.T) {
		t.Parallel()
		req := createTestRequest(map[string]string{
			"User-Agent":      "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)",
			"Accept":          "text/html,application/xhtml+xml",
			"Accept-Language": "en-US,en;q=0.9",
			"Accept-Encoding": "gzip, deflate, br",
			"Connection":      "keep-alive",
		}, "192.168.1.100:54321")

		fingerprints := make(map[string]bool)
		for range 100 {
			fp := fingerprint.Generate(req)
			fingerprints[fp] = true
		}

		assert.Len(t, fingerprints, 1, "should produce only one unique fingerprint for identical requests")
	})
}

func TestFingerprintUniqueness(t *testing.T) {
	t.Parallel()
	t.Run("generates unique fingerprints for different clients", func(t *testing.T) {
		t.Parallel()
		testCases := []struct {
			name    string
			headers map[string]string
			ip      string
		}{
			{
				name: "Chrome on Mac",
				headers: map[string]string{
					"User-Agent":      "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36",
					"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9",
					"Accept-Language": "en-US,en;q=0.9",
					"Accept-Encoding": "gzip, deflate, br",
				},
				ip: "192.168.1.100:54321",
			},
			{
				name: "Firefox on Windows",
				headers: map[string]string{
					"User-Agent":      "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:91.0) Gecko/20100101",
					"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
					"Accept-Language": "en-US,en;q=0.5",
					"Accept-Encoding": "gzip, deflate",
				},
				ip: "192.168.1.101:54321",
			},
			{
				name: "Safari on iOS",
				headers: map[string]string{
					"User-Agent":      "Mozilla/5.0 (iPhone; CPU iPhone OS 14_7_1 like Mac OS X)",
					"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
					"Accept-Language": "en-us",
					"Accept-Encoding": "gzip, deflate",
				},
				ip: "192.168.1.102:54321",
			},
			{
				name: "API Client",
				headers: map[string]string{
					"User-Agent": "MyApp/1.0",
					"Accept":     "application/json",
				},
				ip: "192.168.1.103:54321",
			},
		}

		fingerprints := make(map[string]string)
		for _, tc := range testCases {
			req := createTestRequest(tc.headers, tc.ip)
			fp := fingerprint.Generate(req)

			// Check for collisions
			if existing, exists := fingerprints[fp]; exists {
				t.Errorf("Fingerprint collision: %s and %s produced same fingerprint %s",
					existing, tc.name, fp)
			}
			fingerprints[fp] = tc.name
		}

		assert.Len(t, fingerprints, len(testCases), "each client should have unique fingerprint")
	})
}

func BenchmarkGenerate(b *testing.B) {
	req := createTestRequest(map[string]string{
		"User-Agent":                "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36",
		"Accept":                    "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8",
		"Accept-Language":           "en-US,en;q=0.9",
		"Accept-Encoding":           "gzip, deflate, br",
		"Connection":                "keep-alive",
		"Upgrade-Insecure-Requests": "1",
		"Sec-Fetch-Dest":            "document",
		"Sec-Fetch-Mode":            "navigate",
		"Sec-Fetch-Site":            "none",
		"Cache-Control":             "max-age=0",
	}, "192.168.1.100:54321")

	b.ResetTimer()
	for b.Loop() {
		fingerprint.Generate(req)
	}
}

func BenchmarkValidate(b *testing.B) {
	req := createTestRequest(map[string]string{
		"User-Agent":      "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)",
		"Accept":          "text/html",
		"Accept-Language": "en-US",
		"Accept-Encoding": "gzip, deflate",
	}, "192.168.1.100:54321")

	storedFingerprint := fingerprint.Generate(req)

	b.ResetTimer()
	for b.Loop() {
		_ = fingerprint.Validate(req, storedFingerprint)
	}
}

func BenchmarkGenerateMinimalHeaders(b *testing.B) {
	req := createTestRequest(map[string]string{
		"User-Agent": "TestBot/1.0",
	}, "127.0.0.1:8080")

	b.ResetTimer()
	for b.Loop() {
		fingerprint.Generate(req)
	}
}

func BenchmarkStrict(b *testing.B) {
	req := createTestRequest(map[string]string{
		"User-Agent":      "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)",
		"Accept":          "text/html",
		"Accept-Language": "en-US",
	}, "192.168.1.100:54321")

	b.ResetTimer()
	for b.Loop() {
		fingerprint.Strict(req)
	}
}

func BenchmarkCookie(b *testing.B) {
	req := createTestRequest(map[string]string{
		"User-Agent":      "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)",
		"Accept":          "text/html",
		"Accept-Language": "en-US",
	}, "192.168.1.100:54321")

	b.ResetTimer()
	for b.Loop() {
		fingerprint.Cookie(req)
	}
}

func BenchmarkJWT(b *testing.B) {
	req := createTestRequest(map[string]string{
		"User-Agent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)",
	}, "192.168.1.100:54321")

	b.ResetTimer()
	for b.Loop() {
		fingerprint.JWT(req)
	}
}

// Helper function to create test requests
func createTestRequest(headers map[string]string, remoteAddr string) *http.Request {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = remoteAddr

	for key, value := range headers {
		req.Header.Set(key, value)
	}

	return req
}
