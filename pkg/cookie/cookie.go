package cookie

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

// Errors.
var (
	ErrNotFound  = errors.New("cookie: not found")
	ErrNoSecret  = errors.New("cookie: secret required")
	ErrBadSecret = errors.New("cookie: secret must be 32+ bytes")
	ErrBadSig    = errors.New("cookie: invalid signature")
	ErrDecrypt   = errors.New("cookie: decryption failed")
)

// Manager handles cookie operations.
type Manager struct {
	secret   []byte // nil = no encryption/signing
	domain   string
	path     string
	secure   bool
	httpOnly bool
	sameSite http.SameSite
}

// Option configures the Manager.
type Option func(*Manager)

// New creates a cookie Manager with the given options.
func New(opts ...Option) *Manager {
	m := &Manager{
		path:     "/",
		httpOnly: true,
		sameSite: http.SameSiteLaxMode,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// WithSecret sets the secret for signing and encryption.
// Must be at least 32 bytes.
func WithSecret(secret string) Option {
	return func(m *Manager) {
		if len(secret) >= 32 {
			m.secret = []byte(secret)
		}
	}
}

// WithDomain sets the cookie domain.
func WithDomain(domain string) Option {
	return func(m *Manager) {
		m.domain = domain
	}
}

// WithPath sets the cookie path.
func WithPath(path string) Option {
	return func(m *Manager) {
		m.path = path
	}
}

// WithSecure sets the Secure flag.
func WithSecure(secure bool) Option {
	return func(m *Manager) {
		m.secure = secure
	}
}

// WithHTTPOnly sets the HttpOnly flag.
func WithHTTPOnly(httpOnly bool) Option {
	return func(m *Manager) {
		m.httpOnly = httpOnly
	}
}

// WithSameSite sets the SameSite attribute.
func WithSameSite(ss http.SameSite) Option {
	return func(m *Manager) {
		m.sameSite = ss
	}
}

// Get returns a plain cookie value.
func (m *Manager) Get(r *http.Request, name string) (string, error) {
	c, err := r.Cookie(name)
	if err != nil {
		if errors.Is(err, http.ErrNoCookie) {
			return "", ErrNotFound
		}
		return "", err
	}
	return c.Value, nil
}

// Set sets a plain cookie.
func (m *Manager) Set(w http.ResponseWriter, name, value string, maxAge int) {
	http.SetCookie(w, m.cookie(name, value, maxAge))
}

// Delete removes a cookie.
func (m *Manager) Delete(w http.ResponseWriter, name string) {
	http.SetCookie(w, m.cookie(name, "", -1))
}

// GetSigned returns a signed cookie value.
// Returns ErrNoSecret if no secret is configured.
// Returns ErrBadSig if signature verification fails.
func (m *Manager) GetSigned(r *http.Request, name string) (string, error) {
	if m.secret == nil {
		return "", ErrNoSecret
	}

	raw, err := m.Get(r, name)
	if err != nil {
		return "", err
	}

	// Format: base64(value).base64(signature)
	parts := strings.SplitN(raw, ".", 2)
	if len(parts) != 2 {
		return "", ErrBadSig
	}

	value, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", ErrBadSig
	}

	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", ErrBadSig
	}

	// Verify signature
	mac := hmac.New(sha256.New, m.secret)
	mac.Write(value)
	expected := mac.Sum(nil)

	if !hmac.Equal(sig, expected) {
		return "", ErrBadSig
	}

	return string(value), nil
}

// SetSigned sets a signed cookie.
// Returns ErrNoSecret if no secret is configured.
func (m *Manager) SetSigned(w http.ResponseWriter, name, value string, maxAge int) error {
	if m.secret == nil {
		return ErrNoSecret
	}

	// Sign the value
	mac := hmac.New(sha256.New, m.secret)
	mac.Write([]byte(value))
	sig := mac.Sum(nil)

	// Format: base64(value).base64(signature)
	encoded := base64.RawURLEncoding.EncodeToString([]byte(value)) +
		"." + base64.RawURLEncoding.EncodeToString(sig)

	http.SetCookie(w, m.cookie(name, encoded, maxAge))
	return nil
}

// GetEncrypted returns an encrypted cookie value.
// Returns ErrNoSecret if no secret is configured.
// Returns ErrDecrypt if decryption fails.
func (m *Manager) GetEncrypted(r *http.Request, name string) (string, error) {
	if m.secret == nil {
		return "", ErrNoSecret
	}

	raw, err := m.Get(r, name)
	if err != nil {
		return "", err
	}

	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return "", ErrDecrypt
	}

	plaintext, err := m.decrypt(data)
	if err != nil {
		return "", ErrDecrypt
	}

	return string(plaintext), nil
}

// SetEncrypted sets an encrypted cookie.
// Returns ErrNoSecret if no secret is configured.
func (m *Manager) SetEncrypted(w http.ResponseWriter, name, value string, maxAge int) error {
	if m.secret == nil {
		return ErrNoSecret
	}

	ciphertext, err := m.encrypt([]byte(value))
	if err != nil {
		return err
	}

	encoded := base64.RawURLEncoding.EncodeToString(ciphertext)
	http.SetCookie(w, m.cookie(name, encoded, maxAge))
	return nil
}

// Flash reads and deletes a flash message.
// Returns ErrNoSecret if no secret is configured.
// Returns ErrNotFound if the flash cookie doesn't exist.
func (m *Manager) Flash(w http.ResponseWriter, r *http.Request, key string, dest any) error {
	if m.secret == nil {
		return ErrNoSecret
	}

	name := "flash_" + key
	raw, err := m.GetEncrypted(r, name)
	if err != nil {
		return err
	}

	// Delete after reading
	m.Delete(w, name)

	return json.Unmarshal([]byte(raw), dest)
}

// SetFlash sets a flash message.
// Returns ErrNoSecret if no secret is configured.
func (m *Manager) SetFlash(w http.ResponseWriter, key string, value any) error {
	if m.secret == nil {
		return ErrNoSecret
	}

	data, err := json.Marshal(value)
	if err != nil {
		return err
	}

	return m.SetEncrypted(w, "flash_"+key, string(data), 0)
}

// cookie creates a cookie with the manager's defaults.
func (m *Manager) cookie(name, value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     m.path,
		Domain:   m.domain,
		MaxAge:   maxAge,
		Secure:   m.secure,
		HttpOnly: m.httpOnly,
		SameSite: m.sameSite,
	}
}

// encrypt uses AES-GCM.
func (m *Manager) encrypt(plaintext []byte) ([]byte, error) {
	// Derive 32-byte key from secret
	key := sha256.Sum256(m.secret)

	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	return aead.Seal(nonce, nonce, plaintext, nil), nil
}

// decrypt uses AES-GCM.
func (m *Manager) decrypt(ciphertext []byte) ([]byte, error) {
	// Derive 32-byte key from secret
	key := sha256.Sum256(m.secret)

	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	if len(ciphertext) < aead.NonceSize() {
		return nil, errors.New("ciphertext too short")
	}

	nonce := ciphertext[:aead.NonceSize()]
	ciphertext = ciphertext[aead.NonceSize():]

	return aead.Open(nil, nonce, ciphertext, nil)
}
