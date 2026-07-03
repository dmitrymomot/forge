package logger

import (
	"fmt"
	"os"
	"path/filepath"
)

// openFile opens path for appending, creating the file and any missing parent
// directories. Both the mkdir and open failures are wrapped with ErrOpenFile.
func openFile(path string) (*os.File, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrOpenFile, err)
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrOpenFile, err)
	}
	return f, nil
}
