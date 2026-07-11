package mmdb

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSourceBytes(t *testing.T) {
	data, closer, err := loadSource(source{bytes: []byte("hello mmap")}, false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := closer(); err != nil {
			t.Fatalf("close: %v", err)
		}
	}()
	if string(data) != "hello mmap" {
		t.Fatalf("got %q", data)
	}
}

func TestLoadSourceFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "db.bin")
	if err := os.WriteFile(p, []byte("hello mmap"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, inMem := range []bool{false, true} {
		data, closer, err := loadSource(source{path: p, hasPath: true}, inMem)
		if err != nil {
			t.Fatalf("inMemory=%v: %v", inMem, err)
		}
		if string(data) != "hello mmap" {
			t.Fatalf("inMemory=%v: got %q", inMem, data)
		}
		if err := closer(); err != nil {
			t.Fatalf("close: %v", err)
		}
	}
}

func TestLoadSourceEmpty(t *testing.T) {
	if _, _, err := loadSource(source{}, false); err == nil {
		t.Fatal("empty source should error")
	}
}
