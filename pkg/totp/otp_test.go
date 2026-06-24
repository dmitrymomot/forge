package totp_test

import (
	"regexp"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/pkg/totp"

	"github.com/stretchr/testify/require"
)

// base32SecretRegex mirrors the package's internal Base32 validation so tests
// can assert the generated secret format without relying on an exported var.
var base32SecretRegex = regexp.MustCompile("^[A-Z2-7]+=*$")

// rfc6238SeedSHA1 is the RFC 6238 Appendix B SHA1 shared secret
// ("12345678901234567890" ASCII), Base32-encoded.
const rfc6238SeedSHA1 = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"

func TestGenerateSecretKey(t *testing.T) {
	t.Parallel()
	secret, err := totp.GenerateSecretKey()
	require.NoError(t, err)
	require.NotEmpty(t, secret)
	require.Regexp(t, base32SecretRegex, secret)
	// 160-bit secret -> 32 Base32 chars (no padding).
	require.Len(t, secret, 32)
}

func TestGetTOTPURI(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		params  totp.TOTPParams
		want    string
		wantErr error
	}{
		{
			name: "Basic URI applies defaults",
			params: totp.TOTPParams{
				Secret:      "ABCDEFGHIJKLMNOP",
				AccountName: "test@example.com",
				Issuer:      "TestApp",
			},
			want: "otpauth://totp/TestApp:test@example.com?algorithm=SHA1&digits=6&issuer=TestApp&period=30&secret=ABCDEFGHIJKLMNOP",
		},
		{
			name: "URI with special characters",
			params: totp.TOTPParams{
				Secret:      "ABCDEFGHIJKLMNOP",
				AccountName: "test+user@example.com",
				Issuer:      "Test & App",
				Algorithm:   "SHA1",
				Digits:      6,
				Period:      30,
			},
			want: "otpauth://totp/Test%20&%20App:test+user@example.com?algorithm=SHA1&digits=6&issuer=Test+%26+App&period=30&secret=ABCDEFGHIJKLMNOP",
		},
		{
			name: "Non-default algorithm digits and period",
			params: totp.TOTPParams{
				Secret:      "ABCDEFGHIJKLMNOP",
				AccountName: "u@example.com",
				Issuer:      "App",
				Algorithm:   "SHA256",
				Digits:      8,
				Period:      60,
			},
			want: "otpauth://totp/App:u@example.com?algorithm=SHA256&digits=8&issuer=App&period=60&secret=ABCDEFGHIJKLMNOP",
		},
		{
			name: "Missing secret returns error",
			params: totp.TOTPParams{
				AccountName: "u@example.com",
				Issuer:      "App",
			},
			wantErr: totp.ErrMissingSecret,
		},
		{
			name: "Invalid base32 secret returns error",
			params: totp.TOTPParams{
				Secret:      "invalid-base32!@#$",
				AccountName: "u@example.com",
				Issuer:      "App",
			},
			wantErr: totp.ErrInvalidSecret,
		},
		{
			name: "Missing account name returns error",
			params: totp.TOTPParams{
				Secret: "ABCDEFGHIJKLMNOP",
				Issuer: "App",
			},
			wantErr: totp.ErrMissingAccountName,
		},
		{
			name: "Missing issuer returns error",
			params: totp.TOTPParams{
				Secret:      "ABCDEFGHIJKLMNOP",
				AccountName: "u@example.com",
			},
			wantErr: totp.ErrMissingIssuer,
		},
		{
			name: "Unsupported algorithm returns error",
			params: totp.TOTPParams{
				Secret:      "ABCDEFGHIJKLMNOP",
				AccountName: "u@example.com",
				Issuer:      "App",
				Algorithm:   "MD5",
			},
			wantErr: totp.ErrUnsupportedAlgorithm,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := totp.GetTOTPURI(tt.params)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				require.Empty(t, got)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestTOTPParamsGetDefaults(t *testing.T) {
	t.Parallel()

	t.Run("fills zero-valued fields with RFC defaults", func(t *testing.T) {
		t.Parallel()
		got := totp.TOTPParams{Secret: "ABCDEFGHIJKLMNOP"}.GetDefaults()
		require.Equal(t, totp.DefaultAlgorithm, got.Algorithm)
		require.Equal(t, totp.DefaultDigits, got.Digits)
		require.Equal(t, totp.DefaultPeriod, got.Period)
	})

	t.Run("preserves explicitly set fields", func(t *testing.T) {
		t.Parallel()
		got := totp.TOTPParams{
			Secret:    "ABCDEFGHIJKLMNOP",
			Algorithm: "SHA512",
			Digits:    8,
			Period:    60,
		}.GetDefaults()
		require.Equal(t, "SHA512", got.Algorithm)
		require.Equal(t, 8, got.Digits)
		require.Equal(t, 60, got.Period)
	})
}

func TestValidateTOTP(t *testing.T) {
	t.Parallel()
	validSecret, err := totp.GenerateSecretKey()
	require.NoError(t, err)
	require.NotEmpty(t, validSecret)

	// Generate valid OTP for testing
	validOTP, err := totp.GenerateTOTP(validSecret)
	require.NoError(t, err)
	require.NotEmpty(t, validOTP)

	tests := []struct {
		name      string
		secret    string
		otp       string
		wantErr   bool
		errTarget error
		result    bool
	}{
		{
			name:      "Invalid base32 secret",
			secret:    "invalid-base32!@#$",
			otp:       "123456",
			wantErr:   true,
			errTarget: totp.ErrInvalidSecret,
			result:    false,
		},
		{
			name:    "Invalid OTP length",
			secret:  "ABCDEFGHIJKLMNOP",
			otp:     "12345",
			wantErr: true,
			result:  false,
		},
		{
			name:    "Invalid OTP characters",
			secret:  "ABCDEFGHIJKLMNOP",
			otp:     "12345a",
			wantErr: true,
			result:  false,
		},
		{
			name:      "Empty secret",
			secret:    "",
			otp:       "123456",
			wantErr:   true,
			errTarget: totp.ErrInvalidSecret,
			result:    false,
		},
		{
			name:    "Empty OTP",
			secret:  "ABCDEFGHIJKLMNOP",
			otp:     "",
			wantErr: true,
			result:  false,
		},
		{
			name:    "Valid OTP",
			secret:  validSecret,
			otp:     validOTP,
			wantErr: false,
			result:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := totp.ValidateTOTP(tt.secret, tt.otp)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errTarget != nil {
					require.ErrorIs(t, err, tt.errTarget)
				}
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tt.result, result)
		})
	}
}

