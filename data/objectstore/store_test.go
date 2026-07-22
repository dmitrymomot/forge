package objectstore_test

import (
	"bytes"
	"context"
	"slices"
	"testing"

	"github.com/dmitrymomot/forge/data/objectstore"
	"github.com/dmitrymomot/forge/data/objectstore/storetest"
)

// pngHead is a valid PNG signature followed by filler — detected as image/png.
var pngHead = append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, bytes.Repeat([]byte{0xAB}, 64)...)

// binBlob is content no signature or fallback recognizes — application/octet-stream.
var binBlob = []byte{0x00, 0x01, 0x02, 0x03, 0xFE, 0xFF, 0x00, 0x10}

func collectKeys(t testing.TB, seq func(func(objectstore.Info, error) bool)) []string {
	t.Helper()
	return storetest.CollectKeys(t, seq)
}

func TestMemoryConformance(t *testing.T) {
	t.Parallel()
	storetest.Run(t, func(t *testing.T) objectstore.Store {
		t.Helper()
		return objectstore.NewMemory()
	})
}

func TestDiskConformance(t *testing.T) {
	t.Parallel()
	storetest.Run(t, func(t *testing.T) objectstore.Store {
		t.Helper()
		d, err := objectstore.NewDisk(t.TempDir())
		if err != nil {
			t.Fatalf("NewDisk: %v", err)
		}
		t.Cleanup(func() { _ = d.Close() })
		return d
	})
}

func TestMemoryListLexicographic(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := objectstore.NewMemory()
	for _, key := range []string{"b", "a", "c/x", "c/a"} {
		if err := m.Put(ctx, key, "application/octet-stream", bytes.NewReader(binBlob)); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}
	keys := collectKeys(t, m.List(ctx, ""))
	if want := []string{"a", "b", "c/a", "c/x"}; !slices.Equal(keys, want) {
		t.Fatalf("List = %v, want %v", keys, want)
	}
}

func TestMemoryStoresContentTypeVerbatim(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := objectstore.NewMemory()
	if err := m.Put(ctx, "k", "application/x-custom", bytes.NewReader(binBlob)); err != nil {
		t.Fatalf("Put: %v", err)
	}
	info, err := m.Stat(ctx, "k")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.ContentType != "application/x-custom" {
		t.Fatalf("ContentType = %q", info.ContentType)
	}
}
