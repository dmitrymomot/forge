//go:build unix

package mmdb

import (
	"os"
	"syscall"
)

// mapFile memory-maps path read-only, returning the mapped bytes and a closer
// that unmaps them.
func mapFile(path string) (data []byte, closer func() error, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = f.Close() }()
	fi, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}
	size := int(fi.Size())
	if size <= 0 {
		return nil, nil, ErrInvalidDatabase
	}
	b, err := syscall.Mmap(int(f.Fd()), 0, size, syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		return nil, nil, err
	}
	return b, func() error { return syscall.Munmap(b) }, nil
}

func readFile(path string) ([]byte, error) { return os.ReadFile(path) }
