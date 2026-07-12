package fingerprint

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/dmitrymomot/forge/core/random"
	"github.com/dmitrymomot/forge/web/clientip"
)

type tokenClaims struct {
	Nonce  string `json:"n"`
	IPHash string `json:"i"`
	Exp    int64  `json:"e"`
}

// IssueToken returns a short-lived signed token binding a fresh nonce, an expiry
// (now + TokenTTL), and a hash of the client IP. Embed it in the page so the JS
// probe can echo it to IngestHandler.
//
// The token is not single-use: it remains valid for repeated IngestHandler
// calls until it expires, and each accepted call overwrites the stored payload
// for its nonce. Single-use isn't enforced because the default carry is a
// stateless signed cookie with nothing server-side to consume; the short
// TokenTTL plus the IP-hash binding limit the effect to the poster overwriting
// their own fingerprint data, so this is intended behavior for v1.
func (fp *Fingerprinter) IssueToken(r *http.Request) (string, error) {
	claims := tokenClaims{
		Nonce:  random.String(16),
		Exp:    fp.clock.Now().Add(fp.cfg.TokenTTL).Unix(),
		IPHash: fp.ipHash(r),
	}
	raw, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(raw)
	return payload + "." + fp.signer.SignString(payload), nil
}

func (fp *Fingerprinter) verifyToken(r *http.Request, token string) (tokenClaims, error) {
	payload, sig, ok := strings.Cut(token, ".")
	if !ok || !fp.signer.VerifyString(payload, sig) {
		return tokenClaims{}, ErrBadToken
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return tokenClaims{}, ErrBadToken
	}
	var claims tokenClaims
	if err := json.Unmarshal(raw, &claims); err != nil {
		return tokenClaims{}, ErrBadToken
	}
	if fp.clock.Now().Unix() > claims.Exp {
		return tokenClaims{}, ErrBadToken
	}
	if !hmac.Equal([]byte(claims.IPHash), []byte(fp.ipHash(r))) {
		return tokenClaims{}, ErrBadToken
	}
	return claims, nil
}

// ipHash is a keyed, non-reversible hash of the client IP, used to bind a token
// to the requester without storing the raw address.
func (fp *Fingerprinter) ipHash(r *http.Request) string {
	m := hmac.New(sha256.New, fp.secret)
	m.Write([]byte(clientip.Get(r)))
	return hex.EncodeToString(m.Sum(nil)[:8])
}
