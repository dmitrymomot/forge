package flash

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/resilience/cache"
	"github.com/dmitrymomot/forge/web/cookie"
)

// KeyPrefix namespaces the entries a CacheStore writes, so a shared cache backend
// cannot collide with another consumer's keys.
const KeyPrefix = "flash:"

// CacheStore keeps messages server-side and sends the client only a random claim
// ticket. Use it when the text is long enough to strain a cookie, or when it must
// never reach the client until it is rendered. It needs a durable cache.Store when
// the app runs on more than one instance: the LRU memory store may evict a message
// before its page loads, and another instance cannot see it at all.
type CacheStore struct {
	store    cache.Store
	codec    *cookie.Codec
	name     string
	lifetime time.Duration
}

var _ Store = (*CacheStore)(nil)

// NewCacheStore returns a store that holds messages in store and writes a signed
// ticket cookie with codec. Both are required.
func NewCacheStore(store cache.Store, codec *cookie.Codec, opts ...Option) (*CacheStore, error) {
	c := config{name: DefaultCookieName, lifetime: DefaultLifetime}
	if store == nil {
		c.errs = append(c.errs, fmt.Errorf("%w: NewCacheStore received a nil cache.Store", ErrInvalidConfig))
	}
	if codec == nil {
		c.errs = append(c.errs, fmt.Errorf("%w: NewCacheStore received a nil *cookie.Codec", ErrInvalidConfig))
	}
	for _, opt := range opts {
		opt(&c)
	}
	if err := c.validate(); err != nil {
		return nil, err
	}
	return &CacheStore{store: store, codec: codec, name: c.name, lifetime: c.lifetime}, nil
}

// Set writes the messages under a fresh random key and hands the client the key in a
// signed cookie. The key is a ULID, so one client's ticket never names another's
// entry even if the cookie signature were somehow forged.
func (s *CacheStore) Set(w http.ResponseWriter, r *http.Request, msgs ...Message) error {
	msgs = withText(msgs)
	if len(msgs) == 0 {
		return nil
	}
	ticket := id.NewULID().String()
	err := s.store.Set(r.Context(), KeyPrefix+ticket, []byte(encode(msgs)), cache.WithTTL(s.lifetime))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrStore, err)
	}
	return s.codec.SetSigned(w, s.name, ticket, cookie.WithWriteMaxAge(s.lifetime))
}

// Take reads the entry the ticket names, deletes both the entry and the cookie, and
// returns the messages. A missing or unverifiable ticket, and an entry the cache no
// longer holds, read as no messages and no error; only a failing store is an error.
func (s *CacheStore) Take(w http.ResponseWriter, r *http.Request) ([]Message, error) {
	ticket, err := s.codec.GetSigned(r, s.name)
	if err != nil || ticket == "" {
		return nil, nil
	}
	s.codec.Delete(w, s.name)

	key := KeyPrefix + ticket
	raw, err := s.store.Get(r.Context(), key)
	if errors.Is(err, cache.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStore, err)
	}
	if err := s.store.Delete(r.Context(), key); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStore, err)
	}
	return decode(string(raw)), nil
}
