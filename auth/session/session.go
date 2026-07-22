package session

import (
	"encoding/json"
	"time"

	"github.com/dmitrymomot/forge/core/id"
)

// Session is one visitor's record plus its decoded payload. It is
// request-scoped and NOT safe for concurrent use: a handler that fans out to
// goroutines must serialize access itself.
type Session struct {
	raw     map[string]json.RawMessage // lazily parsed from rec.Payload
	cache   map[string]any             // decoded namespace values
	dirty   map[string]struct{}
	now     func() time.Time
	token   string
	rec     Record
	parsed  bool
	isNew   bool
	deleted bool
}

func newSession(rec Record, token string, isNew bool, now func() time.Time) *Session {
	return &Session{rec: rec, token: token, isNew: isNew, now: now}
}

// ID returns the stable session id. It survives token rotation.
func (s *Session) ID() id.UUID { return s.rec.ID }

// UserID returns the bound principal, or "" for an anonymous session.
func (s *Session) UserID() string { return s.rec.UserID }

// Token returns the credential the client should present next. It changes on
// every rotation, so read it after Authenticate, not before.
func (s *Session) Token() string { return s.token }

// CreatedAt returns when the session was first minted.
func (s *Session) CreatedAt() time.Time { return s.rec.CreatedAt }

// ExpiresAt returns the effective deadline: the lesser of the idle and
// absolute limits.
func (s *Session) ExpiresAt() time.Time { return s.rec.ExpiresAt }

// LastSeenAt returns the last time a request carried this session.
func (s *Session) LastSeenAt() time.Time { return s.rec.LastSeenAt }

// ElevatedAt returns when identity was last re-proved. Zero means never.
func (s *Session) ElevatedAt() time.Time { return s.rec.ElevatedAt }

// IP returns the client address pinned when the session was created.
func (s *Session) IP() string { return s.rec.IP }

// UserAgent returns the user agent pinned when the session was created.
func (s *Session) UserAgent() string { return s.rec.UserAgent }

// Fingerprint returns the device fingerprint hash pinned at creation.
func (s *Session) Fingerprint() string { return s.rec.Fingerprint }

// Remembered reports whether this session uses the remember-me deadlines.
// Transports read it to choose persistent versus session-scoped storage.
func (s *Session) Remembered() bool { return s.rec.Remembered }

// IsNew reports whether the session has never been persisted.
func (s *Session) IsNew() bool { return s.isNew }

// Authenticated reports whether a principal is bound.
func (s *Session) Authenticated() bool { return s.rec.UserID != "" }

// ElevatedWithin reports whether identity was re-proved within d of now.
// The manager stamps now via elevatedNow so tests can drive a mock clock.
func (s *Session) ElevatedWithin(d time.Duration) bool {
	if s.rec.ElevatedAt.IsZero() || d <= 0 {
		return false
	}
	return s.now().Sub(s.rec.ElevatedAt) < d
}

// parse materializes the raw namespace map on first payload access.
func (s *Session) parse() error {
	if s.parsed {
		return nil
	}
	s.parsed = true
	s.raw = make(map[string]json.RawMessage)
	if len(s.rec.Payload) == 0 {
		return nil
	}
	return json.Unmarshal(s.rec.Payload, &s.raw)
}

// markDirty records a namespace write. Only dirty namespaces re-encode on save.
func (s *Session) markDirty(name string, v any) {
	if s.cache == nil {
		s.cache = make(map[string]any, 1)
	}
	if s.dirty == nil {
		s.dirty = make(map[string]struct{}, 1)
	}
	s.cache[name] = v
	s.dirty[name] = struct{}{}
}

func (s *Session) isDirty() bool { return len(s.dirty) > 0 }

// encode folds dirty namespaces back into the payload. Namespaces the process
// never touched keep their original bytes, so a plugin's keys survive a save by
// a handler that has never heard of that plugin.
func (s *Session) encode() error {
	if !s.isDirty() {
		return nil
	}
	if err := s.parse(); err != nil {
		return err
	}
	for name := range s.dirty {
		v, ok := s.cache[name]
		if !ok || v == nil {
			delete(s.raw, name)
			continue
		}
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		s.raw[name] = b
	}
	if len(s.raw) == 0 {
		s.rec.Payload = nil
		clear(s.dirty)
		return nil
	}
	b, err := json.Marshal(s.raw)
	if err != nil {
		return err
	}
	s.rec.Payload = b
	clear(s.dirty)
	return nil
}

func (s *Session) record() Record { return s.rec }
