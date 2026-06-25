package totp

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base32"
	"errors"
	"fmt"
	"hash"
	"math"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	DefaultDigits    = 6      // Standard 6-digit TOTP codes
	DefaultPeriod    = 30     // 30-second validity window (RFC 6238 standard)
	DefaultAlgorithm = "SHA1" // HMAC-SHA1 algorithm (RFC 6238 standard)

	// minSecretBytes is the minimum decoded secret length (80 bits) accepted by
	// generation/validation. RFC 4226 recommends a 128-bit (16-byte) secret and
	// mandates at least 128 bits; we floor at 80 bits to reject obviously weak
	// or empty keys while still accepting common 16-byte test vectors.
	minSecretBytes = 10
)

// validateSecretKeyRegex ensures Base32 format: uppercase A-Z, digits 2-7, optional padding.
// Unexported so consumers cannot clobber the shared compiled regex.
var validateSecretKeyRegex = regexp.MustCompile("^[A-Z2-7]+=*$")

// hashForAlgorithm maps an RFC 6238 algorithm name to its hash constructor.
// Returns nil for unsupported algorithms.
func hashForAlgorithm(algorithm string) func() hash.Hash {
	switch strings.ToUpper(strings.TrimSpace(algorithm)) {
	case "", DefaultAlgorithm:
		return sha1.New
	case "SHA256":
		return sha256.New
	case "SHA512":
		return sha512.New
	default:
		return nil
	}
}

// decodeSecret normalizes, validates, and Base32-decodes a TOTP secret.
// It rejects empty/too-short secrets that would otherwise produce a weak,
// near-all-zero HMAC key.
func decodeSecret(secret string) ([]byte, error) {
	secret = strings.TrimSpace(strings.ToUpper(secret))
	if !validateSecretKeyRegex.MatchString(secret) {
		return nil, ErrInvalidSecret
	}

	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		return nil, err
	}

	if len(key) < minSecretBytes {
		return nil, ErrSecretTooShort
	}

	return key, nil
}

// TOTPParams contains the parameters for TOTP URI generation
type TOTPParams struct {
	Secret      string // Base32-encoded TOTP secret key (required)
	AccountName string // User identifier like email (required)
	Issuer      string // Service name displayed in authenticator apps (required)
	Algorithm   string // HMAC algorithm (optional, defaults to SHA1)
	Digits      int    // Number of digits in generated codes (optional, defaults to 6)
	Period      int    // Code validity period in seconds (optional, defaults to 30)
}

// Validate ensures all required TOTP parameters are present and valid
func (p TOTPParams) Validate() error {
	if p.Secret == "" {
		return ErrMissingSecret
	}
	if _, err := decodeSecret(p.Secret); err != nil {
		return err
	}
	if p.AccountName == "" {
		return ErrMissingAccountName
	}
	if p.Issuer == "" {
		return ErrMissingIssuer
	}
	if p.Algorithm != "" && hashForAlgorithm(p.Algorithm) == nil {
		return ErrUnsupportedAlgorithm
	}
	if p.Digits < 0 {
		return ErrInvalidDigits
	}
	if p.Period < 0 {
		return ErrInvalidPeriod
	}
	return nil
}

// GetDefaults returns a copy with RFC 6238 standard defaults applied to zero-valued fields
func (p TOTPParams) GetDefaults() TOTPParams {
	if p.Algorithm == "" {
		p.Algorithm = DefaultAlgorithm
	}
	if p.Digits == 0 {
		p.Digits = DefaultDigits
	}
	if p.Period == 0 {
		p.Period = DefaultPeriod
	}
	return p
}

// GenerateSecretKey generates a new Base32-encoded secret key for TOTP.
func GenerateSecretKey() (string, error) {
	secret := make([]byte, 20) // 160-bit secret (RFC 4226 recommendation for cryptographic strength)
	if _, err := rand.Read(secret); err != nil {
		return "", errors.Join(ErrFailedToGenerateSecretKey, err)
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret), nil
}

// GetTOTPURI creates a properly encoded TOTP URI for use with authenticator apps.
// The URI format follows the Key Uri Format specification:
// https://github.com/google/google-authenticator/wiki/Key-Uri-Format
func GetTOTPURI(params TOTPParams) (string, error) {
	if err := params.Validate(); err != nil {
		return "", err
	}

	params = params.GetDefaults()

	label := fmt.Sprintf("%s:%s",
		url.PathEscape(params.Issuer),
		url.PathEscape(params.AccountName),
	)

	query := url.Values{}
	query.Set("secret", params.Secret)
	query.Set("issuer", params.Issuer)
	query.Set("algorithm", params.Algorithm)
	query.Set("digits", fmt.Sprintf("%d", params.Digits))
	query.Set("period", fmt.Sprintf("%d", params.Period))

	uri := fmt.Sprintf("otpauth://totp/%s?%s", label, query.Encode())

	return uri, nil
}

// TOTPConfig holds the algorithm parameters used by generation and validation.
// Construct it from defaults via newTOTPConfig and tune it with TOTPOption values
// so that codes are produced and verified with the same Algorithm/Digits/Period
// that GetTOTPURI advertises to authenticator apps.
type totpConfig struct {
	algorithm string
	digits    int
	period    int
}

// TOTPOption customizes the algorithm parameters of ValidateTOTP, GenerateTOTP,
// and GenerateTOTPWithTime so they honor non-default Algorithm/Digits/Period.
type TOTPOption func(*totpConfig)

