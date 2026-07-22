package objectstore_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/data/objectstore"
)

func newDisk(t *testing.T) (*objectstore.Disk, string) {
	t.Helper()
	dir := t.TempDir()
	d, err := objectstore.NewDisk(dir)
	if err != nil {
		t.Fatalf("NewDisk: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d, dir
}

func TestDiskCreatesRoot(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "nested", "uploads")
	d, err := objectstore.NewDisk(dir)
	if err != nil {
		t.Fatalf("NewDisk: %v", err)
	}
	defer func() { _ = d.Close() }()
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		t.Fatalf("root not created: %v", err)
	}
}

func TestDiskTraversalConfined(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	base := t.TempDir()
	secret := filepath.Join(base, "secret.txt")
	if err := os.WriteFile(secret, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	d, err := objectstore.NewDisk(filepath.Join(base, "store"))
	if err != nil {
		t.Fatalf("NewDisk: %v", err)
	}
	defer func() { _ = d.Close() }()

	for _, key := range []string{"../secret.txt", "a/../../secret.txt", "..", "/etc/passwd"} {
		if _, _, err := d.Get(ctx, key); !errors.Is(err, objectstore.ErrInvalidKey) {
			t.Errorf("Get(%q) = %v, want ErrInvalidKey", key, err)
		}
		if err := d.Put(ctx, key, "", strings.NewReader("x")); !errors.Is(err, objectstore.ErrInvalidKey) {
			t.Errorf("Put(%q) = %v, want ErrInvalidKey", key, err)
		}
	}
	if got, err := os.ReadFile(secret); err != nil || string(got) != "secret" {
		t.Fatalf("secret file touched: %q, %v", got, err)
	}
}

func TestDiskSymlinkEscapeBlocked(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on windows")
	}
	ctx := context.Background()
	base := t.TempDir()
	outside := filepath.Join(base, "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	storeDir := filepath.Join(base, "store")
	d, err := objectstore.NewDisk(storeDir)
	if err != nil {
		t.Fatalf("NewDisk: %v", err)
	}
	defer func() { _ = d.Close() }()
	// A symlink planted inside the root pointing outside it must not be
	// followable through the store.
	if err := os.Symlink(base, filepath.Join(storeDir, "escape")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := d.Get(ctx, "escape/outside.txt"); err == nil {
		t.Fatal("Get through escaping symlink succeeded")
	}
	if err := d.Put(ctx, "escape/new.txt", "", strings.NewReader("x")); err == nil {
		t.Fatal("Put through escaping symlink succeeded")
	}
	if _, err := os.Stat(filepath.Join(base, "new.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("file escaped the root")
	}
}

func TestDiskTmpReserved(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d, dir := newDisk(t)
	for _, key := range []string{".tmp", ".tmp/x"} {
		if err := d.Put(ctx, key, "", strings.NewReader("x")); !errors.Is(err, objectstore.ErrInvalidKey) {
			t.Errorf("Put(%q) = %v, want ErrInvalidKey", key, err)
		}
		if _, _, err := d.Get(ctx, key); !errors.Is(err, objectstore.ErrInvalidKey) {
			t.Errorf("Get(%q) = %v, want ErrInvalidKey", key, err)
		}
	}
	// Objects exist alongside an in-flight temp file; List never shows .tmp.
	if err := d.Put(ctx, "real", "", bytes.NewReader(binBlob)); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".tmp", "leftover"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	keys := collectKeys(t, d.List(ctx, ""))
	if len(keys) != 1 || keys[0] != "real" {
		t.Fatalf("List = %v, want [real]", keys)
	}
	// A dot-prefixed FILE that is not .tmp itself is a normal object.
	if err := d.Put(ctx, ".tmpfile", "", bytes.NewReader(binBlob)); err != nil {
		t.Fatalf("Put .tmpfile: %v", err)
	}
	keys = collectKeys(t, d.List(ctx, ".tmp"))
	if len(keys) != 1 || keys[0] != ".tmpfile" {
		t.Fatalf("List(.tmp) = %v, want [.tmpfile]", keys)
	}
}

func TestDiskFailedPutLeavesNothing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d, dir := newDisk(t)
	boom := errors.New("stream broke")
	err := d.Put(ctx, "partial", "", io.MultiReader(bytes.NewReader(make([]byte, 1024)), &errReader{err: boom}))
	if !errors.Is(err, boom) {
		t.Fatalf("Put = %v, want stream error", err)
	}
	if _, _, err := d.Get(ctx, "partial"); !errors.Is(err, objectstore.ErrNotFound) {
		t.Fatalf("Get after failed Put = %v, want ErrNotFound", err)
	}
	// The temp file was cleaned up too.
	entries, err := os.ReadDir(filepath.Join(dir, ".tmp"))
	if err != nil {
		t.Fatalf("read tmp dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("temp leftovers: %v", entries)
	}
}

func TestDiskRedetectsContentType(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d, _ := newDisk(t)
	// The stored contentType hint is ignored; Get re-detects from bytes.
	if err := d.Put(ctx, "img", "text/plain", bytes.NewReader(pngHead)); err != nil {
		t.Fatalf("Put: %v", err)
	}
	info, err := d.Stat(ctx, "img")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.ContentType != "image/png" {
		t.Fatalf("ContentType = %q, want image/png", info.ContentType)
	}
}

func TestDiskGetDirectoryIsNotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d, _ := newDisk(t)
	if err := d.Put(ctx, "dir/file", "", bytes.NewReader(binBlob)); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, _, err := d.Get(ctx, "dir"); !errors.Is(err, objectstore.ErrNotFound) {
		t.Fatalf("Get(dir) = %v, want ErrNotFound", err)
	}
	if err := d.Delete(ctx, "dir"); err != nil {
		t.Fatalf("Delete(dir) = %v, want nil (not an object)", err)
	}
	if _, _, err := d.Get(ctx, "dir/file"); err != nil {
		t.Fatalf("dir/file gone after Delete(dir): %v", err)
	}
}

func TestDiskListSkipsSymlinks(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on windows")
	}
	ctx := context.Background()
	d, dir := newDisk(t)
	if err := d.Put(ctx, "real", "", bytes.NewReader(binBlob)); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := os.Symlink(filepath.Join(dir, "real"), filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	keys := collectKeys(t, d.List(ctx, ""))
	if len(keys) != 1 || keys[0] != "real" {
		t.Fatalf("List = %v, want [real]", keys)
	}
}

func TestDiskLargeObjectRoundtrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d, _ := newDisk(t)
	// Larger than the 512-byte sniff window and any internal buffer.
	payload := bytes.Repeat([]byte("0123456789abcdef"), 64*1024) // 1 MiB
	if err := d.Put(ctx, "big.bin", "", bytes.NewReader(payload)); err != nil {
		t.Fatalf("Put: %v", err)
	}
	rc, info, err := d.Get(ctx, "big.bin")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer func() { _ = rc.Close() }()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("content mismatch")
	}
	if info.Size != int64(len(payload)) {
		t.Fatalf("Size = %d, want %d", info.Size, len(payload))
	}
}
