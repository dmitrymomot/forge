package totp

import (
	"encoding/base32"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/dmitrymomot/forge/core/random"
)

// GenerateSecret returns a new shared secret: cryptographically random
// bytes sized to the configured algorithm's hash length (RFC 4226 §4),
// encoded as unpadded uppercase base32 for authenticator-app entry.
func (t *TOTP) GenerateSecret() (string, error) {
	buf := make([]byte, t.cfg.algorithm.secretSize())
	if err := random.Read(buf); err != nil {
		return "", fmt.Errorf("totp: generate secret: %w", err)
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf), nil
}

// ProvisioningURI renders the otpauth:// Key URI for authenticator-app
// enrollment (render it as a QR with core/qrcode's DataURI, or show it for
// manual entry). The label is Issuer:account (account only when the issuer
// is empty); issuer, algorithm, digits, and period ride as query params so
// the app enrolls with exactly the parameters Verify checks.
func (t *TOTP) ProvisioningURI(secret, account string) string {
	label := escapeLabelPart(account)
	q := url.Values{}
	q.Set("secret", secret)
	if t.cfg.issuer != "" {
		label = escapeLabelPart(t.cfg.issuer) + ":" + label
		q.Set("issuer", t.cfg.issuer)
	}
	q.Set("algorithm", t.cfg.algorithm.String())
	q.Set("digits", strconv.Itoa(t.cfg.digits))
	q.Set("period", strconv.FormatInt(int64(t.cfg.period/time.Second), 10))
	return "otpauth://totp/" + label + "?" + q.Encode()
}

// escapeLabelPart path-escapes an otpauth label component and, unlike
// url.PathEscape, also escapes the colon: a colon inside the issuer or account
// would otherwise be indistinguishable from the Issuer:account separator that
// authenticator apps split on. The authoritative issuer= query param still
// carries the raw value.
func escapeLabelPart(s string) string {
	return strings.ReplaceAll(url.PathEscape(s), ":", "%3A")
}
