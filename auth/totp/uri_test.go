package totp_test

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/totp"
)

func TestProvisioningURI_Full(t *testing.T) {
	t.Parallel()
	tp, err := totp.New(totp.WithIssuer("Acme Corp"), totp.WithDigits(8),
		totp.WithPeriod(60*time.Second), totp.WithAlgorithm(totp.SHA256))
	require.NoError(t, err)

	uri := tp.ProvisioningURI("JBSWY3DPEHPK3PXP", "user+tag@acme.com")

	u, err := url.Parse(uri)
	require.NoError(t, err)
	assert.Equal(t, "otpauth", u.Scheme)
	assert.Equal(t, "totp", u.Host)
	// Label is Issuer:account, path-escaped; url.Parse unescapes it.
	assert.Equal(t, "/Acme Corp:user+tag@acme.com", u.Path)

	q := u.Query()
	assert.Equal(t, "JBSWY3DPEHPK3PXP", q.Get("secret"))
	assert.Equal(t, "Acme Corp", q.Get("issuer"))
	assert.Equal(t, "SHA256", q.Get("algorithm"))
	assert.Equal(t, "8", q.Get("digits"))
	assert.Equal(t, "60", q.Get("period"))
}

func TestProvisioningURI_EmptyIssuer(t *testing.T) {
	t.Parallel()
	tp, err := totp.New()
	require.NoError(t, err)
	uri := tp.ProvisioningURI("JBSWY3DPEHPK3PXP", "user@acme.com")

	u, err := url.Parse(uri)
	require.NoError(t, err)
	assert.Equal(t, "/user@acme.com", u.Path, "no issuer prefix in label")
	assert.False(t, u.Query().Has("issuer"), "issuer param omitted")
	assert.Equal(t, "30", u.Query().Get("period"))
	assert.Equal(t, "6", u.Query().Get("digits"))
	assert.Equal(t, "SHA1", u.Query().Get("algorithm"))
}

func TestProvisioningURI_EscapesLabel(t *testing.T) {
	t.Parallel()
	tp, err := totp.New(totp.WithIssuer("Ac/me:Corp"))
	require.NoError(t, err)
	uri := tp.ProvisioningURI("JBSWY3DPEHPK3PXP", "a b/c@x.io")
	// Raw reserved characters must not appear unescaped in the label part.
	label := strings.TrimPrefix(uri[:strings.Index(uri, "?")], "otpauth://totp/")
	assert.NotContains(t, label, " ")
	assert.NotContains(t, label, "/")
	u, err := url.Parse(uri)
	require.NoError(t, err)
	assert.Equal(t, "Ac/me:Corp", u.Query().Get("issuer"))
}
