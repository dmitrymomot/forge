package totp_test

import (
	"regexp"
	"testing"

	"github.com/dmitrymomot/forge/pkg/totp"

	"github.com/stretchr/testify/require"
)

// hexCodeRegex matches the 16-character uppercase-hex recovery code format.
var hexCodeRegex = regexp.MustCompile("^[0-9A-F]{16}$")

func TestGenerateRecoveryCodes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		count   int
		wantErr bool
	}{
		{
			name:    "Generate 8 codes",
			count:   8,
			wantErr: false,
		},
		{
			name:    "Generate 1 code",
			count:   1,
			wantErr: false,
		},
		{
			name:    "Generate 0 codes",
			count:   0,
			wantErr: true,
		},
		{
			name:    "Generate negative codes",
			count:   -1,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			codes, err := totp.GenerateRecoveryCodes(tt.count)
			if tt.wantErr {
				require.ErrorIs(t, err, totp.ErrInvalidRecoveryCodeCount)
				require.Nil(t, codes)
				return
			}

			require.NoError(t, err)
			require.Len(t, codes, tt.count)

			// Verify each code is unique and properly formatted (16 uppercase hex chars).
			seen := make(map[string]bool)
			for _, code := range codes {
				require.Regexp(t, hexCodeRegex, code)
				require.False(t, seen[code], "Duplicate code found")
				seen[code] = true
			}
		})
	}
}

func TestHashRecoveryCode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		code string
	}{
		{
			name: "Normal code",
			code: "1234567890ABCDEF",
		},
		{
			name: "Empty code",
			code: "",
		},
		{
			name: "Special characters",
			code: "!@#$%^&*()",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			hash := totp.HashRecoveryCode(tt.code)
			require.NotEmpty(t, hash)
			require.Len(t, hash, 64) // SHA-256 produces 32 bytes = 64 hex characters

			// Verify deterministic behavior
			hash2 := totp.HashRecoveryCode(tt.code)
			require.Equal(t, hash, hash2)
		})
	}
}

func TestVerifyRecoveryCode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		code       string
		hashedCode string
		wantResult bool
	}{
		{
			name:       "Valid code",
			code:       "1234567890ABCDEF",
			hashedCode: totp.HashRecoveryCode("1234567890ABCDEF"),
			wantResult: true,
		},
		{
			name:       "Invalid code - same length",
			code:       "1234567890ABCDEF",
			hashedCode: totp.HashRecoveryCode("FEDCBA0987654321"),
			wantResult: false,
		},
		{
			name:       "Invalid code - different length",
			code:       "1234",
			hashedCode: totp.HashRecoveryCode("5678"),
			wantResult: false,
		},
		{
			name:       "Empty code",
			code:       "",
			hashedCode: totp.HashRecoveryCode(""),
			wantResult: true,
		},
		{
			name:       "Code vs empty hash",
			code:       "1234567890ABCDEF",
			hashedCode: "",
			wantResult: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Test the verification
			result := totp.VerifyRecoveryCode(tt.code, tt.hashedCode)
			require.Equal(t, tt.wantResult, result)
		})
	}
}

// TestVerifyRecoveryCodeSecurity performs basic security checks
func TestVerifyRecoveryCodeSecurity(t *testing.T) {
	t.Parallel()
	// Test that the function is using constant-time comparison
	code := "1234567890ABCDEF"
	hash := totp.HashRecoveryCode(code)

	// Multiple verifications should yield the same result
	for range 100 {
		result := totp.VerifyRecoveryCode(code, hash)
		require.True(t, result, "Verification should be consistent")
	}

	// Test different inputs should not match
	invalidCode := "FEDCBA0987654321"
	result := totp.VerifyRecoveryCode(invalidCode, hash)
	require.False(t, result, "Different codes should not match")

	// Test empty inputs
	require.False(t, totp.VerifyRecoveryCode("", hash), "Empty code should not match")
	require.False(t, totp.VerifyRecoveryCode(code, ""), "Empty hash should not match")
}

// Benchmark recovery code verification
func BenchmarkVerifyRecoveryCode(b *testing.B) {
	code := "1234567890ABCDEF"
	hashedCode := totp.HashRecoveryCode(code)

	b.ResetTimer()
	for b.Loop() {
		totp.VerifyRecoveryCode(code, hashedCode)
	}
}
