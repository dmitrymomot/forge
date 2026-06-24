package fingerprint_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

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

		fp1 := fingerprint.Generate(req, fingerprint.DefaultConfig())
		fp2 := fingerprint.Generate(req, fingerprint.DefaultConfig())

		require.Equal(t, fp1, fp2, "fingerprints should be consistent")
		require.Len(t, fp1, 35, "fingerprint should be 35 characters (v1: + 32 hex)")
		require.Regexp(t, "^v1:[a-f0-9]{32}$", fp1, "fingerprint should be v1:hash format")
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

		fp1 := fingerprint.Generate(req1, fingerprint.DefaultConfig())
		fp2 := fingerprint.Generate(req2, fingerprint.DefaultConfig())

		require.NotEqual(t, fp1, fp2, "different user agents should produce different fingerprints")
	})

	t.Run("generates same fingerprints for different IPs with default config", func(t *testing.T) {
		t.Parallel()
		headers := map[string]string{
			"User-Agent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)",
			"Accept":     "text/html",
		}

		req1 := createTestRequest(headers, "192.168.1.100:54321")
		req2 := createTestRequest(headers, "192.168.1.101:54321")

		fp1 := fingerprint.Generate(req1, fingerprint.DefaultConfig())
		fp2 := fingerprint.Generate(req2, fingerprint.DefaultConfig())

		require.Equal(t, fp1, fp2, "default config excludes IP, so different IPs should produce same fingerprint")
	})

	t.Run("generates different fingerprints for different IPs when IncludeIP is set", func(t *testing.T) {
		t.Parallel()
		headers := map[string]string{
			"User-Agent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)",
			"Accept":     "text/html",
		}

		req1 := createTestRequest(headers, "192.168.1.100:54321")
		req2 := createTestRequest(headers, "192.168.1.101:54321")

		cfg := fingerprint.DefaultConfig()
		cfg.IncludeIP = true

		fp1 := fingerprint.Generate(req1, cfg)
		fp2 := fingerprint.Generate(req2, cfg)

		require.NotEqual(t, fp1, fp2, "with IncludeIP, different IPs should produce different fingerprints")
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

		fp1 := fingerprint.Generate(req1, fingerprint.DefaultConfig())
		fp2 := fingerprint.Generate(req2, fingerprint.DefaultConfig())

		require.NotEqual(t, fp1, fp2, "different accept headers should produce different fingerprints")
	})

	t.Run("handles missing headers gracefully", func(t *testing.T) {
		t.Parallel()
		req := createTestRequest(map[string]string{
			"User-Agent": "TestBot/1.0",
		}, "192.168.1.100:54321")

		fp := fingerprint.Generate(req, fingerprint.DefaultConfig())
		require.NotEmpty(t, fp)
		require.Len(t, fp, 35)
	})

	t.Run("handles empty request", func(t *testing.T) {
		t.Parallel()
		req := createTestRequest(map[string]string{}, "127.0.0.1:8080")

		fp := fingerprint.Generate(req, fingerprint.DefaultConfig())
		require.NotEmpty(t, fp)
		require.Len(t, fp, 35)
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

		fp1 := fingerprint.Generate(req1, fingerprint.DefaultConfig())
		fp2 := fingerprint.Generate(req2, fingerprint.DefaultConfig())

		require.NotEqual(t, fp1, fp2, "different header sets should produce different fingerprints")
	})

	t.Run("uses client IP from headers when IncludeIP is set", func(t *testing.T) {
		t.Parallel()
		req := createTestRequest(map[string]string{
			"User-Agent":       "Mozilla/5.0",
			"CF-Connecting-IP": "203.0.113.195",
		}, "192.168.1.100:54321")

		cfg := fingerprint.DefaultConfig()
		cfg.IncludeIP = true

		fp := fingerprint.Generate(req, cfg)
		require.NotEmpty(t, fp)
		require.Len(t, fp, 35)

		// Same request without CF header should produce different fingerprint
		req2 := createTestRequest(map[string]string{
			"User-Agent": "Mozilla/5.0",
		}, "192.168.1.100:54321")

		fp2 := fingerprint.Generate(req2, cfg)
		require.NotEqual(t, fp, fp2, "different client IPs should produce different fingerprints when IncludeIP is set")
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

		storedFingerprint := fingerprint.Generate(req, fingerprint.DefaultConfig())
		err := fingerprint.Validate(req, storedFingerprint, fingerprint.DefaultConfig())

		require.NoError(t, err, "should validate matching fingerprints")
	})

	t.Run("rejects non-matching fingerprints", func(t *testing.T) {
		t.Parallel()
		req1 := createTestRequest(map[string]string{
			"User-Agent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)",
		}, "192.168.1.100:54321")

		req2 := createTestRequest(map[string]string{
			"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64)",
		}, "192.168.1.100:54321")

		storedFingerprint := fingerprint.Generate(req1, fingerprint.DefaultConfig())
		err := fingerprint.Validate(req2, storedFingerprint, fingerprint.DefaultConfig())

		require.Error(t, err, "should reject non-matching fingerprints")
		require.ErrorIs(t, err, fingerprint.ErrMismatch, "should return ErrMismatch")
	})

	t.Run("rejects invalid stored fingerprint", func(t *testing.T) {
		t.Parallel()
		req := createTestRequest(map[string]string{
			"User-Agent": "Mozilla/5.0",
		}, "192.168.1.100:54321")

		err := fingerprint.Validate(req, "invalid-fingerprint", fingerprint.DefaultConfig())
		require.Error(t, err, "should reject invalid fingerprint format")
		require.ErrorIs(t, err, fingerprint.ErrInvalidFingerprint, "should return ErrInvalidFingerprint")
	})

	t.Run("rejects empty stored fingerprint", func(t *testing.T) {
		t.Parallel()
		req := createTestRequest(map[string]string{
			"User-Agent": "Mozilla/5.0",
		}, "192.168.1.100:54321")

		err := fingerprint.Validate(req, "", fingerprint.DefaultConfig())
		require.Error(t, err, "should reject empty fingerprint")
		require.ErrorIs(t, err, fingerprint.ErrInvalidFingerprint, "should return ErrInvalidFingerprint")
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

		cfg := fingerprint.DefaultConfig()
		cfg.IncludeIP = true

		// Generate with IP
		storedFingerprint := fingerprint.Generate(req1, cfg)
		// Validate with same config
		err := fingerprint.Validate(req2, storedFingerprint, cfg)

		require.Error(t, err, "should detect IP change")
		require.ErrorIs(t, err, fingerprint.ErrMismatch, "should return ErrMismatch")
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

		require.NoError(t, err, "ValidateCookie should validate Cookie-generated fingerprints")
	})

	t.Run("ValidateJWT matches JWT generator", func(t *testing.T) {
		t.Parallel()
		req := createTestRequest(map[string]string{
			"User-Agent": "Mozilla/5.0",
			"Accept":     "text/html",
		}, "192.168.1.100:54321")

		storedFP := fingerprint.JWT(req)
		err := fingerprint.ValidateJWT(req, storedFP)

		require.NoError(t, err, "ValidateJWT should validate JWT-generated fingerprints")
	})

	t.Run("ValidateStrict matches Strict generator", func(t *testing.T) {
		t.Parallel()
		req := createTestRequest(map[string]string{
			"User-Agent": "Mozilla/5.0",
			"Accept":     "text/html",
		}, "192.168.1.100:54321")

		storedFP := fingerprint.Strict(req)
		err := fingerprint.ValidateStrict(req, storedFP)

		require.NoError(t, err, "ValidateStrict should validate Strict-generated fingerprints")
	})

	t.Run("ValidateHTMX matches HTMX generator", func(t *testing.T) {
		t.Parallel()
		req := createTestRequest(map[string]string{
			"User-Agent": "Mozilla/5.0",
			"Accept":     "text/html",
		}, "192.168.1.100:54321")

		storedFP := fingerprint.HTMX(req)
		require.Regexp(t, "^v1:[a-f0-9]{32}$", storedFP, "HTMX should produce a v1:hash fingerprint")

		err := fingerprint.ValidateHTMX(req, storedFP)
		require.NoError(t, err, "ValidateHTMX should validate HTMX-generated fingerprints")
	})

	t.Run("HTMX ignores Accept and HTMX-specific headers", func(t *testing.T) {
		t.Parallel()
		// HTMX uses only User-Agent, so changing Accept headers, header set,
		// and HTMX-specific headers must NOT change the fingerprint.
		req1 := createTestRequest(map[string]string{
			"User-Agent":      "Mozilla/5.0",
			"Accept":          "text/html",
			"Accept-Language": "en-US",
			"HX-Request":      "true",
			"HX-Current-URL":  "https://example.com/page",
			"Connection":      "keep-alive",
		}, "192.168.1.100:54321")

		req2 := createTestRequest(map[string]string{
			"User-Agent":      "Mozilla/5.0",
			"Accept":          "application/json",
			"Accept-Language": "fr-FR",
			"HX-Request":      "false",
			"HX-Current-URL":  "https://example.com/other",
		}, "192.168.1.100:54321")

		fp1 := fingerprint.HTMX(req1)
		fp2 := fingerprint.HTMX(req2)

		require.Equal(t, fp1, fp2, "HTMX should only depend on User-Agent")

		// And a request from a different User-Agent must NOT validate.
		req3 := createTestRequest(map[string]string{
			"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64)",
			"Accept":     "text/html",
		}, "192.168.1.100:54321")

		err := fingerprint.ValidateHTMX(req3, fp1)
		require.Error(t, err, "ValidateHTMX should reject a different User-Agent")
		require.ErrorIs(t, err, fingerprint.ErrMismatch, "should return ErrMismatch")
	})

	t.Run("Validate fails when config doesn't match generation", func(t *testing.T) {
		t.Parallel()
		req := createTestRequest(map[string]string{
			"User-Agent": "Mozilla/5.0",
			"Accept":     "text/html",
		}, "192.168.1.100:54321")

		cfgWithIP := fingerprint.DefaultConfig()
		cfgWithIP.IncludeIP = true

		// Generate with IP
		storedFP := fingerprint.Generate(req, cfgWithIP)

		// Validate WITHOUT IP - should fail because fingerprints won't match
		err := fingerprint.Validate(req, storedFP, fingerprint.DefaultConfig())
		require.Error(t, err)
		require.ErrorIs(t, err, fingerprint.ErrMismatch, "should return ErrMismatch when config doesn't match")

		// Validate WITH IP - should succeed
		err = fingerprint.Validate(req, storedFP, cfgWithIP)
		require.NoError(t, err, "should succeed when using same config")
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
		require.Error(t, err, "ValidateJWT should reject Cookie fingerprint")
		require.ErrorIs(t, err, fingerprint.ErrMismatch)

		// JWT fingerprint validated with Cookie should fail
		jwtFP := fingerprint.JWT(req)
		err = fingerprint.ValidateCookie(req, jwtFP)
		require.Error(t, err, "ValidateCookie should reject JWT fingerprint")
		require.ErrorIs(t, err, fingerprint.ErrMismatch)

		// Strict fingerprint validated with Cookie should fail (different IP handling)
		strictFP := fingerprint.Strict(req)
		err = fingerprint.ValidateCookie(req, strictFP)
		require.Error(t, err, "ValidateCookie should reject Strict fingerprint")
		require.ErrorIs(t, err, fingerprint.ErrMismatch)

		// HTMX fingerprint validated with Cookie should fail (HTMX excludes Accept + header set)
		htmxFP := fingerprint.HTMX(req)
		err = fingerprint.ValidateCookie(req, htmxFP)
		require.Error(t, err, "ValidateCookie should reject HTMX fingerprint")
		require.ErrorIs(t, err, fingerprint.ErrMismatch)

		// Cookie fingerprint validated with HTMX should fail
		err = fingerprint.ValidateHTMX(req, cookieFP)
		require.Error(t, err, "ValidateHTMX should reject Cookie fingerprint")
		require.ErrorIs(t, err, fingerprint.ErrMismatch)
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
		cfg := fingerprint.Config{}

		fp1 := fingerprint.Generate(req1, cfg)
		fp2 := fingerprint.Generate(req2, cfg)

		require.NotEmpty(t, fp1)
		require.Len(t, fp1, 35, "should still produce valid fingerprint format")
		require.Equal(t, fp1, fp2, "should produce same fingerprint when all components disabled")

		// Should validate successfully
		err := fingerprint.Validate(req2, fp1, cfg)
		require.NoError(t, err)
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

		fp1 := fingerprint.Generate(req1, fingerprint.DefaultConfig())
		fp2 := fingerprint.Generate(req2, fingerprint.DefaultConfig())

		require.Equal(t, fp1, fp2, "non-whitelisted headers (Cookie, Authorization, X-Custom) should not affect fingerprint")
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
			fp := fingerprint.Generate(req, fingerprint.DefaultConfig())
			fingerprints[fp] = true
		}

		require.Len(t, fingerprints, 1, "should produce only one unique fingerprint for identical requests")
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
			fp := fingerprint.Generate(req, fingerprint.DefaultConfig())

			// Check for collisions
			if existing, exists := fingerprints[fp]; exists {
				t.Errorf("Fingerprint collision: %s and %s produced same fingerprint %s",
					existing, tc.name, fp)
			}
			fingerprints[fp] = tc.name
		}

		require.Len(t, fingerprints, len(testCases), "each client should have unique fingerprint")
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

	cfg := fingerprint.DefaultConfig()
	b.ResetTimer()
	for b.Loop() {
		fingerprint.Generate(req, cfg)
	}
}

func BenchmarkValidate(b *testing.B) {
	req := createTestRequest(map[string]string{
		"User-Agent":      "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)",
		"Accept":          "text/html",
		"Accept-Language": "en-US",
		"Accept-Encoding": "gzip, deflate",
	}, "192.168.1.100:54321")

	cfg := fingerprint.DefaultConfig()
	storedFingerprint := fingerprint.Generate(req, cfg)

	b.ResetTimer()
	for b.Loop() {
		_ = fingerprint.Validate(req, storedFingerprint, cfg)
	}
}

func BenchmarkGenerateMinimalHeaders(b *testing.B) {
	req := createTestRequest(map[string]string{
		"User-Agent": "TestBot/1.0",
	}, "127.0.0.1:8080")

	cfg := fingerprint.DefaultConfig()
	b.ResetTimer()
	for b.Loop() {
		fingerprint.Generate(req, cfg)
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

func BenchmarkHTMX(b *testing.B) {
	req := createTestRequest(map[string]string{
		"User-Agent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)",
	}, "192.168.1.100:54321")

	b.ResetTimer()
	for b.Loop() {
		fingerprint.HTMX(req)
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
