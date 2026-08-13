package flash

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/dmitrymomot/forge/web/cookie"
)

// DefaultCookieName is the cookie a store writes when no option renames it.
const DefaultCookieName = "flash"

// DefaultLifetime bounds how long an unread message waits for the page that shows it.
const DefaultLifetime = 5 * time.Minute

// MaxCookieBytes caps the encoded payload a CookieStore will write. Browsers drop a
// cookie over ~4KB, and the signature and encoding overhead ride on top, so a larger
// payload is refused with ErrTooLarge instead of vanishing silently in the browser.
const MaxCookieBytes = 3072

// CookieStore keeps messages in one signed cookie. It needs no backing store, which
// makes it the right default; the payload travels to the client, so keep the text
// short and never put anything secret in a flash.
type CookieStore struct {
	codec    *cookie.Codec
	name     string
	lifetime time.Duration
}

var _ Store = (*CookieStore)(nil)

// NewCookieStore returns a store that signs its cookie with codec. A nil codec is
// rejected: an unsigned flash cookie is a client-controlled string reaching a
// template.
func NewCookieStore(codec *cookie.Codec, opts ...Option) (*CookieStore, error) {
	if codec == nil {
		return nil, fmt.Errorf("%w: NewCookieStore received a nil *cookie.Codec", ErrInvalidConfig)
	}
	c := config{name: DefaultCookieName, lifetime: DefaultLifetime}
	for _, opt := range opts {
		opt(&c)
	}
	if err := c.validate(); err != nil {
		return nil, err
	}
	return &CookieStore{codec: codec, name: c.name, lifetime: c.lifetime}, nil
}

// Set stages msgs for the next request. Calling it twice on one response replaces
// rather than appends — pass every message in one call. Messages with empty text are
// dropped, and a call left with nothing to say writes no cookie.
//
// A payload too large for a cookie always reports ErrTooLarge, whichever cap catches
// it: base64 and the signature expand the message, so one that clears MaxCookieBytes
// can still overflow the codec's 4096-byte encoded limit.
func (s *CookieStore) Set(w http.ResponseWriter, _ *http.Request, msgs ...Message) error {
	msgs = withText(msgs)
	if len(msgs) == 0 {
		return nil
	}
	raw := encode(msgs)
	if len(raw) > MaxCookieBytes {
		return fmt.Errorf("%w: %d bytes exceeds the %d-byte flash cookie cap", ErrTooLarge, len(raw), MaxCookieBytes)
	}
	err := s.codec.SetSigned(w, s.name, raw, cookie.WithWriteMaxAge(s.lifetime))
	if errors.Is(err, cookie.ErrTooLarge) {
		return fmt.Errorf("%w: %v", ErrTooLarge, err)
	}
	return err
}

// Take returns the messages the previous response staged and deletes the cookie. A
// missing, expired, or unverifiable cookie reads as no messages and no error.
func (s *CookieStore) Take(w http.ResponseWriter, r *http.Request) ([]Message, error) {
	raw, err := s.codec.GetSigned(r, s.name)
	if err != nil || raw == "" {
		return nil, nil
	}
	s.codec.Delete(w, s.name)
	return decode(raw), nil
}