// TestTOTPRejectsShortSecret verifies that empty/too-short secrets that pass the
// Base32 regex but would yield a weak (near all-zero) HMAC key are rejected.
func TestTOTPRejectsShortSecret(t *testing.T) {
	t.Parallel()

	// "AA" decodes to 1 byte, "AAAAAAAA" decodes to 5 bytes -> both below the floor.
	for _, secret := range []string{"AA", "AAAAAAAA"} {
		t.Run("generate rejects "+secret, func(t *testing.T) {
			t.Parallel()
			_, err := totp.GenerateTOTP(secret)
			require.ErrorIs(t, err, totp.ErrSecretTooShort)
		})
		t.Run("validate rejects "+secret, func(t *testing.T) {
			t.Parallel()
			ok, err := totp.ValidateTOTP(secret, "123456")
			require.ErrorIs(t, err, totp.ErrSecretTooShort)
			require.False(t, ok)
		})
	}
}

// TestGenerateHOTP_RFC4226Vector asserts the canonical RFC 6238 Appendix B
// vector: HMAC-SHA1 with the shared secret "12345678901234567890" at counter 1
// produces the 8-digit code 94287082.
func TestGenerateHOTP_RFC4226Vector(t *testing.T) {
	t.Parallel()
	key := []byte("12345678901234567890")
	require.Equal(t, 94287082, totp.GenerateHOTP(key, 1, 8))
}

func TestGenerateHOTP_Bounds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		key     []byte
		counter int64
		digits  int
	}{
		{name: "6 digits", key: []byte("12345678901234567890"), counter: 0, digits: 6},
		{name: "8 digits", key: []byte("12345678901234567890"), counter: 1, digits: 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			code := totp.GenerateHOTP(tt.key, tt.counter, tt.digits)
			require.GreaterOrEqual(t, code, 0)
			require.Less(t, code, int(pow10(tt.digits)))
		})
	}
}

