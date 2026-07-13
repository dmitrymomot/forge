package totp_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/totp"
)

func TestNew_Defaults(t *testing.T) {
	t.Parallel()
	tp, err := totp.New()
	require.NoError(t, err)
	require.NotNil(t, tp)
}

func TestNew_InvalidConfig(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		opt  totp.Option
	}{
		{"digits 7", totp.WithDigits(7)},
		{"digits 0", totp.WithDigits(0)},
		{"period sub-second", totp.WithPeriod(500 * time.Millisecond)},
		{"period fractional", totp.WithPeriod(30*time.Second + 500*time.Millisecond)},
		{"period zero", totp.WithPeriod(0)},
		{"negative skew", totp.WithSkew(-1)},
		{"unknown algorithm", totp.WithAlgorithm(totp.Algorithm(99))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := totp.New(tt.opt)
			assert.Error(t, err)
		})
	}
}

func TestNew_ValidVariants(t *testing.T) {
	t.Parallel()
	_, err := totp.New(
		totp.WithIssuer("Acme"),
		totp.WithDigits(8),
		totp.WithPeriod(60*time.Second),
		totp.WithAlgorithm(totp.SHA512),
		totp.WithSkew(0),
	)
	require.NoError(t, err)
}

func TestAlgorithm_String(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "SHA1", totp.SHA1.String())
	assert.Equal(t, "SHA256", totp.SHA256.String())
	assert.Equal(t, "SHA512", totp.SHA512.String())
}
