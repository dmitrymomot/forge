package totp_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/totp"
)

// FuzzVerify asserts Verify never panics on hostile secret/code input —
// malformed base32 must surface as an error.
func FuzzVerify(f *testing.F) {
	f.Add("GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ", "123456")
	f.Add("", "")
	f.Add("!!!!", "00000000")
	f.Add("gezd gnbv====", "abcdef")
	f.Add("A", "999999999999999999")
	tp, err := totp.New()
	require.NoError(f, err)
	f.Fuzz(func(t *testing.T, secret, code string) {
		_, _ = tp.Verify(secret, code, time.Time{})
		_, _ = tp.Code(secret, time.Unix(59, 0))
		_, _ = tp.HOTPCode(secret, 42)
	})
}

// FuzzBackupCode asserts normalization/verification never panics and that
// verification of arbitrary input against arbitrary hash sets is total.
func FuzzBackupCode(f *testing.F) {
	f.Add("abcde-fghij", []byte("0123456789abcdef0123456789abcdef"))
	f.Add("", []byte{})
	f.Add("ABCDE FGHIJ  ", []byte{1})
	f.Fuzz(func(t *testing.T, code string, hash []byte) {
		_, _ = totp.VerifyBackupCode(code, [][]byte{hash})
		_, _ = totp.VerifyBackupCode(code, nil)
	})
}
