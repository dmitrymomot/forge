package cookie

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

// Errors.
var (
	ErrNotFound  = errors.New("cookie: not found")
	ErrNoSecret  = errors.New("cookie: secret required")
	ErrBadSecret = errors.New("cookie: secret must be 32+ bytes")
	ErrBadSig    = errors.New("cookie: invalid signature")
	ErrDecrypt   = errors.New("cookie: decryption failed")
	ErrExpired   = errors.New("cookie: value expired")
	// ErrBadVersion is returned when a framed payload carries an unsupported
	// version byte (i.e. it was produced by a different on-the-wire format).
	ErrBadVersion = errors.New("cookie: unsupported payload version")
)

// payloadVersion is the first byte of every signed/encrypted payload. It allows
// the on-the-wire format to evolve without silently accepting older layouts.
const payloadVersion byte = 1

// payloadHeaderSize is the byte length of the framed payload header:
// 1 version byte + 8 bytes big-endian Unix-seconds expiry (0 = no expiry).
const payloadHeaderSize = 1 + 8

// Config configures the cookie Manager.
type Config struct {
	Secret   string `env:"SECRET"`
	Domain   string `env:"DOMAIN"`
	Path     string `env:"PATH"      envDefault:"/"`
	SameSite string `env:"SAME_SITE" envDefault:"lax"`
	Secure   bool   `env:"SECURE"    envDefault:"false"`
	HTTPOnly bool   `env:"HTTP_ONLY" envDefault:"true"`
}

// Manager handles cookie operations.
type Manager struct {
	domain     string
	path       string
	secret     []byte   // nil = no encryption/signing
	derivedKey [32]byte // SHA-256 of secret, computed once at construction
	sameSite   http.SameSite
	secure     bool
	httpOnly   bool
}

// New creates a cookie Manager with the given config.
// Returns ErrBadSecret if Secret is non-empty but shorter than 32 bytes.
//
// HttpOnly is honored from cfg.HTTPOnly. Configs loaded from the environment
// default HTTPOnly to true (a secure default), but a directly-constructed
// Config{} leaves it at the zero value (false), so set it explicitly for
// server-managed cookies. When SameSite is "none", Secure is forced on because
// browsers reject SameSite=None cookies without the Secure attribute.
func New(cfg Config) (*Manager, error) {
	if cfg.Path == "" {
		cfg.Path = "/"
	}

	sameSite := parseSameSite(cfg.SameSite)

	secure := cfg.Secure
	// SameSite=None is only honored by browsers when Secure is also set, so
	// force it on rather than emitting a cookie the browser will silently drop.
	if sameSite == http.SameSiteNoneMode {
		secure = true
	}

	m := &Manager{
		domain:   cfg.Domain,
		path:     cfg.Path,
		sameSite: sameSite,
		secure:   secure,
		// HttpOnly is honored from the config. Env-loaded configs default it to
		// true (a secure default that blocks client-side JS access to reduce XSS
		// exposure); a directly-constructed Config{} leaves it false, so callers
		// that need server-managed cookies should set HTTPOnly explicitly.
		httpOnly: cfg.HTTPOnly,
	}

	if cfg.Secret != "" {
		if len(cfg.Secret) < 32 {
			return nil, ErrBadSecret
		}
		m.secret = []byte(cfg.Secret)
		// Derive the AES key once at construction rather than per operation.
		m.derivedKey = sha256.Sum256(m.secret)
	}

	return m, nil
}

// parseSameSite converts a string to http.SameSite.
func parseSameSite(s string) http.SameSite {
	switch strings.ToLower(s) {
	case "strict":
		return http.SameSiteStrictMode
	case "none":
		return http.SameSiteNoneMode
	case "lax":
		return http.SameSiteLaxMode
	default:
		return http.SameSiteLaxMode
	}
}

// Get returns a plain cookie value.
func (m *Manager) Get(r *http.Request, name string) (string, error) {
	c, err := r.Cookie(name)
	if err != nil {
		// r.Cookie only ever returns http.ErrNoCookie; map any absence to
		// ErrNotFound so callers have a single sentinel to match.
		return "", ErrNotFound
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
// Returns ErrExpired if the embedded expiry has passed.
func (m *Manager) GetSigned(r *http.Request, name string) (string, error) {
	if m.secret == nil {
		return "", ErrNoSecret
	}

	raw, err := m.Get(r, name)
	if err != nil {
		return "", err
	}

	// Format: base64(payload).base64(signature)
	parts := strings.SplitN(raw, ".", 2)
	if len(parts) != 2 {
		return "", ErrBadSig
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", ErrBadSig
	}

	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", ErrBadSig
	}

	// Verify signature over the cookie name + payload so a value signed for one
	// name cannot be replayed under a different cookie name.
	if !hmac.Equal(sig, m.sign(name, payload)) {
		return "", ErrBadSig
	}

	value, err := unframePayload(payload)
	if err != nil {
		return "", err
	}

	return string(value), nil
}

// SetSigned sets a signed cookie.
// Returns ErrNoSecret if no secret is configured.
//
// maxAge mirrors http.Cookie.MaxAge semantics and is embedded into the signed
// payload: a positive maxAge sets an expiry that GetSigned enforces, while zero
// or negative means no embedded expiry.
func (m *Manager) SetSigned(w http.ResponseWriter, name, value string, maxAge int) error {
	if m.secret == nil {
		return ErrNoSecret
	}

	payload := framePayload([]byte(value), maxAge)
	sig := m.sign(name, payload)

	// Format: base64(payload).base64(signature)
	encoded := base64.RawURLEncoding.EncodeToString(payload) +
		"." + base64.RawURLEncoding.EncodeToString(sig)

	http.SetCookie(w, m.cookie(name, encoded, maxAge))
	return nil
}

// GetEncrypted returns an encrypted cookie value.
// Returns ErrNoSecret if no secret is configured.
// Returns ErrDecrypt if decryption fails.
// Returns ErrExpired if the embedded expiry has passed.
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

	// Bind the cookie name as additional authenticated data so a ciphertext
	// produced for one name cannot be moved to another.
	payload, err := m.decrypt(data, []byte(name))
	if err != nil {
		return "", ErrDecrypt
	}

	value, err := unframePayload(payload)
	if err != nil {
		return "", err
	}

	return string(value), nil
}

