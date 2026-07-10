package jwt

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
)

// jwk is the wire form of a single RFC 7517 key.
type jwk struct {
	Kty    string   `json:"kty"`
	Kid    string   `json:"kid,omitempty"`
	Alg    string   `json:"alg,omitempty"`
	Use    string   `json:"use,omitempty"`
	N      string   `json:"n,omitempty"`
	E      string   `json:"e,omitempty"`
	Crv    string   `json:"crv,omitempty"`
	X      string   `json:"x,omitempty"`
	Y      string   `json:"y,omitempty"`
	KeyOps []string `json:"key_ops,omitempty"`
}

type jwkSet struct {
	Keys []jwk `json:"keys"`
}

// publicJWK renders an asymmetric verification key as a JWK. HS256 and
// unknown key types report ok=false.
func publicJWK(k Key) (jwk, bool) {
	enc := base64.RawURLEncoding
	out := jwk{Kid: k.KID, Alg: string(k.Alg), Use: "sig"}
	switch pub := k.Key.(type) {
	case *rsa.PublicKey:
		out.Kty = "RSA"
		out.N = enc.EncodeToString(pub.N.Bytes())
		out.E = enc.EncodeToString(big.NewInt(int64(pub.E)).Bytes())
	case *ecdsa.PublicKey:
		// pub.Bytes() is the SEC1 uncompressed point 0x04 || X(32) || Y(32); slice out the two P-256 coordinates.
		b, err := pub.Bytes()
		if err != nil || len(b) != 65 {
			return jwk{}, false
		}
		out.Kty, out.Crv = "EC", "P-256"
		out.X = enc.EncodeToString(b[1:33])
		out.Y = enc.EncodeToString(b[33:65])
	case ed25519.PublicKey:
		out.Kty, out.Crv = "OKP", "Ed25519"
		out.X = enc.EncodeToString(pub)
	default:
		return jwk{}, false
	}
	return out, true
}

// JWKS returns an http.Handler serving the signer's asymmetric public keys
// as an RFC 7517 JWK set. HS256 keys are never published; an HS256-only
// signer serves an empty set. The body is rendered once at handler
// construction — signer keys do not change after NewSigner.
func (s *Signer) JWKS(opts ...ServeOption) http.Handler {
	var cfg serveConfig
	for _, o := range opts {
		o(&cfg)
	}
	set := jwkSet{Keys: []jwk{}}
	for _, k := range s.PublicKeys() {
		if j, ok := publicJWK(k); ok {
			set.Keys = append(set.Keys, j)
		}
	}
	body, err := json.Marshal(set)
	if err != nil {
		// Key material was validated at NewSigner; this is unreachable.
		panic(fmt.Sprintf("jwt: marshal jwks: %v", err))
	}
	cacheControl := ""
	if cfg.maxAge > 0 {
		cacheControl = fmt.Sprintf("public, max-age=%d", int(cfg.maxAge.Seconds()))
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if cacheControl != "" {
			w.Header().Set("Cache-Control", cacheControl)
		}
		_, _ = w.Write(body)
	})
}
