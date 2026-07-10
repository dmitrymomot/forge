package jwt

import (
	"context"
	"crypto"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/crypto/keyset"
	"github.com/dmitrymomot/forge/web/httpclient"
)

// maxTokenLen bounds Verify input before any decoding (DoS guard).
const maxTokenLen = 64 << 10

const defaultLeeway = 30 * time.Second

// Verifier checks compact JWTs against a fixed key set and claim policy.
// Key sources are combinable; policy is fixed at construction.
type Verifier struct {
	clk        clock.Clock
	static     map[string]Key
	jwks       *jwksCache // nil unless WithJWKSURL
	iss        string
	aud        string
	leeway     time.Duration
	requireExp bool
}

// NewVerifier builds a Verifier from at least one key source.
func NewVerifier(opts ...VerifierOption) (*Verifier, error) {
	cfg := verifierConfig{
		leeway:     defaultLeeway,
		requireExp: true,
		clk:        clock.System(),
		jwksCfg:    jwksConfig{refresh: time.Hour, cooldown: time.Minute, fetchTimeout: 30 * time.Second},
	}
	for _, o := range opts {
		o(&cfg)
	}
	keys := cfg.keys
	for _, ks := range cfg.keysets {
		expanded, err := verifyKeysFromKeyset(ks)
		if err != nil {
			return nil, err
		}
		keys = append(keys, expanded...)
	}
	for _, ks := range cfg.hsKeysets {
		for version, material := range ks.All() {
			keys = append(keys, Key{KID: strconv.Itoa(version), Alg: HS256, Key: material})
		}
	}
	static := make(map[string]Key, len(keys))
	for _, k := range keys {
		if err := checkVerifyKey(k); err != nil {
			return nil, err
		}
		if _, dup := static[k.KID]; dup {
			return nil, fmt.Errorf("%w: duplicate kid %q", ErrBadKey, k.KID)
		}
		static[k.KID] = k
	}
	if len(static) == 0 && cfg.jwksURL == "" {
		return nil, fmt.Errorf("%w: at least one key source required", ErrBadKey)
	}
	v := &Verifier{
		static:     static,
		iss:        cfg.iss,
		aud:        cfg.aud,
		leeway:     cfg.leeway,
		requireExp: cfg.requireExp,
		clk:        cfg.clk,
	}
	if cfg.jwksURL != "" {
		client := cfg.jwksCfg.client
		if client == nil {
			client = httpclient.New()
		}
		v.jwks = &jwksCache{
			url:          cfg.jwksURL,
			client:       client,
			refresh:      cfg.jwksCfg.refresh,
			cooldown:     cfg.jwksCfg.cooldown,
			fetchTimeout: cfg.jwksCfg.fetchTimeout,
			clk:          cfg.clk,
		}
	}
	return v, nil
}

func verifyKeysFromKeyset(ks *keyset.Keyset) ([]Key, error) {
	var keys []Key
	for version, material := range ks.All() {
		parsed, err := x509.ParsePKCS8PrivateKey(material)
		if err != nil {
			return nil, fmt.Errorf("%w: key version %d is not PKCS#8 DER: %v", ErrBadKey, version, err)
		}
		alg, err := algForPrivate(parsed)
		if err != nil {
			return nil, fmt.Errorf("key version %d: %w", version, err)
		}
		keys = append(keys, Key{KID: strconv.Itoa(version), Alg: alg, Key: parsed.(crypto.Signer).Public()})
	}
	return keys, nil
}

// Verify checks token against v's keys and claim policy and unmarshals the
// payload into T. T should embed Claims. It is a package-level function
// because Go methods cannot take type parameters.
func Verify[T any](ctx context.Context, v *Verifier, token string) (*T, error) {
	_, payload, err := v.verify(ctx, token)
	if err != nil {
		return nil, err
	}
	out := new(T)
	if err := json.Unmarshal(payload, out); err != nil {
		return nil, fmt.Errorf("%w: claims: %v", ErrMalformed, err)
	}
	return out, nil
}

// verify runs parse -> key resolution -> signature -> registered claims and
// returns the validated registered claims plus the raw payload.
func (v *Verifier) verify(ctx context.Context, token string) (*Claims, []byte, error) {
	if len(token) > maxTokenLen {
		return nil, nil, fmt.Errorf("%w: token exceeds %d bytes", ErrMalformed, maxTokenLen)
	}
	h64, rest, ok := strings.Cut(token, ".")
	if !ok {
		return nil, nil, fmt.Errorf("%w: want 3 segments", ErrMalformed)
	}
	p64, s64, ok := strings.Cut(rest, ".")
	if !ok || strings.Contains(s64, ".") {
		return nil, nil, fmt.Errorf("%w: want 3 segments", ErrMalformed)
	}
	enc := base64.RawURLEncoding.Strict()
	headerBytes, err := enc.DecodeString(h64)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: header encoding", ErrMalformed)
	}
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
		Typ string `json:"typ"`
	}
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return nil, nil, fmt.Errorf("%w: header is not a JSON object", ErrMalformed)
	}
	if header.Typ != "" && !strings.EqualFold(header.Typ, "JWT") {
		return nil, nil, fmt.Errorf("%w: unexpected typ %q", ErrMalformed, header.Typ)
	}
	key, err := v.resolveKey(ctx, header.Kid)
	if err != nil {
		return nil, nil, err
	}
	if header.Alg != string(key.Alg) {
		return nil, nil, fmt.Errorf("%w: token alg %q does not match key alg %q", ErrSignature, header.Alg, key.Alg)
	}
	sig, err := enc.DecodeString(s64)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: signature encoding", ErrMalformed)
	}
	if !verifyBytes(key, []byte(h64+"."+p64), sig) {
		return nil, nil, ErrSignature
	}
	payload, err := enc.DecodeString(p64)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: payload encoding", ErrMalformed)
	}
	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, nil, fmt.Errorf("%w: payload is not a JSON object", ErrMalformed)
	}
	if err := v.checkClaims(&claims); err != nil {
		return nil, nil, err
	}
	return &claims, payload, nil
}

func (v *Verifier) checkClaims(c *Claims) error {
	now := v.clk.Now()
	switch {
	case c.ExpiresAt == nil:
		if v.requireExp {
			return fmt.Errorf("%w: exp claim missing", ErrExpired)
		}
	case !now.Before(c.ExpiresAt.Add(v.leeway)):
		return ErrExpired
	}
	if c.NotBefore != nil && now.Before(c.NotBefore.Add(-v.leeway)) {
		return ErrNotYetValid
	}
	if v.iss != "" && c.Issuer != v.iss {
		return ErrIssuerMismatch
	}
	if v.aud != "" && !c.Audience.Contains(v.aud) {
		return ErrAudienceMismatch
	}
	return nil
}

// resolveKey maps a token's kid to a verification key. Without a kid the
// sole static key is used iff the verifier holds exactly one key and no
// JWKS source; there is no try-all-keys fallback.
func (v *Verifier) resolveKey(ctx context.Context, kid string) (Key, error) {
	if kid == "" {
		if v.jwks == nil && len(v.static) == 1 {
			for _, k := range v.static {
				return k, nil
			}
		}
		return Key{}, fmt.Errorf("%w: no kid and no single-key fallback", ErrUnknownKey)
	}
	if k, ok := v.static[kid]; ok {
		return k, nil
	}
	if v.jwks != nil {
		return v.jwks.get(ctx, kid)
	}
	return Key{}, fmt.Errorf("%w: kid %q", ErrUnknownKey, kid)
}