// WithAlgorithm sets the HMAC algorithm ("SHA1", "SHA256", or "SHA512").
func WithAlgorithm(algorithm string) TOTPOption {
	return func(c *totpConfig) { c.algorithm = algorithm }
}

// WithDigits sets the number of digits in generated codes (typically 6 or 8).
func WithDigits(digits int) TOTPOption {
	return func(c *totpConfig) { c.digits = digits }
}

// WithPeriod sets the code validity period in seconds (typically 30).
func WithPeriod(period int) TOTPOption {
	return func(c *totpConfig) { c.period = period }
}

// WithParams derives algorithm options from a TOTPParams value (after defaults
// are applied) so the same parameters used to build the provisioning URI are
// used for generation and validation.
func WithParams(params TOTPParams) TOTPOption {
	params = params.GetDefaults()
	return func(c *totpConfig) {
		c.algorithm = params.Algorithm
		c.digits = params.Digits
		c.period = params.Period
	}
}

// resolveConfig builds a validated totpConfig from defaults and the supplied options.
func resolveConfig(opts ...TOTPOption) (totpConfig, error) {
	cfg := totpConfig{
		algorithm: DefaultAlgorithm,
		digits:    DefaultDigits,
		period:    DefaultPeriod,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if hashForAlgorithm(cfg.algorithm) == nil {
		return cfg, ErrUnsupportedAlgorithm
	}
	if cfg.digits < 1 {
		return cfg, ErrInvalidDigits
	}
	if cfg.period < 1 {
		return cfg, ErrInvalidPeriod
	}
	return cfg, nil
}

// ValidateTOTP validates the TOTP code provided by the user.
//
// By default it uses the RFC 6238 standard parameters (SHA1, 6 digits, 30s period).
// Pass options such as WithAlgorithm/WithDigits/WithPeriod (or WithParams) to honor
// the exact parameters advertised in the provisioning URI from GetTOTPURI.
func ValidateTOTP(secret, otp string, opts ...TOTPOption) (bool, error) {
	cfg, err := resolveConfig(opts...)
	if err != nil {
		return false, err
	}

	key, err := decodeSecret(secret)
	if err != nil {
		return false, errors.Join(ErrFailedToValidateTOTP, err)
	}

	otp = strings.TrimSpace(otp)
	if !regexp.MustCompile(fmt.Sprintf(`^\d{%d}$`, cfg.digits)).MatchString(otp) {
		return false, ErrInvalidOTP
	}

	counter := time.Now().Unix() / int64(cfg.period)

	// Accept codes from previous, current, and next windows to handle clock drift
	for i := -1; i <= 1; i++ {
		code := generateCode(key, counter+int64(i), cfg.digits, cfg.algorithm)
		if formatCode(code, cfg.digits) == otp {
			return true, nil
		}
	}

	return false, nil
}

// GenerateTOTP generates a time-based one-time password for the current window.
// The secret must be a valid Base32-encoded string.
//
// By default it uses the RFC 6238 standard parameters (SHA1, 6 digits, 30s period);
// pass options to match the parameters advertised in the provisioning URI.
func GenerateTOTP(secret string, opts ...TOTPOption) (string, error) {
	return GenerateTOTPWithTime(secret, time.Now(), opts...)
}

// GenerateHOTP implements the RFC 4226 HMAC-based One-Time Password algorithm
// using HMAC-SHA1. The algorithm converts a counter value into a numeric code.
func GenerateHOTP(key []byte, counter int64, digits int) int {
	return generateCode(key, counter, digits, DefaultAlgorithm)
}

// generateCode implements RFC 4226 dynamic truncation for the given HMAC algorithm.
func generateCode(key []byte, counter int64, digits int, algorithm string) int {
	hasher := hashForAlgorithm(algorithm)
	if hasher == nil {
		hasher = sha1.New
	}

	// Convert counter to big-endian 8-byte array (RFC 4226 requirement)
	counterBytes := make([]byte, 8)
	for i := 7; i >= 0; i-- {
		counterBytes[i] = byte(counter & 0xff)
		counter = counter >> 8
	}

	hmacHash := hmac.New(hasher, key)
	hmacHash.Write(counterBytes)
	hashSum := hmacHash.Sum(nil)

	// Dynamic truncation (RFC 4226): use last 4 bits as offset into hash
	offset := hashSum[len(hashSum)-1] & 0x0f
	// Extract 31-bit value (clear MSB to ensure positive number)
	code := (int(hashSum[offset]&0x7f) << 24) |
		(int(hashSum[offset+1]&0xff) << 16) |
		(int(hashSum[offset+2]&0xff) << 8) |
		(int(hashSum[offset+3] & 0xff))

	// Reduce to desired number of digits
	code = code % int(math.Pow10(digits))

	return code
}

// formatCode zero-pads a numeric code to the desired number of digits.
func formatCode(code, digits int) string {
	return fmt.Sprintf("%0*d", digits, code)
}

// GenerateTOTPWithTime generates a TOTP code for the window containing the specified time.
// Useful for testing or generating codes for specific moments.
//
// By default it uses the RFC 6238 standard parameters; pass options to match the
// parameters advertised in the provisioning URI.
func GenerateTOTPWithTime(secret string, t time.Time, opts ...TOTPOption) (string, error) {
	cfg, err := resolveConfig(opts...)
	if err != nil {
		return "", err
	}

	key, err := decodeSecret(secret)
	if err != nil {
		return "", errors.Join(ErrFailedToGenerateTOTP, err)
	}

	counter := t.Unix() / int64(cfg.period)

	code := generateCode(key, counter, cfg.digits, cfg.algorithm)

	return formatCode(code, cfg.digits), nil
}
