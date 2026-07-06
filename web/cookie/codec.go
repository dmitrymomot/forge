package cookie

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/dmitrymomot/forge/crypto/keyset"
	"github.com/dmitrymomot/forge/crypto/secret"
	"github.com/dmitrymomot/forge/crypto/sign"
)

// maxEncodedLen is the practical Set-Cookie size limit shared by browsers.
const maxEncodedLen = 4096

const hostPrefix = "__Host-"

// Codec writes and reads plain, signed, and encrypted cookies under one
// policy. Signed = tamper-proof but client-readable (HMAC). Encrypted =
// tamper-proof AND opaque (AEAD; the auth tag makes a separate signature
// redundant).
type Codec struct {
	ks     *keyset.Keyset
	signer *sign.Signer
	boxes  sync.Map // cookie name -> *secret.Box, AAD-bound to the name
	pol    policy
}

// New builds a Codec over ks. Signing and encryption keys derive from the
// same keyset, so rotation is one operation.
func New(ks *keyset.Keyset, opts ...Option) (*Codec, error) {
	if ks == nil {
		return nil, fmt.Errorf("%w: nil keyset", ErrInvalidConfig)
	}
	pol := policy{path: "/", sameSite: http.SameSiteLaxMode, secure: true, httpOnly: true}
	for _, o := range opts {
		o(&pol)
	}
	if pol.sameSite == http.SameSiteNoneMode && !pol.secure {
		return nil, fmt.Errorf("%w: SameSite=none requires Secure", ErrInvalidConfig)
	}
	signer, err := sign.FromKeyset(ks)
	if err != nil {
		return nil, err
	}
	return &Codec{ks: ks, signer: signer, pol: pol}, nil
}

// FromConfig builds the keyset from cfg.Keys and applies the policy fields.
// Empty Path/SameSite normalize to the defaults; opts apply last and win.
func FromConfig(cfg Config, opts ...Option) (*Codec, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	ks, err := keyset.New(keyset.WithBase64Keys(cfg.Keys))
	if err != nil {
		return nil, err
	}
	sameSite, err := parseSameSite(cfg.SameSite)
	if err != nil {
		return nil, err
	}
	path := cfg.Path
	if path == "" {
		path = "/"
	}
	base := []Option{
		WithPath(path),
		WithDomain(cfg.Domain),
		WithMaxAge(cfg.MaxAge),
		WithSameSite(sameSite),
		WithSecure(cfg.Secure),
		WithHTTPOnly(cfg.HTTPOnly),
	}
	return New(ks, append(base, opts...)...)
}

// SupportsHostPrefix reports whether the codec policy satisfies the __Host-
// cookie-prefix rules (Secure, Path=/, host-only).
func (c *Codec) SupportsHostPrefix() bool {
	return c.pol.secure && c.pol.path == "/" && c.pol.domain == ""
}

// Set writes a plain cookie with the codec policy applied. Use it for
// non-sensitive values so the app never mixes stdlib http.SetCookie calls
// (with forgotten flags) into a codec-managed cookie surface.
func (c *Codec) Set(w http.ResponseWriter, name, value string, opts ...WriteOption) error {
	return c.write(w, name, value, opts)
}

// Get reads a plain cookie. Absent cookies return ErrInvalidCookie, matching
// the signed/encrypted paths.
func (c *Codec) Get(r *http.Request, name string) (string, error) {
	ck, err := r.Cookie(name)
	if err != nil {
		return "", ErrInvalidCookie
	}
	return ck.Value, nil
}

// SetSigned writes value with an HMAC bound to the cookie name, so a value
// minted for one cookie cannot be replayed as another.
func (c *Codec) SetSigned(w http.ResponseWriter, name, value string, opts ...WriteOption) error {
	mac := c.signer.SignString(bindName(name, value))
	encoded := base64.RawURLEncoding.EncodeToString([]byte(value)) + "." + mac
	return c.write(w, name, encoded, opts)
}