// SetEncrypted sets an encrypted cookie.
// Returns ErrNoSecret if no secret is configured.
//
// maxAge mirrors http.Cookie.MaxAge semantics and is embedded into the
// encrypted payload so GetEncrypted can reject replayed values past their
// intended lifetime.
func (m *Manager) SetEncrypted(w http.ResponseWriter, name, value string, maxAge int) error {
	if m.secret == nil {
		return ErrNoSecret
	}

	payload := framePayload([]byte(value), maxAge)
	ciphertext, err := m.encrypt(payload, []byte(name))
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
//
// The flash cookie is deleted on any read attempt that found a cookie, even when
// decryption fails, so a corrupt or tampered flash cookie cannot persist across
// requests.
func (m *Manager) Flash(w http.ResponseWriter, r *http.Request, key string, dest any) error {
	if m.secret == nil {
		return ErrNoSecret
	}

	name := "flash_" + key
	raw, err := m.GetEncrypted(r, name)
	if err != nil {
		// Only ErrNotFound means there was nothing to clear. For every other
		// failure (decrypt error, expiry, tampering) a cookie was present, so
		// delete it to prevent a broken flash from sticking around.
		if !errors.Is(err, ErrNotFound) {
			m.Delete(w, name)
		}
		return err
	}

	// Delete after a successful read so the flash is single-use.
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

// framePayload prepends a version + expiry header to value. A positive maxAge
// records an absolute Unix-seconds expiry; zero or negative records 0 (no
// expiry), matching http.Cookie.MaxAge semantics.
func framePayload(value []byte, maxAge int) []byte {
	payload := make([]byte, payloadHeaderSize+len(value))
	payload[0] = payloadVersion

	var expiry int64
	if maxAge > 0 {
		expiry = time.Now().Add(time.Duration(maxAge) * time.Second).Unix()
	}
	binary.BigEndian.PutUint64(payload[1:payloadHeaderSize], uint64(expiry))

	copy(payload[payloadHeaderSize:], value)
	return payload
}

// unframePayload validates the header and returns the embedded value, returning
// ErrExpired when the recorded expiry has passed.
func unframePayload(payload []byte) ([]byte, error) {
	if len(payload) < payloadHeaderSize {
		return nil, ErrBadSig
	}
	// A mismatched version byte means the payload was produced by a different
	// on-the-wire format rather than tampered with, so surface a distinct
	// sentinel instead of the misleading ErrBadSig.
	if payload[0] != payloadVersion {
		return nil, ErrBadVersion
	}

	expiry := int64(binary.BigEndian.Uint64(payload[1:payloadHeaderSize]))
	if expiry != 0 && time.Now().Unix() >= expiry {
		return nil, ErrExpired
	}

	return payload[payloadHeaderSize:], nil
}

// sign computes the HMAC-SHA256 over the cookie name and payload. Binding the
// name prevents a value from being valid under a different cookie name.
func (m *Manager) sign(name string, payload []byte) []byte {
	mac := hmac.New(sha256.New, m.secret)
	mac.Write([]byte(name))
	mac.Write([]byte{0}) // domain separator between name and payload
	mac.Write(payload)
	return mac.Sum(nil)
}

// encrypt uses AES-256-GCM with aad bound as additional authenticated data.
func (m *Manager) encrypt(plaintext, aad []byte) ([]byte, error) {
	aead, err := m.aead()
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	return aead.Seal(nonce, nonce, plaintext, aad), nil
}

// decrypt uses AES-256-GCM, requiring aad to match the value used at encrypt time.
func (m *Manager) decrypt(ciphertext, aad []byte) ([]byte, error) {
	aead, err := m.aead()
	if err != nil {
		return nil, err
	}

	if len(ciphertext) < aead.NonceSize() {
		return nil, errors.New("ciphertext too short")
	}

	nonce := ciphertext[:aead.NonceSize()]
	ciphertext = ciphertext[aead.NonceSize():]

	return aead.Open(nil, nonce, ciphertext, aad)
}

// aead builds an AES-256-GCM AEAD from the cached derived key.
func (m *Manager) aead() (cipher.AEAD, error) {
	block, err := aes.NewCipher(m.derivedKey[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