// TestTOTP_RFC6238Vectors asserts known TOTP codes from RFC 6238 Appendix B for
// SHA1, SHA256, and SHA512 at the canonical test times (T0=0, X=30).
func TestTOTP_RFC6238Vectors(t *testing.T) {
	t.Parallel()

	const (
		seedSHA256 = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQGEZA"
		seedSHA512 = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQGEZDGNA"
	)

	tests := []struct {
		name      string
		secret    string
		algorithm string
		unix      int64
		want      string
	}{
		{name: "SHA1 T=59", secret: rfc6238SeedSHA1, algorithm: "SHA1", unix: 59, want: "94287082"},
		{name: "SHA1 T=1111111109", secret: rfc6238SeedSHA1, algorithm: "SHA1", unix: 1111111109, want: "07081804"},
		{name: "SHA1 T=1234567890", secret: rfc6238SeedSHA1, algorithm: "SHA1", unix: 1234567890, want: "89005924"},
		{name: "SHA1 T=2000000000", secret: rfc6238SeedSHA1, algorithm: "SHA1", unix: 2000000000, want: "69279037"},
		{name: "SHA256 T=59", secret: seedSHA256, algorithm: "SHA256", unix: 59, want: "46119246"},
		{name: "SHA512 T=59", secret: seedSHA512, algorithm: "SHA512", unix: 59, want: "90693936"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := totp.GenerateTOTPWithTime(
				tt.secret,
				time.Unix(tt.unix, 0),
				totp.WithAlgorithm(tt.algorithm),
				totp.WithDigits(8),
			)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

// TestTOTP_RoundTripNonDefaults verifies that generation and validation honor
// the same non-default Algorithm/Digits/Period, so the parameters advertised in
// the provisioning URI actually validate.
func TestTOTP_RoundTripNonDefaults(t *testing.T) {
	t.Parallel()

	secret, err := totp.GenerateSecretKey()
	require.NoError(t, err)

	tests := []struct {
		name      string
		algorithm string
		digits    int
		period    int
	}{
		{name: "SHA256 8 digits 60s", algorithm: "SHA256", digits: 8, period: 60},
		{name: "SHA512 7 digits 45s", algorithm: "SHA512", digits: 7, period: 45},
		{name: "SHA1 8 digits 30s", algorithm: "SHA1", digits: 8, period: 30},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			opts := []totp.TOTPOption{
				totp.WithAlgorithm(tt.algorithm),
				totp.WithDigits(tt.digits),
				totp.WithPeriod(tt.period),
			}

			code, err := totp.GenerateTOTP(secret, opts...)
			require.NoError(t, err)
			require.Len(t, code, tt.digits)

			ok, err := totp.ValidateTOTP(secret, code, opts...)
			require.NoError(t, err)
			require.True(t, ok, "code generated with non-default params must validate with the same params")

			// A mismatched algorithm must NOT validate (proves the params are honored).
			wrongAlg := "SHA512"
			if tt.algorithm == "SHA512" {
				wrongAlg = "SHA256"
			}
			ok, err = totp.ValidateTOTP(secret, code,
				totp.WithAlgorithm(wrongAlg),
				totp.WithDigits(tt.digits),
				totp.WithPeriod(tt.period),
			)
			require.NoError(t, err)
			require.False(t, ok, "code must not validate under a different algorithm")
		})
	}
}

// TestTOTP_WithParamsHonorsURI proves the URI parameters and validation agree:
// the same TOTPParams used to build the provisioning URI, threaded through
// WithParams, generate a code that validates.
func TestTOTP_WithParamsHonorsURI(t *testing.T) {
	t.Parallel()

	secret, err := totp.GenerateSecretKey()
	require.NoError(t, err)

	params := totp.TOTPParams{
		Secret:      secret,
		AccountName: "u@example.com",
		Issuer:      "App",
		Algorithm:   "SHA256",
		Digits:      8,
		Period:      60,
	}

	uri, err := totp.GetTOTPURI(params)
	require.NoError(t, err)
	require.Contains(t, uri, "algorithm=SHA256")
	require.Contains(t, uri, "digits=8")
	require.Contains(t, uri, "period=60")

	code, err := totp.GenerateTOTP(secret, totp.WithParams(params))
	require.NoError(t, err)
	require.Len(t, code, 8)

	ok, err := totp.ValidateTOTP(secret, code, totp.WithParams(params))
	require.NoError(t, err)
	require.True(t, ok)
}

func TestTOTP_InvalidOptions(t *testing.T) {
	t.Parallel()

	secret, err := totp.GenerateSecretKey()
	require.NoError(t, err)

	t.Run("unsupported algorithm", func(t *testing.T) {
		t.Parallel()
		_, err := totp.GenerateTOTP(secret, totp.WithAlgorithm("MD5"))
		require.ErrorIs(t, err, totp.ErrUnsupportedAlgorithm)
	})

	t.Run("zero digits", func(t *testing.T) {
		t.Parallel()
		_, err := totp.GenerateTOTP(secret, totp.WithDigits(0))
		require.ErrorIs(t, err, totp.ErrInvalidDigits)
	})

	t.Run("zero period", func(t *testing.T) {
		t.Parallel()
		ok, err := totp.ValidateTOTP(secret, "123456", totp.WithPeriod(0))
		require.ErrorIs(t, err, totp.ErrInvalidPeriod)
		require.False(t, ok)
	})
}

func pow10(n int) int64 {
	result := int64(1)
	for range n {
		result *= 10
	}
	return result
}

func TestValidateTOTPWithTimeWindow(t *testing.T) {
	t.Parallel()
	validSecret, err := totp.GenerateSecretKey()
	require.NoError(t, err)
	require.NotEmpty(t, validSecret)

	// Generate OTPs for -30s, now, and +30s
	pastOTP, err := totp.GenerateTOTPWithTime(validSecret, time.Now().Add(-30*time.Second))
	require.NoError(t, err)

	currentOTP, err := totp.GenerateTOTP(validSecret)
	require.NoError(t, err)

	futureOTP, err := totp.GenerateTOTPWithTime(validSecret, time.Now().Add(30*time.Second))
	require.NoError(t, err)

	tests := []struct {
		name   string
		otp    string
		result bool
	}{
		{name: "Past OTP within window", otp: pastOTP, result: true},
		{name: "Current OTP", otp: currentOTP, result: true},
		{name: "Future OTP within window", otp: futureOTP, result: true},
		{name: "Invalid OTP", otp: "000000", result: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := totp.ValidateTOTP(validSecret, tt.otp)
			require.NoError(t, err)
			require.Equal(t, tt.result, result)
		})
	}
}
