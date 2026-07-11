//go:build !unix

package mmdb

import "os"

// mapFile reads path into memory (platforms without mmap support).
func mapFile(path string) (data []byte, closer func() error, err error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	return b, func() error { return nil }, nil
}

func readFile(path string) ([]byte, error) { return os.ReadFile(path) }
