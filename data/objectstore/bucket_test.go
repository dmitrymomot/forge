package objectstore_test

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/data/objectstore"
)

func newBucket(t *testing.T, opts ...objectstore.Option) *objectstore.Bucket {
	t.Helper()
	b, err := objectstore.New(objectstore.NewMemory(), opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return b
}

func TestNewValidation(t *testing.T) {
	t.Parallel()
	if _, err := objectstore.New(nil); !errors.Is(err, objectstore.ErrNilStore) {
		t.Errorf("New(nil) = %v, want ErrNilStore", err)
	}
	if _, err := objectstore.New(objectstore.NewMemory(), objectstore.WithMaxSize(-1)); err == nil {
		t.Error("WithMaxSize(-1) accepted")
	}
	if _, err := objectstore.New(objectstore.NewMemory(), objectstore.WithAllowedTypes("")); err == nil {
		t.Error("WithAllowedTypes(\"\") accepted")
	}
	// An empty expansion must not silently mean "allow everything".
	if _, err := objectstore.New(objectstore.NewMemory(), objectstore.WithAllowedTypes()); err == nil {
		t.Error("WithAllowedTypes() accepted")
	}
}

func TestZeroValueBucketFails(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var b objectstore.Bucket
	if _, err := b.Put(ctx, "k", bytes.NewReader(binBlob)); err == nil {
		t.Error("zero-value Put succeeded")
	}
	if _, _, err := b.Get(ctx, "k"); err == nil {
		t.Error("zero-value Get succeeded")
	}
	for _, err := range b.List(ctx, "") {
		if err == nil {
			t.Error("zero-value List yielded no error")
		}
	}
}

func TestBucketPutDetectsContentType(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	b := newBucket(t)
	info, err := b.Put(ctx, "img.png", bytes.NewReader(pngHead))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if info.ContentType != "image/png" {
		t.Errorf("ContentType = %q, want image/png", info.ContentType)
	}
	if info.Size != int64(len(pngHead)) {
		t.Errorf("Size = %d, want %d", info.Size, len(pngHead))
	}
	if info.Key != "img.png" {
		t.Errorf("Key = %q", info.Key)
	}

	// Unknown content is stored as octet-stream when no allowlist is set.
	info, err = b.Put(ctx, "blob", bytes.NewReader(binBlob))
	if err != nil {
		t.Fatalf("Put blob: %v", err)
	}
	if info.ContentType != "application/octet-stream" {
		t.Errorf("ContentType = %q, want application/octet-stream", info.ContentType)
	}
}

func TestBucketAllowedTypes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	b := newBucket(t, objectstore.WithAllowedTypes("image/png", "image/jpeg"))

	if _, err := b.Put(ctx, "ok.png", bytes.NewReader(pngHead)); err != nil {
		t.Fatalf("Put png: %v", err)
	}
	// A PNG renamed to .pdf is still a PNG — extension is irrelevant.
	if _, err := b.Put(ctx, "sneaky.pdf", bytes.NewReader(pngHead)); err != nil {
		t.Fatalf("Put png-as-pdf: %v", err)
	}
	// Text content is not in the allowlist.
	if _, err := b.Put(ctx, "no.txt", strings.NewReader("plain text")); !errors.Is(err, objectstore.ErrUnsupportedType) {
		t.Fatalf("Put text = %v, want ErrUnsupportedType", err)
	}
	// Unrecognizable content is rejected too.
	if _, err := b.Put(ctx, "no.bin", bytes.NewReader(binBlob)); !errors.Is(err, objectstore.ErrUnsupportedType) {
		t.Fatalf("Put unknown = %v, want ErrUnsupportedType", err)
	}
	// Nothing was stored for the rejected keys.
	if _, err := b.Stat(ctx, "no.txt"); !errors.Is(err, objectstore.ErrNotFound) {
		t.Fatalf("Stat rejected = %v, want ErrNotFound", err)
	}
}

