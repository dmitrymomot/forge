package jwt

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/asn1"
	"fmt"
	"math/big"
)

// Alg identifies a JWS signing algorithm. The set is closed: forge never
// negotiates algorithms and offers no registration hook.
type Alg string

const (
	HS256 Alg = "HS256"
	RS256 Alg = "RS256"
	ES256 Alg = "ES256"
	EdDSA Alg = "EdDSA"
)

// Key is a verification key bound to exactly one algorithm. Key holds
// *rsa.PublicKey, *ecdsa.PublicKey, ed25519.PublicKey, or a []byte secret
// for HS256.
type Key struct {
	Key crypto.PublicKey
	KID string
	Alg Alg
}

const minHS256KeyLen = 32

// algForPrivate infers the pinned alg for a parsed PKCS#8 private key.
func algForPrivate(key any) (Alg, error) {
	switch k := key.(type) {
	case *rsa.PrivateKey:
		if k.N.BitLen() < 2048 {
			return "", fmt.Errorf("%w: RSA key must be at least 2048 bits", ErrBadKey)
		}
		return RS256, nil
	case *ecdsa.PrivateKey:
		if k.Curve != elliptic.P256() {
			return "", fmt.Errorf("%w: ECDSA key must use P-256", ErrBadKey)
		}
		return ES256, nil
	case ed25519.PrivateKey:
		return EdDSA, nil
	default:
		return "", fmt.Errorf("%w: unsupported key type %T", ErrBadKey, key)
	}
}

// checkSignerAlg validates that a caller-supplied crypto.Signer matches its
// declared alg.
func checkSignerAlg(alg Alg, pub crypto.PublicKey) error {
	switch alg {
	case HS256:
		return fmt.Errorf("%w: HS256 needs a secret, use WithHS256Keyset", ErrBadKey)
	case RS256:
		if k, ok := pub.(*rsa.PublicKey); !ok || k.N.BitLen() < 2048 {
			return fmt.Errorf("%w: RS256 requires an RSA key of at least 2048 bits", ErrBadKey)
		}
	case ES256:
		if k, ok := pub.(*ecdsa.PublicKey); !ok || k.Curve != elliptic.P256() {
			return fmt.Errorf("%w: ES256 requires an ECDSA P-256 key", ErrBadKey)
		}
	case EdDSA:
		if _, ok := pub.(ed25519.PublicKey); !ok {
			return fmt.Errorf("%w: EdDSA requires an Ed25519 key", ErrBadKey)
		}
	default:
		return fmt.Errorf("%w: unsupported alg %q", ErrBadKey, alg)
	}
	return nil
}

// checkVerifyKey validates a verification Key's alg/key-type binding.
func checkVerifyKey(k Key) error {
	if k.Alg == HS256 {
		secret, ok := k.Key.([]byte)
		if !ok || len(secret) < minHS256KeyLen {
			return fmt.Errorf("%w: HS256 key must be a []byte secret of at least %d bytes", ErrBadKey, minHS256KeyLen)
		}
		return nil
	}
	return checkSignerAlg(k.Alg, k.Key)
}

// signBytes signs the JWS signing input. Exactly one of signer/secret is set.
func signBytes(alg Alg, signer crypto.Signer, secret, input []byte) ([]byte, error) {
	switch alg {
	case HS256:
		m := hmac.New(sha256.New, secret)
		m.Write(input)
		return m.Sum(nil), nil
	case RS256:
		d := sha256.Sum256(input)
		return signer.Sign(rand.Reader, d[:], crypto.SHA256)
	case ES256:
		d := sha256.Sum256(input)
		der, err := signer.Sign(rand.Reader, d[:], crypto.SHA256)
		if err != nil {
			return nil, err
		}
		return derToRawES256(der)
	case EdDSA:
		return signer.Sign(rand.Reader, input, crypto.Hash(0))
	default:
		return nil, fmt.Errorf("%w: unsupported alg %q", ErrBadKey, alg)
	}
}

// verifyBytes reports whether sig is a valid signature over input for k.
func verifyBytes(k Key, input, sig []byte) bool {
	switch k.Alg {
	case HS256:
		secret, ok := k.Key.([]byte)
		if !ok {
			return false
		}
		m := hmac.New(sha256.New, secret)
		m.Write(input)
		return hmac.Equal(sig, m.Sum(nil))
	case RS256:
		pub, ok := k.Key.(*rsa.PublicKey)
		if !ok {
			return false
		}
		d := sha256.Sum256(input)
		return rsa.VerifyPKCS1v15(pub, crypto.SHA256, d[:], sig) == nil
	case ES256:
		pub, ok := k.Key.(*ecdsa.PublicKey)
		if !ok || len(sig) != 64 {
			return false
		}
		d := sha256.Sum256(input)
		r := new(big.Int).SetBytes(sig[:32])
		s := new(big.Int).SetBytes(sig[32:])
		return ecdsa.Verify(pub, d[:], r, s)
	case EdDSA:
		pub, ok := k.Key.(ed25519.PublicKey)
		if !ok {
			return false
		}
		return ed25519.Verify(pub, input, sig)
	default:
		return false
	}
}

// derToRawES256 converts an ASN.1 DER ECDSA signature (what crypto.Signer
// returns) to the raw fixed-width R||S form JWS requires.
func derToRawES256(der []byte) ([]byte, error) {
	var sig struct{ R, S *big.Int }
	if _, err := asn1.Unmarshal(der, &sig); err != nil {
		return nil, fmt.Errorf("jwt: ES256 signature encoding: %w", err)
	}
	out := make([]byte, 64)
	sig.R.FillBytes(out[:32])
	sig.S.FillBytes(out[32:])
	return out, nil
}