// GetSigned reads and verifies a signed cookie. Any failure — absent,
// malformed, bad signature, wrong name — returns ErrInvalidCookie.
func (c *Codec) GetSigned(r *http.Request, name string) (string, error) {
	ck, err := r.Cookie(name)
	if err != nil {
		return "", ErrInvalidCookie
	}
	rawValue, mac, ok := strings.Cut(ck.Value, ".")
	if !ok {
		return "", ErrInvalidCookie
	}
	vb, err := base64.RawURLEncoding.DecodeString(rawValue)
	if err != nil {
		return "", ErrInvalidCookie
	}
	value := string(vb)
	if !c.signer.VerifyString(bindName(name, value), mac) {
		return "", ErrInvalidCookie
	}
	return value, nil
}

// SetEncrypted writes value AEAD-encrypted with the cookie name as AAD.
func (c *Codec) SetEncrypted(w http.ResponseWriter, name, value string, opts ...WriteOption) error {
	box, err := c.boxFor(name)
	if err != nil {
		return err
	}
	enc, err := box.EncryptString(value)
	if err != nil {
		return err
	}
	return c.write(w, name, enc, opts)
}

// GetEncrypted reads and decrypts an encrypted cookie. Any failure returns
// ErrInvalidCookie.
func (c *Codec) GetEncrypted(r *http.Request, name string) (string, error) {
	ck, err := r.Cookie(name)
	if err != nil {
		return "", ErrInvalidCookie
	}
	box, err := c.boxFor(name)
	if err != nil {
		return "", err
	}
	value, err := box.DecryptString(ck.Value)
	if err != nil {
		return "", ErrInvalidCookie
	}
	return value, nil
}

// Delete expires the named cookie under the codec policy's path/domain.
func (c *Codec) Delete(w http.ResponseWriter, name string) {
	ck := &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     c.pol.path,
		Domain:   c.pol.domain,
		MaxAge:   -1,
		Secure:   c.pol.secure,
		HttpOnly: c.pol.httpOnly,
		SameSite: c.pol.sameSite,
	}
	if strings.HasPrefix(name, hostPrefix) {
		ck.Path = "/"
		ck.Domain = ""
	}
	http.SetCookie(w, ck)
}

func bindName(name, value string) string { return name + "\x00" + value }

func (c *Codec) boxFor(name string) (*secret.Box, error) {
	if b, ok := c.boxes.Load(name); ok {
		return b.(*secret.Box), nil
	}
	b, err := secret.FromKeyset(c.ks, secret.WithAAD([]byte("forge/web/cookie:"+name)))
	if err != nil {
		return nil, err
	}
	actual, _ := c.boxes.LoadOrStore(name, b)
	return actual.(*secret.Box), nil
}

func (c *Codec) write(w http.ResponseWriter, name, value string, opts []WriteOption) error {
	pol := c.pol
	for _, o := range opts {
		o(&pol)
	}
	if strings.HasPrefix(name, hostPrefix) && (!pol.secure || pol.path != "/" || pol.domain != "") {
		return fmt.Errorf("%w: %s cookie requires Secure, Path=/, and no Domain", ErrInvalidConfig, hostPrefix)
	}
	ck := &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     pol.path,
		Domain:   pol.domain,
		Secure:   pol.secure,
		HttpOnly: pol.httpOnly,
		SameSite: pol.sameSite,
	}
	switch {
	case pol.maxAge > 0:
		ck.MaxAge = int(pol.maxAge / time.Second)
	case pol.maxAge < 0:
		ck.MaxAge = -1
	}
	if err := ck.Valid(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidCookie, err)
	}
	if encoded := ck.String(); len(encoded) > maxEncodedLen {
		return fmt.Errorf("%w: %d bytes", ErrTooLarge, len(encoded))
	}
	http.SetCookie(w, ck)
	return nil
}
