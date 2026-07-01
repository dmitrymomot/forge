package password

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"

	"github.com/dmitrymomot/forge/kdf"
	"github.com/dmitrymomot/forge/random"
	"github.com/dmitrymomot/forge/consttime"
)

const saltLen = 16

// Hash hashes password into a self-describing PHC string (Argon2id by default; bcrypt
// when Algorithm Bcrypt is selected).
func Hash(password string, opts ...Option) (string, error) {
	c := newConfig(opts...)
	if c.algo == Bcrypt {
		h, err := bcrypt.GenerateFromPassword([]byte(password), c.bcost)
		if err != nil {
			return "", fmt.Errorf("password: bcrypt: %w", err)
		}
		return string(h), nil
	}
	if err := c.argon.Validate(); err != nil {
		return "", fmt.Errorf("password: invalid params: %w", err)
	}
	salt := random.Bytes(saltLen)
	key := argon2.IDKey([]byte(password), salt, c.argon.Time, c.argon.Memory, c.argon.Threads, c.argon.KeyLen)
	return encodeArgon(c.argon, salt, key), nil
}

func encodeArgon(p kdf.Params, salt, key []byte) string {
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.Memory, p.Time, p.Threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key))
}

// Verify checks password against encoded, detecting the algorithm from the encoded
// prefix. ok reports whether the password matches; needsRehash is true when the stored
// parameters/algorithm differ from the current defaults (rehash on next login); err is
// non-nil only when encoded is malformed.
func Verify(password, encoded string) (ok bool, needsRehash bool, err error) {
	switch {
	case strings.HasPrefix(encoded, "$argon2id$"):
		return verifyArgon(password, encoded)
	case strings.HasPrefix(encoded, "$2a$"),
		strings.HasPrefix(encoded, "$2b$"),
		strings.HasPrefix(encoded, "$2y$"):
		e := bcrypt.CompareHashAndPassword([]byte(encoded), []byte(password))
		switch {
		case e == nil:
			// Stored as bcrypt; the default target is Argon2id → migrate on login.
			return true, true, nil
		case errIsBcryptMismatch(e):
			return false, false, nil
		default:
			return false, false, fmt.Errorf("%w: %v", ErrInvalidHash, e)
		}
	default:
		return false, false, ErrInvalidHash
	}
}

func errIsBcryptMismatch(e error) bool {
	return errors.Is(e, bcrypt.ErrMismatchedHashAndPassword)
}

func verifyArgon(password, encoded string) (bool, bool, error) {
	parts := strings.Split(encoded, "$") // ["", "argon2id", "v=19", "m=..,t=..,p=..", salt, hash]
	if len(parts) != 6 {
		return false, false, ErrInvalidHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return false, false, ErrInvalidHash
	}
	if version != argon2.Version {
		return false, false, ErrInvalidHash
	}
	var p kdf.Params
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.Memory, &p.Time, &p.Threads); err != nil {
		return false, false, ErrInvalidHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, false, ErrInvalidHash
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, false, ErrInvalidHash
	}
	p.KeyLen = uint32(len(want))
	got := argon2.IDKey([]byte(password), salt, p.Time, p.Memory, p.Threads, p.KeyLen)
	if !consttime.BytesEqual(got, want) {
		return false, false, nil
	}
	def := kdf.DefaultParams()
	needs := p.Time != def.Time || p.Memory != def.Memory || p.Threads != def.Threads || p.KeyLen != def.KeyLen
	return true, needs, nil
}
