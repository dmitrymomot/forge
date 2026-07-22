// Package storetest is the executable contract for objectstore.Store
// implementations. Every driver's test suite must call Run; the in-memory
// store is the reference implementation. The suite asserts only what the
// seam guarantees — key validation, roundtrips, idempotent deletes, raw
// string-prefix listing — and leaves ordering and content-type persistence
// (backend-defined) alone.
package storetest

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/data/objectstore"
)

// pngHead is a valid PNG signature followed by filler — detected as image/png.
var pngHead = append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, bytes.Repeat([]byte{0xAB}, 64)...)

// binBlob is content no signature or fallback recognizes — application/octet-stream.
var binBlob = []byte{0x00, 0x01, 0x02, 0x03, 0xFE, 0xFF, 0x00, 0x10}

// Run executes the Store conformance suite. factory must return a fresh,
// empty store (or one namespaced per test) each call.
func Run(t *testing.T, factory func(t *testing.T) objectstore.Store) {
	t.Helper()
	ctx := context.Background()

	t.Run("PutGetRoundtrip", func(t *testing.T) {
		s := factory(t)
		if err := s.Put(ctx, "a/b/file.png", "image/png", bytes.NewReader(pngHead)); err != nil {
			t.Fatalf("Put: %v", err)
		}
		rc, info, err := s.Get(ctx, "a/b/file.png")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		defer func() { _ = rc.Close() }()
		got, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if !bytes.Equal(got, pngHead) {
			t.Fatalf("content mismatch: got %d bytes, want %d", len(got), len(pngHead))
		}
		if info.Key != "a/b/file.png" {
			t.Errorf("Key = %q", info.Key)
		}
		if info.Size != int64(len(pngHead)) {
			t.Errorf("Size = %d, want %d", info.Size, len(pngHead))
		}
		if info.ContentType != "image/png" {
			t.Errorf("ContentType = %q, want image/png", info.ContentType)
		}
		if info.ModTime.IsZero() {
			t.Error("ModTime is zero")
		}
	})

	t.Run("PutReplacesExisting", func(t *testing.T) {
		s := factory(t)
		if err := s.Put(ctx, "k", "application/octet-stream", bytes.NewReader(binBlob)); err != nil {
			t.Fatalf("Put: %v", err)
		}
		next := []byte("replaced content")
		if err := s.Put(ctx, "k", "text/plain", bytes.NewReader(next)); err != nil {
			t.Fatalf("Put replace: %v", err)
		}
		rc, info, err := s.Get(ctx, "k")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		defer func() { _ = rc.Close() }()
		got, _ := io.ReadAll(rc)
		if !bytes.Equal(got, next) {
			t.Fatalf("content = %q, want %q", got, next)
		}
		if info.Size != int64(len(next)) {
			t.Errorf("Size = %d, want %d", info.Size, len(next))
		}
	})

	t.Run("MissingKey", func(t *testing.T) {
		s := factory(t)
		if _, _, err := s.Get(ctx, "absent"); !errors.Is(err, objectstore.ErrNotFound) {
			t.Fatalf("Get missing = %v, want ErrNotFound", err)
		}
		if _, err := s.Stat(ctx, "absent"); !errors.Is(err, objectstore.ErrNotFound) {
			t.Fatalf("Stat missing = %v, want ErrNotFound", err)
		}
	})

	t.Run("Stat", func(t *testing.T) {
		s := factory(t)
		before := time.Now().Add(-time.Minute)
		if err := s.Put(ctx, "dir/obj", "image/png", bytes.NewReader(pngHead)); err != nil {
			t.Fatalf("Put: %v", err)
		}
		info, err := s.Stat(ctx, "dir/obj")
		if err != nil {
			t.Fatalf("Stat: %v", err)
		}
		if info.Key != "dir/obj" || info.Size != int64(len(pngHead)) || info.ContentType != "image/png" {
			t.Errorf("Info = %+v", info)
		}
		if info.ModTime.Before(before) {
			t.Errorf("ModTime %v too old", info.ModTime)
		}
	})

	t.Run("DeleteIdempotent", func(t *testing.T) {
		s := factory(t)
		if err := s.Put(ctx, "gone", "application/octet-stream", bytes.NewReader(binBlob)); err != nil {
			t.Fatalf("Put: %v", err)
		}
		if err := s.Delete(ctx, "gone"); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if _, _, err := s.Get(ctx, "gone"); !errors.Is(err, objectstore.ErrNotFound) {
			t.Fatalf("Get after delete = %v, want ErrNotFound", err)
		}
		if err := s.Delete(ctx, "gone"); err != nil {
			t.Fatalf("second Delete: %v", err)
		}
		if err := s.Delete(ctx, "never/existed"); err != nil {
			t.Fatalf("Delete absent: %v", err)
		}
	})

	t.Run("ListByPrefix", func(t *testing.T) {
		s := factory(t)
		for _, key := range []string{"a/1", "a/2", "a/sub/3", "b/1", "top"} {
			if err := s.Put(ctx, key, "application/octet-stream", bytes.NewReader(binBlob)); err != nil {
				t.Fatalf("Put %q: %v", key, err)
			}
		}
		keys := CollectKeys(t, s.List(ctx, "a/"))
		slices.Sort(keys)
		if want := []string{"a/1", "a/2", "a/sub/3"}; !slices.Equal(keys, want) {
			t.Fatalf("List(a/) = %v, want %v", keys, want)
		}
		all := CollectKeys(t, s.List(ctx, ""))
		if len(all) != 5 {
			t.Fatalf("List(\"\") = %v, want 5 keys", all)
		}
		none := CollectKeys(t, s.List(ctx, "zzz"))
		if len(none) != 0 {
			t.Fatalf("List(zzz) = %v, want empty", none)
		}
	})

	t.Run("ListRawStringPrefix", func(t *testing.T) {
		s := factory(t)
		for _, key := range []string{"img/u1.png", "img/u12.png", "img/u2.png"} {
			if err := s.Put(ctx, key, "image/png", bytes.NewReader(pngHead)); err != nil {
				t.Fatalf("Put %q: %v", key, err)
			}
		}
		keys := CollectKeys(t, s.List(ctx, "img/u1"))
		slices.Sort(keys)
		if want := []string{"img/u1.png", "img/u12.png"}; !slices.Equal(keys, want) {
			t.Fatalf("List(img/u1) = %v, want %v", keys, want)
		}
	})

	t.Run("ListStopsWhenConsumerBreaks", func(t *testing.T) {
		s := factory(t)
		for _, key := range []string{"x/1", "x/2", "x/3"} {
			if err := s.Put(ctx, key, "application/octet-stream", bytes.NewReader(binBlob)); err != nil {
				t.Fatalf("Put: %v", err)
			}
		}
		var seen int
		for _, err := range s.List(ctx, "x/") {
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			seen++
			break
		}
		if seen != 1 {
			t.Fatalf("seen = %d, want 1", seen)
		}
	})

	t.Run("ListInfoHasSizeAndModTime", func(t *testing.T) {
		s := factory(t)
		if err := s.Put(ctx, "sized", "application/octet-stream", bytes.NewReader(binBlob)); err != nil {
			t.Fatalf("Put: %v", err)
		}
		for info, err := range s.List(ctx, "sized") {
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if info.Size != int64(len(binBlob)) {
				t.Errorf("Size = %d, want %d", info.Size, len(binBlob))
			}
			if info.ModTime.IsZero() {
				t.Error("ModTime is zero")
			}
		}
	})

	t.Run("InvalidKeysRejected", func(t *testing.T) {
		s := factory(t)
		for _, key := range []string{
			"", "/abs", "a//b", "a/", "..", "../x", "a/../b", "a/./b", ".",
			"a\\b", "a\x00b", "a\nb", string([]byte{0xff, 0xfe}), strings.Repeat("k", 1025),
		} {
			if err := s.Put(ctx, key, "application/octet-stream", bytes.NewReader(binBlob)); !errors.Is(err, objectstore.ErrInvalidKey) {
				t.Errorf("Put(%q) = %v, want ErrInvalidKey", key, err)
			}
			if _, _, err := s.Get(ctx, key); !errors.Is(err, objectstore.ErrInvalidKey) {
				t.Errorf("Get(%q) = %v, want ErrInvalidKey", key, err)
			}
			if err := s.Delete(ctx, key); !errors.Is(err, objectstore.ErrInvalidKey) {
				t.Errorf("Delete(%q) = %v, want ErrInvalidKey", key, err)
			}
		}
	})

	t.Run("CanceledContext", func(t *testing.T) {
		s := factory(t)
		canceled, cancel := context.WithCancel(context.Background())
		cancel()
		if err := s.Put(canceled, "c", "application/octet-stream", bytes.NewReader(binBlob)); err == nil {
			t.Error("Put with canceled context succeeded")
		}
	})

	t.Run("ConcurrentAccess", func(t *testing.T) {
		s := factory(t)
		var wg sync.WaitGroup
		for i := range 8 {
			wg.Go(func() {
				key := "conc/" + string(rune('a'+i))
				if err := s.Put(ctx, key, "application/octet-stream", bytes.NewReader(binBlob)); err != nil {
					t.Errorf("Put %q: %v", key, err)
					return
				}
				rc, _, err := s.Get(ctx, key)
				if err != nil {
					t.Errorf("Get %q: %v", key, err)
					return
				}
				_, _ = io.ReadAll(rc)
				_ = rc.Close()
			})
		}
		wg.Wait()
		if got := len(CollectKeys(t, s.List(ctx, "conc/"))); got != 8 {
			t.Fatalf("List(conc/) = %d keys, want 8", got)
		}
	})
}

// CollectKeys drains a List iterator, failing the test on a yielded error.
func CollectKeys(t testing.TB, seq func(func(objectstore.Info, error) bool)) []string {
	t.Helper()
	var keys []string
	for info, err := range seq {
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		keys = append(keys, info.Key)
	}
	return keys
}
