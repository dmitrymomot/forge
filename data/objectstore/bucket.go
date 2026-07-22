package objectstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"strings"
	"time"

	"github.com/dmitrymomot/forge/core/filetype"
)

// errNotConstructed reports use of a zero-value Bucket that bypassed New.
var errNotConstructed = errors.New("objectstore: bucket not constructed with New")

// Bucket is the consumer-facing facade over a Store: it validates keys,
// detects and enforces content types from magic bytes, caps sizes, and
// applies the optional tenant scope. Construct with New; methods on a
// zero-value Bucket fail.
type Bucket struct {
	store   Store
	scope   func(context.Context) (string, error)
	allowed map[string]struct{}
	maxSize int64
}

// New wraps store with validation and scoping. It fails on a nil store or an
// invalid option.
func New(store Store, opts ...Option) (*Bucket, error) {
	if store == nil {
		return nil, ErrNilStore
	}
	var cfg config
	for _, opt := range opts {
		opt(&cfg)
	}
	if len(cfg.optErrs) > 0 {
		return nil, errors.Join(cfg.optErrs...)
	}
	return &Bucket{
		store:   store,
		scope:   cfg.scope,
		allowed: cfg.allowed,
		maxSize: cfg.maxSize,
	}, nil
}

// scopedKey validates key and, on a scoped bucket, resolves and prepends the
// tenant scope. Fail-closed: any scope problem returns ErrScope.
func (b *Bucket) scopedKey(ctx context.Context, key string) (string, error) {
	if err := ValidateKey(key); err != nil {
		return "", err
	}
	if b.scope == nil {
		return key, nil
	}
	scope, err := b.scopePrefix(ctx)
	if err != nil {
		return "", err
	}
	full := scope + key
	if len(full) > maxKeyLen {
		return "", fmt.Errorf("%w: scoped key longer than %d bytes", ErrInvalidKey, maxKeyLen)
	}
	return full, nil
}

// scopePrefix resolves the tenant scope and returns it with a trailing "/".
func (b *Bucket) scopePrefix(ctx context.Context) (string, error) {
	scope, err := b.scope(ctx)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrScope, err)
	}
	if scope == "" {
		return "", fmt.Errorf("%w: empty scope", ErrScope)
	}
	if err := ValidateKey(scope); err != nil {
		return "", fmt.Errorf("%w: %w", ErrScope, err)
	}
	return scope + "/", nil
}

// Put streams r into the bucket under key. The content type is detected from
// the leading bytes (never taken from the caller), checked against the
// WithAllowedTypes allowlist, and stored with the object. Content over the
// WithMaxSize cap aborts with ErrTooLarge. The returned Info carries the
// caller's key, the detected type, and the byte count written; ModTime is
// zero (the backend assigns it — read via Stat).
func (b *Bucket) Put(ctx context.Context, key string, r io.Reader) (Info, error) {
	if b.store == nil {
		return Info{}, errNotConstructed
	}
	k, err := b.scopedKey(ctx, key)
	if err != nil {
		return Info{}, err
	}
	if r == nil {
		return Info{}, errors.New("objectstore: nil reader")
	}
	metered := &meteredReader{r: r, max: b.maxSize}
	typ, body, err := filetype.DetectReader(metered)
	if err != nil {
		return Info{}, err
	}
	if len(b.allowed) > 0 {
		if _, ok := b.allowed[typ.MIME]; !ok {
			return Info{}, fmt.Errorf("%w: %s", ErrUnsupportedType, typ.MIME)
		}
	}
	if err := b.store.Put(ctx, k, typ.MIME, body); err != nil {
		// The backend (or its SDK) may wrap the reader's error beyond
		// errors.Is reach — the metered reader is the source of truth.
		if metered.exceeded {
			return Info{}, fmt.Errorf("%w (limit %d bytes)", ErrTooLarge, b.maxSize)
		}
		return Info{}, err
	}
	return Info{Key: key, ContentType: typ.MIME, Size: metered.n}, nil
}

// Get returns the object's content and Info; the caller must close the
// reader. A missing key reports ErrNotFound.
func (b *Bucket) Get(ctx context.Context, key string) (io.ReadCloser, Info, error) {
	if b.store == nil {
		return nil, Info{}, errNotConstructed
	}
	k, err := b.scopedKey(ctx, key)
	if err != nil {
		return nil, Info{}, err
	}
	rc, info, err := b.store.Get(ctx, k)
	if err != nil {
		return nil, Info{}, err
	}
	info.Key = key
	return rc, info, nil
}

