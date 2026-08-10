package totp

import (
	"bytes"
	"context"
	"slices"
	"sync"
	"time"
)

// Record is one enrollment as the store sees it: the secret is AEAD
// ciphertext and backup codes are SHA-256 hashes — a store never holds
// plaintext material.
type Record struct {
	LastUsedAt   time.Time // step-start of the last verified TOTP; zero = never
	Secret       []byte    // crypto/secret.Box ciphertext
	BackupHashes [][]byte  // SHA-256 of normalized backup codes
	Confirmed    bool      // false while enrollment is pending first-code proof
}

// Store persists enrollment records keyed (tenant, subject); tenant "" is
// the unscoped namespace and stores interpret it only by equality.
// Implementations must be safe for concurrent use. SavePending, Confirm,
// MarkUsed, and ConsumeBackup carry all concurrency correctness — the rest
// is CRUD.
type Store interface {
	// Get returns the record, or ErrNotFound.
	Get(ctx context.Context, tenant, subject string) (*Record, error)
	// Save upserts the record (full replace). Used for operations on an
	// already-confirmed record (e.g. regenerating backup codes); enroll and
	// confirm go through SavePending and Confirm.
	Save(ctx context.Context, tenant, subject string, r *Record) error
	// SavePending atomically stores a fresh pending (unconfirmed) enrollment,
	// overwriting an existing pending one but refusing to overwrite a
	// confirmed one. false = a confirmed enrollment already exists. This is
	// the enroll gate: it stops a BeginEnroll from clobbering an enrollment a
	// concurrent Confirm just activated. Only r.Secret is read; the stored
	// record is always pending with no backup codes.
	SavePending(ctx context.Context, tenant, subject string, r *Record) (bool, error)
	// Confirm atomically activates a pending enrollment — setting Confirmed,
	// LastUsedAt, and BackupHashes — only if the stored record is still
	// pending and its Secret still equals expectedSecret. false = no such
	// pending record (already confirmed, gone, or its secret was replaced by
	// a concurrent SavePending). This is the confirm gate: concurrent confirms
	// of the same code resolve to exactly one winner, and a confirm never
	// activates a secret the user did not prove.
	Confirm(ctx context.Context, tenant, subject string, expectedSecret []byte, lastUsedAt time.Time, hashes [][]byte) (bool, error)
	// Delete removes the record; absent is a no-op.
	Delete(ctx context.Context, tenant, subject string) error
	// MarkUsed atomically sets LastUsedAt=usedAt iff the stored value is
	// earlier (or zero). false = a concurrent verify already claimed this
	// or a later step, or the record is gone. The replay/race gate.
	MarkUsed(ctx context.Context, tenant, subject string, usedAt time.Time) (bool, error)
	// ConsumeBackup atomically removes hash if present. false = not
	// present (already spent, never existed, or record gone). The
	// single-use gate.
	ConsumeBackup(ctx context.Context, tenant, subject string, hash []byte) (bool, error)
	// DeleteTenant removes every record in tenant, returning the count.
	DeleteTenant(ctx context.Context, tenant string) (int, error)
}

type memKey struct{ tenant, subject string }

type memoryStore struct {
	recs map[memKey]*Record
	mu   sync.RWMutex
}

// NewMemoryStore returns an in-memory Store for tests, development, and
// single-process apps. State does not survive restarts.
func NewMemoryStore() Store {
	return &memoryStore{recs: make(map[memKey]*Record)}
}

// cloneRecord deep-copies so callers and the store never share slices.
// BackupHashes is always non-nil (empty when there are none) — the read
// shape every Store must return so backends are byte-for-byte comparable.
func cloneRecord(r *Record) *Record {
	c := &Record{
		Secret:       slices.Clone(r.Secret),
		Confirmed:    r.Confirmed,
		LastUsedAt:   r.LastUsedAt,
		BackupHashes: make([][]byte, len(r.BackupHashes)),
	}
	for i, h := range r.BackupHashes {
		c.BackupHashes[i] = slices.Clone(h)
	}
	return c
}

func (s *memoryStore) Get(_ context.Context, tenant, subject string) (*Record, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.recs[memKey{tenant, subject}]
	if !ok {
		return nil, ErrNotFound
	}
	return cloneRecord(r), nil
}

func (s *memoryStore) Save(_ context.Context, tenant, subject string, r *Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recs[memKey{tenant, subject}] = cloneRecord(r)
	return nil
}

func (s *memoryStore) SavePending(_ context.Context, tenant, subject string, r *Record) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := memKey{tenant, subject}
	if existing, ok := s.recs[k]; ok && existing.Confirmed {
		return false, nil
	}
	s.recs[k] = &Record{Secret: slices.Clone(r.Secret), BackupHashes: [][]byte{}}
	return true, nil
}

func (s *memoryStore) Confirm(_ context.Context, tenant, subject string, expectedSecret []byte, lastUsedAt time.Time, hashes [][]byte) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.recs[memKey{tenant, subject}]
	if !ok || r.Confirmed || !bytes.Equal(r.Secret, expectedSecret) {
		return false, nil
	}
	r.Confirmed = true
	r.LastUsedAt = lastUsedAt
	r.BackupHashes = make([][]byte, len(hashes))
	for i, h := range hashes {
		r.BackupHashes[i] = slices.Clone(h)
	}
	return true, nil
}

func (s *memoryStore) Delete(_ context.Context, tenant, subject string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.recs, memKey{tenant, subject})
	return nil
}

func (s *memoryStore) MarkUsed(_ context.Context, tenant, subject string, usedAt time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.recs[memKey{tenant, subject}]
	if !ok || (!r.LastUsedAt.IsZero() && !r.LastUsedAt.Before(usedAt)) {
		return false, nil
	}
	r.LastUsedAt = usedAt
	return true, nil
}

func (s *memoryStore) ConsumeBackup(_ context.Context, tenant, subject string, hash []byte) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.recs[memKey{tenant, subject}]
	if !ok {
		return false, nil
	}
	for i, h := range r.BackupHashes {
		if bytes.Equal(h, hash) {
			r.BackupHashes = slices.Delete(r.BackupHashes, i, i+1)
			return true, nil
		}
	}
	return false, nil
}

func (s *memoryStore) DeleteTenant(_ context.Context, tenant string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for k := range s.recs {
		if k.tenant == tenant {
			delete(s.recs, k)
			n++
		}
	}
	return n, nil
}
