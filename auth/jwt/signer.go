package jwt

import (
	"crypto"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/dmitrymomot/forge/crypto/keyset"
)

type signingKey struct {
	kid    string
	alg    Alg
	signer crypto.Signer // nil for HS256
	secret []byte        // HS256 only
}

// Signer issues compact JWTs signed with its primary key. Construct with
// exactly one key source: WithKeyset, WithHS256Keyset, or WithSignerKey.
type Signer struct {
	primary signingKey
	all     []signingKey // primary first, then retired versions
}

// NewSigner builds a Signer from exactly one key-source option.
func NewSigner(opts ...SignerOption) (*Signer, error) {
	var cfg signerConfig
	for _, o := range opts {
		o(&cfg)
	}
	sources := 0
	if cfg.ks != nil {
		sources++
	}
	if cfg.hs != nil {
		sources++
	}
	if cfg.direct != nil {
		sources++
	}
	if sources != 1 {
		return nil, fmt.Errorf("%w: exactly one key source required", ErrBadKey)
	}
	switch {
	case cfg.direct != nil:
		d := cfg.direct
		if d.kid == "" || d.key == nil {
			return nil, fmt.Errorf("%w: WithSignerKey requires a kid and a key", ErrBadKey)
		}
		if err := checkSignerAlg(d.alg, d.key.Public()); err != nil {
			return nil, err
		}
		k := signingKey{kid: d.kid, alg: d.alg, signer: d.key}
		return &Signer{primary: k, all: []signingKey{k}}, nil
	case cfg.hs != nil:
		return newSignerFromKeyset(cfg.hs, hsSigningKey)
	default:
		return newSignerFromKeyset(cfg.ks, asymmetricSigningKey)
	}
}

func hsSigningKey(version int, material []byte) (signingKey, error) {
	if len(material) < minHS256KeyLen {
		return signingKey{}, fmt.Errorf("%w: HS256 key version %d shorter than %d bytes", ErrBadKey, version, minHS256KeyLen)
	}
	return signingKey{kid: strconv.Itoa(version), alg: HS256, secret: material}, nil
}

func asymmetricSigningKey(version int, material []byte) (signingKey, error) {
	parsed, err := x509.ParsePKCS8PrivateKey(material)
	if err != nil {
		return signingKey{}, fmt.Errorf("%w: key version %d is not PKCS#8 DER: %v", ErrBadKey, version, err)
	}
	alg, err := algForPrivate(parsed)
	if err != nil {
		return signingKey{}, fmt.Errorf("key version %d: %w", version, err)
	}
	return signingKey{kid: strconv.Itoa(version), alg: alg, signer: parsed.(crypto.Signer)}, nil
}

func newSignerFromKeyset(ks *keyset.Keyset, build func(int, []byte) (signingKey, error)) (*Signer, error) {
	primaryVersion, primaryMaterial := ks.Primary()
	primary, err := build(primaryVersion, primaryMaterial)
	if err != nil {
		return nil, err
	}
	s := &Signer{primary: primary, all: []signingKey{primary}}
	for version, material := range ks.All() {
		if version == primaryVersion {
			continue
		}
		k, err := build(version, material)
		if err != nil {
			return nil, err
		}
		s.all = append(s.all, k)
	}
	return s, nil
}

// Sign marshals claims with encoding/json and returns the signed compact
// JWT. It signs exactly what it is given — no claims are auto-filled. The
// header carries alg, kid, and typ "JWT".
func (s *Signer) Sign(claims any) (string, error) {
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("jwt: marshal claims: %w", err)
	}
	header, err := json.Marshal(map[string]string{
		"alg": string(s.primary.alg),
		"kid": s.primary.kid,
		"typ": "JWT",
	})
	if err != nil {
		return "", fmt.Errorf("jwt: marshal header: %w", err)
	}
	enc := base64.RawURLEncoding
	input := enc.EncodeToString(header) + "." + enc.EncodeToString(payload)
	sig, err := signBytes(s.primary.alg, s.primary.signer, s.primary.secret, []byte(input))
	if err != nil {
		return "", fmt.Errorf("jwt: sign: %w", err)
	}
	return input + "." + enc.EncodeToString(sig), nil
}