// Stat returns the object's Info without its content. A missing key reports
// ErrNotFound.
func (b *Bucket) Stat(ctx context.Context, key string) (Info, error) {
	if b.store == nil {
		return Info{}, errNotConstructed
	}
	k, err := b.scopedKey(ctx, key)
	if err != nil {
		return Info{}, err
	}
	info, err := b.store.Stat(ctx, k)
	if err != nil {
		return Info{}, err
	}
	info.Key = key
	return info, nil
}

// Delete removes the object. It is idempotent: deleting an absent key is not
// an error.
func (b *Bucket) Delete(ctx context.Context, key string) error {
	if b.store == nil {
		return errNotConstructed
	}
	k, err := b.scopedKey(ctx, key)
	if err != nil {
		return err
	}
	return b.store.Delete(ctx, k)
}

// List yields objects whose key starts with prefix (a raw string prefix, not
// a path boundary; empty lists everything) in backend-defined order. On a
// scoped bucket only the tenant's objects are visible and yielded keys have
// the scope stripped. Iteration stops at the first yielded error.
func (b *Bucket) List(ctx context.Context, prefix string) iter.Seq2[Info, error] {
	if b.store == nil {
		return errSeq(errNotConstructed)
	}
	if err := ValidatePrefix(prefix); err != nil {
		return errSeq(err)
	}
	var scope string
	if b.scope != nil {
		s, err := b.scopePrefix(ctx)
		if err != nil {
			return errSeq(err)
		}
		scope = s
	}
	return func(yield func(Info, error) bool) {
		for info, err := range b.store.List(ctx, scope+prefix) {
			if err != nil {
				yield(Info{}, err)
				return
			}
			info.Key = strings.TrimPrefix(info.Key, scope)
			if !yield(info, nil) {
				return
			}
		}
	}
}

// SignedGetURL returns a URL granting a plain HTTP GET of key for ttl. It
// reports ErrNotSupported when the backend does not implement URLSigner.
func (b *Bucket) SignedGetURL(ctx context.Context, key string, ttl time.Duration) (string, error) {
	signer, k, err := b.signer(ctx, key, ttl)
	if err != nil {
		return "", err
	}
	return signer.SignedGetURL(ctx, k, ttl)
}

// SignedPutURL returns a URL granting a plain HTTP PUT to key for ttl.
// Uploads through it bypass Put's type and size validation — validate after
// the fact (Stat) or confine such keys to a quarantine prefix. It reports
// ErrNotSupported when the backend does not implement URLSigner.
func (b *Bucket) SignedPutURL(ctx context.Context, key string, ttl time.Duration) (string, error) {
	signer, k, err := b.signer(ctx, key, ttl)
	if err != nil {
		return "", err
	}
	return signer.SignedPutURL(ctx, k, ttl)
}

func (b *Bucket) signer(ctx context.Context, key string, ttl time.Duration) (URLSigner, string, error) {
	if b.store == nil {
		return nil, "", errNotConstructed
	}
	signer, ok := b.store.(URLSigner)
	if !ok {
		return nil, "", ErrNotSupported
	}
	if ttl <= 0 {
		return nil, "", fmt.Errorf("objectstore: non-positive signed URL ttl %s", ttl)
	}
	k, err := b.scopedKey(ctx, key)
	if err != nil {
		return nil, "", err
	}
	return signer, k, nil
}

// errSeq returns an iterator that yields err once.
func errSeq(err error) iter.Seq2[Info, error] {
	return func(yield func(Info, error) bool) {
		yield(Info{}, err)
	}
}

// meteredReader counts bytes read and, when max > 0, fails the stream with
// ErrTooLarge once the count exceeds it.
type meteredReader struct {
	r        io.Reader
	n        int64
	max      int64
	exceeded bool
}

func (m *meteredReader) Read(p []byte) (int, error) {
	n, err := m.r.Read(p)
	m.n += int64(n)
	if m.max > 0 && m.n > m.max {
		m.exceeded = true
		return 0, fmt.Errorf("%w (limit %d bytes)", ErrTooLarge, m.max)
	}
	return n, err
}
