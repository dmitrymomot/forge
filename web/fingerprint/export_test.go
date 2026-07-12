package fingerprint

import "net/http"

// VerifyTokenForTest exposes verifyToken to the black-box test package.
func (fp *Fingerprinter) VerifyTokenForTest(r *http.Request, tok string) error {
	_, err := fp.verifyToken(r, tok)
	return err
}