func TestBucketMaxSize(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	b := newBucket(t, objectstore.WithMaxSize(100))

	if _, err := b.Put(ctx, "fits", bytes.NewReader(make([]byte, 100))); err != nil {
		t.Fatalf("Put at limit: %v", err)
	}
	if _, err := b.Put(ctx, "big", bytes.NewReader(make([]byte, 101))); !errors.Is(err, objectstore.ErrTooLarge) {
		t.Fatalf("Put over limit = %v, want ErrTooLarge", err)
	}
	if _, err := b.Stat(ctx, "big"); !errors.Is(err, objectstore.ErrNotFound) {
		t.Fatalf("Stat oversize = %v, want ErrNotFound", err)
	}

	// A cap below the 512-byte sniff window still works.
	small := newBucket(t, objectstore.WithMaxSize(4))
	if _, err := small.Put(ctx, "tiny", strings.NewReader("abcd")); err != nil {
		t.Fatalf("Put tiny: %v", err)
	}
	if _, err := small.Put(ctx, "five", strings.NewReader("abcde")); !errors.Is(err, objectstore.ErrTooLarge) {
		t.Fatalf("Put five = %v, want ErrTooLarge", err)
	}
}

func TestBucketInvalidKey(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	b := newBucket(t)
	for _, key := range []string{"", "../etc/passwd", "a//b", "/abs", "a\\b"} {
		if _, err := b.Put(ctx, key, bytes.NewReader(binBlob)); !errors.Is(err, objectstore.ErrInvalidKey) {
			t.Errorf("Put(%q) = %v, want ErrInvalidKey", key, err)
		}
	}
	if _, err := b.Put(ctx, "k", nil); err == nil {
		t.Error("Put(nil reader) succeeded")
	}
}

func TestBucketScope(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	type ctxKey struct{}
	scopeFn := func(ctx context.Context) (string, error) {
		s, _ := ctx.Value(ctxKey{}).(string)
		return s, nil
	}
	mem := objectstore.NewMemory()
	b, err := objectstore.New(mem, objectstore.WithScope(scopeFn))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	tenantA := context.WithValue(ctx, ctxKey{}, "tenant-a")
	tenantB := context.WithValue(ctx, ctxKey{}, "tenant-b")

	if _, err := b.Put(tenantA, "doc.png", bytes.NewReader(pngHead)); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// The backend sees the prefixed key.
	if _, err := mem.Stat(ctx, "tenant-a/doc.png"); err != nil {
		t.Fatalf("backend Stat: %v", err)
	}

	// The owning tenant reads it back under the bare key.
	info, err := b.Stat(tenantA, "doc.png")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Key != "doc.png" {
		t.Errorf("Key = %q, want doc.png (scope stripped)", info.Key)
	}

	// Another tenant cannot see it.
	if _, err := b.Stat(tenantB, "doc.png"); !errors.Is(err, objectstore.ErrNotFound) {
		t.Fatalf("cross-tenant Stat = %v, want ErrNotFound", err)
	}

	// List is confined to the tenant and strips the scope.
	if _, err := b.Put(tenantB, "own.png", bytes.NewReader(pngHead)); err != nil {
		t.Fatalf("Put tenant-b: %v", err)
	}
	var keys []string
	for info, err := range b.List(tenantA, "") {
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		keys = append(keys, info.Key)
	}
	if want := []string{"doc.png"}; !slices.Equal(keys, want) {
		t.Fatalf("List = %v, want %v", keys, want)
	}

	// Missing scope fails closed on every operation.
	if _, err := b.Put(ctx, "x.png", bytes.NewReader(pngHead)); !errors.Is(err, objectstore.ErrScope) {
		t.Fatalf("unscoped Put = %v, want ErrScope", err)
	}
	if _, _, err := b.Get(ctx, "doc.png"); !errors.Is(err, objectstore.ErrScope) {
		t.Fatalf("unscoped Get = %v, want ErrScope", err)
	}
	if err := b.Delete(ctx, "doc.png"); !errors.Is(err, objectstore.ErrScope) {
		t.Fatalf("unscoped Delete = %v, want ErrScope", err)
	}
	for _, err := range b.List(ctx, "") {
		if !errors.Is(err, objectstore.ErrScope) {
			t.Fatalf("unscoped List = %v, want ErrScope", err)
		}
	}
}

func TestBucketScopeErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Hook error fails closed.
	b := newBucket(t, objectstore.WithScope(func(context.Context) (string, error) {
		return "", errors.New("boom")
	}))
	if _, err := b.Put(ctx, "k.png", bytes.NewReader(pngHead)); !errors.Is(err, objectstore.ErrScope) {
		t.Fatalf("Put = %v, want ErrScope", err)
	}

	// A scope breaking the key grammar fails closed (it would escape the prefix).
	evil := newBucket(t, objectstore.WithScope(func(context.Context) (string, error) {
		return "../other", nil
	}))
	if _, err := evil.Put(ctx, "k.png", bytes.NewReader(pngHead)); !errors.Is(err, objectstore.ErrScope) {
		t.Fatalf("evil scope Put = %v, want ErrScope", err)
	}

	// Nil hook leaves the bucket unscoped.
	open := newBucket(t, objectstore.WithScope(nil))
	if _, err := open.Put(ctx, "k.png", bytes.NewReader(pngHead)); err != nil {
		t.Fatalf("nil-scope Put: %v", err)
	}
}

func TestBucketScopedKeyLength(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	b := newBucket(t, objectstore.WithScope(func(context.Context) (string, error) {
		return strings.Repeat("t", 100), nil
	}))
	// Key alone fits, scoped key exceeds 1024.
	key := strings.Repeat("k", 1000)
	if _, err := b.Put(ctx, key, bytes.NewReader(pngHead)); !errors.Is(err, objectstore.ErrInvalidKey) {
		t.Fatalf("Put = %v, want ErrInvalidKey", err)
	}
}

func TestBucketSignedURLsUnsupported(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	b := newBucket(t)
	if _, err := b.SignedGetURL(ctx, "k", time.Minute); !errors.Is(err, objectstore.ErrNotSupported) {
		t.Fatalf("SignedGetURL = %v, want ErrNotSupported", err)
	}
	if _, err := b.SignedPutURL(ctx, "k", time.Minute); !errors.Is(err, objectstore.ErrNotSupported) {
		t.Fatalf("SignedPutURL = %v, want ErrNotSupported", err)
	}
}

// signingStore stubs URLSigner on top of Memory to test the facade wiring.
type signingStore struct {
	*objectstore.Memory
	lastKey string
	lastTTL time.Duration
}

func (s *signingStore) SignedGetURL(_ context.Context, key string, ttl time.Duration) (string, error) {
	s.lastKey, s.lastTTL = key, ttl
	return "https://signed.example/GET/" + key, nil
}

func (s *signingStore) SignedPutURL(_ context.Context, key string, ttl time.Duration) (string, error) {
	s.lastKey, s.lastTTL = key, ttl
	return "https://signed.example/PUT/" + key, nil
}

func TestBucketSignedURLs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := &signingStore{Memory: objectstore.NewMemory()}
	b, err := objectstore.New(store, objectstore.WithScope(func(context.Context) (string, error) {
		return "t1", nil
	}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	url, err := b.SignedGetURL(ctx, "file.png", time.Minute)
	if err != nil {
		t.Fatalf("SignedGetURL: %v", err)
	}
	if url != "https://signed.example/GET/t1/file.png" {
		t.Errorf("url = %q", url)
	}
	if store.lastKey != "t1/file.png" {
		t.Errorf("signer key = %q, want scoped key", store.lastKey)
	}
	if _, err := b.SignedPutURL(ctx, "file.png", 0); err == nil {
		t.Error("zero ttl accepted")
	}
	if _, err := b.SignedGetURL(ctx, "../evil", time.Minute); !errors.Is(err, objectstore.ErrInvalidKey) {
		t.Errorf("traversal key = %v, want ErrInvalidKey", err)
	}
}

// errReader fails after emitting some bytes.
type errReader struct {
	data []byte
	err  error
	sent bool
}

func (e *errReader) Read(p []byte) (int, error) {
	if !e.sent {
		e.sent = true
		return copy(p, e.data), nil
	}
	return 0, e.err
}

func TestBucketPutReaderError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	b := newBucket(t)
	boom := errors.New("stream broke")
	if _, err := b.Put(ctx, "k", &errReader{data: pngHead, err: boom}); !errors.Is(err, boom) {
		t.Fatalf("Put = %v, want stream error", err)
	}
	if _, err := b.Stat(ctx, "k"); !errors.Is(err, objectstore.ErrNotFound) {
		t.Fatalf("Stat after failed Put = %v, want ErrNotFound", err)
	}
}
